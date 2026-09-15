package store

import (
	"context"
	"path/filepath"
	"testing"
)

// TestPollVotesReplaceBallot locks in that a re-vote replaces the voter's old
// ballot and tallies count voters per option.
func TestPollVotesReplaceBallot(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	tally, err := db.RecordPollVotes(ctx, "poll:1", "a@s.whatsapp.net", []string{"yes", "no"})
	if err != nil {
		t.Fatalf("record votes: %v", err)
	}
	if tally["yes"] != 1 || tally["no"] != 1 {
		t.Fatalf("tally = %v, want yes:1 no:1", tally)
	}
	tally, err = db.RecordPollVotes(ctx, "poll:1", "a@s.whatsapp.net", []string{"no"})
	if err != nil {
		t.Fatalf("re-vote: %v", err)
	}
	if _, ok := tally["yes"]; ok || tally["no"] != 1 {
		t.Fatalf("tally after re-vote = %v, want only no:1", tally)
	}
	voters, err := db.PollVoters(ctx, "poll:1", "no")
	if err != nil || len(voters) != 1 || voters[0] != "a@s.whatsapp.net" {
		t.Fatalf("voters = %v, %v; want [a@s.whatsapp.net]", voters, err)
	}
}
