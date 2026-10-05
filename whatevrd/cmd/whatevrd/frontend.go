package main

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
	"github.com/urfave/cli/v3"

	"whatevrd/internal/frontends"
)

// frontendCommand is `whatevrd frontend`: which frontends exist, which one a
// click starts, and the terminal a terminal frontend runs in.
func frontendCommand() *cli.Command {
	fail := func(c *cli.Command, err error) error {
		_, stderr := outputs(c)
		fmt.Fprintf(stderr, "whatevrd frontend: %v\n", err)
		return code(1)
	}
	list := &cli.Command{
		Name:  "list",
		Usage: "list the frontends whatevrd knows",
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Present() {
				return usage(c, "list takes no arguments")
			}
			resp, err := frontendCall(v2.Request_builder{FrontendList: &v2.FrontendList{}}.Build())
			if err != nil {
				return fail(c, err)
			}
			stdout, _ := outputs(c)
			w := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSOURCE\t")
			for _, f := range resp.GetFrontendList().GetFrontends() {
				var flags []string
				if f.GetIsDefault() {
					flags = append(flags, "default")
				}
				if f.GetConnected() {
					flags = append(flags, "connected")
				}
				if f.GetTerminal() {
					flags = append(flags, "terminal")
				}
				source := strings.ToLower(strings.TrimPrefix(f.GetSource().String(), "FRONTEND_SOURCE_"))
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", f.GetId(), f.GetName(), source, strings.Join(flags, ","))
			}
			return w.Flush()
		},
	}
	setDefault := &cli.Command{
		Name:      "set-default",
		Usage:     "pick the frontend a click or link starts",
		ArgsUsage: "<id>",
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Len() != 1 {
				return usage(c, "set-default takes one id")
			}
			if _, err := frontendCall(v2.Request_builder{FrontendSetDefault: v2.FrontendSetDefault_builder{Id: c.Args().First()}.Build()}.Build()); err != nil {
				return fail(c, err)
			}
			return nil
		},
	}
	add := &cli.Command{
		Name:      "add",
		Usage:     "write a frontend manifest for this user",
		ArgsUsage: "<id> -- <argv...>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "name", Usage: "name to show"},
			&cli.BoolFlag{Name: "terminal", Usage: "it runs in a terminal"},
		},
		Action: func(_ context.Context, c *cli.Command) error {
			args := c.Args().Slice()
			if len(args) < 2 {
				return usage(c, "add takes an id and the command to run it")
			}
			p, err := frontends.Write(frontends.DefaultDirs(), frontends.Manifest{ID: args[0], Name: c.String("name"), Exec: args[1:], Terminal: c.Bool("terminal")})
			if err != nil {
				return fail(c, err)
			}
			stdout, _ := outputs(c)
			fmt.Fprintln(stdout, p)
			return nil
		},
	}
	remove := &cli.Command{
		Name:      "remove",
		Usage:     "delete a frontend manifest written by add",
		ArgsUsage: "<id>",
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Len() != 1 {
				return usage(c, "remove takes one id")
			}
			if err := frontends.Remove(frontends.DefaultDirs(), c.Args().First()); err != nil {
				return fail(c, err)
			}
			return nil
		},
	}
	setTerminal := func(c *cli.Command, argv []string) error {
		set := v2.PreferencesSet_builder{Terminal: v2.Argv_builder{Args: argv}.Build()}.Build()
		if _, err := frontendCall(v2.Request_builder{PreferencesSet: set}.Build()); err != nil {
			return fail(c, err)
		}
		return nil
	}
	terminal := group(&cli.Command{
		Name:  "terminal",
		Usage: "set the terminal a terminal frontend runs in",
		Commands: []*cli.Command{
			{
				Name:      "set",
				Usage:     "run terminal frontends as <argv...> <frontend argv...>",
				ArgsUsage: "-- <argv...>",
				Action: func(_ context.Context, c *cli.Command) error {
					if !c.Args().Present() {
						return usage(c, "set takes the terminal's command")
					}
					return setTerminal(c, c.Args().Slice())
				},
			},
			{
				Name:  "unset",
				Usage: "go back to the platform's terminal",
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Present() {
						return usage(c, "unset takes no arguments")
					}
					return setTerminal(c, nil)
				},
			},
		},
	})
	return group(&cli.Command{
		Name:     "frontend",
		Usage:    "list frontends, pick the default and the terminal",
		Commands: []*cli.Command{list, setDefault, add, remove, terminal},
	})
}

func frontendCall(r *v2.Request) (*v2.Response, error) {
	conn, err := dialDaemon("")
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	c, err := newDaemonClient(conn, "whatevrd")
	if err != nil {
		return nil, err
	}
	return c.call(r)
}
