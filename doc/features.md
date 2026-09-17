# Features

Status key: **have** · **new** (this cycle) · **missing** (planned, safe) ·
**blocked** (upstream or server) · **refused** (ban risk, see safety.md).

## Calls

- Incoming/group call notices, Calls tab, reject, missed-call entries,
  ringing badge: **have**.
- Placing/answering voice/video, call links, screen share, PiP, in-call
  toggles: **blocked** — whatsmeow exposes signaling + `RejectCall` only, no
  SRTP/media stack.

## Messages & media

- Text/image/video/GIF/voice/audio/document/location/contact/poll send and
  receive, replies, edits, forwards, reactions, view-once send: **have**.
- HD photo mode, document filename override: **have**.
- Drag-and-drop send, multi-file attach dialogs: **new**.
- Composer `*bold*`/`_italic_`/`~strike~` shortcuts (Ctrl+B/I/U),
  @everyone (@all) and @admins mention expansion: **new** (@all predates).
- Emoji + sticker panels, GIF *playback*: **have**; GIF *search*: **missing**.
- Sticker creation from a photo: **missing** (no new deps wanted; ffmpeg path
  possible).
- Desktop voice recording: **missing**.
- 2 GB/700 MB/5-minute media caps: server-enforced, **refused** to bypass.
- Anti-delete messages (content kept + Deleted mark, toggleable):
  **new**. Ghost story viewing and kept deleted statuses: inherent (no viewed
  receipts are ever sent; status revokes never touched the status store).
- Edit history dialog (current vs previous versions): **new**.
- Sender client indicator (phone app vs linked device, info dialog + bubble
  mark): **new**. OS-level (Android vs iOS) is not exposed by the protocol.

## Chats & navigation

- Three-pane/wide + single-column compact layouts (responsive): **have**.
- Rail tabs (chats, calls, status, channels, logs), archived + starred,
  unread badge, chat search, FTS message search: **have**.
- Unread-only filter: **new**. Favorites filter, chat lock, mark-as-unread:
  **missing**.
- Right-click chat actions (pin/mute/archive/leave/invite): **have**.
- Media gallery with kind filters + separate links view: **have**.
- Custom chat folders: **missing** (planned).
- Multi-account switching: **missing** (planned; seam is per-account data
  dirs + daemon multiplexing).
- Ctrl+click opens a chat in a new window (second process, own daemon
  connection): **new**.
- Message scheduler, app-level PIN lock, per-chat locks: **missing**
  (planned, all local-only and safe).
- Notification inline-reply: **missing**.

## Status & stories

- Status tab with rings, viewer (text/photo/video/audio playback,
  thumbnail-first, auto-download), post text/media, reply/delete/mark-viewed,
  viewer receipts, audience privacy: **have**.
- Per-contact keep with an Archived section for expired statuses: **new**.
- Channel posts route to the Channels tab (never chats): **new**.
- Status editor extras (stickers/music/trim/mentions): **missing**.

## Groups, communities, channels

- Group create/manage/members/invites/permissions: **have**.
- Community directory + link/unlink: **have** (API-level; no browser tab).
- Channels tab (follow/unfollow/mute/messages/views): **have**.

## Privacy, settings, desktop

- Privacy audiences, blocked list, read receipts, disappearing timers,
  notifications/appearance/storage/window settings, QR + phone login,
  backup/restore, tray icon with menu, autostart unit: **have**.
- Typing-indicator toggle: **new** (passive omission).
- Keyboard shortcuts page: **have** (extended this cycle).
- Logs tab with failure surfacing: **have**.
