package view

import (
	"encoding/json"
	"testing"

	"whattui/internal/proto"
)

// Both view models are sinks. Without this the two packages drift apart and
// the failure shows up as a view that silently never updates.
var (
	_ proto.ViewSink = (*Collection[row])(nil)
	_ proto.ViewSink = (*Object[row])(nil)
)

func TestObjectHoldsOneItem(t *testing.T) {
	o := NewObject[row]()
	if _, ok := o.Value(); ok {
		t.Error("a fresh object view already has a value")
	}
	o.Upsert("", raw("self", "me"))
	v, ok := o.Value()
	if !ok || v.Name != "me" {
		t.Fatalf("value = %+v, %v", v, ok)
	}

	o.Upsert("", raw("self", "renamed"))
	if v, _ := o.Value(); v.Name != "renamed" {
		t.Errorf("an upsert must replace wholesale, got %+v", v)
	}

	o.Ready(false, false)
	if !o.IsReady() {
		t.Error("not ready after ready")
	}

	o.Reset()
	if _, ok := o.Value(); ok {
		t.Error("reset left a value")
	}
	if o.IsReady() {
		t.Error("reset left the view ready")
	}
}

func TestObjectReportsAnUndecodableItem(t *testing.T) {
	o := NewObject[row]()
	var seen int
	o.OnDecodeError = func(json.RawMessage, error) { seen++ }
	o.Upsert("", json.RawMessage(`{"id":"self","name":[]}`))
	if seen != 1 {
		t.Errorf("decode errors = %d, want 1", seen)
	}
	if _, ok := o.Value(); ok {
		t.Error("an item that would not decode was stored anyway")
	}
}
