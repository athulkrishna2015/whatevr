//go:build !whatevr_mock

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

// mockRun never exists in a release binary. The mock server mutates
// process-global TLS and certificate state, so it is compiled in only under
// -tags whatevr_mock.
type mockRun struct{}

type mockFlagSet struct{}

// mockFlags registers nothing, so flag.Parse would reject --mock with a bare
// "flag provided but not defined". Say why instead.
func mockFlags(log zerolog.Logger) *mockFlagSet {
	if usesFlag("mock") {
		log.Fatal().Msg("this whatevrd was built without mock support; rebuild with -tags whatevr_mock")
	}
	return nil
}

func mockPrepare(zerolog.Logger, *mockFlagSet) *mockRun { return nil }

func mockScenario(*mockRun) string { return "" }

func mockStart(context.Context, *mockRun, qrSource, string) (func(), error) {
	return func() {}, nil
}

// mockSilencesNotifications is never true in a release build: there is no mock
// run to silence them for.
func mockSilencesNotifications(*mockRun) bool { return false }

// mockTime is nil: the real clocks.
func mockTime(*mockRun) *mockClocks { return nil }

func heapProfiles(context.Context) {}

func runMock(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "whatevrd mock: this whatevrd was built without mock support; rebuild with -tags whatevr_mock")
	return 2
}
