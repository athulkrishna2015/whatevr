//go:build whatevr_mock

package wamock

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	// Sidecar is the per-chunk MAC table that lets the daemon fetch and verify
	// a range instead of the whole file. Only audio and video carry one on the
	// wire, but it costs nothing to compute for everything.
	Sidecar []byte
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
	sidecar := streamingSidecar(macKey, iv, ciphertext)
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
		Sidecar:    sidecar,
	}, nil
}

// streamingSidecar is one truncated HMAC per 64 KiB chunk of ciphertext, each
// covering the block before the chunk followed by the chunk itself. That is the
// leading-IV layout of the two internal/mediastream knows about, and it is what
// lets a video start playing before the whole file has arrived.
func streamingSidecar(macKey, iv, ciphertext []byte) []byte {
	const chunk = 64 * 1024
	var out []byte
	for offset := 0; offset < len(ciphertext); offset += chunk {
		end := min(offset+chunk, len(ciphertext))
		leading := iv
		if offset > 0 {
			leading = ciphertext[offset-aes.BlockSize : offset]
		}
		mac := hmac.New(sha256.New, macKey)
		mac.Write(leading)
		mac.Write(ciphertext[offset:end])
		out = append(out, mac.Sum(nil)[:10]...)
	}
	return out
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

// handleMMS is the media endpoint proper: POST uploads an attachment the
// account is sending, DELETE retires a blob the client has finished with.
func (s *Server) handleMMS(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleMediaUpload(w, r)
	case http.MethodDelete:
		// whatsmeow deletes every history sync chunk once it has read it. The
		// mock keeps the bytes: a scenario that replays has to be able to sync
		// again.
		w.WriteHeader(http.StatusOK)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleMediaUpload takes the encrypted bytes of something the account is
// sending and hosts them, which is what makes an outgoing attachment
// downloadable by the account's own other devices. The mock never sees the
// media key here: it arrives later, inside the message that points at this
// blob, which is exactly the property real end to end encrypted media has.
func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUploadBytes+1))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if len(body) > maxUploadBytes {
		http.Error(w, "too large", http.StatusRequestEntityTooLarge)
		return
	}

	path := fmt.Sprintf("%s%d?ccb=mock", mediaPathPrefix, s.media.nextID())
	s.media.put(strings.SplitN(path, "?", 2)[0], "application/octet-stream", body)

	sum := sha256.Sum256(body)
	response := map[string]string{
		"url":         "https://" + mediaHost + path,
		"direct_path": path,
		"handle":      hex.EncodeToString(sum[:8]),
		"object_id":   hex.EncodeToString(sum[8:16]),
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.log.Printf("upload response: %v", err)
	}
}

// maxUploadBytes is the ceiling on anything a frontend sends through the mock.
// Real WhatsApp caps attachments well below this; the number here exists to
// keep a runaway send from eating the machine's memory.
const maxUploadBytes = 128 << 20

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

// openHostedMedia is the download the client would do, done by the mock: fetch
// the blob, check its MAC, decrypt it, and check the plaintext hash. It is how
// the mock verifies its own upload endpoint rather than trusting it.
func (s *Server) openHostedMedia(directPath string, mediaKey, fileSHA []byte, mediaType whatsmeow.MediaType) error {
	if directPath == "" || len(mediaKey) != 32 {
		return fmt.Errorf("media reference is incomplete")
	}
	b, ok := s.media.get(strings.SplitN(directPath, "?", 2)[0])
	if !ok {
		return fmt.Errorf("nothing hosted at %s", directPath)
	}
	if len(b.body) < 10 {
		return fmt.Errorf("blob at %s is too short to carry a mac", directPath)
	}
	expanded := hkdfutil.SHA256(mediaKey, nil, []byte(mediaType), 112)
	iv, cipherKey, macKey := expanded[:16], expanded[16:48], expanded[48:80]

	// The copy is not optional: cbcutil.Decrypt decrypts in place, so handing
	// it the stored slice would leave the hosted blob as plaintext and every
	// later download of it would fail its own mac check.
	body := bytes.Clone(b.body)
	ciphertext, want := body[:len(body)-10], body[len(body)-10:]
	mac := hmac.New(sha256.New, macKey)
	mac.Write(iv)
	mac.Write(ciphertext)
	if !hmac.Equal(mac.Sum(nil)[:10], want) {
		return fmt.Errorf("mac mismatch on %s", directPath)
	}
	plaintext, err := cbcutil.Decrypt(cipherKey, iv, ciphertext)
	if err != nil {
		return fmt.Errorf("decrypt %s: %w", directPath, err)
	}
	if sum := sha256.Sum256(plaintext); len(fileSHA) == 32 && !hmac.Equal(sum[:], fileSHA) {
		return fmt.Errorf("plaintext hash mismatch on %s", directPath)
	}
	return nil
}
