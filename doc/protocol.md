# Architecture & protocol

```
┌──────────┐  NDJSON over Unix socket  ┌──────────┐
│ whatkevr │ ◄───────────────────────► │ whatevrd │
│ (Qt/QML) │  one socket, many conns   │  (Go)    │
└──────────┘                           └────┬─────┘
     ▲                                      │ whatsmeow
     │ D-Bus tray clicks                    │ (WhatsApp multidevice)
     └──────────────────────────┐     ┌─────┴──────┐
                         ┌───────┴─────┴───┐  SQLite│
                         │ StatusNotifier  │  store │
                         │ tray icon       │  media │
                         └─────────────────┘  cache │
                                                  ───┘
```

- **whatevrd** (`whatevrd/`): WhatsApp connection (whatsmeow), SQLite store,
  media cache + range streaming, D-Bus notifications, tray icon, protocol
  server. One process, one account.
- **whatkevr** (`whatkevr/`): Qt 6 + Kirigami UI. `ProtocolController` owns the
  socket; QML renders view models. Pop-out windows are extra processes with
  their own controller and connection — the daemon already multiplexes
  frontends.
- **Contract:** `PROTOCOL.md` at the repo root (version 1, additive-only).
  Commands are request/response (`send.text`, `status.download`, …); views are
  subscriptions with `upsert`/`remove`/`ready`/`reset` events (`messages`,
  `status`, `channels`, `daemon.logs`, …). Connection-directed events
  (`open_chat`, `activate_window`, `show_tray_menu`, no `sub`) target the most
  recently used live frontend.

## Frontend session

Each connection announces itself with `hello`, then reports focus/selection
with `session.update`. The daemon uses that routing state for notification
clicks and tray actions. A frontend that never sends `session.update` cannot
be targeted — tray clicks fall back to cold-starting the app via
`xdg-open whatevr://` (handled by the desktop file's `x-scheme-handler`).

## Where things live

| Concern | Daemon side | Frontend side |
|---|---|---|
| Chat/message ingest | `whatevrd/internal/wa/messages.go` | `MessageView.qml`, `ChatBubble.qml` |
| Status ingest/download | `wa/status.go`, `wa/media_save.go` | `StatusPage.qml`, `StatusViewerPage.qml` |
| Channels | `wa/channels.go`, `channels_view.go` | `ChannelsPage.qml` |
| Calls (signaling only) | `wa/calls.go` | Calls tab, `call.reject` |
| Tray | `tray/tray.go`, `protocol/activate_window.go` | `Main.qml` tray menu |
| Preferences | `app.AppPreferences`, `preferences.set` | settings pages, `setAppPreference` |
| Media pipeline | `wa/media_*.go`, `mediastream/` | mpv items, `MediaViewer.qml` |
