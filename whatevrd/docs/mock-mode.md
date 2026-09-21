# Mock mode: a fake WhatsApp server

`whatevrd --mock <scenario>` runs the **real daemon** against a fake WhatsApp
Web server instead of WhatsApp. Nothing above the network is stubbed: the
protocol server, the view engine, the sort keys, the sqlite store, `internal/wa`
and whatsmeow itself are all production code. What is fake is the thing on the
other end of the websocket.

This is what frontend work runs against. No phone, no pairing dance, no history
sync wait, and the same scenario produces the same account every time.

    just build-mock
    build/mock/whatevrd --mock-list
    build/mock/whatevrd --mock empty

## Why it is behind a build tag

The mock overwrites `whatsmeow.WACertPubKey`, the pinned key whatsmeow uses to
verify that it is really talking to WhatsApp, and it points
`http.DefaultTransport` at a private CA. Both are process-global. A binary that
can do that is a binary that can be argued into trusting the wrong server, so
the entire `internal/wamock` package is compiled only under
`-tags whatevr_mock`, and `cmd/whatevrd/mock_disabled.go` makes a release build
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

Point a frontend at the socket that prints on startup. The tree is cleared on
each run; `--mock-keep` keeps it, and `--mock-dir` puts it somewhere else. A
directory is only ever cleared if it carries the `.whatevr-mock` marker this
binary wrote, so a mistyped `--mock-dir` fails instead of deleting something.

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

`--mock-scan-delay` is the one worth knowing about: it is how you get a QR to
sit on screen long enough to look at, instead of pairing instantly.

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

The rest of the model: `Contact` (saved by default, `Unsaved()` for somebody who
is not in the address book), `DM`, `Group`, `Chat.Pin`, `Chat.Archive`,
`Chat.Mute`, `Chat.Unread`, `Chat.Typing`, `World.SetOnline`, `World.Receipts`
for how fast a sent message goes to two ticks and then blue, and
`World.HistoryPace` for how slowly the initial sync arrives.

Timestamps are relative (`wamock.Ago`) so day dividers land in the right place
whatever day the scenario runs on.

Register in `scenarios_builtin.go`; `--mock-list` prints the registry. The
strings `READY-HARNESS` and `Visual Test Group` in the `visual` scenario are
load-bearing: `scripts/whattui-screenshot` waits for them on screen, so
renaming either breaks the harness rather than the scenario.

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
names ride along in their own `PUSH_NAME` chunk.

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

## Scope

Stages 0 to 3 are in: handshake, pairing, login, a world model, scenarios,
inbound messages, outbound messages with the full receipt lifecycle, typing and
presence, group metadata, contact lookups, history sync, contact names,
profile pictures, and app state.

Every info query the daemon makes is answered. An unhandled one is still
answered empty and logged as `unanswered iq xmlns=...`, which is the running
list of what a new daemon feature needs.

Still to come: media (`w:m` upload and ranged download of real attachments) and
stickers. A sticker pack catalogue fetch currently answers with an empty list.

One thing the mock surfaces rather than fixes: a saved contact's messages in a
group render as `~Asha` rather than `Asha`. Both names land in the same
`senders.name` column, and the daemon processes history sync chunks ordered by
sync type, so the `PUSH_NAME` chunk always lands after the address book and
overwrites it. A real account does the same thing. Fixing it is a daemon
change, not a mock one.
