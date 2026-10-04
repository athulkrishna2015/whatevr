> _This project is not affiliated with WhatsApp or Meta._
# Whatevr

A Linux-first native client for WhatsApp, built as **one daemon and any number
of thin frontends**. `whatevrd` owns the WhatsApp connection (via
[whatsmeow](https://github.com/tulir/whatsmeow)), the login session, the SQLite
message store, the media cache and notifications. Frontends own pixels. They
talk over a documented protocol on a unix socket, and writing one is a fun weekend (for me atleast)


## Frontends

`whattui` is the terminal frontend, and the one frontend for now.

## Getting it
On Arch-based systems, Whatevr is available on the AUR:
```sh
yay -S whatevr-bin
```
Note: You can install the `whatevr` or `whatevr-git` packages also if you want to build yourself

For other systems, for now you can follow the build instructions below:

## Building
<details>
    <summary>
      Build Instructions</summary>
    
whatevr builds through a single top-level `justfile` that compiles the daemon
(`whatevrd`) and the terminal frontend (`whattui`). The daemon must be running for any frontend to work.

`whatevrd` builds against a fork of whatsmeow and `whattui` against a fork of
vaxis, both carried as git submodules, so clone with `git clone --recursive`, or run
`git submodule update --init --recursive` in an existing checkout.

#### 1. Install dependencies

**Daemon:** Go 1.26+, just, a C compiler, SQLite and libjpeg-turbo dev files, pkg-config.
**Terminal frontend:** the same Go toolchain, nothing else.
**Optional at runtime:** ffmpeg, for video posters and voice note waveforms.

```sh
# Arch
sudo pacman -S --needed base-devel go just sqlite libjpeg-turbo pkgconf

# Fedora
sudo dnf install go just gcc sqlite-devel turbojpeg-devel pkgconf-pkg-config

# Debian 13 "trixie" (needs Go >= 1.26, see Platform support)
sudo apt install golang just gcc libsqlite3-dev libturbojpeg0-dev pkg-config
```

#### 2. Build and install

```sh
just build                            # debug build for local testing
just build-release                    # optimized release build
just install "$HOME/.local"           # user-local release install
# or system-wide:
sudo just install /usr
```

`just install` places the `whatevrd` and `whattui` binaries and the systemd user
units under the selected prefix.
Make sure the chosen `bin` directory is on your `PATH` (e.g. `~/.local/bin`).

Other handy targets: `just version`, `just artifacts`, and `just clean`.

#### 3. Run

Start the daemon, then the frontend:

```sh
whatevrd      # or run it via systemd (below)
whattui
```

The first time, link your phone: `whattui` shows the QR code to scan, or run
`whatevrd pair` to print it in any terminal.

`whattui` is keyboard and mouse driven and needs nothing memorised: `ctrl+p`
opens the command palette, `/` in the composer opens the same commands inline,
and `?` lists every binding. It looks best in a terminal with the kitty
graphics protocol, and degrades by tier down to 16 colours without moving a
single glyph. `whattui --caps` prints what it found.

#### Run the daemon via systemd (optional)

`just install` ships two **mutually exclusive** user units: enable **one**,
never both (they share the same socket path):

- **Socket activation (recommended):** the daemon starts on demand the moment a
  frontend connects, and keeps running afterwards.
- **Always-on service:** the daemon starts at login.

```sh
systemctl --user daemon-reload
systemctl --user enable --now whatevrd.socket     # socket activation (recommended)
# or
systemctl --user enable --now whatevrd.service    # always-on
```

A **user-local** install puts the units under `~/.local/lib/systemd/user`, which
systemd does not search. Copy them into a searched path first (the templated
service needs the binary path substituted):

```sh
mkdir -p ~/.config/systemd/user
sed "s|@BINDIR@|$HOME/.local/bin|g" packaging/systemd/whatevrd.service.in \
  > ~/.config/systemd/user/whatevrd.service
cp packaging/systemd/whatevrd.socket ~/.config/systemd/user/
systemctl --user daemon-reload
```

Distro packages install both units to `/usr/lib/systemd/user/` (shipped disabled).

</details>


## Status
Whatevr is very early-stage software. It is usable for development and testing, but the should be treated as **EXPERIMENTAL**. 
There is lots of missing functionality that is considered essential, and there WILL be bugs.

The **protocol**, on the other hand, is stable at version 2: new views, commands
and message kinds will be added, but nothing already in PROTOCOL.md changes
shape. A frontend written against it today keeps working.

Now with that, here is the current feature map for whatevrd.
<details>
  <summary>Feature Map</summary>
  
| Feature | Status | Notes |
| --- | --- | --- |
| WhatsApp login with QR code | ✅ | |
| Persistent login session | ✅ | |
| Logout | ✅ | |
| Local message database | ✅ | SQLite |
| Older message loading | ✅ | |
| Incoming messages | ✅ | |
| Send text messages | ✅ | |
| Send image messages | ✅ | |
| Reply to messages | ✅ | |
| Message delivery/read status | ✅ | |
| Pin and unpin chats | ✅ | |
| Group chats | ✅ | Basic support + info display |
| Chat avatars | ✅ | |
| Media preview/display | ✅ | Images and cached media |
| Paste image from clipboard | ✅ | |
| Typing indicator | ✅ | Send/receive composing state |
| Online/last-seen presence | ✅ | |
| Offline/history sync progress | ✅ | |
| Desktop notifications | ✅ | Handled by daemon |
| Emoji picker | ✅ | Frontend-local |
| Message search | ✅ | |
| Chat search | ✅ | |
| Contact search/new chat | ✅ | |
| Voice messages | ❌ | |
| Audio playback | ❌ | |
| Video playback | ❌ | |
| View-once messages sending | ❌ | |
| Document/file sending | ❌ | Images/media path exists, general file UX missing |
| Stickers | ✅ | Receive and send stickers |
| Message reactions | ✅ | |
| Composer emoji inline search | ✅ | |
| Edit sent messages | ✅ | Received message edits are handled too |
| Delete messages | ✅ | |
| Forward messages | ✅ | |
| Star/bookmark messages | ✅ | |
| Archive chats | ✅ | |
| Mute chats | ✅ | |
| Pinned messages | ✅ | |
| Group management | ❌ | No create/invite/admin UI |
| Community management | ❌ |  |
| Calls | ❌ | Voice/video calls unsupported |
| Status/stories | ❌ | |
| Settings UI | ✅ | |
| Account/profile editing | ✅ | Includes privacy settings |
| Import/export backups | ❌ | |
| DB encryption and keyring integration | ❌ | |
| Daemon SNI (Tray) | ❌ | |
  
</details>

## Architecture

Whatevr is one background daemon, `whatevrd`, and a socket. The daemon owns the
WhatsApp connection, the login session, the local SQLite store, the media cache
and desktop notifications, and serves all of it over
[the whatevr protocol](PROTOCOL.md). Frontends never speak to WhatsApp directly
and never keep durable state of their own; several can run at once against the
same daemon, and each sees the same rows in the same order because the daemon
computed that order.

The daemon is Go (`whatevrd/`); the terminal frontend is `whattui`, in Go on a
fork of vaxis (`whattui/`). A scriptable CLI is wanted and
unclaimed; that work needs no changes to the daemon.

Whatevr will be Linux-first for now until its stable. I am open to contributions for porting functionality to other platforms as long as they don't affect existing performance and Linux functionality significantly. 


## The protocol is the point

[**PROTOCOL.md**](PROTOCOL.md) is the contract: protobuf frames, each preceded
by its length as a varint, over `$XDG_RUNTIME_DIR/whatevr/whatevrd.sock`.
Protocol version 2. The schema lives in [`proto/`](proto) and generates for
any language; Go and Python types are checked in. Four ideas carry the whole
thing:

- **The daemon owns all state.** A frontend does no sorting, merging, dedup or
  cache invalidation. Ever. It keeps a map of items and renders it.
- **You subscribe to views, not endpoints.** `subscribe` to `chats`, `messages`,
  `typing`, `presence`… and the daemon sends the contents, then keeps your copy
  correct forever with keyed upserts and removes. Every item carries a
  daemon-computed `sort` key; ordering is never your problem.
- **Commands only ever return an id.** Send a message and the response is a
  message id. The message itself arrives through the view you already had open,
  the same way one from your phone would. There is no second code path.
- **Every message renders in every frontend.** Each one carries a `fallback`
  string, so a client that has never heard of stickers still shows something
  sensible. Partial frontends are first-class.

The whole surface is slim. [`examples/frontend.py`](examples/frontend.py) is a
complete frontend in under 80 lines: it says hello, subscribes to the chat
list, keeps it live, and sends a message.

## Acknowledgements
Whatevr stands on the shoulders of:

- [whatsmeow](https://github.com/tulir/whatsmeow): WhatsApp Web multidevice protocol library that whatevrd runs on a fork of (MPL-2.0)
- [vaxis](https://git.sr.ht/~rockorager/vaxis): terminal UI library that whattui runs on a fork of (Apache-2.0)

## License
This program is licensed under the BSD-3-Clause License
