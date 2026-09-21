# Telegram Desktop (tdesktop) reference notes

Sibling checkout: `/mnt/0946E88701BE265B/portable/tdesktop`
(`Telegram/SourceFiles`). tdesktop is Qt Widgets C++ on the Telegram
protocol; whatkevr is QML/Kirigami on the whatevr daemon (WhatsApp via
whatsmeow). Nothing ports verbatim — only behaviors, layout math and
timings. Per-round verdicts below; "adopted" means shipped in whatkevr.

## Area verdicts

| Area | tdesktop | whatkevr | Verdict |
|---|---|---|---|
| Chat list rows (`dialogs/dialogs_row`, `dialogs/ui/dialogs_layout`) | Unread + @mention badges, mute/pin icons, typing replaces preview, draft with red prefix, tick icons | Same except the @mention pill (daemon `chats` view exposes no per-chat mention flag) | Parity except mention pill → TODO (needs daemon `chats` flag + delegate pill) |
| Message grouping (`history/history_item`, sender runs) | Tight spacing, avatar/name only on group edges, small corner radius mid-group | Same via `groupStart/groupEnd`, `showSenderHeader/Avatar/Gutter` | Parity, keep ours |
| Date pills + floating date (`history_inner_widget`, day separators) | Inline pills + floating pill while scrolling, handoff at top | Same (`DateSeparatorPill`, `floatingDateText`, handoff) | Parity, keep ours |
| Unread divider (`history_unread_things`) | "N unread messages" anchor row | Same (`UnreadSeparator`, oldest-unread anchor) | Parity, keep ours |
| Forwarded header (`history_item_components: HistoryMessageForwarded`, `lng_forwarded`) | "Forwarded" / "Forwarded from X" header above content | Daemon stored+sent `forwarded`, frontend dropped it | tdesktop better → **adopted**: `IsForwardedRole` + bubble header (`ChatBubble.forwardedLoader`) |
| Reply quotes + jump-to-original | Quote block, click scrolls to original with glow | Same (`ReplyPreview`, `jumpToReplyTarget`, reply glow) | Parity, keep ours |
| Albums / grouped media (`ui/grouped_layout`) | Collage layout for consecutive photos/videos | Multi-file sends serially, no collage grouping | tdesktop better → TODO (needs grouping math port + bubble slot) |
| Composer (`chat_helpers/message_field`, `field_autocomplete`) | Formatting popup on selection, autocomplete, char count | Ctrl+B/I/U shortcuts, reply/edit banners, emoji+@mention autocomplete | Parity on essentials; selection popup → TODO (small, QML-only) |
| Emoji/stickers/GIF panels (`chat_helpers/*stickers*`, `tabbed_panel`) | Tabbed emoji/sticker/GIF panel | Same (`ExpressionPicker`, `EmojiPane`, `StickerPane`) | Parity, keep ours |
| Media viewer (`window/window_media_preview`) | Zoom/pan, gallery arrows, caption, save/open-with | Basic viewer, zoom/pan still TODO | tdesktop better → TODO (already tracked) |
| Message info (forward/tick details, accessibility text) | Forwarded + via-bot lines in item info/accessibility | Sent/delivered/read/played + sender client | Adopted the forwarded part via header + snapshot `isForwarded`; rest N/A to WhatsApp |
| Object budgets / perf (`rows_scroll_cache`, custom paint) | Custom paint + row caches, cheap delegates | DN9 object budgets, lazy Loaders, pooled delegates | Same idea, different engine — keep ours |
| Animations (`window_slide_animation`, highlight fade) | QWidget timelines (slide ~200-250ms, ease-out; highlight fade) | Kirigami ColumnView slide (platform-owned), shared reply-glow | Timings portable on paper, but our slide is platform-owned — no direct port; reply-glow kept |

## Rules for future ports

- Port behaviors and numbers (durations, easings, spacing, badge rules), never
  widgets. tdesktop timing constants are the only thing worth lifting literally.
- Daemon-owned state stays daemon-owned: any tdesktop pattern that needs a new
  per-chat/per-message flag (mention pill, album group id) needs a protocol
  addition first, frontend second.
- Every new always-instantiated delegate object needs a deliberate
  `tst_chatbubbleperf` budget raise (see `doc/build.md`).
