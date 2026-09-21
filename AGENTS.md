# AGENTS.md — Whatevr

Development guide for the Whatevr project (daemon `whatevrd` + Qt frontend `whatkevr`).

## Build commands

### Daemon (Go)

```sh
GOMAXPROCS=2 go -C whatevrd build -tags sqlite_fts5 ./...
GOMAXPROCS=2 go -C whatevrd test -tags sqlite_fts5 ./internal/...
```

### Frontend (CMake + Ninja)

```sh
cmake -S whatkevr -B build/debug/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build build/debug/whatkevr

# Release:
cmake -S whatkevr -B build/release/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Release
cmake --build build/release/whatkevr
```

### Release install (just)

```sh
just install prefix=/home/admin/.local         # release, user-writable prefix
just install-dev prefix=/home/admin/.local     # debug, user-writable prefix
```

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
