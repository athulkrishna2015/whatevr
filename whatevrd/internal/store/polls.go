package store

// PollOption is one choice on a poll.
type PollOption struct {
	Index int
	Name  string
	// SHA256 is what a decrypted vote names. Votes are matched to options by
	// hash and never by text, which is also why an option can be added later
	// without invalidating votes already cast.
	SHA256 []byte
	// Voters are the participants who chose this option, in the order their
	// votes landed.
	Voters []PollVoter
}

// PollVoter is one participant's choice.
type PollVoter struct {
	JID             string
	DisplayName     string
	AvatarLocalPath string
	VotedAtUnix     int64
	FromMe          bool
}

// PollState is everything about a poll that is not in its payload.
type PollState struct {
	Options []PollOption
	// TotalVoters counts distinct participants, not selections: a poll that
	// allows several answers would otherwise report more votes than voters.
	TotalVoters int
}
