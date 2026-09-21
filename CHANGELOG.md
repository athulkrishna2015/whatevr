# Changelog

All notable changes to Whatevr. The protocol is versioned separately in
PROTOCOL.md (stable at version 1: additive changes only).

## Unreleased

### Fixed

- Media downloads are cancellable by tapping the download control: every
  bubble's download button/ring toggles between start and cancel
  (`media.cancel_download`), and the context menu keeps its Cancel entry.
- Pending uploads animate (spinner in place of the clock) and cancel the
  same way: tapping the footer status or the new Cancel Send menu entry
  marks the queued message failed via new `send.cancel` (already-sent
  messages are rejected, never rewritten).

- Send size caps now match the official Web client: videos up to 100 MiB and
  any file staged as a document (including photos, videos and audio) up to
  2 GiB; other media stays at 25 MiB. The server remains authoritative;
  nothing above official limits is attempted.

- Forwarded messages now show a "Forwarded" header above the bubble content
  (Telegram Desktop's `HistoryMessageForwarded` pattern): the daemon already
  stored and sent the `forwarded` flag, but the frontend dropped it. The
  `ProtocolMessageModel` exposes it as `isForwarded` (plus snapshot support)
  and framed bubbles render the header with the reply/media/body offsets
  shifted below it. See `doc/tdesktop-notes.md` for the full
  whatkevr-vs-tdesktop verdict table (mention pill, albums and viewer
  zoom/pan remain TODO).

- PDF documents show a first-page thumbnail preview: received PDFs render
  the sender-provided preview, outgoing PDFs render one synchronously via
  pdftoppm (also attached on the wire for the recipient's phone), and
  downloads whose sender shipped none derive one afterwards. Existing
  thumbnails are never replaced.
- "Send as document" no longer applies the 25 MiB media ceiling (own
  100 MiB cap, no duration check), so videos staged as documents send as-is.
- Desktop-recorded voice notes now appear on mobile: the outbound path sends
  them as `audio/ogg; codecs=opus` like official clients instead of Go's
  sniffed `application/ogg`, which phones never rendered for PTT.
- The Status column header has a New-status button again: StatusPage's own
  action never reaches a toolbar when embedded list-only, so the column
  forwards to the page's post dialog (and chat-only actions hide outside
  chats mode).
- Text messages with links now render the sender-provided link preview card
  (title, description, thumbnail) between the reply quote and the body;
  tapping opens the URL. Previews are stored at ingest, so rendering never
  fetches. `link_preview` (`url`, `title`, `description`, `thumbnail_path`)
  is documented on the message item.
- Poll, contact, and location bubbles render again: the model reads the
  nested `poll`/`contact`/`location` wire objects (question, per-option vote
  counts, multi-select, contact name/phone, coordinates) instead of flat keys
  the daemon never sent.
- Scheduled messages have a viewer: "Scheduled messages" in the conversation
  menu lists pending sends (soonest first) with per-row cancel, backed by new
  `schedule.list`/`schedule.cancel` commands.
- Quit now stops the daemon too (new `daemon.shutdown` command), so the
  daemon-owned tray icon leaves with the frontend instead of lingering
  headless.
- Channel message subscriptions now use the daemon's `channel_id` parameter,
  so followed-channel timelines load correctly again.
- Channel posts now carry their server IDs on the wire and support add/remove
  reactions through the pinned whatsmeow newsletter API.
- Opening a channel timeline now sends viewed receipts for the loaded posts.
- Status replies use the existing `status.reply` backend path; status text and
  captions linkify HTTP(S) URLs, and GIF statuses animate in the viewer.
- Status posting exposes the daemon-supported text background/font fields and
  image/video/audio file types.
- Status rows, channel rows/messages, calls, logs, stickers, media galleries,
  starred messages, and search results now expose appropriate right-click menus.
- Synced sticker favorite dimensions are no longer swapped, preventing distorted
  sticker tiles after phone reconciliation.
- Backup export and keyring passphrase storage are available from Storage & Cache
  settings; restore remains an offline `whatevrd --restore` operation.
- Media streams inherit the daemon lifecycle, sparse-file progress is clamped to
  the declared size, and subscription snapshots cannot be overtaken by live events.
- Status, Calls, Channels, Logs, and Starred views now share a persistent second
  workspace column with the chat list instead of rebuilding layer pages on each
  switch.
- Archived chats expand inline downward rather than overlaying earlier rows.
- The Archived section header is clickable again: the chat list no longer
  paints over it and swallows its clicks.
- Group composers are disabled for members when the group is configured for
  admin-only sending.
- Message Info shows sent, delivered, read, and played/listened timestamps,
  including per-participant group lists.
- Added a frontend app lock with salted PIN verification, lock-screen focus, and
  settings/shortcut guards. It intentionally does not claim to encrypt the
  daemon database or authenticate other same-user socket clients.
- Status viewing now auto-loads media, plays videos inline, advances still
  statuses after 30 seconds, and proceeds through the next status/person like
  the mobile viewer.
- Added Qt Multimedia desktop voice recording through the existing voice-media
  send path.
- Added durable one-shot text scheduling with a daemon worker and composer time
  picker.
- Added local chat favorites and daemon-owned custom folder assignment/filtering.
- Added schedule/favorite/folder protocol documentation and corrected the chat
  row scanner after the new favorite column migration.
- GIF provider/search, multi-account, and live SQLCipher work remain deferred.
- Chat export: "Export chat…" in the conversation menu writes the transcript
  (.txt, official export shape) via a save dialog.
- Document sends keep their real filenames end to end (regression-tested);
  the bare-"file" display seen earlier came from an older build.
- Tray menu is a top-level popup window at the click point; the icon never
  pulses.
- Channel message pages load older history on scroll (directional window).
- Status viewer refreshes the moment a download lands (row updates now fan
  out).
- `scripts/verify_features.py` checks every README feature row locally.
- Multi-file sends only delivered the first file: the frontend's single
  in-flight guard dropped the rest. `send.media_batch` serializes them
  daemon-side (attach dialogs, both drag-drop halves).
- New `chat.mark_all_read` clears every badge with upstream receipts.
- Tray menu opens at the click point without a focus-stealing raise.
- Log rows are partially selectable, copy works, and the log folder opens.
- Edit history survives same-millisecond edits (autoincrement key).

## 0.8.4 — 2026-09-17

## 0.8.3 — 2026-09-17

- The Logs tab was always empty: the frontend subscribed to the `daemon.logs`
  view, but the daemon only ever registered a `daemon.logs` *command* — the
  subscription failed with `not_found` and the page stayed blank. The view now
  exists (`whatevrd/internal/protocol/logs_view.go`): it serves the newest
  `limit` lines from the in-memory log ring, refreshes on a 500 ms ticker, and
  keys rows by content-assigned stable sequence numbers so new lines stream in
  as ordinary upserts (no churn, duplicate lines each keep a row). Items carry
  the parsed `time`/`level`/`text` the page renders. Documented in the
  PROTOCOL.md view inventory.
- Statuses did not load automatically on opening the Status tab: the page
  rebuilt its contact groups only on the controller's `statusChanged`, which
  fires on subscribe/unsubscribe — not when the subscription's rows actually
  land in the model. It now also rebuilds on the status model's `countChanged`
  / `readyChanged`.
- Status rows carried no sender avatar, so the stories list showed initials
  only: `rebuildGroups()` dropped `sender.avatarPath`. The group now keeps it
  (preferring the freshest non-empty value) and binds it to `AvatarImage`.
- The profile-picture viewer had no way to save the picture it was showing; a
  Save-as button (local copy via `saveMediaAs`, reusing the daemon's fetched
  avatar cache) now sits next to Close.
- The daemon log spammed `connection read error: ... connection reset by peer`
  for every frontend that quit without a clean socket close (including every
  crash); `ECONNRESET` is now recognized as routine churn.
- Status thumbnails never appeared and opened statuses did not auto-download
  reliably: the feed carried no thumbnail until a full download completed, and
  the viewer had no thumbnail-first render. Image/video statuses now cache the
  sender thumbnail plus dimensions at ingest
  (`status_updates.media_thumbnail_local_path/media_width/media_height`,
  exposed as `thumbnail_path`/`width`/`height` on the `status` view); the
  viewer renders the thumbnail instantly, swaps in the full image when
  `status.download` lands, and triggers the download on open, page change, and
  feed update.
- Tray-icon clicks did nothing: the released daemon called `tray.Start`
  without the protocol-server activator, so Activate/ContextMenu went nowhere,
  and the frontend had no handlers for the `activate_window`/`show_tray_menu`
  events. Left-click now raises the frontend window; right-click shows a tray
  menu (Open, Notifications toggle, Quit).
- The v8 `status_updates` migration was nested inside the v7 block and not
  idempotent, so reopening a database whose `user_version` had been rewound
  failed with `duplicate column name`. It is un-nested now, the fresh schema
  carries the columns, and migration goes through an idempotent
  `ensureStatusMediaColumns` check.
- `tst_chatbubbleperf` failed on the new required poll/contact/location
  properties and then on stale object budgets; the test props were completed
  and the budgets raised deliberately to the new measured ceilings.
- The `status@broadcast` chat is back: incoming statuses are filed there as
  ordinary messages (in addition to the Status tab), silently — no unread
  bump, no notification — under the `status_mirror_to_chat` preference
  (default on), toggleable in Settings → Chats → Status. (Reverted before
  release: separate tabs stay the behavior; the one-day mirror's chat row is
  purged automatically on connect.)
- Channel posts (`*@newsletter`) no longer materialize as chats: they route to
  the Channels tab like statuses route to the Status tab (the
  `channel_messages` view fetches live, so open channel pages refresh on
  arrival).
- Statuses stored before ingest-time thumbnails now get them on connect via a
  backfill sweep, and video statuses play in the viewer (thumbnail poster with
  Play opening the shared MediaViewer). Voice/audio statuses play through the
  shared AudioPlayer, so every status kind loads something on open.
- Dropping files onto a conversation sends them through `sendMedia`, and the
  profile-picture Save dialog prefills the chat/contact name.
- The Logs page shows subscribe failures instead of staying blank, the Go tree
  is gofmt-clean again, and the AUR `.SRCINFO` files match their PKGBUILDs
  (missing `qt6-multimedia`, `mpv`, `ffmpeg` entries).
- The Logs tab stayed empty because log rows carried no `id` in their data
  and the model drops keyless rows; the id now rides along (with a regression
  test). Opening a status also triggers its download daemon-side, so a missed
  frontend trigger cannot leave it unloaded.
- The gallery kind filter never applied (frontend sent `kind`, daemon reads
  `kinds`); the frontend sends the array and the daemon also tolerates the
  singular.
- The Channels tab never refreshed its directory on open; it does now.
- Chat-list status rings: DM senders with unexpired statuses ring the avatar
  (highlighted while unviewed); tapping opens their status viewer.
- Misfiled newsletter chats are purged on connect (content lives server-side
  behind Channels); the tray tooltip counts messages, labeled as such.
- Tray right-click menu no longer raises the window first (the focus move
  dismissed the menu before it was seen).
- New channel posts notify like chat messages (fresh, unmuted, globally
  enabled); the tray tooltip counts messages, labeled as such; misfiled
  newsletter chats (with their stale badges) are purged.

### Updated

- whatsmeow pinned to the latest commit
  (`v0.0.0-20260915134308-320ff7ebf928`, 2026-09-15); no API changes.
- The service now runs the daemon from `~/.local/bin/whatevrd` (systemd user
  drop-in), which is built with `sqlite_fts5` — the previously running
  `/usr/bin/whatevrd` predates FTS5 and logged `no such module: fts5` on
  message search.

## 0.8.0 — 2026-09-15

### Fixed

- `whatkevr` failed to launch entirely: the window never appeared, the process
  exited 1, and nothing was printed to the terminal. `ChatBubble.qml` used
  `QQC2.Button` (commit c94de1a) without the `import QtQuick.Controls as QQC2`
  alias, which made the whole QML tree fail to load at runtime
  (`QQC2 is neither a type nor a namespace`), cascading through MessageView →
  ConversationPane → Main. The alias import was added alongside the plain one
  (the file's other Controls types — Button, Label, ToolButton, BusyIndicator —
  use unqualified names). Diagnosed via `journalctl --user | grep whatkevr`:
  QML load errors from a desktop-file launch land in the journal, not on
  stderr.
- `PollCreateDialog.qml` referenced the unqualified `Overlay` attached object
  without importing it — creating a poll logged `ReferenceError: Overlay is
  not defined` and the dialog had no parent/centering. Qualified with the
  file's existing `QQC2` alias, matching `MediaViewer`/`ProfilePictureViewer`.
- `MessageComposer.qml` contact/location attach dialogs used the nonexistent
  `Platform.FileDialog.AcceptOpenFileName` enum (`OpenFile` is the actual
  name, as the file's other dialogs already used), a nonexistent `fileUrl`
  property (the property is `file`), non-QML-JS `new QUrl(...)` and
  `Qt.basename(...)` calls — attaching a contact or location would have thrown
  at runtime. They now use `file` directly, matching the other dialogs in the
  file, with a `decodeURI` basename fallback for the contact name.
- The expression picker froze on the stickers tab: `ExpressionPicker.qml`
  called `stickerPane.deactivate()` when switching back to emoji or closing,
  but `StickerPane` never exposed that function. The TypeError aborted the
  mode switch mid-way (`root.mode` was never updated), wedging the picker in
  stickers mode. `StickerPane` now forwards `deactivate()` to the sticker
  controller, which tears the view subscription down as intended.

### Added — sending

- `send.media` now sends video, audio, voice notes and documents, not just
  images. `kind` forces the kind (`image`/`video`/`audio`/`voice`/`document`,
  empty auto-classifies); `filename` overrides the document display name.
  GIFs are still refused until a GIF→MP4 transcode path exists.
- View-once sending for photo, video and voice notes (`send.media`
  `view_once`, composer eye toggle). Forwards of view-once rows are rejected,
  matching WhatsApp. Inbound view-once keeps its keys on the tombstone row so
  an explicit `media.save` can fetch it; it still renders tombstoned and no
  auto-download policy can pick it up.
- Quoted replies, caption edits and payload-forwarding now cover
  video/audio/document, not just images and stickers.
- Composer attaches any file type (filter widened beyond images).

### Added — Status tab

- New `status` view (newest-first feed, never a chat row: `status@broadcast`
  traffic no longer materializes a bogus chat) plus `status.mark_viewed`,
  `status.post` (text/photo/video/audio), `status.download` and per-contact
  grouping in a dedicated Status tab with rings, a stepped viewer, posting
  and saving. Status viewed receipts to the sender are not sent yet.

### Added — Calls tab

- Incoming 1:1 and group calls ring: desktop notification ("answer on your
  phone"), a `calls` view, `call.reject`, and missed-call tombstones in the
  chat. Answering/placing calls is impossible upstream (whatsmeow exposes
  signaling only, no media stack).

### Added — groups and communities

- `group.create/leave/set_name/set_topic/set_photo/invite_link/join_link/members/set_announce/set_locked`
  with store refresh (participants, subject) after every mutation.
- `community.subgroups/link/unlink`: sub-group directory plus admin
  link/unlink (communities are an umbrella + announcement group + directory
  per the official FAQ model).
- Group card actions: leave (confirmed) and copy invite link.

### Added — saving, backups, logs, tray

- `media.save` copies chat media, statuses, profile photos and inbound
  view-once rows to a caller-owned path, downloading first when the row
  carries keys but no bytes.
- `daemon.backup_export` (AES-256-GCM bundles via scrypt when a passphrase
  is given, plain tar.gz otherwise) and `whatevrd --restore` offline import
  (guarded by the process lock; uses VACUUM INTO snapshots, safe on a live
  DB for export).
- OS keyring integration via Secret Service: `daemon.backup_set_passphrase`
  stores the backup passphrase; exports can use it without touching the wire.
- Debug logging to a rotated file (`whatevrd.log`, 5 MiB × 3) plus a
  `daemon.logs` query over the in-memory ring.
- Daemon StatusNotifierItem tray icon: connection state + unread indicator.

### genuinely out of scope (upstream-blocked)

- Call audio/video, screen share (no SRTP/media stack in whatsmeow).
- Payments initiation, broadcast-list sending (unsupported by whatsmeow /
  WhatsApp Web alike).
- Live SQLite encryption at rest (needs a SQLCipher driver swap).
