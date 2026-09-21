# Whatevr documentation

Whatevr is a Linux-first WhatsApp client: a Go daemon (`whatevrd`) owns the
WhatsApp connection, and a Qt/Kirigami frontend (`whatkevr`) renders it. The
two talk over a versioned JSON protocol on a local socket.

## Contents

- [Building, testing, installing](build.md) — commands, CI checks, release flow.
- [Architecture & protocol](protocol.md) — how the pieces fit, where the contract lives.
- [Features](features.md) — what exists, what is missing, what is deliberately
  refused (with reasons).
- [Telegram Desktop reference](tdesktop-notes.md) — whatkevr vs tdesktop
  area-by-area verdicts, what was borrowed and what is still TODO.
- [Safety policy](safety.md) — how we avoid getting accounts flagged.
- [Troubleshooting](troubleshooting.md) — tray, logs, socket debugging recipes.

## Quickstart

```sh
# Daemon
GOMAXPROCS=2 go -C whatevrd build -tags sqlite_fts5 ./...

# Frontend (debug)
cmake -S whatkevr -B build/debug/whatkevr -G Ninja -DCMAKE_BUILD_TYPE=Debug
cmake --build build/debug/whatkevr

# Install release to ~/.local (positional arg — see build.md)
just install /home/admin/.local
systemctl --user restart whatevrd.service
```

After installing, (re)start the frontend (`whatkevr`) — it does not hot-reload.
