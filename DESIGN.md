---
version: alpha
name: whatevr-whatsapp-linux
description: "Linux-first WhatsApp client (Qt/Kirigami whatkevr + terminal whattui). Friendly conversational UI inspired by Intercom: rounded bubbles, presence/typing/read states, calm surfaces. Adapted for KDE Breeze light/dark, system fonts, WhatsApp green (#25D366 fills, #0B6B5E text-safe actions) as the single action accent. Thin frontends over whatevrd daemon; no marketing chrome."

colors:
  primary: "#0B6B5E"
  on-primary: "#ffffff"
  wa-green: "#25D366"
  wa-green-ink: "#0B6B5E"
  wa-teal-dark: "#00A884"
  ink: "#111111"
  ink-muted: "#626260"
  canvas: "#EFEAE2"
  surface-1: "#ffffff"
  surface-2: "#F0F2F5"
  chat-out-light: "#D9FDD3"
  chat-in-light: "#ffffff"
  chat-out-dark: "#005C4B"
  chat-in-dark: "#202C33"
  inverse-canvas: "#0B141A"
  inverse-surface-1: "#202C33"
  inverse-ink: "#E9EDEF"
  inverse-ink-muted: "#8696A0"
  hairline: "#d3cec6"
  hairline-soft: "#ebe7e1"
  hairline-dark: "#2A3942"
  semantic-error: "#c41c1c"

typography:
  display-xl:
    fontFamily: Inter, system-ui
    fontSize: 72px
    fontWeight: 500
    lineHeight: 1.05
    letterSpacing: -2.0px
  display-lg:
    fontFamily: Inter, system-ui
    fontSize: 56px
    fontWeight: 500
    lineHeight: 1.10
    letterSpacing: -1.4px
  display-md:
    fontFamily: Inter, system-ui
    fontSize: 40px
    fontWeight: 500
    lineHeight: 1.15
    letterSpacing: -0.8px
  headline:
    fontFamily: Inter, system-ui
    fontSize: 28px
    fontWeight: 500
    lineHeight: 1.20
    letterSpacing: -0.5px
  card-title:
    fontFamily: Inter, system-ui
    fontSize: 22px
    fontWeight: 500
    lineHeight: 1.25
    letterSpacing: -0.3px
  subhead:
    fontFamily: Inter, system-ui
    fontSize: 20px
    fontWeight: 400
    lineHeight: 1.40
    letterSpacing: -0.2px
  body-lg:
    fontFamily: Inter, system-ui
    fontSize: 18px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: -0.1px
  body:
    fontFamily: Inter, system-ui
    fontSize: 16px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0
  body-sm:
    fontFamily: Inter, system-ui
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0
  caption:
    fontFamily: Inter, system-ui
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.40
    letterSpacing: 0
  button:
    fontFamily: Inter, system-ui
    fontSize: 15px
    fontWeight: 500
    lineHeight: 1.20
    letterSpacing: 0
  eyebrow:
    fontFamily: Inter, system-ui
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.30
    letterSpacing: 0
  mono:
    fontFamily: JetBrains Mono, ui-monospace
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  xxl: 24px
  pill: 9999px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px
  section: 96px

components:
  button-primary:
    backgroundColor: "{colors.wa-green-ink}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 10px 18px
  button-primary-pressed:
    backgroundColor: "{colors.inverse-canvas}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
  button-secondary:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 10px 18px
  button-tertiary:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 10px 18px
  button-fin:
    backgroundColor: "{colors.wa-green-ink}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 10px 18px
  button-primary-dark:
    backgroundColor: "{colors.wa-teal-dark}"
    textColor: "{colors.inverse-canvas}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 10px 18px
  unread-badge:
    backgroundColor: "{colors.wa-green}"
    textColor: "{colors.inverse-canvas}"
    typography: "{typography.caption}"
    rounded: "{rounded.full}"
    padding: 2px 7px
  message-status-error:
    backgroundColor: "{colors.semantic-error}"
    textColor: "{colors.on-primary}"
    typography: "{typography.caption}"
    rounded: "{rounded.sm}"
    padding: 2px 8px
  chat-list-row:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 12px 16px
  chat-list-row-selected:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 12px 16px
  message-bubble-in:
    backgroundColor: "{colors.chat-in-light}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 8px 12px
  message-bubble-out:
    backgroundColor: "{colors.chat-out-light}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 8px 12px
  message-bubble-in-dark:
    backgroundColor: "{colors.chat-in-dark}"
    textColor: "{colors.inverse-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 8px 12px
  message-bubble-out-dark:
    backgroundColor: "{colors.chat-out-dark}"
    textColor: "{colors.inverse-ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: 8px 12px
  message-composer:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.xl}"
    padding: 10px 14px
  status-chip:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 6px 12px
  date-pill:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.md}"
    padding: 6px 12px
  list-divider:
    backgroundColor: "{colors.hairline}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 0 16px
  bubble-divider:
    backgroundColor: "{colors.hairline-soft}"
    textColor: "{colors.ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 0 12px
  list-divider-dark:
    backgroundColor: "{colors.hairline-dark}"
    textColor: "{colors.inverse-ink}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 0 16px
  caption-dark:
    backgroundColor: "{colors.inverse-canvas}"
    textColor: "{colors.inverse-ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 2px 6px
  text-input:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 10px 14px
  text-input-focused:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    padding: 10px 14px
  top-nav:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body-sm}"
    rounded: "{rounded.xs}"
    height: 56px
  footer:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.xs}"
    padding: 64px 32px
---

## Overview

Whatevr is a Linux-first WhatsApp client: `whatkevr` (Qt 6 / Kirigami QML) + `whattui` (terminal) as thin frontends over the `whatevrd` daemon. Inspiration is Intercom's conversational calm — rounded bubbles, readable density, presence/typing/read states — re-grounded for KDE Breeze light/dark, system fonts, and WhatsApp green as the single action color.

Light: warm chat canvas (`{colors.canvas}` #EFEAE2), white in-bubbles (`{colors.chat-in-light}`), pale-green out-bubbles (`{colors.chat-out-light}` #D9FDD3). Dark: deep ground (`{colors.inverse-canvas}` #0B141A), in-bubbles `{colors.chat-in-dark}` #202C33, out-bubbles `{colors.chat-out-dark}` #005C4B. Accents are WhatsApp green: fills and badges in `{colors.wa-green}` #25D366 with dark text, text-safe actions in `{colors.wa-green-ink}` #0B6B5E (light) and `{colors.wa-teal-dark}` #00A884 (dark) — send, ticks, selected states only.

Type is system-native: Inter / system-ui for UI (Kirigami default), JetBrains Mono for timestamps/code. No proprietary Saans. QML maps: `ChatListPane.qml` → chat-list-row, `ConversationPane.qml` / `MessageView.qml` → message-bubble-in/out, `MessageComposer.qml` → message-composer, `Palette.qml` is token source of truth — never hardcode hex in QML.

**Key Characteristics:**
- **Chat canvas, not marketing canvas** — list → timeline → composer 3-pane at desktop; single-pane with back-nav on narrow.
- **Bubbles carry meaning**: in-white vs out-green (light), in-slate vs out-teal-green (dark); ticks/retry/error in accent/semantic only.
- **System fonts only**; hierarchy by size + weight, not family change.
- **WhatsApp green** `{colors.wa-green-ink}` is the action color — send, confirm, selected. Badge fills use `{colors.wa-green}`. Never decorative fills.
- Corners `{rounded.lg}` 12px bubbles, `{rounded.xl}` 16px composer/cards, `{rounded.full}` avatars — never square bubbles.
- Respects Breeze/Kirigami ColorScheme + 16-color terminal fallback in `whattui`.
- Chat text stays at 0 tracking; display sizes (login, empty states) may pull slightly negative.
- Bubbles `{rounded.lg}` 12px, composer/dialogs `{rounded.xl}` 16px, avatars `{rounded.full}` — never square bubbles, never pill buttons.

## Colors

> Adapted from Intercom's conversational palette for a WhatsApp client on KDE Breeze. Light values are defaults; dark values use `inverse-*` + `chat-*-dark`.

### Brand & Accent
- **WhatsApp green** (`{colors.wa-green-ink}` #0B6B5E light, `{colors.wa-teal-dark}` #00A884 dark accents): the single action color — send button, read ticks, selected chat, links. Never decorative fills.
- **Badge green** (`{colors.wa-green}` #25D366): unread badges and fills, always with dark text.
- **Primary** ({colors.primary}): send/confirm actions. Text on accent is `{colors.on-primary}` white.
- **Charcoal** ({colors.ink}): light-mode text primary — message text, headlines, button labels.

### Surface
- **Canvas** ({colors.canvas}): light chat ground #EFEAE2.
- **Surface 1** ({colors.surface-1}): cards, composer, list background — white in light.
- **Surface 2** ({colors.surface-2}): selected rows, date pills, alt bands — #F0F2F5 in light.
- **In-bubble** (`{colors.chat-in-light}` white) vs **out-bubble** (`{colors.chat-out-light}` #D9FDD3): direction is color, not alignment alone.
- **Hairline** ({colors.hairline}): 1px dividers between chats; `{colors.hairline-soft}` for in-bubble dividers.
- **Inverse Canvas** ({colors.inverse-canvas}): dark chat ground #0B141A.
- **Inverse Surface 1** ({colors.inverse-surface-1}): dark cards/rows #202C33; dark divider `{colors.hairline-dark}` #2A3942.

### Text
- **Ink** ({colors.ink}): message + headline text in light.
- **Ink Muted** ({colors.ink-muted}): timestamps, previews, secondary rows, helper text.
- **Inverse Ink** ({colors.inverse-ink}): dark-mode text #E9EDEF.
- **Inverse Ink Muted** ({colors.inverse-ink-muted}): dark secondary #8696A0.

### Semantic
- **Error Red** ({colors.semantic-error}): failed send, destructive confirms (`message-status-error`).
- Success, presence, and read states reuse the WhatsApp greens — no separate success hue.

## Typography

### Font Family

- **Inter / system-ui** — Carries display, body, eyebrow, and button. In QML this means the Kirigami/system default font — never bundle a custom sans.
- **JetBrains Mono / ui-monospace** — Timestamps, message-info diagnostics, `whatevrd logs` viewer, code snippets.

Hierarchy is carried by size + weight, not by family change.

### Hierarchy

| Token | Size | Weight | Line Height | Letter Spacing | Use |
|---|---|---|---|---|---|
| `{typography.display-xl}` | 72px | 500 | 1.05 | -2.0px | Login/QR pairing headline (scales down on narrow) |
| `{typography.display-lg}` | 56px | 500 | 1.10 | -1.4px | Empty-state headline (no chats, no results) |
| `{typography.display-md}` | 40px | 500 | 1.15 | -0.8px | Section headline in settings |
| `{typography.headline}` | 28px | 500 | 1.20 | -0.5px | Dialog titles, media viewer title |
| `{typography.card-title}` | 22px | 500 | 1.25 | -0.3px | Contact/group names in headers |
| `{typography.subhead}` | 20px | 400 | 1.40 | -0.2px | Chat preview lead line |
| `{typography.body-lg}` | 18px | 400 | 1.50 | -0.1px | Large message text (accessibility scale) |
| `{typography.body}` | 16px | 400 | 1.50 | 0 | Message text default |
| `{typography.body-sm}` | 14px | 400 | 1.50 | 0 | Chat list preview, settings rows |
| `{typography.caption}` | 12px | 400 | 1.40 | 0 | Timestamps, ticks, presence |
| `{typography.button}` | 15px | 500 | 1.20 | 0 | Button labels |
| `{typography.eyebrow}` | 14px | 500 | 1.30 | 0 | Section eyebrow, sentence case |
| `{typography.mono}` | 13px | 400 | 1.50 | 0 | Timestamps, logs, code |

### Principles

- **Weight 500 carries headings.** Medium reads as confident without bold.
- **Chat text stays at 0 tracking.** Negative tracking only on large display sizes.
- **Line-heights relax on body.** 1.50 on message text for multi-line readability.
- **Mono only for time/code/logs.** Chat chrome stays in the system sans.
- **Eyebrow uses sentence case** at 14px / 500 weight — no all-caps tracking.

### Note on Fonts

Use the KDE system font stack (Kirigami default). Inter is the closest free match when a bundled fallback is needed; JetBrains Mono at 400 covers mono. Never ship a proprietary brand font.

## Layout

### Spacing System

- **Base unit**: 8px.
- **Tokens (front matter)**: `{spacing.xxs}` 4px · `{spacing.xs}` 8px · `{spacing.sm}` 12px · `{spacing.md}` 16px · `{spacing.lg}` 24px · `{spacing.xl}` 32px · `{spacing.xxl}` 48px · `{spacing.section}` 96px.
- Bubble interior: 8px vertical · 12px horizontal; bubble gaps `{spacing.xs}` 8px, message groups `{spacing.md}` 16px.
- Composer padding 10px 14px; list rows 12px 16px; settings cards `{spacing.lg}` 24px.

### Grid & Container

- Desktop: 3-pane (sidebar ~300px | timeline flex, max ~800px | optional info/media rail as overlay).
- Narrow (<768px): single-pane with back navigation; list and conversation never share the viewport.
- Media gallery and Status viewer go full-bleed overlay, not inline columns.

### Whitespace Philosophy

Density is a feature in a chat client: rows sit tight (`{spacing.xs}` gaps) so dozens of chats scan at once, while the open conversation breathes with group spacing (`{spacing.md}`) and date pills. Sections separate by surface change, not big marketing gaps.

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| 0 (flat) | No shadow, no border | Chat ground, message text, footer |
| 1 (lift) | `{colors.surface-1}` on `{colors.canvas}` | Chat list, composer, settings cards |
| 2 (hairline lift) | `{colors.surface-1}` + 1px `{colors.hairline}` (dark: `{colors.hairline-dark}`) | Bubbles, dialogs, popups |
| 3 (overlay) | Scrim + lifted surface | Media viewer, emoji/sticker picker, call overlay |

No drop shadows on bubbles in light mode — direction color + hairline carry depth. Overlays and the emoji picker may use a restrained shadow.

### Decorative Depth

- **Conversation is the protagonist**: the timeline carries color (in/out bubbles); chrome around it stays quiet.
- **No atmospheric gradients, no spotlight cards.** Restraint keeps day-long chat readable.

## Shapes

### Border Radius Scale

| Token | Value | Use |
|---|---|---|
| `{rounded.xs}` | 4px | Small chips, badges |
| `{rounded.sm}` | 6px | Inline tags, reaction pills |
| `{rounded.md}` | 8px | Buttons, inputs, date pills |
| `{rounded.lg}` | 12px | Message bubbles, list rows, settings cards |
| `{rounded.xl}` | 16px | Composer, dialogs, media cards |
| `{rounded.xxl}` | 24px | Full-screen sheets (Status viewer) |
| `{rounded.pill}` | 9999px | Status chips, typing indicator |
| `{rounded.full}` | 9999px | Avatars (40–48px), call buttons |

### Media & Avatar Geometry

- Message media (image/video) fills the bubble with `{rounded.lg}` clipping; captions sit inside the bubble below the media.
- Avatars are always `{rounded.full}`: 40px list rows, 32px group members, 48px headers/viewers.
- Outgoing bubbles may square one corner (tail side) at most — never fully square bubbles.

## Components

### Buttons

**`button-primary`** — WhatsApp-green send/confirm.
- Background `{colors.wa-green-ink}` (dark: `button-primary-dark` on `{colors.wa-teal-dark}`), text `{colors.on-primary}`, type `{typography.button}`, padding 10px 18px, rounded `{rounded.md}`.
- Pressed state lives in `button-primary-pressed`.

**`button-secondary`** — Quiet action on surface.
- Background `{colors.surface-1}`, text `{colors.ink}`, type `{typography.button}`, padding 10px 18px, rounded `{rounded.md}`. 1px `{colors.hairline}` border (dark: `{colors.hairline-dark}`).

**`button-tertiary`** — Text-only action (cancel, skip).
- Transparent over parent, text `{colors.ink}`, type `{typography.button}`, rounded `{rounded.md}`, padding 10px 18px.

**`button-fin`** — Alias of send-green (kept for schema compat; prefer `button-primary`).
- Background `{colors.wa-green-ink}`, text `{colors.on-primary}`, type `{typography.button}`, rounded `{rounded.md}`, padding 10px 18px.

**`button-primary-dark`** — Dark-scheme send/confirm with dark text on teal.
- Background `{colors.wa-teal-dark}`, text `{colors.inverse-canvas}`, type `{typography.button}`, rounded `{rounded.md}`, padding 10px 18px.

**`unread-badge`** — Unread count pill on chat rows and app icon.
- Background `{colors.wa-green}`, text `{colors.inverse-canvas}`, type `{typography.caption}`, rounded `{rounded.full}`, padding 2px 7px.

**`message-status-error`** — Failed-send flag on the affected bubble.
- Background `{colors.semantic-error}`, text `{colors.on-primary}`, type `{typography.caption}`, rounded `{rounded.sm}`, padding 2px 8px.

### Chat list

**`chat-list-row`** + **`chat-list-row-selected`** — One row per chat in `ChatListPane.qml`: avatar, name, preview, time, unread badge, mute/pin icons.
- Default on `{colors.surface-1}`; selected on `{colors.surface-2}`. Unread badge is green with `{typography.caption}` count.

### Conversation

**`message-bubble-in`** — Incoming bubble in `MessageView.qml`.
- Background `{colors.chat-in-light}` (dark: `message-bubble-in-dark` on `{colors.chat-in-dark}`), text `{colors.ink}` (dark: `{colors.inverse-ink}`), type `{typography.body}`, rounded `{rounded.lg}`, padding 8px 12px.

**`message-bubble-out`** — Outgoing bubble.
- Background `{colors.chat-out-light}` (dark: `message-bubble-out-dark` on `{colors.chat-out-dark}`), same type/padding. Ticks use WhatsApp green; errors use `{colors.semantic-error}`.

**`message-bubble-in-dark`** / **`message-bubble-out-dark`** — Dark-scheme incoming/outgoing.
- Backgrounds `{colors.chat-in-dark}` / `{colors.chat-out-dark}`, text `{colors.inverse-ink}`, type `{typography.body}`, rounded `{rounded.lg}`, padding 8px 12px.

**`message-composer`** — `MessageComposer.qml`: rounded input with attach, emoji, voice, send.
- Background `{colors.surface-1}`, text `{colors.ink}`, type `{typography.body}`, rounded `{rounded.xl}`, padding 10px 14px. Send affordance turns green when there is content.

**`date-pill`** + **`status-chip`** — Centered timeline markers (date separators, encryption notice, typing).
- Date pill on `{colors.surface-2}`; status/typing chips `{rounded.pill}` with `{typography.caption}` muted text.

**`list-divider`** / **`bubble-divider`** / **`list-divider-dark`** — 1px rules between chats and inside bubbles.
- Backgrounds `{colors.hairline}` / `{colors.hairline-soft}` / `{colors.hairline-dark}`, label text `{colors.ink}` (dark: `{colors.inverse-ink}`), type `{typography.caption}`.

**`caption-dark`** — Dark-scheme timestamp/meta text on the chat ground.
- Background `{colors.inverse-canvas}`, text `{colors.inverse-ink-muted}`, type `{typography.caption}`, rounded `{rounded.xs}`, padding 2px 6px.

### Inputs & Forms

**`text-input`** + **`text-input-focused`** — Search fields, phone-number entry on pairing, settings edits.
- Background `{colors.surface-1}`, text `{colors.ink}`, type `{typography.body}`, rounded `{rounded.md}`, padding 10px 14px. Focus ring is green.

### Navigation

**`top-nav`** — Conversation header (avatar, name, presence, call/media/search actions) and sidebar header.
- Background `{colors.surface-1}`, text `{colors.ink}`, type `{typography.body-sm}`, height 56px.

### Footer

**`footer`** — Settings about-page and debug info block.
- Background `{colors.canvas}` (dark: `{colors.inverse-canvas}`), text `{colors.ink-muted}`, type `{typography.caption}`, padding 64px 32px.

## Do's and Don'ts

### Do

- Keep `{colors.canvas}` / `{colors.inverse-canvas}` as the chat ground per color scheme — never pure white in dark mode.
- Signal direction with bubble color (`chat-in-*` vs `chat-out-*`), not alignment alone.
- Use WhatsApp green ONLY for send, read ticks, selected/link states.
- Pair headings at weight 500 with message text at 400, all in the system font.
- Route every new QML color through `Palette.qml` tokens first.
- Use `{rounded.lg}` 12px for bubbles and `{rounded.xl}` 16px for composer/dialogs.
- Keep chat text at 0 tracking; timestamps in `{typography.caption}` muted.

### Don't

- Don't use green as a section background or full-bleed fill.
- Don't add drop shadows to bubbles.
- Don't introduce a second display family or ship a brand font.
- Don't pill-round buttons or square off bubbles.
- Don't write all-caps tracked eyebrows in chat chrome.
- Don't promote report/chart colors to chat surfaces.
- Don't hardcode hex in QML — no literals outside `Palette.qml`.
- Don't break 16-color terminal fallback in `whattui` (green → bold-green, slate → dim).

## Responsive Behavior

### Breakpoints

| Name | Width | Key Changes |
|---|---|---|
| Desktop-XL | 1440px | 3-pane with info/media rail |
| Desktop | 1280px | List + timeline; rail overlays |
| Tablet | 1024px | Narrower list (~280px); timeline flexes |
| Mobile-Lg | 768px | Single-pane; list ↔ conversation via back nav |
| Mobile | 480px | Composer condenses; timeline type holds 16px |

### Touch Targets

- Composer send/attach/voice hold ≥44px tap targets.
- Chat rows hold ≥64px row height with full-row tap.
- Reaction/long-press actions hold ≥40px targets.

### Collapsing Strategy

- **Top nav**: conversation actions collapse into overflow below 768px; back + avatar + call stay visible.
- **Panes**: list and conversation never share <768px; media gallery/Status always overlay.
- **Display type**: login/empty-state display scales 72px → ~32px on narrow.

### Image Behavior

- Timeline media keeps aspect ratio, caps at bubble width; tap opens the full viewer uncropped.
- Avatars stay circular at all sizes; group stacks overlap by 25%.

## Iteration Guide

1. Focus on ONE component at a time and reference it by its `components:` token name.
2. When adding UI, decide first: chat ground, bubble (in/out + scheme), or lifted surface.
3. Default message text to `{typography.body}` at weight 400, timestamps to `{typography.caption}`.
4. Run `npx @google/design.md lint DESIGN.md` after edits.
5. Add new variants as separate component entries (e.g. `message-bubble-out-error`).
6. Treat WhatsApp green as an action accent: send, ticks, selected, links only.
7. Map every token into `whatkevr/src/qml/Palette.qml` before using it in a view.

## Known Gaps

- Dark-mode bubble/tick values are specified here but `Palette.qml` is the enforcement point — verify contrast on-device in both Breeze schemes.
- Voice-note waveforms and video posters depend on runtime ffmpeg; poster fallback styling is not tokenized yet.
- `whattui` 16-color degradation mapping is described in Don'ts but not enumerated per token.
- Call-overlay (ringing/active) elevation and Status-viewer sheet motion are not tokenized yet.
