package store

// Reaction is a single reactor's emoji on a message. One reaction per sender
// per message; the latest one wins (an empty emoji removes it).
type Reaction struct {
	Emoji         string
	SenderID      string
	SenderName    string
	TimestampUnix int64
	FromMe        bool
}
