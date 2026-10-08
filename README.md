> _This project is not affiliated with WhatsApp or Meta._
# Whatevr

A Linux-first native client for WhatsApp, built as **one daemon and any number
of thin frontends**. `whatevrd` owns the WhatsApp connection (via
[whatsmeow](https://github.com/tulir/whatsmeow)), the login session, the SQLite
message store, the media cache and notifications. Frontends own pixels. They
talk over a documented protocol on a unix socket, and writing one is a fun weekend (for me atleast)


## Frontends

`whattui` is the terminal frontend, and `whatkevr` is the graphical Qt/Kirigami
frontend for Linux desktops (chat list, conversation timeline, media gallery,
calls/status pages where the daemon serves them, settings, tray icon). Both
are thin: all state lives in the daemon.

The daemon knows which frontends are installed, so it can start one when
nothing is open. Each says who it is in a small manifest; add your own, or pick
the default:

```sh
whatevrd frontend list
whatevrd frontend add --name "whattui in ghostty" ghostty-tui -- ghostty -e whattui
whatevrd frontend set-default ghostty-tui
whatevrd frontend terminal set -- foot -e    # where terminal frontends run
```

A notification click, or a `whatevr://` link, opens the chat in the frontend
you're using, or starts the default one when none is open. Links only ever
open a chat:

```sh
whatevrd open 'whatevr://chat?phone=+15551234567'
xdg-open 'whatevr://'    # Linux, through the installed whatevr.desktop
```

## Getting it
On Arch-based systems, Whatevr is available on the AUR:
```sh
yay -S whatevr-bin
```
Note: You can install the `whatevr` or `whatevr-git` packages also if you want to build yourself

On macOS, from the Homebrew tap, prebuilt:
```sh
brew install --cask codelif/tap/whatevr
```
or built from source (`--HEAD` for main):
```sh
brew install codelif/tap/whatevr
```
The cask is ad-hoc signed, not notarized, and clears the quarantine flag on
install. `brew uninstall --cask --zap whatevr` also removes the login service
and all data.

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
**Graphical frontend (`whatkevr`):** Qt 6.10+ (Core, Gui, Multimedia, Network,
OpenGL, Qml, Quick, QuickControls2, ShaderTools, Widgets), KDE Frameworks 6
(ColorScheme, CoreAddons, DBusAddons, I18n, Kirigami, Prison, QQC2DesktopStyle),
Kirigami Addons, KQuickImageEditor, rlottie, mpv, protobuf, CMake, Ninja,
extra-cmake-modules.
**Optional at runtime:** ffmpeg, for video posters and voice note waveforms.

```sh
# Arch (daemon + terminal + graphical)
sudo pacman -S --needed base-devel go just sqlite libjpeg-turbo pkgconf \
  cmake ninja extra-cmake-modules qt6-base qt6-declarative qt6-multimedia \
  qt6-shadertools mpv ffmpeg kcoreaddons kdbusaddons ki18n kirigami \
  kirigami-addons prison qqc2-desktop-style kquickimageeditor protobuf
# rlottie comes from the AUR: yay -S rlottie

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

Build and install the graphical frontend separately with CMake (it is not
part of `just build`):

```sh
cmake -S whatkevr -B build/release/whatkevr -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DWHATEVR_VERSION_FULL="$(scripts/version.py full)" \
  -DWHATEVR_VERSION="$(scripts/version.py numeric)"
cmake --build build/release/whatkevr
cmake --install build/release/whatkevr --prefix "$HOME/.local"
# with tests: -DWHATEVR_BUILD_TESTS=ON, then
# QT_QPA_PLATFORM=offscreen ctest --test-dir build/debug/whatkevr
```

`just install` places the `whatevrd` and `whattui` binaries and the systemd user
units under the selected prefix.
Make sure the chosen `bin` directory is on your `PATH` (e.g. `~/.local/bin`).

The AUR and Homebrew packages install shell completions for `whatevrd`. With
`just install`, load them from your shell's rc file:

```sh
source <(whatevrd completion zsh)    # or bash; fish: whatevrd completion fish | source
```

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
| Group management | ✅ | Create, invite links, members, photo, announce/locked flags |
| Community management | ✅ | Link/unlink groups |
| Calls | ❌ | Voice/video calls unsupported |
| Status/stories | ✅ | Feed, post text/media, viewed flags, mutes, replies, viewers, delete |
| Settings UI | ✅ | |
| Account/profile editing | ✅ | Includes privacy settings |
| Import/export backups | ❌ | |
| DB encryption and keyring integration | ❌ | |
| Daemon SNI (Tray) | ❌ | |
| Graphical Qt frontend (`whatkevr`) | ✅ | Chat list, timeline, media gallery, settings, tray icon, notifications mute |
| Forward-to picker with archived chats | ✅ | |
| Unread-only chat filter | ✅ | Local proxy over the `all` subscription (no v2 filter API) |
| Typing-indicator preference | ✅ | Frontend-local (no v2 preference field) |
| Chat folders/lists | ✅ | Served by the `chat_folders` view and `chat_folder.*` commands |
| Favorites filter and per-chat favorite | ✅ | `CHAT_FILTER_FAVORITE` plus `chat.favorite` |
| Status/stories tab | ✅ | Served by the `status` and `status.muted` views |
| Calls tab and call history | ✅ | Ringing set from the call log plus `call_history` and `call.reject` |
| Channels tab | ✅ | Served by the `channels`/`channel_messages` views and `channel.*` commands |
| Daemon logs viewer | ✅ | Served by the `logs` view over the run log |
| Chat media links tab | ✅ | Served by the new `chat_links` view |
| Keep-archived / anti-delete / archived-mute prefs | ✅ | Native v2 preference fields |
  
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
fork of vaxis (`whattui/`); the graphical frontend is `whatkevr`, in C++/QML
on Qt 6 and Kirigami (`whatkevr/`). A scriptable CLI is wanted and
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

## macOS

macOS 13 or later is supported. The daemon and terminal frontend use native
macOS defaults; no XDG variables or shell configuration are needed to connect.
Install Apple's Command Line Tools (`xcode-select --install`) and dependencies:

```sh
brew install go just jpeg-turbo pkgconf python ffmpeg
```

Go 1.26 or later is required. ffmpeg is optional. Both macOS executables use cgo
for native path lookup; Swift builds the private notification app. The SQLite
Go driver includes SQLite itself, so a separate Homebrew SQLite installation is
not required. Clone submodules as described above, then:

```sh
just build-release
./build/release/whatevrd
# In another terminal:
./build/release/whattui
```

To install without administrator privileges:

```sh
just install "$HOME/.local"
# Add $HOME/.local/bin to PATH, or invoke these executables by absolute path.
```

The installation is `Whatevr.app` in the prefix, holding the notification helper
and `whatevrd`, with `bin/whatevrd` linking into it. Spotlight only indexes app
folders, so link it there if you want to open Whatevr from Spotlight:

```sh
ln -s "$HOME/.local/Whatevr.app" ~/Applications/Whatevr.app
```

Debug builds (`just build`, `just install-dev`) are a separate app, **Whatevr
Dev** (`in.codelif.whatevr.dev`), so they never take notification permission
from the installed one. Local builds are ad-hoc signed; they are intended for
use on the build machine. Developer ID signing and notarization are required
separately for public distribution.

Login startup is optional and never enabled by installation:

```sh
whatevrd service enable       # starts now, at GUI login, and whenever a frontend connects
whatevrd service status
whatevrd service disable      # stops the service and removes its LaunchAgent
```

The LaunchAgent holds the socket, so launchd starts the daemon again whenever a
frontend connects, including after a crash. Stop a manually running daemon before
enabling the service; while the service is enabled, running `whatevrd` by hand on
the default socket is refused. Enable the service with XDG and `WHATEVR_SOCKET`
overrides unset, so it uses the same native defaults as normal terminal clients.
The agent uses the executable path as invoked, so a Homebrew `bin` link keeps
working across upgrades, and a PATH containing the installation prefix, the
detected Homebrew prefix, and system tools. Running `whatevrd service enable`
again replaces an agent written by an older version or from another prefix.
Disable the service before uninstalling.

Notification authorization is explicit:

```sh
whatevrd notifications setup
whatevrd notifications status
```

Allow notifications in the macOS dialog. If previously denied, enable **Whatevr**
in System Settings → Notifications. Notification clicks select
the chat in a connected frontend. If no frontend is open, the default one is
started (see Frontends) and opens on that chat; if the daemon isn't running,
the click starts it through the login service.

Whatevr.app handles `whatevr://` links, for Raycast, Shortcuts or scripts:
`open 'whatevr://chat?phone=+15551234567'`. Opening the app itself brings up
the default frontend.
The sender's avatar shows as the notification thumbnail.
The helper follows system notification/sound settings; a missing helper or denied
permission does not prevent messaging. Local rebuilds may require checking
notification authorization again because they use ad-hoc signing.

Run `whatevrd paths --json` to inspect all resolved locations:

| Contents | Default location |
| --- | --- |
| Databases and WhatsApp session | `~/Library/Application Support/in.codelif.whatevr/daemon` |
| Configuration and send guard | `~/Library/Application Support/in.codelif.whatevr/config` |
| Development state and captures | `~/Library/Application Support/in.codelif.whatevr/{state,captures}` |
| Media and frontend caches | `~/Library/Caches/in.codelif.whatevr/{daemon,tui}` |
| Daemon and frontend diagnostics | `~/Library/Logs/in.codelif.whatevr/{daemon,tui}` |
| Socket and lock | OS-provided per-user temporary directory, `in.codelif.whatevr.{sock,lock}` |

The socket uses Darwin's per-user temporary directory, rather than an inherited
`TMPDIR`, so launchd and terminal sessions agree. The socket sits directly in that
private directory, sockets are mode 0600, and peers must be the same user. Existing Linux-style
macOS data is left untouched; the native location starts as a fresh linked device.
Explicit XDG overrides retain their existing layout, and `WHATEVR_SOCKET` selects
an absolute socket path. Darwin socket paths must fit within 103 bytes.

Debug mock and capture runs isolate their account state. Their Darwin sockets
use short, deterministic instance directories under the native runtime directory.
Query a mock instance with `build/debug/whatevrd paths --json --mock-dir DIR`, then
pass its `SocketPath` to `whattui --socket`. The capture startup log prints its
`WHATEVR_SOCKET`. Mock notifications remain disabled unless `--mock-notify` is set.

`just test` uses `/tmp` for temporary test files to keep existing socket test paths
short. Network and wake monitoring use Network.framework and IOKit independently
of the notification app. Linux keeps its XDG, systemd, D-Bus, and netlink support.
