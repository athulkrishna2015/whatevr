package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"

	"whatevrd/internal/core"
	"whatevrd/internal/ingest"
	"whatevrd/internal/model"
)

func onceClient(t *testing.T) *Client {
	t.Helper()
	ctx := context.Background()
	db, err := core.Open(ctx, filepath.Join(t.TempDir(), "core.db"), core.Options{Domains: model.Domains(), Log: zerolog.Nop()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Client{ingest: ingest.New(ctx, db), r: model.NewReader(db.Read())}
}

func TestASendRepeatedUnderItsKeyQueuesOnce(t *testing.T) {
	c := onceClient(t)
	ctx := context.Background()
	const chat = "917770000001@s.whatsapp.net"
	var sends atomic.Int32
	send := func(o Once) func() (Ref, error) {
		return func() (Ref, error) {
			id := fmt.Sprintf("ID%d", sends.Add(1))
			return Ref{Chat: chat, ID: id}, c.ingest.Queued(ctx, chat, id, nil, "", o)
		}
	}
	o := Once{Key: "k1", Params: "p"}
	var wg sync.WaitGroup
	refs := make([]Ref, 8)
	for i := range refs {
		wg.Go(func() {
			r, err := c.once(ctx, o, send(o))
			if err != nil {
				t.Error(err)
			}
			refs[i] = r
		})
	}
	wg.Wait()
	if sends.Load() != 1 {
		t.Fatalf("%d sends", sends.Load())
	}
	for _, r := range refs {
		if r != refs[0] || r.ID == "" {
			t.Fatalf("answers %v", refs)
		}
	}
	if _, err := c.once(ctx, Once{Key: "k1", Params: "other"}, send(o)); !errors.Is(err, ErrInvalid) || sends.Load() != 1 {
		t.Fatalf("other params: %v, %d sends", err, sends.Load())
	}
	if _, err := c.once(ctx, Once{}, send(Once{})); err != nil || sends.Load() != 2 {
		t.Fatalf("no key: %v, %d sends", err, sends.Load())
	}
}

func TestAKeyedForwardAnswersInOrder(t *testing.T) {
	c := onceClient(t)
	ctx := context.Background()
	o := Once{Key: "fwd", Params: "p"}
	for _, chat := range []string{"917770000003@s.whatsapp.net", "917770000001@s.whatsapp.net", "917770000002@s.whatsapp.net"} {
		if err := c.ingest.Queued(ctx, chat, "F"+chat[11:12], nil, "", o); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := c.earlier(ctx, o)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 || refs[0].ID != "F3" || refs[1].ID != "F1" || refs[2].ID != "F2" {
		t.Fatalf("refs %v", refs)
	}
}
