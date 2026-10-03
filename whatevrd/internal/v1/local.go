package v1

import (
	"context"
	"encoding/json"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/store"
)

// avatars is the pictures of every address of every key, in one read.
func (a *Adapter) avatars(ctx context.Context, w *model.World, keys []string) map[string]model.Avatar {
	var addrs []string
	for _, k := range keys {
		addrs = append(addrs, w.Addrs(k)...)
	}
	av, err := a.r.Avatars(ctx, addrs)
	if err != nil {
		a.log.Warn().Err(err).Msg("v1: avatars")
	}
	return av
}

// avatar is key's picture: of its addresses, the one with the newest answer.
func avatar(w *model.World, av map[string]model.Avatar, key string) (model.Avatar, bool) {
	var best model.Avatar
	found := false
	for _, addr := range w.Addrs(key) {
		x, ok := av[addr]
		if !ok {
			continue
		}
		if !found || better(x, best) {
			best, found = x, true
		}
	}
	return best, found
}

// better says x is the newer answer, any answer beating none
func better(x, than model.Avatar) bool {
	if (x.Status != "") != (than.Status != "") {
		return x.Status != ""
	}
	if x.Status != "" {
		return x.T > than.T
	}
	return x.LastTry > than.LastTry
}

// avatarStatus is the old store's word for how the newest fetch went.
func avatarStatus(x model.Avatar) string {
	switch x.Status {
	case core.AvatarOK:
		return store.AvatarStatusAvailable
	case core.AvatarNone:
		return store.AvatarStatusNotSet
	case core.AvatarHidden:
		return store.AvatarStatusUnauthorized
	}
	return store.AvatarStatusTransient
}

// local is what this daemon did for the row on its own: its file, a failed
// download, a voice note played, the full link preview picture, the group
// an invite points at.
func local(out *store.Message, l model.Local) {
	if l.File != "" {
		out.MediaLocalPath = l.File
		if l.W > 0 && l.H > 0 {
			out.MediaWidth, out.MediaHeight = l.W, l.H
		}
	}
	out.MediaDownloadError = l.DownloadError()
	out.MediaPlayed = out.MediaPlayed || l.Played
	if l.Poster != "" {
		out.MediaThumbnailLocalPath = l.Poster
	}
	if len(out.MediaWaveform) == 0 {
		out.MediaWaveform = l.Waveform
	}
	if l.Preview == "" && len(l.Invite) == 0 && l.InviteErr == "" {
		return
	}
	p := store.DecodePayload(out.PayloadJSON)
	if lp := p.LinkPreview; lp != nil && l.Preview != "" {
		lp.ThumbnailPath, lp.ThumbnailWidth, lp.ThumbnailHeight = l.Preview, int(l.PreviewW), int(l.PreviewH)
	}
	if g := p.GroupInvite; g != nil {
		var r store.GroupInvitePayload
		if len(l.Invite) > 0 && json.Unmarshal(l.Invite, &r) == nil {
			g.Subject, g.Topic, g.MemberCount, g.ResolvedAt = r.Subject, r.Topic, r.MemberCount, r.ResolvedAt
		}
		g.ResolveError = l.InviteErr
		out.PayloadSummary = g.DisplayName()
	}
	if enc, err := store.EncodePayload(p); err == nil {
		out.PayloadJSON = enc
	}
}
