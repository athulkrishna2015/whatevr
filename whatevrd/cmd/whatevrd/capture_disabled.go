//go:build !whatevr_capture

package main

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/urfave/cli/v3"
)

// captures hold every message in plaintext, so a shipped binary cannot make
// one: the code is only built with -tags whatevr_capture.

type captureFlagSet struct{}

type captureRun struct{}

func captureFlags() (*captureFlagSet, []cli.Flag) { return nil, nil }

func captureUnbuilt(args []string) string {
	if usesFlag(args, "capture") || usesFlag(args, "send-guard") {
		return "this whatevrd was built without capture support; rebuild with -tags whatevr_capture"
	}
	return ""
}

func capturePrepare(zerolog.Logger, *captureFlagSet, string) *captureRun { return nil }

func captureStart(context.Context, *captureRun, string) (clientHooks, func()) {
	return clientHooks{}, func() {}
}

func captureTap(*captureRun) tap { return nil }

// captureCommand is hidden here, it only says what to rebuild with.
func captureCommand() *cli.Command {
	return &cli.Command{
		Name:            "capture",
		Usage:           "read a capture",
		Category:        "debug",
		Hidden:          true,
		SkipFlagParsing: true,
		Action: func(_ context.Context, c *cli.Command) error {
			fmt.Fprintln(c.Root().ErrWriter, "whatevrd capture: this whatevrd was built without capture support; rebuild with -tags whatevr_capture")
			return code(1)
		},
	}
}
