# AGENTS.md — Whatevr

Development guide for the Whatevr project (daemon `whatevrd` + Qt frontend `whatkevr`).

## Build commands

Every build and test run is capped at **2 parallel jobs** (`GOMAXPROCS=2` for
Go, `-- -j2` for Ninja). Never let these run unbounded.

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

```sh
just install prefix=/home/admin/.local         # release, user-writable prefix
just install-dev prefix=/home/admin/.local     # debug, user-writable prefix
```

### After install: restart both processes

A new build takes effect only after both processes run the new binaries.
Each time a new version is installed, fully close the app and relaunch both
the daemon and the UI: quit the frontend via the tray menu → Quit (a window
hidden via close-to-tray keeps the old binary alive), restart the user
daemon, then launch the UI again.

```sh
systemctl --user restart whatevrd.service
```

Verifying against a stale daemon or a stale hidden UI produces misleading
results — always restart both before testing an install.

Full quit (tray icon included — the icon is daemon-owned, so quitting the UI
alone leaves it behind):

```sh
pkill -f '/home/admin/.local/bin/whatkevr'
systemctl --user stop whatevrd.service
```

Relaunch and verify:

```sh
systemctl --user start whatevrd.service
setsid nohup /home/admin/.local/bin/whatkevr > /tmp/whatkevr-ui.log 2>&1 < /dev/null &
ps -o pid,lstart,cmd -C whatkevr -C whatevrd
qdbus org.kde.StatusNotifierWatcher /StatusNotifierWatcher org.kde.StatusNotifierWatcher.RegisteredStatusNotifierItems
```

`whatevrd.socket` stays active after a stop, so the daemon also
socket-activates on the next UI launch.

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

## Contribution and PR scope

- Prefer small, focused pull requests. Separate daemon/protocol work, Qt/QML
  frontend work, CI fixes, and unrelated bug fixes instead of combining them
  into one broad feature branch.
- Keep each PR reviewable by stating its exact scope, included features,
  exclusions, dependencies, and verification steps in the PR description.
- Do not add or expand generated feature logs, implementation logs, screenshot
  inventories, or speculative documentation as part of feature work. Add only
  concise, hand-reviewed documentation required by the change. Larger design
  notes and feature inventories belong in the project wiki unless explicitly
  requested.
- Do not bump `VERSION`, package versions, release metadata, changelogs, or
  release notes in ordinary feature PRs. Release/version changes are handled by
  the maintainer's release scripts when a release is cut.
- Do not commit `AGENTS.md`, `CLAUDE.md`, editor settings, local screenshots,
  session transcripts, generated build output, or other personal development
  fixtures unless the maintainer explicitly requests that repository-level
  file change.
- Do not modify `.gitignore` to accommodate local tools or personal workspace
  files. Keep environment-specific exclusions local.
- Before opening a PR, inspect the diff against upstream, remove unrelated
  files, and confirm that no secrets, local paths, generated artifacts, or
  release-only changes are included.
