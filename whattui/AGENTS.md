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
- Run offscreen frame tests and real kitty screenshots for UI changes:
  `just test-whattui` and `just screenshot`. The screenshot harness builds a
  dedicated kitty on a Hyprland dummy monitor it creates and removes, so it
  never touches the screen you are looking at.
- A dummy monitor suspends the toplevel on it, so kitty reports the window
  hidden and rendering stops. `WHATTUI_ALWAYS_RENDER=1` draws anyway; the
  harness sets it everywhere except the scenario that tests coming back from a
  hidden tab.
- Never test against the real daemon account. Use `protocol-fixture` with synthetic data.
- Verify Vaxis with normal tests, `kittyonly` tests, and vet. Verify pawbar with normal and `kittyonly` builds.
- Commit after each completed work unit. Sign every commit. Use single-line subjects and no co-author trailers.
