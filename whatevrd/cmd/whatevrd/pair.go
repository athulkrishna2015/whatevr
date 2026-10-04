package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"
	"rsc.io/qr"

	"github.com/codelif/whatevr/platform"
	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/app"
)

// quietZone is the blank border the qr spec asks for, in modules.
const quietZone = 4

// runPair is `whatevrd pair`: a protocol client for the login view, so a
// headless box can link a phone without any frontend.
func runPair(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	socket := fs.String("socket", "", "whatevrd socket (default: WHATEVR_SOCKET or platform runtime directory)")
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

	if err := platform.ValidateSocket(*socket); err != nil {
		fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
		return 1
	}
	conn, err := net.Dial("unix", *socket)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			fmt.Fprintf(stderr, "whatevrd is not running on %s\nstart it: whatevrd (or enable your platform service)\n", *socket)
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
	fail := func(err error) int {
		fmt.Fprintf(stderr, "whatevrd pair: %v\n", err)
		return 1
	}
	hello := v2.Request_builder{Id: 1, Hello: v2.Hello_builder{Client: "whatevrd-pair", Protocol: 2}.Build()}.Build()
	sub := v2.Request_builder{Id: 2, Subscribe: v2.Subscribe_builder{Login: &v2.LoginView{}}.Build()}.Build()
	for _, r := range []*v2.Request{hello, sub} {
		if _, err := protodelim.MarshalTo(conn, v2.Frame_builder{Request: r}.Build()); err != nil {
			return fail(err)
		}
	}

	in := bufio.NewReader(conn)
	var subID uint64
	sawQR := false
	shown := ""
	for {
		var f v2.Frame
		if err := protodelim.UnmarshalFrom(in, &f); err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(stderr, "whatevrd pair: the daemon closed the connection")
				return 1
			}
			return fail(err)
		}
		if r := f.GetResponse(); r != nil {
			if e := r.GetError(); e != nil {
				return fail(fmt.Errorf("%s: %s", e.GetCode(), e.GetMessage()))
			}
			if r.GetId() == 2 {
				subID = r.GetSubscribe().GetSub()
			}
			continue
		}
		u := f.GetEvent().GetUpdate()
		if u == nil || subID == 0 || u.GetSub() != subID {
			continue
		}
		for _, ch := range u.GetChanges() {
			login := ch.GetUpsert().GetLogin()
			if login == nil {
				continue
			}
			switch login.GetState() {
			case v2.LoginState_LOGIN_STATE_LOGGED_IN:
				if sawQR {
					fmt.Fprintln(stdout, "linked.")
				} else {
					fmt.Fprintln(stdout, "already linked to a phone.")
				}
				return 0
			case v2.LoginState_LOGIN_STATE_QR:
			default:
				if login.GetDetail() != "" {
					fmt.Fprintln(stdout, login.GetDetail())
				}
				continue
			}
			if login.GetQr() == "" || login.GetQr() == shown {
				continue
			}
			code, err := qr.Encode(login.GetQr(), qr.L)
			if err != nil {
				return fail(fmt.Errorf("encode qr: %w", err))
			}
			shown, sawQR = login.GetQr(), true
			if tty {
				// one code on screen at a time; an old one only fails the scan
				fmt.Fprint(stdout, "\x1b[H\x1b[2J")
			}
			fmt.Fprint(stdout, halfBlocks(code))
			fmt.Fprintln(stdout, "on your phone: WhatsApp > Linked devices > Link a device, then scan this.")
			if ms := login.GetQrExpiresMs(); ms > 0 {
				fmt.Fprintf(stdout, "a new code replaces this one at %s.\n", time.UnixMilli(ms).Local().Format("15:04:05"))
			}
		}
	}
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
