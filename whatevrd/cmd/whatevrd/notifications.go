package main

import (
	"context"
	"fmt"
	"time"

	"github.com/urfave/cli/v3"

	"whatevrd/internal/notify"
)

func notificationsCommand() *cli.Command {
	sub := func(name, use string) *cli.Command {
		return &cli.Command{
			Name:  name,
			Usage: use,
			Action: func(_ context.Context, c *cli.Command) error {
				out, stderr := outputs(c)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				status, err := notify.Configure(ctx, name == "setup")
				if err != nil {
					fmt.Fprintln(stderr, err)
					return code(1)
				}
				fmt.Fprintln(out, status)
				return nil
			},
		}
	}
	return group(&cli.Command{
		Name:     "notifications",
		Usage:    "set up desktop notifications or check on them",
		Commands: []*cli.Command{sub("setup", "ask for permission to notify"), sub("status", "print whether notifications can show")},
	})
}
