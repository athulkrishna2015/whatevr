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
    group.Say(asha, "READY-HARNESS: stable synthetic conversation", wamock.Ago(3*time.Hour))
    group.SayFromMe("this side is the account itself", wamock.Ago(2*time.Hour))

    w.After(3*time.Second, func() { group.Say(ravi, "live", time.Now()) })
}
```

Anything said while the world is being built is backlog: it reaches the daemon
as an offline sync the moment the frontend logs in, so it is already there when
the first frame draws. Anything said from an `After` callback goes out live, on
the wire, while a frontend watches. That second kind is the thing no static
fixture can produce.

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
with its own identity key and its own `memSignalStore`; the first message from
one runs an X3DH against that bundle and comes out as a `pkmsg`, and everything
after it rides the ratchet as a `msg`. `padMessage` is whatsmeow's v2 padding,
reimplemented because it is unexported there.

Two things about addressing are worth knowing, because both fail silently:

- **The account's own device does not get to pick its identity key.** At pair
  time whatsmeow writes the account signature key down as the identity of
  `<account>@lid` device 0. A message from the account's other device has to be
  signed with that same key or the client reports the account as having been
  taken over.
- **whatsmeow decrypts under a LID whenever its store knows one**, and the one
  it always knows is the account's own. So messages from the account are
  encrypted to a LID address and messages from everyone else to a phone number.

Contacts stay on phone numbers rather than LIDs on purpose. The mock never
hands out a LID mapping for them, so the daemon keeps addressing them by
number, which is what makes a mock account readable.

Group chats are announced with a `w:gp2` create notification before their first
message. Without it the daemon has nothing to name them: it deliberately skips
its group info lookup for messages that arrive in an offline sync, and the
names a real account gets at that point come from a history sync.

## Scope

Stages 0 and 1 are in: handshake, pairing, login, a world model, scenarios,
inbound messages in direct and group chats, group metadata and contact
lookups.

Still to come, in the order they are planned: sending and receipts, history
sync, app state, media. Until then an unhandled info query is answered with an
empty result and logged as `unanswered iq xmlns=...`, which is the running list
of what is left to build.

Two gaps follow from that and are visible in the UI today:

- **Direct chats are named by phone number, not by contact name.** The daemon
  prefers a saved address-book name and falls back to the number; saved names
  only ever arrive through history sync or app state, so they land in later
  stages. Push names do work, which is why a group shows `~Asha` as a sender.
- **Pin, archive, mute and unread state are not scripted yet.** They are app
  state, so they arrive with it.
