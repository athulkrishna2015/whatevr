package core

// Input kinds, one per kind of fact. A head is the json in Input.Head; a body,
// where a kind has one, is whatsapp's own protobuf.
const (
	// body: the waE2E.Message, exactly as decrypted when Exact
	KindMessage = "message"
	KindReceipt = "receipt"
	// a message that could not be decrypted, waiting on a resend
	KindUndecryptable = "undecryptable"
	// one app state mutation. body: the waSyncAction.SyncActionValue
	KindAppState = "app_state"
	// a sync domain moved: an app state collection reached a version, a
	// history sync made progress
	KindSyncState = "sync_state"
	// body: the waE2E.HistorySyncNotification
	KindHistoryNotification = "history_notification"
	KindHistoryConversation = "history_conversation"
	KindHistoryExtra        = "history_extra"
	KindGroupInfo           = "group_info"
	KindPicture             = "picture"
	KindIdentityChange      = "identity_change"
	KindPushName            = "push_name"
	KindBusinessName        = "business_name"
	KindBlocklist           = "blocklist"
	KindPrivacy             = "privacy"
	KindCall                = "call"
	KindNewsletter          = "newsletter"
	KindLIDMapping          = "lid_mapping"
)

// Times in heads are whatsapp's, unix seconds; 0 when whatsapp gave none,
// and the fold falls back to Input.At. Jids are whatsapp's string form.

// Source is where a message or receipt came from.
type Source struct {
	Chat           string `json:"chat"`
	Sender         string `json:"sender"`
	SenderAlt      string `json:"sender_alt,omitempty"`
	RecipientAlt   string `json:"recipient_alt,omitempty"`
	FromMe         bool   `json:"from_me,omitempty"`
	Group          bool   `json:"group,omitempty"`
	Addressing     string `json:"addressing,omitempty"`
	BroadcastOwner string `json:"broadcast_owner,omitempty"`
}

type MessageHead struct {
	Source
	ID            string `json:"id"`
	ServerID      int    `json:"server_id,omitempty"`
	T             int64  `json:"t"`
	Type          string `json:"type,omitempty"`
	PushName      string `json:"push_name,omitempty"`
	Category      string `json:"category,omitempty"`
	Edit          string `json:"edit,omitempty"`
	MediaType     string `json:"media_type,omitempty"`
	Retry         int    `json:"retry,omitempty"`
	DeviceSentTo  string `json:"device_sent_to,omitempty"`
	ThreadID      string `json:"thread_id,omitempty"`
	ThreadSender  string `json:"thread_sender,omitempty"`
	VerifiedName  string `json:"verified_name,omitempty"`
	VerifiedLevel string `json:"verified_level,omitempty"`
	// Exact says the body is the plaintext as it was decrypted. otherwise it
	// is a deterministic re-encoding of what whatsmeow parsed.
	Exact bool `json:"exact,omitempty"`
}

type ReceiptHead struct {
	Source
	IDs           []string `json:"ids"`
	Type          string   `json:"type,omitempty"`
	T             int64    `json:"t"`
	MessageSender string   `json:"message_sender,omitempty"`
}

type UndecryptableHead struct {
	Source
	ID              string `json:"id"`
	T               int64  `json:"t"`
	Unavailable     bool   `json:"unavailable,omitempty"`
	UnavailableType string `json:"unavailable_type,omitempty"`
	FailMode        string `json:"fail_mode,omitempty"`
}

type AppStateHead struct {
	Collection string `json:"collection"`
	// Version is the collection version the mutation's patch moved it to.
	Version       uint64   `json:"version"`
	Index         []string `json:"index"`
	Op            string   `json:"op"`
	ActionVersion int32    `json:"action_version,omitempty"`
	// Snapshot says the mutation came in a snapshot: the whole collection at
	// Version, so an index missing from it is gone.
	Snapshot bool `json:"snapshot,omitempty"`
}

type SyncStateHead struct {
	// Domain is "app_state:<collection>" or "history:<sync type>".
	Domain   string `json:"domain"`
	Version  uint64 `json:"version,omitempty"`
	Snapshot bool   `json:"snapshot,omitempty"`
	Count    int    `json:"count,omitempty"`
}

type HistoryNotificationHead struct {
	ID         string `json:"id"`
	T          int64  `json:"t"`
	SyncType   string `json:"sync_type"`
	ChunkOrder uint32 `json:"chunk_order,omitempty"`
	Progress   uint32 `json:"progress,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	OriginalID string `json:"original_id,omitempty"`
}

type PushNameHead struct {
	JID    string `json:"jid"`
	JIDAlt string `json:"jid_alt,omitempty"`
	ID     string `json:"id,omitempty"`
	T      int64  `json:"t"`
	Old    string `json:"old,omitempty"`
	New    string `json:"new"`
}

type BusinessNameHead struct {
	JID string `json:"jid"`
	ID  string `json:"id,omitempty"`
	T   int64  `json:"t"`
	Old string `json:"old,omitempty"`
	New string `json:"new"`
}

type IdentityChangeHead struct {
	JID      string `json:"jid"`
	T        int64  `json:"t"`
	Implicit bool   `json:"implicit,omitempty"`
}

type PictureHead struct {
	JID       string `json:"jid"`
	Author    string `json:"author,omitempty"`
	T         int64  `json:"t"`
	Remove    bool   `json:"remove,omitempty"`
	PictureID string `json:"picture_id,omitempty"`
}

type BlocklistHead struct {
	// Action "modify" means the whole list has to be fetched again.
	Action    string            `json:"action,omitempty"`
	DHash     string            `json:"dhash,omitempty"`
	PrevDHash string            `json:"prev_dhash,omitempty"`
	Changes   []BlocklistChange `json:"changes,omitempty"`
}

type BlocklistChange struct {
	JID    string `json:"jid"`
	Action string `json:"action"`
}

type PrivacyHead struct {
	// Settings is every category's value after the change.
	Settings map[string]string `json:"settings"`
	Changed  []string          `json:"changed,omitempty"`
}
