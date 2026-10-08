package whatsapp

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"whatevrd/internal/backup"
)

// ExportBackup writes an encrypted-or-plain backup bundle (message store +
// session + media) to dest, defaulting to a timestamped file next to the
// data dir. The passphrase comes from the argument, or from the OS keyring
// when useKeyring is set.
func (c *Client) ExportBackup(ctx context.Context, dest, passphrase string, useKeyring bool) (string, int64, error) {
	pass := []byte(passphrase)
	if len(pass) == 0 && useKeyring {
		keyringPass, err := backup.GetBackupPassphrase()
		if err != nil {
			return "", 0, err
		}
		pass = keyringPass
	}
	if strings.TrimSpace(dest) == "" {
		dest = backup.DefaultDest(c.sources())
	}
	size, err := backup.Export(ctx, c.sources(), dest, pass)
	if err != nil {
		return "", 0, err
	}
	return dest, size, nil
}

// SetBackupPassphrase stores the backup passphrase in the OS keyring
// (Secret Service: GNOME Keyring, KWallet, KeePassXC…).
func (c *Client) SetBackupPassphrase(ctx context.Context, passphrase string) error {
	if strings.TrimSpace(passphrase) == "" {
		return Errorf(ErrInvalid, "passphrase is required")
	}
	if err := backup.SetBackupPassphrase([]byte(passphrase)); err != nil {
		if errors.Is(err, backup.ErrNoKeyring) {
			return Errorf(ErrRejected, "no secret service on the session bus: pass the passphrase explicitly")
		}
		return err
	}
	return nil
}

func (c *Client) sources() backup.Sources {
	return backup.Sources{
		CoreDB:    filepath.Join(c.o.Paths.DataDir, "whatevr.db"),
		SessionDB: c.o.Paths.SessionDBPath,
		MediaDir:  c.o.Paths.MediaCacheDir,
		DataDir:   c.o.Paths.DataDir,
	}
}
