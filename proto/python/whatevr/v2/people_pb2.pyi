from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class Availability(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    AVAILABILITY_UNSPECIFIED: _ClassVar[Availability]
    AVAILABILITY_ONLINE: _ClassVar[Availability]
    AVAILABILITY_OFFLINE: _ClassVar[Availability]
AVAILABILITY_UNSPECIFIED: Availability
AVAILABILITY_ONLINE: Availability
AVAILABILITY_OFFLINE: Availability

class Address(_message.Message):
    __slots__ = ("id", "phone", "lid", "username")
    ID_FIELD_NUMBER: _ClassVar[int]
    PHONE_FIELD_NUMBER: _ClassVar[int]
    LID_FIELD_NUMBER: _ClassVar[int]
    USERNAME_FIELD_NUMBER: _ClassVar[int]
    id: str
    phone: str
    lid: str
    username: str
    def __init__(self, id: _Optional[str] = ..., phone: _Optional[str] = ..., lid: _Optional[str] = ..., username: _Optional[str] = ...) -> None: ...

class Person(_message.Message):
    __slots__ = ("id", "name", "phone", "avatar_path", "self")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PHONE_FIELD_NUMBER: _ClassVar[int]
    AVATAR_PATH_FIELD_NUMBER: _ClassVar[int]
    SELF_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    phone: str
    avatar_path: str
    self: bool
    def __init__(self_, id: _Optional[str] = ..., name: _Optional[str] = ..., phone: _Optional[str] = ..., avatar_path: _Optional[str] = ..., self: _Optional[bool] = ...) -> None: ...

class SelfView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SelfRow(_message.Message):
    __slots__ = ("id", "phone", "push_name", "about", "avatar_path")
    ID_FIELD_NUMBER: _ClassVar[int]
    PHONE_FIELD_NUMBER: _ClassVar[int]
    PUSH_NAME_FIELD_NUMBER: _ClassVar[int]
    ABOUT_FIELD_NUMBER: _ClassVar[int]
    AVATAR_PATH_FIELD_NUMBER: _ClassVar[int]
    id: str
    phone: str
    push_name: str
    about: str
    avatar_path: str
    def __init__(self, id: _Optional[str] = ..., phone: _Optional[str] = ..., push_name: _Optional[str] = ..., about: _Optional[str] = ..., avatar_path: _Optional[str] = ...) -> None: ...

class ContactView(_message.Message):
    __slots__ = ("person",)
    PERSON_FIELD_NUMBER: _ClassVar[int]
    person: Address
    def __init__(self, person: _Optional[_Union[Address, _Mapping]] = ...) -> None: ...

class ContactRow(_message.Message):
    __slots__ = ("id", "phone", "saved_name", "push_name", "business_name", "business", "about", "avatar_path", "blocked")
    ID_FIELD_NUMBER: _ClassVar[int]
    PHONE_FIELD_NUMBER: _ClassVar[int]
    SAVED_NAME_FIELD_NUMBER: _ClassVar[int]
    PUSH_NAME_FIELD_NUMBER: _ClassVar[int]
    BUSINESS_NAME_FIELD_NUMBER: _ClassVar[int]
    BUSINESS_FIELD_NUMBER: _ClassVar[int]
    ABOUT_FIELD_NUMBER: _ClassVar[int]
    AVATAR_PATH_FIELD_NUMBER: _ClassVar[int]
    BLOCKED_FIELD_NUMBER: _ClassVar[int]
    id: str
    phone: str
    saved_name: str
    push_name: str
    business_name: str
    business: bool
    about: str
    avatar_path: str
    blocked: bool
    def __init__(self, id: _Optional[str] = ..., phone: _Optional[str] = ..., saved_name: _Optional[str] = ..., push_name: _Optional[str] = ..., business_name: _Optional[str] = ..., business: _Optional[bool] = ..., about: _Optional[str] = ..., avatar_path: _Optional[str] = ..., blocked: _Optional[bool] = ...) -> None: ...

class BlocklistView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class BlockedRow(_message.Message):
    __slots__ = ("person",)
    PERSON_FIELD_NUMBER: _ClassVar[int]
    person: Person
    def __init__(self, person: _Optional[_Union[Person, _Mapping]] = ...) -> None: ...

class PresenceView(_message.Message):
    __slots__ = ("chat_id",)
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    chat_id: str
    def __init__(self, chat_id: _Optional[str] = ...) -> None: ...

class PresenceRow(_message.Message):
    __slots__ = ("person", "availability", "last_seen_ms")
    PERSON_FIELD_NUMBER: _ClassVar[int]
    AVAILABILITY_FIELD_NUMBER: _ClassVar[int]
    LAST_SEEN_MS_FIELD_NUMBER: _ClassVar[int]
    person: Person
    availability: Availability
    last_seen_ms: int
    def __init__(self, person: _Optional[_Union[Person, _Mapping]] = ..., availability: _Optional[_Union[Availability, str]] = ..., last_seen_ms: _Optional[int] = ...) -> None: ...

class TypingView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TypingRow(_message.Message):
    __slots__ = ("chat_id", "typists")
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    TYPISTS_FIELD_NUMBER: _ClassVar[int]
    chat_id: str
    typists: _containers.RepeatedCompositeFieldContainer[Typist]
    def __init__(self, chat_id: _Optional[str] = ..., typists: _Optional[_Iterable[_Union[Typist, _Mapping]]] = ...) -> None: ...

class Typist(_message.Message):
    __slots__ = ("person", "recording")
    PERSON_FIELD_NUMBER: _ClassVar[int]
    RECORDING_FIELD_NUMBER: _ClassVar[int]
    person: Person
    recording: bool
    def __init__(self, person: _Optional[_Union[Person, _Mapping]] = ..., recording: _Optional[bool] = ...) -> None: ...

class ContactBlock(_message.Message):
    __slots__ = ("person", "blocked")
    PERSON_FIELD_NUMBER: _ClassVar[int]
    BLOCKED_FIELD_NUMBER: _ClassVar[int]
    person: Address
    blocked: bool
    def __init__(self, person: _Optional[_Union[Address, _Mapping]] = ..., blocked: _Optional[bool] = ...) -> None: ...

class SelfSetAbout(_message.Message):
    __slots__ = ("text",)
    TEXT_FIELD_NUMBER: _ClassVar[int]
    text: str
    def __init__(self, text: _Optional[str] = ...) -> None: ...
