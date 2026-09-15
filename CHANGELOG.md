# Changelog

All notable changes to Whatevr. The protocol is versioned separately in
PROTOCOL.md (stable at version 1: additive changes only).

## Unreleased

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
