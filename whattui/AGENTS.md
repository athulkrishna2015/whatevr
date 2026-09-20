# whattui agent rules

- `PROTOCOL.md` owns daemon and frontend behavior. Do not change it without explicit approval.
- The daemon owns state, ordering, sorting, merging, deduplication, and durable caches.
- The frontend may cache presentation work only.
- Every user action lives in one command registry. Slash commands, the palette, help, hints, and keybindings project that registry.
- Use one selector implementation for commands, chats, mentions, emoji, forwarding, stickers, and themes.
- Modal input takes precedence over pane input. Escape pops exactly one level.
- Text over kitty graphics sets foreground only. A cell background hides graphics at negative z-index.
- Geometry stays identical across capability tiers.
- Anything clickable visibly responds to pointer hover or press.
- Frames are tested offscreen, by cells and placements: `just test-whattui`.
  Nothing compares screenshots; a png diff says a pixel moved and never says
  which rule was broken, and the frame tests already hold the rules.
- Look at what you changed: `just screenshot` photographs whattui at any size
  and in any state, in a dedicated kitty on a Hyprland dummy monitor it
  creates and removes, so it never touches the screen you are looking at.
  `--list` for the states, `--size`, `--keys` and `--wait` for anything else.
- A dummy monitor suspends the toplevel on it, so kitty reports the window
  hidden and rendering stops. `WHATTUI_ALWAYS_RENDER=1` draws anyway; the
  harness sets it except when it is shrinking a window behind a covering
  tab.
- Never test against the real daemon account. Use `protocol-fixture` with synthetic data.
- Verify Vaxis with normal tests, `kittyonly` tests, and vet. Verify pawbar with normal and `kittyonly` builds.
- Commit after each completed work unit. Sign every commit. Use single-line subjects and no co-author trailers.
