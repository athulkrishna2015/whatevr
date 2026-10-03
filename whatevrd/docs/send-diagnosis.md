# Diagnosing "Waiting for this message" on the recipient side

A recipient's phone shows *"Waiting for this message. This may take a while."*
when it received our encrypted message but could not decrypt it. The recipient
then sends **retry receipts**; whatsmeow answers them automatically by
re-encrypting the stored plaintext (rebuilding the Signal session / fetching
fresh prekeys as needed). The placeholder resolves only when one of those
retries succeeds, so a permanent placeholder means either the retries are not
being answered, or every re-encryption fails the same way. This runbook
pinpoints which.

Background facts (whatsmeow pinned at `v0.0.0-20260622185415`):

- The daemon sets `UseRetryMessageStore = true`: every outgoing message is
  buffered in the `whatsmeow_retry_buffer` table of the **session** sqlite for
  7 days, written before the wire send. A buffer miss falls back to the
  message in the daemon's own log (`internal/whatsapp/retry.go`), quietly.
- Retry receipts never reach daemon code; whatsmeow handles them internally.
  The recipient stops asking after 5 retries; we drop requests after 10.
- Paths (default XDG): the core `~/.local/share/whatevrd/core.db`, session DB
  `~/.local/share/whatevrd/session/whatsmeow.db`.

## 1. Run the daemon with debug logging

```sh
WHATEVRD_LOG_LEVEL=debug whatevrd
whatevrd logs -f module=whatsmeow
```

`WHATEVRD_LOG_LEVEL` sets the run file (`$XDG_STATE_HOME/whatevr/logs`),
which is at info by default; stderr stays at warn unless
`WHATEVRD_LOG_STDERR` says otherwise. The retry-success lines below are
debug-only, the failure lines show at warn.

## 2. Confirm the send actually left

A send that fails logs a warn `whatsapp: send` line with `chat`, `id`,
`attempt` and `final` (`internal/whatsapp/send.go`). Then check the outbox in
the core:

```sh
sqlite3 "file:$HOME/.local/share/whatevrd/core.db?mode=ro" \
  "SELECT o.id, o.queued_t, t.t, t.error, t.final FROM outbox o
   LEFT JOIN outbox_try t ON t.chat = o.chat AND t.id = o.id
   WHERE o.chat = '<jid>' ORDER BY o.queued_t DESC, t.t LIMIT 20;"
```

A message still in `outbox` with tries in `outbox_try` (for example an
untrusted-identity error) is a **send-side** failure: the recipient never got
anything, which is a different bug class from an undecryptable delivery. Stop
here and fix that instead.

## 3. Watch the retry receipt traffic

Grep the daemon log after the recipient shows the placeholder:

| log line | meaning |
| --- | --- |
| *(nothing retry-related at all)* | The recipient never asked, or we were offline when it did (`EnableAutoReconnect` is off; check the connection supervisor around that time). Zero retry receipts with a placeholder points at recipient-side session establishment, not our buffer. |
| `Failed to handle retry receipt for <chat>/<id> …: failed to get message from retry store: sql: no rows` | Buffer miss. The log fallback answers these; if this still shows, the message is in neither the whatsmeow buffer nor the daemon's log (check step 4). |
| `Sent retry #N for <chat>/<id> to <sender>` (DEBUG) | We answered. Repeated `#1..#4` with the placeholder still stuck means the recipient cannot decrypt *our re-encryptions either*: a session/identity problem; go to step 4. |
| `Failed to handle retry receipt …: failed to encrypt message for retry / failed to fetch prekeys / didn't get prekey bundle` | Re-encryption itself failing; usually connectivity or a server-side prekey problem for that device. |
| `Dropping retry request … internal retry counter is 10` | We gave up on that (sender, message) pair. |
| `Error decrypting message …: failed to decrypt prekey message: untrusted identity` | The peer's Signal identity changed (they reinstalled/re-registered WhatsApp). **This blocks both directions at once**: their messages fail to decrypt here, and our messages were encrypted for their dead pre-reinstall session, so their phone shows the placeholder. Auto-trust (the default) clears the stale identity and self-heals on the next message. If you opted into strict mode (`WHATEVRD_AUTO_TRUST_IDENTITY=0`) this stays broken until you clear the peer's rows from `whatsmeow_identity_keys`/`whatsmeow_sessions` with the daemon stopped. |

## 4. Inspect the session store (read-only!)

```sh
sqlite3 "file:$HOME/.local/share/whatevrd/session/whatsmeow.db?mode=ro"
```

```sql
-- Was the outgoing message buffered for retries at all?
SELECT chat_jid,message_id,format,length(plaintext),
       datetime(timestamp/1000,'unixepoch')
FROM whatsmeow_retry_buffer ORDER BY timestamp DESC LIMIT 20;

-- Signal sessions per recipient device (PN and LID forms):
SELECT their_id FROM whatsmeow_sessions WHERE their_id LIKE '<user>%';

-- Identity known/changed?
SELECT their_id FROM whatsmeow_identity_keys WHERE their_id LIKE '<user>%';

-- LID mapping (the alternate-JID lookup on retries depends on this):
SELECT lid,pn FROM whatsmeow_lid_map WHERE pn LIKE '<user>%';
```

No session rows for the recipient's devices after a send means session
establishment failed; missing `whatsmeow_lid_map` rows can make retries keyed
by the LID chat miss a PN-keyed buffer entry (the daemon fallback tries both).

## 5. Repro matrix

Send to the same recipient in each configuration and record steps 2–4:

| axis | cells | what a divergence means |
| --- | --- | --- |
| frontend | `examples/frontend.py send` vs whattui | Both reach `whatsapp.Client.SendText` through `send_text`; a difference implicates environment/daemon instance, not code. |
| chat age | long-standing chat vs fresh contact | Fresh contact exercises new-session establishment. |
| daemon lifecycle | up throughout vs restarted between send and retry | Validates buffer persistence + the daemon-store fallback. |
| addressing | recipient with vs without a `whatsmeow_lid_map` row | Exercises the alternate-JID retry lookup. |

## 6. Success criterion

The recipient's bubble resolves right after a `Sent retry #N` line. If retries are answered
but the placeholder persists across all matrix cells, collect the step-3/4
output; that is a session/identity problem to debug upstream, not a daemon
send-path bug.
