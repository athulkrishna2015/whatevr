// Package model is what the daemon knows, folded from the core input log:
// who is who, every message and what was said about it, app state, groups and
// history. folds live here, beside the reads that turn their rows into what
// the protocol serves.
package model

import "whatevrd/internal/core"

// Domains is every domain, in the order each input is folded through them:
// app state and identity before messages, which consult both. the chat list
// sums up the rest, so it goes last.
func Domains() []core.Domain {
	return []core.Domain{identityDomain, appStateDomain, historyDomain, groupsDomain, accountDomain, messagesDomain, outboxDomain, localDomain, syncDomain, chatlistDomain}
}
