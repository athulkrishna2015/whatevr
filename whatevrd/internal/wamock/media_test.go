//go:build whatevr_mock

package wamock

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"whatevrd/internal/mediastream"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// The sidecar is the mock's own invention rather than something whatsmeow
// computes, so it gets checked against the code that consumes it: if the layout
// is wrong the daemon silently falls back to whole-file downloads and streaming
// looks fine while never actually streaming.
func TestPutEncryptedSidecarVerifies(t *testing.T) {
	srv := testServer(t)
	plaintext := bytes.Repeat([]byte("stream me. "), 30000)

	ref, err := srv.putEncrypted(plaintext, whatsmeow.MediaVideo)
	if err != nil {
		t.Fatalf("putEncrypted: %v", err)
	}
	keys, err := mediastream.DeriveKeys(ref.MediaKey, string(whatsmeow.MediaVideo))
	if err != nil {
		t.Fatalf("derive keys: %v", err)
	}

	blob, ok := srv.media.get("/mock/media/1")
	if !ok {
		t.Fatal("nothing hosted")
	}
	ciphertext := blob.body[:len(blob.body)-10]

	chunks := mediastream.ChunkCount(int64(len(plaintext)))
	if chunks < 2 {
		t.Fatalf("test payload only spans %d chunks", chunks)
	}
	if want := chunks * 10; len(ref.Sidecar) != want {
		t.Fatalf("sidecar is %d bytes, want %d for %d chunks", len(ref.Sidecar), want, chunks)
	}

	verifier := mediastream.NewVerifier(keys, ref.Sidecar)
	decryptor, err := mediastream.NewDecryptor(keys, int64(len(plaintext)))
	if err != nil {
		t.Fatalf("decryptor: %v", err)
	}
	var rebuilt []byte
	for i := 0; i < chunks; i++ {
		start, end := mediastream.CipherRange(i, i, int64(len(plaintext)))
		body := ciphertext[start:end]
		leading := keys.IV
		if start > 0 {
			leading, body = body[:16], body[16:]
		}
		var trailing []byte
		if int(end)+16 <= len(ciphertext) {
			trailing = ciphertext[end : end+16]
		}
		if err := verifier.Verify(i, leading, body, trailing); err != nil {
			t.Fatalf("chunk %d does not verify: %v", i, err)
		}
		chunk, err := decryptor.DecryptChunk(i, leading, body)
		if err != nil {
			t.Fatalf("chunk %d does not decrypt: %v", i, err)
		}
		rebuilt = append(rebuilt, chunk...)
	}
	if !bytes.Equal(rebuilt, plaintext) {
		t.Fatal("chunks did not reassemble into the original")
	}
	if got := verifier.Layout(); got != mediastream.SidecarLayoutLeadingIV {
		t.Fatalf("layout = %v, want leading-iv", got)
	}
}

// The upload endpoint is what a frontend's own attachments go through, and its
// response shape is parsed by whatsmeow rather than by anything here.
func TestMediaUploadRoundTrips(t *testing.T) {
	srv := testServer(t)
	payload := []byte("an attachment the account is sending")

	recorder := httptest.NewRecorder()
	srv.handleMMS(recorder, httptest.NewRequest(http.MethodPost, "/mms/image/token?auth=mock", bytes.NewReader(payload)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload status %d", recorder.Code)
	}
	var response struct {
		URL        string `json:"url"`
		DirectPath string `json:"direct_path"`
		Handle     string `json:"handle"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}
	if response.DirectPath == "" || response.Handle == "" || response.URL == "" {
		t.Fatalf("incomplete upload response: %+v", response)
	}

	get := httptest.NewRecorder()
	srv.handleMedia(get, httptest.NewRequest(http.MethodGet, response.DirectPath, nil))
	if get.Code != http.StatusOK {
		t.Fatalf("download status %d", get.Code)
	}
	if !bytes.Equal(get.Body.Bytes(), payload) {
		t.Fatal("what came back is not what went up")
	}
}

// Every attachment kind has to produce a message the mock can open again with
// nothing but the key inside it, which is the same thing the daemon does.
func TestEveryAttachmentOpens(t *testing.T) {
	srv := testServer(t)
	for _, attachment := range []*Attachment{
		Image("caption"),
		Video("caption", 2*time.Second),
		GIF("caption"),
		VideoNote(2 * time.Second),
		Voice(2 * time.Second),
		Audio("title", 2*time.Second),
		Document("report.pdf"),
		Document("notes.txt"),
		Sticker(),
	} {
		message, err := srv.buildAttachment(attachment, "seed-"+attachment.kind)
		if err != nil {
			t.Fatalf("%s: %v", attachment.kind, err)
		}
		if !hasMedia(message) {
			t.Fatalf("%s built a message with no media in it", attachment.kind)
		}
		// checkUploadedMedia logs rather than returns, so go through the same
		// opener it uses and assert on the error directly.
		directPath, key, sha, kind := mediaFields(message)
		if err := srv.openHostedMedia(directPath, key, sha, kind); err != nil {
			t.Fatalf("%s: %v", attachment.kind, err)
		}
	}
}

// The sticker store is three different responses off one path, and the daemon
// parses each of them into a different shape.
func TestStickerStoreServesIndexPacksAndTrays(t *testing.T) {
	srv := testServer(t)

	index := stickerRequest(t, srv, "/sticker?cat=all&lg=en&lottie=1")
	if len(index) != stickerPackCount {
		t.Fatalf("index lists %d packs, want %d", len(index), stickerPackCount)
	}
	if len(index[0].Stickers) != 0 {
		t.Fatal("the index should not carry pack contents")
	}
	if index[0].TrayImageID == "" {
		t.Fatal("pack has no tray image")
	}

	packs := stickerRequest(t, srv, "/sticker?lottie=1&cat=sticker_pack_data&id="+index[0].StickerPackID+"&lg=en")
	if len(packs) != 1 {
		t.Fatalf("pack fetch returned %d packs", len(packs))
	}
	if len(packs[0].Stickers) != stickersPerPack {
		t.Fatalf("pack holds %d stickers, want %d", len(packs[0].Stickers), stickersPerPack)
	}
	item := packs[0].Stickers[0]
	if err := srv.openHostedMedia(item.DirectPath, item.MediaKey, item.FileHash, whatsmeow.MediaImage); err != nil {
		t.Fatalf("pack sticker does not open: %v", err)
	}

	recorder := httptest.NewRecorder()
	srv.handleStickerStore(recorder, httptest.NewRequest(http.MethodGet, "/sticker?img="+index[0].TrayImageID, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("tray image status %d", recorder.Code)
	}
	if !bytes.HasPrefix(recorder.Body.Bytes(), []byte("\x89PNG")) {
		t.Fatal("tray image is not a png")
	}

	missing := stickerRequest(t, srv, "/sticker?cat=sticker_pack_data&id=nosuchpack")
	if len(missing) != 0 {
		t.Fatal("an unknown pack should come back empty")
	}
}

func stickerRequest(t *testing.T, srv *Server, target string) []types.StickerPack {
	t.Helper()
	recorder := httptest.NewRecorder()
	srv.handleStickerStore(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s: status %d", target, recorder.Code)
	}
	var packs []types.StickerPack
	if err := json.Unmarshal(recorder.Body.Bytes(), &packs); err != nil {
		t.Fatalf("%s: decode: %v", target, err)
	}
	return packs
}
