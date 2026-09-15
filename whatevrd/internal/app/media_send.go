package app

// MediaSendOptions steers media sends. Kind is "", "auto", "image", "video",
// "audio", "voice" or "document"; empty/auto classifies from the file's MIME
// type. ViewOnce sends photo/video/audio media view-once. Filename overrides
// the document display name. It lives here (rather than in wa) so the
// protocol layer can name it without importing the WhatsApp client.
type MediaSendOptions struct {
	Kind     string
	ViewOnce bool
	Filename string
}

// CommunityGroup is one sub-group linked under a community: its JID plus the
// best-effort display name. Shared between wa and protocol for the same
// import-cycle reason as MediaSendOptions.
type CommunityGroup struct {
	ID   string
	Name string
}
