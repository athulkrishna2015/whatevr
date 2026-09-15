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

- Video playback in the status viewer.
- Voice-status playback in the status viewer.
- Status media save dialog improvements.
- Status audience picker directly in the Status tab.
- Status viewer list UI with names and avatars.
- Status delete/reply controls in the viewer.
- Full status text editor with color/font controls.

### Sticker and GIF Frontend

- Diagnose and fix the sticker tab becoming the default/stuck tab.
- Diagnose sticker rows failing to load inside chats.
- Sticker search UX improvements.
- GIF search and send flow.
- GIF playback and receive rendering validation.
- Sticker creation from image.

### Channels Frontend

- New Channels tab.
- Followed-channel list UI.
- Explore channel invite/link flow.
- Channel message feed page.
- Channel follow/unfollow/mute controls.
- Channel reaction UI.

### Chat and Contact UX

- Chat info filters for media, links, documents, contacts, locations, polls,
  audio, voice, and video.
- Archived-chat button and dedicated archived-chat page/button flow.
- Status avatars in the status tab.
- DP/profile-photo save affordance in the profile viewer.
- Business profile details page using `GetBusinessProfile`.
- Business catalog/order features where upstream APIs allow them.
- Username support and username editing. The pinned whatsmeow dependency does
  not expose a stable username setter or resolver API, so this needs upstream
  support or reverse engineering.

### Calls

- Voice-call initiation from a chat header.
- Video-call initiation from a chat header.
- Voice/video-call initiation from the Calls tab.

These remain blocked by the upstream media-stack limitation described above.

### Miscellaneous

- Undecryptable-message retry action using whatsmeow's phone-request API.
- Complete channel/newsletter posting/admin support.
- Live database encryption with SQLCipher.
- Import/export settings UI.
- Full business commerce/catalog operations.
- Poll voter-details frontend.
- Full location map preview and open-in-map actions.

## Verification

The daemon was verified with:

```sh
GOMAXPROCS=2 go -C whatevrd build ./...
GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...
```

The full Qt frontend was built successfully with:

```sh
just build-release
```

The release binaries were installed and socket activation was verified with a
protocol `hello` request. The daemon responded online with the feature commit
version.

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
