//go:build whatevr_capture

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
	waBinary "go.mau.fi/whatsmeow/binary"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"

	"whatevrd/internal/app"
	"whatevrd/internal/capture"
)

// captureCommand is `whatevrd capture`, it never touches the daemon or the account.
func captureCommand() *cli.Command {
	fail := func(c *cli.Command, err error) error {
		_, stderr := outputs(c)
		fmt.Fprintf(stderr, "whatevrd capture: %v\n", err)
		return code(1)
	}
	return group(&cli.Command{
		Name:     "capture",
		Usage:    "list or read the captures -capture records",
		Category: "debug",
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "list captures",
				Action: func(_ context.Context, c *cli.Command) error {
					stateHome, err := app.StateHome()
					if err != nil {
						return fail(c, err)
					}
					stdout, stderr := outputs(c)
					return code(listCaptures(capture.Root(stateHome), stdout, stderr))
				},
			},
			{
				Name:      "show",
				Usage:     "print a capture one record per line",
				ArgsUsage: "NAME|PATH",
				Description: `stanzas print as xml and payloads as protobuf text. it holds real
messages, read it on this machine only.`,
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "segment", Usage: "only this segment"},
					&cli.StringFlag{Name: "kind", Usage: "only these kinds, comma separated (recv,send,decrypted,encrypted,media,http,frontend,conn,account,start,end)"},
				},
				Action: func(_ context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return usage(c, "show takes one capture")
					}
					stateHome, err := app.StateHome()
					if err != nil {
						return fail(c, err)
					}
					dir, err := capture.Resolve(c.Args().First(), stateHome)
					if err != nil {
						return fail(c, err)
					}
					want := map[string]bool{}
					for _, k := range strings.Split(c.String("kind"), ",") {
						if k = strings.TrimSpace(k); k != "" {
							want[k] = true
						}
					}
					stdout, stderr := outputs(c)
					return code(showCapture(dir, c.Int("segment"), want, stdout, stderr))
				},
			},
		},
	})
}

func listCaptures(root string, stdout, stderr io.Writer) int {
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "whatevrd capture: %v\n", err)
		return 1
	}
	for _, e := range entries {
		c, err := capture.Load(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		acct := "unpaired"
		if len(c.Segments) > 0 {
			if a, err := c.Account(c.Segments[len(c.Segments)-1]); err == nil {
				acct = a.PN
			}
		}
		fmt.Fprintf(stdout, "%-24s  %s  %d segment(s)  %s\n", e.Name(), c.Meta.Created.Local().Format("2006-01-02 15:04"), len(c.Segments), acct)
	}
	return 0
}

func showCapture(dir string, only int, want map[string]bool, stdout, stderr io.Writer) int {
	c, err := capture.Load(dir)
	if err != nil {
		fmt.Fprintf(stderr, "whatevrd capture: %v\n", err)
		return 1
	}
	for _, n := range c.Segments {
		if only != 0 && n != only {
			continue
		}
		recs, err := c.Records(n)
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd capture: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "== segment %d\n", n)
		for _, r := range recs {
			if len(want) > 0 && !want[r.Kind] {
				continue
			}
			fmt.Fprintf(stdout, "%6d %s %-9s %s\n", r.Seq, r.T.Local().Format("15:04:05.000"), r.Kind, describe(r))
		}
	}
	return 0
}

func describe(r capture.Record) string {
	switch {
	case r.Frame != nil:
		node, err := waBinary.Unmarshal(r.Frame.Data)
		if err != nil {
			return fmt.Sprintf("<%s id=%s> undecodable: %v", r.Frame.Tag, r.Frame.ID, err)
		}
		return node.String()
	case r.Payload != nil && r.Kind == capture.KindDecrypted:
		return fmt.Sprintf("ref=%d child=%d enc=%s addr=%s %s", r.Payload.Ref, r.Payload.Child, r.Payload.Enc, r.Payload.Addr, messageText(r.Payload.Data))
	case r.Payload != nil:
		return fmt.Sprintf("to=%s enc=%s %s", r.Payload.To, r.Payload.Enc, messageText(r.Payload.Data))
	case r.HTTP != nil:
		h := r.HTTP
		s := fmt.Sprintf("%s %s", h.Method, h.URL)
		if h.Range != "" {
			s += " range=" + h.Range
		}
		if h.Err != "" {
			return s + " error: " + h.Err
		}
		s += fmt.Sprintf(" -> %d, %d B", h.Status, h.Size)
		if h.Blob == "" {
			s += ", body not kept"
		} else if h.Partial {
			s += ", partial"
		}
		return s
	case r.Frontend != nil:
		return fmt.Sprintf("conn=%d %s %s", r.Frontend.Conn, r.Frontend.Dir, r.Frontend.Line)
	}
	raw, _ := json.Marshal(struct {
		*capture.Start   `json:",omitempty"`
		*capture.Account `json:",omitempty"`
		*capture.Media   `json:",omitempty"`
		*capture.Conn    `json:",omitempty"`
	}{r.Start, r.Account, r.Media, r.Conn})
	return string(raw)
}

func messageText(data []byte) string {
	var msg waE2E.Message
	if err := proto.Unmarshal(data, &msg); err != nil {
		return fmt.Sprintf("%d bytes, not a Message: %v", len(data), err)
	}
	return prototext.MarshalOptions{}.Format(&msg)
}
