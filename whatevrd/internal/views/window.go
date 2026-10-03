package views

import (
	"context"
	"encoding/binary"
	"math"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/server"
)

// viewFunc is a View as a function.
type viewFunc func(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error)

func (f viewFunc) Open(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	return f(ctx, s, req)
}

// win is a window that reads its items whole each pass.
type win struct {
	params *v2.Subscribe
	items  func(ctx context.Context, max int) ([]*v2.Upsert, error)
	wake   func(c core.Change) bool
	close  func()
	// replaced, when set, says what a removed id folded into
	replaced func(id string) string
	// limit is the window size for a subscribe that gave none, 0 for all
	limit int
}

func (w *win) Items(ctx context.Context, max int) ([]*v2.Upsert, error) { return w.items(ctx, max) }
func (w *win) Wake(c core.Change) bool                                  { return w.wake(c) }
func (w *win) Params() *v2.Subscribe                                    { return w.params }
func (w *win) DefaultLimit() int                                        { return w.limit }

func (w *win) Close() {
	if w.close != nil {
		w.close()
	}
}

// merging is a win whose items fold into each other.
type merging struct{ *win }

func (m merging) ReplacedBy(id string) string { return m.replaced(id) }

// touches says c names any of kinds at all.
func touches(c core.Change, kinds ...string) bool {
	for _, k := range kinds {
		if c.All[k] || len(c.Keys[k]) > 0 {
			return true
		}
	}
	return false
}

// touchesAny says c names kind as a whole, or one of keys under it. a
// message or transfer key counts by its chat.
func touchesAny(c core.Change, kind string, keys map[string]bool) bool {
	if c.All[kind] {
		return true
	}
	for _, k := range c.Keys[kind] {
		if kind == "message" || kind == live.TouchTransfer {
			k, _, _ = cut(k)
		}
		if keys[k] {
			return true
		}
	}
	return false
}

func cut(mk string) (string, string, bool) {
	for i := 0; i < len(mk); i++ {
		if mk[i] == ':' {
			return mk[:i], mk[i+1:], true
		}
	}
	return mk, "", false
}

func set(ss ...string) map[string]bool {
	out := make(map[string]bool, len(ss))
	for _, s := range ss {
		out[s] = true
	}
	return out
}

// one is an object view's only item.
func one(it *v2.Upsert) []*v2.Upsert {
	it.SetSort([]byte{0})
	return []*v2.Upsert{it}
}

// asc is t in bytes that sort as t does.
func asc(b []byte, t int64) []byte {
	return binary.BigEndian.AppendUint64(b, uint64(t)^(1<<63))
}

// desc is t in bytes that sort newest first.
func desc(b []byte, t int64) []byte { return asc(b, math.MaxInt64-t) }

// limited is the first max of items, all when max is 0.
func limited[T any](items []T, max int) []T {
	if max > 0 && len(items) > max {
		return items[:max]
	}
	return items
}

// toMS takes a time in whichever unit whatsapp sent it.
func toMS(t int64) int64 {
	if t > 0 && t < 1e11 {
		return t * 1000
	}
	return t
}

func invalid(format string, args ...any) error {
	return server.Errorf(v2.ErrorCode_ERROR_CODE_INVALID_PARAMS, format, args...)
}

func notFound(format string, args ...any) error {
	return server.Errorf(v2.ErrorCode_ERROR_CODE_NOT_FOUND, format, args...)
}
