// Command wa-testsend drives the daemon's development-only `dev.send_raw`
// command, which builds a waE2E message of a given kind and pushes it through
// the real receive path.
//
// Section 1 of feature-gap.md is about receiving message types. Verifying that
// work means something has to send them, and reaching for a second handset on
// every iteration is not a development loop. This tool is that loop.
//
//	# render a location as if the peer had just sent one, no network at all
//	wa-testsend -chat 917060029183@s.whatsapp.net -local location \
//	    '{"lat":12.9716,"lng":77.5946,"name":"Cafe Noir","address":"12 MG Road"}'
//
//	# really send it, so the peer's phone gets it too
//	wa-testsend -chat 917060029183@s.whatsapp.net location '{"lat":12.9716,"lng":77.5946}'
//
// The daemon must be running with WHATEVR_DEV_COMMANDS=1; without it the
// command is not registered and this exits with "unknown method".
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type request struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *protocolError  `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func main() {
	chat := flag.String("chat", "", "chat id to send into, e.g. 917060029183@s.whatsapp.net or 1203...@g.us")
	socketPath := flag.String("socket", defaultSocket(), "daemon socket path")
	local := flag.Bool("local", false, "inject as an inbound message without touching the network")
	incoming := flag.Bool("incoming", false, "ingest a real send as inbound rather than outbound")
	flag.Usage = usage
	flag.Parse()

	if *chat == "" || flag.NArg() < 1 {
		usage()
		os.Exit(2)
	}
	kind := flag.Arg(0)
	params := json.RawMessage("{}")
	if flag.NArg() > 1 {
		raw := flag.Arg(1)
		if !json.Valid([]byte(raw)) {
			fatalf("params are not valid JSON: %s", raw)
		}
		params = json.RawMessage(raw)
	}

	conn, err := net.DialTimeout("unix", *socketPath, 5*time.Second)
	if err != nil {
		fatalf("dial %s: %v", *socketPath, err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)
	encoder := json.NewEncoder(conn)

	send := func(req request) response {
		if err := encoder.Encode(req); err != nil {
			fatalf("write request: %v", err)
		}
		// Skip any events that arrive between our request and its response;
		// only responses carry our id.
		for {
			line, err := reader.ReadBytes('\n')
			if err != nil {
				fatalf("read response: %v", err)
			}
			var resp response
			if err := json.Unmarshal(line, &resp); err != nil {
				continue
			}
			if resp.ID == req.ID {
				return resp
			}
		}
	}

	hello := send(request{ID: 1, Method: "hello", Params: map[string]any{
		"client": "wa-testsend", "protocol": 1,
	}})
	if hello.Error != nil {
		fatalf("hello: %s: %s", hello.Error.Code, hello.Error.Message)
	}

	resp := send(request{ID: 2, Method: "dev.send_raw", Params: map[string]any{
		"chat_id":  *chat,
		"kind":     kind,
		"params":   params,
		"local":    *local,
		"incoming": *incoming,
	}})
	if resp.Error != nil {
		if resp.Error.Code == "unknown_method" {
			fatalf("the daemon has no dev.send_raw: restart it with WHATEVR_DEV_COMMANDS=1")
		}
		fatalf("dev.send_raw: %s: %s", resp.Error.Code, resp.Error.Message)
	}

	var result struct {
		MessageID string `json:"message_id"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		fatalf("decode result: %v", err)
	}
	fmt.Println(result.MessageID)
}

func defaultSocket() string {
	if explicit := strings.TrimSpace(os.Getenv("WHATEVR_SOCKET")); explicit != "" {
		return explicit
	}
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "whatevr", "whatevrd.sock")
}

func usage() {
	fmt.Fprintf(os.Stderr, `wa-testsend -chat <chat_id> [flags] <kind> [params-json]

Builds one message of <kind> and pushes it through the daemon's real receive
path. Requires a daemon started with WHATEVR_DEV_COMMANDS=1.

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
The set of kinds is whatever the daemon's builder table knows; ask it for one it
does not have and the error lists them all.
`)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "wa-testsend: "+format+"\n", args...)
	os.Exit(1)
}
