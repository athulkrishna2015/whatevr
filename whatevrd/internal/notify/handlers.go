package notify

// Handlers is what a notification click, a link or opening the app reaches.
type Handlers struct {
	// Chat opens a clicked notification's chat, by its key
	Chat func(key string) bool
	// Link opens a whatevr:// link the app was handed
	Link func(url string)
	// Activate is the app opened with nothing in particular
	Activate func()
}
