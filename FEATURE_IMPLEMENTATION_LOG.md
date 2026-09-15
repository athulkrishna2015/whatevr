# Feature Implementation Log

This document records the feature requests discussed during the implementation
session, what was implemented, what was verified, and what remains incomplete.
It intentionally excludes personal information from screenshots, chats,
contacts, phone numbers, profile photos, and message contents.

## Project Context

Whatevr is a Linux-first WhatsApp client composed of:

- `whatevrd`: Go daemon owning the WhatsApp connection, SQLite store, media
  cache, notifications, and protocol socket.
- `whatkevr`: Qt/Kirigami frontend communicating with `whatevrd` over the
  stable protocol.
- `PROTOCOL.md`: newline-delimited JSON protocol contract, version 1.

The implementation used the repository's pinned `whatsmeow` dependency and
checked its upstream GitHub/pkg.go.dev APIs where relevant.

## Implemented Features

### Media Sending

- `send.media` accepts images, videos, audio, voice-note files, and documents.
- Automatic media-kind detection from MIME type.
- Explicit media kind selection through `kind`.
- Document filename override through `filename`.
- Document/file sending is no longer image-only.
- View-once sending for photo, video, audio, and voice media through
  `view_once`.
- View-once messages are rejected for unsupported kinds such as documents.
- View-once messages cannot be forwarded.
- Standard photo quality downscales large photos to approximately 1600px.
- HD photo mode keeps original image bytes.
- Existing reply, caption, edit, forward, and mention paths were extended for
  the new media kinds where supported.
- Composer attachment filter was widened beyond images.
- Composer view-once toggle was added.

### Media Saving

- `media.save` saves one of:
  - Chat message media.
  - Status media.
  - Profile photos.
  - Inbound view-once media after an explicit user save request.
- Saves use a daemon-side cache copy and an atomic destination write.
- Destination paths are validated as absolute, non-symlink paths.
- Media is downloaded first when metadata exists but local bytes are absent.

### Status / Stories

- Statuses are stored outside the chat table.
- `status@broadcast` no longer needs to create a normal chat row for newly
  ingested statuses.
- Dedicated status collection/view infrastructure was added.
- Per-contact status grouping was added to the frontend status page.
- Status rings show viewed/unviewed state.
- Status viewer supports text and downloaded image status display.
- Status media auto-downloads when a status is opened.
- Status posting supports text and media paths.
- Text status background color and font fields were added to storage and wire
  models.
- `status.mark_viewed` was added.
- `status.download` was added.
- `status.reply` was added for replying to another person's status in a DM.
- `status.delete` was added for deleting the user's own status.
- Status viewer records were added for who viewed a user's own status.
- Status audience privacy remains connected to the existing WhatsApp privacy
  setting plumbing.

### Calls

- Incoming one-to-one call offers are tracked.
- Incoming group call notices are tracked.
- A dedicated `calls` view was added.
- A dedicated Calls tab was added to the frontend.
- Desktop notifications explain that answering must happen on the phone.
- `call.reject` was added.
- Missed calls become chat transcript tombstones.
- Ringing call count appears in the frontend rail.

Important limitation: the pinned `whatsmeow` version exposes call signaling
and rejection but does not provide the SRTP/media stack required to place or
answer voice/video calls from the desktop. A true desktop call button cannot
be made functional without upstream support.

### Groups

- Create group.
- Leave group.
- Rename group.
- Change group description/topic.
- Change or clear group photo.
- Add, remove, promote, and demote members.
- Get/reset invite link.
- Join by invite link.
- Toggle admins-only sending.
- Toggle admins-only group-info editing.
- Group card actions for leaving and copying invite links.
- Participant refresh after group mutations.

### Communities

- Community sub-group directory API.
- Link an existing group to a community.
- Unlink a group from a community.

The community model follows the official WhatsApp concept: an umbrella,
announcement group, and linked sub-groups. A complete frontend community
browser and admin dashboard are still pending.

### Polls, Contacts, and Locations

- Poll creation with single-select or multi-select options.
- Poll options are validated and limited to 2–12 options.
- Poll voting through `message.vote`.
- Poll vote replacement for a user's new ballot.
- Poll tally persistence and voter lookup storage.
- Poll receive-side parsing and fallback rendering.
- Contact-card sending through vCard data.
- Contact-card receive-side storage and fallback rendering.
- Location sending with latitude, longitude, name, and address.
- Location receive-side storage and fallback rendering.

### Channels

- Channel/newsletter store table.
- Channel directory model for subscribed channels.
- Refresh followed channels.
- Follow channel by JID.
- Follow channel by invite link/code.
- Unfollow channel.
- Mute/unmute channel.
- Mark channel messages viewed.
- Fetch recent channel messages live.
- Channel message fallback kinds and view-count fields.

A dedicated Channels frontend tab and channel exploration page still need to be
wired into the Qt application. The upstream library has APIs for followed
channels and invite-based lookup, but no broad public directory search API.

### Groups and Chat Information

- Existing media gallery was expanded conceptually to include:
  - Photos.
  - Videos.
  - GIFs.
  - Voice notes.
  - Audio.
  - Documents.
  - Polls.
  - Contacts.
  - Locations.
- A separate link-gallery view was added at the protocol/store layer.
- Link gallery matches HTTP, HTTPS, and `www.` URLs.
- Media gallery supports filtering by media kind at the protocol layer.

Frontend filter controls for Photos, Videos, Voice, Audio, Documents,
Contacts, Locations, Polls, and Links still need to be connected to the
existing media gallery page.

### Backups and Security

- Backup export bundles the daemon database, session database, and media cache.
- SQLite databases are snapshotted using `VACUUM INTO` rather than unsafe raw
  file copying.
- Optional encrypted backup format using scrypt-derived AES-256-GCM.
- Offline restore through `whatevrd --restore`.
- OS Secret Service keyring integration for backup passphrases.
- Backup round-trip tests were added.

Live SQLCipher database encryption at rest is not implemented yet.

### Logs and Desktop Integration

- Automatic daemon log file under the cache directory.
- Log rotation at approximately 5 MiB.
- Three rotated log files are retained.
- Recent log lines are kept in an in-memory ring.
- `daemon.logs` protocol query exposes recent logs for debugging.
- StatusNotifierItem tray icon was added.
- Tray state reports connection state and unread count.


- Full-resolution profile-picture fetch was already available.
- `media.save` now provides the user-facing save path for profile photos.
- Group profile photos can be updated through group management.

Self-profile photo editing remains incomplete because the pinned whatsmeow
version exposes profile-picture reading but not a direct public self-avatar
setter.

### Polls Frontend

- `PollCreateDialog.qml` added; wired to `send.poll` via
  `ProtocolController::sendPoll` (chat_id from the selected conversation,
  multi-select toggle, 2–12 options validated client-side).
- Poll bubbles render question, options with vote bars (single/multi icons),
  and a vote-count label.
- Poll voting wired via `ProtocolController::votePoll` (`message.vote`);
  multi-select toggles and re-votes per server-side ballot replacement.

### Contact and Location Sharing

- `send.contact` wired via `ProtocolController::sendContact`; the composer's
  attach menu opens a vCard file dialog that extracts name and phone.
- `send.location` wired via `ProtocolController::sendLocation`; the composer's
  attach menu opens a JSON location file dialog.
- Contact bubbles render name, phone, and a "Message" button that opens a
  direct chat with the contact's JID.
- Location bubbles render a pin placeholder, name, address, and an
  "Open in Maps" link to OpenStreetMap.

### Gallery Filters

- `ChatMediaGalleryPage.qml` filters extended to all daemon-supported
  gallery kinds: Photos, Videos, Voice, Audio, Docs, Polls, Contacts, Locations.
- Links use the separate `chat_links` view, which remains a pending separate
  page (not a `chat_media` kind).

### Channels Frontend

- `ChannelsPage.qml` added with a followed-channel list and a "Follow Channel"
  action that accepts an invite link or JID.
- `ChannelMessagesPage.qml` added with a read-only channel message timeline,
  unfollow and mute/unmute actions.
- `ChatListSidebar.qml` rail gains Channels and Logs buttons.
- `ProtocolController` gains `openChannels`/`closeChannels`,
  `openChannelMessages`/`closeChannelMessages`, `followChannel`,
  `unfollowChannel`, `muteChannel`, and the `channelsModel` /
  `channelMessagesModel` properties.

### Logs Tab

- `LogsPage.qml` added; subscribes `daemon.logs` for the page lifetime.
- Error/warn rows get a tinted background; log level and timestamp are
  rendered in monospace.
- `ProtocolController` gains `openLogs`/`closeLogs` and `logsModel`/`logsLoading`.

### Chat and Contact UX (implemented)

- Chat info filters for media, documents, contacts, locations, polls,
  audio, voice, and video — wired in `ChatMediaGalleryPage.qml`.
- Location bubble includes "Open in Maps" via OpenStreetMap.
- Contact bubble includes a "Message" button to start a direct chat.

## Existing Features Confirmed Before This Session

These were already present in the repository and were not reimplemented:

- QR login.
- Persistent login session.
- Logout.
- SQLite message database.
- Older-message loading.
- Incoming text/image/sticker messages.
- Text and image sending.
- Replies.
- Delivery/read receipts.
- Chat pin/archive/mute.
- Basic groups and group information.
- Chat avatars.
- Image/media display.
- Clipboard image paste.
- Typing indicators.
- Presence and last-seen data.
- History-sync progress.
- Desktop notifications.
- Emoji picker.
- Message and chat search.
- Sticker receive/send infrastructure.
- Message reactions.
- Composer emoji search.
- Message edit/delete/forward/star/pin.
- Settings and privacy pages.
- Profile and about editing.
- Auto-download preferences.
- Media streaming through the local range server.
- Video and audio playback infrastructure through libmpv.

## Requested Features Still Pending

### Frontend Media Editor

- Crop before sending photos.
- Rotate before sending photos.
- Draw on photos.
- Add text overlays to photos.
- Add stickers to photos.
- Edit photo captions before send.
- Multi-photo gallery selection and album grouping.
- Video trim before send.
- Video crop and rotation.
- Video mute toggle.
- Video text/sticker/drawing overlays.
- Camera button for taking a photo or video directly.
- Full multi-file drag-and-drop send flow.
- Proper GIF-to-MP4 conversion for sending.

### Status Frontend

- Status creation picker sheet (Text, Layout, Voice, Camera, Gallery,
  Mentions).
- Voice status recording and sharing.
- Music picker interface for adding background music/stickers.
- Sticker & element sheet in the status editor (stickers, shapes, GIFs).
- Text formatting mode (color palette, font options, alignment).
- Video status trim & edit (timeline trimmer, audio toggle, caption).
- Text status creation mode (background color editor).
- Status viewer list UI with names and avatars.
- Status delete/reply controls in the viewer.
- Status audience/privacy picker directly in the Status tab.
- Video playback in the status viewer.
- Voice-status playback in the status viewer.
- Status media save dialog improvements.
- Full status text editor with color/font controls.

### Sticker and GIF Frontend

- Diagnose and fix the sticker tab becoming the default/stuck tab.
- Diagnose sticker rows failing to load inside chats.
- Sticker search UX improvements.
- GIF search and send flow.
- GIF playback and receive rendering validation.
- Sticker creation from image.

### Channels Frontend (remaining)

- Channel reaction UI.

### Chat and Contact UX (remaining)

- Archived-chat button and dedicated archived-chat page/button flow.
- Status avatars in the status tab.
- DP/profile-photo save affordance in the profile viewer.

### Settings, Privacy, and Account

- Username support and editing (blocked by upstream whatsmeow).
- Account settings: passkeys, password, email, two-step verification,
  security notifications, change number, request account info, delete account.
- Privacy settings: last seen, profile photo, about, status visibility,
  read receipts, disappearing messages, group/live location, call options
  (silence unknown callers), blocked contacts, app lock, chat lock, camera
  effects, advanced privacy.
- Chat settings: theme, default chat theme, "Enter is send", media
  visibility, font size, sticker suggestions, "keep chats archived".
- Storage & data settings: storage management, network usage metrics,
  "use less data for calls", proxy, media upload/download quality,
  auto-download rules per network.
- Live SQLite encryption at rest (blocked by SQLCipher).

> The Channels Frontend, Polls Frontend, Contact/Location Sharing, Gallery
> Filters, Logs Tab, and the chat info filter / location map / contact
> message-button items listed above were implemented in this session. See the
> "Implemented Features" section above for details.

### Calls

- Voice-call initiation from a chat header.
- Video-call initiation from a chat header.
- Voice/video-call initiation from the Calls tab.
- Call contact picker (up to 32 people, scheduled calls, keypad dialer).
- Schedule call form (title/description, start/end times, call type,
  reminders).
- In-call audio screen with speaker/video/mute/screen-share/end controls.
- Ongoing voice chat banner with join button in the Calls tab.

These remain blocked by the upstream media-stack limitation described above.

### Tools / Business Hub Tab

- Business hub landing screen with 7-day performance metrics.
- Ad creation banners and business growth tools (Catalog, Advertise,
  Business broadcasts, Payments).
- Chat organization tools (Lists, Greeting message, Away message,
  Quick replies).
- Business account management and tutorial videos.

### Hidden Updates

- Hidden/muted status updates list screen.

### Miscellaneous

- Undecryptable-message retry action using whatsmeow's phone-request API.
- Complete channel/newsletter posting/admin support.
- Live database encryption with SQLCipher.
- Import/export settings UI.
- Full business commerce/catalog operations.
- Poll voter-detail overlay (who voted what) in the poll bubble.

## Verification

The daemon was verified with:

```sh
GOMAXPROCS=2 go -C whatevrd build ./...
GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...
```

The full Qt frontend was built successfully with:

```sh
cmake -S whatkevr -B build/debug/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build build/debug/whatkevr
```

Both builds were green with no warnings or errors.

### Release Build and Installation

The app was built in release mode and installed to `/home/admin/.local`
(user-writable prefix; `/usr/local` was not writable without sudo):

- `whatevrd` → `/home/admin/.local/bin/whatevrd`
- `whatkevr` → `/home/admin/.local/bin/whatkevr`
- `whatevrd.service` → `/home/admin/.local/lib/systemd/user/whatevrd.service`
- `whatevrd.socket` → `/home/admin/.local/lib/systemd/user/whatevrd.socket`

Smoke test: the daemon started, created the protocol socket at
`$XDG_RUNTIME_DIR/whatevr/whatevrd.sock`, and shut down cleanly:

## Screenshot Notes

Screenshots were supplied during the session but could not be inspected in this
environment: the model in use does not support image/PDF input. Feature
requirements were therefore derived from the codebase itself and the
session transcript, not from screenshot content. No personal information from
screenshots was used.

## Debugging Notes

Current daemon log path:

```text
~/.cache/whatevrd/whatevrd.log
```

Recent log lines are available through:

```json
{"id":1,"method":"daemon.logs","params":{"limit":200}}
```

The live logs observed during the session contained expected media-expiry
messages such as “media is no longer available on WhatsApp”. These indicate
WhatsApp CDN expiry and should be handled with media-retry receipt support in a
future pass.

## Privacy

No personal information from screenshots or local chats is recorded in this
document. No names, phone numbers, chat IDs, profile photos, message text, or
private media are intentionally included.
