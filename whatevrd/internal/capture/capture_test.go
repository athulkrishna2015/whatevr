package capture

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

func TestSegmentsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "c")
	w, err := Open(dir, "c", Start{Version: "test", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	seq := w.Write(Record{Kind: KindRecv, Frame: &Frame{Tag: "message", ID: "a", Data: []byte{1, 2, 3}}})
	w.Write(Record{Kind: KindDecrypted, Payload: &Payload{Ref: seq, Child: 0, Enc: "msg", Data: []byte("hi")}})
	w.Write(Record{Kind: KindAccount, Account: &Account{PN: "1:2@s.whatsapp.net", LID: "9:2@lid"}})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w2, err := Open(dir, "c", Start{Version: "test", PID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if w2.Segment() != 2 {
		t.Fatalf("second run got segment %d", w2.Segment())
	}
	w2.Close()

	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Segments) != 2 || c.Meta.Name != "c" {
		t.Fatalf("got %+v", c)
	}
	recs, err := c.Records(1)
	if err != nil {
		t.Fatal(err)
	}
	kinds := ""
	for _, r := range recs {
		kinds += r.Kind + " "
	}
	if kinds != "start recv decrypted account end " {
		t.Fatalf("kinds %q", kinds)
	}
	if recs[2].Payload.Ref != recs[1].Seq || string(recs[2].Payload.Data) != "hi" || !bytes.Equal(recs[1].Frame.Data, []byte{1, 2, 3}) {
		t.Fatalf("payload %+v frame %+v", recs[2].Payload, recs[1].Frame)
	}
	acct, err := c.Account(2)
	if err != nil || acct.LID != "9:2@lid" {
		t.Fatalf("account %+v %v", acct, err)
	}
}

func TestTornLastLineReads(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "c", Start{})
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	f, _ := os.OpenFile(segmentPath(dir, 1), os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString(`{"seq":9,"kind":"re`)
	f.Close()
	c, _ := Load(dir)
	recs, err := c.Records(1)
	if err != nil || len(recs) != 2 {
		t.Fatalf("got %d records, %v", len(recs), err)
	}
}

func TestBlobDedupAndCap(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(dir, "c", Start{})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	a, err := w.Blob([]byte("same"))
	if err != nil || a == "" {
		t.Fatal(a, err)
	}
	b, _ := w.Blob([]byte("same"))
	if a != b {
		t.Fatal("same bytes, two blobs")
	}
	old := BlobMax
	BlobMax = 4
	defer func() { BlobMax = old }()
	big, err := w.Blob([]byte("too big"))
	if err != nil || big != "" {
		t.Fatalf("over the cap stored %q %v", big, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, blobsDir, ".tmp-*"))
	if len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
}

func TestTransportRecordsBodies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write([]byte("body of " + r.URL.Path))
	}))
	defer srv.Close()

	dir := t.TempDir()
	w, err := Open(dir, "c", Start{})
	if err != nil {
		t.Fatal(err)
	}
	r := NewRecorder(w, zerolog.Nop())
	client := &http.Client{Transport: r.Transport(nil)}
	for _, path := range []string{"/whole", "/early"} {
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if path == "/whole" {
			io.ReadAll(resp.Body)
		} else {
			resp.Body.Read(make([]byte, 2))
		}
		resp.Body.Close()
	}
	r.Frontend(3, FrontendReq, []byte(`{"id":1,"method":"hello"}`))
	w.Close()

	c, _ := Load(dir)
	recs, _ := c.Records(1)
	var https []*HTTP
	for _, rec := range recs {
		if rec.HTTP != nil {
			https = append(https, rec.HTTP)
		}
	}
	if len(https) != 2 {
		t.Fatalf("got %d http records", len(https))
	}
	body, err := c.Blob(https[0].Blob)
	if err != nil || string(body) != "body of /whole" || https[0].Header["Content-Type"] != "image/jpeg" || https[0].Partial {
		t.Fatalf("whole: %+v %q %v", https[0], body, err)
	}
	if !https[1].Partial {
		t.Fatalf("early close not partial: %+v", https[1])
	}
	last := recs[len(recs)-2]
	if last.Frontend == nil || string(last.Frontend.Line) != `{"id":1,"method":"hello"}` {
		t.Fatalf("frontend %+v", last)
	}
}

func TestResolve(t *testing.T) {
	got, err := Resolve("mine", "/state")
	if err != nil || got != "/state/whatevr/captures/mine" {
		t.Fatal(got, err)
	}
	if _, err := Resolve("..", "/state"); err == nil {
		t.Fatal("accepted ..")
	}
	if got, _ := Resolve("./x", "/state"); !filepath.IsAbs(got) {
		t.Fatal(got)
	}
}
