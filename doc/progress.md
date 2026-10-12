# Whatevr progress · working state

Last updated 2026-10-12. All work tracked here.

## Commit structure

`origin` is the fork `athulkrishna2015/whatevr`; `upstream` is
`codelif/whatevr`. This tree is the fork. Feature work lands via PRs on
`origin`: push the branch, open a PR to `main`, CI runs on `pull_request`
(`ci.yml` only triggers on `main` pushes, PRs, manual dispatch and the weekly
cron — a bare branch push runs nothing).

## Source to use for features

User instruction: implement missing/not-working features using
https://github.com/crmne/zapfast as the reference, even where we have our own
way. When they differ, zapfast's is the better implementation. Keep our custom
features such as anti-delete (revoked content stays visible with a "Deleted"
mark — `whatevrd/internal/model/revoke.go` + `model.SetAntiDelete`).

whatevr is already far ahead of zapfast on message kinds (vcard, events/RSVP,
commerce cards, live location, PTV, view-once, albums, channels, status, calls,
scheduling, anti-delete) — all of which zapfast lacks. The genuine remaining
gaps were audited into `feature-gap.md` §4-16.

## Feature clusters

1. **Cluster 1 — Group admin** — COMPLETE (commit `be9411a`).
2. **Cluster 2 — Account/identity** — in progress.
3. **Cluster 3 — Labels** — not started.
4. **Cluster 4 — Media robustness** — already wired.
5. **Cluster 5 — Notification/desktop polish** — not started.

## Progress log

### 2026-10-12 — build fixes for `be9411a` (groups), by continuation agent

The previous agent committed `be9411a` (group member add mode, join approval,
join requests, invite preview) but left the tree **not compiling** — the Qt
DEBUG build died and three defects were sitting in the working state:

1. `whatkevr/src/app/protocolcontroller.cpp:3087` — `m_groupInvitePreview`
   is a `QVariantMap` but was assigned a `QJsonObject` (`= result`). The
   previous agent's half-fix defined a hand-rolled `jsobjToVariantMap`
   helper **inside an anonymous namespace in `v2shim.cpp`** (invisible to
   other translation units) with no header declaration.
   **Fixed** by following the codebase's own pattern instead —
   `result.toVariantMap()`, the same conversion `protocolclient.cpp:376` and
   a dozen other call sites already use. The `jsobjToVariantMap` helper was
   dropped entirely (both files checked out, so no orphan decl/def remains).
2. `v2shim.cpp:370` — the new `kGroupJoinRequests` arm calls
   `translateV2Person` ~2600 lines before its definition (an anonymous-
   namespace helper defined with the body translators). Same class of error
   at `v2shim.cpp:1804`: `v2PersonFromJson` used in `buildV2Request` long
   before its definition. Both were introduced by `be9411a`.
   **Fixed** with one early anonymous-namespace block between
   `buildV2Unsubscribe` and `translateV2Response` forward-declaring both
   (`v2shim.cpp:246`). Note: forward-declaring at `whatevr::proto` scope
   instead would be a link/ambiguity disaster — the definitions are in an
   anonymous namespace, so the declaration must live in an anonymous
   namespace too (all anonymous blocks in a TU are the same namespace; the
   file already does this for `translateV2MessageBody`). Verified both
   defects reproduce on pristine `HEAD` (`git show HEAD:` + standalone
   `g++ -fsyntax-only`) — the commit itself was broken, not just the tree.
3. The old `/tmp/qtbuild.log` failure (`jsobjToVariantMap was not declared`
   at protocolcontroller.cpp:3087) is resolved by fix 1.

**Validation state (when this entry was written):** `-fsyntax-only` passes
for both `v2shim.cpp` and `protocolcontroller.cpp`; a full
`cmake --build build/debug/whatkevr -- -j2` is running (`/tmp/qtbuild.log`,
started from a recovered/corrupt `.ninja_log`, so it is a near-full rebuild
of ~660 steps). After it: `ctest --output-on-failure`, then the daemon
(`GOMAXPROCS=2 go -C whatevrd build/vet/test -tags sqlite_fts5`). Then push
+ PR to `origin/main` and watch CI with the poll script in AGENTS.md.

**Gotcha for the next agent:** `translateV2Person` and `v2PersonFromJson`
must keep their forward declarations in sync with the anonymous-namespace
definitions — any new call site before their definitions needs them visible.

## Cluster 4 — Media robustness (ALREADY WIRED, do not duplicate)

`whatevrd/internal/whatsapp/media.go` already does the right thing on a stale
403/404/410, which zapfast does too (`media_reupload().request()` in
`src/backend/worker.rs`). Flow: `staleMedia(err)` detects it, log "media did
not verify, asking the sender for a fresh path", call
`md.retry()` → `SendMediaRetryReceipt`, wait for the phone to answer; if the
phone then also can't serve it, return "this media is no longer available from
the sender". `retried(evt)` applies the answer and toggles `retries[key]`.
`media_retry.go` (uint16 timer) is the timeout. Nothing to do here.

## Cluster 2 — Account/identity (in progress)

### Reference: zapfast, whatwhatevr already has
zapfast's account screen is light; whatevr has the WhatsApp API surface on the
whatsmeow side. Gaps from our feature-gap §10 that are NOT yet wired:

| Feature | whatsmeow API | Status |
|---|---|---|
| own profile photo (change/remove) | SetGroupPhoto with self JID | TODO |
| own push name | `appstate.BuildSettingPushName` exists in whatsmeow; ProfilePage says "not wired up yet" | TODO |
| business profile detail | GetBusinessProfile | only a badge today |
| contact QR (mine + resolve) | GetContactQRLink, ResolveContactQRLink | TODO |
| security-code / linked-devices screen | GetUserDevices | TODO |
| shared groups in common (on contact card) | local join over participant store | TODO |
| last seen/online in chat header | presence events + PresenceView | mostly there, tweak |

### Build system notes
- Proto is regenerated with these exact commands. Run from the repo root
  after editing `proto/whatevr/v2/*.proto`:
  ```
  PROTECT_GEN=/home/admin/go/bin/protoc-gen-go
  protoc --plugin=protoc-gen-go=$PROTECT_GEN \
    --go_out=proto --go_opt=paths=source_relative \
    --go_opt=Mwhatevr/v2/X.proto=github.com/codelif/whatevr/proto/whatevr/v2;whatevrv2 \
    --proto_path=proto proto/whatevr/v2/X.proto
  ```
  for X in account chats frame frontends groups media messages notifications
  people search settings stickers
- `go test -C whatevrd ./...` takes a long time on this laptop (AGENTS.md
  says: push to CI, poll the branch's run instead). Use
  `-run '^TestName$'` for the new tests. Full daemon suite:
  `GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...`.

## Todo (next agent)

1. Finish the `be9411a` build-fix validation: watch `/tmp/qtbuild.log` to
   `EXIT=0`, run `ctest --test-dir build/debug/whatkevr --output-on-failure`,
   then the daemon trio above. Fix whatever else `be9411a` broke (it had
   never compiled — more than the three defects above may surface; QML and Go
   were never run either).
2. Push the branch, open a PR to `origin/main`, watch CI with the AGENTS.md
   poll loop. Merge when green.
3. Cluster 2: own profile photo + push name (whatsmeow side through
   `SetGroupPhoto`/`BuildSettingPushName`), business profile row on contact
   card, contact QR view, device list view. Then QML side (profile page,
   contact card).
4. Cluster 3: labels — appstate already folds `label_edit`/`label_jid` but has
   no views/commands/UI; add the rest.
5. Cluster 5: notifications (inline reply + mark-read + replace-on-update
   already wired: `notify/worker.go` `replaces`, and `format.go` actions
   "reply"/"Mark as read") + desktop (autostart toggle, launcher unread badge).
