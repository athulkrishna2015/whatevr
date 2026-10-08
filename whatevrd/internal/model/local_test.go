package model

import (
	"context"
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

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

func TestFavoriteFlagsChatsAndFiltersThem(t *testing.T) {
	fav := func(chat string, on bool, sec int) core.Input {
		return in(core.KindFavorite, core.FavoriteHead{Chat: chat, On: on}, nil, at(sec))
	}
	ins := []core.Input{
		msgIn("M1", ashaL, ashaL, ashaPN, false, 10, text("hi")),
		msgIn("M2", grp, boL, boPN, false, 11, text("hi")),
		fav(ashaL, true, 20),
	}
	both(t, ins, func(t *testing.T, db *core.DB, r *Reader) {
		ctx := context.Background()
		w, err := r.World(ctx)
		if err != nil {
			t.Fatal(err)
		}
		all, err := r.ChatsIn(ctx, w, ChatFilter{Any: true})
		if err != nil {
			t.Fatal(err)
		}
		byKey := map[string]Chat{}
		for _, c := range all {
			byKey[c.Key] = c
		}
		if !byKey[ashaL].Favorite {
			t.Fatalf("ashaL not favorited: %+v", byKey[ashaL])
		}
		if byKey[grp].Favorite {
			t.Fatalf("group favorited: %+v", byKey[grp])
		}
		only, err := r.ChatsIn(ctx, w, ChatFilter{Any: true, Kind: "favorite"})
		if err != nil {
			t.Fatal(err)
		}
		if len(only) != 1 || only[0].Key != ashaL || !only[0].Favorite {
			t.Fatalf("favorite filter: %+v", only)
		}
	})
}

func TestScheduledTextsListDueAndCancel(t *testing.T) {
	sched := func(op, chat, text string, sendAt, id int64, sec int) core.Input {
		return in(core.KindSchedule, core.ScheduleHead{Op: op, Chat: chat, Text: text, SendAt: sendAt, ID: id}, nil, at(sec))
	}
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		sched("add", "a", "one", 100, 0, 1),
		sched("add", "b", "two", 200, 0, 2),
	})
	r := NewReader(db.Read())
	all, err := r.Scheduled(ctx, "", 0)
	if err != nil || len(all) != 2 || all[0].Text != "one" || all[1].Text != "two" {
		t.Fatalf("listed %+v %v", all, err)
	}
	due, err := r.DueScheduled(ctx, 150, 0)
	if err != nil || len(due) != 1 || due[0].Text != "one" {
		t.Fatalf("due %+v %v", due, err)
	}
	only, err := r.Scheduled(ctx, "b", 0)
	if err != nil || len(only) != 1 || only[0].Text != "two" {
		t.Fatalf("chat filter %+v %v", only, err)
	}
	feed(t, db, []core.Input{sched("cancel", "", "", 0, all[0].ID, 3)})
	if rest, err := r.Scheduled(ctx, "", 0); err != nil || len(rest) != 1 || rest[0].Text != "two" {
		t.Fatalf("after cancel %+v %v", rest, err)
	}
}

func TestChatFoldersCreateRenameAssignAndDelete(t *testing.T) {
	folder := func(op string, id int64, name, chat string, f int64, sec int) core.Input {
		return in(core.KindFolder, core.FolderHead{Op: op, ID: id, Name: name, Chat: chat, Folder: f}, nil, at(sec))
	}
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		msgIn("M1", ashaL, ashaL, ashaPN, false, 10, text("hi")),
		folder("create", 0, "Work", "", 0, 1),
		folder("create", 0, "Friends", "", 0, 2),
	})
	r := NewReader(db.Read())
	all, err := r.Folders(ctx)
	if err != nil || len(all) != 2 || all[0].Name != "Friends" || all[1].Name != "Work" {
		t.Fatalf("listed %+v %v", all, err)
	}
	work := all[1].ID
	feed(t, db, []core.Input{
		folder("rename", all[0].ID, "Family", "", 0, 3),
		folder("set", 0, "", ashaL, work, 4),
	})
	renamed, err := r.Folders(ctx)
	if err != nil || len(renamed) != 2 || renamed[0].Name != "Family" {
		t.Fatalf("renamed %+v %v", renamed, err)
	}
	inFolder, err := r.ChatsIn(ctx, mustWorld(t, r), ChatFilter{Any: true, Folder: work})
	if err != nil || len(inFolder) != 1 {
		t.Fatalf("folder filter %+v %v", inFolder, err)
	}
	outFolder, err := r.ChatsIn(ctx, mustWorld(t, r), ChatFilter{Any: true, Folder: work + 1000})
	if err != nil || len(outFolder) != 0 {
		t.Fatalf("other folder %+v %v", outFolder, err)
	}
	feed(t, db, []core.Input{
		folder("unset", 0, "", ashaL, 0, 5),
		folder("delete", work, "", "", 0, 6),
	})
	if rest, err := r.Folders(ctx); err != nil || len(rest) != 1 || rest[0].Name != "Family" {
		t.Fatalf("after delete %+v %v", rest, err)
	}
}

func mustWorld(t *testing.T, r *Reader) *World {
	t.Helper()
	w, err := r.World(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestEditHistoryKeepsEveryVersion(t *testing.T) {
	edit := func(text string, sec int) core.Input {
		return msgIn("E1", ashaPN, mePN, "", true, sec, &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{RemoteJID: proto.String(ashaPN), ID: proto.String("M2"), FromMe: proto.Bool(true)},
			EditedMessage: &waE2E.Message{Conversation: proto.String(text)},
			TimestampMS:   proto.Int64(at(sec).UnixMilli()),
		}})
	}
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		msgIn("M2", ashaPN, mePN, "", true, 11, text("v0")),
		edit("v1", 12),
		edit("v2", 13),
	})
	r := NewReader(db.Read())
	edits, err := r.Edits(ctx, []string{ashaPN}, "M2")
	if err != nil || len(edits) != 2 || edits[0].Text != "v1" || edits[1].Text != "v2" {
		t.Fatalf("history %+v %v", edits, err)
	}
}

func TestStatusMuteAndViewedFlags(t *testing.T) {
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		in(core.KindStatusMute, core.StatusMuteHead{Sender: "b@s", Muted: true}, nil, at(1)),
		in(core.KindLocal, core.LocalHead{Chat: "status@broadcast", ID: "s1", Op: StatusViewOp}, nil, at(2)),
	})
	r := NewReader(db.Read())
	muted, err := r.StatusMuted(ctx)
	if err != nil || len(muted) != 1 || muted[0] != "b@s" {
		t.Fatalf("muted %+v %v", muted, err)
	}
	seen, err := r.StatusViewed(ctx, "status@broadcast", []string{"s1", "s2"})
	if err != nil || !seen["s1"] || seen["s2"] {
		t.Fatalf("viewed %+v %v", seen, err)
	}
	feed(t, db, []core.Input{
		in(core.KindStatusMute, core.StatusMuteHead{Sender: "b@s", Muted: false}, nil, at(3)),
	})
	if rest, err := r.StatusMuted(ctx); err != nil || len(rest) != 0 {
		t.Fatalf("after unmute %+v %v", rest, err)
	}
}

func TestRingingListsOffersWithoutAnEnd(t *testing.T) {
	call := func(id, event string, sec int) core.Input {
		return in(core.KindCall, core.CallHead{ID: id, From: ashaPN, T: at(sec).UnixMilli(), Event: event}, nil, at(sec))
	}
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		call("c1", "offer", 1),
		call("c2", "offer", 2),
		call("c2", "terminate", 3),
	})
	r := NewReader(db.Read())
	ringing, err := r.Ringing(ctx)
	if err != nil || len(ringing) != 1 || ringing[0].ID != "c1" {
		t.Fatalf("ringing %+v %v", ringing, err)
	}
}

func TestChannelsListDirectoryAndLeave(t *testing.T) {
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		in(core.KindNewsletter, core.NewsletterHead{JID: "1@newsletter", Event: "join", Name: "News"}, nil, at(1)),
		in(core.KindNewsletter, core.NewsletterHead{JID: "1@newsletter", Event: "directory", Name: "News",
			Description: "Daily", Followers: 10, Verified: true}, nil, at(2)),
		in(core.KindNewsletter, core.NewsletterHead{JID: "2@newsletter", Event: "join", Name: "Gone"}, nil, at(3)),
		in(core.KindNewsletter, core.NewsletterHead{JID: "2@newsletter", Event: "leave"}, nil, at(4)),
	})
	r := NewReader(db.Read())
	channels, err := r.Channels(ctx)
	if err != nil || len(channels) != 1 {
		t.Fatalf("channels %+v %v", channels, err)
	}
	if channels[0].Description != "Daily" || channels[0].Followers != 10 || !channels[0].Verified {
		t.Fatalf("directory %+v", channels[0])
	}
}

func TestNewsletterAddressesAreNoChats(t *testing.T) {
	db := openModel(t)
	ctx := context.Background()
	feed(t, db, []core.Input{
		msgIn("N1", "1@newsletter", "1@newsletter", "", false, 10, text("broadcast")),
		msgIn("M1", ashaL, ashaL, ashaPN, false, 11, text("hi")),
	})
	r := NewReader(db.Read())
	w, err := r.World(ctx)
	if err != nil {
		t.Fatal(err)
	}
	chats, err := r.ChatsIn(ctx, w, ChatFilter{Any: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range chats {
		if c.Key == "1@newsletter" {
			t.Fatalf("newsletter listed as chat: %+v", c)
		}
	}
	if _, ok, err := r.ChatIn(ctx, w, "1@newsletter"); err != nil || ok {
		t.Fatalf("newsletter opens as chat: %v %v", ok, err)
	}
}
