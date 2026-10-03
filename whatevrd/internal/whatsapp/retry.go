package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// retryBuffer answers a retry receipt for one of our messages from the log
// when whatsmeow's own store has dropped it: that store forgets after a day,
// a phone offline for a week still asks.
type retryBuffer struct {
	store.EventBuffer
	c *Client
}

func (b *retryBuffer) GetOutgoingEvent(ctx context.Context, chat, alt types.JID, id types.MessageID) (string, []byte, error) {
	format, buf, err := b.EventBuffer.GetOutgoingEvent(ctx, chat, alt, id)
	if err != nil || len(buf) > 0 {
		return format, buf, err
	}
	m, _, merr := b.c.message(ctx, Ref{Chat: chat.ToNonAD().String(), ID: string(id)})
	if merr != nil || !m.FromMe {
		return format, buf, nil
	}
	raw, _ := m.Content()
	if d := raw.GetDeviceSentMessage().GetMessage(); d != nil {
		raw = d
	}
	if raw == nil {
		return format, buf, nil
	}
	buf, err = proto.Marshal(raw)
	if err != nil {
		return "", nil, err
	}
	return "wa", buf, nil
}
