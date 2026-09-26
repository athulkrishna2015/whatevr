package proto

// The row shapes whattui renders. They mirror whatevrd's own structs, and
// every one of them ignores fields it does not model: protocol version 1
// allows additions forever, and a frontend that choked on an unknown field
// would break on the next daemon release.

// ChatRow is one row of the chats view. Mirrors whatevrd's chatItem.
type ChatRow struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	IsGroup              bool   `json:"is_group"`
	Preview              string `json:"preview"`
	LastMessageTime      int64  `json:"last_message_time"`
	LastMessageDirection string `json:"last_message_direction"`
	LastMessageStatus    string `json:"last_message_status"`
	Unread               int32  `json:"unread"`
	Pinned               bool   `json:"pinned"`
	Archived             bool   `json:"archived"`
	Muted                bool   `json:"muted"`
	MuteEndTimestamp     int64  `json:"mute_end_timestamp"`
	HistoryExhausted     bool   `json:"history_exhausted"`
	AvatarPath           string `json:"avatar_path"`
}

// Sender is who wrote a message.
type Sender struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	AvatarPath string `json:"avatar_path"`
}

// Media is what a media-bearing message carries. Path is empty until the file
// has been downloaded.
type Media struct {
	Mime          string `json:"mime"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ThumbnailPath string `json:"thumbnail_path"`
	Path          string `json:"path"`
	Downloading   bool   `json:"downloading"`
	DownloadError string `json:"download_error"`
	SizeBytes     int64  `json:"size_bytes"`
	DurationSecs  int    `json:"duration_secs"`
	Filename      string `json:"filename"`
	PageCount     int    `json:"page_count"`
	// Waveform is 64 buckets of 0-100. The one piece of media data that rides
	// the socket rather than a file, because the bubble needs it before any
	// download.
	Waveform []int `json:"waveform"`
	Played   bool  `json:"played"`
}

// Reaction is one emoji and who put it there.
type Reaction struct {
	Emoji       string   `json:"emoji"`
	Count       int      `json:"count"`
	Senders     []string `json:"senders"`
	SelfReacted bool     `json:"self_reacted"`
}

// ReplyQuote is the message a message is answering.
type ReplyQuote struct {
	MessageID string `json:"message_id"`
	Sender    Sender `json:"sender"`
	Fallback  string `json:"fallback"`
	Text      string `json:"text"`
}

// System is something a chat did to itself. Text is a finished sentence, which
// is what rule 5 needs: any frontend can print it and be correct.
type System struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	AboutSelf bool   `json:"about_self"`
}

// CallLog is a call that happened. Not a message anybody wrote, which is why
// it draws centred.
type CallLog struct {
	Video        bool   `json:"video"`
	Outcome      string `json:"outcome"`
	DurationSecs int    `json:"duration_secs"`
	Group        bool   `json:"group"`
}

// MessageRow is one row of a messages view. Mirrors whatevrd's messageItem.
//
// Only the kinds whattui draws itself are modelled here. Everything else
// renders Fallback, which the daemon guarantees for every kind, so a kind this
// struct has never heard of still shows up as a readable line.
type MessageRow struct {
	ID        string      `json:"id"`
	ChatID    string      `json:"chat_id"`
	Kind      string      `json:"kind"`
	Fallback  string      `json:"fallback"`
	Text      string      `json:"text"`
	Sender    Sender      `json:"sender"`
	Timestamp int64       `json:"timestamp"`
	Direction string      `json:"direction"`
	Status    string      `json:"status"`
	ReplyTo   *ReplyQuote `json:"reply_to"`
	Reactions []Reaction  `json:"reactions"`
	Edited    bool        `json:"edited"`
	Revoked   bool        `json:"revoked"`
	Starred   bool        `json:"starred"`
	Forwarded bool        `json:"forwarded"`
	Media     *Media      `json:"media"`
	System    *System     `json:"system"`
	CallLog   *CallLog    `json:"call_log"`
}

// Outgoing reports whether we sent this.
func (m MessageRow) Outgoing() bool { return m.Direction == "outgoing" }

// Centred reports whether the row draws in the middle of the transcript with
// no bubble and no author, which is what a message nobody wrote looks like.
func (m MessageRow) Centred() bool { return m.Kind == "system" || m.Kind == "call_log" }

// Body is what to draw as the message's words. A revoked message has none, and
// a kind whattui does not implement falls back to the daemon's one-liner.
func (m MessageRow) Body() string {
	switch {
	case m.Revoked:
		return "This message was deleted"
	case m.Centred() && m.System != nil:
		return m.System.Text
	case m.Kind == "text" && m.Text != "":
		return m.Text
	case m.Text != "":
		// A caption rides the item-level text for every kind, so a photo with
		// words shows the words under whatever the fallback said it was.
		return m.Fallback + "\n" + m.Text
	default:
		return m.Fallback
	}
}

// Connection is the connection view's object: where the daemon and WhatsApp
// are, which is not the same thing as whether our socket is up.
type Connection struct {
	State           string `json:"state"`
	RetryInSecs     int    `json:"retry_in_secs"`
	PendingOutgoing int    `json:"pending_outgoing"`
}

// Self is our own profile.
type Self struct {
	JID        string `json:"jid"`
	Phone      string `json:"phone"`
	PushName   string `json:"push_name"`
	About      string `json:"about"`
	AvatarPath string `json:"avatar_path"`
}
