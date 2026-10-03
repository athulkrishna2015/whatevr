//go:build !whatevr_capture

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/rs/zerolog"
)

// captures hold every message in plaintext, so a shipped binary cannot make
// one: the code is only built with -tags whatevr_capture.

type captureFlagSet struct{}

type captureRun struct{}

func captureFlags(log zerolog.Logger) *captureFlagSet {
	if usesFlag("capture") || usesFlag("send-guard") {
		log.Fatal().Msg("this whatevrd was built without capture support; rebuild with -tags whatevr_capture")
	}
	return nil
}

func capturePrepare(zerolog.Logger, *captureFlagSet, string) *captureRun { return nil }

func captureStart(context.Context, *captureRun, string) (clientHooks, func()) {
	return clientHooks{}, func() {}
}

func captureTap(*captureRun) tap { return nil }

func runCapture(_ []string, _, stderr io.Writer) int {
	fmt.Fprintln(stderr, "whatevrd capture: this whatevrd was built without capture support; rebuild with -tags whatevr_capture")
	return 2
}
