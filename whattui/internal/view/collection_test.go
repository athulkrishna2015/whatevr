package view

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
)

type row struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func raw(id, name string) json.RawMessage {
	return json.RawMessage(fmt.Sprintf(`{"id":%q,"name":%q}`, id, name))
}

// order is the ids in view order, which is the only thing a renderer cares
// about.
func order(c *Collection[row]) string {
	var ids []string
	c.Read(func(items []Item[row]) {
		for _, it := range items {
			ids = append(ids, it.ID)
		}
	})
	return strings.Join(ids, ",")
}

// fill applies a batch the way the client would.
func fill(c *Collection[row], apply func()) {
	c.BatchBegin()
	apply()
	c.BatchEnd()
}

func TestOrdersByBytewiseSortNotByAnyField(t *testing.T) {
	c := NewCollection[row]()
	// Names sort one way, sort keys the other. The daemon's key wins.
	fill(c, func() {
		c.Upsert("3", raw("a", "aaa"))
		c.Upsert("1", raw("b", "zzz"))
		c.Upsert("2", raw("c", "mmm"))
	})
	if got := order(c); got != "b,c,a" {
		t.Errorf("order = %q, want b,c,a", got)
	}
}

func TestSortIsComparedAsBytesNotAsANumber(t *testing.T) {
	c := NewCollection[row]()
	// "10" sorts before "9" bytewise. A frontend that parsed the key would
	// get this the other way round, which is exactly why it must not.
	fill(c, func() {
		c.Upsert("9", raw("nine", ""))
		c.Upsert("10", raw("ten", ""))
	})
	if got := order(c); got != "ten,nine" {
		t.Errorf("order = %q, want ten,nine", got)
	}
}

func TestIdBreaksATieStably(t *testing.T) {
	c := NewCollection[row]()
	fill(c, func() {
		c.Upsert("same", raw("z", ""))
		c.Upsert("same", raw("a", ""))
		c.Upsert("same", raw("m", ""))
	})
	if got := order(c); got != "a,m,z" {
		t.Errorf("order = %q, want a,m,z", got)
	}
}

func TestAChangedSortMovesTheItem(t *testing.T) {
	c := NewCollection[row]()
	fill(c, func() {
		c.Upsert("1", raw("a", ""))
		c.Upsert("2", raw("b", ""))
		c.Upsert("3", raw("c", ""))
	})
	// This is a chat being bumped to the top of the list by a new message.
	fill(c, func() { c.Upsert("0", raw("c", "bumped")) })

	if got := order(c); got != "c,a,b" {
		t.Errorf("order = %q, want c,a,b", got)
	}
	if got, _ := c.Get("c"); got.Value.Name != "bumped" {
		t.Errorf("value did not update on the move: %+v", got.Value)
	}
	if c.Len() != 3 {
		t.Errorf("len = %d, want 3: a move must not duplicate", c.Len())
	}
}

func TestRemoveThenReupsertOfOneIdResolvesInArrivalOrder(t *testing.T) {
	// The wire meant: it went away, then it came back. Resolving these the
	// other way round loses the row entirely.
	c := NewCollection[row]()
	fill(c, func() { c.Upsert("1", raw("x", "first")) })
	fill(c, func() {
		c.Remove("x")
		c.Upsert("2", raw("x", "again"))
	})
	if got := order(c); got != "x" {
		t.Fatalf("order = %q, want x", got)
	}
	it, _ := c.Get("x")
	if it.Value.Name != "again" || it.Sort != "2" {
		t.Errorf("item = %+v, want the re-upserted one", it)
	}
}

func TestReupsertThenRemoveOfOneIdAlsoResolvesInArrivalOrder(t *testing.T) {
	c := NewCollection[row]()
	fill(c, func() { c.Upsert("1", raw("x", "first")) })
	fill(c, func() {
		c.Upsert("2", raw("x", "again"))
		c.Remove("x")
	})
	if got := order(c); got != "" {
		t.Errorf("order = %q, want empty: the remove came last", got)
	}
}

func TestABigBatchOrdersTheSameAsASmallOne(t *testing.T) {
	// Past rebuildThreshold the batch settles through a map and re-sorts
	// instead of inserting one at a time. Both paths must agree.
	small, big := NewCollection[row](), NewCollection[row]()
	keys := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		keys = append(keys, fmt.Sprintf("%03d", (i*17)%40))
	}
	for i, k := range keys {
		id := fmt.Sprintf("id%02d", i)
		fill(small, func() { small.Upsert(k, raw(id, "")) })
	}
	fill(big, func() {
		for i, k := range keys {
			big.Upsert(k, raw(fmt.Sprintf("id%02d", i), ""))
		}
	})
	if order(small) != order(big) {
		t.Errorf("one-at-a-time %q != rebuilt %q", order(small), order(big))
	}
}

func TestABigBatchDropsRemovedIdsFromTheIndex(t *testing.T) {
	// The rebuild path re-sorts from a map. Patching the index instead of
	// rebuilding it leaves a removed id pointing at whatever row took its
	// place.
	c := NewCollection[row]()
	fill(c, func() {
		for i := 0; i < 30; i++ {
			c.Upsert(fmt.Sprintf("%03d", i), raw(fmt.Sprintf("id%02d", i), ""))
		}
	})
	fill(c, func() {
		for i := 0; i < 30; i += 2 {
			c.Remove(fmt.Sprintf("id%02d", i))
		}
		for i := 100; i < 120; i++ {
			c.Upsert(fmt.Sprintf("%03d", i), raw(fmt.Sprintf("new%02d", i), ""))
		}
	})
	if _, ok := c.Get("id00"); ok {
		t.Error("a removed id is still resolvable")
	}
	if got := c.IndexOf("id00"); got != -1 {
		t.Errorf("IndexOf a removed id = %d, want -1", got)
	}
	c.Read(func(items []Item[row]) {
		for i, it := range items {
			if c.index[it.ID] != i {
				t.Fatalf("index for %s says %d, actually %d", it.ID, c.index[it.ID], i)
			}
		}
	})
}

func TestResetDiscardsEverythingIncludingReadiness(t *testing.T) {
	c := NewCollection[row]()
	fill(c, func() { c.Upsert("1", raw("a", "")) })
	c.Ready(true, true)

	c.Reset()

	if c.Len() != 0 || order(c) != "" {
		t.Errorf("reset left %d items", c.Len())
	}
	if c.IsReady() {
		t.Error("reset left the view ready")
	}
	if ex, has := c.Exhausted(); ex || has {
		t.Errorf("reset left exhausted = %v/%v", ex, has)
	}
	if _, ok := c.Get("a"); ok {
		t.Error("reset left the index populated")
	}
}

func TestResetMidBatchDoesNotLetThePendingOpsLandAfterwards(t *testing.T) {
	// The daemon recovering a slow consumer sends reset in the middle of a
	// read. Ops banked before it describe a world that no longer exists.
	c := NewCollection[row]()
	c.BatchBegin()
	c.Upsert("1", raw("stale", ""))
	c.Reset()
	c.BatchEnd()

	if got := order(c); got != "" {
		t.Errorf("order = %q, want empty: pre-reset ops must not survive it", got)
	}
}

func TestExhaustedTellsAnOmittedFieldFromAFalseOne(t *testing.T) {
	c := NewCollection[row]()
	c.Ready(false, false)
	if ex, has := c.Exhausted(); ex || has {
		t.Errorf("omitted: got %v/%v, want false/false", ex, has)
	}
	c.Ready(false, true)
	if ex, has := c.Exhausted(); ex || !has {
		t.Errorf("present and false: got %v/%v, want false/true", ex, has)
	}
}

func TestReverseFlipsTheComparisonWithoutTouchingTheKey(t *testing.T) {
	c := NewCollection[row]()
	c.SetReverse(true)
	fill(c, func() {
		c.Upsert("1", raw("oldest", ""))
		c.Upsert("2", raw("middle", ""))
		c.Upsert("3", raw("newest", ""))
	})
	if got := order(c); got != "newest,middle,oldest" {
		t.Errorf("order = %q, want the live edge first", got)
	}
	it, _ := c.Get("newest")
	if it.Sort != "3" {
		t.Errorf("sort key was rewritten to %q; it is opaque and must survive", it.Sort)
	}
}

func TestNoReaderSeesAHalfAppliedBatch(t *testing.T) {
	c := NewCollection[row]()
	fill(c, func() {
		for i := 0; i < 50; i++ {
			c.Upsert(fmt.Sprintf("%03d", i), raw(fmt.Sprintf("id%02d", i), ""))
		}
	})

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
			}
			c.Read(func(items []Item[row]) {
				// The window is only ever 50 or 100 items, never a count
				// from the middle of a batch.
				if n := len(items); n != 50 && n != 100 {
					panic(fmt.Sprintf("reader saw %d items mid-batch", n))
				}
			})
		}
	}()

	fill(c, func() {
		for i := 50; i < 100; i++ {
			c.Upsert(fmt.Sprintf("%03d", i), raw(fmt.Sprintf("id%02d", i), ""))
		}
	})
	close(stop)
	wg.Wait()
}

func TestAnUndecodableRowIsReportedNotSwallowed(t *testing.T) {
	c := NewCollection[row]()
	var seen int
	c.OnDecodeError = func(json.RawMessage, error) { seen++ }
	fill(c, func() { c.Upsert("1", json.RawMessage(`{"id":"a","name":42}`)) })
	if seen != 1 {
		t.Errorf("decode errors reported = %d, want 1", seen)
	}
	if c.Len() != 0 {
		t.Errorf("a row that would not decode was inserted anyway")
	}
}

func TestVersionBumpsOnEveryAppliedChange(t *testing.T) {
	c := NewCollection[row]()
	v0 := c.Version()
	fill(c, func() { c.Upsert("1", raw("a", "")) })
	v1 := c.Version()
	if v1 == v0 {
		t.Error("version did not bump on an upsert")
	}
	c.Ready(false, true)
	if c.Version() == v1 {
		t.Error("version did not bump on ready")
	}
}
