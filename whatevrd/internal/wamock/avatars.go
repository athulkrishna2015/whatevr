//go:build whatevr_mock

package wamock

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"sync"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/types"
)

// avatarSize is the full resolution the mock serves. Real WhatsApp hands out
// 640 for the full picture and 96 for the preview; the daemon asks for the full
// one and scales it itself.
const avatarSize = 640
const avatarPreviewSize = 96

// avatars caches the generated picture for each jid so that a re-fetch gets the
// same id and the daemon's content-addressed cache stays put.
type avatarCache struct {
	mu   sync.Mutex
	byID map[string]avatarEntry
}

type avatarEntry struct {
	id  string
	url string
}

func newAvatarCache() *avatarCache { return &avatarCache{byID: map[string]avatarEntry{}} }

// handleProfilePictureIQ answers the avatar query. Everybody in the world has a
// picture; anybody else gets the 404 that the daemon renders as initials.
func (s *session) handleProfilePictureIQ(ctx context.Context, node *waBinary.Node) error {
	target := node.AttrGetter().OptionalJIDOrEmpty("target")
	if target.IsEmpty() {
		target = node.AttrGetter().OptionalJIDOrEmpty("to")
	}
	if target.Server == types.HiddenUserServer {
		target.Server = types.DefaultUserServer
	}
	world := s.srv.world
	if world == nil || !world.knows(target) {
		return s.sendNode(ctx, iqError(node, 404, "item-not-found"))
	}

	picture, _ := node.GetOptionalChildByTag("picture")
	preview := picture.AttrGetter().OptionalString("type") == "preview"
	entry, err := s.srv.avatarFor(target, preview)
	if err != nil {
		return s.sendNode(ctx, iqError(node, 404, "item-not-found"))
	}
	// A client that already has this picture asks with its id and gets told
	// nothing changed, which is the path the daemon's avatar TTL leans on.
	if existing := picture.AttrGetter().OptionalString("id"); existing == entry.id {
		return s.sendNode(ctx, iqResult(node, waBinary.Node{
			Tag:   "picture",
			Attrs: waBinary.Attrs{"status": "304"},
		}))
	}
	kind := "image"
	if preview {
		kind = "preview"
	}
	return s.sendNode(ctx, iqResult(node, waBinary.Node{
		Tag: "picture",
		Attrs: waBinary.Attrs{
			"id":          entry.id,
			"url":         entry.url,
			"type":        kind,
			"direct_path": avatarPathPrefix + entry.id + ".jpg",
		},
	}))
}

// avatarFor draws somebody's picture once and hosts it.
func (s *Server) avatarFor(jid types.JID, preview bool) (avatarEntry, error) {
	key := jid.ToNonAD().String()
	if preview {
		key += "|preview"
	}
	s.avatars.mu.Lock()
	defer s.avatars.mu.Unlock()
	if entry, ok := s.avatars.byID[key]; ok {
		return entry, nil
	}
	size := avatarSize
	if preview {
		size = avatarPreviewSize
	}
	data, err := drawAvatar(jid, size)
	if err != nil {
		return avatarEntry{}, err
	}
	id, url := s.putAvatar(data)
	entry := avatarEntry{id: id, url: url}
	s.avatars.byID[key] = entry
	return entry, nil
}

// drawAvatar makes a picture out of a jid: a flat colour with a lighter band
// across it, so two contacts are told apart at a glance and the same contact
// looks the same every run.
func drawAvatar(jid types.JID, size int) ([]byte, error) {
	sum := sha256.Sum256([]byte(jid.ToNonAD().String()))
	base := color.RGBA{R: 60 + sum[0]/2, G: 60 + sum[1]/2, B: 60 + sum[2]/2, A: 255}
	band := color.RGBA{R: 255 - sum[0]/3, G: 255 - sum[1]/3, B: 255 - sum[2]/3, A: 255}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	third := size / 3
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			pixel := base
			if (x+y)/third%2 == 1 {
				pixel = band
			}
			img.Set(x, y, pixel)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		return nil, fmt.Errorf("encode avatar: %w", err)
	}
	return buf.Bytes(), nil
}

// knows reports whether a jid belongs to anybody or any chat in the world.
func (w *World) knows(jid types.JID) bool {
	if jid.IsEmpty() {
		return false
	}
	key := jid.ToNonAD().String()
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.contacts[key]; ok {
		return true
	}
	_, ok := w.chats[key]
	return ok
}
