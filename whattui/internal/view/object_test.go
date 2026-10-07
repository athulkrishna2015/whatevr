package view

import (
	"testing"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

func conn(s v2.ConnectionState) *v2.ViewUpdate {
	up := v2.Upsert_builder{Connection: v2.ConnectionRow_builder{State: s}.Build()}.Build()
	return v2.ViewUpdate_builder{Changes: []*v2.Change{v2.Change_builder{Upsert: up}.Build()}, Ready: &v2.Ready{}}.Build()
}

func TestObjectHoldsTheLastItem(t *testing.T) {
	o := NewObject(Connection)
	if _, ok := o.Value(); ok || o.IsReady() {
		t.Fatal("a new object has something in it")
	}
	o.Apply(conn(v2.ConnectionState_CONNECTION_STATE_CONNECTING), false)
	o.Apply(conn(v2.ConnectionState_CONNECTION_STATE_ONLINE), false)
	c, ok := o.Value()
	if !ok || !o.IsReady() || c.GetState() != v2.ConnectionState_CONNECTION_STATE_ONLINE {
		t.Fatalf("value %v ready %v, want online", c, o.IsReady())
	}
	o.Apply(v2.ViewUpdate_builder{Changes: []*v2.Change{
		v2.Change_builder{Remove: v2.Remove_builder{Id: ""}.Build()}.Build(),
	}}.Build(), false)
	if _, ok := o.Value(); ok {
		t.Error("a remove left the item")
	}
}

func TestAFreshObjectUpdateForgetsTheOldOne(t *testing.T) {
	o := NewObject(Connection)
	o.Apply(conn(v2.ConnectionState_CONNECTION_STATE_ONLINE), false)
	o.Apply(v2.ViewUpdate_builder{}.Build(), true)
	if _, ok := o.Value(); ok || o.IsReady() {
		t.Error("a fresh update kept the previous subscription's item")
	}
}

func TestAnObjectReportsAnotherViewsRow(t *testing.T) {
	o := NewObject(Connection)
	var bad int
	o.OnMismatch = func(*v2.Upsert) { bad++ }
	up := v2.Upsert_builder{Login: &v2.LoginRow{}}.Build()
	o.Apply(v2.ViewUpdate_builder{Changes: []*v2.Change{v2.Change_builder{Upsert: up}.Build()}}.Build(), false)
	if _, ok := o.Value(); ok || bad != 1 {
		t.Errorf("took a login row as a connection, mismatches %d", bad)
	}
}
