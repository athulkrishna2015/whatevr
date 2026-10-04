package conn

/*
#include <stdint.h>
*/
import "C"

import "runtime/cgo"

//export nativeGoNetworkEvent
func nativeGoNetworkEvent(handle C.uintptr_t, up C.int) {
	n := cgo.Handle(handle).Value().(*darwinNetwork)
	n.up.Store(up != 0)
	select {
	case n.events <- struct{}{}:
	default:
	}
}

//export nativeGoWakeEvent
func nativeGoWakeEvent(handle C.uintptr_t) {
	events := cgo.Handle(handle).Value().(chan struct{})
	select {
	case events <- struct{}{}:
	default:
	}
}
