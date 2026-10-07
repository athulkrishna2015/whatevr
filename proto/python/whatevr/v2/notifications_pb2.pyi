from whatevr.v2 import people_pb2 as _people_pb2
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class NotificationsView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class NotificationRow(_message.Message):
    __slots__ = ("id", "chat_id", "message_id", "title", "body", "sender", "avatar_path", "count", "t_ms", "sound")
    ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    TITLE_FIELD_NUMBER: _ClassVar[int]
    BODY_FIELD_NUMBER: _ClassVar[int]
    SENDER_FIELD_NUMBER: _ClassVar[int]
    AVATAR_PATH_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    T_MS_FIELD_NUMBER: _ClassVar[int]
    SOUND_FIELD_NUMBER: _ClassVar[int]
    id: str
    chat_id: str
    message_id: str
    title: str
    body: str
    sender: _people_pb2.Person
    avatar_path: str
    count: int
    t_ms: int
    sound: bool
    def __init__(self, id: _Optional[str] = ..., chat_id: _Optional[str] = ..., message_id: _Optional[str] = ..., title: _Optional[str] = ..., body: _Optional[str] = ..., sender: _Optional[_Union[_people_pb2.Person, _Mapping]] = ..., avatar_path: _Optional[str] = ..., count: _Optional[int] = ..., t_ms: _Optional[int] = ..., sound: _Optional[bool] = ...) -> None: ...

class NotificationDismiss(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...
