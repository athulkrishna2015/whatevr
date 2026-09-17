# Building, testing, installing

## Daemon (Go)

```sh
GOMAXPROCS=2 go -C whatevrd build -tags sqlite_fts5 ./...
GOMAXPROCS=2 go test -tags sqlite_fts5 ./internal/...        # full suite
gofmt -l .                                                   # must be empty
```

`sqlite_fts5` is required (message search). Run `gofmt` before pushing: CI
fails the build on any unformatted file.

WhatsApp library pins: `go.mau.fi/whatsmeow` has no tagged releases; the
version in `whatevrd/go.mod` is a commit pseudo-version. After `go get -u`,
check the event/proto types the daemon uses (`types/events`, `types`,
`proto/waE2E`) for renames.

## Frontend (Qt/Kirigami)

```sh
cmake -S whatkevr -B build/debug/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build build/debug/whatkevr
```

Unit tests (chat-bubble budgets, playback, models) need an explicit flag —
the default configure builds none of them:

```sh
cmake -S whatkevr -B build/debug/whatkevr-tests -G Ninja \
  -DCMAKE_BUILD_TYPE=Debug -DWHATEVR_BUILD_TESTS=ON
cmake --build build/debug/whatkevr-tests
ctest --test-dir build/debug/whatkevr-tests --output-on-failure
```

New QML files must be added to `WHATKEVR_QML_FILES` in
`whatkevr/src/CMakeLists.txt`, and new `required` delegate properties must be
added to `tst_chatbubbleperf.cpp`'s `baseProps()` or CI fails. New always-
instantiated delegate objects may need a deliberate budget raise (see the
comments in that file).

## Install

```sh
just install /home/admin/.local        # release
just install-dev /home/admin/.local    # debug smoke tests
```

**Gotcha:** the installed `just` treats `prefix=/path` as a positional value
and installs under a literal `prefix=.../` directory. Always use the
positional form. Then restart both halves:

```sh
systemctl --user restart whatevrd.service   # daemon
# quit and reopen whatkevr — the frontend does not hot-reload
```

## CI

`.github/workflows/ci.yml` runs formatting, vet, metadata validation, the Go
suite, the Qt suite, and a full Arch build+install. The AUR check regenerates
`.SRCINFO` from each `packaging/aur/*/PKGBUILD` with `makepkg --printsrcinfo`
and diffs: after touching any PKGBUILD `depends`, update the matching
`.SRCINFO` by hand (no makepkg off Arch).

## Releases

`just release` only forwards the version (see `scripts/release.py` — pass
extra flags to it directly with `uv run`):

```sh
uv run scripts/release.py 0.8.2 --notes-file /tmp/notes.md
git push --follow-tags
```

The script needs a clean tree, writes `VERSION`/doodle/metainfo/AUR files,
validates, commits `version: x.y.z`, and creates annotated tag `vx.y.z` whose
body becomes the GitHub release notes (the workflow requires the tag to be
annotated with a non-empty body). Pushing the tag triggers
`.github/workflows/release.yml`, which builds artifacts and publishes the
GitHub release.
