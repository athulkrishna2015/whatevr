package core

import "encoding/json"

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
	// a send this daemon queued, cancelled or tried
	KindOutbox = "outbox"

	// what this daemon did on its own: files it fetched or made, settings
	KindLocal  = "local"
	KindAvatar = "avatar"
	KindPrefs  = "prefs"
	// a chat the user favorited on this device
	KindFavorite = "favorite"
	// a text the daemon sends later, see ScheduleHead
	KindSchedule = "schedule"
	// user-made chat lists, see FolderHead
	KindFolder = "folder"
	// what this daemon fetched or did for the sticker picker
	KindSticker = "sticker"
	// the id this daemon gave an address the first time it showed it. read
	// from the log directly, never folded
	KindPersonID = "person_id"
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
	// Sent says this device sent it and the server took it; the body is the
	// message as it went out
	Sent bool `json:"sent,omitempty"`
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
	// State is how a sync of the domain ended: SyncComplete or SyncError,
	// "" for a batch applied on the way
	State string `json:"state,omitempty"`
	Error string `json:"error,omitempty"`
	// Recovery is a collection rebuilt after it stopped decoding
	Recovery bool `json:"recovery,omitempty"`
	// Step is how far recovering an app state collection got, whatsmeow's
	// AppStateRecoveryStep. nil is an input that says nothing about it
	Step *int `json:"step,omitempty"`
}

type PersonIDHead struct {
	Addr string `json:"a"`
	ID   string `json:"id"`
}

const (
	SyncComplete = "complete"
	SyncError    = "error"
)

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

type LIDMappingHead struct {
	LID string `json:"lid"`
	PN  string `json:"pn"`
	// Self says the pair is this account's own
	Self bool `json:"self,omitempty"`
}

// HistoryConversationHead is one conversation out of a history blob. body:
// the waHistorySync.Conversation.
type HistoryConversationHead struct {
	// Notification is the id of the history_notification it was downloaded for
	Notification string `json:"notification"`
	SyncType     string `json:"sync_type"`
	ChunkOrder   uint32 `json:"chunk_order,omitempty"`
	ID           string `json:"id"`
	// Offset is the index of the body's first message in the conversation. a
	// long one comes in pieces; only the one at 0 has the rest of it.
	Offset int `json:"offset,omitempty"`
}

// HistoryExtraHead closes a downloaded history blob: everything in it but the
// conversations is the body, a waHistorySync.HistorySync. Error says the blob
// could not be had and never will be; there is no body then.
type HistoryExtraHead struct {
	Notification  string `json:"notification"`
	SyncType      string `json:"sync_type"`
	ChunkOrder    uint32 `json:"chunk_order,omitempty"`
	Progress      uint32 `json:"progress,omitempty"`
	Conversations int    `json:"conversations,omitempty"`
	Error         string `json:"error,omitempty"`
}

// GroupInfoHead is a group as the server described it. Full is the whole
// group from a fetch or a join; otherwise it is one change and only the set
// fields moved. Error is a fetch the server refused for good ("not
// participating", "does not exist").
type GroupInfoHead struct {
	JID   string `json:"jid"`
	Full  bool   `json:"full,omitempty"`
	T     int64  `json:"t"`
	By    string `json:"by,omitempty"`
	ByPN  string `json:"by_pn,omitempty"`
	Error string `json:"error,omitempty"`

	Name      *string `json:"name,omitempty"`
	NameT     int64   `json:"name_t,omitempty"`
	Topic     *string `json:"topic,omitempty"`
	TopicT    int64   `json:"topic_t,omitempty"`
	TopicDel  bool    `json:"topic_deleted,omitempty"`
	Announce  *bool   `json:"announce,omitempty"`
	Locked    *bool   `json:"locked,omitempty"`
	Ephemeral *uint32 `json:"ephemeral,omitempty"`
	Parent    bool    `json:"parent,omitempty"`
	LinkedTo  string  `json:"linked_to,omitempty"`
	Deleted   bool    `json:"deleted,omitempty"`
	Created   int64   `json:"created,omitempty"`
	Owner     string  `json:"owner,omitempty"`
	Addressed string  `json:"addressing,omitempty"`

	Participants []GroupParticipant `json:"participants,omitempty"`
	Join         []string           `json:"join,omitempty"`
	Leave        []string           `json:"leave,omitempty"`
	Promote      []string           `json:"promote,omitempty"`
	Demote       []string           `json:"demote,omitempty"`
	JoinReason   string             `json:"join_reason,omitempty"`

	// what a notification says beyond the group's own fields
	Approval     *bool  `json:"approval,omitempty"`
	InviteLink   bool   `json:"invite_link,omitempty"`
	DeleteReason string `json:"delete_reason,omitempty"`
	// LinkChange is a community link made ("link") or undone ("unlink"),
	// LinkType whatsmeow's parent or sub, LinkName the other group's name
	LinkChange string `json:"link_change,omitempty"`
	LinkType   string `json:"link_type,omitempty"`
	LinkName   string `json:"link_name,omitempty"`
}

type GroupParticipant struct {
	JID   string `json:"jid"`
	PN    string `json:"pn,omitempty"`
	LID   string `json:"lid,omitempty"`
	Admin bool   `json:"admin,omitempty"`
	Super bool   `json:"super,omitempty"`
}

type CallHead struct {
	ID    string `json:"id"`
	From  string `json:"from"`
	Alt   string `json:"alt,omitempty"`
	T     int64  `json:"t"`
	Event string `json:"event"`
	Video bool   `json:"video,omitempty"`
	Group string `json:"group,omitempty"`
	// Reason is why it ended, on a terminate
	Reason string `json:"reason,omitempty"`
}

type NewsletterHead struct {
	JID   string `json:"jid"`
	Event string `json:"event"`
	Role  string `json:"role,omitempty"`
	Mute  string `json:"mute,omitempty"`
	Name  string `json:"name,omitempty"`
}

// OutboxHead is one step of a send this daemon queued: Op is queue, cancel
// or attempt. a queue's body is the waE2E.Message as it will go, File the
// local media to upload into it first. an attempt that failed carries the
// error, Final when it will not be tried again. times are Input.At.
type OutboxHead struct {
	Op    string `json:"op"`
	Chat  string `json:"chat"`
	ID    string `json:"id"`
	File  string `json:"file,omitempty"`
	Error string `json:"error,omitempty"`
	Final bool   `json:"final,omitempty"`
	// Key is the frontend's key for a queued send, Params a digest of the
	// rest of what it asked
	Key    string `json:"key,omitempty"`
	Params string `json:"params,omitempty"`
}

const (
	OutboxQueue   = "queue"
	OutboxCancel  = "cancel"
	OutboxAttempt = "attempt"
)

// LocalHead is one thing this daemon learned about a message on its own: its
// media, or what the group invite in it points at. Op says which; a newer one
// of the same op replaces the older.
type LocalHead struct {
	Chat string `json:"chat"`
	ID   string `json:"id"`
	Op   string `json:"op"`
	// Path is the local file for file, poster, map and preview; "" is gone
	Path string `json:"path,omitempty"`
	W    int32  `json:"w,omitempty"`
	H    int32  `json:"h,omitempty"`
	// Error is why a download failed; "" on an error op clears it
	Error    string `json:"error,omitempty"`
	Waveform []byte `json:"waveform,omitempty"`
	// DirectPath is where the phone re-uploaded media the server had lost
	DirectPath string `json:"direct_path,omitempty"`
	// Invite is the group an invite resolved to, Error why it did not
	Invite json.RawMessage `json:"invite,omitempty"`
	N      int             `json:"n,omitempty"`
}

const (
	MediaFile     = "file"
	MediaError    = "error"
	MediaPlayed   = "played"
	MediaPoster   = "poster"
	MediaWaveform = "waveform"
	MediaMap      = "map"
	MediaPreview  = "preview"
	MediaDirect   = "direct"
	LocalInvite   = "invite"
	// the phone was asked to resend a message that did not decrypt; N counts
	LocalAsked = "asked"
)

// AvatarHead is one picture fetch and how it went.
type AvatarHead struct {
	JID       string `json:"jid"`
	PictureID string `json:"picture_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

const (
	AvatarOK     = "ok"
	AvatarNone   = "none"
	AvatarHidden = "hidden"
	AvatarError  = "error"
)

// StickerHead is one thing this daemon fetched or did for the sticker picker.
// Op says which; a newer one of the same op and key replaces the older.
type StickerHead struct {
	Op string `json:"op"`
	// Key is the pack id for pack and installed, the tray image id for tray,
	// the sticker's plaintext sha256 in hex for file and upload; "" for index
	Key string `json:"key,omitempty"`
	// Enc is the encrypted sha256 in hex a file was fetched by
	Enc      string `json:"enc,omitempty"`
	Path     string `json:"path,omitempty"`
	Archive  string `json:"archive,omitempty"`
	Animated bool   `json:"animated,omitempty"`
	On       bool   `json:"on,omitempty"`
}

const (
	// the body is the store index as fetched, a json list of packs
	StickerIndex = "index"
	// the body is the pack as fetched, json with its stickers
	StickerPack      = "pack"
	StickerInstalled = "installed"
	StickerTray      = "tray"
	StickerFile      = "file"
	// the body is the StickerMessage our own upload made, none once it is
	// no good
	StickerUpload = "upload"
)

// PrefsHead is every preference after a change.
type PrefsHead struct {
	Prefs json.RawMessage `json:"prefs"`
}

// FavoriteHead is one chat's favorite flag after a change.
type FavoriteHead struct {
	Chat string `json:"chat"`
	On   bool   `json:"on"`
}

// ScheduleHead is one scheduled-message change. Op is add, sent or cancel;
// add carries the chat, text and send_at, sent and cancel name the schedule
// by the input seq the add was logged under.
type ScheduleHead struct {
	Op     string `json:"op"`
	Chat   string `json:"chat,omitempty"`
	Text   string `json:"text,omitempty"`
	SendAt int64  `json:"send_at,omitempty"`
	ID     int64  `json:"id,omitempty"`
}

// FolderHead is one chat-folder change. Op is create, rename, delete, set
// or unset. A create's input seq is the folder id; set and unset name the
// chat by key and the folder by id.
type FolderHead struct {
	Op     string `json:"op"`
	ID     int64  `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Chat   string `json:"chat,omitempty"`
	Folder int64  `json:"folder,omitempty"`
}
