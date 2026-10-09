package views

import (
	"context"
	"slices"
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/live"
	"whatevrd/internal/model"
	"whatevrd/internal/server"
)

// Address is the person or chat key an address names, "" for none known.
func (rs *Reads) Address(ctx context.Context, a *v2.Address) (string, error) {
	w, err := rs.World(ctx)
	if err != nil {
		return "", err
	}
	return address(rs.ids, w, a), nil
}

func address(ids *model.IDs, w *model.World, a *v2.Address) string {
	switch a.WhichAddress() {
	case v2.Address_Id_case:
		k, _ := ids.Key(w, a.GetId())
		return k
	case v2.Address_Phone_case:
		var digits strings.Builder
		for _, r := range a.GetPhone() {
			if r >= '0' && r <= '9' {
				digits.WriteRune(r)
			}
		}
		if digits.Len() == 0 {
			return ""
		}
		return w.Now(digits.String() + "@s.whatsapp.net")
	case v2.Address_Lid_case:
		lid := strings.TrimSuffix(strings.TrimSpace(a.GetLid()), "@lid")
		if lid == "" {
			return ""
		}
		return w.Now(lid + "@lid")
	case v2.Address_Username_case:
		return w.Username(a.GetUsername())
	}
	return ""
}

// about is what key says about themselves, as last fetched.
func (rs *Reads) about(w *model.World, key string) string {
	if rs.live == nil {
		return ""
	}
	for _, a := range w.Addrs(key) {
		if ab, ok := rs.live.About(a); ok {
			return ab.Text
		}
	}
	return ""
}

func (rs *Reads) selfView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		pn := c.w.SelfPN()
		row := v2.SelfRow_builder{Phone: phone(pn), PushName: c.w.SelfName()}.Build()
		if pn != "" {
			key := c.w.Now(pn)
			about := rs.about(c.w, key)
			if ab, ok := rs.live.About("self"); ok && about == "" {
				about = ab.Text
			}
			row.SetAbout(about)
			c.wait(key, func(id, av string) { row.SetId(id); row.SetAvatarPath(av) })
		}
		it := &v2.Upsert{}
		it.SetSelf(row)
		return one(it), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, "person", live.TouchAbout, live.TouchLogin) }
	return w, nil, nil
}

func (rs *Reads) contactView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	key, err := rs.Address(ctx, req.GetContact().GetPerson())
	if err != nil {
		return nil, nil, err
	}
	if key == "" {
		return nil, nil, notFound("no such person")
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		// a number that turned out to have a lid follows it
		k := c.w.Now(key)
		row := c.contactRow(k, c.blockedSet(ctx))
		if err := c.finish(); err != nil {
			return nil, err
		}
		it := &v2.Upsert{}
		it.SetContact(row)
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "person", "appstate", "blocklist", live.TouchAbout) }
	return w, nil, nil
}

// contactsView is every saved contact: the phone's address book as whatsapp
// syncs it (appstate contact names, history-sync inline names where those
// are missing), in saved-name order. Starting a chat with one is
// chat_ensure_direct with its id.
func (rs *Reads) contactsView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		blocked := c.blockedSet(ctx)
		var out []*v2.Upsert
		for _, sc := range c.w.SavedContacts() {
			row := c.contactRow(sc.Key, blocked)
			it := &v2.Upsert{}
			c.wait(sc.Key, func(id, av string) { it.SetId(id); row.SetId(id); row.SetAvatarPath(av) })
			it.SetSort(contactSort(sc.Saved, sc.Key))
			it.SetContact(row)
			out = append(out, it)
		}
		if err := c.finish(); err != nil {
			return nil, err
		}
		return limited(sorted(out), max), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "person", "appstate", "blocklist", live.TouchAbout) }
	w.replaced = rs.replaced
	return merging{w}, nil, nil
}

// contactRow is key as a contact card row, its id and avatar arriving with
// the read's finish.
func (c *rc) contactRow(key string, blocked map[string]bool) *v2.ContactRow {
	saved, push, business := c.w.Names(key)
	row := v2.ContactRow_builder{
		Phone: phone(c.w.PN(key)), SavedName: saved, PushName: push, BusinessName: business,
		Business: business != "", About: c.Reads.about(c.w, key), Blocked: blocked[c.w.Now(key)],
	}.Build()
	c.wait(key, func(id, av string) { row.SetId(id); row.SetAvatarPath(av) })
	return row
}

// blockedSet is every person on the blocklist, folded to Now.
func (c *rc) blockedSet(ctx context.Context) map[string]bool {
	out := map[string]bool{}
	blocked, err := c.r.Blocked(ctx)
	if err != nil {
		return out
	}
	for _, b := range blocked {
		out[c.w.Now(b)] = true
	}
	return out
}

// contactSort orders saved contacts by name, then key.
func contactSort(saved, key string) []byte {
	return append(append([]byte(strings.ToLower(saved)), 0), key...)
}

func (rs *Reads) blocklistView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		blocked, err := rs.r.Blocked(ctx)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []*v2.Upsert
		for _, b := range blocked {
			k := c.w.Now(b)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, c.personItem(k, func(it *v2.Upsert, p *v2.Person) {
				it.SetBlocked(v2.BlockedRow_builder{Person: p}.Build())
			}))
		}
		return limited(sorted(out), max), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, "blocklist", "person") }
	w.replaced = rs.replaced
	return merging{w}, nil, nil
}

// personItem is an item standing for key, by name then key, with its id the
// person's.
func (c *rc) personItem(key string, fill func(it *v2.Upsert, p *v2.Person)) *v2.Upsert {
	it := &v2.Upsert{}
	p := c.person(key)
	c.wait(key, func(id, _ string) { it.SetId(id) })
	it.SetSort(append(append([]byte(strings.ToLower(strings.TrimPrefix(p.GetName(), "~"))), 0), key...))
	fill(it, p)
	return it
}

// sorted is items in their sort order.
func sorted(items []*v2.Upsert) []*v2.Upsert {
	slices.SortFunc(items, func(a, b *v2.Upsert) int { return strings.Compare(string(a.GetSort()), string(b.GetSort())) })
	return items
}

func (rs *Reads) typingView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		byKey := map[string][]live.Typist{}
		for chat, ts := range rs.live.Typing() {
			k := c.w.Now(model.Norm(chat))
			byKey[k] = append(byKey[k], ts...)
		}
		var out []*v2.Upsert
		for k, ts := range byKey {
			slices.SortFunc(ts, func(a, b live.Typist) int { return a.Since.Compare(b.Since) })
			row := &v2.TypingRow{}
			it := &v2.Upsert{}
			c.wait(k, func(id, _ string) { it.SetId(id); row.SetChatId(id) })
			var typists []*v2.Typist
			for _, t := range ts {
				typists = append(typists, v2.Typist_builder{Person: c.now(t.Addr), Recording: t.Recording}.Build())
			}
			row.SetTypists(typists)
			it.SetSort([]byte(k))
			it.SetTyping(row)
			out = append(out, it)
		}
		return limited(sorted(out), max), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchTyping, "person") }
	return w, nil, nil
}

func (rs *Reads) presenceView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetPresence().GetChatId()
	if _, err := rs.chatByID(ctx, id); err != nil {
		return nil, nil, err
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		ch, ok, err := c.chat(id)
		if err != nil || !ok {
			return nil, err
		}
		people := []string{ch.Key}
		if ch.Group {
			people = people[:0]
			ms, err := rs.r.Members(ctx, ch.Key)
			if err != nil {
				return nil, err
			}
			for _, m := range ms {
				if m.In && !c.w.IsSelf(m.JID) {
					people = append(people, c.w.Now(m.JID))
				}
			}
		}
		var out []*v2.Upsert
		seen := map[string]bool{}
		for _, k := range people {
			if seen[k] {
				continue
			}
			seen[k] = true
			pr, ok := rs.presence(c.w, k)
			out = append(out, c.personItem(k, func(it *v2.Upsert, p *v2.Person) {
				row := v2.PresenceRow_builder{Person: p}.Build()
				if ok {
					row.SetAvailability(v2.Availability_AVAILABILITY_OFFLINE)
					if pr.Online {
						row.SetAvailability(v2.Availability_AVAILABILITY_ONLINE)
					}
					row.SetLastSeenMs(ms(pr.LastSeen))
				}
				it.SetPresence(row)
			}))
		}
		return limited(sorted(out), max), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, live.TouchPresence, "person", "group") }
	w.replaced = rs.replaced
	return merging{w}, nil, nil
}

// presence is key's newest presence under any of its addresses.
func (rs *Reads) presence(w *model.World, key string) (live.Presence, bool) {
	var best live.Presence
	found := false
	for _, a := range w.Addrs(key) {
		if p, ok := rs.live.Presence(a); ok && (!found || p.At.After(best.At)) {
			best, found = p, true
		}
	}
	return best, found
}

func (rs *Reads) groupView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetGroup().GetChatId()
	ch, err := rs.chatByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ch.Group {
		return nil, nil, invalid("chat %q is not a group", id)
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		g, _, err := rs.r.Group(ctx, ch.Key)
		if err != nil {
			return nil, err
		}
		members, err := rs.r.Members(ctx, ch.Key)
		if err != nil {
			return nil, err
		}
		row := v2.GroupRow_builder{Subject: g.Name, Description: g.Topic, CreatedMs: toMS(g.Created),
			Announce: g.Announce, Locked: g.Locked, Approval: g.Approval, Error: g.Error}.Build()
		if g.Name == "" {
			row.SetSubject(ch.Name)
		}
		if g.Owner != "" {
			row.SetOwner(c.at(g.Owner, g.Created))
		}
		n := 0
		role := v2.GroupRole_GROUP_ROLE_LEFT
		for _, m := range members {
			if !m.In {
				continue
			}
			n++
			if c.w.IsSelf(m.JID) {
				role = memberRole(m)
			}
		}
		row.SetMemberCount(uint32(n))
		row.SetMyRole(role)
		c.wait(ch.Key, func(id, av string) { row.SetChatId(id); row.SetAvatarPath(av) })
		if g.LinkedTo != "" {
			c.wait(model.Norm(g.LinkedTo), func(id, _ string) { row.SetCommunityId(id) })
		}
		it := &v2.Upsert{}
		it.SetGroup(row)
		return one(it), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, "group", "person", "chatrow") }
	return w, nil, nil
}

func memberRole(m model.Member) v2.GroupRole {
	switch {
	case m.Super:
		return v2.GroupRole_GROUP_ROLE_SUPERADMIN
	case m.Admin:
		return v2.GroupRole_GROUP_ROLE_ADMIN
	}
	return v2.GroupRole_GROUP_ROLE_MEMBER
}

func (rs *Reads) groupMembersView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	id := req.GetGroupMembers().GetChatId()
	ch, err := rs.chatByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if !ch.Group {
		return nil, nil, invalid("chat %q is not a group", id)
	}
	w := &win{params: req}
	w.items = func(ctx context.Context, max int) ([]*v2.Upsert, error) {
		c, err := rs.begin(ctx)
		if err != nil {
			return nil, err
		}
		members, err := rs.r.Members(ctx, ch.Key)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		var out []*v2.Upsert
		for _, m := range members {
			k := c.w.Now(m.JID)
			if !m.In || seen[k] {
				continue
			}
			seen[k] = true
			role := memberRole(m)
			out = append(out, c.personItem(k, func(it *v2.Upsert, p *v2.Person) {
				// admins first, then by name
				it.SetSort(append([]byte{byte(3 - role)}, it.GetSort()...))
				it.SetGroupMember(v2.GroupMemberRow_builder{Person: p, Role: role}.Build())
			}))
		}
		return limited(sorted(out), max), c.finish()
	}
	w.wake = func(c core.Change) bool { return touches(c, "group", "person") }
	w.replaced = rs.replaced
	return merging{w}, nil, nil
}
