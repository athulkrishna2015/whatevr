# AGENTS.md — Whatevr

Development guide for the Whatevr project (daemon `whatevrd` + Qt frontend `whatkevr`).

## Build commands

Builds and full test runs go through GitHub Actions CI on `origin`: push the
branch and let the runners (unbounded CPU) do the work instead of this slow
laptop. Local builds are for quick iteration only.

Every local build and test run is capped at **2 parallel jobs**
(`GOMAXPROCS=2` for Go, `-- -j2` for Ninja). Never let these run unbounded.

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

### After install: it restarts itself

A new build takes effect only once both processes run the new binaries, so
`just install` restarts them for you. There is nothing to remember and no
second command: an install that leaves the old daemon running is an install
that has not happened yet.

```sh
just install /home/admin/.local
```

It kills the frontend (a window hidden to tray is still running, so it is
killed rather than asked), restarts `whatevrd.service`, and relaunches the UI.
It only does this when something was actually running, and never into a
staging root: a `DESTDIR` install is packaging, and CI skips it.

`just restart /home/admin/.local` is the same thing on its own, for a rebuild
or a `git pull` that changed code you had installed.

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

### Qt frontend from CI artifacts (no local build)

The `qt` CI job (`Qt frontend (Arch)`) configures `whatkevr` with tests,
builds it, runs `ctest` offscreen, then uploads the `DESTDIR` install tree
as artifact `whatkevr-<sha>` (7-day retention). Install from it instead of
compiling on the laptop:

```sh
gh run download <run-id> --repo athulkrishna2015/whatevr -n whatkevr-<sha> -D /tmp/opencode/qt-root
cp -r /tmp/opencode/qt-root/usr/. /home/admin/.local/
```

The artifact is a debug build: fine for testing, not for keeping. The Arch
container usually carries newer Qt/KF libraries than the laptop; if the
binary refuses to start over missing `.so` versions, fall back to a local
release build of `whatkevr` only.

Never `just install` a v2-only daemon over a live v1 session: the Qt app
still speaks protocol v1, so replacing the daemon breaks the running
frontend. Install the Qt binary only and restart just the UI.

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
