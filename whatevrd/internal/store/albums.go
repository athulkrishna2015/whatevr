package store

import (
	"context"
	"database/sql"
	"strings"
)

// Albums: several pictures sent as one thing.
//
// WhatsApp does not send an album as one message. It sends a header carrying
// nothing but the counts, then each picture as an ordinary image or video
// message naming that header as its parent. Every child keeps its own id, its
// own media, its own download state, its own receipts and its own reactions,
// which is exactly what we want: a tile is a real message and reuses every path
// a lone photo already has.
//
// So the grouping is a read-time question, not a storage one. A child row is
// hidden from the transcript when, and only when, its parent row actually
// exists here (albumChildExclusion), and the parent carries its children with
// it (attachAlbums). The two use the same predicate, which is what makes an
// album that lost its header degrade into ordinary bubbles rather than into
// nothing: no parent, no hiding.

// albumChildExclusion hides an album's children from a transcript query. Append
// it to a WHERE clause that already has a condition.
//
// The parent-exists half is not defensive: a child can arrive before its header
// (history sync orders by nothing in particular, and a resend arrives alone),
// and a child hidden behind a parent that never came would be a picture that
// silently does not exist. The empty-string test in front is what keeps this
// off the hot path for the 99% of rows that are in no album at all.
const albumChildExclusion = `
	AND (m.album_parent_id = '' OR NOT EXISTS (
		SELECT 1 FROM messages parent WHERE parent.id = m.album_parent_id
	))
`

// attachAlbums loads the children of every album row in the page, in one query
// for the whole page, and hangs them off their parent in album order.
//
// Children are scanned as ordinary messages and deliberately get no extras of
// their own: attaching reactions and tallies to them would recurse (a child is
// a message, which could carry an album, which has children), and a tile has no
// room to draw a reaction anyway. Opening one goes through the viewer, which
// reads the child by id and gets the full row.
func attachAlbums(ctx context.Context, q reactionQueryer, messages []Message) error {
	parentIDs := make([]any, 0)
	indexByID := make(map[string]int)
	for i := range messages {
		messages[i].Album = nil
		if messages[i].MediaKind != MediaKindAlbum {
			continue
		}
		parentIDs = append(parentIDs, messages[i].ID)
		indexByID[messages[i].ID] = i
	}
	if len(parentIDs) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(parentIDs)), ",")
	rows, err := q.QueryContext(ctx, messageSelectPrefix+`
		WHERE m.album_parent_id IN (`+placeholders+`)
		ORDER BY m.album_index ASC, m.timestamp ASC, m.rowid ASC
	`, parentIDs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	children, err := scanMessageRows(rows, len(parentIDs))
	if err != nil {
		return err
	}
	for _, child := range children {
		i, ok := indexByID[child.AlbumParentID]
		if !ok {
			continue
		}
		messages[i].Album = append(messages[i].Album, child)
	}
	return nil
}

// rowQueryer is satisfied by both *sql.DB and *sql.Tx, so the same question can
// be asked on the read connection or inside the write transaction that is about
// to insert the row it is being asked about.
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// albumChildIsHidden reports whether a row with this parent id will be hidden
// from the transcript, which is the single fact everything else about an album
// child follows from. Hidden means the album draws it, so it must not also
// reorder the chat, change its preview, raise a notification or add to the
// unread badge: the album header already did all four.
func albumChildIsHidden(ctx context.Context, q rowQueryer, albumParentID string) bool {
	if albumParentID == "" {
		return false
	}
	var id string
	return q.QueryRowContext(ctx,
		`SELECT id FROM messages WHERE id = ?`, albumParentID).Scan(&id) == nil
}

// albumParentOf returns the id of the album row that hides this message, or ""
// when the message is not a hidden child. It answers the same question
// albumChildExclusion asks, for one row, so jumping to a message can land on
// the album that actually renders it rather than on a row nothing draws.
func (db *DB) albumParentOf(ctx context.Context, message Message) string {
	if message.AlbumParentID == "" {
		return ""
	}
	var id string
	err := db.reader().QueryRowContext(ctx,
		`SELECT id FROM messages WHERE id = ?`, message.AlbumParentID).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}
