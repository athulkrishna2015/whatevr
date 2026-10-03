package main

import (
	"bufio"
	"bytes"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protodelim"
	"rsc.io/qr"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"
)

// reading the glyphs back must give the code's own modules, quiet zone included
func TestHalfBlocksDrawsEveryModule(t *testing.T) {
	code, err := qr.Encode("2@abc,def,ghi,jkl", qr.L)
	if err != nil {
		t.Fatal(err)
	}
	n := code.Size + 2*quietZone
	var rows []string
	for _, line := range strings.Split(strings.TrimSuffix(halfBlocks(code), "\n"), "\n") {
		line = strings.TrimPrefix(line, "\x1b[30;47m")
		rows = append(rows, strings.TrimSuffix(line, "\x1b[0m"))
	}
	if len(rows) != (n+1)/2 {
		t.Fatalf("%d rows, want %d", len(rows), (n+1)/2)
	}
	for ry, row := range rows {
		cells := []rune(row)
		if len(cells) != n {
			t.Fatalf("row %d is %d wide, want %d", ry, len(cells), n)
		}
		for x, r := range cells {
			top := r == '█' || r == '▀'
			bottom := r == '█' || r == '▄'
			y := ry * 2
			if want := code.Black(x-quietZone, y-quietZone); top != want {
				t.Fatalf("module %d,%d drawn %v, want %v", x, y, top, want)
			}
			if want := code.Black(x-quietZone, y+1-quietZone); bottom != want {
				t.Fatalf("module %d,%d drawn %v, want %v", x, y+1, bottom, want)
			}
		}
	}
}

// login is one login row as the daemon sends it
func login(state v2.LoginState, code string) *v2.LoginRow {
	return v2.LoginRow_builder{State: state, Qr: code, QrExpiresMs: time.Now().Add(time.Minute).UnixMilli()}.Build()
}

// fakeDaemon answers hello and the login subscribe, then sends rows in order
func fakeDaemon(t *testing.T, rows ...*v2.LoginRow) net.Conn {
	t.Helper()
	// a real socket: net.Pipe has no buffer, and the client writes twice
	// before it reads, exactly as it does against the daemon
	ln, err := net.Listen("unix", t.TempDir()+"/d.sock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		server, err := ln.Accept()
		if err != nil {
			return
		}
		defer server.Close()
		in := bufio.NewReader(server)
		send := func(f *v2.Frame) { protodelim.MarshalTo(server, f) }
		for {
			var f v2.Frame
			if protodelim.UnmarshalFrom(in, &f) != nil {
				return
			}
			req := f.GetRequest()
			switch {
			case req.HasHello():
				send(v2.Frame_builder{Response: v2.Response_builder{Id: req.GetId(),
					Hello: v2.HelloResult_builder{Protocol: 2}.Build()}.Build()}.Build())
			case req.HasSubscribe():
				if !req.GetSubscribe().HasLogin() {
					send(v2.Frame_builder{Response: v2.Response_builder{Id: req.GetId(),
						Error: v2.Error_builder{Code: v2.ErrorCode_ERROR_CODE_UNKNOWN_METHOD}.Build()}.Build()}.Build())
					return
				}
				send(v2.Frame_builder{Response: v2.Response_builder{Id: req.GetId(),
					Subscribe: v2.SubscribeResult_builder{Sub: 7}.Build()}.Build()}.Build())
				for _, row := range rows {
					up := v2.Upsert_builder{Id: "login", Login: row}.Build()
					send(v2.Frame_builder{Event: v2.Event_builder{Update: v2.ViewUpdate_builder{Sub: 7,
						Changes: []*v2.Change{v2.Change_builder{Upsert: up}.Build()}}.Build()}.Build()}.Build())
				}
			}
		}
	}()
	client, err := net.Dial("unix", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestPairShowsEachCodeOnceAndStopsWhenLinked(t *testing.T) {
	conn := fakeDaemon(t,
		login(v2.LoginState_LOGIN_STATE_UNSPECIFIED, ""),
		login(v2.LoginState_LOGIN_STATE_QR, "2@first"),
		login(v2.LoginState_LOGIN_STATE_QR, "2@first"),
		login(v2.LoginState_LOGIN_STATE_QR, "2@second"),
		login(v2.LoginState_LOGIN_STATE_PAIRING, ""),
		login(v2.LoginState_LOGIN_STATE_LOGGED_IN, ""),
	)
	var out, errs bytes.Buffer
	if rc := pairSession(conn, &out, &errs, true); rc != 0 {
		t.Fatalf("exit %d, stderr %q", rc, errs.String())
	}
	if n := strings.Count(out.String(), "\x1b[2J"); n != 2 {
		t.Errorf("cleared the screen %d times, want once per distinct code (2)", n)
	}
	if !strings.HasSuffix(out.String(), "linked.\n") {
		t.Errorf("did not say it linked:\n%s", out.String())
	}
}

func TestPairOnALinkedDaemonSaysSoWithoutAQR(t *testing.T) {
	var out, errs bytes.Buffer
	if rc := pairSession(fakeDaemon(t, login(v2.LoginState_LOGIN_STATE_LOGGED_IN, "")), &out, &errs, false); rc != 0 {
		t.Fatalf("exit %d, stderr %q", rc, errs.String())
	}
	if got := out.String(); got != "already linked to a phone.\n" {
		t.Errorf("got %q", got)
	}
}

func TestPairNeverClearsAScreenItIsNotWritingTo(t *testing.T) {
	conn := fakeDaemon(t,
		login(v2.LoginState_LOGIN_STATE_QR, "2@only"),
		login(v2.LoginState_LOGIN_STATE_LOGGED_IN, ""),
	)
	var out, errs bytes.Buffer
	if rc := pairSession(conn, &out, &errs, false); rc != 0 {
		t.Fatalf("exit %d, stderr %q", rc, errs.String())
	}
	if strings.Contains(out.String(), "\x1b[2J") {
		t.Error("cleared the screen of a pipe")
	}
}
