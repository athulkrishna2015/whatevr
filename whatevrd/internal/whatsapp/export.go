package whatsapp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"whatevrd/internal/model"
)

// exportTimestampLayout matches the official WhatsApp .txt chat export:
// non-padded month/day/year with 12-hour clock, e.g. "5/7/24, 9:50 AM".
const exportTimestampLayout = "1/2/06, 3:04 PM"

// ExportChat writes the chat transcript to destPath in the official WhatsApp
// .txt export format ("M/D/YY, H:MM AM - Sender: body", media as
// "<Media omitted>"). Revoked rows are omitted — deleted stays deleted. The
// write is atomic (temp file + rename) so a failed export never leaves a
// half-written transcript.
func (c *Client) ExportChat(ctx context.Context, chat, destPath string) (string, error) {
	destPath = strings.TrimSpace(destPath)
	if destPath == "" {
		return "", Errorf(ErrInvalid, "path is required")
	}
	if !filepath.IsAbs(destPath) {
		return "", Errorf(ErrInvalid, "destination path must be absolute")
	}
	w, err := c.world()
	if err != nil {
		return "", err
	}
	key := w.Now(model.Norm(chat))
	if key == "" {
		return "", Errorf(ErrInvalid, "chat_id is required")
	}
	if _, ok, err := c.r.ChatIn(ctx, w, key); err != nil {
		return "", err
	} else if !ok {
		return "", Errorf(ErrNotFound, "no chat %q", chat)
	}
	addrs := w.Addrs(key)
	var out strings.Builder
	out.WriteString("Messages and calls are end-to-end encrypted. Only people in this chat can read, listen to, or share them.\n")
	from := model.Cursor{}
	for {
		ms, err := c.r.Messages(ctx, addrs, from, 500, true)
		if err != nil {
			return "", err
		}
		if len(ms) == 0 {
			break
		}
		for _, m := range ms {
			if line := exportLine(w, m); line != "" {
				out.WriteString(line)
				out.WriteString("\n")
			}
		}
		last := ms[len(ms)-1]
		from = model.Cursor{T: last.T, Ord: last.Ord, ID: last.ID}
		if len(ms) < 500 {
			break
		}
	}
	parent := filepath.Dir(destPath)
	if info, err := os.Stat(parent); err != nil || !info.IsDir() {
		return "", Errorf(ErrInvalid, "destination directory does not exist")
	}
	if info, err := os.Lstat(destPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", Errorf(ErrInvalid, "destination must not be a symlink")
		}
		if !info.Mode().IsRegular() {
			return "", Errorf(ErrInvalid, "destination must be a regular file")
		}
	} else if !os.IsNotExist(err) {
		return "", Errorf(ErrInvalid, "destination is not accessible")
	}
	if err := writeFileAtomic(destPath, []byte(out.String()), 0o600); err != nil {
		return "", err
	}
	return destPath, nil
}

// exportLine renders one transcript line. The body keeps its raw text;
// media of any kind becomes "<Media omitted>". Revoked rows and rows with
// neither text nor media return "" and are skipped.
func exportLine(w *model.World, m model.Message) string {
	if m.Facts.Revoked || m.System != nil && m.Text == "" && m.Kind == "" {
		return ""
	}
	body := strings.TrimSpace(exportText(m))
	if body == "" {
		if !exportHasMedia(m) {
			return ""
		}
		body = "<Media omitted>"
	}
	stamp := time.Unix(m.T, 0).Local().Format(exportTimestampLayout)
	return stamp + " - " + exportSender(w, m) + ": " + body
}

// exportText prefers the newest edited content, like the timeline shows.
func exportText(m model.Message) string {
	if e := m.Facts.Edit; e != nil {
		if t := e.GetConversation(); t != "" {
			return t
		}
		if t := e.GetExtendedTextMessage().GetText(); t != "" {
			return t
		}
	}
	return m.Text
}

// exportHasMedia says the row carries media: a media kind, a local file, or
// a live share.
func exportHasMedia(m model.Message) bool {
	switch m.Kind {
	case "imageMessage", "videoMessage", "documentMessage", "audioMessage", "ptvMessage",
		"stickerMessage", "contactMessage", "contactsArrayMessage", "locationMessage",
		"liveLocationMessage", "pollCreationMessage", "groupInviteMessage":
		return true
	}
	return m.Facts.Local.File != "" || m.Facts.Live != nil
}

// exportSender matches the official files: "You" for outgoing, display name
// otherwise, the address user part as fallback.
func exportSender(w *model.World, m model.Message) string {
	if m.FromMe {
		return "You"
	}
	addr := m.Sender
	if addr == "" {
		addr = m.Chat
	}
	if name, _ := w.Name(w.Now(model.Norm(addr))); strings.TrimSpace(name) != "" {
		return name
	}
	if at := strings.Index(addr, "@"); at > 0 {
		return addr[:at]
	}
	return strings.TrimSpace(addr)
}
