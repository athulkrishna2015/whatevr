package protocol

import (
	"context"
	"log"
	"strings"
	"unicode/utf8"
)

// status.mark_viewed flags a status as seen locally. Viewed receipts to the
// sender are not sent yet; this only drives the local ring/badge state.
func (h commandHandlers) statusMarkViewed(ctx context.Context, _ *conn, req request) (any, *Error) {
	if err := h.requireActions(); err != nil {
		return nil, err
	}
	var p statusIDParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.StatusID) == "" {
		return nil, errorf(CodeInvalidParams, "status_id is required")
	}
	_, err := h.actions.MarkStatusViewed(ctx, strings.TrimSpace(p.StatusID))
	return nil, mapCommandError(err)
}

type statusIDParams struct {
	StatusID string `json:"status_id"`
}

// status.download is ack-then-lifecycle like media.download: the response is
// {} and the outcome is observable through the `status` view (media.path on
// success). Runs in the background so the command does not block on the
// fetch.
func (h commandHandlers) statusDownload(_ *conn, req request) (any, *Error) {
	if err := h.requireActions(); err != nil {
		return nil, err
	}
	var p statusIDParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if strings.TrimSpace(p.StatusID) == "" {
		return nil, errorf(CodeInvalidParams, "status_id is required")
	}
	statusID := strings.TrimSpace(p.StatusID)
	go func() {
		if _, err := h.actions.DownloadStatusMedia(context.Background(), statusID); err != nil {
			log.Printf("protocol: status.download %s: %v", statusID, err)
		}
	}()
	return nil, nil
}

type statusPostParams struct {
	Text    string `json:"text"`
	Path    string `json:"path"`
	Caption string `json:"caption"`
}

// status.post publishes a text or media status. Exactly one of text or path
// must be set; the response carries the stored status id for correlation, and
// the status itself arrives through the `status` view like anyone else's.
func (h commandHandlers) statusPost(ctx context.Context, _ *conn, req request) (any, *Error) {
	if err := h.requireActions(); err != nil {
		return nil, err
	}
	var p statusPostParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(p.Text) > maxCommandTextRunes {
		return nil, errorf(CodeInvalidParams, "text must be <= %d characters", maxCommandTextRunes)
	}
	if utf8.RuneCountInString(p.Caption) > maxCommandCaptionRunes {
		return nil, errorf(CodeInvalidParams, "caption must be <= %d characters", maxCommandCaptionRunes)
	}
	posted, err := h.actions.PostStatus(ctx, p.Text, strings.TrimSpace(p.Path), p.Caption)
	if perr := mapCommandError(err); perr != nil {
		return nil, perr
	}
	return map[string]any{"status_id": posted.ID}, nil
}
