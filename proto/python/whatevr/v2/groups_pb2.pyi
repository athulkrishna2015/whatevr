from whatevr.v2 import people_pb2 as _people_pb2
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class GroupRole(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    GROUP_ROLE_UNSPECIFIED: _ClassVar[GroupRole]
    GROUP_ROLE_MEMBER: _ClassVar[GroupRole]
    GROUP_ROLE_ADMIN: _ClassVar[GroupRole]
    GROUP_ROLE_SUPERADMIN: _ClassVar[GroupRole]
    GROUP_ROLE_LEFT: _ClassVar[GroupRole]
GROUP_ROLE_UNSPECIFIED: GroupRole
GROUP_ROLE_MEMBER: GroupRole
GROUP_ROLE_ADMIN: GroupRole
GROUP_ROLE_SUPERADMIN: GroupRole
GROUP_ROLE_LEFT: GroupRole

class GroupView(_message.Message):
    __slots__ = ("chat_id",)
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    chat_id: str
    def __init__(self, chat_id: _Optional[str] = ...) -> None: ...

class GroupRow(_message.Message):
    __slots__ = ("chat_id", "subject", "description", "avatar_path", "created_ms", "owner", "member_count", "my_role", "announce", "locked", "approval", "community_id", "error")
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    SUBJECT_FIELD_NUMBER: _ClassVar[int]
    DESCRIPTION_FIELD_NUMBER: _ClassVar[int]
    AVATAR_PATH_FIELD_NUMBER: _ClassVar[int]
    CREATED_MS_FIELD_NUMBER: _ClassVar[int]
    OWNER_FIELD_NUMBER: _ClassVar[int]
    MEMBER_COUNT_FIELD_NUMBER: _ClassVar[int]
    MY_ROLE_FIELD_NUMBER: _ClassVar[int]
    ANNOUNCE_FIELD_NUMBER: _ClassVar[int]
    LOCKED_FIELD_NUMBER: _ClassVar[int]
    APPROVAL_FIELD_NUMBER: _ClassVar[int]
    COMMUNITY_ID_FIELD_NUMBER: _ClassVar[int]
    ERROR_FIELD_NUMBER: _ClassVar[int]
    chat_id: str
    subject: str
    description: str
    avatar_path: str
    created_ms: int
    owner: _people_pb2.Person
    member_count: int
    my_role: GroupRole
    announce: bool
    locked: bool
    approval: bool
    community_id: str
    error: str
    def __init__(self, chat_id: _Optional[str] = ..., subject: _Optional[str] = ..., description: _Optional[str] = ..., avatar_path: _Optional[str] = ..., created_ms: _Optional[int] = ..., owner: _Optional[_Union[_people_pb2.Person, _Mapping]] = ..., member_count: _Optional[int] = ..., my_role: _Optional[_Union[GroupRole, str]] = ..., announce: _Optional[bool] = ..., locked: _Optional[bool] = ..., approval: _Optional[bool] = ..., community_id: _Optional[str] = ..., error: _Optional[str] = ...) -> None: ...

class GroupMembersView(_message.Message):
    __slots__ = ("chat_id",)
    CHAT_ID_FIELD_NUMBER: _ClassVar[int]
    chat_id: str
    def __init__(self, chat_id: _Optional[str] = ...) -> None: ...

class GroupMemberRow(_message.Message):
    __slots__ = ("person", "role")
    PERSON_FIELD_NUMBER: _ClassVar[int]
    ROLE_FIELD_NUMBER: _ClassVar[int]
    person: _people_pb2.Person
    role: GroupRole
    def __init__(self, person: _Optional[_Union[_people_pb2.Person, _Mapping]] = ..., role: _Optional[_Union[GroupRole, str]] = ...) -> None: ...
