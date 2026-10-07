//go:build whatevr_mock

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"
)

// heapProfiles writes a heap profile into $WHATEVR_PROFILE_DIR every 15 s
// until ctx ends, for measuring a replay. mock builds only.
func heapProfiles(ctx context.Context) {
	dir := os.Getenv("WHATEVR_PROFILE_DIR")
	if dir == "" {
		return
	}
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for n := 0; ; n++ {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("heap-%d-%04d.pb.gz", os.Getpid(), n)))
			if err != nil {
				continue
			}
			pprof.Lookup("heap").WriteTo(f, 0)
			f.Close()
		}
	}()
}
