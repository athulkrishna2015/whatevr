from whatevr.v2 import chats_pb2 as _chats_pb2
from whatevr.v2 import messages_pb2 as _messages_pb2
from whatevr.v2 import stickers_pb2 as _stickers_pb2
from google.protobuf.internal import containers as _containers
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class SearchChats(_message.Message):
    __slots__ = ("query", "limit")
    QUERY_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    query: str
    limit: int
    def __init__(self, query: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class SearchChatsResult(_message.Message):
    __slots__ = ("chats",)
    CHATS_FIELD_NUMBER: _ClassVar[int]
    chats: _containers.RepeatedCompositeFieldContainer[_chats_pb2.ChatRow]
    def __init__(self, chats: _Optional[_Iterable[_Union[_chats_pb2.ChatRow, _Mapping]]] = ...) -> None: ...

class SearchMessages(_message.Message):
    __slots__ = ("query", "chat_id", "limit", "before")
    QUERY_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    BEFORE_FIELD_NUMBER: _ClassVar[int]
    query: str
    chat_id: str
    limit: int
    before: str
    def __init__(self, query: _Optional[str] = ..., chat_id: _Optional[str] = ..., limit: _Optional[int] = ..., before: _Optional[str] = ...) -> None: ...

class SearchMessagesResult(_message.Message):
    __slots__ = ("messages", "more")
    MESSAGES_FIELD_NUMBER: _ClassVar[int]
    MORE_FIELD_NUMBER: _ClassVar[int]
    messages: _containers.RepeatedCompositeFieldContainer[_messages_pb2.MessageRow]
    more: bool
    def __init__(self, messages: _Optional[_Iterable[_Union[_messages_pb2.MessageRow, _Mapping]]] = ..., more: _Optional[bool] = ...) -> None: ...

class SearchStickers(_message.Message):
    __slots__ = ("query", "limit")
    QUERY_FIELD_NUMBER: _ClassVar[int]
    LIMIT_FIELD_NUMBER: _ClassVar[int]
    query: str
    limit: int
    def __init__(self, query: _Optional[str] = ..., limit: _Optional[int] = ...) -> None: ...

class SearchStickersResult(_message.Message):
    __slots__ = ("stickers",)
    STICKERS_FIELD_NUMBER: _ClassVar[int]
    stickers: _containers.RepeatedCompositeFieldContainer[_stickers_pb2.StickerRow]
    def __init__(self, stickers: _Optional[_Iterable[_Union[_stickers_pb2.StickerRow, _Mapping]]] = ...) -> None: ...

class ContactCheckPhone(_message.Message):
    __slots__ = ("phone",)
    PHONE_FIELD_NUMBER: _ClassVar[int]
    phone: str
    def __init__(self, phone: _Optional[str] = ...) -> None: ...

class ContactCheckPhoneResult(_message.Message):
    __slots__ = ("registered", "phone", "person_id", "name", "business")
    REGISTERED_FIELD_NUMBER: _ClassVar[int]
    PHONE_FIELD_NUMBER: _ClassVar[int]
    PERSON_ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    BUSINESS_FIELD_NUMBER: _ClassVar[int]
    registered: bool
    phone: str
    person_id: str
    name: str
    business: bool
    def __init__(self, registered: _Optional[bool] = ..., phone: _Optional[str] = ..., person_id: _Optional[str] = ..., name: _Optional[str] = ..., business: _Optional[bool] = ...) -> None: ...
