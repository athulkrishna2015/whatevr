package wa

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"whatevrd/internal/app"
	appstore "whatevrd/internal/store"
)

const (
	connBackoffBase = 5 * time.Second
	connBackoffMax  = 60 * time.Second
	qrRetryDelay    = 300 * time.Millisecond
	qrBackoffMax    = 30 * time.Second

	waVersionTimeout  = 10 * time.Second
	waVersionInterval = 6 * time.Hour

	connReconcileInterval = 5 * time.Second
)

// http.DefaultClient has no timeout, so a black-holed route after sleep used to
// hang the supervisor for minutes before backoff even started.
var waVersionClient = &http.Client{Timeout: waVersionTimeout}

var (
	// errQRCodeExpired is the ordinary case: nobody scanned in time. A fresh
	// code has to appear straight away, so this one is never backed off.
	errQRCodeExpired = errors.New("QR code expired")
	// errQRLoginRetry is a QR attempt that actually failed. Retrying those at
	// the same speed is a hammer, so they back off.
	errQRLoginRetry = errors.New("retry QR login")
)

// runConnectionSupervisor replaces the old one-shot start(). It loops
// forever (until ctx is cancelled), connecting and retrying with
// exponential backoff on any failure.
func (c *Client) runConnectionSupervisor(ctx context.Context) {
	attempt := 0
	var lastVersionFetch time.Time

	for {
		if ctx.Err() != nil {
			return
		}

		// Drain any stale reconnect signal before each attempt.
		select {
		case <-c.reconnectCh:
		default:
		}
		c.closeForReconnectIfRequested()

		// store.SetWAVersion writes unsynchronized globals that connect reads,
		// so this has to stay on the supervisor goroutine. It is refreshed on an
		// interval rather than per attempt: a failed fetch must never cost a
		// connect.
		if time.Since(lastVersionFetch) >= waVersionInterval {
			if c.refreshWAVersion(ctx) {
				lastVersionFetch = time.Now()
			}
		}

		err := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}

		if err != nil {
			retry := connectionRetry(attempt, err)
			attempt = retry.attempt
			nextRetryUnix := int64(0)
			if retry.nextRetryUnix {
				nextRetryUnix = time.Now().Add(retry.delay).Unix()
			}
			if retry.detail != "" {
				c.daemon.SetConnection(retry.state, retry.detail, int32(attempt), nextRetryUnix, retry.canReconnect)
			} else {
				c.daemon.SetConnMeta(int32(attempt), nextRetryUnix, retry.canReconnect)
			}

			select {
			case <-ctx.Done():
				return
			case <-c.reconnectCh:
				attempt = 0
			case <-time.After(retry.delay):
			}
			continue
		}

		// Successful connect — reset backoff, clear retry meta.
		attempt = 0
		c.daemon.SetConnMeta(0, 0, false)

		// Park until a reconnect is signalled.
		if !c.waitForReconnectSignal(ctx) {
			return
		}

		// Brief pause so whatsmeow can finish cleanup.
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (c *Client) waitForReconnectSignal(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-c.reconnectCh:
		return true
	}
}

// runConnectionReconciler keeps what the daemon says about the connection
// matched to what the client actually has.
//
// It runs for the whole life of the account, not only while the supervisor is
// parked, and it corrects in both directions. The direction that was missing is
// the one that mattered: a socket that is up and delivering messages while the
// UI still reads "Still offline. Check your internet connection." had nothing
// that would ever notice, because every path that could publish Online only ran
// on a connect the supervisor no longer believed it needed.
func (c *Client) runConnectionReconciler(ctx context.Context) {
	ticker := time.NewTicker(connReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileConnectionState()
		}
	}
}

func (c *Client) reconcileConnectionState() {
	client := c.currentClient()
	if client == nil || client.Store == nil || client.Store.ID == nil {
		// Not logged in: the login flow owns the state and this must not
		// overwrite "waiting for a QR scan" with a connection verdict.
		return
	}

	published, _, _, _, _ := c.daemon.ConnectionSnapshot()
	live := client.IsConnected() && client.IsLoggedIn()

	switch {
	case live && published != app.StateOnline:
		c.log.Debugf("Reconciler: socket is live but state was %v; publishing online", published)
		c.daemon.SetConnection(app.StateOnline, "Connected to WhatsApp", 0, 0, false)
	case !live && published == app.StateOnline:
		c.log.Debugf("Reconciler: state was online but the socket is gone; reconnecting")
		c.daemon.SetConnection(app.StateOffline, "Connection lost. Reconnecting...", 0, 0, true)
		c.requestReconnect(true)
	}
}

func (c *Client) closeForReconnectIfRequested() {
	if !c.reconnectNow.Swap(false) {
		return
	}
	if client := c.currentClient(); client != nil {
		client.Disconnect()
	}
}

func (c *Client) desiredPresence() types.Presence {
	c.presenceMu.Lock()
	defer c.presenceMu.Unlock()
	if len(c.frontendSessions) > 0 {
		return types.PresenceAvailable
	}
	return types.PresenceUnavailable
}

// refreshWAVersion updates the advertised client version, reporting whether it
// succeeded. Callers must be on the supervisor goroutine.
func (c *Client) refreshWAVersion(ctx context.Context) bool {
	fetchCtx, cancel := context.WithTimeout(ctx, waVersionTimeout)
	defer cancel()

	latest, err := whatsmeow.GetLatestVersion(fetchCtx, waVersionClient)
	if err != nil {
		c.log.Warnf("Failed to fetch latest WhatsApp version: %v", err)
		return false
	}
	store.SetWAVersion(*latest)
	return true
}

// connectOnce performs a single connection attempt. For QR-login sessions it
// blocks until the QR flow concludes (success or failure). Returns nil on a
// successful connect; the supervisor will then park until signalled.
func (c *Client) connectOnce(ctx context.Context) error {
	client := c.currentClient()
	if client == nil {
		return fmt.Errorf("WhatsApp client is not initialized")
	}

	if client.Store.ID == nil {
		return c.startQRLogin(ctx, client)
	}

	c.daemon.SetStateDetail(app.StateConnecting, "Connecting to WhatsApp...")
	// whatsmeow dispatches Disconnected on its own goroutine, so one for a dead
	// socket can land after a new socket is already up. Treating the resulting
	// ErrAlreadyConnected as a failure wedged the supervisor into permanent
	// backoff while the connection was healthy.
	if err := client.ConnectContext(ctx); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return fmt.Errorf("connect: %w", err)
	}
	if client.IsLoggedIn() && client.IsConnected() {
		c.daemon.SetConnection(app.StateOnline, "Connected to WhatsApp", 0, 0, false)
	}
	return nil
}

func (c *Client) startQRLogin(ctx context.Context, client *whatsmeow.Client) (err error) {
	c.daemon.SetStateDetail(app.StateNeedLogin, "Waiting for WhatsApp QR scan")

	// GetQRChannel refuses to run on a connected socket, so an attempt that
	// ended with the socket still up makes every attempt after it fail before
	// it starts, and the QR flow never recovers. Leave the socket down unless
	// the scan actually succeeded and pairing is continuing on it.
	if client.IsConnected() {
		client.Disconnect()
	}
	defer func() {
		if err != nil && client.IsConnected() {
			client.Disconnect()
		}
	}()

	qrChan, err := client.GetQRChannel(ctx)
	if err != nil {
		return fmt.Errorf("create QR channel: %w", err)
	}

	if err := client.ConnectContext(ctx); err != nil && !errors.Is(err, whatsmeow.ErrAlreadyConnected) {
		return fmt.Errorf("connect for QR login: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case item, ok := <-qrChan:
			if !ok {
				return fmt.Errorf("QR channel closed unexpectedly: %w", errQRLoginRetry)
			}

			switch item.Event {
			case whatsmeow.QRChannelEventCode:
				c.daemon.PublishQRCode(item.Code, time.Now().Add(item.Timeout))
			case whatsmeow.QRChannelSuccess.Event:
				c.daemon.SetStateDetail(app.StateConnecting, "QR scanned; completing login")
				return nil
			case whatsmeow.QRChannelTimeout.Event:
				c.daemon.SetStateDetail(app.StateNeedLogin, "QR login timed out; scan a new code")
				return fmt.Errorf("QR login timed out: %w", errQRCodeExpired)
			case whatsmeow.QRChannelClientOutdated.Event:
				c.daemon.SetConnection(app.StateOffline, "WhatsApp client is outdated. Update whatevr/whatevrd.", 0, 0, false)
				return fmt.Errorf("client outdated")
			case whatsmeow.QRChannelScannedWithoutMultidevice.Event:
				c.daemon.SetStateDetail(app.StateNeedLogin, "Enable multi-device on your phone and scan again")
				return fmt.Errorf("multi-device not enabled: %w", errQRLoginRetry)
			case whatsmeow.QRChannelEventError:
				c.daemon.SetStateDetail(app.StateNeedLogin, fmt.Sprintf("QR login error: %v", item.Error))
				if item.Error != nil {
					return errors.Join(errQRLoginRetry, item.Error)
				}
				return errQRLoginRetry
			}
		}
	}
}

type retryPlan struct {
	attempt       int
	delay         time.Duration
	state         app.State
	detail        string
	canReconnect  bool
	nextRetryUnix bool
}

func connectionRetry(attempt int, err error) retryPlan {
	// An expired code is not a failure; the user is still looking at the
	// screen and needs the next one immediately.
	if errors.Is(err, errQRCodeExpired) {
		return retryPlan{
			attempt: 0,
			delay:   qrRetryDelay,
			state:   app.StateNeedLogin,
		}
	}
	if errors.Is(err, errQRLoginRetry) {
		attempt++
		return retryPlan{
			attempt: attempt,
			delay:   qrBackoffDelay(attempt),
			state:   app.StateNeedLogin,
		}
	}

	attempt++
	delay := connBackoffDelay(attempt)
	return retryPlan{
		attempt:       attempt,
		delay:         delay,
		state:         app.StateOffline,
		detail:        retryDetail(attempt, delay),
		canReconnect:  true,
		nextRetryUnix: true,
	}
}

func (c *Client) resetAfterExternalLogout() {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	ctx := context.Background()
	c.endSessionLocked()
	// The loops come back whatever happens below. A failed wipe used to return
	// here and leave the daemon with no supervisor, so it never reconnected and
	// never asked for a QR either.
	defer c.restartRunLoopsLocked()

	c.mu.Lock()
	old := c.client
	c.mu.Unlock()

	if old != nil {
		old.Disconnect()
		if old.Store.ID != nil {
			if err := old.Store.Delete(ctx); err != nil {
				c.log.Warnf("Failed to delete device store after remote logout: %v", err)
			}
		}
	}

	if err := c.wipeAccountData(ctx); err != nil {
		c.log.Errorf("Failed to wipe account data after remote logout: %v", err)
	}
}

func (c *Client) Logout(ctx context.Context) error {
	c.lifecycleMu.Lock()
	defer c.lifecycleMu.Unlock()

	c.daemon.SetStateDetail(app.StateConnecting, "Logging out and clearing local session data")

	client := c.currentClient()
	if client != nil {
		if client.Store.ID != nil {
			logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := client.Logout(logoutCtx); err != nil && err != store.ErrDeviceDeleted {
				c.log.Warnf("Remote WhatsApp logout failed; clearing local session anyway: %v", err)
			}
			cancel()
		} else {
			client.Disconnect()
		}
	}
	c.endSessionLocked()
	// Same as the external-logout path: a failed wipe must not leave the daemon
	// without a supervisor.
	defer c.restartRunLoopsLocked()

	if err := c.wipeAccountData(context.Background()); err != nil {
		return err
	}

	c.daemon.SetStateDetail(app.StateNeedLogin, "Logged out")
	return nil
}

// wipeAccountData clears every piece of account-scoped state: the app DB
// (all tables except machine-level preferences), the media cache (avatars,
// stickers, media files), the whatsmeow session store, and in-memory caches
// keyed by account data. It is shared by user-initiated Logout and
// resetAfterExternalLogout so both paths reset identically. Callers must
// hold lifecycleMu and have cancelled the run context first.
func (c *Client) wipeAccountData(ctx context.Context) error {
	c.backupBeforeWipe(ctx)

	if err := c.store.ClearSessionData(ctx); err != nil {
		return err
	}

	if err := os.RemoveAll(c.paths.MediaCacheDir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.paths.MediaCacheDir, 0o700); err != nil {
		return err
	}

	c.mu.Lock()
	if c.client != nil {
		c.client.Disconnect()
		c.client = nil
	}
	oldContainer := c.container
	c.container = nil
	c.mu.Unlock()

	if oldContainer != nil {
		if err := oldContainer.Close(); err != nil {
			return err
		}
	}

	if err := os.RemoveAll(c.paths.SessionDir); err != nil {
		return err
	}
	if err := os.MkdirAll(c.paths.SessionDir, 0o700); err != nil {
		return err
	}

	container, err := openSessionStore(ctx, c.paths.SessionDBPath, c.log.Sub("DB"))
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.container = container
	c.mu.Unlock()

	c.resetInMemoryAccountState()
	c.daemon.ResetAccountState()

	return c.resetClient(ctx)
}

// backupBeforeWipe snapshots the app DB before a wipe, keeping only the most
// recent snapshot so repeated logouts don't accumulate backups. Failure to
// back up never blocks the wipe.
func (c *Client) backupBeforeWipe(ctx context.Context) {
	backupPath := filepath.Join(c.paths.DataDir, fmt.Sprintf("whatevrd-before-logout-%d.db", time.Now().Unix()))
	if err := c.store.Backup(ctx, backupPath); err != nil {
		c.log.Warnf("Failed to back up local database before logout: %v", err)
		return
	}
	c.log.Infof("Backed up local database before logout to %s", backupPath)

	entries, err := filepath.Glob(filepath.Join(c.paths.DataDir, "whatevrd-before-logout-*.db"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry == backupPath {
			continue
		}
		if err := os.Remove(entry); err != nil {
			c.log.Warnf("Failed to remove old logout backup %s: %v", entry, err)
		}
	}
}

// resetInMemoryAccountState drops in-memory caches keyed by account data so
// nothing bleeds into the next login. The run context must be cancelled
// (workers stopped) before calling.
func (c *Client) resetInMemoryAccountState() {
	c.avatarMu.Lock()
	c.avatarHigh = nil
	c.avatarLow = nil
	c.avatarQueued = make(map[appstore.AvatarSubject]avatarPriority)
	c.avatarMu.Unlock()

	c.offlineSyncMu.Lock()
	c.offlineSyncActive = false
	c.offlineSyncTotalEvents = 0
	c.offlineSyncTotalMessages = 0
	c.offlineSyncProcessedEvents = 0
	c.offlineSyncProcessedMessages = 0
	c.offlineSyncChangedChats = nil
	c.offlineSyncLastPublish = time.Time{}
	c.offlineSyncMu.Unlock()

	c.groupParticipantsMu.Lock()
	c.groupParticipantsFresh = make(map[string]time.Time)
	c.groupParticipantsInFlight = make(map[string]bool)
	c.groupParticipantsMu.Unlock()

	c.sendTimingsMu.Lock()
	c.sendTimings = make(map[string]*sendTiming)
	c.sendTimingsMu.Unlock()

	// A call ringing on the previous account must not survive into the next
	// one: neither the `calls` view nor a reject should reach a session that
	// is gone.
	c.callsMu.Lock()
	c.pendingCalls = nil
	c.callsMu.Unlock()

	c.pendingAppStateMu.Lock()
	c.pendingAppState = nil
	c.pendingAppStateMu.Unlock()

	c.clearHistorySyncStallWatch()
	c.historySyncMu.Lock()
	c.historySyncLastEvent = app.HistorySyncEvent{}
	c.historySyncLastActivity = time.Time{}
	c.historySyncMu.Unlock()

	c.backfillMu.Lock()
	pendingBackfills := c.backfillInFlight
	c.backfillInFlight = nil
	c.backfillMu.Unlock()
	for _, req := range pendingBackfills {
		if req.timer != nil {
			req.timer.Stop()
		}
	}

	c.posterMu.Lock()
	c.posterHigh = nil
	c.posterLow = nil
	c.posterQueued = nil
	c.posterMu.Unlock()

	// The cancels belong to downloads started on the previous session's
	// context, which is already cancelled; calling them is what releases the
	// waiters instead of leaving them on a channel nobody will close.
	c.mediaDownloadMu.Lock()
	downloads := c.mediaDownloads
	c.mediaDownloads = nil
	c.mediaDownloadMu.Unlock()
	for _, download := range downloads {
		if download.cancel != nil {
			download.cancel()
		}
	}

	c.mediaRetryMu.Lock()
	c.mediaRetries = nil
	c.mediaRetryMu.Unlock()

	c.mediaStreamMu.Lock()
	c.mediaStreams = nil
	c.mediaStreamMu.Unlock()

	c.messageStickerMu.Lock()
	c.messageStickerLocks = nil
	c.messageStickerMu.Unlock()

	c.stickerMu.Lock()
	c.stickerDownloads = nil
	stickerTimers := c.stickerLibraryTimers
	c.stickerLibraryTimers = nil
	c.stickerMu.Unlock()
	for _, timer := range stickerTimers {
		timer.Stop()
	}

	c.presenceMu.Lock()
	c.cancelPresenceOfflineTimerLocked()
	c.lastPresence = ""
	c.presenceMu.Unlock()

	// Latches. Each one says "work is already queued or running"; left set, the
	// next account's first request for that work is dropped as a duplicate.
	c.reconnectNow.Store(false)
	c.pinBackfill.Store(false)
	c.pinBackfillAgain.Store(false)

	c.historySyncMu.Lock()
	c.historySyncRunning = false
	c.historySyncWake = false
	c.historySyncMu.Unlock()

	c.privacyPublishMu.Lock()
	c.privacyPublishRunning = false
	c.privacyPublishAgain = false
	c.privacyPublishMu.Unlock()
}

// restartRunLoopsLocked restarts the background loops torn down by a wipe.
func (c *Client) restartRunLoopsLocked() {
	c.startRunLoopsLocked(context.Background())
}

// qrBackoffDelay grows from the fast retry up to qrBackoffMax, so a QR attempt
// that keeps failing stops hammering without ever giving up on the user.
func qrBackoffDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return qrRetryDelay
	}
	shift := attempt - 1
	if shift > 7 {
		shift = 7
	}
	delay := qrRetryDelay * (1 << uint(shift))
	if delay > qrBackoffMax {
		delay = qrBackoffMax
	}
	return delay
}

func connBackoffDelay(attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 4 {
		shift = 4
	}
	delay := connBackoffBase * (1 << uint(shift))
	if delay > connBackoffMax {
		delay = connBackoffMax
	}
	// Add up to 20% jitter.
	jitter := time.Duration(rand.Int64N(int64(delay / 5)))
	return delay + jitter
}

func retryDetail(attempt int, delay time.Duration) string {
	secs := int(delay.Round(time.Second).Seconds())
	if attempt <= 2 {
		return fmt.Sprintf("Connection lost. Retrying in %ds.", secs)
	}
	return fmt.Sprintf("Still offline. Check your internet connection. Retrying in %ds.", secs)
}
