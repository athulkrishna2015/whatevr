package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"rsc.io/qr"

	"whatevrd/internal/app"
)

// quietZone is the blank border the qr spec asks for, in modules.
const quietZone = 4

type pairLogin struct {
	State  string `json:"state"`
	Detail string `json:"detail"`
	QR     *struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	} `json:"qr"`
}

type pairFrame struct {
	ID     *json.RawMessage `json:"id"`
	Result json.RawMessage  `json:"result"`
	Error  *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Sub   int64           `json:"sub"`
	Event string          `json:"event"`
	Item  json.RawMessage `json:"item"`
}

// runPair is `whatevrd pair`: a protocol client for the login view, so a
// headless box can link a phone without any frontend.
func runPair(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", "", "whatevrd socket (default: $XDG_RUNTIME_DIR/whatevr/whatevrd.sock)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *socket == "" {
		paths, err := app.ResolvePaths()
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
			return 1
		}
		*socket = paths.SocketPath
	}

	conn, err := net.Dial("unix", *socket)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Fprintf(stderr, "whatevrd is not running on %s\nstart it: systemctl --user start whatevrd\n", *socket)
		} else {
			fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
		}
		return 1
	}
	defer conn.Close()

	tty := false
	if f, ok := stdout.(*os.File); ok {
		if st, err := f.Stat(); err == nil {
			tty = st.Mode()&os.ModeCharDevice != 0
		}
	}
	return pairSession(conn, stdout, stderr, tty)
}

func pairSession(conn io.ReadWriter, stdout, stderr io.Writer, tty bool) int {
	send := func(id int, method string, params any) error {
		line, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
		if err != nil {
			return err
		}
		_, err = conn.Write(append(line, '\n'))
		return err
	}
	if err := send(1, "hello", map[string]any{"client": "whatevrd-pair", "protocol": 1}); err != nil {
		fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
		return 1
	}
	if err := send(2, "subscribe", map[string]any{"view": "login"}); err != nil {
		fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
		return 1
	}

	lines := bufio.NewScanner(conn)
	lines.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var sub int64
	sawQR := false
	shown := ""
	for lines.Scan() {
		var f pairFrame
		if err := json.Unmarshal(lines.Bytes(), &f); err != nil {
			fmt.Fprintf(stderr, "whatevrd pair: bad frame from daemon: %v\n", err)
			return 1
		}
		if f.ID != nil {
			if f.Error != nil {
				fmt.Fprintf(stderr, "whatevrd pair: %s: %s\n", f.Error.Code, f.Error.Message)
				return 1
			}
			var r struct {
				Sub int64 `json:"sub"`
			}
			if json.Unmarshal(f.Result, &r) == nil && r.Sub != 0 {
				sub = r.Sub
			}
			continue
		}
		if f.Event != "upsert" || sub == 0 || f.Sub != sub {
			continue
		}
		var login pairLogin
		if err := json.Unmarshal(f.Item, &login); err != nil {
			fmt.Fprintf(stderr, "whatevrd pair: bad login item: %v\n", err)
			return 1
		}
		switch login.State {
		case "starting":
			continue
		case "need_login":
		default:
			// anything past need_login means an account is behind the daemon
			if sawQR {
				fmt.Fprintln(stdout, "linked.")
			} else {
				fmt.Fprintln(stdout, "already linked to a phone.")
			}
			return 0
		}
		if login.QR == nil || login.QR.Code == "" || login.QR.Code == shown {
			if login.Detail != "" && login.QR == nil {
				fmt.Fprintln(stdout, login.Detail)
			}
			continue
		}
		code, err := qr.Encode(login.QR.Code, qr.L)
		if err != nil {
			fmt.Fprintf(stderr, "whatevrd pair: encode qr: %v\n", err)
			return 1
		}
		shown, sawQR = login.QR.Code, true
		if tty {
			// one code on screen at a time; an old one only fails the scan
			fmt.Fprint(stdout, "\x1b[H\x1b[2J")
		}
		fmt.Fprint(stdout, halfBlocks(code))
		fmt.Fprintln(stdout, "on your phone: WhatsApp > Linked devices > Link a device, then scan this.")
		if !login.QR.ExpiresAt.IsZero() {
			fmt.Fprintf(stdout, "a new code replaces this one at %s.\n", login.QR.ExpiresAt.Local().Format("15:04:05"))
		}
	}
	if err := lines.Err(); err != nil {
		fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
	} else {
		fmt.Fprintln(stderr, "whatevrd pair: the daemon closed the connection")
	}
	return 1
}

// halfBlocks draws two module rows per text row, dark on light whatever the
// terminal's own colours are, so a phone can read it off a dark theme.
func halfBlocks(code *qr.Code) string {
	var b strings.Builder
	n := code.Size + 2*quietZone
	dark := func(x, y int) bool { return code.Black(x-quietZone, y-quietZone) }
	for y := 0; y < n; y += 2 {
		b.WriteString("\x1b[30;47m")
		for x := 0; x < n; x++ {
			top, bottom := dark(x, y), y+1 < n && dark(x, y+1)
			switch {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}
