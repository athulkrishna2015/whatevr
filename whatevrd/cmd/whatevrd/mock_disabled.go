//go:build !whatevr_mock

package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"

	"whatevrd/internal/model"
)

// mockRun never exists in a release binary. The mock server mutates
// process-global TLS and certificate state, so it is compiled in only under
// -tags whatevr_mock.
type mockRun struct{}

type mockFlagSet struct{}

func mockFlags() (*mockFlagSet, []cli.Flag) { return nil, nil }

// mockUnbuilt says why --mock is refused, where urfave would only say
// "flag provided but not defined".
func mockUnbuilt(args []string) string {
	if usesFlag(args, "mock") {
		return "this whatevrd was built without mock support; rebuild with -tags whatevr_mock"
	}
	return ""
}

func mockPrepare(zerolog.Logger, *mockFlagSet) *mockRun { return nil }

func mockScenario(*mockRun) string { return "" }

func mockStart(context.Context, *mockRun, qrSource, string) (func(), error) {
	return func() {}, nil
}

// mockSilencesNotifications is never true in a release build: there is no mock
// run to silence them for.
func mockSilencesNotifications(*mockRun) bool { return false }

func mockIDs(*mockRun, *model.IDs) {}

// mockTime is nil: the real clocks.
func mockTime(*mockRun) *mockClocks { return nil }

func heapProfiles(context.Context) {}

// mockCommand is hidden here, it only says what to rebuild with.
func mockCommand() *cli.Command {
	return &cli.Command{
		Name:            "mock",
		Usage:           "mock daemon probes",
		Category:        "debug",
		Hidden:          true,
		SkipFlagParsing: true,
		Action: func(_ context.Context, c *cli.Command) error {
			fmt.Fprintln(c.Root().ErrWriter, "whatevrd mock: this whatevrd was built without mock support; rebuild with -tags whatevr_mock")
			return code(1)
		},
	}
}

const mockPathsSupported = false
