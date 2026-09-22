package store

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow"
)

// The deadline crosses the socket, so what it says is a frontend's whole answer
// about whether to offer an edit. A message that could never be edited has to
// say 0 rather than a time that has passed: the two read the same to a clock
// and mean different things to a reader.
func TestEditableUntilAnswersForEveryMessageAnEditCouldReach(t *testing.T) {
	sent := time.Now().Add(-time.Minute).Unix()
	window := int64(whatsmeow.EditWindow / time.Second)

	cases := []struct {
		name string
		msg  Message
		want int64
	}{
		{"our own text", Message{Direction: DirectionOutgoing, TimestampUnix: sent}, sent + window},
		{"our own photo, whose caption is editable", Message{
			Direction: DirectionOutgoing, MediaKind: MediaKindImage, TimestampUnix: sent,
		}, sent + window},
		{"somebody else's", Message{Direction: DirectionIncoming, TimestampUnix: sent}, 0},
		{"one we deleted", Message{Direction: DirectionOutgoing, IsRevoked: true, TimestampUnix: sent}, 0},
		{"a sticker, which has no words", Message{
			Direction: DirectionOutgoing, MediaKind: MediaKindSticker, TimestampUnix: sent,
		}, 0},
		{"a poll", Message{
			Direction: DirectionOutgoing, MediaKind: MediaKindPoll, TimestampUnix: sent,
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.msg.EditableUntil(); got != tc.want {
				t.Fatalf("EditableUntil() = %d, want %d", got, tc.want)
			}
		})
	}
}
