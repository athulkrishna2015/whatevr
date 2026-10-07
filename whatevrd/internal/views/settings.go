package views

import (
	"context"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/server"
	"whatevrd/internal/whatsapp"
)

func (rs *Reads) privacyView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		p, err := rs.r.Privacy(ctx)
		if err != nil {
			return nil, err
		}
		row := v2.PrivacyRow_builder{
			LastSeen:     privacyValue(p["last"]),
			Online:       privacyValue(p["online"]),
			ProfilePhoto: privacyValue(p["profile"]),
			About:        privacyValue(p["status"]),
			GroupAdd:     privacyValue(p["groupadd"]),
			CallAdd:      privacyValue(p["calladd"]),
			ReadReceipts: p["readreceipts"] != "none",
		}.Build()
		it := &v2.Upsert{}
		it.SetPrivacy(row)
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "privacy") }
	return w, nil, nil
}

func privacyValue(v string) v2.PrivacyValue {
	switch v {
	case "all":
		return v2.PrivacyValue_PRIVACY_VALUE_ALL
	case "contacts":
		return v2.PrivacyValue_PRIVACY_VALUE_CONTACTS
	case "contact_blacklist":
		return v2.PrivacyValue_PRIVACY_VALUE_CONTACTS_EXCEPT
	case "none":
		return v2.PrivacyValue_PRIVACY_VALUE_NOBODY
	case "match_last_seen":
		return v2.PrivacyValue_PRIVACY_VALUE_MATCH_LAST_SEEN
	case "known":
		return v2.PrivacyValue_PRIVACY_VALUE_KNOWN
	}
	return v2.PrivacyValue_PRIVACY_VALUE_UNSPECIFIED
}

// Prefs is the preferences now.
func (rs *Reads) Prefs(ctx context.Context) (*v2.Preferences, error) {
	raw, err := rs.r.Prefs(ctx)
	if err != nil {
		return nil, err
	}
	return whatsapp.Preferences(raw), nil
}

func (rs *Reads) preferencesView(ctx context.Context, s *server.Session, req *v2.Subscribe) (server.Window, *v2.SubscribeResult, error) {
	w := &win{params: req}
	w.items = func(ctx context.Context, _ int) ([]*v2.Upsert, error) {
		p, err := rs.Prefs(ctx)
		if err != nil {
			return nil, err
		}
		it := &v2.Upsert{}
		it.SetPreferences(v2.PreferencesRow_builder{Preferences: p}.Build())
		return one(it), nil
	}
	w.wake = func(c core.Change) bool { return touches(c, "prefs") }
	return w, nil, nil
}
