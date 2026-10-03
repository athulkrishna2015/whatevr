package model

import (
	"context"
	"reflect"
	"testing"
	"time"

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
