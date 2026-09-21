# whattui agent rules

- `PROTOCOL.md` owns daemon and frontend behavior. Do not change it without explicit approval.
- The daemon owns state, ordering, sorting, merging, deduplication, and durable caches.
- The frontend may cache presentation work only.
- Every user action lives in one command registry. Slash commands, the palette, help, hints, and keybindings project that registry.
- Use one selector implementation for commands, chats, mentions, emoji, forwarding, stickers, and themes.
- The transcript draws runs, not messages: one rule in the speaker's colour
  down the side of everything they said without interruption, their name and
  disc once at the top, and the time in a gutter rail outside the words. A
  shape per message is a screenful of boxes. Boxes are opt-in (`/boxes`).
- The pointer lights the one message it is over, because a run is one shape and
  something has to say where one message in it ends.
- Modal input takes precedence over pane input. Escape pops exactly one level.
- Text over kitty graphics sets foreground only. A cell background hides graphics at negative z-index.
- Geometry stays identical across capability tiers.
- Text sizing is its own capability, not part of the tier: a terminal with
  kitty graphics and no OSC 66 scaling is a real terminal. Anything that asks
  for a scaled glyph checks `caps.TextScale` and picks a layout that works at
  natural size, because vaxis correctly draws an unscalable glyph in the top
  left of the block it reserved. `WHATTUI_NO_TEXT_SCALE=1` stands that
  terminal in, and `just screenshot --env WHATTUI_NO_TEXT_SCALE=1` photographs
  it.
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
- Never hold `App.mu` while asking a collection anything, and never ask a
  collection anything from inside its own `Read`: the window arrives as
  `view.State` for exactly that reason. Both are deadlocks the moment the
  daemon has a message to deliver, and they present as a frozen terminal.
- Never test against the real daemon account. Use a mock scenario: the golden
  frames and `just screenshot` both run a real `whatevrd --mock` against a fake
  WhatsApp server, so everything below the socket is production code and
  nothing is a real account. `just screenshot --account <name>` picks the
  scenario; `whatevrd --mock-list` names them. `torture`, `flood` and `fuzz`
  are the ones that exist to break a renderer.
- Verify Vaxis with normal tests, `kittyonly` tests, and vet. Verify pawbar with normal and `kittyonly` builds.
- Commit after each completed work unit. Sign every commit. Use single-line subjects and no co-author trailers.
