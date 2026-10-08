from google.protobuf.internal import containers as _containers
from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from collections.abc import Iterable as _Iterable, Mapping as _Mapping
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PrivacyCategory(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRIVACY_CATEGORY_UNSPECIFIED: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_LAST_SEEN: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_ONLINE: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_PROFILE_PHOTO: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_ABOUT: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_GROUP_ADD: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_CALL_ADD: _ClassVar[PrivacyCategory]
    PRIVACY_CATEGORY_READ_RECEIPTS: _ClassVar[PrivacyCategory]

class PrivacyValue(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PRIVACY_VALUE_UNSPECIFIED: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_ALL: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_CONTACTS: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_CONTACTS_EXCEPT: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_NOBODY: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_MATCH_LAST_SEEN: _ClassVar[PrivacyValue]
    PRIVACY_VALUE_KNOWN: _ClassVar[PrivacyValue]
PRIVACY_CATEGORY_UNSPECIFIED: PrivacyCategory
PRIVACY_CATEGORY_LAST_SEEN: PrivacyCategory
PRIVACY_CATEGORY_ONLINE: PrivacyCategory
PRIVACY_CATEGORY_PROFILE_PHOTO: PrivacyCategory
PRIVACY_CATEGORY_ABOUT: PrivacyCategory
PRIVACY_CATEGORY_GROUP_ADD: PrivacyCategory
PRIVACY_CATEGORY_CALL_ADD: PrivacyCategory
PRIVACY_CATEGORY_READ_RECEIPTS: PrivacyCategory
PRIVACY_VALUE_UNSPECIFIED: PrivacyValue
PRIVACY_VALUE_ALL: PrivacyValue
PRIVACY_VALUE_CONTACTS: PrivacyValue
PRIVACY_VALUE_CONTACTS_EXCEPT: PrivacyValue
PRIVACY_VALUE_NOBODY: PrivacyValue
PRIVACY_VALUE_MATCH_LAST_SEEN: PrivacyValue
PRIVACY_VALUE_KNOWN: PrivacyValue

class PrivacyView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class PrivacyRow(_message.Message):
    __slots__ = ("last_seen", "online", "profile_photo", "about", "group_add", "call_add", "read_receipts", "default_timer_secs")
    LAST_SEEN_FIELD_NUMBER: _ClassVar[int]
    ONLINE_FIELD_NUMBER: _ClassVar[int]
    PROFILE_PHOTO_FIELD_NUMBER: _ClassVar[int]
    ABOUT_FIELD_NUMBER: _ClassVar[int]
    GROUP_ADD_FIELD_NUMBER: _ClassVar[int]
    CALL_ADD_FIELD_NUMBER: _ClassVar[int]
    READ_RECEIPTS_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_TIMER_SECS_FIELD_NUMBER: _ClassVar[int]
    last_seen: PrivacyValue
    online: PrivacyValue
    profile_photo: PrivacyValue
    about: PrivacyValue
    group_add: PrivacyValue
    call_add: PrivacyValue
    read_receipts: bool
    default_timer_secs: int
    def __init__(self, last_seen: _Optional[_Union[PrivacyValue, str]] = ..., online: _Optional[_Union[PrivacyValue, str]] = ..., profile_photo: _Optional[_Union[PrivacyValue, str]] = ..., about: _Optional[_Union[PrivacyValue, str]] = ..., group_add: _Optional[_Union[PrivacyValue, str]] = ..., call_add: _Optional[_Union[PrivacyValue, str]] = ..., read_receipts: _Optional[bool] = ..., default_timer_secs: _Optional[int] = ...) -> None: ...

class PrivacySet(_message.Message):
    __slots__ = ("category", "value")
    CATEGORY_FIELD_NUMBER: _ClassVar[int]
    VALUE_FIELD_NUMBER: _ClassVar[int]
    category: PrivacyCategory
    value: PrivacyValue
    def __init__(self, category: _Optional[_Union[PrivacyCategory, str]] = ..., value: _Optional[_Union[PrivacyValue, str]] = ...) -> None: ...

class PrivacySetDefaultTimer(_message.Message):
    __slots__ = ("seconds",)
    SECONDS_FIELD_NUMBER: _ClassVar[int]
    seconds: int
    def __init__(self, seconds: _Optional[int] = ...) -> None: ...

class PreferencesView(_message.Message):
    __slots__ = ()
    def __init__(self) -> None: ...

class PreferencesRow(_message.Message):
    __slots__ = ("preferences",)
    PREFERENCES_FIELD_NUMBER: _ClassVar[int]
    preferences: Preferences
    def __init__(self, preferences: _Optional[_Union[Preferences, _Mapping]] = ...) -> None: ...

class Preferences(_message.Message):
    __slots__ = ("notifications", "notification_sound", "notification_preview", "auto_download_photos", "auto_download_videos", "auto_download_audio", "auto_download_documents", "auto_download_stickers", "auto_download_max_bytes", "auto_fetch_maps", "default_frontend", "terminal", "mute_archived_chats", "anti_delete", "keep_chats_archived")
    NOTIFICATIONS_FIELD_NUMBER: _ClassVar[int]
    NOTIFICATION_SOUND_FIELD_NUMBER: _ClassVar[int]
    NOTIFICATION_PREVIEW_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_PHOTOS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_VIDEOS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_AUDIO_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_DOCUMENTS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_STICKERS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_MAX_BYTES_FIELD_NUMBER: _ClassVar[int]
    AUTO_FETCH_MAPS_FIELD_NUMBER: _ClassVar[int]
    DEFAULT_FRONTEND_FIELD_NUMBER: _ClassVar[int]
    TERMINAL_FIELD_NUMBER: _ClassVar[int]
    MUTE_ARCHIVED_CHATS_FIELD_NUMBER: _ClassVar[int]
    ANTI_DELETE_FIELD_NUMBER: _ClassVar[int]
    KEEP_CHATS_ARCHIVED_FIELD_NUMBER: _ClassVar[int]
    notifications: bool
    notification_sound: bool
    notification_preview: bool
    auto_download_photos: bool
    auto_download_videos: bool
    auto_download_audio: bool
    auto_download_documents: bool
    auto_download_stickers: bool
    auto_download_max_bytes: int
    auto_fetch_maps: bool
    default_frontend: str
    terminal: _containers.RepeatedScalarFieldContainer[str]
    mute_archived_chats: bool
    anti_delete: bool
    keep_chats_archived: bool
    def __init__(self, notifications: _Optional[bool] = ..., notification_sound: _Optional[bool] = ..., notification_preview: _Optional[bool] = ..., auto_download_photos: _Optional[bool] = ..., auto_download_videos: _Optional[bool] = ..., auto_download_audio: _Optional[bool] = ..., auto_download_documents: _Optional[bool] = ..., auto_download_stickers: _Optional[bool] = ..., auto_download_max_bytes: _Optional[int] = ..., auto_fetch_maps: _Optional[bool] = ..., default_frontend: _Optional[str] = ..., terminal: _Optional[_Iterable[str]] = ..., mute_archived_chats: _Optional[bool] = ..., anti_delete: _Optional[bool] = ..., keep_chats_archived: _Optional[bool] = ...) -> None: ...

class Argv(_message.Message):
    __slots__ = ("args",)
    ARGS_FIELD_NUMBER: _ClassVar[int]
    args: _containers.RepeatedScalarFieldContainer[str]
    def __init__(self, args: _Optional[_Iterable[str]] = ...) -> None: ...

class PreferencesSet(_message.Message):
    __slots__ = ("notifications", "notification_sound", "notification_preview", "auto_download_photos", "auto_download_videos", "auto_download_audio", "auto_download_documents", "auto_download_stickers", "auto_download_max_bytes", "auto_fetch_maps", "terminal", "mute_archived_chats", "anti_delete", "keep_chats_archived")
    NOTIFICATIONS_FIELD_NUMBER: _ClassVar[int]
    NOTIFICATION_SOUND_FIELD_NUMBER: _ClassVar[int]
    NOTIFICATION_PREVIEW_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_PHOTOS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_VIDEOS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_AUDIO_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_DOCUMENTS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_STICKERS_FIELD_NUMBER: _ClassVar[int]
    AUTO_DOWNLOAD_MAX_BYTES_FIELD_NUMBER: _ClassVar[int]
    AUTO_FETCH_MAPS_FIELD_NUMBER: _ClassVar[int]
    TERMINAL_FIELD_NUMBER: _ClassVar[int]
    MUTE_ARCHIVED_CHATS_FIELD_NUMBER: _ClassVar[int]
    ANTI_DELETE_FIELD_NUMBER: _ClassVar[int]
    KEEP_CHATS_ARCHIVED_FIELD_NUMBER: _ClassVar[int]
    notifications: bool
    notification_sound: bool
    notification_preview: bool
    auto_download_photos: bool
    auto_download_videos: bool
    auto_download_audio: bool
    auto_download_documents: bool
    auto_download_stickers: bool
    auto_download_max_bytes: int
    auto_fetch_maps: bool
    terminal: Argv
    mute_archived_chats: bool
    anti_delete: bool
    keep_chats_archived: bool
    def __init__(self, notifications: _Optional[bool] = ..., notification_sound: _Optional[bool] = ..., notification_preview: _Optional[bool] = ..., auto_download_photos: _Optional[bool] = ..., auto_download_videos: _Optional[bool] = ..., auto_download_audio: _Optional[bool] = ..., auto_download_documents: _Optional[bool] = ..., auto_download_stickers: _Optional[bool] = ..., auto_download_max_bytes: _Optional[int] = ..., auto_fetch_maps: _Optional[bool] = ..., terminal: _Optional[_Union[Argv, _Mapping]] = ..., mute_archived_chats: _Optional[bool] = ..., anti_delete: _Optional[bool] = ..., keep_chats_archived: _Optional[bool] = ...) -> None: ...
