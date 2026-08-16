package store

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"
)

func seedPoll(t *testing.T, db *DB, messageID, chatID string, optionNames ...string) [][]byte {
	t.Helper()
	ctx := context.Background()
	if _, err := db.SaveMediaMessage(ctx, MediaMessageInput{
		TextMessageInput: TextMessageInput{
			ID:          messageID,
			ChatID:      chatID,
			SenderID:    "ana@s.whatsapp.net",
			Timestamp:   time.Unix(1_700_000_000, 0),
			PayloadJSON: `{"poll":{"question":"dinner?"}}`,
		},
		MediaKind:      MediaKindPoll,
		PayloadSummary: "dinner?",
	}); err != nil {
		t.Fatalf("seed poll: %v", err)
	}

	options := make([]PollOption, 0, len(optionNames))
	hashes := make([][]byte, 0, len(optionNames))
	for i, name := range optionNames {
		sum := sha256.Sum256([]byte(name))
		options = append(options, PollOption{Index: i, Name: name, SHA256: sum[:]})
		hashes = append(hashes, sum[:])
	}
	if err := db.SavePollOptions(ctx, messageID, options); err != nil {
		t.Fatalf("save options: %v", err)
	}
	return hashes
}

func pollFor(t *testing.T, db *DB, chatID, messageID string) *PollState {
	t.Helper()
	messages, err := db.ListMessages(context.Background(), chatID, 50, "")
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	for _, m := range messages {
		if m.ID == messageID {
			return m.Poll
		}
	}
	t.Fatalf("poll %s not in the page", messageID)
	return nil
}

// A vote message carries a voter's entire current selection, not a delta.
// Accumulating instead would leave every option they ever touched selected.
func TestApplyPollVoteReplacesTheWholeSelection(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai", "Pizza", "Sushi")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("first vote: %v", err)
	}
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[2]}, 200); err != nil {
		t.Fatalf("changed vote: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if len(poll.Options[0].Voters) != 0 {
		t.Fatalf("the old choice survived: %+v", poll.Options[0].Voters)
	}
	if len(poll.Options[2].Voters) != 1 {
		t.Fatalf("the new choice did not land: %+v", poll.Options[2].Voters)
	}
	if poll.TotalVoters != 1 {
		t.Fatalf("TotalVoters = %d, want 1", poll.TotalVoters)
	}
}

// WhatsApp redelivers messages on reconnect, so an old vote coming round again
// is routine. Applying it would undo whatever the voter did since.
func TestStalePollVotesAreIgnored(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai", "Pizza")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[1]}, 200); err != nil {
		t.Fatalf("current vote: %v", err)
	}
	applied, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100)
	if err != nil {
		t.Fatalf("replayed vote: %v", err)
	}
	if applied {
		t.Error("a vote older than the one on record was applied")
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if len(poll.Options[0].Voters) != 0 {
		t.Errorf("the replayed choice landed: %+v", poll.Options[0].Voters)
	}
	if len(poll.Options[1].Voters) != 1 {
		t.Errorf("the current choice was lost: %+v", poll.Options[1].Voters)
	}
}

// The case that actually bit: withdrawing a vote leaves no vote rows, so a
// redelivered older vote has nothing to look stale against unless the store
// remembers when each voter last answered whether or not they chose anything.
func TestAWithdrawnVoteIsNotResurrectedByAReplay(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai", "Pizza")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", nil, 200); err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	// The server sends the original vote again on the next reconnect.
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("replay: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if poll.TotalVoters != 0 {
		t.Fatalf("a withdrawn vote came back: %+v", poll)
	}
}

// A poll allowing several answers counts people, not selections, or a chat of
// three would report nine votes.
func TestPollTotalVotersCountsPeopleNotSelections(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Mon", "Tue", "Wed")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0], hashes[1], hashes[2]}, 100); err != nil {
		t.Fatalf("ana: %v", err)
	}
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "bo@s.whatsapp.net", [][]byte{hashes[0]}, 200); err != nil {
		t.Fatalf("bo: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if poll.TotalVoters != 2 {
		t.Fatalf("TotalVoters = %d, want 2 people", poll.TotalVoters)
	}
	if len(poll.Options[0].Voters) != 2 {
		t.Fatalf("Mon has %d voters, want 2", len(poll.Options[0].Voters))
	}
}

// Emptying the selection is how a voter takes their answer back.
func TestApplyPollVoteWithNothingSelectedClearsTheVote(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Yes", "No")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", nil, 200); err != nil {
		t.Fatalf("unvote: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if poll.TotalVoters != 0 {
		t.Fatalf("TotalVoters = %d after taking the vote back, want 0", poll.TotalVoters)
	}
}

// Votes name their choices by hash. A vote for an option we do not have (an
// edited poll, a choice never announced) must be dropped rather than invent one.
func TestPollVotesForUnknownOptionsAreIgnored(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	seedPoll(t, db, "chat-1:poll", "chat-1", "Thai")

	stranger := sha256.Sum256([]byte("Something Else"))
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{stranger[:]}, 100); err != nil {
		t.Fatalf("stray vote: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if len(poll.Options) != 1 {
		t.Fatalf("%d options, want the one we announced", len(poll.Options))
	}
	if len(poll.Options[0].Voters) != 0 || poll.TotalVoters != 0 {
		t.Fatalf("a vote for an unknown option was counted: %+v", poll)
	}
}

// A vote can outrun its poll (history sync promises no ordering), so it is
// parked and replayed rather than lost.
func TestPendingPollVotesAreParkedAndTakenOnce(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	vote := PendingPollVote{
		ID:            "chat-1:poll\x1fana@s.whatsapp.net",
		ChatID:        "chat-1",
		PollMessageID: "chat-1:poll",
		VoterJID:      "ana@s.whatsapp.net",
		EncPayload:    []byte("payload"),
		EncIV:         []byte("iv"),
		SenderTSMS:    100,
	}
	if err := db.ParkPollVote(ctx, vote); err != nil {
		t.Fatalf("park: %v", err)
	}
	// Changing their mind before the poll arrives replaces the parked row
	// rather than piling up a second one.
	vote.EncPayload = []byte("newer")
	vote.SenderTSMS = 200
	if err := db.ParkPollVote(ctx, vote); err != nil {
		t.Fatalf("re-park: %v", err)
	}

	taken, err := db.TakePendingPollVotes(ctx, "chat-1:poll")
	if err != nil {
		t.Fatalf("take: %v", err)
	}
	if len(taken) != 1 || string(taken[0].EncPayload) != "newer" {
		t.Fatalf("taken = %+v, want one row holding the newer payload", taken)
	}

	again, err := db.TakePendingPollVotes(ctx, "chat-1:poll")
	if err != nil {
		t.Fatalf("second take: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("taking twice returned %d rows, want none", len(again))
	}
}

// An option added after the poll was created lands at the end and does not
// disturb votes already cast.
func TestAddPollOptionAppendsWithoutDisturbingVotes(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai", "Pizza")
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("vote: %v", err)
	}

	added := sha256.Sum256([]byte("Sushi"))
	if err := db.AddPollOption(ctx, "chat-1:poll", "Sushi", added[:]); err != nil {
		t.Fatalf("add option: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if len(poll.Options) != 3 || poll.Options[2].Name != "Sushi" {
		t.Fatalf("options = %+v", poll.Options)
	}
	if len(poll.Options[0].Voters) != 1 {
		t.Fatalf("the existing vote was disturbed: %+v", poll.Options[0])
	}
}

// Deleting a poll must take its options and votes with it.
func TestPollRowsCascadeWithTheirMessage(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai")
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("vote: %v", err)
	}

	if _, _, _, err := db.DeleteMessageForMe(ctx, "chat-1:poll"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	options, err := db.PollOptionHashes(ctx, "chat-1:poll")
	if err != nil {
		t.Fatalf("options after delete: %v", err)
	}
	if len(options) != 0 {
		t.Fatalf("options outlived their poll: %+v", options)
	}
}

// Our own vote is marked so a bubble can show its answered state without
// searching the voter list for itself.
func TestPollMarksOurOwnVote(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := db.SetSelfJID(ctx, "me@s.whatsapp.net"); err != nil {
		t.Fatalf("set self jid: %v", err)
	}
	hashes := seedPoll(t, db, "chat-1:poll", "chat-1", "Thai", "Pizza")

	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "me@s.whatsapp.net", [][]byte{hashes[0]}, 100); err != nil {
		t.Fatalf("our vote: %v", err)
	}
	if _, err := db.ApplyPollVote(ctx, "chat-1:poll", "ana@s.whatsapp.net", [][]byte{hashes[1]}, 200); err != nil {
		t.Fatalf("their vote: %v", err)
	}

	poll := pollFor(t, db, "chat-1", "chat-1:poll")
	if !poll.Options[0].Voters[0].FromMe {
		t.Error("our own vote was not marked")
	}
	if poll.Options[1].Voters[0].FromMe {
		t.Error("somebody else's vote was marked as ours")
	}
}
