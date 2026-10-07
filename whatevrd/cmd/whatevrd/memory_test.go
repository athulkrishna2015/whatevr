package main

import (
	"runtime"
	"sync"
	"testing"
)

func TestMallocArenasStayCapped(t *testing.T) {
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			runtime.LockOSThread()
			mallocChurn(20000)
		})
	}
	wg.Wait()
	if n := mallocArenas(); n > 2 {
		t.Fatalf("%d malloc arenas, want at most 2", n)
	}
}
