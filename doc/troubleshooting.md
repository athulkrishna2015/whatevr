# Troubleshooting

## Tray clicks do nothing

Chain: tray icon (daemon `tray/`) → `activate_window` / `show_tray_menu`
protocol event → frontend raises window / shows menu.

1. Is a frontend connected? `pgrep -a whatkevr`. With none running, a click
   cold-starts via `xdg-open whatevr://` (test it by hand).
2. With one running, trigger the paths directly:
   ```sh
   DPID=$(systemctl --user show whatevrd.service -p MainPID --value)
   dbus-send --session --print-reply \
     --dest=org.kde.StatusNotifierItem-$DPID-1 /StatusNotifierItem \
     org.kde.StatusNotifierItem.Activate int32:0 int32:0
   ```
   No new `whatkevr` process afterwards means the event reached the frontend
   (a spawn means no live session was found).
3. The daemon only targets sessions that sent `session.update` (focus/chat
   selection). An old frontend ignores the new events (protocol rule 5):
   restart it after upgrading.
4. A pulsing icon is NeedsAttention = unread chats exist. It settles once
   chats are read.

History: the shipped daemon once called `tray.Start` without the
protocol-server activator, so clicks went nowhere; and the log line `tray:
activated (no window in daemon mode)` came from that era.

## Logs tab is empty

The daemon serves `daemon.logs` (verify over the socket — recipe below). If
the page stays empty with no error, suspect the frontend build: rows without
an `id` inside their data are dropped by `CollectionViewModel::onUpsert`
(protocol rule 3). Known instance: `logsItem` once lacked the id.

## Statuses don't load

1. Ask the daemon directly whether the row has media/payload and try the
   download yourself (a fire-and-forget `status.download`; success is silent,
   failure logs `protocol: status.download <id>: …`).
2. `journalctl --user | grep whatkevr` shows QML TypeErrors. A null
   `Whatevr.ProtocolController` at page teardown is benign shutdown noise;
   errors during use are real bugs.
3. The viewer auto-downloads on open *and* viewing marks trigger a
   daemon-side fetch, so a persistent Load button with no daemon log line
   means the request never left the frontend.

## Talking to the daemon by hand

```sh
python3 -u -c "
import socket, json
s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
s.settimeout(5)
s.connect('/run/user/1000/whatevr/whatevrd.sock')
f = s.makefile('r')
s.sendall(b'{\"id\":1,\"method\":\"hello\",\"params\":{\"client\":\"dbg\",\"protocol\":1}}\n')
print(f.readline())
s.sendall(b'{\"id\":2,\"method\":\"daemon.logs\",\"params\":{\"limit\":10}}\n')
print(f.readline())
"
```

Useful views to subscribe: `status`, `status.kept`, `chats`,
`channel_messages` (`{"channel_id": …}`), `daemon.logs` (`{"limit": …}`).

## Upgrading

Install with the positional prefix form (`just install /home/admin/.local`,
never `prefix=…`), `systemctl --user restart whatevrd.service`, then quit and
reopen `whatkevr`. Check the running versions: `readlink -f
/proc/$(systemctl --user show whatevrd.service -p MainPID --value)/exe`
should not say `(deleted)`, and the daemon log should show a fresh
`listening on …` line.
