package wa

import (
	"context"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"

	waE2E "go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

func historySyncNotificationEvent(id string) *events.Message {
	syncType := waE2E.HistorySyncType_RECENT
	return &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{}, ID: types.MessageID(id)},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				HistorySyncNotification: &waE2E.HistorySyncNotification{
					SyncType:   &syncType,
					ChunkOrder: proto.Uint32(3),
					DirectPath: proto.String("/v/t62.1234"),
					MediaKey:   []byte("key"),
				},
			},
		},
	}
}

// The saved row is the only record that a chunk was offered, and the media it
// names is the only copy of that history. If the insert fails the ack has to go
// unsent so the server offers it again.
func TestFailedHistorySyncChunkPersistIsNotAcked(t *testing.T) {
	ctx := context.Background()
	db, err := appstore.Open(ctx, filepath.Join(t.TempDir(), "whatevrd.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	client := &Client{store: db, daemon: app.NewDaemon(app.Paths{}), log: waLog.Noop}

	if !client.handleMessage(ctx, historySyncNotificationEvent("chunk-ok"), false) {
		t.Fatal("a chunk that persisted refused the ack")
	}
	chunks, err := db.ListRecoverableHistorySyncChunks(ctx, 10)
	if err != nil {
		t.Fatalf("list chunks: %v", err)
	}
	if len(chunks) != 1 || chunks[0].ID != "chunk-ok" {
		t.Fatalf("expected the chunk to be queued, got %+v", chunks)
	}

	// The store is gone under it: every write from here fails.
	db.Close()
	if client.handleMessage(ctx, historySyncNotificationEvent("chunk-lost"), false) {
		t.Fatal("a chunk that could not be persisted was acked, so the server will never offer it again")
	}
}
