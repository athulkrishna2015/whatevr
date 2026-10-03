package model

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/core"
)

// naiveSearch is the search as one LIKE over every message, what Search has
// to answer whichever way it goes.
func naiveSearch(ctx context.Context, db *sql.DB, query string, addrs []string, from Cursor, limit int) ([]string, error) {
	if from == (Cursor{}) {
		from = Cursor{T: tMax}
	}
	q := `SELECT m.chat, m.id FROM msg m NOT INDEXED
		WHERE m.text LIKE ? ESCAPE '\' AND (m.t, m.ord, m.id) < (?, ?, ?)
		AND m.text != ''`
	args := []any{"%" + escapeLike(query) + "%", from.T, from.Ord, from.ID}
	if len(addrs) > 0 {
		q += ` AND m.chat IN (` + placeholders(len(addrs)) + `)`
		args = append(args, anys(addrs)...)
	}
	q += ` ORDER BY m.t DESC, m.ord DESC, m.id DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c, id string
		if err := rows.Scan(&c, &id); err != nil {
			return nil, err
		}
		out = append(out, c+"/"+id)
	}
	return out, rows.Err()
}

func TestSearchAnswersAsOneLikeOverEverything(t *testing.T) {
	const chatA, chatB, chatC = "100000000001@lid", "917770000001@s.whatsapp.net", "1203630000001@g.us"
	words := []string{"radhe", "Radhe", "RADHE", "the", "THE", "café", "CAFÉ", "straße", "😀😀", "नमस्ते", "50%", "a_b",
		`back\slash`, `say "hi"`, "  ", "x", "ab", "abc", "abcd", "ünï", "\u0130stanbul", "ΣΊΣΥΦΟΣ", "σίσυφος"}
	rnd := rand.New(rand.NewSource(1))
	var ins []core.Input
	for i := range 400 {
		var parts []string
		for range 1 + rnd.Intn(3) {
			parts = append(parts, words[rnd.Intn(len(words))])
		}
		chat := []string{chatA, chatB, chatC}[i%3]
		ins = append(ins, msgIn(fmt.Sprintf("M%03d", i), chat, chat, "", i%4 == 0, rnd.Intn(60), text(strings.Join(parts, " "))))
	}
	// a deleted one stays out
	ins = append(ins, msgIn("X1", chatA, chatA, "", true, 70, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  &waCommon.MessageKey{RemoteJID: proto.String(chatA), ID: proto.String("M000"), FromMe: proto.Bool(true)},
	}}))
	db := openModel(t)
	feed(t, db, ins)
	ctx := context.Background()
	r := NewReader(db.Read())
	queries := append([]string{"", "a", "Ab", "%", "_", `\`, `"`, `""`, "e r", "hé", "zzz", "radhe radhe"}, words...)
	for _, w := range words {
		rs := []rune(w)
		for i := range rs {
			for j := i + 1; j <= len(rs) && j <= i+4; j++ {
				queries = append(queries, string(rs[i:j]), strings.ToUpper(string(rs[i:j])))
			}
		}
	}
	defer func(n int) { searchMax = n }(searchMax)
	for _, max := range []int{4096, 3} {
		searchMax = max
		for _, q := range queries {
			for _, addrs := range [][]string{nil, {chatA}, {chatA, chatB}} {
				for _, from := range []Cursor{{}, {T: (base.Unix() + 30) * 1000, ID: "M200"}} {
					for _, limit := range []int{5, 1000} {
						want, err := naiveSearch(ctx, db.Read(), q, addrs, from, limit)
						if err != nil {
							t.Fatal(err)
						}
						ms, err := r.Search(ctx, q, addrs, from, limit)
						if err != nil {
							t.Fatalf("search %q: %v", q, err)
						}
						var got []string
						for _, m := range ms {
							got = append(got, m.Chat+"/"+m.ID)
						}
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("searchMax %d, %q in %v from %v limit %d:\n got %v\nwant %v", max, q, addrs, from, limit, got, want)
						}
					}
				}
			}
		}
	}
}

// TestTextIndexFollowsEveryChange: inserts, text changes, deletes and a
// deleted rowid taken again, in batches, and the index caught up after each
// holds exactly what the rows say.
func TestTextIndexFollowsEveryChange(t *testing.T) {
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range messagesDomain.Schema {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	rnd := rand.New(rand.NewSource(2))
	texts := []string{"", "", "radhe", "hello there", "café", "50% off", "x"}
	next := 0
	for batch := range 300 {
		tx, err := db.Begin()
		if err != nil {
			t.Fatal(err)
		}
		for range 1 + rnd.Intn(6) {
			var err error
			switch rnd.Intn(5) {
			case 0, 1:
				next++
				_, err = tx.Exec(`INSERT INTO msg (chat, id, sender, from_me, t, kind, text, src, hash, seq, ord) VALUES ('c', ?, 's', 0, 1, 'text', ?, 0, x'', 0, 0)`,
					fmt.Sprint("m", next), texts[rnd.Intn(len(texts))])
			case 2:
				_, err = tx.Exec(`UPDATE msg SET text = ? WHERE rowid = (SELECT rowid FROM msg ORDER BY random() LIMIT 1)`, texts[rnd.Intn(len(texts))])
			case 3:
				// the newest row, so the next insert takes its rowid again
				_, err = tx.Exec(`DELETE FROM msg WHERE rowid = (SELECT MAX(rowid) FROM msg)`)
			case 4:
				_, err = tx.Exec(`DELETE FROM msg WHERE rowid = (SELECT rowid FROM msg ORDER BY random() LIMIT 1)`)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, q := range indexTextSQL {
			if _, err := tx.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO msg_text (msg_text, rank) VALUES ('integrity-check', 1)`); err != nil {
			t.Fatalf("batch %d: index out of step: %v", batch, err)
		}
	}
}
