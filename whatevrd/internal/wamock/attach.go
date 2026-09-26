//go:build whatevr_mock

package wamock

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// Attachment is a media message a scenario wants in a chat. It describes what
// the message should be; the bytes are generated, encrypted and hosted when the
// message is created, so a scenario never deals in files.
type Attachment struct {
	kind     string
	caption  string
	filename string
	duration time.Duration
	width    int
	height   int
}

// The media kinds a scenario can ask for. These are the mock's own names; what
// goes on the wire is an ImageMessage, a VideoMessage with or without
// GifPlayback, a PtvMessage, an AudioMessage with or without PTT, a
// DocumentMessage or a StickerMessage.
const (
	attachImage     = "image"
	attachVideo     = "video"
	attachGIF       = "gif"
	attachVideoNote = "videonote"
	attachVoice     = "voice"
	attachAudio     = "audio"
	attachDocument  = "document"
	attachSticker   = "sticker"
)

// Image is a photo, with an optional caption.
func Image(caption string) *Attachment {
	return &Attachment{kind: attachImage, caption: caption, width: 640, height: 480}
}

// Video is a clip that plays with sound controls and a duration. The mock ships
// no video encoder, so the file is motion JPEG in a QuickTime container: real
// enough that ffmpeg pulls a poster out of it and mpv plays it.
func Video(caption string, d time.Duration) *Attachment {
	return &Attachment{kind: attachVideo, caption: caption, duration: d, width: 480, height: 360}
}

// GIF is the looping, muted kind of video. On the wire it is a VideoMessage
// with GifPlayback set, which is what WhatsApp does too; the mock's file is a
// real animated gif.
func GIF(caption string) *Attachment {
	return &Attachment{kind: attachGIF, caption: caption, duration: 2 * time.Second, width: 320, height: 240}
}

// VideoNote is the round one, recorded in the app.
func VideoNote(d time.Duration) *Attachment {
	return &Attachment{kind: attachVideoNote, duration: d, width: 240, height: 240}
}

// Voice is a recorded voice note: the bubble is a waveform and a play button.
func Voice(d time.Duration) *Attachment {
	return &Attachment{kind: attachVoice, duration: d}
}

// Audio is a shared audio file rather than a recording, so it gets a title
// instead of a waveform.
func Audio(title string, d time.Duration) *Attachment {
	return &Attachment{kind: attachAudio, caption: title, duration: d}
}

// Document is a file, named by whatever it is called.
func Document(filename string) *Attachment {
	return &Attachment{kind: attachDocument, filename: filename}
}

// Sticker is a sticker. The file is a real lossless WebP, because that is the
// one format WhatsApp will only ever send a sticker as.
func Sticker() *Attachment {
	return &Attachment{kind: attachSticker, width: 512, height: 512}
}

// Size overrides the dimensions, for testing how a frontend lays out something
// very wide or very tall.
func (a *Attachment) Size(width, height int) *Attachment {
	a.width, a.height = width, height
	return a
}

// text is what the message reads as in a list preview, which for media is the
// caption or nothing at all.
func (a *Attachment) text() string {
	switch a.kind {
	case attachImage, attachVideo, attachGIF:
		return a.caption
	default:
		return ""
	}
}

// build turns an attachment into the protobuf that goes on the wire, hosting
// the file as it goes. seed makes the picture deterministic per message, so two
// runs of the same scenario produce byte-identical media.
func (s *Server) buildAttachment(a *Attachment, seed string) (*waE2E.Message, error) {
	switch a.kind {
	case attachImage:
		data, err := synthJPEG(a.width, a.height, seed)
		if err != nil {
			return nil, err
		}
		ref, err := s.putEncrypted(data, whatsmeow.MediaImage)
		if err != nil {
			return nil, err
		}
		image := &waE2E.ImageMessage{
			Mimetype:      proto.String("image/jpeg"),
			Width:         proto.Uint32(uint32(a.width)),
			Height:        proto.Uint32(uint32(a.height)),
			JPEGThumbnail: synthThumbnail(seed),
		}
		applyMediaRef(image, ref)
		if a.caption != "" {
			image.Caption = proto.String(a.caption)
		}
		return &waE2E.Message{ImageMessage: image}, nil

	case attachVideo, attachGIF, attachVideoNote:
		var (
			data []byte
			mime string
			err  error
		)
		if a.kind == attachGIF {
			data, err = synthGIF(a.width, a.height, 12, seed)
			mime = "image/gif"
		} else {
			frames := max(int(a.duration.Seconds()*5), 5)
			data, err = synthMOV(a.width, a.height, frames, 5, seed)
			mime = "video/quicktime"
		}
		if err != nil {
			return nil, err
		}
		ref, err := s.putEncrypted(data, whatsmeow.MediaVideo)
		if err != nil {
			return nil, err
		}
		video := &waE2E.VideoMessage{
			Mimetype:         proto.String(mime),
			Width:            proto.Uint32(uint32(a.width)),
			Height:           proto.Uint32(uint32(a.height)),
			Seconds:          proto.Uint32(uint32(a.duration.Seconds())),
			JPEGThumbnail:    synthThumbnail(seed),
			StreamingSidecar: ref.Sidecar,
		}
		applyMediaRef(video, ref)
		if a.caption != "" {
			video.Caption = proto.String(a.caption)
		}
		if a.kind == attachGIF {
			video.GifPlayback = proto.Bool(true)
		}
		if a.kind == attachVideoNote {
			return &waE2E.Message{PtvMessage: video}, nil
		}
		return &waE2E.Message{VideoMessage: video}, nil

	case attachVoice, attachAudio:
		data := synthWAV(a.duration, seed)
		ref, err := s.putEncrypted(data, whatsmeow.MediaAudio)
		if err != nil {
			return nil, err
		}
		audio := &waE2E.AudioMessage{
			Mimetype:         proto.String("audio/wav"),
			Seconds:          proto.Uint32(uint32(a.duration.Seconds())),
			StreamingSidecar: ref.Sidecar,
		}
		applyMediaRef(audio, ref)
		if a.kind == attachVoice {
			audio.PTT = proto.Bool(true)
			audio.Waveform = synthWaveform(seed)
		}
		return &waE2E.Message{AudioMessage: audio}, nil

	case attachDocument:
		title := strings.TrimSuffix(a.filename, filepath.Ext(a.filename))
		data, mime := synthDocument(a.filename, title)
		ref, err := s.putEncrypted(data, whatsmeow.MediaDocument)
		if err != nil {
			return nil, err
		}
		document := &waE2E.DocumentMessage{
			Mimetype:  proto.String(mime),
			FileName:  proto.String(a.filename),
			Title:     proto.String(title),
			PageCount: proto.Uint32(1),
		}
		applyMediaRef(document, ref)
		return &waE2E.Message{DocumentMessage: document}, nil

	case attachSticker:
		data, err := synthWebP(a.width, a.height, seed)
		if err != nil {
			return nil, err
		}
		ref, err := s.putEncrypted(data, whatsmeow.MediaImage)
		if err != nil {
			return nil, err
		}
		sticker := &waE2E.StickerMessage{
			Mimetype: proto.String("image/webp"),
			Width:    proto.Uint32(uint32(a.width)),
			Height:   proto.Uint32(uint32(a.height)),
		}
		applyMediaRef(sticker, ref)
		return &waE2E.Message{StickerMessage: sticker}, nil
	}
	return nil, fmt.Errorf("unknown attachment kind %q", a.kind)
}

// mediaCarrier is the shape every media protobuf shares. Setting the fields
// through it rather than six times over is the only reason it exists.
type mediaCarrier interface {
	GetURL() string
}

func applyMediaRef(message mediaCarrier, ref mediaRef) {
	url := "https://" + mediaHost + ref.DirectPath
	switch m := message.(type) {
	case *waE2E.ImageMessage:
		m.URL, m.DirectPath, m.MediaKey = &url, &ref.DirectPath, ref.MediaKey
		m.FileSHA256, m.FileEncSHA256, m.FileLength = ref.FileSHA256, ref.FileEncSHA, &ref.FileLength
	case *waE2E.VideoMessage:
		m.URL, m.DirectPath, m.MediaKey = &url, &ref.DirectPath, ref.MediaKey
		m.FileSHA256, m.FileEncSHA256, m.FileLength = ref.FileSHA256, ref.FileEncSHA, &ref.FileLength
	case *waE2E.AudioMessage:
		m.URL, m.DirectPath, m.MediaKey = &url, &ref.DirectPath, ref.MediaKey
		m.FileSHA256, m.FileEncSHA256, m.FileLength = ref.FileSHA256, ref.FileEncSHA, &ref.FileLength
	case *waE2E.DocumentMessage:
		m.URL, m.DirectPath, m.MediaKey = &url, &ref.DirectPath, ref.MediaKey
		m.FileSHA256, m.FileEncSHA256, m.FileLength = ref.FileSHA256, ref.FileEncSHA, &ref.FileLength
	case *waE2E.StickerMessage:
		m.URL, m.DirectPath, m.MediaKey = &url, &ref.DirectPath, ref.MediaKey
		m.FileSHA256, m.FileEncSHA256, m.FileLength = ref.FileSHA256, ref.FileEncSHA, &ref.FileLength
	}
}

// synthWaveform is the 64 byte bar chart a voice note carries so the bubble has
// something to draw before the file is downloaded.
func synthWaveform(seed string) []byte {
	out := make([]byte, 64)
	for i := range out {
		progress := float64(i) / float64(len(out))
		// The same swell the audio itself has, quantised to the 0-100 range
		// WhatsApp uses.
		out[i] = byte(20 + 75*progress*(1-progress)*4)
	}
	return out
}

// synthDocument makes a file that actually opens. A .pdf gets a real one page
// pdf; anything else gets plain text, because a mock that hands the desktop a
// .docx full of pdf is worse than one that is honest about what it can make.
func synthDocument(filename, title string) (data []byte, mime string) {
	if strings.EqualFold(filepath.Ext(filename), ".pdf") {
		return synthPDF(title), "application/pdf"
	}
	body := fmt.Sprintf("%s\n\nGenerated by the whatevrd mock server.\nThis file exists so that a document message has something real to download.\n", title)
	return []byte(body), "text/plain"
}
