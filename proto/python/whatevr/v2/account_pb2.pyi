from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class ConnectionState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONNECTION_STATE_UNSPECIFIED: _ClassVar[ConnectionState]
    CONNECTION_STATE_STARTING: _ClassVar[ConnectionState]
    CONNECTION_STATE_NEED_LOGIN: _ClassVar[ConnectionState]
    CONNECTION_STATE_CONNECTING: _ClassVar[ConnectionState]
    CONNECTION_STATE_ONLINE: _ClassVar[ConnectionState]
    CONNECTION_STATE_WAITING: _ClassVar[ConnectionState]
    CONNECTION_STATE_OFFLINE: _ClassVar[ConnectionState]

class ConnectionCause(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    CONNECTION_CAUSE_UNSPECIFIED: _ClassVar[ConnectionCause]
    CONNECTION_CAUSE_UNREACHABLE: _ClassVar[ConnectionCause]
    CONNECTION_CAUSE_NO_ANSWER: _ClassVar[ConnectionCause]
    CONNECTION_CAUSE_REFUSED: _ClassVar[ConnectionCause]
    CONNECTION_CAUSE_SILENT: _ClassVar[ConnectionCause]
    CONNECTION_CAUSE_CLOSED: _ClassVar[ConnectionCause]

class LoginState(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    LOGIN_STATE_UNSPECIFIED: _ClassVar[LoginState]
    LOGIN_STATE_LOGGED_IN: _ClassVar[LoginState]
    LOGIN_STATE_QR: _ClassVar[LoginState]
    LOGIN_STATE_PAIRING: _ClassVar[LoginState]
    LOGIN_STATE_FAILED: _ClassVar[LoginState]

class SyncType(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SYNC_TYPE_UNSPECIFIED: _ClassVar[SyncType]
    SYNC_TYPE_INITIAL: _ClassVar[SyncType]
    SYNC_TYPE_RECENT: _ClassVar[SyncType]
    SYNC_TYPE_FULL: _ClassVar[SyncType]
    SYNC_TYPE_ON_DEMAND: _ClassVar[SyncType]
    SYNC_TYPE_PUSH_NAMES: _ClassVar[SyncType]

class SyncPhase(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    SYNC_PHASE_UNSPECIFIED: _ClassVar[SyncPhase]
    SYNC_PHASE_IDLE: _ClassVar[SyncPhase]
    SYNC_PHASE_RUNNING: _ClassVar[SyncPhase]
    SYNC_PHASE_STALLED: _ClassVar[SyncPhase]
    SYNC_PHASE_DONE: _ClassVar[SyncPhase]

class ProblemKind(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PROBLEM_KIND_UNSPECIFIED: _ClassVar[ProblemKind]
    PROBLEM_KIND_OFFLINE: _ClassVar[ProblemKind]
    PROBLEM_KIND_LOGGED_OUT: _ClassVar[ProblemKind]
    PROBLEM_KIND_TEMP_BANNED: _ClassVar[ProblemKind]
    PROBLEM_KIND_CLIENT_OUTDATED: _ClassVar[ProblemKind]
    PROBLEM_KIND_SEND_FAILING: _ClassVar[ProblemKind]
    PROBLEM_KIND_HISTORY_STALLED: _ClassVar[ProblemKind]
    PROBLEM_KIND_APP_STATE: _ClassVar[ProblemKind]
    PROBLEM_KIND_UNDECRYPTED: _ClassVar[ProblemKind]
    PROBLEM_KIND_STORAGE: _ClassVar[ProblemKind]
    PROBLEM_KIND_STUCK_BEFORE_LOGIN: _ClassVar[ProblemKind]
    PROBLEM_KIND_SERVER_REFUSED: _ClassVar[ProblemKind]
    PROBLEM_KIND_STREAM_REPLACED: _ClassVar[ProblemKind]
    PROBLEM_KIND_KEEPALIVE_LOST: _ClassVar[ProblemKind]
    PROBLEM_KIND_MEDIA_FAILING: _ClassVar[ProblemKind]
CONNECTION_STATE_UNSPECIFIED: ConnectionState
CONNECTION_STATE_STARTING: ConnectionState
CONNECTION_STATE_NEED_LOGIN: ConnectionState
CONNECTION_STATE_CONNECTING: ConnectionState
CONNECTION_STATE_ONLINE: ConnectionState
CONNECTION_STATE_WAITING: ConnectionState
CONNECTION_STATE_OFFLINE: ConnectionState
CONNECTION_CAUSE_UNSPECIFIED: ConnectionCause
CONNECTION_CAUSE_UNREACHABLE: ConnectionCause
CONNECTION_CAUSE_NO_ANSWER: ConnectionCause
CONNECTION_CAUSE_REFUSED: ConnectionCause
CONNECTION_CAUSE_SILENT: ConnectionCause
CONNECTION_CAUSE_CLOSED: ConnectionCause
LOGIN_STATE_UNSPECIFIED: LoginState
LOGIN_STATE_LOGGED_IN: LoginState
LOGIN_STATE_QR: LoginState
LOGIN_STATE_PAIRING: LoginState
LOGIN_STATE_FAILED: LoginState
SYNC_TYPE_UNSPECIFIED: SyncType
SYNC_TYPE_INITIAL: SyncType
SYNC_TYPE_RECENT: SyncType
SYNC_TYPE_FULL: SyncType
SYNC_TYPE_ON_DEMAND: SyncType
SYNC_TYPE_PUSH_NAMES: SyncType
SYNC_PHASE_UNSPECIFIED: SyncPhase
SYNC_PHASE_IDLE: SyncPhase
SYNC_PHASE_RUNNING: SyncPhase
SYNC_PHASE_STALLED: SyncPhase
SYNC_PHASE_DONE: SyncPhase
PROBLEM_KIND_UNSPECIFIED: ProblemKind
PROBLEM_KIND_OFFLINE: ProblemKind
PROBLEM_KIND_LOGGED_OUT: ProblemKind
PROBLEM_KIND_TEMP_BANNED: ProblemKind
PROBLEM_KIND_CLIENT_OUTDATED: ProblemKind
PROBLEM_KIND_SEND_FAILING: ProblemKind
PROBLEM_KIND_HISTORY_STALLED: ProblemKind
PROBLEM_KIND_APP_STATE: ProblemKind
PROBLEM_KIND_UNDECRYPTED: ProblemKind
PROBLEM_KIND_STORAGE: ProblemKind
PROBLEM_KIND_STUCK_BEFORE_LOGIN: ProblemKind
PROBLEM_KIND_SERVER_REFUSED: ProblemKind
PROBLEM_KIND_STREAM_REPLACED: ProblemKind
PROBLEM_KIND_KEEPALIVE_LOST: ProblemKind
PROBLEM_KIND_MEDIA_FAILING: ProblemKind

class ConnectionView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ConnectionRow(_message.Message):
    __slots__ = ("state", "since_ms", "detail", "cause", "attempt", "next_retry_ms", "can_reconnect", "pending_outgoing")
    STATE_FIELD_NUMBER: _ClassVar[int]
    SINCE_MS_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    CAUSE_FIELD_NUMBER: _ClassVar[int]
    ATTEMPT_FIELD_NUMBER: _ClassVar[int]
    NEXT_RETRY_MS_FIELD_NUMBER: _ClassVar[int]
    CAN_RECONNECT_FIELD_NUMBER: _ClassVar[int]
    PENDING_OUTGOING_FIELD_NUMBER: _ClassVar[int]
    state: ConnectionState
    since_ms: int
    detail: str
    cause: ConnectionCause
    attempt: int
    next_retry_ms: int
    can_reconnect: bool
    pending_outgoing: int
    def __init__(self, state: _Optional[_Union[ConnectionState, str]] = ..., since_ms: _Optional[int] = ..., detail: _Optional[str] = ..., cause: _Optional[_Union[ConnectionCause, str]] = ..., attempt: _Optional[int] = ..., next_retry_ms: _Optional[int] = ..., can_reconnect: _Optional[bool] = ..., pending_outgoing: _Optional[int] = ...) -> None: ...

class LoginView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class LoginRow(_message.Message):
    __slots__ = ("state", "detail", "qr", "qr_expires_ms")
    STATE_FIELD_NUMBER: _ClassVar[int]
    DETAIL_FIELD_NUMBER: _ClassVar[int]
    QR_FIELD_NUMBER: _ClassVar[int]
    QR_EXPIRES_MS_FIELD_NUMBER: _ClassVar[int]
    state: LoginState
    detail: str
    qr: str
    qr_expires_ms: int
    def __init__(self, state: _Optional[_Union[LoginState, str]] = ..., detail: _Optional[str] = ..., qr: _Optional[str] = ..., qr_expires_ms: _Optional[int] = ...) -> None: ...

class SyncView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class SyncRow(_message.Message):
    __slots__ = ("type", "phase", "percent", "chunk", "chunk_chats", "chunk_messages", "done_chats", "done_messages")
    TYPE_FIELD_NUMBER: _ClassVar[int]
    PHASE_FIELD_NUMBER: _ClassVar[int]
    PERCENT_FIELD_NUMBER: _ClassVar[int]
    CHUNK_FIELD_NUMBER: _ClassVar[int]
    CHUNK_CHATS_FIELD_NUMBER: _ClassVar[int]
    CHUNK_MESSAGES_FIELD_NUMBER: _ClassVar[int]
    DONE_CHATS_FIELD_NUMBER: _ClassVar[int]
    DONE_MESSAGES_FIELD_NUMBER: _ClassVar[int]
    type: SyncType
    phase: SyncPhase
    percent: int
    chunk: int
    chunk_chats: int
    chunk_messages: int
    done_chats: int
    done_messages: int
    def __init__(self, type: _Optional[_Union[SyncType, str]] = ..., phase: _Optional[_Union[SyncPhase, str]] = ..., percent: _Optional[int] = ..., chunk: _Optional[int] = ..., chunk_chats: _Optional[int] = ..., chunk_messages: _Optional[int] = ..., done_chats: _Optional[int] = ..., done_messages: _Optional[int] = ...) -> None: ...

class ProblemsView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class ProblemRow(_message.Message):
    __slots__ = ("kind", "since_ms", "next_retry_ms", "text")
    KIND_FIELD_NUMBER: _ClassVar[int]
    SINCE_MS_FIELD_NUMBER: _ClassVar[int]
    NEXT_RETRY_MS_FIELD_NUMBER: _ClassVar[int]
    TEXT_FIELD_NUMBER: _ClassVar[int]
    kind: ProblemKind
    since_ms: int
    next_retry_ms: int
    text: str
    def __init__(self, kind: _Optional[_Union[ProblemKind, str]] = ..., since_ms: _Optional[int] = ..., next_retry_ms: _Optional[int] = ..., text: _Optional[str] = ...) -> None: ...

class SessionUpdate(_message.Message):
    __slots__ = ("focused", "active_chat_id", "shows_notifications")
    FOCUSED_FIELD_NUMBER: _ClassVar[int]
    ACTIVE_CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    SHOWS_NOTIFICATIONS_FIELD_NUMBER: _ClassVar[int]
    focused: bool
    active_chat_id: str
    shows_notifications: bool
    def __init__(self, focused: _Optional[bool] = ..., active_chat_id: _Optional[str] = ..., shows_notifications: _Optional[bool] = ...) -> None: ...

class DaemonReconnect(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class AccountLogout(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...
