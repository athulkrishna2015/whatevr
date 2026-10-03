package status

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOnlyTheOwnerSetsAKind(t *testing.T) {
	b := New()
	if err := b.Set("outbox", Problem{Kind: NoNetwork}); err == nil {
		t.Fatal("the outbox set the network state")
	}
	if err := b.Set("conn", Problem{Kind: NoNetwork, Detail: "no route"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Clear("sync", NoNetwork); err == nil {
		t.Fatal("sync cleared the network state")
	}
}

func TestADetailChangeKeepsSince(t *testing.T) {
	b := New()
	first := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	b.Set("conn", Problem{Kind: Refused, Since: first, Detail: "503"})
	b.Set("conn", Problem{Kind: Refused, Detail: "502"})
	ps := b.List(context.Background())
	if len(ps) != 1 || !ps[0].Since.Equal(first) || ps[0].Detail != "502" {
		t.Fatalf("%+v", ps)
	}
}

func TestOnlyClearsTheOwnersOtherKinds(t *testing.T) {
	b := New()
	b.Set("conn", Problem{Kind: Refused})
	b.Set("conn", Problem{Kind: KeepaliveLost})
	b.Set("media", Problem{Kind: MediaFailing})
	b.Only("conn", Problem{Kind: NoNetwork})
	got := map[Kind]bool{}
	for _, p := range b.List(context.Background()) {
		got[p.Kind] = true
	}
	if !got[NoNetwork] || got[Refused] || got[KeepaliveLost] || !got[MediaFailing] {
		t.Fatalf("%v", got)
	}
}

func TestProvidersAreReadWhenAskedAndOnlyForTheirKinds(t *testing.T) {
	b := New()
	b.Provide("sync", func(context.Context) ([]Problem, error) {
		return []Problem{{Kind: HistoryStalled}, {Kind: NoNetwork}}, nil
	})
	b.Provide("outbox", func(context.Context) ([]Problem, error) { return nil, errors.New("disk I/O error") })
	got := map[Kind]string{}
	for _, p := range b.List(context.Background()) {
		got[p.Kind] = p.Detail
	}
	if _, ok := got[HistoryStalled]; !ok {
		t.Error("the sync provider's problem is missing")
	}
	if _, ok := got[NoNetwork]; ok {
		t.Error("a provider answered for a kind it does not own")
	}
	if got[StoreFailing] != "outbox: disk I/O error" {
		t.Errorf("a failing provider is %q", got[StoreFailing])
	}
}
