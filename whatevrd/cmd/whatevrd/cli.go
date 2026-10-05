package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/urfave/cli/v3"
)

func init() {
	cli.VersionFlag = &cli.BoolFlag{Name: "version", Usage: "print the version", Local: true}
}

// exitCode is how an action that already said what went wrong sets the status.
type exitCode int

func (e exitCode) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func code(n int) error {
	if n == 0 {
		return nil
	}
	return exitCode(n)
}

// usageError is one urfave already printed, help included.
type usageError struct{ error }

func (e usageError) Unwrap() error { return e.error }

func outputs(c *cli.Command) (stdout, stderr io.Writer) {
	return c.Root().Writer, c.Root().ErrWriter
}

// usage prints what urfave prints for a bad flag, for bad positionals.
func usage(c *cli.Command, msg string) error {
	showUsage(c, msg)
	return usageError{errors.New(msg)}
}

func showUsage(c *cli.Command, msg string) {
	fmt.Fprintf(c.Root().ErrWriter, "Incorrect Usage: %s\n\n", msg)
	if c.Root() == c {
		_ = cli.ShowRootCommandHelp(c)
	} else {
		_ = cli.ShowSubcommandHelp(c)
	}
}

func newRoot(stdout, stderr io.Writer) *cli.Command {
	mock, mockFlagList := mockFlags()
	capture, captureFlagList := captureFlags()
	root := &cli.Command{
		Name:                  "whatevrd",
		Usage:                 "whatsapp daemon behind whattui and other whatevr frontends",
		UsageText:             "whatevrd [flags]\nwhatevrd command [flags] [args]",
		Description:           "with no command it runs the daemon on the platform socket.",
		Version:               version,
		Writer:                stdout,
		ErrWriter:             stderr,
		EnableShellCompletion: true,
		ConfigureShellCompletionCommand: func(c *cli.Command) {
			c.Hidden = false
			c.Usage = "print a bash, zsh, fish or powershell completion script"
			setUsageErrors(c)
		},
		Flags: append(mockFlagList, captureFlagList...),
		Commands: []*cli.Command{
			pathsCommand(),
			serviceCommand(),
			notificationsCommand(),
			pairCommand(),
			frontendCommand(),
			openCommand(),
			logsCommand(),
			versionCommand(),
			captureCommand(),
			mockCommand(),
			rederiveCommand(),
		},
		Action: func(_ context.Context, c *cli.Command) error {
			if c.Args().Present() {
				return usage(c, fmt.Sprintf("no command %q", c.Args().First()))
			}
			runDaemon(mock, capture)
			return nil
		},
		// the actions print their own errors, urfave must not exit for them
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	setUsageErrors(root)
	return root
}

func setUsageErrors(c *cli.Command) {
	c.OnUsageError = func(_ context.Context, c *cli.Command, err error, _ bool) error {
		showUsage(c, err.Error())
		return usageError{err}
	}
	for _, sub := range c.Commands {
		setUsageErrors(sub)
	}
}

// run is the whole command line, args[0] included. what main exits with.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 && strings.HasPrefix(args[1], "-") {
		for _, msg := range []string{mockUnbuilt(args[1:]), captureUnbuilt(args[1:])} {
			if msg != "" {
				fmt.Fprintf(stderr, "whatevrd: %s\n", msg)
				return 1
			}
		}
	}
	err := newRoot(stdout, stderr).Run(context.Background(), args)
	var exit exitCode
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return int(exit)
	case errors.As(err, new(usageError)):
		return 1
	}
	fmt.Fprintf(stderr, "whatevrd: %v\n", err)
	return 1
}

func versionCommand() *cli.Command {
	return &cli.Command{
		Name:  "version",
		Usage: "print the version",
		Action: func(_ context.Context, c *cli.Command) error {
			cli.ShowVersion(c.Root())
			return nil
		},
	}
}

// group is a command that only holds subcommands: a missing or unknown one
// is a usage error, not a silent help screen.
func group(c *cli.Command) *cli.Command {
	c.Action = func(_ context.Context, c *cli.Command) error {
		if !c.Args().Present() {
			return usage(c, "missing command")
		}
		return usage(c, fmt.Sprintf("no command %q", c.Args().First()))
	}
	return c
}
