package main

import (
	"context"

	"github.com/urfave/cli/v3"
)

func serviceCommand() *cli.Command {
	sub := func(name, use string) *cli.Command {
		return &cli.Command{
			Name:  name,
			Usage: use,
			Action: func(_ context.Context, c *cli.Command) error {
				out, stderr := outputs(c)
				return code(runService(name, out, stderr))
			},
		}
	}
	return group(&cli.Command{
		Name:  "service",
		Usage: "manage the login service that starts whatevrd on demand (macOS)",
		Commands: []*cli.Command{
			sub("enable", "install the launch agent and start on demand"),
			sub("disable", "stop and remove the launch agent"),
			sub("status", "print the launch agent's state"),
		},
	})
}
