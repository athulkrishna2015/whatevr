package main

import (
	"context"
	"fmt"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"github.com/urfave/cli/v3"
)

// openCommand is `whatevrd open [url]`: hands a whatevr:// link to the
// daemon, starting it if the service holds the socket. what the desktop's
// link handler runs. no url brings a frontend up.
func openCommand() *cli.Command {
	return &cli.Command{
		Name:      "open",
		Usage:     "open a whatevr:// link in a frontend, or bring one up",
		ArgsUsage: "[whatevr://...]",
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Len() > 1 {
				return usage(c, "open takes at most one link")
			}
			url := "whatevr://"
			if c.Args().Present() {
				url = c.Args().First()
			}
			if _, err := frontendCall(v2.Request_builder{LinkOpen: v2.LinkOpen_builder{Url: url}.Build()}.Build()); err != nil {
				_, stderr := outputs(c)
				fmt.Fprintf(stderr, "whatevrd open: %v\n", err)
				return code(1)
			}
			return nil
		},
	}
}
