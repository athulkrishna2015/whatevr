//go:build whatevr_mock

package wamock

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/util/cbcutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"

	"go.mau.fi/whatsmeow"
)

// mediaHost is the name the mock hands out for downloads. It has to look like
// a WhatsApp host or the transport refuses to dial it.
const mediaHost = "mmg.whatsapp.net"

// avatarHost is where profile pictures come from, kept separate because the
// daemon fetches those with a plain http.Client rather than the media one.
const avatarHost = "pps.whatsapp.net"

const mediaPathPrefix = "/mock/media/"
const avatarPathPrefix = "/mock/avatar/"

// blob is one thing the mock is hosting: the exact bytes a GET returns.
type blob struct {
	contentType string
	body        []byte
}

// mediaRef is everything a message needs to say in order to point at a blob.
type mediaRef struct {
	DirectPath string
	MediaKey   []byte
	FileSHA256 []byte
	FileEncSHA []byte
	FileLength uint64
}

type mediaStore struct {
	mu    sync.Mutex
	blobs map[string]blob
	seq   int
}

func newMediaStore() *mediaStore {
	return &mediaStore{blobs: map[string]blob{}}
}

func (m *mediaStore) get(path string) (blob, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[path]
	return b, ok
}

func (m *mediaStore) put(path, contentType string, body []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[path] = blob{contentType: contentType, body: body}
}

func (m *mediaStore) nextID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	return m.seq
}

// putEncrypted hosts a blob the way WhatsApp hosts media: AES-CBC under a key
// derived from a random media key, with a truncated HMAC appended. The client
// does the whole real download and decrypt, which is the point.
func (s *Server) putEncrypted(plaintext []byte, mediaType whatsmeow.MediaType) (mediaRef, error) {
	mediaKey := s.rng.bytes(32)
	expanded := hkdfutil.SHA256(mediaKey, nil, []byte(mediaType), 112)
	iv, cipherKey, macKey := expanded[:16], expanded[16:48], expanded[48:80]

	ciphertext, err := cbcutil.Encrypt(cipherKey, iv, plaintext)
	if err != nil {
		return mediaRef{}, fmt.Errorf("encrypt media: %w", err)
	}
	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ciphertext)
	body := append(ciphertext, mac.Sum(nil)[:10]...)

	fileSHA := sha256.Sum256(plaintext)
	encSHA := sha256.Sum256(body)
	// A real direct path already carries a query string, and the download URL
	// is built by appending &hash=... to it. Without the ?, that & starts a
	// path segment rather than a parameter.
	path := fmt.Sprintf("%s%d?ccb=mock", mediaPathPrefix, s.media.nextID())
	s.media.put(strings.SplitN(path, "?", 2)[0], "application/octet-stream", body)
	return mediaRef{
		DirectPath: path,
		MediaKey:   mediaKey,
		FileSHA256: fileSHA[:],
		FileEncSHA: encSHA[:],
		FileLength: uint64(len(plaintext)),
	}, nil
}

// putAvatar hosts an image unencrypted, which is how profile pictures work.
// The id is content addressed so a changed picture gets a new url, the way the
// daemon's avatar cache expects.
func (s *Server) putAvatar(data []byte) (id, url string) {
	sum := sha256.Sum256(data)
	id = hex.EncodeToString(sum[:8])
	path := avatarPathPrefix + id + ".jpg"
	s.media.put(path, "image/jpeg", data)
	return id, "https://" + avatarHost + path
}

// handleStickerPack answers the sticker pack catalogue fetch with an empty
// list, which is what a world with no sticker packs in it looks like. Stickers
// themselves are a later stage; an unanswered fetch here would only show up as
// a 404 in the log.
func (s *Server) handleStickerPack(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte("[]"))
}

// handleMediaDelete answers the client's request to drop a blob it has finished
// with, which is what whatsmeow does after every history sync chunk. The mock
// keeps the bytes: a scenario that replays has to be able to sync again.
func (s *Server) handleMediaDelete(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	b, ok := s.media.get(r.URL.Path)
	if !ok {
		s.log.Printf("media miss %s", r.URL.Path)
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", b.contentType)
	http.ServeContent(w, r, r.URL.Path, time.Time{}, strings.NewReader(string(b.body)))
}

// handleMediaConnIQ points the client's downloads at the mock. The auth token
// is not checked by anything; it exists because the client refuses a response
// without one.
func (s *session) handleMediaConnIQ(ctx context.Context, node *waBinary.Node) error {
	return s.sendNode(ctx, iqResult(node, waBinary.Node{
		Tag: "media_conn",
		Attrs: waBinary.Attrs{
			"auth":        "mock",
			"ttl":         "3600",
			"auth_ttl":    "3600",
			"max_buckets": "1",
		},
		Content: []waBinary.Node{{
			Tag:   "host",
			Attrs: waBinary.Attrs{"hostname": mediaHost},
		}},
	}))
}
