from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class StickerSource(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    STICKER_SOURCE_UNSPECIFIED: _ClassVar[StickerSource]
    STICKER_SOURCE_RECENT: _ClassVar[StickerSource]
    STICKER_SOURCE_FAVORITE: _ClassVar[StickerSource]
    STICKER_SOURCE_ALL: _ClassVar[StickerSource]
STICKER_SOURCE_UNSPECIFIED: StickerSource
STICKER_SOURCE_RECENT: StickerSource
STICKER_SOURCE_FAVORITE: StickerSource
STICKER_SOURCE_ALL: StickerSource

class StickersView(_message.Message):
    __slots__ = ("source",)
    SOURCE_FIELD_NUMBER: _ClassVar[int]
    source: StickerSource
    def __init__(self, source: _Optional[_Union[StickerSource, str]] = ...) -> None: ...

class StickerRow(_message.Message):
    __slots__ = ("id", "path", "mime", "animated", "lottie", "width", "height", "emojis", "accessibility_text", "pack_id", "favorite", "last_used_ms")
    ID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    MIME_FIELD_NUMBER: _ClassVar[int]
    ANIMATED_FIELD_NUMBER: _ClassVar[int]
    LOTTIE_FIELD_NUMBER: _ClassVar[int]
    WIDTH_FIELD_NUMBER: _ClassVar[int]
    HEIGHT_FIELD_NUMBER: _ClassVar[int]
    EMOJIS_FIELD_NUMBER: _ClassVar[int]
    ACCESSIBILITY_TEXT_FIELD_NUMBER: _ClassVar[int]
    PACK_ID_FIELD_NUMBER: _ClassVar[int]
    FAVORITE_FIELD_NUMBER: _ClassVar[int]
    LAST_USED_MS_FIELD_NUMBER: _ClassVar[int]
    id: str
    path: str
    mime: str
    animated: bool
    lottie: bool
    width: int
    height: int
    emojis: _containers.RepeatedScalarFieldContainer[str]
    accessibility_text: str
    pack_id: str
    favorite: bool
    last_used_ms: int
    def __init__(self, id: _Optional[str] = ..., path: _Optional[str] = ..., mime: _Optional[str] = ..., animated: _Optional[bool] = ..., lottie: _Optional[bool] = ..., width: _Optional[int] = ..., height: _Optional[int] = ..., emojis: _Optional[_Iterable[str]] = ..., accessibility_text: _Optional[str] = ..., pack_id: _Optional[str] = ..., favorite: _Optional[bool] = ..., last_used_ms: _Optional[int] = ...) -> None: ...

class StickerPacksView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class StickerPackView(_message.Message):
    __slots__ = ("pack_id",)
    PACK_ID_FIELD_NUMBER: _ClassVar[int]
    pack_id: str
    def __init__(self, pack_id: _Optional[str] = ...) -> None: ...

class StickerPackRow(_message.Message):
    __slots__ = ("id", "name", "publisher", "description", "animated", "lottie", "tray_path", "count", "installed", "fetched")
    ID_FIELD_NUMBER: _ClassVar[int]
    NAME_FIELD_NUMBER: _ClassVar[int]
    PUBLISHER_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    ANIMATED_FIELD_NUMBER: _ClassVar[int]
    LOTTIE_FIELD_NUMBER: _ClassVar[int]
    TRAY_PATH_FIELD_NUMBER: _ClassVar[int]
    COUNT_FIELD_NUMBER: _ClassVar[int]
    INSTALLED_FIELD_NUMBER: _ClassVar[int]
    FETCHED_FIELD_NUMBER: _ClassVar[int]
    id: str
    name: str
    publisher: str
    description: str
    animated: bool
    lottie: bool
    tray_path: str
    count: int
    installed: bool
    fetched: bool
    def __init__(self, id: _Optional[str] = ..., name: _Optional[str] = ..., publisher: _Optional[str] = ..., description: _Optional[str] = ..., animated: _Optional[bool] = ..., lottie: _Optional[bool] = ..., tray_path: _Optional[str] = ..., count: _Optional[int] = ..., installed: _Optional[bool] = ..., fetched: _Optional[bool] = ...) -> None: ...

class StickerFavorite(_message.Message):
    __slots__ = ("sticker_id", "message_id", "favorite")
    STICKER_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    FAVORITE_FIELD_NUMBER: _ClassVar[int]
    sticker_id: str
    message_id: str
    favorite: bool
    def __init__(self, sticker_id: _Optional[str] = ..., message_id: _Optional[str] = ..., favorite: _Optional[bool] = ...) -> None: ...

class StickerDownload(_message.Message):
    __slots__ = ("sticker_id",)
    STICKER_ID_FIELD_NUMBER: _ClassVar[int]
    sticker_id: str
    def __init__(self, sticker_id: _Optional[str] = ...) -> None: ...

class StickerPackInstall(_message.Message):
    __slots__ = ("pack_id", "installed")
    PACK_ID_FIELD_NUMBER: _ClassVar[int]
    INSTALLED_FIELD_NUMBER: _ClassVar[int]
    pack_id: str
    installed: bool
    def __init__(self, pack_id: _Optional[str] = ..., installed: _Optional[bool] = ...) -> None: ...

class StickerPacksRefresh(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
