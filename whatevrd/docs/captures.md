# Captures: record the real account, replay it through the mock

A capture is everything one account saw and did on the wire, kept so it can be
played back through wamock as many times as needed. The daemon is then
developed against what WhatsApp really sends instead of what we guess it
sends.

A capture holds the plaintext of every message, every media file it fetched,
names, numbers, avatars. It stays on the machine it was taken on. It never
goes in this repo, which is public, and neither does anything `scripts/replay`
or `whatevrd capture show` prints from one.

## Building

Captures are behind `-tags whatevr_capture`, like the mock is behind
`whatevr_mock`. `just build` (the debug build) has both. A release binary
refuses `--capture`, `--send-guard` and `whatevrd capture`, and
`scripts/check-mock-gate` keeps it that way.

## Taking one

    build/debug/whatevrd --capture first-sync

On the real server this does three things:

- **Its own linked device.** Data, cache and run logs move under
  `$XDG_STATE_HOME/whatevr/captures/first-sync/home`, the socket to
  `$XDG_RUNTIME_DIR/whatevr-capture/first-sync`. The first run pairs (scan the
  QR from the phone), so a capture always starts from a pairing and the daily
  driver's database is never touched. The daemon logs the
  `XDG_RUNTIME_DIR` to give frontends:

      XDG_RUNTIME_DIR=$XDG_RUNTIME_DIR/whatevr-capture/first-sync whattui

- **The send guard.** Every outward stanza (messages, read/played receipts,
  typing, calls, group and profile changes, app state writes) is refused
  unless it goes to this account or to +91 0000000000. The rules live in the
  fork, `whatsmeow/sendguard.go`. `--send-guard` alone turns the guard on for a
  plain run on the real database without recording anything.
- **The recording.** Every daemon run is one segment. Running the same
  `--capture` name again pairs nothing and appends the next segment, which is
  how "restart mid sync" or "offline while the phone changed things" gets
  captured.

`--capture` with `--mock <scenario>` records a mock run instead: no guard, no
own device, the capture lands in the mock's scratch state. That is what the
self-test does.

Stop it with Ctrl-C. Afterwards, unlink the device from the phone and delete
the directory once the capture is no longer needed.

## What is in it

    captures/<name>/
      capture.json          format, name, created
      segments/0001.jsonl   one daemon run, one record per line
      blobs/<sha256>        http bodies and decrypted media, by content
      home/                 the linked device itself (real-account captures)
      expected/             reviewed replay snapshots, see below

Record kinds, in the order they happened:

| kind | what |
|---|---|
| `start` | daemon version, the logx run id of the same run, mock scenario, guard |
| `account` | pn and lid with the device, push name, platform |
| `recv` | an inbound stanza, the exact bytes after noise and zlib |
| `send` | an outbound stanza, the same form |
| `decrypted` | the plaintext of one `<enc>`, the `recv` it came in (`ref`) and the signal address it opened under |
| `encrypted` | a plaintext this device encrypted, per recipient device |
| `media` | a decrypted media file, as a blob |
| `http` | one http exchange: method, url, range, status, a few headers, the body as a blob |
| `frontend` | a frontend connection opening, a request line, its response, closing |
| `conn` | whatsmeow connection events |

Bodies over 256 MiB keep only their size and hash. A segment cut short by a
crash reads up to its last whole line.

## Reading one

    build/debug/whatevrd capture list
    build/debug/whatevrd capture show first-sync --segment 2 --kind recv,decrypted

Stanzas print as xml, payloads as protobuf text.

## Replaying

    build/debug/whatevrd --mock-capture first-sync --mock-segment 1

The mock plays one segment at the client:

- It pairs on its own, as the recorded number, lid and device, then takes each
  websocket connection the capture had (each starts at `success`) in turn.
- Every recorded push goes out as it was, except `<message>`: each `<enc>` is
  encrypted again under the mock's own signal session for the address it
  opened under, `skmsg` included (it goes out pairwise). An `<enc>` the
  original client never opened goes out as junk, so the replay fails the same
  way and asks for the same retry. A contact whose recorded `pkmsg` shows a new
  identity key gets a new mock identity from then on.
- A push waits until the client has sent as many stanzas of each class as the
  original had by then (iq sets, receipts, messages, presence, ib). A class
  the client stays short on for `--mock-gate` is forgiven for the rest of the
  segment and counted as a gate timeout.
- A client query gets the recorded answer to the same question (ids,
  timestamps and child order do not count). Prekeys, pairing and pings stay the
  mock's own. A question the capture never saw is a miss: the mock makes up an
  answer and says so in the log.
- Http requests get the recorded body for the same method, url and range.
- The recorded frontend requests go to the daemon socket again, one
  connection per recorded one. Ids a response handed out (sent messages,
  subscriptions) are mapped to the replayed ones for every later request.
- Messages the client sends get new ids. Receipts, replies and anything else
  in a later push that names the old id gets the new one, and the server ack
  carries the recorded time.

Peer identities come from the seed and the address, so they survive the
daemon restart between segments.

## scripts/replay

    scripts/replay first-sync           every segment, compared with expected/
    scripts/replay --accept first-sync  write expected/ from this replay
    scripts/replay --self-test          capture mock scenarios, replay, compare

Per segment it runs a mock daemon on the capture in a scratch dir kept between
segments, waits for the replay to finish, takes `whatevrd mock snapshot` (every
view, every chat with its messages, pins, media, group and members, as their
final items), stops the daemon and checks `core.db` and the whatsmeow session
with `integrity_check` and `foreign_key_check`, and `core.db` for inputs that
did not fold.

A capture becomes a regression test when its snapshot has been read and
accepted. It stays opt-in: nothing runs a capture unless asked, and CI only
ever runs the self-test.

The self-test takes the `busy` scenario (one run) and `echo` (two runs with a
send each) with `--capture`, replays them, and requires the replay to make the
same whatsmeow events (by the run log's `event` lines) with no misses, gate
timeouts or junk. It is part of `just test` and CI.

## What it cannot do yet

- **The old core is not deterministic.** It folds history chunks on its own
  goroutine and stamps sent messages and system rows with its own clock, so two
  replays of one capture can differ in unread counts, previews and the times of
  sent messages. The self-test prints those view differences without failing
  on them. The new core has to take every time from an input or an injected
  clock and be order independent, then snapshots compare exactly.
- `msmsg` (bot) payloads are not replayed, they need the message secret.
- A body over the cap, or an http exchange that failed at capture time, is a
  404 in the replay.
- Frontend requests are replayed one at a time, each after the wire went quiet.

## What to capture

The list from the plan, each a capture or a segment of one: fresh pairing with
full history sync; restart mid sync; restart after sync; pins, stars, mutes and
archive changed on the phone while offline; edits, revokes, reactions, polls
and events arriving before their targets; request older history; stickers
(recents, favorites, packs); a big group; a LID-only contact; name changes;
media of every kind; undecryptable and a phone resend; logout from the phone.
