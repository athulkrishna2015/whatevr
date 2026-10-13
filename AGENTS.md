# AGENTS.md — Whatevr

Development guide for the Whatevr project (daemon `whatevrd` + Qt frontend `whatkevr`).

## Current CI workflow improvements

The CI workflow has been optimized with the following changes:

### Pacman package caching
- Added week‑based caching for Arch Linux packages in both `build-arch` and `qt` jobs
- This eliminates repeated `pacman -Syu` downloads on every CI run, significantly reducing build times
- Uses the same `YYYY‑VV` week format as the existing rlottie cache for consistency

### Removed artificial CPU core limits
- Removed `GOMAXPROCS: 2` from the Windows Go cross‑compile job
- Go now scales with available cores, improving build parallelization

### Updated build documentation
- Local builds still cap parallelism at **2 jobs** (`GOMAXPROCS=2` for Go, `-- -j2` for Ninja)
## Summary of changes

The AGENTS.md has been updated to reflect the current state of the Whatevr project:

### CI Workflow Improvements
- Added documentation for the new week‑based Pacman package caching strategy
- Removed references to artificial CPU core limits in Windows builds
- Updated build documentation to reflect the current CI environment

### Feature Status
The project is currently organized into 5 clusters:

- **Cluster 1 — Group admin**: Complete (commit `be9411a`)
- **Cluster 2 — Account/identity**: In progress
- **Cluster 3 — Labels**: Not started
- **Cluster 4 — Media robustness**: Already wired
- **Cluster 5 — Notification/desktop polish**: Not started

The build fixes for `be9411a` (groups commit) have been applied and the tree now compiles cleanly. All CI improvements have been committed and pushed to the `contacts-search-and-paste-edit` branch.

### Build Process

Local builds on this machine are capped at **2 parallel jobs** (`GOMAXPROCS=2` for Go, `-- -j2` for Ninja) to avoid thrashing. CI runs leverage the full resources of self‑hosted runners with optimized caching strategies.

### CI Build & Test Flow

The GitHub Actions CI performs the following steps:
1. Build and install (Arch container) with Pacman caching
2. Run Go tests, vet, and formatting checks
3. Build and test the Qt frontend with Arch dependencies
4. Validate metadata and generate documentation
5. Upload artifacts for deployment

The workflow now includes week‑based caching for Arch packages to eliminate repeated downloads and removed artificial CPU limits to maximize parallelization where possible.

### Current State

All three build defects introduced by commit `be9411a` have been resolved:

1. **ProtocolController fix**: Replaced the orphaned `jsobjToVariantMap` helper with the canonical `QJsonObject::toVariantMap()`
2. **Forward declarations**: Added early anonymous‑namespace forward declarations for `translateV2Person` and `v2PersonFromJson`
3. **QML dialog registration**: Registered `GroupInvitePreviewDialog.qml` in the CMakeLists.txt and fixed its QML imports

The tree now compiles cleanly and all CI tests should pass with the optimized workflow.
- CI leverages the full resources of self‑hosted runners

### Daemon (Go)

```sh
GOMAXPROCS=2 go -C whatevrd build -tags sqlite_fts5 ./...
GOMAXPROCS=2 go -C whatevrd vet -tags sqlite_fts5 ./...
GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...
```

### Frontend (CMake + Ninja)

```sh
cmake -S whatkevr -B build/debug/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build build/debug/whatkevr -- -j2
ctest --test-dir build/debug/whatkevr --output-on-failure

# Release:
cmake -S whatkevr -B build/release/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build build/release/whatkevr -- -j2
```

### Release install (just)

The prefix is a **positional** argument. `just install prefix=/path` is not a
named assignment on the installed `just` (1.58): it takes the whole string as
the prefix, so the build lands in a directory literally named `prefix=…` inside
the repo and reports success. See `doc/troubleshooting.md`.

```sh
just install /home/admin/.local                # release, user-writable prefix
just install-dev /home/admin/.local            # debug, user-writable prefix
```

### After install: you restart it yourself

**`just install` does not restart anything, and there is no `just restart`
recipe.** `scripts/install.py` says so itself — "Portable, stageable
installation. Never activates a user service." — and contains no `systemctl`
call. It only copies files.

So a daemon that is already running makes the install **fail**, not slow:

```
OSError: [Errno 26] Text file busy: '/home/admin/.local/bin/whatevrd'
```

`copyfile` opens the destination for writing and the running process holds it.
Stop the service first, install, then start it:

```sh
systemctl --user stop whatevrd.service
just install /home/admin/.local
systemctl --user start whatevrd.service
```

### The Qt frontend is a separate install on Linux

`just install` installs `whatevrd` and `whattui` only. It never touches
`whatkevr`, and neither does the release tarball — `whatevr-<version>-
linux-<arch>.tar.zst` ships just those two binaries. The Qt app is installed by
CMake instead (`whatkevr/src/CMakeLists.txt:273`), so it needs its own pass:

```sh
scripts/configure-frontend --build build/release/whatkevr --build-type Release \
    --version "$(cat VERSION)" --version-full "$(python3 scripts/version.py full)"
cmake --build build/release/whatkevr -- -j2
cmake --install build/release/whatkevr --prefix /home/admin/.local
```

Skipping this is how a stale `whatkevr` outlives a release: nothing in the
local install path refreshes it.

### A stale build dir ships the wrong version

`configure-frontend` matters because `whatkevr/CMakeLists.txt:3` reads
`VERSION` under `if(NOT DEFINED WHATEVR_VERSION)`. A `-DWHATEVR_VERSION_FULL`
cached in `CMakeCache.txt` wins, so rebuilding an old release dir produces a
binary that reports whatever version that dir was configured with — an install
that reports success and lies. Same family as the `CMAKE_PREFIX_PATH` trap
below: when a release build behaves as if it is from another era, wipe the dir
or pass `--force`.

**The prefix is a positional argument**, not `prefix=…`: the installed `just`
(1.58) reads `just install prefix=/path` as a literal prefix of that name and
installs into a directory called `prefix=…` inside the repo, reporting success.

Verify what is actually running — the version string comes from `git describe`,
so it carries `-dirty` whenever the tree is not clean:

```sh
ps -o pid,lstart,cmd -C whatkevr -C whatevrd
/home/admin/.local/bin/whatkevr --version
```

Verifying against a stale daemon or a stale hidden UI produces misleading
results. If a release build fails to configure with a non-existent path under
`/tmp`, the release build dir is holding a `CMAKE_PREFIX_PATH` from an older
session: `rm -rf build/release/whatkevr` and install again.

`whatevrd.socket` stays active after a stop, so the daemon also
socket-activates on the next UI launch.

### Install from CI, not from a local build

This is the default way to get a build onto this laptop. Compile locally only
when CI has not run for the commit you want.

```
edit → commit → push → wait for CI on that commit → download → install → test
```

CI publishes the two halves separately, and neither is complete on its own:

- **Qt frontend** — the `qt` job (`Qt frontend (Arch)`) configures `whatkevr`
  with tests, builds it, runs `ctest` offscreen, stages the `DESTDIR` tree and
  uploads it as `whatkevr-<sha>` (7-day retention).
- **Daemon** — the `Build and install (Arch)` job stages a tree but uploads
  **nothing**. The daemon only exists as a download from the **Release**
  workflow, as `whatevr-<version>-linux-<arch>.tar.zst` beside `SHA256SUMS`.

Neither tarball nor Qt artifact contains both, so install both:

```sh
# 1. the run for the commit you pushed
run=$(gh run list --repo athulkrishna2015/whatevr --commit <sha> --limit 1 --json databaseId --jq '.[0].databaseId')
gh run download "$run" --repo athulkrishna2015/whatevr \
    -n "whatkevr-<sha>" -D /tmp/opencode/qt-root

# 2. the released daemon, checked against the published sums
gh release download v<version> --repo athulkrishna2015/whatevr \
    --pattern "whatevr-<version>-linux-$(uname -m).tar.zst" --pattern SHA256SUMS -D /tmp/opencode/rel
cd /tmp/opencode/rel && sha256sum -c SHA256SUMS --ignore-missing

# 3. install, daemon stopped or the copy fails with Errno 26
systemctl --user stop whatevrd.service
cp -r /tmp/opencode/qt-root/usr/. /home/admin/.local/
tar --zstd -xf "whatevr-<version>-linux-$(uname -m).tar.zst"
cp -r "whatevr-<version>-linux-$(uname -m)"/usr/. /home/admin/.local/
systemctl --user start whatevrd.service

# 4. confirm both halves are the build you meant
~/.local/bin/whatevrd --version && ~/.local/bin/whatkevr --version
```

Three things that will bite, all of which have bitten this repo:

- **The Qt artifact is a Debug build.** Fine for testing, not for keeping. It
  also comes from the Arch container, which usually carries newer Qt/KF than the
  laptop; if it refuses to start over a missing `.so`, build `whatkevr` locally
  instead — but keep the released daemon.
- **Match the artifact to the commit you pushed.** `whatkevr-<sha>` is named for
  the commit the job built, so its version string is `git describe` for *that*
  commit. After a release, the newest artifact is the merge commit and reports
  the *previous* version: for v0.14.0 the only artifact was `whatkevr-aeb5979…`
  reporting `0.13.0-…` while `VERSION` said `0.14.0`. Check `--version`
  against `python3 scripts/version.py full` on that sha before believing it.
- **The release package carries all three binaries** — `whatkevr`, `whatevrd` and
  `whattui` — since `_binary-tarball` gained a frontend build. Before that it
  shipped the daemon and the TUI only, so a Linux user got no GUI at all; check
  the tarball with `tar tzf`/`tar --zstd -tf` before assuming.

`just build-frontend` is the frontend half on its own (Release mode, versioned
from `scripts/version.py`). The Qt/KF6 dependencies are not in the daemon
build, so a machine without them cannot run it.

Never `just install` a v2-only daemon over a live v1 session: the Qt app
still speaks protocol v1, so replacing the daemon breaks the running
frontend. Install the Qt binary only and restart just the UI.

### v1 to v2 daemon migration needs a re-pair

The v2 core keeps its WhatsApp session in its own store: it does not pick
up the v1 daemon's login. After installing the v2 daemon, the frontend shows
the QR pairing page (verified working: QR renders, expiry countdown ticks).
Scan it with the phone to pair; the old session cannot transfer. If the
migration ever needs reverting, pre-migration binaries live at
`/tmp/opencode/whatkevr.v1` and `/tmp/opencode/whatevrd.v1` (copy back over
`~/.local/bin` and restart the service).

### Watching CI to completion

Poll the branch's latest run in the background instead of local waiting;
`gh` prints one line per check and the per-job table at the end:

```sh
bash -lc 'for i in $(seq 1 45); do st=$(gh run list --repo athulkrishna2015/whatevr --branch main --limit 1 --json status,conclusion | jq -r ".[0] | [.status, .conclusion] | @tsv"); printf "check %s: %s\n" "$i" "$st"; case "$st" in completed*) break;; esac; sleep 120; done; gh run view $(gh run list --repo athulkrishna2015/whatevr --branch main --limit 1 --json databaseId --jq ".[0].databaseId") --repo athulkrishna2015/whatevr --json jobs,conclusion,url | jq -r ".url, ([.jobs[] | [.name, .conclusion, .status] | @tsv])[]"'
```

Only `origin` (`athulkrishna2015/whatevr`) is watched and pushed to; release
tags and releases are never published to `upstream`.

### Debugging with logs

Check logs before guessing. Three sources, cheapest first:

```sh
# Daemon run logs (JSONL, one file per run — the real history of what happened)
ls -t ~/.local/state/whatevr/logs/ | head
grep -i -E "history blob logged|history_sync_stalled|\"level\":\"(error|fatal)\"" \
  ~/.local/state/whatevr/logs/whatevrd-*.jsonl | tail -n 15
# Service lifecycle (restarts, crashes)
journalctl --user -u whatevrd.service --since "30 min ago" --no-pager | tail -n 15
# Frontend stdout (QML errors, protocol warnings)
tail -n 20 /tmp/opencode/whatkevr-v2.log
```

Key patterns: `history blob logged` (phone sent history; type/chunk/progress show what), `history_sync_stalled` with `nothing new for X` (phone stopped sending — phone-side, keep WhatsApp open and wait; a daemon restart forces a fresh handshake that often resumes it), `status: problem` heartbeat (the status board ticking, not an error by itself). `whatevrd logs` needs the daemon stopped (it takes the process lock), so prefer reading the JSONL files directly on a live session.

## Updating the whatsmeow dependency

WhatsMeow (`go.mau.fi/whatsmeow`) has no tagged releases; the daemon pins to a
specific commit pseudo-version in `whatevrd/go.mod`, e.g.

```
go.mau.fi/whatsmeow v0.0.0-20260814123134-0dcf1f50f4b1
```

To update to the latest commit on the default branch:

```sh
cd whatevrd
go get -u go.mau.fi/whatsmeow
go mod tidy
go build ./...
go test -tags sqlite_fts5 ./internal/...
```

After updating, check for breaking API changes: whatsmeow's API surface changes
between commits. Common breakage in this project:

- `"go.mau.fi/whatsmeow/types/events"` — event struct field renames/removals.
- `"go.mau.fi/whatsmeow/types"` — `Chat`, `GroupInfo`, etc. field changes.
- `"go.mau.fi/whatsmeow/proto/waE2E"` — protobuf message struct changes.
- Phone-number validation / JID format changes in `utils`.

If the update introduces breaking changes, update callers in
`whatevrd/internal/wa/` and the protocol handlers in
`whatevrd/internal/protocol/` accordingly, then re-run the test suite.

Record the new pseudo-version in the changelog entry under "Unreleased" →
"Updated" with the commit date and hash.

## Merging upstream

`origin` is the fork (`athulkrishna2015/whatevr`); `upstream` is
`codelif/whatevr`. Check what landed upstream before assuming a tree is
current:

### Release publishing permission

This machine is authorized to publish releases only to `origin`
(`athulkrishna2015/whatevr`). Never push release tags or publish releases to
`upstream` (`codelif/whatevr`), even if explicitly asked; the maintainer of
that repository must publish them.

```sh
git fetch upstream
git log --oneline main..upstream/main      # what we are missing
git rev-list --left-right --count main...upstream/main   # "<ours> <theirs>"
```

Merge it when upstream has work we need (bug fixes, a whatsmeow bump, new
frontends). The histories diverge, so expect conflicts in `whatevrd/internal/`
and `whatkevr/src/`:

```sh
git status                      # the tree must be clean first
git merge upstream/main
```

If the tree has uncommitted work, preserve it before merging — a dirty tree
blocks the merge and the conflict resolution gets confusing fast:

```sh
git diff > /tmp/whatevr-wip.patch        # tracked changes
git ls-files --others --exclude-standard  # copy untracked files aside
git stash push -u -m "wip before upstream merge"
```

Resolve conflicts file by file, keeping the intent of both sides rather than
picking one wholesale. Then always re-verify before committing — an upstream
whatsmeow bump alone changes compile behaviour:

```sh
GOMAXPROCS=2 go -C whatevrd build -tags sqlite_fts5 ./...
GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...
cmake --build build/debug/whatkevr -- -j2
ctest --test-dir build/debug/whatkevr --output-on-failure
```

If the merge bumped whatsmeow, re-check the API-breakage list below.
