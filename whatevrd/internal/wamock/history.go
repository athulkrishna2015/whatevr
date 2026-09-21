//go:build whatevr_mock

package wamock

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"sort"
	"time"

	"go.mau.fi/whatsmeow"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waHistorySync"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"google.golang.org/protobuf/proto"
)

// historyChunkChats is how many conversations go in one chunk. Small on
// purpose: the point of several chunks is that the sync view has something to
// report progress about, which is a UI state that cannot otherwise be produced
// on demand.
const historyChunkChats = 2

// sendHistorySync hands the client everything the account already had: the
// contact names, the push names, and whatever the scenario put behind
// Chat.History. Each chunk is a real zlib-compressed, AES-encrypted blob the
// client downloads from the mock's media host, because that is the path the
// daemon takes with ManualHistorySyncDownload set.
func (s *session) sendHistorySync(ctx context.Context) {
	world := s.srv.world
	if world == nil {
		return
	}
	chunks := world.historyChunks()
	if len(chunks) == 0 {
		return
	}
	pace := world.historyPace()
	if pace <= 0 {
		pace = s.srv.opts.HistoryDelay
	}
	for _, chunk := range chunks {
		blob := chunk
		s.enqueue(func(ctx context.Context) error {
			// A real initial sync arrives over tens of seconds. Pacing it is
			// what makes the sync view's progress an observable state rather
			// than something that has already finished by the time a frontend
			// draws its first frame.
			if pace > 0 {
				select {
				case <-time.After(pace):
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return s.sendHistoryChunk(ctx, blob)
		})
	}
}

// historyChunks builds the sync the scenario described. The first chunk carries
// the contacts, because that is where saved names come from and a chat named by
// its phone number is the most visible thing a mock can get wrong.
func (w *World) historyChunks() []*waHistorySync.HistorySync {
	w.mu.Lock()
	chats := append([]*Chat(nil), w.order...)
	contacts := make([]*Contact, 0, len(w.contacts))
	for _, contact := range w.contacts {
		contacts = append(contacts, contact)
	}
	history := append([]*Msg(nil), w.history...)
	w.mu.Unlock()

	sort.Slice(contacts, func(i, j int) bool { return contacts[i].JID.User < contacts[j].JID.User })
	sort.SliceStable(history, func(i, j int) bool { return history[i].At.Before(history[j].At) })

	byChat := map[string][]*Msg{}
	for _, msg := range history {
		byChat[msg.Chat.JID.String()] = append(byChat[msg.Chat.JID.String()], msg)
	}

	conversations := make([]*waHistorySync.Conversation, 0, len(chats))
	for _, chat := range chats {
		conversations = append(conversations, chat.conversation(byChat[chat.JID.String()]))
	}

	var chunks []*waHistorySync.HistorySync
	pushNames := make([]*waHistorySync.Pushname, 0, len(contacts))
	inline := make([]*waHistorySync.InlineContact, 0, len(contacts))
	for _, contact := range contacts {
		if contact.Name != "" {
			pushNames = append(pushNames, &waHistorySync.Pushname{
				ID:       proto.String(contact.JID.String()),
				Pushname: proto.String(contact.Name),
			})
		}
		if contact.Saved == "" {
			continue
		}
		inline = append(inline, &waHistorySync.InlineContact{
			PnJID:     proto.String(contact.JID.String()),
			LidJID:    proto.String(lidFor(contact.JID).String()),
			FullName:  proto.String(contact.Saved),
			FirstName: proto.String(firstWord(contact.Saved)),
		})
	}

	total := (len(conversations) + historyChunkChats - 1) / historyChunkChats
	if total == 0 {
		total = 1
	}
	for i := 0; i < total; i++ {
		start := i * historyChunkChats
		end := min(start+historyChunkChats, len(conversations))
		syncType := waHistorySync.HistorySync_INITIAL_BOOTSTRAP
		chunk := &waHistorySync.HistorySync{
			SyncType:               &syncType,
			ChunkOrder:             proto.Uint32(uint32(i + 1)),
			Progress:               proto.Uint32(uint32((i + 1) * 100 / total)),
			Conversations:          conversations[start:end],
			InlineContactsProvided: proto.Bool(i == 0),
		}
		if i == 0 {
			chunk.InlineContacts = inline
		}
		chunks = append(chunks, chunk)
	}
	// Push names last, which is where a real sync puts them. The daemon sorts
	// chunks by sync type before processing anyway, so the wire order decides
	// nothing.
	if len(pushNames) > 0 {
		syncType := waHistorySync.HistorySync_PUSH_NAME
		chunks = append(chunks, &waHistorySync.HistorySync{
			SyncType:   &syncType,
			ChunkOrder: proto.Uint32(uint32(total + 1)),
			Progress:   proto.Uint32(100),
			Pushnames:  pushNames,
		})
	}
	return chunks
}

// conversation is one chat as history sync describes it: its own metadata plus
// whatever messages the scenario put behind Chat.History.
func (c *Chat) conversation(messages []*Msg) *waHistorySync.Conversation {
	conv := &waHistorySync.Conversation{
		ID:                   proto.String(c.JID.String()),
		UnreadCount:          proto.Uint32(c.unread),
		EndOfHistoryTransfer: proto.Bool(true),
	}
	if c.IsGroup {
		conv.Name = proto.String(c.Name)
		conv.CreatedAt = proto.Uint64(uint64(c.CreatedAt.Unix()))
		if owner := c.Other(); owner != nil {
			conv.CreatedBy = proto.String(owner.JID.String())
		}
	} else {
		conv.LidJID = proto.String(lidFor(c.JID).String())
	}
	if c.pinOrder > 0 {
		conv.Pinned = proto.Uint32(c.pinOrder)
	}
	if len(messages) > 0 {
		conv.LastMsgTimestamp = proto.Uint64(uint64(messages[len(messages)-1].At.Unix()))
		conv.ConversationTimestamp = conv.LastMsgTimestamp
	}
	for _, msg := range messages {
		conv.Messages = append(conv.Messages, &waHistorySync.HistorySyncMsg{Message: msg.webMessage()})
	}
	return conv
}

// webMessage is the WebMessageInfo shape ParseWebMessage reads. History sync is
// the one place messages arrive as protobuf rather than as Signal ciphertext.
func (m *Msg) webMessage() *waWeb.WebMessageInfo {
	status := waWeb.WebMessageInfo_DELIVERY_ACK
	if m.FromMe {
		status = waWeb.WebMessageInfo_READ
	}
	info := &waWeb.WebMessageInfo{
		Key: &waCommon.MessageKey{
			ID:        proto.String(m.ID),
			FromMe:    proto.Bool(m.FromMe),
			RemoteJID: proto.String(m.Chat.JID.String()),
		},
		MessageTimestamp: proto.Uint64(uint64(m.At.Unix())),
		Message:          &waE2E.Message{Conversation: proto.String(m.Text)},
		Status:           &status,
	}
	if m.Chat.IsGroup && !m.FromMe {
		info.Participant = proto.String(m.From.JID.String())
		info.Key.Participant = proto.String(m.From.JID.String())
	}
	if !m.FromMe && m.From.Name != "" {
		info.PushName = proto.String(m.From.Name)
	}
	return info
}

// sendHistoryChunk hosts one chunk and tells the client where to find it. The
// notification is a protocol message from the account's own device, which is
// the only kind of message the client will act on this way.
func (s *session) sendHistoryChunk(ctx context.Context, chunk *waHistorySync.HistorySync) error {
	raw, err := proto.Marshal(chunk)
	if err != nil {
		return fmt.Errorf("marshal history sync: %w", err)
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		return fmt.Errorf("compress history sync: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("compress history sync: %w", err)
	}
	ref, err := s.srv.putEncrypted(compressed.Bytes(), whatsmeow.MediaHistory)
	if err != nil {
		return err
	}

	syncType := waE2E.HistorySyncType(chunk.GetSyncType())
	protocolType := waE2E.ProtocolMessage_HISTORY_SYNC_NOTIFICATION
	message := &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type: &protocolType,
			HistorySyncNotification: &waE2E.HistorySyncNotification{
				SyncType:      &syncType,
				ChunkOrder:    proto.Uint32(chunk.GetChunkOrder()),
				Progress:      proto.Uint32(chunk.GetProgress()),
				FileLength:    proto.Uint64(ref.FileLength),
				DirectPath:    proto.String(ref.DirectPath),
				MediaKey:      ref.MediaKey,
				FileSHA256:    ref.FileSHA256,
				FileEncSHA256: ref.FileEncSHA,
			},
		},
	}
	plaintext, err := proto.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal history notification: %w", err)
	}
	return s.sendSelfProtocolMessage(ctx, plaintext)
}

// sendSelfProtocolMessage delivers something from the account's own phone to
// this device. whatsmeow ignores a protocol message that is not from me, so the
// addressing here is load-bearing rather than cosmetic.
func (s *session) sendSelfProtocolMessage(ctx context.Context, plaintext []byte) error {
	sender := s.srv.accountJID()
	sender.Device = selfDevice
	p, err := s.srv.peerFor(sender, s.srv.encryptionJID(sender))
	if err != nil {
		return err
	}
	enc, err := p.encrypt(ctx, s.srv, s.jid, plaintext)
	if err != nil {
		return err
	}
	return s.sendNode(ctx, waBinary.Node{
		Tag: "message",
		Attrs: waBinary.Attrs{
			"id":                 s.srv.rng.messageID(),
			"from":               sender,
			"recipient":          s.srv.accountJID(),
			"peer_recipient_lid": lidFor(s.srv.accountJID()),
			"t":                  fmt.Sprintf("%d", time.Now().Unix()),
			"type":               "text",
			"category":           "peer",
		},
		Content: []waBinary.Node{enc},
	})
}

func firstWord(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == ' ' {
			return name[:i]
		}
	}
	return name
}
