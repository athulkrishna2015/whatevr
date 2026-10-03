package wa

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	appstore "whatevrd/internal/store"
)

// what a connection owner outside this package needs from it

// QRLogin is the pairing flow on cli, for a device with no session: codes go
// to the frontends, and it returns once the phone scanned one or it failed.
func (c *Client) QRLogin(ctx context.Context, cli *whatsmeow.Client) error {
	return c.startQRLogin(ctx, cli)
}

// QRExpired says nobody scanned in time, so a new code is due at once.
func QRExpired(err error) bool { return errors.Is(err, errQRCodeExpired) }

// RefreshVersion fetches the version whatsapp web is on, at most every
// waVersionInterval. a failed fetch never holds up a connect.
func (c *Client) RefreshVersion(ctx context.Context) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if time.Since(c.versionFetched) < waVersionInterval {
		return
	}
	if c.refreshWAVersion(ctx) {
		c.versionFetched = time.Now()
	}
}

// queued tells the outbox about a send just saved as pending.
func (c *Client) queued(ctx context.Context, m appstore.Message) {
	o := c.instrument.Outbox
	if o == nil {
		return
	}
	if err := o.Queued(ctx, m.ChatID, appstore.ExternalMessageID(m.ChatID, m.ID), nil, ""); err != nil {
		zerolog.Ctx(ctx).Error().Err(err).Str("msg", m.ID).Msg("outbox: queued send not logged, it will not go out")
	}
}

func (c *Client) attempted(ctx context.Context, m appstore.Message, err error, final bool) {
	if o := c.instrument.Outbox; o != nil {
		o.Attempted(ctx, m.ChatID, appstore.ExternalMessageID(m.ChatID, m.ID), err, final)
	}
}

// guardedSend is a message send past the daemon's half of the send guard,
// for the sends that do not go through the queue.
func (c *Client) guardedSend(ctx context.Context, client *whatsmeow.Client, to types.JID, msg *waE2E.Message, extra ...whatsmeow.SendRequestExtra) (whatsmeow.SendResponse, error) {
	if g := c.instrument.Guard; g != nil {
		if err := g(ctx, to.ToNonAD().String()); err != nil {
			return whatsmeow.SendResponse{}, err
		}
	}
	return client.SendMessage(ctx, to, msg, extra...)
}
