# Safety policy

WhatsApp bans accounts for automation and protocol abuse. Whatevr is already
a third-party client (baseline risk the user accepts by running it), so every
feature must avoid adding *new* detectable abnormal behavior. The rule:

- **Local-only is safe.** Rendering, storing, organizing, or hiding data the
  account already received changes nothing on the wire: anti-delete display,
  edit history, status keep/archive, themes, filters, folders, local search,
  transcription, backups. These can never get an account flagged.
- **Normal user actions are safe.** Sending what the user typed, attaching
  what the user picked, @-mentioning members, muting, blocking, following
  channels — ordinary protocol traffic at human pace.
- **Passive omission is low-risk.** Not announcing typing presence is
  indistinguishable from an idle client. Off by default-behavior change, on
  by toggle.
- **Refused.** Anything that lies to the server or behaves no official client
  does (see below). These are routinely cited in ban reports and are out of
  scope no matter how often requested.

## Refused features (with reasons)

| Feature | Why |
|---|---|
| Placing/answering calls from desktop | No media stack upstream (whatsmeow); impossible, not just risky. |
| Late revoke ("delete anytime") | Server rejects past-window revokes; hammering it is abnormal. |
| Unlimited forwards / mass sender | Bulk sending is the #1 ban trigger. |
| Auto-reply bots on personal accounts | Autonomous sending reads as bot behavior. |
| Read-receipt spoofing (read without blue ticks) | Receipt state is server-tracked; lying is detectable. |
| Freeze-last-seen / ghost-online | Presence lies are server-visible anomalies. |
| Sending past server media/size caps | Server-enforced; oversized uploads fail or flag. |
| View-once bypass beyond explicit save | Only the deliberate per-item user save exists. |

Ghost story viewing needs no feature: viewed receipts are never sent, so
watching a status is already invisible. Others' deleted statuses are likewise
already kept (revokes only ever touched chat rows).
