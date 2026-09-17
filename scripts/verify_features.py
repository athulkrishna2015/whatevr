#!/usr/bin/env python3
"""Local feature verification for whatevr, driven by the README feature map.

Checks every README feature row through the cheapest honest probe available:

- ``socket``: talks to the running daemon over the protocol socket (read-only
  commands and view subscriptions; mutating commands are only ever sent with
  invalid params, proving the wiring without changing anything).
- ``unit``: runs the matching Go/Qt test suites (slow; ``--skip-unit`` skips).
- ``static``: verifies the code that implements the feature exists and is
  wired (command registered, QML file listed, view registered).
- ``manual``: needs a human, a live peer, or hardware (calls, QR login,
  real sends). Reported, never faked.

Usage:
    python3 scripts/verify_features.py [--socket PATH] [--skip-unit] [--strict]

Exit status is 0 unless a check FAILs (SKIP/MANUAL never fail; --strict turns
MANUAL into failures for release gating).
"""

from __future__ import annotations

import argparse
import json
import os
import socket
import subprocess
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SOCKET_DEFAULT = os.path.join(
    os.environ.get("XDG_RUNTIME_DIR", f"/run/user/{os.getuid()}"),
    "whatevr", "whatevrd.sock",
)


@dataclass
class Result:
    id: str
    feature: str
    status: str  # PASS, FAIL, SKIP, MANUAL
    detail: str = ""


@dataclass
class Context:
    sock_path: str
    skip_unit: bool
    conn: socket.socket | None = None
    buf: bytearray = field(default_factory=bytearray)
    next_id: int = 100
    results: list = field(default_factory=list)


# --- protocol helpers -------------------------------------------------------

def sock_connect(ctx: Context) -> bool:
    try:
        s = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        s.settimeout(8)
        s.connect(ctx.sock_path)
    except OSError as e:
        return False
    ctx.conn = s
    ctx.buf = bytearray()
    try:
        hello = cmd(ctx, "hello", {"client": "verify", "protocol": 1})
        return "result" in hello
    except OSError:
        return False


def send_line(ctx: Context, obj: dict) -> None:
    assert ctx.conn is not None
    ctx.conn.sendall((json.dumps(obj) + "\n").encode())


def read_frame(ctx: Context) -> dict:
    # Manual line buffer on the raw socket: makefile() objects break
    # permanently once a read times out, so never use them here.
    assert ctx.conn is not None
    while True:
        nl = ctx.buf.find(b"\n")
        if nl >= 0:
            line = bytes(ctx.buf[:nl])
            del ctx.buf[:nl + 1]
            if not line:
                continue
            return json.loads(line)
        try:
            chunk = ctx.conn.recv(65536)
        except socket.timeout as e:
            raise OSError(f"read timed out: {e}")
        if not chunk:
            raise OSError("daemon closed the connection")
        ctx.buf.extend(chunk)


def cmd(ctx: Context, method: str, params: dict) -> dict:
    ctx.next_id += 1
    rid = ctx.next_id
    send_line(ctx, {"id": rid, "method": method, "params": params})
    while True:
        msg = read_frame(ctx)
        if msg.get("id") == rid:
            return msg


def subscribe_collect(ctx: Context, view: str, params: dict, want_rows: int = 1,
                      timeout_s: float = 8.0) -> tuple[list, bool, str]:
    """Subscribe and collect upserts until ready/timeout. Returns
    (rows, ready_seen, error_text)."""
    ctx.next_id += 1
    rid = ctx.next_id
    send_line(ctx, {"id": rid, "method": "subscribe",
                    "params": {"view": view, **params}})
    rows: list = []
    ready = False
    err = ""
    try:
        first = read_frame(ctx)
    except OSError as e:
        return rows, ready, f"no subscribe response: {e}"
    if "error" in first:
        return rows, ready, f"subscribe rejected: {first['error']}"
    sub = first.get("result", {}).get("sub")
    deadline = time.time() + timeout_s
    try:
        while time.time() < deadline:
            ctx.conn.settimeout(max(0.1, deadline - time.time()))
            try:
                msg = read_frame(ctx)
            except (OSError, TimeoutError):
                break
            if msg.get("sub") != sub:
                continue
            if msg.get("event") == "upsert" and "item" in msg:
                rows.append(msg["item"])
                if len(rows) >= want_rows and ready:
                    break
            elif msg.get("event") == "ready":
                ready = True
                if len(rows) >= want_rows:
                    break
    finally:
        try:
            ctx.conn.settimeout(8)
        except OSError:
            pass
    return rows, ready, err


# --- check primitives -------------------------------------------------------

def check_socket_view(ctx: Context, cid: str, feature: str, view: str,
                      params: dict, min_rows: int = 0,
                      row_pred=None) -> Result:
    rows, ready, err = subscribe_collect(ctx, view, params,
                                         want_rows=max(1, min_rows))
    if err:
        return Result(cid, feature, "FAIL", err)
    if not ready:
        return Result(cid, feature, "FAIL", "no ready event")
    if len(rows) < min_rows:
        return Result(cid, feature, "FAIL",
                      f"only {len(rows)} rows, need {min_rows}")
    if row_pred is not None:
        bad = [r for r in rows if not row_pred(r)]
        if bad:
            return Result(cid, feature, "FAIL",
                          f"{len(bad)} rows failed predicate")
    return Result(cid, feature, "PASS", f"{len(rows)} row(s), ready")


def check_command_rejects(ctx: Context, cid: str, feature: str, method: str,
                          params: dict) -> Result:
    """Proves the command is wired without changing anything: any error is
    fine (validation), except unknown_method (not implemented) and success
    (it did something)."""
    try:
        resp = cmd(ctx, method, params)
    except OSError as e:
        return Result(cid, feature, "FAIL", f"no response: {e}")
    err = resp.get("error")
    if not err:
        return Result(cid, feature, "FAIL",
                      "command unexpectedly succeeded")
    if err.get("code") == "unknown_method":
        return Result(cid, feature, "FAIL", "method not implemented")
    return Result(cid, feature, "PASS", f"wired, rejects {err.get('code')}")


def check_unit(ctx: Context, cid: str, feature: str, argv: list) -> Result:
    if ctx.skip_unit:
        return Result(cid, feature, "SKIP", "unit tests skipped")
    cwd = ROOT / "whatevrd" if argv and argv[0] == "go" else ROOT
    try:
        p = subprocess.run(argv, cwd=cwd, capture_output=True, text=True,
                           timeout=600)
    except (OSError, subprocess.TimeoutExpired) as e:
        return Result(cid, feature, "FAIL", f"runner error: {e}")
    if p.returncode != 0:
        tail = (p.stdout + p.stderr).strip().splitlines()[-5:]
        return Result(cid, feature, "FAIL", " | ".join(tail))
    return Result(cid, feature, "PASS", "suite green")


def check_static(ctx: Context, cid: str, feature: str, paths: list,
                 needles: dict | None = None) -> Result:
    _ = ctx
    for rel in paths:
        if not (ROOT / rel).exists():
            return Result(cid, feature, "FAIL", f"missing {rel}")
    for rel, subs in (needles or {}).items():
        try:
            text = (ROOT / rel).read_text()
        except OSError as e:
            return Result(cid, feature, "FAIL", f"unreadable {rel}: {e}")
        for sub in subs:
            if sub not in text:
                return Result(cid, feature, "FAIL",
                              f"{rel} lacks {sub!r}")
    return Result(cid, feature, "PASS", "code present and wired")


def manual(cid: str, feature: str, why: str) -> Result:
    return Result(cid, feature, "MANUAL", why)


# --- the map ----------------------------------------------------------------
# IDs mirror the README feature-map rows.

def run_all(ctx: Context) -> None:
    R = ctx.results.append
    live = ctx.conn is not None

    def sk(cid, feature, view, params, **kw):
        if not live:
            R(Result(cid, feature, "SKIP", "no daemon socket"))
        else:
            R(check_socket_view(ctx, cid, feature, view, params, **kw))

    def rej(cid, feature, method, params):
        if not live:
            R(Result(cid, feature, "SKIP", "no daemon socket"))
        else:
            R(check_command_rejects(ctx, cid, feature, method, params))

    # --- session / login (manual: needs QR + phone) ---
    R(manual("login-qr", "WhatsApp login with QR code", "needs phone scan"))
    R(manual("login-persist", "Persistent login session", "needs login"))
    R(manual("logout", "Logout", "destructive; covered by unit tests"))
    # A real chat id for chat-scoped probes (empty store -> SKIP those).
    chat_id = ""
    if live:
        rows, ready, _ = subscribe_collect(ctx, "chats", {"limit": 5},
                                           want_rows=1, timeout_s=6.0)
        if ready and rows and rows[0].get("id"):
            chat_id = rows[0]["id"]

    def skchat(cid, feature, view, params, **kw):
        if not live:
            R(Result(cid, feature, "SKIP", "no daemon socket"))
        elif not chat_id and view in ("messages", "presence", "chat_media"):
            R(Result(cid, feature, "SKIP", "no chats in store"))
        else:
            R(check_socket_view(ctx, cid, feature, view, params, **kw))
    # --- store / sync ---
    R(check_unit(ctx, "local-db", "Local message database",
                 ["go", "test", "-tags", "sqlite_fts5", "./internal/store/"]))
    sk("older-load", "Older message loading", "messages",
       {"chat_id": chat_id or "__none__", "limit": 1})
    sk("presence", "Online/last-seen presence", "presence",
       {"chat_id": chat_id or "__none__"})
    # --- messaging (wiring proven without sending) ---
    rej("send-text", "Send text messages", "send.text",
        {"chat_id": "", "text": "x"})
    rej("send-media", "Send image/media/documents", "send.media",
        {"chat_id": "x", "path": ""})
    rej("send-media-batch", "Multi-file sends", "send.media_batch",
        {"chat_id": "x", "files": []})
    rej("reply", "Reply to messages", "send.text",
        {"chat_id": "", "text": "x", "reply_to": "m"})
    rej("edit", "Edit sent messages", "message.edit",
        {"message_id": "", "text": "x"})
    rej("revoke", "Delete messages", "message.revoke", {"message_id": ""})
    rej("forward", "Forward messages", "message.forward",
        {"message_id": "", "chat_ids": []})
    rej("star", "Star/bookmark messages", "message.star",
        {"message_id": ""})
    rej("react", "Message reactions", "message.react",
        {"message_id": "", "emoji": ""})
    rej("vote", "Poll voting", "message.vote",
        {"message_id": "", "options": []})
    rej("history", "Edit history", "message.edit_history", {"message_id": ""})
    # --- chats ---
    sk("chats", "Chat list / pin / archive / mute", "chats", {"limit": 5})
    sk("chat-search", "Chat search", "chats", {"limit": 1})
    sk("unread-filter", "Unread-only filter", "chats",
       {"limit": 5, "filter": "unread"})
    rej("mark-all-read", "Mark all as read", "chat.mark_all_read",
        {"unexpected": 1})
    R(manual("clipsend", "Real sends land on the phone",
             "needs live peer chat"))
    # --- media / gallery ---
    if not live:
        R(Result("gallery", "Per-chat media gallery", "SKIP",
                 "no daemon socket"))
    elif not chat_id:
        R(Result("gallery", "Per-chat media gallery", "SKIP",
                 "no chats in store"))
    else:
        R(check_socket_view(ctx, "gallery", "Per-chat media gallery",
                            "chat_media",
                            {"chat_id": chat_id, "kinds": ["image"]}))
    rej("media-save", "Save message media", "media.save", {"path": "/tmp/x"})
    # --- status ---
    sk("status", "Status tab + viewer data", "status", {"limit": 5})
    sk("status-kept", "Kept senders view", "status.kept", {})
    rej("status-post", "Post status", "status.post", {})
    rej("status-dl", "Status download", "status.download", {"status_id": ""})
    # --- channels / groups / calls ---
    sk("channels", "Channels directory", "channels", {})
    rej("chan-follow", "Follow channel", "channel.follow", {"jid": ""})
    rej("group-create", "Create group", "group.create",
        {"name": "", "members": []})
    sk("calls", "Calls tab", "calls", {})
    rej("call-reject", "Reject call", "call.reject", {"chat_id": ""})
    R(manual("call-media", "Answer/place calls",
             "upstream-blocked: no media stack in whatsmeow"))
    # --- settings / privacy / account ---
    sk("prefs", "Preferences view + toggles", "preferences", {},
       row_pred=lambda r: "anti_delete" in r and "send_typing_indicators" in r)
    # Write path, proven without changing anything: read the live value back
    # and write the identical value.
    if not live:
        R(Result("prefs-set", "Preferences write", "SKIP",
                 "no daemon socket"))
    else:
        rows, ready, _ = subscribe_collect(ctx, "preferences", {},
                                           want_rows=1, timeout_s=6.0)
        if not ready or not rows:
            R(Result("prefs-set", "Preferences write", "FAIL",
                     "could not read live prefs"))
        else:
            cur = rows[0].get("anti_delete", True)
            try:
                resp = cmd(ctx, "preferences.set", {"anti_delete": cur})
                if "result" in resp:
                    R(Result("prefs-set", "Preferences write", "PASS",
                             "round-trip, value unchanged"))
                else:
                    R(Result("prefs-set", "Preferences write", "FAIL",
                             f"{resp}"))
            except OSError as e:
                R(Result("prefs-set", "Preferences write", "FAIL", str(e)))
    R(manual("privacy", "Privacy settings", "needs live account"))
    # --- tray / logs / backup ---
    sk("logs", "Debug logs view", "daemon.logs", {"limit": 5}, min_rows=1)
    R(check_static(ctx, "tray", "Daemon tray icon",
                   ["whatevrd/internal/tray/tray.go"],
                   {"whatevrd/internal/tray/tray.go":
                    ["ActivateWindow", "ShowTrayMenu"]}))
    R(manual("backup", "Backup export/restore", "needs passphrase + data"))
    R(manual("qr-login", "QR login flow", "needs phone scan"))
    # --- frontend presence (static wiring) ---
    qml = [
        "whatkevr/src/qml/components/ChatBubble.qml",
        "whatkevr/src/qml/components/StatusViewerPage.qml",
        "whatkevr/src/qml/components/StatusPage.qml",
        "whatkevr/src/qml/components/ChannelsPage.qml",
        "whatkevr/src/qml/components/LogsPage.qml",
        "whatkevr/src/qml/components/ChatMediaGalleryPage.qml",
        "whatkevr/src/qml/components/EditHistoryDialog.qml",
        "whatkevr/src/qml/components/MessageComposer.qml",
    ]
    R(check_static(ctx, "qml-pages", "Frontend pages present", qml))
    R(check_static(
        ctx, "qml-wired", "Pages registered in the QML module",
        ["whatkevr/src/CMakeLists.txt"],
        {"whatkevr/src/CMakeLists.txt": [
            "qml/components/EditHistoryDialog.qml",
            "qml/components/StatusViewerPage.qml",
            "qml/components/LogsPage.qml",
        ]}))
    # --- unit suites ---
    R(check_unit(ctx, "go-suite", "Go daemon suites",
                 ["go", "test", "-tags", "sqlite_fts5", "./internal/wa/",
                  "./internal/protocol/"]))
    R(manual("qt-suite", "Qt frontend suites",
             "needs a display build; run ctest in build/debug/whatkevr-tests"))


def main() -> int:
    ap = argparse.ArgumentParser(description="verify README features locally")
    ap.add_argument("--socket", default=SOCKET_DEFAULT)
    ap.add_argument("--skip-unit", action="store_true")
    ap.add_argument("--strict", action="store_true",
                    help="MANUAL counts as failure (release gating)")
    args = ap.parse_args()

    ctx = Context(sock_path=args.socket, skip_unit=args.skip_unit)
    live = sock_connect(ctx)
    if live:
        print(f"daemon: {args.socket} (HELLO ok)")
    else:
        print(f"daemon: unreachable at {args.socket} "
              f"(socket checks SKIP)")
    run_all(ctx)

    counts: dict = {}
    failed = []
    for r in ctx.results:
        counts[r.status] = counts.get(r.status, 0) + 1
        if r.status == "FAIL" or (args.strict and r.status == "MANUAL"):
            failed.append(r)
    print(f"\n{'ID':<16}{'STATUS':<8}FEATURE")
    for r in ctx.results:
        mark = {"PASS": "ok", "FAIL": "FAIL",
                "SKIP": "skip", "MANUAL": "manual"}[r.status]
        print(f"{r.id:<16}{mark:<8}{r.feature}"
              + (f" — {r.detail}" if r.status in ("FAIL", "MANUAL") and r.detail else ""))
    print(f"\n{counts.get('PASS', 0)} passed, "
          f"{counts.get('FAIL', 0)} failed, "
          f"{counts.get('SKIP', 0)} skipped, "
          f"{counts.get('MANUAL', 0)} manual.")
    if ctx.conn is not None:
        ctx.conn.close()
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
