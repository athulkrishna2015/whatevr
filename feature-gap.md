# Whatevr Feature Gap Audit

> **Dated 2026-07-03, before the protocol migration finished.** The gRPC stack
> it audits against (`proto/whatevr.proto`, `whatevrd/internal/rpc/`) has since
> been deleted; the daemon now serves [PROTOCOL.md](PROTOCOL.md) on one socket.
> Read every "RPC" below as "daemon method". The *gaps* it lists are still
> accurate: they are missing features, not missing plumbing.

A comprehensive list of everything not yet implemented in whatevr, audited against the
**actual code**: `proto/whatevr.proto` (the full RPC surface), the daemon's
`internal/wa` / `internal/store` / `internal/notify` packages, the **whatkevr**
Qt/Kirigami frontend (QML + C++ controllers), and the public API of the pinned
whatsmeow (`v0.0.0-20260622185415-5f04eac6dbbb`) from the module cache.

Each item is one of: implementable with whatsmeow today (API named where relevant),
a standard app feature needing no whatsmeow, a **wired-but-unexposed** gap (daemon/
controller support exists, no UI entry point), a bug/rough edge, or an upstream
whatsmeow gap.

---

## 0. Baseline: what is ALREADY implemented (do not re-report these)

Verified in code, daemon + whatkevr:

**Account/connection:** QR login, logout, reconnect/backoff with retry status page,
history sync (initial, recent, on-demand older with per-chat exhaustion flag,
offline catchup, stall detection + "open phone" hint), daemon status page
(`StatusPage.qml`), systemd user units.

**Chat list:** pinned/archived sections, filters (Home / Direct messages / Groups),
unified search (chats + full-text messages + phone-number lookup via usync),
pin/archive chat, mute (8h / 1w / always / **custom duration dialog**), typing
indicator in the chat row, **draft indicator** ("Draft: …"), unread badges,
last-message preview with direction/status ticks, keyset pagination, self-profile
footer, starred-messages entry point.

**Conversation:** text/image/sticker bubbles (Lottie animated stickers via rlottie),
replies with quote preview + **jump-to-quoted-message** (with "not available"
toast), @-mentions (render + **mention autocomplete** with cached member list +
clickable mentions opening contact info), reactions (quick bar, full emoji popup,
chips, **ReactionDetailsDialog showing who reacted**), edit (with WhatsApp edit
window enforcement client- and daemon-side), revoke/delete-for-me (single + bulk),
forward to multiple chats (picker dialog), star/unstar, pin with durations
(24h/7d/30d) + pinned-messages banner + expiry handling, per-message info dialog
(per-participant delivered/read/played rows), **multi-select mode** (copy, copy as
Markdown, forward, reply, info, delete, select-all, whole-day toggle), context menu
(Reply/Edit/Forward/**Copy Text**/**Copy as Markdown**/Copy Link(s)/Copy
Image/Save As…/Star/Pin/Select/Info/Delete), date separators, **unread separator +
viewed-based read marking** (scroll-into-view + focus, not open), go-to-bottom FAB
with pending count, "Read more" long-message expansion + full-text dialog with
copy, in-chat search bar with match navigation (n of m, next/prev), per-chat
starred list, "Load older messages from phone" affordance, WhatsApp text formatting
**rendering** (bold/italic/strike/code spans etc. in `messagemarkup.cpp`),
URL linkification with real TLD table, image download-on-demand with streamed
progress circle + "Load image" button + auto-download prefs.

**Composer:** Enter-to-send (configurable to Ctrl+Enter), Shift+Enter newline,
**emoji autocomplete** (`:name:` suggestion mode) and mention suggestion bar,
emoji picker with categories/skin tone/recents, sticker picker (recents,
favorites, packs, search; favorite/unfavorite; install/uninstall packs), attach
image via file dialog, **paste image from clipboard** (`sendClipboardImage`),
image caption, reply banner, edit banner, per-chat **persistent drafts**
(optional), typing presence sent (composing).

**Info cards:** contact card (saved/push/business names, phone, about (fetched
async), business badge, avatar with full-res **ProfilePictureViewer**, "Message"
button), group card (subject, description, created date, member list with
**search**, admin/superadmin badges, member → contact card navigation with Back).

**Settings:** privacy (last seen/online/profile photo/about/read receipts/group
add/call add with correct audience constraints incl. "My contacts except…" as a
*choice*), blocked contacts list + **unblock**, notifications
(enable/preview/sound), auto-download toggles (photos/videos/audio/docs/stickers),
appearance (system/light/dark, KDE color schemes, density, message text size,
exact font size, chat-list avatars toggle), chats (enter behavior, drafts toggle,
**wallpaper: doodle pattern / custom SVG, scale, opacity, auto-tint, color**),
emoji skin tone + reset recents, storage (cache size, location, clear cache),
window layout (remember size/position, chat-list width), profile page (name,
about, phone, connection state, logout), keyboard shortcuts page, about page.

**Notifications (daemon-side D-Bus):** capability sniffing (actions, markup,
image), sender avatar image, message preview toggle, sound, click-to-open-chat
routed to the focused frontend via `HoldSession`/`OpenChat`, suppression for the
actively viewed chat.

Everything below is **missing** (or broken).

---

## 1. Receiving message types

**Closed 2026-08-20.** When this was written the daemon stored only
`Conversation`, `ExtendedTextMessage`, `ImageMessage` and `StickerMessage`, and
everything else fell into a grey tombstone or into nothing at all. Every entry
below is now struck through; each needed storage (a `MediaKind` plus, for most,
a payload object on the wire), a bubble or pill in whatkevr, and a chat-list
preview string. The per-entry notes say what landed and what deliberately did
not.

- ~~**Video**: playback, duration, thumbnail-first render (`VideoMessage`).~~
- ~~**GIFs**: `VideoMessage` + `GifPlayback`; auto-looping muted playback.~~
- ~~**Voice notes (PTT)**: `AudioMessage` with `PTT=true` ... Playback speed
  toggle (1×/1.5×/2×) in the bubble.~~
- ~~**Audio files**: `AudioMessage` with `PTT=false`.~~
- ~~**Documents**: `DocumentMessage`: filename, size, page count, MIME icon,
  open-with / save-as; plus the `DocumentWithCaptionMessage` wrapper.~~
- ~~**Video notes (PTV)**: round instant videos (`PtvMessage`).~~
  **Done 2026-08-13:** all six kinds now ingest into real rows
  (`videoMessageInput` / `audioMessageInput` / `documentMessageInput`), carry
  `duration_secs`, `size_bytes`, `filename`, `page_count`, `waveform` and
  `played` on the wire, and render as their own bubbles (`VideoBubble`,
  `VoiceBubble`, `DocumentBubble`) plus a full-screen `MediaViewer` and a
  per-chat gallery over the new `chat_media` view. Playback is libmpv, for
  audio and video alike, drawn into the scene graph through mpv's render API.
  Voice notes send a played receipt on first listen
  (`message.mark_played`), auto-advance to the next note, remember where you
  stopped, and get a daemon-derived waveform via ffmpeg when the sender omitted
  one. Media no longer waits for a whole-file download: `media.stream` serves
  ranged, chunk-verified bytes over a loopback endpoint while the fetch fills a
  sparse cache file, so a large video starts immediately and seeks anywhere;
  when the last chunk lands the file is hash-verified and becomes an ordinary
  cache entry. Note view-once video and audio deliberately stay tombstoned.
- ~~**Location**: `LocationMessage`: lat/long, name, address, embedded JPEG thumb;
  click → OSM/system maps.~~
- ~~**Live location**: `LiveLocationMessage` + its update stream.~~
  **Done 2026-08-14:** both ingest into real rows carrying `location` and, for a
  live share, `live`. The map is not the sender's thumbnail: the daemon fetches
  and stitches OSM raster tiles into its own cache (`wa/maps.go`, one preference
  to turn it off) and serves them through the ordinary download lifecycle, so a
  location gets a real map with a progress ring and an offline-first inline
  thumbnail. whatsmeow has no live-location correlation at all, so the daemon
  owns the grouping: updates are swallowed by an interceptor, ordered and deduped
  by `SequenceNumber`, and the trail is drawn as a polyline. Moving positions
  live in their own `live_locations` view, with a banner above the timeline while
  a share is running. The desktop-native part is the `geo:` handoff, which opens
  whatever holds the handler.
- ~~**Contact cards**: `ContactMessage` / `ContactsArrayMessage` (vCard parse,
  "Message" / "Add" actions).~~ **Done 2026-08-15:** vCards are parsed daemon-side
  into structured fields (phones with labels, emails, org, title, url, birthday)
  with the raw text kept for a `.vcf` export. The card resolves each number
  through the existing `contacts.check_phone` path, so a number that is on
  WhatsApp gets a real avatar and a Message action routed into the existing
  contact dialog; `ContactsArrayMessage` renders as a deck that expands.
- ~~**Polls**: render `PollCreationMessage(V2/V3)`, tally votes via
  `DecryptPollVote`, show voters, **vote** via `BuildPollVote`/`EncryptPollVote`,
  live result updates.~~ **Done 2026-08-16:** full two-way. Votes arrive as
  ordinary messages, are decrypted, mapped back to options and replace that
  voter's whole selection (a vote message is the current selection, not a delta);
  a vote for a poll we have not seen yet parks in `poll_votes_pending` and drains
  when the poll lands. `poll.vote` sends our own. The bubble fills each row with
  a bar and stacks the voters' avatars at its end, which is strictly more than
  WhatsApp shows, and history-sync polls are tallyable because the parse loop now
  stores message secrets `ParseWebMessage` skips.
- ~~**View-once media**: view-once photos/videos/voice notes render as *nothing*.~~
  **Done 2026-07-04:** whatsmeow's `UnwrapRaw` already unwraps the wrappers (sets
  `evt.IsViewOnce`); these now get an honest "View once, view on your phone"
  tombstone bubble. Actual view-once media display remains out of scope.
- ~~**Ephemeral wrapper**: `EphemeralMessage` is not unwrapped.~~ **Stale claim
  (verified 2026-07-04):** the pinned whatsmeow calls `evt.UnwrapRaw()` for both
  live messages and `ParseWebMessage`, so `EphemeralMessage` / `DeviceSentMessage` /
  `ViewOnceMessage*` / `DocumentWithCaptionMessage` arrive already unwrapped and
  ephemeral text/images render fine. The real gap was silent dropping of
  non-text/image/sticker payloads, fixed by the tombstone below.
- ~~**Group invites**: `GroupInviteMessage`: group preview + Join button
  (`JoinGroupWithInvite`).~~ **Done 2026-08-16:** the daemon resolves the invite
  through `GetGroupInfoFromInvite`, so the card shows the group's real subject,
  photo and member count rather than the sender's copy, and says when you are
  **already a member**, in which case the button reads Open and jumps to the
  chat. Expiry runs as a live countdown and the card greys out when it passes.
  `group.join_invite` joins.
- ~~**Event messages**: WhatsApp group events (`EventMessage`): time/place/RSVPs
  (`EncryptComment`/`DecryptComment`).~~ **Done 2026-08-17:** a calendar-leaf card
  with the time range, the venue drawn with the same map treatment as a location,
  the join link as a live button, and three RSVP chips with attendee avatars and
  a full responses dialog. This whatsmeow exports the poll and comment secret
  pairs but not the event-response one, so `wa/msgsecret_event.go` reimplements
  the derivation over the exported `hkdfutil` / `gcmutil`; it carries a known
  vector so an upstream change fails loudly. The desktop payoff is **Add to
  calendar**, which generates an `.ics` and hands it to the desktop (§18).
- ~~**Albums**: `AlbumMessage`: collage-cluster consecutive photos/videos.~~
  **Done 2026-08-18:** the daemon groups them. Children are stored as ordinary
  rows tagged with their parent, so each keeps its own id, media, download state
  and receipts, and the transcript simply excludes a child whose parent exists;
  an album whose header never arrived degrades to ordinary bubbles rather than to
  nothing. The bubble is an aspect-respecting mosaic (2 side by side, 3 as one
  large plus two stacked, 4 as a grid, 5+ with a "+N" tile) that opens into the
  viewer with the album as its gallery.
- ~~**Buttons / lists / templates / interactive** business messages: at minimum
  render their text content instead of dropping them.~~
- ~~**Order / product / catalog / payment / sticker-pack-share / call-log
  messages**: placeholder cards.~~ **Done 2026-08-19:** WhatsApp's four wire
  shapes for one idea (`ButtonsMessage`, `ListMessage`, hydrated `TemplateMessage`,
  `InteractiveMessage` including carousel recursion) are flattened daemon-side
  into one `interactive` object, so a frontend draws one card rather than four.
  URL and call buttons are genuinely live because they are only a link and a
  `tel:` handoff; a quick reply renders with a lock glyph and one honest line
  saying it needs the phone. Products, orders and payments share one commerce
  card with the money crossing as WhatsApp's integer plus its currency code,
  never pre-formatted. Sticker-pack shares are not a placeholder at all: they get
  an Add button wired to the existing `sticker_pack.install`. Call logs are a
  centered pill rather than a card, because nobody said them. Non-hydrated
  `HighlyStructuredMessage` still needs a template catalogue we do not have.
- ~~**Keep-in-chat** (`KeepInChatMessage`): honor + badge kept messages.~~
  **Done 2026-08-20:** an interceptor rather than a row: it flags the message it
  names and the transcript draws a bookmark next to the time. History sync reads
  the flag off `waWeb.KeepInChat`, since backfill never replays the control
  message. Honoring it *fully* needs the disappearing-message expiry sweep, which
  is §4 and still out of scope; the flag and the badge are honest on their own.
- ~~**Link previews (receive)**: `ExtendedTextMessage` title/description/thumbnail
  are ignored; render the preview card.~~ **Done 2026-08-18:** stored and rendered
  as a card above the words, in one of two layouts chosen by the sender's own
  preview type. Everything on it arrived inline, so the card costs no request and
  leaks nothing; the hi-res `ThumbnailDirectPath` fetch is deliberately left out
  for the same reason. Note this whatsmeow has no `canonicalURL`: the URL is
  `MatchedText`.
- ~~**"Unsupported message" tombstone**: unrecognized `waE2E.Message` types should
  produce a visible "Unsupported message, view on phone" bubble~~ **Done
  2026-07-04:** documents/video/audio/voice/location/contacts/polls/events/group
  invites/view-once now store a `MediaKindUnsupported` tombstone with a best-effort
  label ("Voice message", "Poll: …", "Document: name.pdf") shown in bubble + chat
  preview. Note: tombstoned rows do **not** self-upgrade when real rendering for a
  kind lands later (dedup by message ID). ~~`BuildUnavailableMessageRequest` fetch
  from phone still TODO.~~ **Done 2026-08-20**, see the undecryptable entry below.
  The unsupported path now also stores `media_payload`, so a future kind can
  upgrade its own tombstones if that is ever worth doing.
- ~~**System bubbles in history:** disappearing-timer changes, group
  join/leave/subject/photo/settings/admin changes (`events.GroupInfo` mutates
  state today but leaves no trace in the transcript), **`events.IdentityChange`**
  ("security code changed"), currently entirely unhandled.~~ **Done 2026-08-20:**
  seventeen event types now write `system` rows and render as centered pills with
  a per-type glyph. The daemon composes the sentence and bounds it to three names
  plus a count, so a forty-person add is one pill and never forty; a burst of
  separate events for one action folds into a single row. They are **quiet**: no
  reorder, no chat-list preview, no unread, because somebody joining a group four
  hundred messages ago must not raise a conversation to the top. The exception is
  an event that named you (added, removed, promoted, demoted, security code), and
  that decision is made once, in the daemon, and rides on the row. The pill's
  words are composed in the frontend from the flags so they can be translated,
  with the daemon's own sentence as the fallback for a type it does not know.
- ~~**Undecryptable placeholder retry**: `events.UndecryptableMessage` is stored,
  but there's no "Waiting for this message… request from phone" flow
  (`immediate/delayedRequestMessageFromPhone`).~~ **Done 2026-08-20:** the hole
  gets a `waiting` row that says it is a hole, counts down to the automatic
  attempt (`AutomaticMessageRerequestFromPhone` is on now), offers **Ask again**
  once that is spent (`message.request_from_phone` over
  `BuildUnavailableMessageRequest` + `SendPeerMessage`), and says plainly that
  the phone did not answer rather than shimmering forever. The resend carries the
  *same message id*, so this is the one place the dedup-by-id rule yields: the
  placeholder is replaced in place, keeping its position in the transcript and
  its unread accounting, instead of being dropped as a duplicate. A message the
  server marked hidden, and a view-once message, get no placeholder: promising
  either would be a wait that never ends.

**Section 1 is closed.** Every payload WhatsApp can deliver now renders as its
own bubble or pill, every one has a real chat-list preview, a poll can be voted
in and an event answered from whatevr, a group invite can be joined, a group's
changes leave a trail, and an undecryptable message resolves itself. What
deliberately stays out of scope, in the sections that own it: displaying
view-once media (above), the disappearing-message expiry sweep that would make
keep-in-chat mean something (§4), the security-code verification screen the
identity pill's Learn more would open (§10), and **sending** any of these kinds
(§2).

## 2. Sending message types & composer

Outgoing media is **images only** (`outboundImageExtension`: jpeg/png/gif/webp,
hardcoded `MediaKindImage`; whatkevr's attach dialog filters to images and only
`sendImage` exists). All of the below is `Upload` + `SendMessage` with the right
proto fields:

- **Send video** (caption, thumbnail generation; optional transcode helper).
- **Send GIFs properly**: GIF→MP4 with `GifPlayback=true`. ~~Today a `.gif` file
  is accepted and sent as a static image, active misbehavior.~~ **Partial
  2026-07-04:** `.gif` is now rejected daemon-side with a clear error and filtered
  from the attach dialog/clipboard path; the actual GIF→MP4 send remains TODO.
- **Send documents**: any file type; "send as document" (uncompressed) option
  for images.
- **Send audio files**; **record & send voice notes** (opus, waveform, duration),
  including the **"recording audio…" presence**: whatsmeow's `SendChatPresence`
  takes `ChatPresenceMediaAudio`, but the RPC only carries a `composing` bool, so
  the wire can't express it end-to-end.
- **Create sticker from image**: image→512×512 webp flow (library stickers send
  fine; ad-hoc creation missing).
- **Send location / contact cards / polls** (`BuildPollCreation`) /
  **view-once media**.
- **Multi-file send + album grouping**; multi-image selection in the file dialog.
- **Drag-and-drop files onto the chat**: paste-from-clipboard exists, a
  `DropArea` does not.
- **Link previews (send)**: fetch OpenGraph locally, fill `ExtendedTextMessage`
  preview fields.
- **Mentions in media captions** (mentions are text-path only:
  `SendMediaRequest` has no `mentioned_jids`).
- **Reply-to context for new media kinds**: `quotedMessageFromStored`
  reconstructs only sticker/image/text; extend per kind as they land.
- **Forwarded flag**: verify forwards set `ContextInfo.IsForwarded` /
  `ForwardingScore`, and render the "Forwarded" / "Forwarded many times" badge on
  receive (nothing in the proto models it).
- **Broadcast lists**: true broadcast send (whatsmeow `broadcast.go`).
- **Scheduled local sends**: daemon send-queue already persists pending
  messages; add a `send_at` column and it falls out.
- **Composer polish (no whatsmeow):** spellcheck (none today; Qt/Sonnet),
  formatting toolbar or Ctrl+B/I shortcuts that wrap selection in `*` `_` `~`,
  multi-line paste warning, attach-anything button, image paste preview dialog
  with caption before send (does paste send immediately? verify UX), emoji
  variation for recently-used sync with phone.

## 3. Message actions & in-chat UX

- ~~**Full-screen image lightbox for message photos**: `ProfilePictureViewer`
  exists for avatars, but clicking a message image opens nothing.~~ **Done
  2026-07-04:** clicking a downloaded message photo opens the full-screen viewer
  (reuses `ProfilePictureViewer`). Zoom/pan/gallery arrows still TODO.
- **In-chat media / links / docs gallery**: "Media, links and docs" per chat
  (pure local SQLite; links are already extractable by the markup engine).
- **Reaction with any emoji from the full picker** exists? The
  `ReactionEmojiPopup` exists; verify it exposes the complete emoji set, not
  just quick reactions; if so this is done, else finish it.
- **Copy selection of a bubble's text** (partial-text selection in bubbles) and
  "reply to selection".
- **Message translation hook** (local/offline or service-configurable).
- **Jump to date**: calendar scrubber in chat scroll.
- **Unread mentions badge** ("@" pill on the chat row when you were mentioned in
  unread messages; needs a small store flag, all local).
- **Search filters**: by sender, by media kind, date range (FTS store exists;
  extend query + UI chips).
- **Starred/pinned rows → jump into conversation context**: StarredMessagesPage
  and the pinned banner list items; verify each navigates via
  `showMessageInChat` (banner does; starred page should too).

## 4. Chat list & chat management

- **Mark chat as read (manual)** and **mark as unread**: the viewed-based
  auto-read is implemented, but there is no context-menu override; unread needs
  `appstate.BuildMarkChatAsRead(read=false)` (unused in whatsmeow) + a store
  flag.
- **Delete chat**: `appstate.BuildDeleteChat` (syncs to phone) + local purge +
  confirmation. No RPC exists.
- **Clear chat** (keep the row, wipe messages): local wipe + appstate clear.
- **Exit group from the chat row** (see §5) and **Block from the chat row/card**
  (see §10).
- **Unread-only filter chip** and **archived-list unread badge**; "keep chats
  archived" toggle (WA setting: archived chats stay archived on new messages;
  verify current daemon behavior and expose the choice).
- **Favorites filter**: WA favorites sync via appstate `favorites` patches;
  worst case implement locally.
- **Chat-list previews for every new media kind** (🎤 0:12, 📄 name.pdf, 📍
  Location, 📊 poll question…); falls out of §1 but the preview strings live in
  the daemon's chat store, list it so it isn't forgotten.
- **Per-chat notification override** (mute is synced; a local "notify without
  sound / hide preview for this chat" tier is UI-only).
- **Per-chat wallpaper**: global wallpaper/pattern engine already exists
  (`ChatWallpaper.qml`); add a per-chat key.
- **Disappearing messages**: per-chat timer (`SetDisappearingTimer`), default
  for new chats (`SetDefaultDisappearingTimer`), local expiry sweep, timer state
  in the header. ~~System bubbles (§1).~~ The pills landed with §1 on 2026-08-20,
  and so did the keep-in-chat flag; **the expiry sweep is what is left**, and it
  is what would make both mean something: nothing local ever actually disappears
  today.
- **Chat lock / hidden chats** (local PIN/keyring).
- **Export chat** to txt/zip with media (local).
- **Labels**: WhatsApp labels: `BuildLabelChat` / `BuildLabelMessage` /
  `BuildLabelEdit` all exist in whatsmeow appstate and are unused; incoming
  `events.LabelEdit` / `events.LabelAssociation` are not handled.

## 5. Groups: the single biggest gap (whatsmeow has 100% of this, zero RPCs exist)

The group card is view-only. All directly available in whatsmeow:

- **Create group**: `CreateGroup` (name, members, photo); "New group" flow in
  the sidebar.
- **Add/remove members, promote/demote admins**: `UpdateGroupParticipants`;
  actions on the member list (the UI skeleton of member rows, search and admin
  badges already exists in `ContactInfoDialog`).
- **Leave group**: `LeaveGroup` (+ render "you left", disable composer).
- **Edit subject/description/photo**: `SetGroupName`, `SetGroupTopic`/
  `SetGroupDescription`, `SetGroupPhoto` (nil clears).
- **Group permission settings**: `SetGroupAnnounce` (admins-only send: also
  needs composer lockout when announce is on and you're not admin; currently
  nothing checks this), `SetGroupLocked`, `SetGroupMemberAddMode`,
  `SetGroupJoinApprovalMode`.
- **Invite links**: `GetGroupInviteLink(reset)` show/copy/revoke; **join via
  link** `JoinGroupWithLink` with preview (`GetGroupInfoFromLink` /
  `GetGroupInfoFromInvite`); detect chat.whatsapp.com links in message text and
  offer join.
- **Join requests**: `GetGroupRequestParticipants` +
  `UpdateGroupRequestParticipants` (approve/reject) with a pending-requests
  badge for admins.
- **Groups directory**: `GetJoinedGroups` browser.
- **Member actions**: message privately (EnsureDirectChat exists, wire it),
  make/dismiss admin, remove.
- **Owner + "created by" in the card** (`GroupInfo.OwnerJID` is available;
  created time is already shown).
- **Mention-all helper** for admins (compose `@` for every member).

## 6. Communities

Nothing exists. whatsmeow: `GetSubGroups`, `GetLinkedGroupsParticipants`,
`LinkGroup`/`UnlinkGroup`. Community list, linked-group browser, announcement
group rendering, community info card.

## 7. Channels (newsletters)

Nothing exists; whatsmeow has a full API: `GetSubscribedNewsletters`,
`GetNewsletterInfo(WithInvite)`, `GetNewsletterMessages` +
`NewsletterSubscribeLiveUpdates` + `GetNewsletterMessageUpdates` (view counts),
`FollowNewsletter`/`UnfollowNewsletter`, `NewsletterSendReaction`,
`NewsletterToggleMute`, `NewsletterMarkViewed`, `CreateNewsletter`,
`UploadNewsletter(Reader)` (note: channel media is plaintext uploads), channel
comments via `DecryptComment`/`EncryptComment`. Needs its own sidebar section, a
feed view, and reaction UI.

## 8. Status / Stories

Nothing exists. Statuses arrive as ordinary `events.Message` from
`status@broadcast` (currently dropped or misfiled; **verify they don't pollute
the chat list**):

- Status tab: contact rings, auto-advance viewer for text (bg color/font),
  photo, video, audio statuses; mark-viewed receipts.
- **Post status**: send to `status@broadcast`, audience from
  `GetStatusPrivacy`; text-status composer with backgrounds.
- Status replies (DM with quoted status context), mute someone's status.

## 9. Calls

Zero handling: `events.CallOffer` / `CallOfferNotice` / `CallTerminate` /
`CallReject` are not subscribed, so **an incoming call shows nothing at all**:

- Incoming-call notification ("answer on your phone"), missed-call chat-list
  entry / call log (local persistence).
- **Reject from desktop**: `RejectCall` exists in whatsmeow.
- Actually answering/placing calls is upstream-blocked (§17).

## 10. Contacts, profile & account

- ~~**Edit own About text**: wired but unexposed.~~ **Done 2026-07-04:** About is
  editable on the profile page via the existing `SetProfileStatus` plumbing.
- **Set own profile photo**: the `w:profile:picture` IQ with self target
  (whatsmeow's `SetGroupPhoto` path); add change/remove on the profile page.
- **Set own push name**: `appstate.BuildSettingPushName` exists in the pinned
  whatsmeow. ~~The proto comment on `SetProfileStatus` claiming the display name
  is not settable via whatsmeow is outdated, fix comment~~ (comment fixed
  2026-07-04); the RPC + UI implementation is still TODO.
- **Block/report from the contact card and chat**: ~~wired but unexposed: add
  Block on the contact card (with confirm)~~ **Done 2026-07-04:** Block/Unblock
  (with confirmation) on the contact card. Block+report-spam still TODO
  (whatsmeow has reporting-token plumbing).
- **Contact list / "New chat" picker**: browse the synced address book
  (`Store.Contacts.GetAllContacts`) instead of only searching existing chats and
  typing full phone numbers.
- **Shared groups in common** on the contact card (local join across group
  participant store).
- **Last seen / online in the contact card and chat header**: presence events
  are already consumed for the conversation; surface availability + last-seen
  line in the header and card.
- **Contact QR**: show mine (`GetContactQRLink`), resolve scanned/pasted ones
  (`ResolveContactQRLink`); handle `wa.me/...` links in search and message text.
- **Security/identity screen**: `GetUserDevices`, identity key comparison,
  40-digit code + QR verify. `events.IdentityChange` is handled as of 2026-08-20
  (§1): a "your security code changed" pill lands in the chat and its Learn more
  affordance is exactly the screen that does not exist yet.
- **Business profile detail**: `GetBusinessProfile`: category, address, hours,
  website, catalog link (card shows only a "Business account" badge today).
- **Meta AI bots**: `GetBotListV2` / `GetBotProfiles` (label those chats
  properly at minimum).

## 11. Privacy, security & settings

- **"My contacts except…" exception picker**: the audience is selectable in
  `PrivacyAudienceCombo` but there is **no UI or RPC to edit the exception
  list**. The one-IQ `contact_blacklist` protocol is documented in project
  memory (retired-fork notes) and works against upstream now.
- **Status privacy**: `GetStatusPrivacy` (pairs with §8).
- **Default disappearing timer**: `SetDefaultDisappearingTimer` (pairs with §4).
- **App lock**: PIN/keyring lock for the desktop app (local).
- **Encrypted DB at rest**: SQLCipher option for the daemon store (local).
- **Proxy support**: `SetProxy`/`SetSOCKSProxy`/`SetProxyAddress` + daemon
  config + settings UI (censored-network users).
- **Multi-account**: N whatsmeow clients over one sqlstore container; account
  switcher in the sidebar.
- **Local backup/restore** of the daemon DB + media (export/import bundle).
- **Storage management, deeper**: per-chat usage breakdown, auto-download size
  caps, auto-clear policy (page exists with only total + clear-all).
- **Temporary-ban / TOS screens**: `events.TemporaryBan` is handled
  daemon-side; verify whatkevr renders a real blocking screen; `AcceptTOSNotice`
  for TOS blocks.

## 12. Login & connection

- **Pair by phone number**: `PairPhone` (8-char code) as an alternative to QR;
  needed for headless daemon installs and accessibility.
- **Re-login without daemon restart** after `events.LoggedOut`; verify the
  LoginPage flow covers a mid-session logout-from-phone gracefully.
- **"Appear offline" mode**: `SendPresence(unavailable)` strategy toggle
  (notifications keep flowing to the phone); related: `SetPassive`.
- **Connection diagnostics**: keepalive latency history
  (`KeepAliveTimeout/Restored` already handled), socket state timeline on the
  status page.

## 13. Media pipeline & storage

- **Media retry protocol**: when media expired from WA servers, send
  `SendMediaRetryReceipt` and consume `events.MediaRetry` so the phone re-uploads
  (today an expired download is a dead error).
- **Thumbnail-first everywhere**: `DownloadThumbnail` for video/docs before the
  full blob.
- **Streaming playback**: play video/audio while `DownloadToFile` streams.
- **Content-hash dedup for all media** (stickers already dedup by SHA256; extend
  to images/video/docs across chats).
- **`DeleteMedia`** server-side cleanup after revoke.
- **EXIF strip toggle on image send** (privacy; local).
- ~~The auto-download prefs for video/audio/documents are **dead switches** until
  §1 lands those kinds; either wire them or hide them.~~ **Done 2026-08-13:**
  §1 landed those kinds, so every switch is live, gated by a 16 MiB ceiling
  above which nothing auto-fetches.

## 14. Notifications & Linux desktop integration

Current daemon notifier is solid (avatars, markup, sound, click-to-open). Missing:

- **Inline reply action**: `content.Actions` is only `{"default", "Open Chat"}`;
  org.freedesktop.Notifications inline-reply capability (`inline-reply` cap,
  `x-kde-reply` hints) lets you reply straight from the popup; add "Mark as
  read" action too.
- **Notification grouping/stacking per chat** and replace-on-update
  (`replaces_id`) instead of one popup per message.
- **DND / notification schedule / snooze-per-chat until…** (local).
- **System tray / StatusNotifierItem**: none exists; unread count badge,
  close-to-tray, middle-click quick actions.
- **Launcher badge**: Unity LauncherEntry D-Bus unread count on the taskbar
  icon.
- **Autostart toggle** in settings (systemd user unit exists; expose
  enable/disable).
- **Start minimized** option (WindowLayout page has an "On startup" group to
  extend).
- **Multi-window**: pop a conversation into its own window.
- **Global shortcuts audit**: only `StandardKey.Preferences` is a window-level
  `Shortcut`; add Ctrl+K chat switcher, Ctrl+F in-chat search, Alt+Up/Down
  next/prev chat, Ctrl+W close chat (a "Close Chat" action exists; verify its
  binding), Esc-to-clear stack; the KeyboardShortcutsPage lists only 5 entries.
- **XDG portals**: verify file dialogs go through portals for Flatpak
  readiness; "Show in folder" / "Open with…" on saved media.
- **MPRIS** for voice-note/audio playback once audio lands.
- **KRunner / GNOME Shell search provider**: fuzzy-find chats from the shell
  (search RPCs already exist; D-Bus shim).
- **Share portal target**: "Send via Whatevr" system-wide.
- **CLI companion** (`whatevrctl send/list/watch`) over the protocol socket,
  near-free and huge for scripting; `examples/shell-frontend.sh` is most of it
  already.
- **Packaging spread**: Flatpak/flathub manifest, RPM/deb CI artifacts (AUR
  exists).

## 15. whatkevr parity notes (frontend-only debt)

Where the daemon already provides everything and only QML/controller work
remains:

- ~~Edit **About** (§10), **Block** entry points (§10), both one-dialog jobs.~~
  **Done 2026-07-04.**
- ~~Image lightbox (§3).~~ **Done 2026-07-04** (basic viewer; zoom/pan TODO).
- Caption editing UI when pasting an image (verify paste → caption path).
- Read-receipts-off awareness: when the account disables read receipts, grey
  ticks should still show delivered correctly; verify MessageInfo/status
  rendering against privacy state.
- Accessibility pass: `Accessible.name` exists in places (sidebar); audit
  bubbles/menus; sticker `accessibility_text` is modeled; verify it reaches
  `Accessible.name` on sticker bubbles.
- RTL layout audit; translation catalog completeness (I18n wrapper exists).
- whatgevr (the Rust/GTK frontend) trails whatkevr on nearly all of §0; decide
  whether it's a showcase or a maintained peer, and say so in the README.

## 16. Bugs & rough edges (from this audit)

- ~~**Ephemeral wrapper not unwrapped**~~ **Stale (verified 2026-07-04):** whatsmeow
  `UnwrapRaw` already unwraps it on both live and history-sync paths (§1).
- ~~**View-once and captioned-document messages silently dropped**~~ **Done
  2026-07-04:** both arrive unwrapped and now render as labeled tombstones (§1).
- ~~**`.gif` sent as a static image** via the image path (§2)~~ **Done 2026-07-04:**
  rejected with a clear typed error (transcode-to-video path still TODO, §2).
- ~~**Outdated proto comment**: `SetProfileStatus` doc says push name isn't
  settable via whatsmeow~~ **Fixed 2026-07-04** (implementation tracked in §10).
- ~~**`events.DeleteForMe` / `DeleteChat` / `ClearChat` unhandled**~~ **Done
  2026-07-04:** phone-side delete-for-me hard-deletes the local row; delete chat /
  clear chat sync to the local store and open frontends.
- **`events.MarkChatAsRead` not in the handled-event list**: phone-side
  read/unread toggles may only partially sync (self-receipts vs appstate);
  verify unread counts after marking read on the phone while whatevr is closed.
- **Call events invisible** (§9).
- **`events.IdentityChange` unhandled**: no security-code-change notices, and
  no re-verify prompt (§1, §10).
- **Status broadcast messages**: confirm `status@broadcast` traffic doesn't
  create a bogus chat row (§8).
- **Deprecated `offset` in `ListChatsRequest`**: documented as skip/repeat-prone;
  remove once no frontend uses it.
- **SendMedia error surface**: non-image file paths should fail with a typed,
  user-explainable gRPC error; verify it's not a generic string.
- **Announce-mode groups**: composer isn't disabled when only admins may send
  (§5); sends will just fail server-side.
- **History-sync stall** has detection + hint; add a user-triggered retry
  (re-issue `BuildHistorySyncRequest`) instead of waiting on the phone.
- **Edit-window UX**: `canEditAt` hides Edit client-side and daemon enforces;
  also hide **Delete for Everyone** past WhatsApp's revoke window (server
  rejects late revokes; check the daemon surfaces that failure).

## 17. Upstream whatsmeow gaps (WhatsApp Web has it; whatsmeow does not)

Track separately; not buildable without upstream/reverse-engineering work:

- **Voice/video call media**: signaling events + `RejectCall` only; no
  SRTP/media stack, so answering/placing calls (and screen share) is impossible.
- **Payments**: render-only; no initiation API.
- **Catalog/cart/business commerce ops**: only `GetBusinessProfile` +
  message rendering.
- **Curated sticker-store browsing**: `FetchStickerPack` covers synced/shared
  packs only.
- **Newsletter admin/moderation ops**: partially covered MEX calls upstream.
- **Meta AI conversational features** beyond bot listing.
- **Usernames (@handles)**: rolling out in WA; watch upstream `usync`/JID work.
- **Companion media-quality (HD default) account setting sync**: no API.

## 18. Beyond-parity ideas (differentiators)

- **Local voice-note transcription** (whisper.cpp): desktop-only superpower;
  pairs with voice-note playback (§1).
- **Local semantic search** (embeddings) layered over existing FTS.
- **Rule engine / hooks**: "on message matching X from Y, run Z" + the
  `whatevrctl` CLI = the scriptable WhatsApp for Linux.
- **Headless relay mode**: daemon-only install forwarding to ntfy/email/Matrix
  when no frontend holds a session (SessionBus already knows).
- **Date/time detection → "add to calendar"** action on messages. **Partly done
  2026-08-17:** an `EventMessage` exports a real `.ics` and hands it to the
  desktop (§1). Detecting a date in ordinary prose is the half that remains.
- **Message statistics dashboard**: volume, response times, top emoji (local
  SQLite).
- **Archive exporter**: per-chat Markdown/HTML with inlined media.
- **Read-position sync across frontends**: HoldSession/focus plumbing exists;
  share viewed-state between whatkevr windows/instances.
- **Smart auto-download**: learn per-chat which media the user always opens.

---

*Generated 2026-07-03. Sources: `proto/whatevr.proto`; `whatevrd/internal/{wa,store,notify,rpc}`;*
*`whatkevr/src` (all QML pages/components, `AppController`, `messagemarkup`); whatsmeow*
*`v0.0.0-20260622185415-5f04eac6dbbb` public API (`Client` methods + `appstate` builders + `types/events`).*
