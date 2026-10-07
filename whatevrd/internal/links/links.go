// Package links reads whatevr:// links. a link only names where to go: a
// chat, a person to open a chat with, or nowhere, which brings a frontend
// up. nothing in one can send.
package links

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Schemes are the ones a link may use: the installed app's and a dev build's.
var Schemes = []string{"whatevr", "whatevr-dev"}

type Kind int

const (
	// Activate brings a frontend forward, no chat
	Activate Kind = iota
	// Chat opens a chat by its protocol id
	Chat
	// Address opens the direct chat with whoever the address names
	Address
)

type Link struct {
	Kind Kind
	// ChatID is set for Chat
	ChatID string
	// one of these is set for Address
	Phone, LID, Username string
}

const maxLen = 2048

// Parse reads raw, which has to be a whole whatevr link.
func Parse(raw string) (Link, error) {
	if len(raw) > maxLen {
		return Link{}, errors.New("link too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Link{}, err
	}
	ok := false
	for _, s := range Schemes {
		ok = ok || strings.EqualFold(u.Scheme, s)
	}
	if !ok {
		return Link{}, fmt.Errorf("not a whatevr link: %q", u.Scheme)
	}
	if u.User != nil || u.Port() != "" || u.Fragment != "" {
		return Link{}, errors.New("unexpected parts in link")
	}
	// whatevr://chat/x and whatevr:chat/x both mean the same
	host, path := u.Host, u.Path
	if u.Opaque != "" {
		host, path, _ = strings.Cut(u.Opaque, "/")
		path = "/" + path
	}
	path = strings.TrimSuffix(path, "/")
	q := u.Query()
	switch {
	case host == "" && (path == "" || path == "/") && len(q) == 0:
		return Link{Kind: Activate}, nil
	case host != "chat":
		return Link{}, fmt.Errorf("unknown link %q", host)
	case path != "":
		if len(q) != 0 {
			return Link{}, errors.New("a chat link takes an id or an address, not both")
		}
		id, err := url.PathUnescape(strings.TrimPrefix(path, "/"))
		if err != nil || id == "" || strings.Contains(id, "/") {
			return Link{}, errors.New("bad chat id")
		}
		return Link{Kind: Chat, ChatID: id}, nil
	}
	l := Link{Kind: Address}
	for k, v := range q {
		if len(v) != 1 || v[0] == "" {
			return Link{}, fmt.Errorf("bad %s", k)
		}
		switch k {
		case "phone":
			l.Phone = v[0]
		case "lid":
			l.LID = v[0]
		case "username":
			l.Username = v[0]
		default:
			return Link{}, fmt.Errorf("unknown parameter %q", k)
		}
	}
	if len(q) != 1 {
		return Link{}, errors.New("a chat link takes one of phone, lid or username")
	}
	return l, nil
}
