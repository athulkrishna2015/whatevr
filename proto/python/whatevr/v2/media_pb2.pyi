from whatevr.v2 import people_pb2 as _people_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class TransferDirection(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    TRANSFER_DIRECTION_UNSPECIFIED: _ClassVar[TransferDirection]
    TRANSFER_DIRECTION_DOWNLOAD: _ClassVar[TransferDirection]
    TRANSFER_DIRECTION_UPLOAD: _ClassVar[TransferDirection]

class MediaStreamState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    MEDIA_STREAM_STATE_UNSPECIFIED: _ClassVar[MediaStreamState]
    MEDIA_STREAM_STATE_LOCAL: _ClassVar[MediaStreamState]
    MEDIA_STREAM_STATE_FAILED: _ClassVar[MediaStreamState]
TRANSFER_DIRECTION_UNSPECIFIED: TransferDirection
TRANSFER_DIRECTION_DOWNLOAD: TransferDirection
TRANSFER_DIRECTION_UPLOAD: TransferDirection
MEDIA_STREAM_STATE_UNSPECIFIED: MediaStreamState
MEDIA_STREAM_STATE_LOCAL: MediaStreamState
MEDIA_STREAM_STATE_FAILED: MediaStreamState

class TransfersView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class TransferRow(_message.Message):
    __slots__ = ("message_id", "chat_id", "direction", "done_bytes", "total_bytes", "error")
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    DIRECTION_FIELD_NUMBER: _ClassVar[int]
    DONE_BYTES_FIELD_NUMBER: _ClassVar[int]
    TOTAL_BYTES_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    message_id: str
    chat_id: str
    direction: TransferDirection
    done_bytes: int
    total_bytes: int
    error: str
    def __init__(self, message_id: _Optional[str] = ..., chat_id: _Optional[str] = ..., direction: _Optional[_Union[TransferDirection, str]] = ..., done_bytes: _Optional[int] = ..., total_bytes: _Optional[int] = ..., error: _Optional[str] = ...) -> None: ...

class MediaDownload(_message.Message):
    __slots__ = ("message_id",)
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    message_id: str
    def __init__(self, message_id: _Optional[str] = ...) -> None: ...

class MediaStream(_message.Message):
    __slots__ = ("message_id",)
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    message_id: str
    def __init__(self, message_id: _Optional[str] = ...) -> None: ...

class MediaStreamResult(_message.Message):
    __slots__ = ("stream_id", "url", "mime", "size_bytes", "duration_ms")
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    URL_FIELD_NUMBER: _ClassVar[int]
    MIME_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    DURATION_MS_FIELD_NUMBER: _ClassVar[int]
    stream_id: str
    url: str
    mime: str
    size_bytes: int
    duration_ms: int
    def __init__(self, stream_id: _Optional[str] = ..., url: _Optional[str] = ..., mime: _Optional[str] = ..., size_bytes: _Optional[int] = ..., duration_ms: _Optional[int] = ...) -> None: ...

class MediaStreamUpdate(_message.Message):
    __slots__ = ("stream_id", "message_id", "state", "path", "error")
    STREAM_ID_FIELD_NUMBER: _ClassVar[int]
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    STATE_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    stream_id: str
    message_id: str
    state: MediaStreamState
    path: str
    error: str
    def __init__(self, stream_id: _Optional[str] = ..., message_id: _Optional[str] = ..., state: _Optional[_Union[MediaStreamState, str]] = ..., path: _Optional[str] = ..., error: _Optional[str] = ...) -> None: ...

class MediaCancelDownload(_message.Message):
    __slots__ = ("message_id",)
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    message_id: str
    def __init__(self, message_id: _Optional[str] = ...) -> None: ...

class MediaRead(_message.Message):
    __slots__ = ("path", "offset", "max_bytes")
    PATH_FIELD_NUMBER: _ClassVar[int]
    OFFSET_FIELD_NUMBER: _ClassVar[int]
    MAX_BYTES_FIELD_NUMBER: _ClassVar[int]
    path: str
    offset: int
    max_bytes: int
    def __init__(self, path: _Optional[str] = ..., offset: _Optional[int] = ..., max_bytes: _Optional[int] = ...) -> None: ...

class MediaReadResult(_message.Message):
    __slots__ = ("data", "size_bytes", "eof")
    DATA_FIELD_NUMBER: _ClassVar[int]
    SIZE_BYTES_FIELD_NUMBER: _ClassVar[int]
    EOF_FIELD_NUMBER: _ClassVar[int]
    data: bytes
    size_bytes: int
    eof: bool
    def __init__(self, data: _Optional[bytes] = ..., size_bytes: _Optional[int] = ..., eof: _Optional[bool] = ...) -> None: ...

class MediaFetchProfilePicture(_message.Message):
    __slots__ = ("person",)
    PERSON_FIELD_NUMBER: _ClassVar[int]
    person: _people_pb2.Address
    def __init__(self, person: _Optional[_Union[_people_pb2.Address, _Mapping]] = ...) -> None: ...

class MediaFetchProfilePictureResult(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...

class MediaSave(_message.Message):
    __slots__ = ("message_id", "status_id", "jid", "path")
    MESSAGE_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_ID_FIELD_NUMBER: _ClassVar[int]
    JID_FIELD_NUMBER: _ClassVar[int]
    PATH_FIELD_NUMBER: _ClassVar[int]
    message_id: str
    status_id: str
    jid: str
    path: str
    def __init__(self, message_id: _Optional[str] = ..., status_id: _Optional[str] = ..., jid: _Optional[str] = ..., path: _Optional[str] = ...) -> None: ...

class MediaSaveResult(_message.Message):
    __slots__ = ("path",)
    PATH_FIELD_NUMBER: _ClassVar[int]
    path: str
    def __init__(self, path: _Optional[str] = ...) -> None: ...
