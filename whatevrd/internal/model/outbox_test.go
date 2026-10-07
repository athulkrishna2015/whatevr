package model

import (
	"context"
	"math/rand/v2"
	"reflect"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

func outboxIn(op, chat, id, err string, final bool, t int) core.Input {
	return in(core.KindOutbox, core.OutboxHead{Op: op, Chat: chat, ID: id, Error: err, Final: final}, nil, at(t))
}

func queueIn(chat, id, file string, m *waE2E.Message, t int) core.Input {
	return in(core.KindOutbox, core.OutboxHead{Op: core.OutboxQueue, Chat: chat, ID: id, File: file}, pb(m), at(t))
}

func photo(caption string) *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{Caption: proto.String(caption), Mimetype: proto.String("image/jpeg")}}
}

// outboxScenario is three sends this daemon queued, one of them sent after
// two failed tries, one cancelled, one still owed and queued twice; and a
// message of ours history left pending that nothing here ever queued.
func outboxScenario() []core.Input {
	return []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0)),
		queueIn(ashaPN, "Q1", "", text("finally"), 10),
		outboxIn(core.OutboxAttempt, ashaPN, "Q1", "no route", false, 11),
		outboxIn(core.OutboxAttempt, ashaPN, "Q1", "timed out", false, 12),
		msgIn("Q1", ashaPN, mePN, "", true, 13, text("finally")),
		outboxIn(core.OutboxQueue, ashaPN, "Q2", "", false, 14),
		outboxIn(core.OutboxCancel, ashaPN, "Q2", "", false, 15),
		queueIn(ashaPN, "Q3", "/tmp/a.jpg", photo("look"), 16),
		queueIn(ashaPN, "Q3", "/tmp/b.jpg", photo("again"), 18),
		outboxIn(core.OutboxAttempt, ashaPN, "Q3", "no route", false, 17),
		historyConv(ashaPN, webMsg("H9", ashaPN, true, "", 5, text("the phone never sent this"))),
	}
}

func TestOutboxFoldsTheSameInAnyOrder(t *testing.T) {
	scen := outboxScenario()
	ref := openModel(t)
	feed(t, ref, scen)
	want := normalized(t, ref, scen)
	r := rand.New(rand.NewPCG(3, 4))
	orders := [][]core.Input{reversed(scen), append(append([]core.Input(nil), scen...), scen...)}
	for range 8 {
		p := append([]core.Input(nil), scen...)
		r.Shuffle(len(p), func(i, j int) { p[i], p[j] = p[j], p[i] })
		orders = append(orders, p)
	}
	for i, order := range orders {
		db := openModel(t)
		feed(t, db, order)
		if got := normalized(t, db, scen); !reflect.DeepEqual(got, want) {
			t.Fatalf("order %d:\n got  %q\n want %q", i, got, want)
		}
	}
}

func TestOutboxStandsInUntilTheSendIsLogged(t *testing.T) {
	ctx := context.Background()
	db := openModel(t)
	feed(t, db, outboxScenario())
	r := NewReader(db.Read())

	q1, ok, err := r.Outgoing(ctx, ashaPN, "Q1")
	if err != nil || !ok || !q1.Sent || q1.Attempts != 2 || q1.Error != "timed out" {
		t.Errorf("Q1 %+v %v %v", q1, ok, err)
	}
	q2, _, _ := r.Outgoing(ctx, ashaPN, "Q2")
	if !q2.Cancelled {
		t.Errorf("Q2 %+v", q2)
	}
	if _, ok, _ := r.Outgoing(ctx, ashaPN, "H9"); ok {
		t.Error("a pending history message counts as queued here")
	}
	unsent, err := r.Unsent(ctx, nil, 1000)
	if err != nil || len(unsent) != 1 || unsent[0].ID != "Q3" || unsent[0].Attempts != 1 {
		t.Fatalf("unsent %+v %v", unsent, err)
	}

	msgs, err := r.Messages(ctx, []string{ashaPN}, Cursor{}, 50, true)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range msgs {
		tag := m.ID
		if m.Queued {
			tag += " queued"
		}
		got = append(got, tag)
	}
	// Q1 is its real row now, Q2 is gone, Q3 waits
	if want := []string{"H9", "Q1", "Q3 queued"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("transcript %v, want %v", got, want)
	}
	m, ok, err := r.Message(ctx, []string{ashaPN}, "Q3")
	if err != nil || !ok || !m.Queued || m.Out.Error != "no route" {
		t.Fatalf("Q3 by id %+v %v %v", m, ok, err)
	}
	// the first queue is the send
	if raw, _ := m.Content(); m.Kind != "imageMessage" || m.Text != "look" || m.Out.File != "/tmp/a.jpg" || raw.GetImageMessage().GetCaption() != "look" {
		t.Fatalf("Q3 is %q %q %q %v", m.Kind, m.Text, m.Out.File, raw)
	}
}

// a chat whose only message is a send still queued is in the list, and
// drops out again when that send is cancelled
func TestQueuedSendMakesItsChat(t *testing.T) {
	ctx := context.Background()
	db := openModel(t)
	feed(t, db, []core.Input{
		in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0)),
		msgIn("G1", grp, boL, boPN, false, 10, text("old")),
		outboxIn(core.OutboxQueue, ashaPN, "Q1", "", false, 20),
	})
	r := NewReader(db.Read())
	chats, err := r.Chats(ctx, ChatFilter{})
	if err != nil || len(chats) != 2 || chats[0].Key != w(t, r).Now(ashaPN) || chats[0].Unread != 0 {
		t.Fatalf("chats %+v %v, want asha first for the queued send", chats, err)
	}
	if c, ok, err := r.Chat(ctx, ashaPN); err != nil || !ok || c.LastT != at(20).UnixMilli() {
		t.Fatalf("chat %+v %v %v", c, ok, err)
	}
	feed(t, db, []core.Input{outboxIn(core.OutboxCancel, ashaPN, "Q1", "", false, 21)})
	if chats, err := r.Chats(ctx, ChatFilter{}); err != nil || len(chats) != 1 {
		t.Fatalf("after cancel %+v %v", chats, err)
	}
}

func w(t *testing.T, r *Reader) *World {
	t.Helper()
	w, err := r.World(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestTheOutboxIsReadAPageAtATime(t *testing.T) {
	ctx := context.Background()
	db := openModel(t)
	// one second, so the log's order breaks the tie
	scen := []core.Input{in(core.KindLIDMapping, core.LIDMappingHead{LID: meLID, PN: mePN, Self: true}, nil, at(0))}
	for _, id := range []string{"A", "B", "C", "D", "E"} {
		scen = append(scen, queueIn(ashaPN, id, "", text(id), 10))
	}
	feed(t, db, scen)
	r := NewReader(db.Read())
	var got []string
	var after *Outgoing
	for {
		page, err := r.Unsent(ctx, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page {
			got = append(got, o.ID)
		}
		if len(page) < 2 {
			break
		}
		after = &page[len(page)-1]
	}
	if want := []string{"A", "B", "C", "D", "E"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("paged %v, want %v", got, want)
	}
	if n, err := r.UnsentCount(ctx); err != nil || n != 5 {
		t.Fatalf("count %d %v", n, err)
	}
}
