package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/frontends"
)

const frontendUsage = `usage:
  whatevrd frontend list
  whatevrd frontend set-default <id>
  whatevrd frontend add [--name NAME] [--terminal] <id> -- <argv...>
  whatevrd frontend remove <id>
  whatevrd frontend terminal set -- <argv...>
  whatevrd frontend terminal unset
`

// runFrontend is `whatevrd frontend`: which frontends exist, which one a
// click starts, and the terminal a terminal frontend runs in.
func runFrontend(args []string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "whatevrd frontend: %v\n", err)
		return 1
	}
	if len(args) == 0 {
		fmt.Fprint(stderr, frontendUsage)
		return 2
	}
	switch args[0] {
	case "list":
		resp, err := frontendCall(v2.Request_builder{FrontendList: &v2.FrontendList{}}.Build())
		if err != nil {
			return fail(err)
		}
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
		w.Flush()
		return 0
	case "set-default":
		if len(args) != 2 {
			fmt.Fprint(stderr, frontendUsage)
			return 2
		}
		if _, err := frontendCall(v2.Request_builder{FrontendSetDefault: v2.FrontendSetDefault_builder{Id: args[1]}.Build()}.Build()); err != nil {
			return fail(err)
		}
		return 0
	case "add":
		fs := flag.NewFlagSet("frontend add", flag.ContinueOnError)
		fs.SetOutput(stderr)
		name := fs.String("name", "", "name to show")
		terminal := fs.Bool("terminal", false, "it runs in a terminal")
		if err := fs.Parse(args[1:]); err != nil {
			return 2
		}
		rest := fs.Args()
		if len(rest) < 3 || rest[1] != "--" {
			fmt.Fprint(stderr, frontendUsage)
			return 2
		}
		p, err := frontends.Write(frontends.DefaultDirs(), frontends.Manifest{ID: rest[0], Name: *name, Exec: rest[2:], Terminal: *terminal})
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, p)
		return 0
	case "remove":
		if len(args) != 2 {
			fmt.Fprint(stderr, frontendUsage)
			return 2
		}
		if err := frontends.Remove(frontends.DefaultDirs(), args[1]); err != nil {
			return fail(err)
		}
		return 0
	case "terminal":
		var argv []string
		switch {
		case len(args) >= 4 && args[1] == "set" && args[2] == "--":
			argv = args[3:]
		case len(args) == 2 && args[1] == "unset":
		default:
			fmt.Fprint(stderr, frontendUsage)
			return 2
		}
		set := v2.PreferencesSet_builder{Terminal: v2.Argv_builder{Args: argv}.Build()}.Build()
		if _, err := frontendCall(v2.Request_builder{PreferencesSet: set}.Build()); err != nil {
			return fail(err)
		}
		return 0
	}
	fmt.Fprint(stderr, frontendUsage)
	return 2
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
