#!/usr/bin/env python3
"""A whole whatevr frontend: the chat list, kept live.

    examples/frontend.py                    print the chat list as it changes
    examples/frontend.py send CHAT_ID TEXT  send a message

Needs the python protobuf runtime and the types checked in under
proto/python.
"""

import itertools
import ctypes
import os
import socket
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "proto" / "python"))
from whatevr.v2 import chats_pb2, frame_pb2, messages_pb2  # noqa: E402

def runtime_dir():
    if os.environ.get("XDG_RUNTIME_DIR"):
        return os.environ["XDG_RUNTIME_DIR"]
    if sys.platform == "darwin":
        # Darwin libc's per-user directory is identical under launchd and shells.
        libc = ctypes.CDLL("/usr/lib/libSystem.B.dylib")
        libc.confstr.argtypes = [ctypes.c_int, ctypes.c_void_p, ctypes.c_size_t]
        libc.confstr.restype = ctypes.c_size_t
        size = libc.confstr(65537, None, 0)  # _CS_DARWIN_USER_TEMP_DIR
        if not size:
            raise RuntimeError("Cannot resolve macOS per-user temporary directory")
        buffer = ctypes.create_string_buffer(size)
        if not libc.confstr(65537, buffer, size):
            raise RuntimeError("Cannot resolve macOS per-user temporary directory")
        return os.fsdecode(buffer.value)
    raise RuntimeError("XDG_RUNTIME_DIR is not set")


def default_socket():
    if sys.platform == "darwin" and not os.environ.get("XDG_RUNTIME_DIR"):
        return os.path.join(runtime_dir(), "in.codelif.whatevr.sock")
    return os.path.join(runtime_dir(), "whatevr", "whatevrd.sock")


path = os.environ.get("WHATEVR_SOCKET") or default_socket()
sock = socket.socket(socket.AF_UNIX)
sock.connect(path)
stream = sock.makefile("rb")


ids = itertools.count(1)


# every frame is a varint length, then the Frame
def send(**method) -> int:
    rid = next(ids)
    body = frame_pb2.Frame(request=frame_pb2.Request(id=rid, **method)).SerializeToString()
    n, head = len(body), b""
    while n > 0x7F:
        head, n = head + bytes([n & 0x7F | 0x80]), n >> 7
    sock.sendall(head + bytes([n]) + body)
    return rid


def read() -> frame_pb2.Frame:
    n = shift = 0
    while True:
        b = stream.read(1)[0]
        n |= (b & 0x7F) << shift
        shift += 7
        if b < 0x80:
            return frame_pb2.Frame.FromString(stream.read(n))


send(hello=frame_pb2.Hello(client="frontend.py", protocol=2))
if len(sys.argv) == 4 and sys.argv[1] == "send":
    rid = send(send_text=messages_pb2.SendText(chat_id=sys.argv[2], text=sys.argv[3]))
    while (f := read()).WhichOneof("frame") != "response" or f.response.id != rid:
        pass
    print(f.response)
    sys.exit(f.response.HasField("error"))

send(subscribe=frame_pb2.Subscribe(limit=20, chats=chats_pb2.ChatsView(filter=chats_pb2.CHAT_FILTER_ALL)))
rows = {}
while True:
    f = read()
    if f.WhichOneof("frame") != "event" or not f.event.HasField("update"):
        continue
    u = f.event.update
    if u.reset:
        rows.clear()
    for c in u.changes:
        if c.HasField("upsert"):
            rows[c.upsert.id] = c.upsert
        else:
            rows.pop(c.remove.id, None)
    # the daemon sorts; a frontend only compares the bytes it was given
    print("\033[2J\033[H", end="")
    for r in sorted(rows.values(), key=lambda r: r.sort):
        chat = r.chat
        unread = " (%d)" % chat.unread if chat.unread else ""
        print("%-28s%s  %s" % (chat.name[:28], unread, chat.preview.text[:60]))
    sys.stdout.flush()
