package store

// Sticker is one sticker in the user's library, keyed by content hash. The
// same row serves every source (recents, favorites, store packs, chat cache):
// membership is expressed by last_used, is_favorite and pack_id.
type Sticker struct {
	// CacheKey is hex(FileSHA256). Rows created from app-state favorites lack
	// the plaintext hash and carry a provisional "enc:"+hex(FileEncSHA256)
	// key until the file is downloaded once and rekeyed.
	CacheKey          string
	EncCacheKey       string
	MimeType          string
	IsAnimated        bool
	Width             int32
	Height            int32
	LocalPath         string
	ArchivePath       string
	StickerPayload    []byte
	UploadPayload     []byte
	UploadTS          int64
	Emojis            string
	AccessibilityText string
	PackID            string
	PackOrder         int32
	IsFavorite        bool
	FavoriteTS        int64
	RecentWeight      float64
	LastUsed          int64
}

type StickerPack struct {
	ID                string
	Name              string
	Publisher         string
	Description       string
	Animated          bool
	Lottie            bool
	TrayImageID       string
	TrayLocalPath     string
	ImageDataHash     string
	StickerCount      int32
	StoreOrder        int32
	Installed         bool
	InstalledTS       int64
	ContentsFetchedAt int64
}
