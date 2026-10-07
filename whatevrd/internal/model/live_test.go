package model

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

func liveOpen(lat float64) *waE2E.Message {
	return &waE2E.Message{LocationMessage: &waE2E.LocationMessage{IsLive: proto.Bool(true),
		DegreesLatitude: proto.Float64(lat), DegreesLongitude: proto.Float64(77)}}
}

func liveAt(lat float64, seq int64) *waE2E.Message {
	return &waE2E.Message{LiveLocationMessage: &waE2E.LiveLocationMessage{SequenceNumber: proto.Int64(seq),
		DegreesLatitude: proto.Float64(lat), DegreesLongitude: proto.Float64(77), SpeedInMps: proto.Float32(2)}}
}

func ids(t *testing.T, r *Reader, addrs ...string) []string {
	t.Helper()
	ms, err := r.Messages(context.Background(), addrs, Cursor{}, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// a share is the row it opened with, at its newest point; the updates are
// no rows of their own
func TestALiveShareIsOneRow(t *testing.T) {
	ins := []core.Input{
		msgIn("L0", ashaPN, ashaPN, "", false, 10, liveOpen(28.0)),
		msgIn("L1", ashaPN, ashaPN, "", false, 20, liveAt(28.1, 1)),
		msgIn("L2", ashaPN, ashaPN, "", false, 30, liveAt(28.2, 2)),
		msgIn("L3", ashaPN, ashaPN, "", false, 40, liveAt(28.3, 3)),
		msgIn("T1", ashaPN, ashaPN, "", false, 35, text("between")),
		// an update with no opener before it is a row of its own, and so is
		// one that comes after the share went quiet
		msgIn("O1", ashaPN, ashaPN, "", false, 3000, liveAt(29.0, 9)),
		msgIn("O2", ashaPN, ashaPN, "", false, 3010, liveAt(29.1, 10)),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		if got, want := ids(t, r, ashaPN), []string{"L0", "T1", "O1"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("transcript %v, want %v", got, want)
		}
		l := one(t, r, []string{ashaPN}, "L0").Facts.Live
		if l == nil || l.Points != 4 || l.Lat != 28.3 || l.Speed != 2 || l.Start != at(10).UnixMilli() || l.Updated != at(40).UnixMilli() {
			t.Fatalf("L0 share %+v", l)
		}
		if l := one(t, r, []string{ashaPN}, "O1").Facts.Live; l == nil || l.Points != 2 || l.Lat != 29.1 {
			t.Fatalf("O1 share %+v", l)
		}
		if one(t, r, []string{ashaPN}, "T1").Facts.Live != nil {
			t.Fatal("a text has a share")
		}
		trail, err := r.LiveTrail(context.Background(), ashaPN, "L0")
		if err != nil || len(trail) != 4 || trail[3].Lat != 28.3 {
			t.Fatalf("trail %+v %v", trail, err)
		}
		ctx := context.Background()
		shares, err := r.LiveShares(ctx, []string{ashaPN}, at(3020).UnixMilli())
		if err != nil || len(shares) != 1 || shares[0].ID != "O1" {
			t.Fatalf("running %+v %v", shares, err)
		}
		if shares, _ := r.LiveShares(ctx, []string{ashaPN}, at(3010+901).UnixMilli()); len(shares) != 0 {
			t.Fatalf("a quiet share still runs %+v", shares)
		}
	})
}

// a deleted opener keeps its updates hidden and forgets where it was
func TestARevokedShareStaysOneRow(t *testing.T) {
	ins := []core.Input{
		msgIn("L0", ashaPN, ashaPN, "", false, 10, liveOpen(28.0)),
		msgIn("L1", ashaPN, ashaPN, "", false, 20, liveAt(28.1, 1)),
		msgIn("X", ashaPN, ashaPN, "", false, 30, revokeOf(target(ashaPN, "L0", ""))),
		msgIn("X1", ashaPN, ashaPN, "", false, 31, revokeOf(target(ashaPN, "L1", ""))),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		if got, want := ids(t, r, ashaPN), []string{"L0"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("transcript %v, want %v", got, want)
		}
		expect(t, db, []string{"L0|<nil>", "L1|<nil>"}, `SELECT id, lat FROM live ORDER BY id`)
	})
}

// a deleted live location keeps that it was one in the log, history or not,
// so a rebuild hides the same updates
func TestADeletedShareRebuildsTheSame(t *testing.T) {
	scen := []core.Input{
		historyConv(ashaPN, webMsg("HL0", ashaPN, false, "", 10, liveOpen(28.0)), webMsg("HL1", ashaPN, false, "", 20, liveAt(28.1, 1))),
		msgIn("L0", ashaL, ashaL, ashaPN, false, 100, liveOpen(29.0)),
		msgIn("L1", ashaL, ashaL, ashaPN, false, 110, liveAt(29.1, 1)),
		msgIn("X0", ashaPN, ashaPN, "", false, 200, revokeOf(target(ashaPN, "HL0", ""))),
		msgIn("X1", ashaL, ashaL, ashaPN, false, 201, revokeOf(target(ashaL, "L0", ""))),
	}
	rebuildAgrees(t, scen, func(ins []core.Input) {
		for _, x := range ins {
			if strings.Contains(string(x.Body), "\x09\x00\x00\x00\x00\x00\x00\x3c\x40") || strings.Contains(string(x.Body), "\x09\x00\x00\x00\x00\x00\x00\x3d\x40") {
				t.Errorf("input %d still says where a deleted share was", x.Seq)
			}
		}
	})
}
