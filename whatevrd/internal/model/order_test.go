package model

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// whatsapp stamps whole seconds, and in each pair here the ids sort against
// the order the messages were sent in
func TestASecondKeepsTheOrderMessagesCameIn(t *testing.T) {
	first := msgIn("ZZ", ashaPN, ashaPN, "", false, 5, text("first"))
	second := msgIn("AA", ashaPN, ashaPN, "", false, 5, text("second"))
	second.At = first.At.Add(300 * time.Millisecond)
	// history lists a chat newest first
	hist := historyConv(ashaPN,
		webMsg("HB", ashaPN, false, "", 2, text("newer")),
		webMsg("HZ", ashaPN, false, "", 2, text("older")))
	scen := []core.Input{hist, first, second}
	for i, order := range [][]core.Input{scen, reversed(scen)} {
		db := openModel(t)
		feed(t, db, order)
		ms, err := NewReader(db.Read()).Messages(context.Background(), []string{ashaPN}, Cursor{}, 10, true)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, m := range ms {
			got = append(got, m.ID)
		}
		if want := []string{"HZ", "HB", "ZZ", "AA"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("order %d: got %v, want %v", i, got, want)
		}
	}
}

// pieces is c as ingest logs a long conversation: n messages an input, the
// first with the rest of c
func pieces(c *waHistorySync.Conversation, n int) []core.Input {
	var out []core.Input
	for off := 0; off < len(c.Messages); off += n {
		p := &waHistorySync.Conversation{ID: c.ID}
		if off == 0 {
			p = proto.Clone(c).(*waHistorySync.Conversation)
		}
		p.Messages = c.Messages[off:min(off+n, len(c.Messages))]
		out = append(out, in(core.KindHistoryConversation, core.HistoryConversationHead{Notification: "N1", SyncType: "FULL", ID: c.GetID(), Offset: off}, pb(p), at(50)))
	}
	return out
}

func TestAConversationInPiecesFoldsAsOne(t *testing.T) {
	g := &waHistorySync.Conversation{ID: proto.String(grp), Name: proto.String("Pieces"), UnreadCount: proto.Uint32(2),
		ConversationTimestamp: proto.Uint64(uint64(base.Unix()))}
	a := &waHistorySync.Conversation{ID: proto.String(ashaPN), LidJID: proto.String(ashaL), Username: proto.String("asha"),
		ConversationTimestamp: proto.Uint64(uint64(base.Unix()))}
	for i := range 7 {
		// all in one second, so only the order in the blob tells them apart
		w := webMsg(fmt.Sprint("G", i), grp, false, boPN, 2, text(fmt.Sprint("g", i)))
		w.PushName = proto.String(fmt.Sprint("Bo ", i))
		g.Messages = append(g.Messages, &waHistorySync.HistorySyncMsg{Message: w})
		a.Messages = append(a.Messages, &waHistorySync.HistorySyncMsg{Message: webMsg(fmt.Sprint("A", i), ashaPN, i%2 == 0, "", 2, text(fmt.Sprint("a", i)))})
	}
	whole := append(pieces(g, 100), pieces(a, 100)...)
	split := append(pieces(g, 3), pieces(a, 3)...)
	if len(split) != 6 {
		t.Fatalf("%d pieces", len(split))
	}
	where := regexp.MustCompile(`\b(seq|off|len)=\S+ ?`)
	dump := func(ins []core.Input) map[string][]string {
		db := openModel(t)
		feed(t, db, ins)
		d, err := db.DumpDerived(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for table, lines := range d {
			for i, l := range lines {
				lines[i] = where.ReplaceAllString(l, "")
			}
			sortStrings(lines)
			// a duplicate input is a second copy of the same row
			var uniq []string
			for i, l := range lines {
				if i == 0 || l != lines[i-1] {
					uniq = append(uniq, l)
				}
			}
			d[table] = uniq
		}
		return d
	}
	want := dump(whole)
	for i, order := range [][]core.Input{split, reversed(split), append(append([]core.Input(nil), split...), split...)} {
		got := dump(order)
		for table := range want {
			if !reflect.DeepEqual(got[table], want[table]) {
				t.Errorf("order %d, table %s:\n got  %q\n want %q", i, table, got[table], want[table])
			}
		}
	}
}
