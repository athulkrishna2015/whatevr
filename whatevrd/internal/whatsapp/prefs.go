package whatsapp

import (
	"context"
	"encoding/json"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	v2 "github.com/codelif/whatevr/proto/whatevr/v2"

	"whatevrd/internal/core"
	"whatevrd/internal/model"
)

// DefaultPreferences is what a daemon nobody configured does.
func DefaultPreferences() *v2.Preferences {
	return v2.Preferences_builder{
		Notifications:        true,
		NotificationPreview:  true,
		AutoDownloadMaxBytes: 16 << 20,
		AutoFetchMaps:        true,
		MuteArchivedChats:    true,
		AntiDelete:           true,
	}.Build()
}

// Preferences is the stored preferences, the defaults when none are. they
// are kept whole as protojson, written with EmitUnpopulated.
func Preferences(raw json.RawMessage) *v2.Preferences {
	if len(raw) == 0 {
		return DefaultPreferences()
	}
	p := &v2.Preferences{}
	if protojson.Unmarshal(raw, p) != nil {
		return DefaultPreferences()
	}
	return p
}

// prefs is the preferences now, the defaults when the read fails.
func (c *Client) prefs(ctx context.Context) *v2.Preferences {
	raw, err := c.r.Prefs(ctx)
	if err != nil {
		c.log.Warn().Err(err).Msg("whatsapp: read the preferences")
	}
	return Preferences(raw)
}

// Prefs is the preferences now, the defaults when the read fails.
func (c *Client) Prefs(ctx context.Context) *v2.Preferences { return c.prefs(ctx) }

// SetDefaultFrontend stores the frontend a click starts. the caller checks
// the id names one.
func (c *Client) SetDefaultFrontend(ctx context.Context, id string) error {
	p := c.prefs(ctx)
	p.SetDefaultFrontend(id)
	return c.storePrefs(ctx, p)
}

// Folders lists every chat folder, by name.
func (c *Client) Folders(ctx context.Context) ([]model.Folder, error) {
	return c.r.Folders(ctx)
}

// CreateFolder makes a user chat list; the input seq it is logged under is
// the folder id.
func (c *Client) CreateFolder(ctx context.Context, name string) (int64, error) {
	if strings.TrimSpace(name) == "" {
		return 0, Errorf(ErrInvalid, "folder name is required")
	}
	h, err := json.Marshal(core.FolderHead{Op: "create", Name: strings.TrimSpace(name)})
	if err != nil {
		return 0, err
	}
	seq, err := c.core.Append(ctx, core.Input{Kind: core.KindFolder, V: 1, Head: h})
	if err != nil {
		return 0, err
	}
	c.waitLogged(ctx)
	return seq, nil
}

// RenameFolder renames a chat folder.
func (c *Client) RenameFolder(ctx context.Context, id int64, name string) error {
	if id <= 0 {
		return Errorf(ErrInvalid, "folder id is required")
	}
	if strings.TrimSpace(name) == "" {
		return Errorf(ErrInvalid, "folder name is required")
	}
	if err := c.append(ctx, core.KindFolder, core.FolderHead{Op: "rename", ID: id, Name: strings.TrimSpace(name)}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// DeleteFolder deletes a chat folder and every membership in it.
func (c *Client) DeleteFolder(ctx context.Context, id int64) error {
	if id <= 0 {
		return Errorf(ErrInvalid, "folder id is required")
	}
	if err := c.append(ctx, core.KindFolder, core.FolderHead{Op: "delete", ID: id}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// SetChatFolder puts a chat in a folder, or takes it out when folder is 0.
// Local display policy, like pins: nothing crosses to WhatsApp.
func (c *Client) SetChatFolder(ctx context.Context, key string, folder int64) error {
	op := "unset"
	if folder > 0 {
		op = "set"
	}
	if err := c.append(ctx, core.KindFolder, core.FolderHead{Op: op, Chat: key, Folder: folder}, nil); err != nil {
		return err
	}
	c.waitLogged(ctx)
	return nil
}

// SetChatFavorite favorites a chat on this device. local display policy,
// like pins: nothing crosses to WhatsApp.
func (c *Client) SetChatFavorite(ctx context.Context, key string, on bool) error {
	return c.append(ctx, core.KindFavorite, core.FavoriteHead{Chat: key, On: on}, nil)
}

// SetPreferences sets the fields set has; the rest stay.
func (c *Client) SetPreferences(ctx context.Context, set *v2.PreferencesSet) error {
	p := c.prefs(ctx)
	if set.HasNotifications() {
		p.SetNotifications(set.GetNotifications())
	}
	if set.HasNotificationSound() {
		p.SetNotificationSound(set.GetNotificationSound())
	}
	if set.HasNotificationPreview() {
		p.SetNotificationPreview(set.GetNotificationPreview())
	}
	if set.HasAutoDownloadPhotos() {
		p.SetAutoDownloadPhotos(set.GetAutoDownloadPhotos())
	}
	if set.HasAutoDownloadVideos() {
		p.SetAutoDownloadVideos(set.GetAutoDownloadVideos())
	}
	if set.HasAutoDownloadAudio() {
		p.SetAutoDownloadAudio(set.GetAutoDownloadAudio())
	}
	if set.HasAutoDownloadDocuments() {
		p.SetAutoDownloadDocuments(set.GetAutoDownloadDocuments())
	}
	if set.HasAutoDownloadStickers() {
		p.SetAutoDownloadStickers(set.GetAutoDownloadStickers())
	}
	if set.HasAutoDownloadMaxBytes() {
		p.SetAutoDownloadMaxBytes(set.GetAutoDownloadMaxBytes())
	}
	if set.HasAutoFetchMaps() {
		p.SetAutoFetchMaps(set.GetAutoFetchMaps())
	}
	if set.HasMuteArchivedChats() {
		p.SetMuteArchivedChats(set.GetMuteArchivedChats())
	}
	if set.HasAntiDelete() {
		p.SetAntiDelete(set.GetAntiDelete())
	}
	if set.HasKeepChatsArchived() {
		p.SetKeepChatsArchived(set.GetKeepChatsArchived())
	}
	if set.HasTerminal() {
		p.SetTerminal(set.GetTerminal().GetArgs())
	}
	if err := c.storePrefs(ctx, p); err != nil {
		return err
	}
	model.SetAntiDelete(p.GetAntiDelete())
	return nil
}

func (c *Client) storePrefs(ctx context.Context, p *v2.Preferences) error {
	raw, err := protojson.MarshalOptions{EmitUnpopulated: true}.Marshal(p)
	if err != nil {
		return err
	}
	return c.SetPrefsRaw(ctx, raw)
}

// SetPrefsRaw logs the preferences as they are stored.
func (c *Client) SetPrefsRaw(ctx context.Context, raw json.RawMessage) error {
	return c.append(ctx, core.KindPrefs, core.PrefsHead{Prefs: raw}, nil)
}

// append logs one input of the daemon's own.
func (c *Client) append(ctx context.Context, kind string, head any, body []byte) error {
	h, err := json.Marshal(head)
	if err != nil {
		return err
	}
	_, err = c.core.Append(ctx, core.Input{Kind: kind, V: 1, Head: h, Body: body})
	return err
}
