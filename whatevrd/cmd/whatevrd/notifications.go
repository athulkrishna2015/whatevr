package main

import (
	"context"
	"fmt"
	"io"
	"time"
	"whatevrd/internal/notify"
)

func runNotifications(args []string, out, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "setup" && args[0] != "status") {
		fmt.Fprintln(stderr, "usage: whatevrd notifications setup|status")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	status, err := notify.Configure(ctx, args[0] == "setup")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintln(out, status)
	return 0
}
