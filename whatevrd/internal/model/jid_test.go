package model

import (
	"testing"

	"go.mau.fi/whatsmeow/types"
)

func slowUser(s string) string {
	if s == "" {
		return ""
	}
	j, err := types.ParseJID(s)
	if err != nil || j.User == "" && j.Server != types.BroadcastServer {
		return ""
	}
	return j.ToNonAD().String()
}

func slowServer(s string) string {
	j, err := types.ParseJID(s)
	if err != nil {
		return ""
	}
	return j.Server
}

var jidSeeds = []string{
	"", "@", "a@", "@b", "a@b", "91999@s.whatsapp.net", "123@lid", "1-2@g.us", "status@broadcast",
	"91999:3@s.whatsapp.net", "91999.0:3@s.whatsapp.net", "91999.1@s.whatsapp.net", "a.b.c@s", "a:b@s",
	"a@b@c", "s.whatsapp.net", "a@b.c", "a@b:1", "x.y:z@s", "a::1@s",
}

func TestPlainJIDsParseTheSame(t *testing.T) {
	for _, s := range jidSeeds {
		if got, want := user(s), slowUser(s); got != want {
			t.Errorf("user(%q) = %q, want %q", s, got, want)
		}
		if got, want := server(s), slowServer(s); got != want {
			t.Errorf("server(%q) = %q, want %q", s, got, want)
		}
	}
}

func FuzzPlainJIDsParseTheSame(f *testing.F) {
	for _, s := range jidSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if got, want := user(s), slowUser(s); got != want {
			t.Errorf("user(%q) = %q, want %q", s, got, want)
		}
		if got, want := server(s), slowServer(s); got != want {
			t.Errorf("server(%q) = %q, want %q", s, got, want)
		}
	})
}
