package whatsapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/model"
)

// scheduleTick is how often due scheduled texts are picked up. The composer
// schedules by the minute, so seconds-level precision buys nothing.
const scheduleTick = 10 * time.Second

// ScheduleText queues text for chat at sendAt. The input seq it is logged
// under is the schedule id the list and cancel name.
func (c *Client) ScheduleText(ctx context.Context, chat, text string, sendAt time.Time) (int64, error) {
	if strings.TrimSpace(chat) == "" {
		return 0, Errorf(ErrInvalid, "chat_id is required")
	}
	if strings.TrimSpace(text) == "" {
		return 0, Errorf(ErrInvalid, "text is required")
	}
	if !sendAt.After(time.Now()) {
		return 0, Errorf(ErrInvalid, "send_at is in the past")
	}
	h, err := json.Marshal(core.ScheduleHead{Op: "add", Chat: strings.TrimSpace(chat), Text: text, SendAt: sendAt.Unix()})
	if err != nil {
		return 0, err
	}
	seq, err := c.core.Append(ctx, core.Input{Kind: core.KindSchedule, V: 1, Head: h})
	if err != nil {
		return 0, err
	}
	c.waitLogged(ctx)
	return seq, nil
}

// Scheduled lists pending scheduled texts, soonest first. An empty chat
// lists every chat; otherwise only that chat's rows. Chat is the id the
// frontend shows, resolved to the key the fold stored.
func (c *Client) Scheduled(ctx context.Context, chat string) ([]model.Scheduled, error) {
	key := ""
	if strings.TrimSpace(chat) != "" {
		w, err := c.world()
		if err != nil {
			return nil, err
		}
		key = w.Now(model.Norm(chat))
	}
	rows, err := c.r.Scheduled(ctx, key, 200)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// CancelScheduled drops a scheduled text that has not gone out.
func (c *Client) CancelScheduled(ctx context.Context, id int64) error {
	if id <= 0 {
		return Errorf(ErrInvalid, "id is required")
	}
	rows, err := c.r.Scheduled(ctx, "", 0)
	if err != nil {
		return err
	}
	found := false
	for _, sc := range rows {
		if sc.ID == id {
			found = true
			break
		}
	}
	if !found {
		return Errorf(ErrNotFound, "no scheduled message %d", id)
	}
	return c.append(ctx, core.KindSchedule, core.ScheduleHead{Op: "cancel", ID: id}, nil)
}

// runScheduled sends due scheduled texts, oldest first, until ctx ends.
func (c *Client) runScheduled(ctx context.Context) {
	t := time.NewTicker(scheduleTick)
	defer t.Stop()
	for {
		c.sendDueScheduled(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (c *Client) sendDueScheduled(ctx context.Context) {
	due, err := c.r.DueScheduled(ctx, time.Now().Unix(), 50)
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: list due scheduled messages")
		return
	}
	for _, sc := range due {
		sum := sha256.Sum256([]byte(sc.Chat + "\x00" + sc.Text))
		_, err := c.SendText(ctx, Draft{Chat: sc.Chat,
			Once: ingest.Once{Key: "schedule:" + strconv.FormatInt(sc.ID, 10), Params: hex.EncodeToString(sum[:])}}, sc.Text)
		if err != nil {
			// offline or gone: the row stays and the next tick tries again;
			// the once key keeps a retry from sending twice.
			c.log.Warn().Err(err).Int64("scheduled", sc.ID).Msg("whatsapp: send due scheduled message")
			continue
		}
		if err := c.append(ctx, core.KindSchedule, core.ScheduleHead{Op: "sent", ID: sc.ID}, nil); err != nil {
			c.log.Warn().Err(err).Int64("scheduled", sc.ID).Msg("whatsapp: clear sent scheduled message")
		}
	}
}
