package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"

	"rsc.io/qr"
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

// fakeDaemon answers hello and the login subscribe, then sends items in order
func fakeDaemon(t *testing.T, items ...map[string]any) net.Conn {
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
		in := bufio.NewScanner(server)
		out := json.NewEncoder(server)
		for in.Scan() {
			var req struct {
				ID     int            `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if json.Unmarshal(in.Bytes(), &req) != nil {
				return
			}
			switch req.Method {
			case "hello":
				out.Encode(map[string]any{"id": req.ID, "result": map[string]any{"protocol": 1}})
			case "subscribe":
				if req.Params["view"] != "login" {
					out.Encode(map[string]any{"id": req.ID, "error": map[string]any{"code": "not_found", "message": "no view"}})
					return
				}
				out.Encode(map[string]any{"id": req.ID, "result": map[string]any{"sub": 7}})
				for _, item := range items {
					item["id"] = "self"
					out.Encode(map[string]any{"sub": 7, "event": "upsert", "sort": "0", "item": item})
				}
				out.Encode(map[string]any{"sub": 7, "event": "ready"})
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
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	conn := fakeDaemon(t,
		map[string]any{"state": "starting"},
		map[string]any{"state": "need_login", "qr": map[string]any{"code": "2@first", "expires_at": expires}},
		map[string]any{"state": "need_login", "qr": map[string]any{"code": "2@first", "expires_at": expires}},
		map[string]any{"state": "need_login", "qr": map[string]any{"code": "2@second", "expires_at": expires}},
		map[string]any{"state": "connecting"},
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
	if rc := pairSession(fakeDaemon(t, map[string]any{"state": "online"}), &out, &errs, false); rc != 0 {
		t.Fatalf("exit %d, stderr %q", rc, errs.String())
	}
	if got := out.String(); got != "already linked to a phone.\n" {
		t.Errorf("got %q", got)
	}
}

func TestPairNeverClearsAScreenItIsNotWritingTo(t *testing.T) {
	expires := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	conn := fakeDaemon(t,
		map[string]any{"state": "need_login", "qr": map[string]any{"code": "2@only", "expires_at": expires}},
		map[string]any{"state": "online"},
	)
	var out, errs bytes.Buffer
	if rc := pairSession(conn, &out, &errs, false); rc != 0 {
		t.Fatalf("exit %d, stderr %q", rc, errs.String())
	}
	if strings.Contains(out.String(), "\x1b[2J") {
		t.Error("cleared the screen of a pipe")
	}
}
