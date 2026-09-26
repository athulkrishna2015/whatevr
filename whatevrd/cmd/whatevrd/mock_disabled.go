//go:build !whatevr_mock

package main

import (
	"context"
	"log"
	"os"
	"strings"

	"whatevrd/internal/app"
)

// mockRun never exists in a release binary. The mock server mutates
// process-global TLS and certificate state, so it is compiled in only under
// -tags whatevr_mock.
type mockRun struct{}

func mockPrepare() *mockRun {
	for _, arg := range os.Args[1:] {
		if arg == "--" {
			break
		}
		name := strings.TrimLeft(arg, "-")
		if name == "mock" || strings.HasPrefix(name, "mock=") || strings.HasPrefix(name, "mock-") {
			log.Fatalf("this whatevrd was built without mock support; rebuild with -tags whatevr_mock")
		}
	}
	return nil
}

func mockStart(context.Context, *mockRun, *app.Daemon) (func(), error) {
	return func() {}, nil
}

// mockSilencesNotifications is never true in a release build: there is no mock
// run to silence them for.
func mockSilencesNotifications(*mockRun) bool { return false }
