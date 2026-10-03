package model

import (
	"context"
	"encoding/json"
	"testing"

	"whatevrd/internal/core"
)

func local(chat, id string, h core.LocalHead, sec int) core.Input {
	h.Chat, h.ID = chat, id
	return in(core.KindLocal, h, nil, at(sec))
}

func TestTheNewestLocalFactWins(t *testing.T) {
	ins := []core.Input{
		msgIn("A1", ashaL, ashaL, ashaPN, false, 10, text("pic")),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaError, Error: "timeout"}, 20),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaFile, Path: "/m/a1", W: 4, H: 3}, 21),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaPlayed}, 22),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaWaveform, Waveform: []byte{1, 2}}, 23),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaPoster, Path: "/m/old.poster"}, 24),
		local(ashaL, "A1", core.LocalHead{Op: core.MediaPoster, Path: "/m/a1.poster"}, 25),
		msgIn("A2", ashaL, ashaL, ashaPN, false, 11, text("pic")),
		local(ashaL, "A2", core.LocalHead{Op: core.MediaFile, Path: "/m/a2"}, 20),
		local(ashaL, "A2", core.LocalHead{Op: core.MediaError, Error: "gone"}, 21),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		l := one(t, r, []string{ashaL}, "A1").Facts.Local
		if l.File != "/m/a1" || l.W != 4 || l.H != 3 || !l.Played || string(l.Waveform) != "\x01\x02" || l.Poster != "/m/a1.poster" {
			t.Fatalf("A1 %+v", l)
		}
		if e := l.DownloadError(); e != "" {
			t.Fatalf("a file after the error still shows %q", e)
		}
		if e := one(t, r, []string{ashaL}, "A2").Facts.Local.DownloadError(); e != "gone" {
			t.Fatalf("an error after the file shows %q", e)
		}
	})
}

// a fact about a message that is gone, before or after it went, is dropped
func TestLocalFactsGoWithTheirMessage(t *testing.T) {
	ins := []core.Input{
		msgIn("D1", ashaL, ashaL, ashaPN, false, 10, text("pic")),
		local(ashaL, "D1", core.LocalHead{Op: core.MediaFile, Path: "/m/d1"}, 20),
		msgIn("D2", ashaL, ashaL, ashaPN, false, 11, text("pic")),
		local(ashaL, "D2", core.LocalHead{Op: core.MediaFile, Path: "/m/d2"}, 21),
		msgIn("X", ashaL, ashaL, ashaPN, false, 30, revokeOf(target(ashaL, "D1", ""))),
		appState("regular_high", 8, "set", []string{"deleteMessageForMe", ashaL, "D2", "0", "0"}, nil, 31),
		local(ashaL, "D1", core.LocalHead{Op: core.MediaPoster, Path: "/m/d1.poster"}, 40),
		local(ashaL, "D2", core.LocalHead{Op: core.MediaPoster, Path: "/m/d2.poster"}, 41),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		expect(t, db, nil, `SELECT chat, id, op FROM msg_local`)
	})
}

func TestAvatarsCountTheFailuresSinceTheLastAnswer(t *testing.T) {
	av := func(j, status, pic string, sec int) core.Input {
		return in(core.KindAvatar, core.AvatarHead{JID: j, Status: status, PictureID: pic, Path: "/a/" + pic, Error: map[bool]string{true: "boom"}[status == core.AvatarError]}, nil, at(sec))
	}
	ins := []core.Input{
		av(ashaL, core.AvatarError, "", 1),
		av(ashaL, core.AvatarOK, "p1", 2),
		av(ashaL, core.AvatarError, "", 3),
		av(ashaL, core.AvatarError, "", 4),
		av(boL, core.AvatarOK, "p1", 1),
		av(boL, core.AvatarNone, "", 5),
		av(mePN, core.AvatarError, "", 7),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		got, err := r.Avatars(context.Background(), []string{ashaL, boL, mePN, boPN})
		if err != nil {
			t.Fatal(err)
		}
		if a := got[ashaL]; a.Status != core.AvatarOK || a.PictureID != "p1" || a.Fails != 2 || a.LastTry != at(4).UnixMilli() || a.Error != "boom" {
			t.Errorf("asha %+v", a)
		}
		if a := got[boL]; a.Status != core.AvatarNone || a.Path != "/a/" || a.Fails != 0 {
			t.Errorf("bo %+v", a)
		}
		if a := got[mePN]; a.Status != "" || a.Fails != 1 {
			t.Errorf("me %+v", a)
		}
		if _, ok := got[boPN]; ok {
			t.Error("an address never fetched has an avatar")
		}
		expect(t, db, []string{ashaL + "|3", boL + "|1", mePN + "|1"}, `SELECT jid, COUNT(*) FROM avatar_try GROUP BY jid ORDER BY jid`)
	})
}

func TestPrefsKeepTheNewest(t *testing.T) {
	ins := []core.Input{
		in(core.KindPrefs, core.PrefsHead{Prefs: json.RawMessage(`{"a":1}`)}, nil, at(1)),
		in(core.KindPrefs, core.PrefsHead{Prefs: json.RawMessage(`{"a":2}`)}, nil, at(2)),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		p, err := r.Prefs(context.Background())
		if err != nil || string(p) != `{"a":2}` {
			t.Fatalf("%s %v", p, err)
		}
	})
	r := NewReader(openModel(t).Read())
	if p, err := r.Prefs(context.Background()); p != nil || err != nil {
		t.Fatalf("no prefs read %s %v", p, err)
	}
}
