package model

import (
	"strings"

	"go.mau.fi/whatsmeow/types"
)

// plain splits a jid that is user@server with no agent or device, which
// is nearly every jid the tables hold: parsing those is all allocation.
func plain(s string) (u, srv string, ok bool) {
	u, srv, ok = strings.Cut(s, "@")
	if !ok || u == "" || srv == "" || strings.ContainsAny(u, ".:") || strings.IndexByte(srv, '@') >= 0 {
		return "", "", false
	}
	return u, srv, true
}

// user is the jid without its device, the form every table stores. "" for
// anything that does not parse.
func user(s string) string {
	if _, _, ok := plain(s); ok {
		return s
	}
	if s == "" {
		return ""
	}
	j, err := types.ParseJID(s)
	if err != nil || j.User == "" && j.Server != types.BroadcastServer {
		return ""
	}
	return j.ToNonAD().String()
}

func server(s string) string {
	if _, srv, ok := plain(s); ok {
		return srv
	}
	j, err := types.ParseJID(s)
	if err != nil {
		return ""
	}
	return j.Server
}

func isPN(s string) bool  { return server(s) == types.DefaultUserServer }
func isLID(s string) bool { return server(s) == types.HiddenUserServer }

// isPerson is a jid a human (or bot) answers to, as opposed to a group,
// broadcast list or channel.
func isPerson(s string) bool {
	switch server(s) {
	case types.DefaultUserServer, types.HiddenUserServer, types.BotServer, types.HostedServer, types.HostedLIDServer:
		return true
	}
	return false
}

// pair sorts two addresses of one human into lid and pn, if they are that.
func pair(a, b string) (lid, pn string, ok bool) {
	a, b = user(a), user(b)
	switch {
	case isLID(a) && isPN(b):
		return a, b, true
	case isPN(a) && isLID(b):
		return b, a, true
	}
	return "", "", false
}

// Norm is an address without its device, "" for one that does not parse.
func Norm(s string) string { return user(s) }

// IsGroup says an address is a group.
func IsGroup(s string) bool { return server(s) == types.GroupServer }
