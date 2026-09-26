//go:build whatevr_mock

package wamock

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// The sticker store is not part of the XMPP protocol at all: whatsmeow and the
// daemon both fetch it over plain https from static.whatsapp.net. Three shapes
// of request come out of that one path, told apart by the query string.
const (
	stickerPackCount    = 3
	stickersPerPack     = 8
	stickerPixelSize    = 512
	trayImagePixelSize  = 96
	stickerPackIDPrefix = "mockpack"
)

// stickerCatalogue is the mock's sticker store: packs, their stickers, and the
// tray images the picker shows. Everything is generated once, on the first
// request, because a scenario that never opens the sticker picker should not
// pay for encrypting two dozen WebPs.
type stickerCatalogue struct {
	mu     sync.Mutex
	once   sync.Once
	packs  []types.StickerPack
	byID   map[string]types.StickerPack
	trays  map[string][]byte
	failed error
}

func newStickerCatalogue() *stickerCatalogue {
	return &stickerCatalogue{byID: map[string]types.StickerPack{}, trays: map[string][]byte{}}
}

// packNames are what the picker lists. Three packs is enough for the store to
// have more than one row and for a pack switch to be a visible thing.
var packNames = []struct{ name, publisher, description string }{
	{"Mock Shapes", "whatevr", "flat colour, sharp edges"},
	{"Mock Faces", "whatevr", "the same shapes, differently coloured"},
	{"Mock Signals", "whatevr", "a third pack, so the tray scrolls"},
}

func (s *Server) stickerStore() (*stickerCatalogue, error) {
	catalogue := s.stickers
	catalogue.once.Do(func() { catalogue.failed = s.buildStickerCatalogue(catalogue) })
	return catalogue, catalogue.failed
}

func (s *Server) buildStickerCatalogue(catalogue *stickerCatalogue) error {
	catalogue.mu.Lock()
	defer catalogue.mu.Unlock()
	for p := 0; p < stickerPackCount; p++ {
		meta := packNames[p%len(packNames)]
		packID := fmt.Sprintf("%s%d", stickerPackIDPrefix, p+1)

		// Tray art is png on the real store, and the daemon writes it to disk
		// under that extension without looking at the bytes. Stickers
		// themselves are webp; the tray is not.
		tray, err := synthPNG(trayImagePixelSize, trayImagePixelSize, packID)
		if err != nil {
			return fmt.Errorf("tray image for %s: %w", packID, err)
		}
		trayID := packID + "-tray"
		catalogue.trays[trayID] = tray

		pack := types.StickerPack{
			StickerPackID: packID,
			Name:          meta.name,
			Publisher:     meta.publisher,
			Description:   meta.description,
			TrayImageID:   trayID,
			ImageDataHash: trayID,
			FileSize:      fmt.Sprintf("%d", len(tray)),
			// A tray preview is a data url the picker can draw before it has
			// fetched anything, which is what stops the store flashing empty.
			TrayImagePreview: "data:image/png;base64," + base64.StdEncoding.EncodeToString(tray),
		}
		for i := 0; i < stickersPerPack; i++ {
			seed := fmt.Sprintf("%s-%d", packID, i)
			data, err := synthWebP(stickerPixelSize, stickerPixelSize, seed)
			if err != nil {
				return fmt.Errorf("sticker %s: %w", seed, err)
			}
			ref, err := s.putEncrypted(data, whatsmeow.MediaImage)
			if err != nil {
				return fmt.Errorf("host sticker %s: %w", seed, err)
			}
			pack.Stickers = append(pack.Stickers, &types.StickerPackItem{
				MediaKey:          ref.MediaKey,
				EncFileHash:       ref.FileEncSHA,
				FileHash:          ref.FileSHA256,
				DirectPath:        ref.DirectPath,
				URL:               "https://" + mediaHost + ref.DirectPath,
				FileSize:          int64(ref.FileLength),
				MimeType:          "image/webp",
				Width:             stickerPixelSize,
				Height:            stickerPixelSize,
				Emojis:            stickerEmojis(i),
				AccessibilityText: fmt.Sprintf("%s sticker %d", meta.name, i+1),
			})
			pack.PreviewImageIDs = append(pack.PreviewImageIDs, seed)
		}
		catalogue.packs = append(catalogue.packs, pack)
		catalogue.byID[packID] = pack
	}
	return nil
}

// stickerEmojis is the search corpus the picker filters on, so a scenario can
// type into the sticker search and get something back.
func stickerEmojis(index int) []string {
	table := [][]string{
		{"😀", "🙂"}, {"😂"}, {"😍", "❤️"}, {"👍"},
		{"🎉", "🥳"}, {"🔥"}, {"😭"}, {"🤔"},
	}
	return table[index%len(table)]
}

// handleStickerStore serves all three sticker store requests: the pack index,
// one pack's contents, and a tray image.
func (s *Server) handleStickerStore(w http.ResponseWriter, r *http.Request) {
	catalogue, err := s.stickerStore()
	if err != nil {
		s.log.Printf("sticker store: %v", err)
		http.Error(w, "sticker store unavailable", http.StatusInternalServerError)
		return
	}
	query := r.URL.Query()

	if imageID := query.Get("img"); imageID != "" {
		catalogue.mu.Lock()
		data, ok := catalogue.trays[imageID]
		catalogue.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
		return
	}

	catalogue.mu.Lock()
	defer catalogue.mu.Unlock()
	var out []types.StickerPack
	if packID := query.Get("id"); packID != "" {
		pack, ok := catalogue.byID[packID]
		if !ok {
			// An empty array is how the real endpoint says it has never heard
			// of a pack; whatsmeow turns that into "no sticker pack found".
			out = []types.StickerPack{}
		} else {
			out = []types.StickerPack{pack}
		}
	} else {
		// The index lists packs without their contents, which is what makes
		// the store cheap to open and the pack fetch a separate request.
		for _, pack := range catalogue.packs {
			listed := pack
			listed.Stickers = nil
			out = append(out, listed)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		s.log.Printf("sticker store response: %v", err)
	}
}
