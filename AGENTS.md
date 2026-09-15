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
