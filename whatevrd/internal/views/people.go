package views

import (
	"context"
	"strings"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
	"whatevrd/internal/whatsapp"
)

// rc is one read: the world it sees, and the people its rows name, who get
// their ids and avatars in one go when the rows are built.
type rc struct {
	*Reads
	ctx context.Context
	w   *model.World
	dec *whatsapp.Decoder

	later []later
	// shown is the addresses of everyone the rows named, "self" for us
	shown map[string]bool
}

// later is a row field that waits on a key's id and avatar.
type later struct {
	key string
	set func(id, avatar string)
}

func (rs *Reads) begin(ctx context.Context) (*rc, error) {
	w, err := rs.World(ctx)
	if err != nil {
		return nil, err
	}
	return &rc{Reads: rs, ctx: ctx, w: w}, nil
}

func (c *rc) decoder() *whatsapp.Decoder {
	if c.dec == nil {
		c.dec = whatsapp.NewDecoder(whatsapp.WorldNames(c.w), c.media)
	}
	return c.dec
}

// wait has set called with key's id and avatar once the rows are built.
func (c *rc) wait(key string, set func(id, avatar string)) {
	if key != "" {
		c.later = append(c.later, later{key: key, set: set})
	}
}

// finish gives every key the rows named an id, reads their avatars in one
// go, and fills them in.
func (c *rc) finish() error {
	if len(c.later) == 0 {
		return nil
	}
	keys := make([]string, 0, len(c.later))
	seen := map[string]bool{}
	var addrs []string
	for _, l := range c.later {
		if !seen[l.key] {
			seen[l.key] = true
			keys = append(keys, l.key)
			addrs = append(addrs, c.w.Addrs(l.key)...)
		}
	}
	if c.shown == nil {
		c.shown = map[string]bool{}
	}
	for _, a := range addrs {
		c.shown[a] = true
	}
	if err := c.ids.Ensure(c.ctx, c.w, keys...); err != nil {
		return err
	}
	av, err := c.r.Avatars(c.ctx, addrs)
	if err != nil {
		c.log.Warn().Err(err).Msg("views: avatars")
	}
	ids := make(map[string]string, len(keys))
	pics := make(map[string]string, len(keys))
	for _, k := range keys {
		ids[k] = c.ids.Of(c.w, k)
		if x, ok := avatar(c.w, av, k); ok && x.Status == core.AvatarOK {
			pics[k] = x.Path
		}
	}
	for _, l := range c.later {
		l.set(ids[l.key], pics[l.key])
	}
	c.later = c.later[:0]
	return nil
}

// person is key as a row names them.
func (c *rc) person(key string) *v2.Person {
	if key == "" {
		return nil
	}
	if key == model.Me || c.w.IsSelf(key) {
		return c.self()
	}
	name, _ := c.w.Name(key)
	p := v2.Person_builder{Name: name, Phone: phone(c.w.PN(key))}.Build()
	c.wait(key, func(id, av string) { p.SetId(id); p.SetAvatarPath(av) })
	return p
}

// self is this account as a row names it.
func (c *rc) self() *v2.Person {
	pn := c.w.SelfPN()
	if c.shown == nil {
		c.shown = map[string]bool{}
	}
	c.shown["self"] = true
	p := v2.Person_builder{Name: c.w.SelfName(), Phone: phone(pn), Self: true}.Build()
	if pn != "" {
		c.wait(c.w.Now(pn), func(id, av string) { p.SetId(id); p.SetAvatarPath(av) })
	}
	return p
}

// at is the person behind addr at t: a number later given to someone else
// stays its old owner's.
func (c *rc) at(addr string, t int64) *v2.Person {
	if addr == "" {
		return nil
	}
	if addr == model.Me || c.w.IsSelf(addr) {
		return c.self()
	}
	return c.person(c.w.Key(model.Norm(addr), t))
}

// now is the person behind addr today.
func (c *rc) now(addr string) *v2.Person {
	if addr == "" {
		return nil
	}
	if addr == model.Me || c.w.IsSelf(addr) {
		return c.self()
	}
	return c.person(c.w.Now(model.Norm(addr)))
}

// phone is a number address as e164 without the plus.
func phone(pn string) string {
	user, _, _ := strings.Cut(pn, "@")
	user, _, _ = strings.Cut(user, ":")
	return user
}

// avatar is key's picture: of its addresses, the one with the newest answer.
func avatar(w *model.World, av map[string]model.Avatar, key string) (model.Avatar, bool) {
	var best model.Avatar
	found := false
	for _, addr := range w.Addrs(key) {
		x, ok := av[addr]
		if !ok {
			continue
		}
		if !found || better(x, best) {
			best, found = x, true
		}
	}
	return best, found
}

// better says x is the newer answer, any answer beating none
func better(x, than model.Avatar) bool {
	if (x.Status != "") != (than.Status != "") {
		return x.Status != ""
	}
	if x.Status != "" {
		return x.T > than.T
	}
	return x.LastTry > than.LastTry
}
