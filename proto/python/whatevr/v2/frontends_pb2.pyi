from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class FrontendSource(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    FRONTEND_SOURCE_UNSPECIFIED: _ClassVar[FrontendSource]
    FRONTEND_SOURCE_SYSTEM: _ClassVar[FrontendSource]
    FRONTEND_SOURCE_NATIVE: _ClassVar[FrontendSource]
    FRONTEND_SOURCE_USER: _ClassVar[FrontendSource]
FRONTEND_SOURCE_UNSPECIFIED: FrontendSource
FRONTEND_SOURCE_SYSTEM: FrontendSource
FRONTEND_SOURCE_NATIVE: FrontendSource
FRONTEND_SOURCE_USER: FrontendSource

class Frontend(_message.Message):
    __slots__ = ("id", "name", "terminal", "source", "connected", "is_default")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    TERMINAL_FIELD_NUMBER: _ClassVar[int]
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    CONNECTED_FIELD_NUMBER: _ClassVar[int]
    IS_DEFAULT_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    terminal: bool
    source: FrontendSource
    connected: bool
    is_default: bool
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., terminal: _Optional[bool] = ..., source: _Optional[_Union[FrontendSource, str]] = ..., connected: _Optional[bool] = ..., is_default: _Optional[bool] = ...) -> None: ...

class FrontendList(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class FrontendListResult(_message.Message):
    __slots__ = ("frontends",)
    FRONTENDS_FIELD_NUMBER: _ClassVar[int]
    frontends: _containers.RepeatedCompositeFieldContainer[Frontend]
    def __init__(self, frontends: _Optional[_Iterable[_Union[Frontend, _Mapping]]] = ...) -> None: ...

class FrontendSetDefault(_message.Message):
    __slots__ = ("id",)
    ID_FIELD_NUMBER: _ClassVar[int]
    id: str
    def __init__(self, id: _Optional[str] = ...) -> None: ...

class LinkOpen(_message.Message):
    __slots__ = ("url",)
    URL_FIELD_NUMBER: _ClassVar[int]
    url: str
    def __init__(self, url: _Optional[str] = ...) -> None: ...

class Activate(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
