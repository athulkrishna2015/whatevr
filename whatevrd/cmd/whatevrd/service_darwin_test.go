package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func TestServicePlistEscapesPaths(t *testing.T) {
	p := servicePlist("/Users/A & B/tool/whatevrd", "/var/folders/x/T/in.codelif.whatevr.sock", "/Users/A & B/Logs", "/Users/A & B/bin:/usr/bin")
	decoder := xml.NewDecoder(bytes.NewReader(p))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !strings.Contains(string(p), "A &amp; B") || strings.Contains(string(p), "XDG_RUNTIME_DIR") {
		t.Fatal("service must escape paths and use native defaults")
	}
	if !strings.Contains(string(p), "<key>SockPathName</key><string>/var/folders/x/T/in.codelif.whatevr.sock</string>") ||
		!strings.Contains(string(p), "<key>SockPathMode</key><integer>384</integer>") {
		t.Fatal("service must hold the socket for launchd activation")
	}
}
