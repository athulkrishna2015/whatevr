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

## Scope

Stage 0 covers the handshake, pairing and login. Inbound messages, receipts,
history sync, app state and media arrive in later stages; until then an
unhandled info query is answered with an empty result and logged as
`unanswered iq xmlns=...`, which is the list of what is left to build.
