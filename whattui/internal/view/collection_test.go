package view

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

func chat(id, name string) *v2.ChatRow { return v2.ChatRow_builder{Id: id, Name: name}.Build() }

func put(sort, id string) *v2.Change {
	return v2.Change_builder{Upsert: v2.Upsert_builder{Id: id, Sort: []byte(sort), Chat: chat(id, "")}.Build()}.Build()
}

func drop(id, by string) *v2.Change {
	return v2.Change_builder{Remove: v2.Remove_builder{Id: id, ReplacedBy: by}.Build()}.Build()
}

func changes(c ...*v2.Change) *v2.ViewUpdate { return v2.ViewUpdate_builder{Changes: c}.Build() }

func order(c *Collection[*v2.ChatRow]) string {
	var ids []string
	c.Read(func(items []Item[*v2.ChatRow], _ State) {
		for _, it := range items {
			ids = append(ids, it.ID)
		}
	})
	return strings.Join(ids, ",")
}

func TestOrdersByBytewiseSortNotByAnyField(t *testing.T) {
	c := NewCollection(Chat)
	up := func(sort, id, name string) *v2.Change {
		return v2.Change_builder{Upsert: v2.Upsert_builder{Id: id, Sort: []byte(sort), Chat: chat(id, name)}.Build()}.Build()
	}
	c.Apply(changes(up("3", "a", "aaa"), up("1", "b", "zzz"), up("2", "c", "mmm")), false)
	if got := order(c); got != "b,c,a" {
		t.Errorf("order = %q, want b,c,a", got)
	}
}

func TestSortIsComparedAsBytesNotAsANumber(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("9", "nine"), put("10", "ten"), put("\xff", "high"), put("\x00", "low")), false)
	if got := order(c); got != "low,ten,nine,high" {
		t.Errorf("order = %q, want low,ten,nine,high", got)
	}
}

func TestIdBreaksATieStably(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("1", "b"), put("1", "a"), put("1", "c")), false)
	if got := order(c); got != "a,b,c" {
		t.Errorf("order = %q, want a,b,c", got)
	}
}

func TestAChangedSortMovesTheItem(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("1", "a"), put("2", "b"), put("3", "c")), false)
	c.Apply(changes(put("4", "a")), false)
	if got := order(c); got != "b,c,a" {
		t.Errorf("order = %q, want b,c,a", got)
	}
	if c.IndexOf("a") != 2 || c.IndexOf("b") != 0 {
		t.Errorf("index is stale: a=%d b=%d", c.IndexOf("a"), c.IndexOf("b"))
	}
}

// a remove and an upsert of one id in one update mean what they say in the
// order they say it, in a small update and in one big enough to rebuild
func TestOneIdChangedTwiceInAnUpdateEndsAsTheLastChangeSays(t *testing.T) {
	for _, pad := range []int{0, rebuildThreshold + 1} {
		var filler []*v2.Change
		for i := 0; i < pad; i++ {
			filler = append(filler, put(fmt.Sprintf("z%03d", i), fmt.Sprintf("f%03d", i)))
		}
		c := NewCollection(Chat)
		c.Apply(changes(put("1", "a")), false)
		c.Apply(changes(append(filler, drop("a", ""), put("2", "a"))...), false)
		if _, ok := c.Get("a"); !ok {
			t.Errorf("pad %d: remove then upsert lost the item", pad)
		}
		c.Apply(changes(append(filler, put("3", "a"), drop("a", ""))...), false)
		if _, ok := c.Get("a"); ok || c.IndexOf("a") != -1 {
			t.Errorf("pad %d: upsert then remove kept the item", pad)
		}
	}
}

func TestABigUpdateOrdersTheSameAsSmallOnes(t *testing.T) {
	var all []*v2.Change
	small := NewCollection(Chat)
	for i := 0; i < 3*rebuildThreshold; i++ {
		ch := put(fmt.Sprintf("%02d", (i*7)%(3*rebuildThreshold)), fmt.Sprintf("i%02d", i))
		all = append(all, ch)
		small.Apply(changes(ch), false)
	}
	big := NewCollection(Chat)
	big.Apply(changes(all...), false)
	if order(big) != order(small) {
		t.Errorf("big %q\nsmall %q", order(big), order(small))
	}
	for i := 0; i < big.Len(); i++ {
		var id string
		big.Read(func(items []Item[*v2.ChatRow], _ State) { id = items[i].ID })
		if big.IndexOf(id) != i {
			t.Fatalf("index of %s is %d, want %d", id, big.IndexOf(id), i)
		}
	}
}

// reset, changes and ready are one update: what was there before never
// shows through, and the new window is ready with it
func TestAResetUpdateReplacesTheWindowWhole(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(v2.ViewUpdate_builder{Changes: []*v2.Change{put("1", "old")}, Ready: v2.Ready_builder{Exhausted: true}.Build()}.Build(), false)
	c.Apply(v2.ViewUpdate_builder{Reset: true, Changes: []*v2.Change{put("1", "new")}}.Build(), false)
	if got := order(c); got != "new" {
		t.Errorf("order = %q, want only the new window", got)
	}
	if c.IsReady() || c.Exhausted() {
		t.Error("a reset kept the old window's ready")
	}
	c.Apply(v2.ViewUpdate_builder{Ready: &v2.Ready{}}.Build(), false)
	if !c.IsReady() || c.Exhausted() {
		t.Error("ready did not land")
	}
}

// the first update of a subscription just issued replaces whatever the sink
// held, reset flag or not: the daemon sends none on a fresh subscribe
func TestAFreshUpdateIsAReset(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("1", "old")), false)
	c.Apply(changes(put("2", "new")), true)
	if got := order(c); got != "new" {
		t.Errorf("order = %q, want the fresh window alone", got)
	}
}

func TestReverseFlipsTheComparisonWithoutTouchingTheKey(t *testing.T) {
	c := NewCollection(Chat)
	c.SetReverse(true)
	c.Apply(changes(put("1", "a"), put("2", "b"), put("2", "c")), false)
	if got := order(c); got != "c,b,a" {
		t.Errorf("order = %q, want c,b,a", got)
	}
	it, _ := c.Get("a")
	if it.Sort != "1" {
		t.Errorf("sort = %q, want the key as sent", it.Sort)
	}
}

// a revision moves with every upsert of an item and only then, which is
// what a layout cache keys on
func TestARevisionMovesOnlyWhenItsItemIsUpserted(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("1", "a"), put("2", "b")), false)
	a1, _ := c.Get("a")
	b1, _ := c.Get("b")
	c.Apply(changes(put("1", "a")), false)
	a2, _ := c.Get("a")
	b2, _ := c.Get("b")
	if a2.Rev == a1.Rev {
		t.Error("an upserted item kept its revision")
	}
	if b2.Rev != b1.Rev {
		t.Error("an untouched item got a new revision")
	}
}

func TestAReplacementIsToldAfterTheUpdateLands(t *testing.T) {
	c := NewCollection(Chat)
	c.Apply(changes(put("1", "old")), false)
	var told []string
	c.OnReplaced = func(old, by string) {
		_, gone := c.Get(old)
		_, here := c.Get(by)
		told = append(told, fmt.Sprintf("%s>%s %v %v", old, by, !gone, here))
	}
	c.Apply(changes(drop("old", "new"), put("1", "new")), false)
	if len(told) != 1 || told[0] != "old>new true true" {
		t.Errorf("told %v, want old>new once with the update applied", told)
	}
}

func TestAnotherViewsRowIsReportedNotSwallowed(t *testing.T) {
	c := NewCollection(Chat)
	var bad []*v2.Upsert
	c.OnMismatch = func(u *v2.Upsert) { bad = append(bad, u) }
	msg := v2.Upsert_builder{Id: "m", Sort: []byte("1"), Message: &v2.MessageRow{}}.Build()
	c.Apply(changes(v2.Change_builder{Upsert: msg}.Build(), put("2", "a")), false)
	if len(bad) != 1 || bad[0].GetId() != "m" {
		t.Errorf("mismatches %v, want the message row", bad)
	}
	if got := order(c); got != "a" {
		t.Errorf("order = %q, want the chat alone", got)
	}
}

func TestVersionBumpsOnEveryAppliedUpdate(t *testing.T) {
	c := NewCollection(Chat)
	v := c.Version()
	c.Apply(changes(put("1", "a")), false)
	c.Apply(v2.ViewUpdate_builder{Ready: &v2.Ready{}}.Build(), false)
	if c.Version() != v+2 {
		t.Errorf("version %d, want %d", c.Version(), v+2)
	}
}

// a reader sees an update all or nothing: a window is never caught between
// its reset and its rows
func TestNoReaderSeesHalfAnUpdate(t *testing.T) {
	c := NewCollection(Chat)
	var window []*v2.Change
	for i := 0; i < 40; i++ {
		window = append(window, put(fmt.Sprintf("%02d", i), fmt.Sprintf("i%02d", i)))
	}
	reset := v2.ViewUpdate_builder{Reset: true, Changes: window, Ready: &v2.Ready{}}.Build()
	c.Apply(reset, false)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				c.Apply(reset, false)
			}
		}
	}()
	for i := 0; i < 2000; i++ {
		c.Read(func(items []Item[*v2.ChatRow], s State) {
			if len(items) != 40 || !s.Ready {
				t.Errorf("read %d items, ready %v", len(items), s.Ready)
			}
		})
	}
	close(stop)
	wg.Wait()
}
