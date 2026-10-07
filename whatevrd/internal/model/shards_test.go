package model

import (
	"fmt"
	"testing"
)

func TestACopyOfShardsSharesNothingItWrites(t *testing.T) {
	var a shards[string]
	for i := range 1000 {
		a.set(fmt.Sprint(i), "a")
	}
	b := a.copy()
	b.set("1", "b")
	b.del("2")
	b.set("new", "b")
	if a.get("1") != "a" || a.get("2") != "a" || a.get("new") != "" {
		t.Fatal("writing the copy changed the original")
	}
	if b.get("1") != "b" || b.get("3") != "a" || b.get("new") != "b" {
		t.Fatal("the copy lost what it had")
	}
	if _, ok := b.lookup("2"); ok {
		t.Fatal("a delete did not take")
	}
	n := 0
	for range b.all() {
		n++
	}
	if n != 1000 {
		t.Fatalf("the copy has %d keys", n)
	}
}
