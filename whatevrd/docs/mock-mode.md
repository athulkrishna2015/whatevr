# Mock mode: a fake WhatsApp server

`whatevrd --mock <scenario>` runs the **real daemon** against a fake WhatsApp
Web server instead of WhatsApp. Nothing above the network is stubbed: the
protocol server, the view engine, the sort keys, the sqlite store, `internal/wa`
and whatsmeow itself are all production code. What is fake is the thing on the
other end of the websocket.

This is what frontend work runs against. No phone, no pairing dance, no history
sync wait, and the same scenario produces the same account every time.

    just build
    build/debug/whatevrd --mock-list
    build/debug/whatevrd --mock empty

Every debug build carries it. Release builds do not, and that is the whole
point of the next section.

## Why it is behind a build tag

The mock overwrites `whatsmeow.WACertPubKey`, the pinned key whatsmeow uses to
verify that it is really talking to WhatsApp, and it points
`http.DefaultTransport` at a private CA. Both are process-global. A binary that
can do that is a binary that can be argued into trusting the wrong server, so
the entire `internal/wamock` package is compiled only under
`-tags whatevr_mock`, which `just build` passes and `just build-release` does
not, and `cmd/whatevrd/mock_disabled.go` makes a release build
refuse `--mock` outright.

`scripts/check-mock-gate` is what keeps that true. It asserts the package is
absent from the release dependency graph, present in the mock one, and that a
release binary rejects the flag. It runs in `just test` and in CI.

## Where state goes

Mock mode repoints the whole XDG triple at a scratch tree, so the socket, the
lock, the app database and the whatsmeow session all land inside it and a mock
run can never open a real account:

    /run/user/$UID/whatevr-mock/<scenario>/
      run/whatevr/whatevrd.sock
      data/whatevrd/whatevrd.db
      data/whatevrd/session/whatsmeow.db
      cache/whatevrd/

Point a frontend at the socket that prints on startup. Both take it as a flag:

    sock=$XDG_RUNTIME_DIR/whatevr-mock/visual/run/whatevr/whatevrd.sock
    build/debug/whatkevr/bin/whatkevr --socket "$sock"
    build/debug/whattui --socket "$sock"

A whatkevr started with `--socket` does not take the single-instance lock, so it
runs beside a whatkevr on the real account instead of just raising its window.

The tree is cleared on each run; `--mock-keep` keeps it, and `--mock-dir` puts
it somewhere else. A directory is only ever cleared if it carries the
`.whatevr-mock` marker this binary wrote, so a mistyped `--mock-dir` fails
instead of deleting something.

Unix socket paths cap out at 108 bytes. A deep `--mock-dir` fails at bind with
`invalid argument`; keep it short.

## Flags

| Flag | Meaning |
|---|---|
| `--mock <name>` | run the named scenario |
| `--mock-list` | print the scenarios and exit |
| `--mock-dir <path>` | scratch tree location |
| `--mock-seed <n>` | seed for every key and identifier the mock generates |
| `--mock-scan-delay <d>` | how long a QR sits unscanned before the mock phone pairs |
| `--mock-history-delay <d>` | how long between history sync chunks |
| `--mock-phone <digits>` | the number the mock account answers as |
| `--mock-keep` | keep existing scratch state |
| `--mock-now <rfc3339>` | pin the clock every scenario timestamp hangs off |
| `--mock-control <path>` | bind the quiescence socket here |
| `--mock-notify` | let a mock run raise desktop notifications |

`--mock-scan-delay` is the one worth knowing about: it is how you get a QR to
sit on screen long enough to look at, instead of pairing instantly.

Desktop notifications are off in mock mode unless asked for. A scenario is
synthetic traffic and a stress scenario is hundreds of messages a second;
a toast for any of it is wrong, and the frontends are what the mock exists to
exercise anyway.

## How a connection goes

The pieces, in the order the client meets them:

1. **Transport.** `installTransport` replaces `http.DefaultTransport`.
   whatsmeow's `NewClient` builds its websocket, pre-login and media clients by
   cloning it, so all three are captured without `internal/wa` changing. Any
   host that is not a WhatsApp one is refused at dial time rather than reaching
   the internet.
2. **Noise.** `noise.go` is the responder half of
   `Noise_XX_25519_AESGCM_SHA256`, mirroring whatsmeow's `doHandshake` step for
   step. It cannot reuse `socket.NoiseHandshake` directly because that type's
   salt is unexported and its `Finish` expects a client-side `FrameSocket`.
3. **Certificate.** `certs.go` fabricates the two-level `CertChain` that
   `verifyServerCert` walks. `notAfter` is not optional: whatsmeow reads an
   unset field as 1970 and calls the certificate expired.
4. **Pairing.** The client turns our `<ref>` into a QR containing its *own*
   noise key, identity key and adv secret. The mock plays the phone: it reads
   the code from the daemon's login events, because the adv secret exists
   nowhere else, then signs an `ADVSignedDeviceIdentity` as the primary device.
5. **Restart.** After `pair-success` the real server sends
   `<stream:error code="515">` rather than hanging up, and whatsmeow has a
   branch that reconnects on it. Closing the socket instead leaves the daemon
   parked forever, waiting for a signal that never comes.
6. **Login.** On the new stream the client sends a username payload, gets
   `<success>`, and `isLoggedIn` flips. It then uploads prekeys, which is also
   how the server learns the keys it needs to encrypt anything back.

## Two framing traps

Both cost an afternoon, so they are worth writing down.

`waBinary.Marshal` **already emits the leading flag byte** that
`waBinary.Unpack` strips, and `waBinary.Unmarshal` does not expect it. So a
frame is `Marshal` to send and `Unpack` then `Unmarshal` to read. Adding a flag
byte of your own corrupts every node.

The one-off `WA\x06\x03` header only prefixes the client's very first frame,
and a WhatsApp frame is not the same thing as a websocket message: frames split
and coalesce across them freely.

## Scenarios

A scenario is Go, not data. It gets a `*wamock.World` and describes an account:

```go
func buildVisual(w *wamock.World) {
    asha := w.Contact("917770000001", "Asha")
    ravi := w.Contact("917770000002", "Ravi")

    group := w.Group("Visual Test Group", asha, ravi)
    group.History(asha, "from before this device existed", wamock.Ago(30*time.Hour))
    group.Say(asha, "READY-HARNESS: stable synthetic conversation", wamock.Ago(3*time.Hour))
    group.SayFromMe("this side is the account itself", wamock.Ago(2*time.Hour))
    group.Pin()

    w.After(3*time.Second, func() { group.Say(ravi, "live", time.Now()) })
    w.OnSend(func(m *wamock.Msg) { m.Chat.Reply(ravi, "got it") })
}
```

Four ways in, and the difference between them is the delivery path, not the
wording:

| call | how it reaches the daemon |
|---|---|
| `History` | a real history sync: encrypted blob, media download, zlib, protobuf |
| `Say` before login | an offline sync, so it is there before the first frame draws |
| `Say` after login | live, on the wire, while a frontend watches |
| `OnSend` | the answer to something the account just sent |

`Attach`, `AttachFromMe` and `AttachHistory` are the same three paths for media:

```go
group.Attach(asha, wamock.Image("look at this"), wamock.Ago(2*time.Hour))
group.AttachFromMe(wamock.Voice(7*time.Second), wamock.Ago(time.Hour))
direct.AttachHistory(ravi, wamock.Document("report.pdf"), wamock.Ago(30*time.Hour))
```

The attachment kinds are `Image`, `Video`, `GIF`, `VideoNote`, `Voice`, `Audio`,
`Document` and `Sticker`, with `.Size(w, h)` for the visual ones.

The rest of the model: `Contact` (saved by default, `Unsaved()` for somebody who
is not in the address book), `DM`, `Group`, `Chat.Typing`, `World.SetOnline`,
`World.Receipts` for how fast a sent message goes to two ticks and then blue,
and `World.HistoryPace` for how slowly the initial sync arrives.

App state has its own set, and they work before login and during it:
`Chat.Pin`/`Unpin`, `Chat.Archive`/`Unarchive`, `Chat.Mute`/`Unmute`,
`Chat.MarkRead`/`MarkUnread` and `Msg.Star`/`Unstar`. Called while a frontend is
connected, each one becomes a real patch plus the notification that makes the
client fetch it, which is how a change made on the phone reaches a linked
device. `Chat.Unread(n)` is the other kind: a badge the account already had,
which rides the history sync rather than app state.

Timestamps are relative (`wamock.Ago`) so day dividers land in the right place
whatever day the scenario runs on.

Register in `scenarios_builtin.go`, or `scenarios_stress.go` for the ones that
exist to hurt; `--mock-list` prints the registry. The strings `READY-HARNESS`
and `Visual Test Group` in the `visual` scenario are load-bearing, and so are
`frames` and its `Reference` chat: the screenshot script and the whattui golden
frames both look for them by name. A guard test asks `--mock-list` whether they
are still there, so a rename fails loudly.

### The ones that exist to hurt

| scenario | what it is for |
|---|---|
| `frames` | fixed, quiet, history only: the account the golden frames are diffed against |
| `torture` | every text and layout case that breaks a renderer, each labelled |
| `flood` | four hundred chats, a three thousand message conversation, live bursts |
| `fuzz` | a different account at every `--mock-seed`, the same one at each |

`torture` is a test sheet. The corpus is in `nasty.go`: bidi overrides and
isolates, zero width joiner families, combining mark stacks, terminal escapes
(colour, cursor movement, screen clear, OSC 8 hyperlinks, OSC 52 clipboard
writes), nul bytes, a forty thousand character message, chat names with
newlines in them, three hundred member groups, twenty four messages at the same
millisecond, timestamps at the epoch and in the future, and attachments four
thousand pixels on one side and one on the other. Each sample is preceded by a
label from the account, as its own message, so a screenshot reads as a sheet
without changing how the sample beside it wraps.

`fuzz` draws from the same corpus with a seeded generator, so a crash is
reproducible from the one number in the log line. It cannot be a golden: use it
for long runs, and quote the seed when something falls over.

`buildFrames` is history only on purpose. A backlog delivered at login and a
history chunk describing the same chat are two pipelines racing, and the unread
badge is whichever lands last. That race belongs in a scenario somebody is
watching, not in the one the frames are diffed against.

## The quiescence barrier

`--mock-control <path>` binds a unix socket that answers one NDJSON request per
line. It exists so a test can ask whether the mock has finished talking instead
of sleeping long enough to be fairly sure.

| command | answer |
|---|---|
| `{"cmd":"sync"}` | blocks until nothing is queued, scheduled or being served, and the wire has been quiet for a moment |
| `{"cmd":"status"}` | the same question without the waiting |
| `{"cmd":"scenario"}` | the name, the seed and the pinned clock |
| `{"cmd":"list"}` | the registry |
| `{"cmd":"say","chat":"Asha","text":"..."}` | put a message in from outside the scenario |

`sync` is only the server's half. Whether a view has emitted `ready`, and
whether the daemon has finished ingesting what it was sent, are things only the
frontend can see, so a caller waits on both: the whattui harness syncs, then
waits for its collections to stop changing for 300ms.

## How a message is delivered

Every message goes through real Signal encryption, because whatsmeow will not
accept anything else.

The client uploads its prekey bundle right after login, and `prekeys.go` keeps
it rather than counting it. Each person in the world is a separate mock device
with its own identity key, its own signed prekey and its own `memSignalStore`;
the first message from one runs an X3DH against the client's bundle and comes
out as a `pkmsg`, and everything after it rides the ratchet as a `msg`.
`padMessage` and `unpadMessage` are whatsmeow's v2 padding, reimplemented
because they are unexported there.

Two things about addressing are worth knowing, because both fail silently:

- **The account's own device does not get to pick its identity key.** At pair
  time whatsmeow writes the account signature key down as the identity of
  `<account>@lid` device 0. A message from the account's other device has to be
  signed with that same key or the client reports the account as having been
  taken over.
- **Everything is addressed by LID.** whatsmeow rewrites the destination of
  every direct message to the LID and decrypts under one whenever its store
  knows the mapping, so a mock that hands out phone numbers cannot send at all.
  Every stanza the mock emits carries `sender_lid` or `participant_lid`, which
  is how the client learns the mapping before it has asked who anybody is. The
  mock's LID for a number is that number on `@lid`: unrealistic, and readable,
  which is the trade a mock should make.

Chat ids stay phone numbers regardless, because `info.Chat` comes from the
stanza's `from` and the mock writes the number there.

Group chats are announced with a `w:gp2` create notification before their first
message. Without it the daemon has nothing to name them: it deliberately skips
its group info lookup for messages that arrive in an offline sync, and the
names a real account gets at that point come from a history sync.

## Sending

A message the account sends is decrypted by the mock rather than counted.

A direct message arrives as one `<enc>` per device, so any peer that is not the
account will do. A group message is a single sender-key ciphertext for
everybody, with the key handed out inside the per-device copies, so opening one
takes both steps: decrypt somebody's copy, process the sender key distribution
message it carries, then decrypt the `skmsg`.

The `<ack>` carries `t`. whatsmeow reads the sent timestamp straight off it, and
a missing one stores every outgoing message in 1970.

Receipts follow on the clock `World.Receipts` sets, defaulting to delivered
after 400ms and read after 1.5s. A group message gets one receipt per member:
the daemon only moves a group message to two ticks once it has heard from all of
them, which is what WhatsApp's own tick semantics do.

## History sync

`Chat.History` puts a message in a real `waHistorySync` blob: zlib compressed,
AES-CBC encrypted under a media key, hosted on the mock's own `mmg.whatsapp.net`
and referenced from a `historySyncNotification` protocol message sent from the
account's own device. The daemon sets `ManualHistorySyncDownload`, so it does
the whole download, MAC check, decrypt and decompress itself.

This is also where contact names come from. `InlineContacts` on the first chunk
is what makes a chat say "Asha" rather than "+91 77700 00001", and the push
names ride along in their own `PUSH_NAME` chunk. Having both is what found the
`~Asha` bug: the daemon processes chunks ordered by sync type, so the push names
always landed last and buried the address book. Sender names now carry a source
and refuse to be downgraded, which is why an unsaved contact still shows as
`~Unknown Caller` while a saved one does not.

Chunks carry two conversations each so the `sync` view has progress to report.
`--mock-history-delay 1500ms` (or `World.HistoryPace`) spreads them out; the
`sync` scenario does it by default, because a sync that finishes before the
first frame draws is a UI state nobody can look at.

## App state

The mock encodes real app state patches with whatsmeow's own
`appstate.Processor`, running against a store that exists only in memory, so
what the client validates is the real MAC chain rather than something shaped
like one. The sync key is handed over in a protocol message at login.

Two things depend on it. The push name, without which whatsmeow refuses to send
presence at all and the `presence` view stays empty forever. And pin, archive
and mute, which the scenario declares and which round-trip: a pin made in the UI
is decoded, kept, and served back on the re-fetch whatsmeow does immediately
afterwards, so it survives a reconnect.

`EncodePatch` takes its hash state by value, so the hash it computes is thrown
away. The mock recovers it by running each patch back through the decoder,
which also checks the mock's own MACs before the client ever sees them.

A collection is answered empty until the client has the key. Serving patches
first costs a failed decode and a key re-request in the daemon's log; whatsmeow
re-syncs everything the moment the key lands, so nothing is lost by waiting.

## Media

Everything the mock attaches to a message is a real file, generated from a seed
so two runs produce identical bytes. A jpeg that decodes, a lossless webp
sticker, an animated gif, a wav with a shape to it, and a one page pdf. Video is
motion jpeg in a quicktime container: ffmpeg pulls a poster out of it and mpv
plays it, which is as close as the mock gets without carrying a video encoder.
`synth_test.go` checks each of them against a decoder that is not the one that
wrote it.

Files are hosted exactly as WhatsApp hosts them: AES-CBC under a key derived
from the message's own media key, with a truncated HMAC appended, and served
over ranges. Audio and video also carry a streaming sidecar, one MAC per 64 KiB
chunk, so `internal/mediastream` fetches and verifies pieces rather than falling
back to the whole file.

Sending works the other way round. `POST /mms/...` takes the encrypted bytes and
hands back a direct path; the key arrives later, inside the message that points
at the blob, which is the property real end to end encrypted media has. The mock
then opens its own upload with that key and logs if it cannot, so a broken
upload fails in the mock's log rather than as a mystery download error three
steps later.

That self-check found the one bug worth remembering here: `cbcutil.Decrypt`
decrypts in place, so verifying a blob without copying it first leaves the
hosted bytes as plaintext and every later download of them fails its own MAC.

The sticker store is not XMPP at all: three packs, eight stickers each, served
off `static.whatsapp.net/sticker` as the pack index, one pack's contents, and a
tray image. Tray art is png and the stickers themselves are webp, because that
is what the daemon writes each of them to disk as.

## Scope

All of it is in: handshake, pairing, login, a world model, scenarios, inbound
and outbound messages with the full receipt lifecycle, typing and presence,
group metadata, contact lookups, history sync, contact names, profile pictures,
app state in both directions, media of every kind, stickers, a pinned clock, a
quiescence barrier, and the whattui golden frames and screenshot harness both
running against a real daemon rather than a recorded fixture.

Every info query the daemon makes is answered. An unhandled one is still
answered empty and logged as `unanswered iq xmlns=...`, which is the running
list of what a new daemon feature needs.

One shortcut worth knowing: a client at version 0 for a collection asks for a
snapshot, and the mock answers with patches instead. The client processes those
as a full sync, which works but means whatsmeow drops the resulting events
unless `EmitAppStateEventsOnFullSync` is set. The daemon sets it for the one
collection where it matters.

## What it found

The mock exists to make states reachable, and the states it reached had bugs in
them. Each of these was a real daemon fault, not a mock one:

- a push name from one history sync chunk buried the address book name from
  another, so group transcripts read `~Asha` for somebody you had saved
- the account's own push name was tagged with the `~` marker that means "a name
  this person chose rather than one you saved", which only makes sense about
  somebody else
- `cbcutil.Decrypt` decrypts in place, so the mock's own upload self-check was
  overwriting hosted blobs with plaintext: the first download of a sent
  attachment worked and every one after it failed its mac
- a message that was empty, or only spaces, was declined by the text path and
  then tombstoned as a payload nobody had written code for
- two avatar refreshes of the same person shared one temp file name, so one
  could rename the other's half-written file into a content addressed path that
  claimed the bytes were whole
- history sync overwrote a chat's unread count with the phone's number, which
  discarded anything that had arrived live since the blob was made. Linking a
  device takes minutes and messages arrive throughout, so the badge for them
  vanished.
