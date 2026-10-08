#include "v2shim.h"

#include <QDateTime>
#include <QJsonArray>

#include <string_view>

#include "protocolclient.h"

namespace whatevr::proto
{

QString v2s(std::string_view view)
{
    return QString::fromUtf8(view.data(), static_cast<int>(view.size()));
}

QString v2ErrorCode(whatevr::v2::ErrorCode code)
{
    using whatevr::v2::ErrorCode;
    switch (code) {
    case ErrorCode::ERROR_CODE_INVALID_REQUEST:
        return QStringLiteral("invalid_request");
    case ErrorCode::ERROR_CODE_UNKNOWN_METHOD:
        return QStringLiteral("unknown_method");
    case ErrorCode::ERROR_CODE_INVALID_PARAMS:
        return QStringLiteral("invalid_params");
    case ErrorCode::ERROR_CODE_NOT_FOUND:
        return QStringLiteral("not_found");
    case ErrorCode::ERROR_CODE_NOT_LOGGED_IN:
        return QStringLiteral("not_logged_in");
    case ErrorCode::ERROR_CODE_NOT_CONNECTED:
        return QStringLiteral("not_connected");
    case ErrorCode::ERROR_CODE_ALREADY_EXISTS:
        return QStringLiteral("already_exists");
    case ErrorCode::ERROR_CODE_EXPIRED:
        return QStringLiteral("expired");
    case ErrorCode::ERROR_CODE_REJECTED:
        return QStringLiteral("rejected");
    case ErrorCode::ERROR_CODE_IO:
        return QStringLiteral("io");
    case ErrorCode::ERROR_CODE_INTERNAL:
        return QStringLiteral("internal");
    case ErrorCode::ERROR_CODE_GUARDED:
        // v2-only; the v1 client surfaces it like a rejection.
        return QStringLiteral("rejected");
    case ErrorCode::ERROR_CODE_UNSPECIFIED:
    default:
        return QStringLiteral("internal");
    }
}

void buildV2Hello(std::uint64_t id, const QString &clientName, whatevr::v2::Request *out)
{
    out->set_id(id);
    auto *hello = out->mutable_hello();
    hello->set_client(clientName.toStdString());
    hello->set_protocol(2);
}

QVariantMap translateV2HelloResult(const whatevr::v2::HelloResult &result)
{
    QVariantMap info;
    info.insert(QStringLiteral("daemon"), v2s(result.daemon()));
    info.insert(QStringLiteral("version"), v2s(result.version()));
    info.insert(QStringLiteral("protocol"), static_cast<qint64>(result.protocol()));
    info.insert(QStringLiteral("data_dir"), v2s(result.data_dir()));
    info.insert(QStringLiteral("cache_dir"), v2s(result.cache_dir()));
    QStringList features;
    features.reserve(result.features_size());
    for (const auto &feature : result.features()) {
        features.append(v2s(feature));
    }
    info.insert(QStringLiteral("features"), features);
    return info;
}

namespace
{

// v1 `filter` strings to the v2 enum; false for anything else.
bool v2ChatFilter(const QString &filter, whatevr::v2::ChatFilter *out)
{
    using whatevr::v2::ChatFilter;
    if (filter.isEmpty() || filter == QLatin1String("all")) {
        *out = ChatFilter::CHAT_FILTER_ALL;
    } else if (filter == QLatin1String("direct")) {
        *out = ChatFilter::CHAT_FILTER_DIRECT;
    } else if (filter == QLatin1String("groups")) {
        *out = ChatFilter::CHAT_FILTER_GROUPS;
    } else if (filter == QLatin1String("favorite")) {
        *out = ChatFilter::CHAT_FILTER_FAVORITE;
    } else {
        // unread has no server filter; the Unread sidebar filter runs over
        // `all` through the unread proxy instead.
        return false;
    }
    return true;
}

} // namespace

bool buildV2Subscribe(std::uint64_t id, const QString &view, const QJsonObject &params,
                      whatevr::v2::Request *out)
{
    out->set_id(id);
    auto *subscribe = out->mutable_subscribe();
    subscribe->set_limit(static_cast<std::uint32_t>(qMax(0, params.value(QStringLiteral("limit")).toInt(0))));
    // Views the v2 daemon serves, 1:1 with the v1 names the frontend uses.
    // Views without a v2 arm (chat_folders, status, calls, channels, logs,
    // chat_links, ...) return false: unknown_method, like an unserved view.
    if (view == QLatin1String("connection")) {
        subscribe->mutable_connection();
    } else if (view == QLatin1String("login")) {
        subscribe->mutable_login();
    } else if (view == QLatin1String("sync")) {
        subscribe->mutable_sync();
    } else if (view == QLatin1String("chats")) {
        auto *chats = subscribe->mutable_chats();
        whatevr::v2::ChatFilter filter = whatevr::v2::CHAT_FILTER_ALL;
        if (!v2ChatFilter(params.value(QStringLiteral("filter")).toString(), &filter)) {
            return false;
        }
        chats->set_filter(filter);
        chats->set_archived(params.value(QStringLiteral("archived")).toBool(false));
        chats->set_folder_id(params.value(QStringLiteral("folder_id")).toInteger());
    } else if (view == QLatin1String("chat_folders")) {
        subscribe->mutable_chat_folders();
    } else if (view == QLatin1String("status")) {
        subscribe->mutable_status();
    } else if (view == QLatin1String("status.muted")) {
        subscribe->mutable_status_muted();
    } else if (view == QLatin1String("typing")) {
        subscribe->mutable_typing();
    } else if (view == QLatin1String("transfers")) {
        subscribe->mutable_transfers();
    } else if (view == QLatin1String("daemon.logs")) {
        subscribe->mutable_logs();
    } else if (view == QLatin1String("self")) {
        subscribe->mutable_self();
    } else if (view == QLatin1String("preferences")) {
        subscribe->mutable_preferences();
    } else if (view == QLatin1String("privacy")) {
        subscribe->mutable_privacy();
    } else if (view == QLatin1String("blocklist")) {
        subscribe->mutable_blocklist();
    } else if (view == QLatin1String("chat")) {
        subscribe->mutable_chat()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("messages")) {
        auto *messages = subscribe->mutable_messages();
        messages->set_chat_id(params.value(QStringLiteral("chat_id")).toString().toStdString());
        const QString anchor = params.value(QStringLiteral("anchor")).toString();
        if (anchor.isEmpty() || anchor == QLatin1String("latest")) {
            messages->mutable_latest();
        } else if (anchor == QLatin1String("unread")) {
            messages->mutable_unread();
        } else {
            messages->set_message_id(anchor.toStdString());
        }
    } else if (view == QLatin1String("contact")) {
        subscribe->mutable_contact()->mutable_person()->set_id(
            params.value(QStringLiteral("jid")).toString().toStdString());
    } else if (view == QLatin1String("group")) {
        subscribe->mutable_group()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("group_members")) {
        subscribe->mutable_group_members()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("presence")) {
        subscribe->mutable_presence()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("receipts")) {
        subscribe->mutable_receipts()->set_message_id(
            params.value(QStringLiteral("message_id")).toString().toStdString());
    } else if (view == QLatin1String("starred")) {
        subscribe->mutable_starred()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("pinned")) {
        subscribe->mutable_pinned()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("live_locations")) {
        subscribe->mutable_live_locations()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("chat_media")) {
        subscribe->mutable_chat_media()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("chat_links")) {
        subscribe->mutable_chat_links()->set_chat_id(
            params.value(QStringLiteral("chat_id")).toString().toStdString());
    } else if (view == QLatin1String("stickers")) {
        const QString source = params.value(QStringLiteral("source")).toString();
        if (source == QLatin1String("recent")) {
            subscribe->mutable_stickers()->set_source(whatevr::v2::STICKER_SOURCE_RECENT);
        } else if (source == QLatin1String("favorite")) {
            subscribe->mutable_stickers()->set_source(whatevr::v2::STICKER_SOURCE_FAVORITE);
        } else if (source == QLatin1String("all")) {
            subscribe->mutable_stickers()->set_source(whatevr::v2::STICKER_SOURCE_ALL);
        } else {
            return false;
        }
    } else if (view == QLatin1String("sticker_packs")) {
        subscribe->mutable_sticker_packs();
    } else if (view == QLatin1String("sticker_pack")) {
        subscribe->mutable_sticker_pack()->set_pack_id(
            params.value(QStringLiteral("pack_id")).toString().toStdString());
    } else {
        // messages/chat/group/presence/... land here as their translators do.
        return false;
    }
    out->set_id(id);
    return true;
}

void buildV2Extend(std::uint64_t id, std::uint64_t sub, int count, const QString &direction,
                   whatevr::v2::Request *out)
{
    out->set_id(id);
    auto *extend = out->mutable_extend();
    extend->set_sub(sub);
    extend->set_count(static_cast<std::uint32_t>(qMax(0, count)));
    if (direction == QLatin1String("newer")) {
        extend->set_direction(whatevr::v2::DIRECTION_NEWER);
    } else {
        extend->set_direction(whatevr::v2::DIRECTION_OLDER);
    }
}

void buildV2Unsubscribe(std::uint64_t id, std::uint64_t sub, whatevr::v2::Request *out)
{
    out->set_id(id);
    out->mutable_unsubscribe()->set_sub(sub);
}

V2ResponseTranslation translateV2Response(const whatevr::v2::Response &response)
{
    V2ResponseTranslation out;
    switch (response.result_case()) {
    case whatevr::v2::Response::kError: {
        const auto &error = response.error();
        out.errorCode = v2ErrorCode(error.code());
        out.errorMessage = v2s(error.message());
        break;
    }
    case whatevr::v2::Response::kDone:
        break;
    case whatevr::v2::Response::kSubscribe: {
        QJsonObject result;
        result.insert(QStringLiteral("sub"), QString::number(response.subscribe().sub()));
        if (!response.subscribe().anchor_id().empty()) {
            result.insert(QStringLiteral("anchor_id"),
                          v2s(response.subscribe().anchor_id()));
        }
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kHello: {
        QJsonObject result;
        const auto &hello = response.hello();
        result.insert(QStringLiteral("daemon"), v2s(hello.daemon()));
        result.insert(QStringLiteral("version"), v2s(hello.version()));
        result.insert(QStringLiteral("protocol"), static_cast<qint64>(hello.protocol()));
        QJsonArray features;
        for (const auto &feature : hello.features()) {
            features.append(v2s(feature));
        }
        result.insert(QStringLiteral("features"), features);
        result.insert(QStringLiteral("data_dir"), v2s(hello.data_dir()));
        result.insert(QStringLiteral("cache_dir"), v2s(hello.cache_dir()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kSend: {
        QJsonObject result;
        if (!response.send().message_id().empty()) {
            result.insert(QStringLiteral("message_id"), v2s(response.send().message_id()));
        }
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kChatRequestOlder: {
        QJsonObject result;
        result.insert(QStringLiteral("requested"), response.chat_request_older().requested());
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kDaemonBackupExport: {
        QJsonObject result;
        result.insert(QStringLiteral("path"), v2s(response.daemon_backup_export().path()));
        result.insert(QStringLiteral("size_bytes"),
                      static_cast<qint64>(response.daemon_backup_export().size_bytes()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kChatEnsureDirect: {
        QJsonObject result;
        result.insert(QStringLiteral("chat_id"), v2s(response.chat_ensure_direct().chat_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kChatMarkAllRead: {
        QJsonObject result;
        result.insert(QStringLiteral("count"), response.chat_mark_all_read().count());
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kScheduleText: {
        QJsonObject result;
        result.insert(QStringLiteral("scheduled_id"),
                      static_cast<qint64>(response.schedule_text().scheduled_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kScheduleList: {
        QJsonArray messages;
        for (const auto &message : response.schedule_list().messages()) {
            QJsonObject item;
            item.insert(QStringLiteral("id"), static_cast<qint64>(message.id()));
            item.insert(QStringLiteral("chat_id"), v2s(message.chat_id()));
            item.insert(QStringLiteral("text"), v2s(message.text()));
            item.insert(QStringLiteral("send_at"), static_cast<qint64>(message.send_at()));
            messages.append(item);
        }
        QJsonObject result;
        result.insert(QStringLiteral("messages"), messages);
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kGroupCreate: {
        QJsonObject result;
        result.insert(QStringLiteral("chat_id"), v2s(response.group_create().chat_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kGroupInviteLink: {
        QJsonObject result;
        result.insert(QStringLiteral("link"), v2s(response.group_invite_link().link()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kChatFolderCreate: {
        QJsonObject result;
        result.insert(QStringLiteral("id"),
                      static_cast<qint64>(response.chat_folder_create().folder().id()));
        result.insert(QStringLiteral("name"), v2s(response.chat_folder_create().folder().name()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kChatExport: {
        QJsonObject result;
        result.insert(QStringLiteral("path"), v2s(response.chat_export().path()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMessageEditHistory: {
        QJsonArray edits;
        for (const auto &version : response.message_edit_history().edits()) {
            QJsonObject item;
            item.insert(QStringLiteral("text"), v2s(version.text()));
            item.insert(QStringLiteral("edited_at"), static_cast<qint64>(version.edited_at()));
            edits.append(item);
        }
        QJsonObject result;
        result.insert(QStringLiteral("edits"), edits);
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMessageForward: {
        // v1 answers the whole array; keep the singular alias some callers use.
        QJsonArray forwardedIds;
        for (const auto &messageId : response.message_forward().message_ids()) {
            forwardedIds.append(v2s(messageId));
        }
        QJsonObject result;
        result.insert(QStringLiteral("message_ids"), forwardedIds);
        if (!forwardedIds.isEmpty()) {
            result.insert(QStringLiteral("message_id"), forwardedIds.first());
        }
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kGroupJoinInvite: {
        QJsonObject result;
        result.insert(QStringLiteral("chat_id"), v2s(response.group_join_invite().chat_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMessageText: {
        QJsonObject result;
        result.insert(QStringLiteral("text"), v2s(response.message_text().text()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMediaStream: {
        QJsonObject result;
        result.insert(QStringLiteral("url"), v2s(response.media_stream().url()));
        result.insert(QStringLiteral("stream_id"), v2s(response.media_stream().stream_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMediaFetchProfilePicture: {
        QJsonObject result;
        result.insert(QStringLiteral("path"), v2s(response.media_fetch_profile_picture().path()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMediaSave: {
        QJsonObject result;
        result.insert(QStringLiteral("path"), v2s(response.media_save().path()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kSearchChats: {
        QJsonArray chats;
        for (const auto &row : response.search_chats().chats()) {
            chats.append(translateV2ChatRow(row));
        }
        QJsonObject result;
        result.insert(QStringLiteral("chats"), chats);
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kSearchMessages: {
        QJsonArray messages;
        for (const auto &row : response.search_messages().messages()) {
            messages.append(translateV2MessageRow(row));
        }
        QJsonObject result;
        result.insert(QStringLiteral("messages"), messages);
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kContactCheckPhone: {
        const auto &phone = response.contact_check_phone();
        QJsonObject result;
        result.insert(QStringLiteral("registered"), phone.registered());
        result.insert(QStringLiteral("jid"), v2s(phone.person_id()));
        result.insert(QStringLiteral("display_name"), v2s(phone.name()));
        result.insert(QStringLiteral("phone"), v2s(phone.phone()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kSearchStickers: {
        QJsonArray stickers;
        for (const auto &row : response.search_stickers().stickers()) {
            QJsonObject item;
            item.insert(QStringLiteral("id"), v2s(row.id()));
            item.insert(QStringLiteral("cache_key"), v2s(row.id()));
            item.insert(QStringLiteral("local_path"), v2s(row.path()));
            item.insert(QStringLiteral("mime_type"), v2s(row.mime()));
            if (row.animated() || row.lottie()) {
                item.insert(QStringLiteral("is_animated"), true);
            }
            item.insert(QStringLiteral("width"), static_cast<qint64>(row.width()));
            item.insert(QStringLiteral("height"), static_cast<qint64>(row.height()));
            stickers.append(item);
        }
        QJsonObject result;
        result.insert(QStringLiteral("stickers"), stickers);
        out.result = result;
        break;
    }
    default:
        // Further result arms gain translators with the commands that need
        // them; an empty result still completes the request.
        break;
    }
    return out;
}

namespace
{

// Forward declaration: defined with the other body translators below.
QPair<QString, QJsonObject> translateV2MessageBody(const whatevr::v2::MessageRow &row);

QString v2ConnectionState(whatevr::v2::ConnectionState state)
{
    using whatevr::v2::ConnectionState;
    switch (state) {
    case ConnectionState::CONNECTION_STATE_NEED_LOGIN:
        return QStringLiteral("need_login");
    case ConnectionState::CONNECTION_STATE_CONNECTING:
        return QStringLiteral("connecting");
    case ConnectionState::CONNECTION_STATE_ONLINE:
        return QStringLiteral("online");
    case ConnectionState::CONNECTION_STATE_WAITING:
        return QStringLiteral("reconnecting");
    case ConnectionState::CONNECTION_STATE_OFFLINE:
        return QStringLiteral("offline");
    case ConnectionState::CONNECTION_STATE_STARTING:
    case ConnectionState::CONNECTION_STATE_UNSPECIFIED:
    default:
        return QStringLiteral("starting");
    }
}

// Go's RFC3339Nano for a whole-millisecond UTC time, which is what the v1
// daemon emitted and what the QML countdown parses with Qt::ISODateWithMs.
QString rfc3339Millis(qint64 millis)
{
    const QDateTime moment = QDateTime::fromMSecsSinceEpoch(millis, Qt::UTC);
    QString text = moment.toString(QStringLiteral("yyyy-MM-ddTHH:mm:ss"));
    const int ms = static_cast<int>(millis % 1000);
    if (ms != 0) {
        text += QStringLiteral(".%1").arg(ms, 3, 10, QLatin1Char('0'));
    }
    text += QStringLiteral("Z");
    return text;
}

} // namespace

QJsonObject translateV2ConnectionRow(const whatevr::v2::ConnectionRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), QStringLiteral("connection"));
    item.insert(QStringLiteral("state"), v2ConnectionState(row.state()));
    if (!row.detail().empty()) {
        item.insert(QStringLiteral("detail"), v2s(row.detail()));
    }
    item.insert(QStringLiteral("retry_attempt"), static_cast<qint64>(row.attempt()));
    if (row.next_retry_ms() > 0) {
        item.insert(QStringLiteral("next_retry_unix"), static_cast<qint64>(row.next_retry_ms() / 1000));
    }
    item.insert(QStringLiteral("can_reconnect"), row.can_reconnect());
    item.insert(QStringLiteral("pending_outgoing_count"), static_cast<qint64>(row.pending_outgoing()));
    return item;
}

QJsonObject translateV2LoginRow(const whatevr::v2::LoginRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), QStringLiteral("self"));
    QString state;
    switch (row.state()) {
    case whatevr::v2::LOGIN_STATE_LOGGED_IN:
        state = QStringLiteral("logged_in");
        break;
    case whatevr::v2::LOGIN_STATE_QR:
        state = QStringLiteral("qr");
        break;
    case whatevr::v2::LOGIN_STATE_PAIRING:
        state = QStringLiteral("pairing");
        break;
    case whatevr::v2::LOGIN_STATE_FAILED:
        state = QStringLiteral("failed");
        break;
    case whatevr::v2::LOGIN_STATE_UNSPECIFIED:
    default:
        state = QStringLiteral("starting");
        break;
    }
    item.insert(QStringLiteral("state"), state);
    if (!row.detail().empty()) {
        item.insert(QStringLiteral("detail"), v2s(row.detail()));
    }
    if (!row.qr().empty()) {
        QJsonObject qr;
        qr.insert(QStringLiteral("code"), v2s(row.qr()));
        qr.insert(QStringLiteral("expires_at"), rfc3339Millis(row.qr_expires_ms()));
        item.insert(QStringLiteral("qr"), qr);
    }
    return item;
}

QJsonObject translateV2ChatRow(const whatevr::v2::ChatRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    item.insert(QStringLiteral("name"), v2s(row.name()));
    item.insert(QStringLiteral("is_group"), row.type() != whatevr::v2::CHAT_TYPE_DIRECT);
    item.insert(QStringLiteral("preview"), v2s(row.preview().text()));
    item.insert(QStringLiteral("last_message_time"), static_cast<qint64>(row.last_ms() / 1000));
    item.insert(QStringLiteral("last_message_direction"),
                row.preview().from_me() ? QStringLiteral("outgoing") : QStringLiteral("incoming"));
    if (row.preview().status() != whatevr::v2::MESSAGE_STATUS_UNSPECIFIED) {
        item.insert(QStringLiteral("last_message_status"),
                    v2s(
                        whatevr::v2::MessageStatus_Name(row.preview().status()))
                        .remove(QStringLiteral("MESSAGE_STATUS_"))
                        .toLower());
    }
    item.insert(QStringLiteral("unread"), static_cast<qint64>(row.unread()));
    item.insert(QStringLiteral("pinned"), row.pinned());
    item.insert(QStringLiteral("favorite"), row.favorite());
    item.insert(QStringLiteral("archived"), row.archived());
    item.insert(QStringLiteral("muted"), row.muted());
    if (row.mute_end_ms() > 0) {
        item.insert(QStringLiteral("mute_end_timestamp"), static_cast<qint64>(row.mute_end_ms() / 1000));
    }
    item.insert(QStringLiteral("history_exhausted"), row.history_exhausted());
    if (!row.avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.avatar_path()));
    }
    return item;
}

QString v2SortKey(std::string_view sort)
{
    return QString::fromLatin1(QByteArray(sort.data(), static_cast<int>(sort.size())).toHex());
}

QJsonObject translateV2MessageRow(const whatevr::v2::MessageRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    item.insert(QStringLiteral("chat_id"), v2s(row.chat_id()));
    if (!row.chat_name().empty()) {
        item.insert(QStringLiteral("chat_name"), v2s(row.chat_name()));
    }
    QJsonObject sender;
    sender.insert(QStringLiteral("id"), v2s(row.sender().id()));
    sender.insert(QStringLiteral("name"), v2s(row.sender().name()));
    if (!row.sender().avatar_path().empty()) {
        sender.insert(QStringLiteral("avatar_path"), v2s(row.sender().avatar_path()));
    }
    item.insert(QStringLiteral("sender"), sender);
    item.insert(QStringLiteral("sender_id"), v2s(row.sender().id()));
    item.insert(QStringLiteral("sender_name"), v2s(row.sender().name()));
    item.insert(QStringLiteral("from_me"), row.from_me());
    item.insert(QStringLiteral("direction"),
                row.from_me() ? QStringLiteral("outgoing") : QStringLiteral("incoming"));
    item.insert(QStringLiteral("timestamp"), static_cast<qint64>(row.t_ms() / 1000));
    if (row.status() != whatevr::v2::MESSAGE_STATUS_UNSPECIFIED) {
        item.insert(QStringLiteral("status"), v2MessageStatus(row.status()));
    }
    item.insert(QStringLiteral("fallback"), v2s(row.fallback()));
    if (!row.text().empty()) {
        item.insert(QStringLiteral("text"), v2s(row.text()));
    }
    if (!row.chat_name().empty()) {
        item.insert(QStringLiteral("chat_name"), v2s(row.chat_name()));
    }
    if (row.edited()) {
        item.insert(QStringLiteral("edited"), true);
    }
    if (row.revoked()) {
        item.insert(QStringLiteral("revoked"), true);
    }
    if (row.starred()) {
        item.insert(QStringLiteral("starred"), true);
    }
    if (row.forwarded()) {
        item.insert(QStringLiteral("forwarded"), true);
    }
    if (row.viewed()) {
        item.insert(QStringLiteral("viewed"), true);
    }
    if (row.kept()) {
        item.insert(QStringLiteral("kept"), true);
    }
    if (row.has_reply_to()) {
        QJsonObject reply;
        reply.insert(QStringLiteral("message_id"), v2s(row.reply_to().message_id()));
        reply.insert(QStringLiteral("sender_id"), v2s(row.reply_to().sender().id()));
        reply.insert(QStringLiteral("sender_name"), v2s(row.reply_to().sender().name()));
        reply.insert(QStringLiteral("text"), v2s(row.reply_to().text()));
        item.insert(QStringLiteral("reply_to"), reply);
    }
    if (!row.mentions().empty()) {
        QJsonArray mentions;
        for (const auto &mention : row.mentions()) {
            QJsonObject entry;
            entry.insert(QStringLiteral("jid"), v2s(mention.person().id()));
            if (!mention.person().name().empty()) {
                entry.insert(QStringLiteral("name"), v2s(mention.person().name()));
            }
            mentions.append(entry);
        }
        item.insert(QStringLiteral("mentions"), mentions);
    }
    if (!row.reactions().empty()) {
        QJsonArray reactions;
        for (const auto &reaction : row.reactions()) {
            QJsonObject entry;
            entry.insert(QStringLiteral("emoji"), v2s(reaction.emoji()));
            entry.insert(QStringLiteral("sender_id"), v2s(reaction.sender().id()));
            entry.insert(QStringLiteral("sender_name"), v2s(reaction.sender().name()));
            if (reaction.t_ms() > 0) {
                entry.insert(QStringLiteral("timestamp"),
                             static_cast<qint64>(reaction.t_ms() / 1000));
            }
            reactions.append(entry);
        }
        item.insert(QStringLiteral("reactions"), reactions);
    }
    const auto [kind, body] = translateV2MessageBody(row);
    if (!kind.isEmpty()) {
        item.insert(QStringLiteral("kind"), kind);
        for (auto it = body.constBegin(); it != body.constEnd(); ++it) {
            item.insert(it.key(), it.value());
        }
    }
    return item;
}

namespace
{

// The v2 `Upsert.item` arm the frontend's models consume, to v1 JSON.
// Rows arrive arm by arm as their translators land; unknown arms stay an
// empty object rather than a crash.
QJsonObject translateV2Item(const whatevr::v2::Upsert &upsert)
{
    switch (upsert.item_case()) {
    case whatevr::v2::Upsert::kConnection:
        return translateV2ConnectionRow(upsert.connection());
    case whatevr::v2::Upsert::kLogin:
        return translateV2LoginRow(upsert.login());
    case whatevr::v2::Upsert::kChat:
        return translateV2ChatRow(upsert.chat());
    case whatevr::v2::Upsert::kMessage:
        return translateV2MessageRow(upsert.message());
    case whatevr::v2::Upsert::kSync:
        return translateV2SyncRow(upsert.sync());
    case whatevr::v2::Upsert::kTyping:
        return translateV2TypingRow(upsert.typing());
    case whatevr::v2::Upsert::kPresence:
        return translateV2PresenceRow(upsert.presence());
    case whatevr::v2::Upsert::kReceipt:
        return translateV2ReceiptRow(upsert.receipt());
    case whatevr::v2::Upsert::kSelf:
        return translateV2SelfRow(upsert.self());
    case whatevr::v2::Upsert::kContact:
        return translateV2ContactRow(upsert.contact());
    case whatevr::v2::Upsert::kGroup:
        return translateV2GroupRow(upsert.group());
    case whatevr::v2::Upsert::kGroupMember:
        return translateV2GroupMemberRow(upsert.group_member());
    case whatevr::v2::Upsert::kPrivacy:
        return translateV2PrivacyRow(upsert.privacy());
    case whatevr::v2::Upsert::kPreferences:
        return translateV2Preferences(upsert.preferences().preferences());
    case whatevr::v2::Upsert::kBlocked: {
        QJsonObject item;
        item.insert(QStringLiteral("id"), v2s(upsert.blocked().person().id()));
        item.insert(QStringLiteral("jid"), v2s(upsert.blocked().person().id()));
        item.insert(QStringLiteral("name"), v2s(upsert.blocked().person().name()));
        if (!upsert.blocked().person().phone().empty()) {
            item.insert(QStringLiteral("phone"), v2s(upsert.blocked().person().phone()));
        }
        if (!upsert.blocked().person().avatar_path().empty()) {
            item.insert(QStringLiteral("avatar_path"),
                        v2s(upsert.blocked().person().avatar_path()));
        }
        return item;
    }
    case whatevr::v2::Upsert::kLiveLocation:
        return translateV2LiveLocationRow(upsert.live_location(), {});
    case whatevr::v2::Upsert::kSticker:
        return translateV2StickerRow(upsert.sticker());
    case whatevr::v2::Upsert::kStickerPack:
        return translateV2StickerPackRow(upsert.sticker_pack());
    case whatevr::v2::Upsert::kStatusMuted: {
        QJsonObject item;
        item.insert(QStringLiteral("id"), v2s(upsert.status_muted().sender_id()));
        return item;
    }
    case whatevr::v2::Upsert::kChatFolder: {
        QJsonObject item;
        item.insert(QStringLiteral("id"), QString::number(upsert.chat_folder().id()));
        item.insert(QStringLiteral("folder_id"), static_cast<qint64>(upsert.chat_folder().id()));
        item.insert(QStringLiteral("name"), v2s(upsert.chat_folder().name()));
        return item;
    }
    case whatevr::v2::Upsert::kTransfer:
        return translateV2TransferRow(upsert.transfer());
    case whatevr::v2::Upsert::kLog:
        return translateV2LogRow(upsert.log());
    default:
        return {};
    }
}

} // namespace

void applyV2ViewUpdate(const whatevr::v2::ViewUpdate &update, ViewSink *sink,
                       const QJsonObject &extraFields)
{
    if (!sink) {
        return;
    }
    if (update.reset()) {
        sink->onReset();
    }
    // A handler can delete the sink mid-update (warm-window eviction), so
    // re-check liveness after every sink call instead of only at the drain end.
    const std::weak_ptr<const void> alive = sink->lifetime();
    for (const auto &change : update.changes()) {
        if (alive.expired()) {
            return;
        }
        switch (change.change_case()) {
        case whatevr::v2::Change::kUpsert: {
            const auto &upsert = change.upsert();
            QJsonObject item = translateV2Item(upsert);
            for (auto it = extraFields.constBegin(); it != extraFields.constEnd(); ++it) {
                if (!item.contains(it.key())) {
                    item.insert(it.key(), it.value());
                }
            }
            sink->onUpsert(v2SortKey(upsert.sort()), item);
            break;
        }
        case whatevr::v2::Change::kRemove:
            sink->onRemove(v2s(change.remove().id()));
            break;
        default:
            break;
        }
    }
    if (update.has_ready()) {
        // v2's `ready` always carries the flag; the v1 models treat present
        // as authoritative either way.
        sink->onReady(update.ready().exhausted(), true);
    }
}

namespace
{

QString v2PrivacyValue(whatevr::v2::PrivacyValue value)
{
    switch (value) {
    case whatevr::v2::PRIVACY_VALUE_ALL:
        return QStringLiteral("all");
    case whatevr::v2::PRIVACY_VALUE_CONTACTS:
        return QStringLiteral("contacts");
    case whatevr::v2::PRIVACY_VALUE_CONTACTS_EXCEPT:
        return QStringLiteral("contact_blacklist");
    case whatevr::v2::PRIVACY_VALUE_NOBODY:
        return QStringLiteral("nobody");
    case whatevr::v2::PRIVACY_VALUE_MATCH_LAST_SEEN:
        return QStringLiteral("match_last_seen");
    case whatevr::v2::PRIVACY_VALUE_KNOWN:
        return QStringLiteral("known");
    default:
        return {};
    }
}

// v1 `chat_id`/`jid` params to a v2 person address.
void setAddressId(whatevr::v2::Address *address, const QJsonObject &params, const char *key)
{
    address->set_id(params.value(QLatin1StringView(key)).toString().toStdString());
}

} // namespace

bool buildV2Request(std::uint64_t id, const QString &method, const QJsonObject &params,
                    whatevr::v2::Request *out)
{
    const auto get = [&](const char *key) { return params.value(QLatin1StringView(key)); };
    auto *request = out;
    request->set_id(id);

    if (method == QLatin1String("hello")) {
        auto *hello = request->mutable_hello();
        hello->set_client(get("client").toString().toStdString());
        hello->set_protocol(static_cast<std::uint32_t>(get("protocol").toInt(2)));
        return true;
    }

    // Session and account.
    if (method == QLatin1String("session.update")) {
        auto *update = request->mutable_session_update();
        update->set_focused(get("focused").toBool());
        update->set_active_chat_id(get("active_chat_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("daemon.backup_export")) {
        auto *exp = request->mutable_daemon_backup_export();
        exp->set_path(get("path").toString().toStdString());
        exp->set_passphrase(get("passphrase").toString().toStdString());
        exp->set_use_keyring(get("use_keyring").toBool());
        return true;
    }
    if (method == QLatin1String("daemon.backup_set_passphrase")) {
        request->mutable_daemon_backup_set_passphrase()->set_passphrase(
            get("passphrase").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("daemon.shutdown")) {
        request->mutable_daemon_shutdown();
        return true;
    }
    if (method == QLatin1String("daemon.reconnect")) {
        request->mutable_daemon_reconnect();
        return true;
    }
    if (method == QLatin1String("daemon.log")) {
        request->mutable_log_message()->set_message(
            get("message").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("account.logout")) {
        request->mutable_account_logout();
        return true;
    }

    // Chats.
    if (method == QLatin1String("chat.mark_read")) {
        auto *mark = request->mutable_chat_mark_read();
        mark->set_chat_id(get("chat_id").toString().toStdString());
        mark->set_up_to_message_id(get("up_to_message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("chat.mark_all_read")) {
        request->mutable_chat_mark_all_read();
        return true;
    }
    if (method == QLatin1String("chat_folder.create")) {
        request->mutable_chat_folder_create()->set_name(
            get("name").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("chat_folder.rename")) {
        auto *rename = request->mutable_chat_folder_rename();
        rename->set_id(get("id").toInteger());
        rename->set_name(get("name").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("chat_folder.delete")) {
        request->mutable_chat_folder_delete()->set_id(get("id").toInteger());
        return true;
    }
    if (method == QLatin1String("chat_folder.set_chat")) {
        auto *set = request->mutable_chat_folder_set_chat();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_folder_id(get("folder_id").toInteger());
        return true;
    }
    if (method == QLatin1String("chat.export")) {
        auto *exp = request->mutable_chat_export();
        exp->set_chat_id(get("chat_id").toString().toStdString());
        exp->set_path(get("path").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("chat.pin")) {
        auto *pin = request->mutable_chat_pin();
        pin->set_chat_id(get("chat_id").toString().toStdString());
        pin->set_pinned(get("pinned").toBool());
        return true;
    }
    if (method == QLatin1String("chat.favorite")) {
        auto *favorite = request->mutable_chat_favorite();
        favorite->set_chat_id(get("chat_id").toString().toStdString());
        favorite->set_favorite(get("favorite").toBool());
        return true;
    }
    if (method == QLatin1String("chat.archive")) {
        auto *archive = request->mutable_chat_archive();
        archive->set_chat_id(get("chat_id").toString().toStdString());
        archive->set_archived(get("archived").toBool());
        return true;
    }
    if (method == QLatin1String("chat.mute")) {
        auto *mute = request->mutable_chat_mute();
        mute->set_chat_id(get("chat_id").toString().toStdString());
        mute->set_muted(get("muted").toBool());
        mute->set_duration_ms(static_cast<std::int64_t>(get("duration_secs").toInt()) * 1000);
        return true;
    }
    if (method == QLatin1String("chat.typing")) {
        auto *typing = request->mutable_chat_typing();
        typing->set_chat_id(get("chat_id").toString().toStdString());
        typing->set_composing(get("composing").toBool());
        return true;
    }
    if (method == QLatin1String("chat.request_older")) {
        request->mutable_chat_request_older()->set_chat_id(get("chat_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("chat.ensure_direct")) {
        setAddressId(request->mutable_chat_ensure_direct()->mutable_person(), params, "jid");
        return true;
    }

    // Sends.
    if (method == QLatin1String("send.text")) {
        auto *send = request->mutable_send_text();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_text(get("text").toString().toStdString());
        send->set_reply_to(get("reply_to").toString().toStdString());
        for (const QJsonValue &mention : get("mentions").toArray()) {
            send->add_mentions()->set_id(mention.toString().toStdString());
        }
        return true;
    }
    if (method == QLatin1String("send.media")) {
        auto *send = request->mutable_send_media();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_path(get("path").toString().toStdString());
        send->set_caption(get("caption").toString().toStdString());
        send->set_reply_to(get("reply_to").toString().toStdString());
        send->set_as_document(get("kind").toString() == QLatin1String("document"));
        send->set_view_once(get("view_once").toBool());
        return true;
    }
    if (method == QLatin1String("send.sticker")) {
        auto *send = request->mutable_send_sticker();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_sticker_id(get("cache_key").toString().toStdString());
        send->set_reply_to(get("reply_to").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("send.poll")) {
        auto *send = request->mutable_send_poll();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_question(get("question").toString().toStdString());
        for (const QJsonValue &option : get("options").toArray()) {
            send->add_options(option.toString().toStdString());
        }
        send->set_multi(get("multi").toBool());
        send->set_reply_to(get("reply_to").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("send.contact")) {
        auto *send = request->mutable_send_contact();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_name(get("name").toString().toStdString());
        send->set_phone(get("phone").toString().toStdString());
        send->set_reply_to(get("reply_to").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("send.location")) {
        auto *send = request->mutable_send_location();
        send->set_chat_id(get("chat_id").toString().toStdString());
        send->set_lat(get("lat").toDouble());
        send->set_lng(get("long").toDouble());
        send->set_name(get("name").toString().toStdString());
        send->set_address(get("address").toString().toStdString());
        send->set_reply_to(get("reply_to").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("send.cancel")) {
        request->mutable_send_cancel()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("schedule.text")) {
        auto *schedule = request->mutable_schedule_text();
        schedule->set_chat_id(get("chat_id").toString().toStdString());
        schedule->set_text(get("text").toString().toStdString());
        schedule->set_send_at(get("send_at").toInteger());
        return true;
    }
    if (method == QLatin1String("schedule.list")) {
        request->mutable_schedule_list()->set_chat_id(
            get("chat_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("schedule.cancel")) {
        request->mutable_schedule_cancel()->set_id(get("id").toInteger());
        return true;
    }

    // Groups and communities.
    if (method == QLatin1String("group.create")) {
        auto *create = request->mutable_group_create();
        create->set_name(get("name").toString().toStdString());
        for (const QJsonValue &member : get("members").toArray()) {
            create->add_members(member.toString().toStdString());
        }
        create->set_photo_path(get("photo_path").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("group.leave")) {
        request->mutable_group_leave()->set_chat_id(
            get("chat_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("group.set_name")) {
        auto *set = request->mutable_group_set_name();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_name(get("name").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("group.set_topic")) {
        auto *set = request->mutable_group_set_topic();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_description(get("description").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("group.set_photo")) {
        auto *set = request->mutable_group_set_photo();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_path(get("path").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("group.invite_link")) {
        auto *link = request->mutable_group_invite_link();
        link->set_chat_id(get("chat_id").toString().toStdString());
        link->set_reset(get("reset").toBool());
        return true;
    }
    if (method == QLatin1String("group.members")) {
        auto *members = request->mutable_group_members();
        members->set_chat_id(get("chat_id").toString().toStdString());
        members->set_action(get("action").toString().toStdString());
        for (const QJsonValue &member : get("members").toArray()) {
            members->add_members(member.toString().toStdString());
        }
        return true;
    }
    if (method == QLatin1String("group.set_announce")) {
        auto *set = request->mutable_group_set_announce();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_enabled(get("enabled").toBool());
        return true;
    }
    if (method == QLatin1String("group.set_locked")) {
        auto *set = request->mutable_group_set_locked();
        set->set_chat_id(get("chat_id").toString().toStdString());
        set->set_enabled(get("enabled").toBool());
        return true;
    }
    if (method == QLatin1String("community.link")) {
        auto *link = request->mutable_community_link();
        link->set_community_id(get("community_id").toString().toStdString());
        link->set_group_id(get("group_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("community.unlink")) {
        auto *unlink = request->mutable_community_unlink();
        unlink->set_community_id(get("community_id").toString().toStdString());
        unlink->set_group_id(get("group_id").toString().toStdString());
        return true;
    }

    // Messages.
    if (method == QLatin1String("message.react")) {
        auto *react = request->mutable_message_react();
        react->set_message_id(get("message_id").toString().toStdString());
        react->set_emoji(get("emoji").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.edit_history")) {
        request->mutable_message_edit_history()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.edit")) {
        auto *edit = request->mutable_message_edit();
        edit->set_message_id(get("message_id").toString().toStdString());
        edit->set_text(get("text").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.revoke")) {
        request->mutable_message_revoke()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.delete")) {
        request->mutable_message_delete()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.star")) {
        auto *star = request->mutable_message_star();
        star->set_message_id(get("message_id").toString().toStdString());
        star->set_starred(get("starred").toBool());
        return true;
    }
    if (method == QLatin1String("message.pin")) {
        auto *pin = request->mutable_message_pin();
        pin->set_message_id(get("message_id").toString().toStdString());
        pin->set_pinned(get("pinned").toBool());
        pin->set_duration_ms(static_cast<std::int64_t>(get("duration_secs").toInt()) * 1000);
        return true;
    }
    if (method == QLatin1String("message.forward")) {
        auto *forward = request->mutable_message_forward();
        forward->set_message_id(get("message_id").toString().toStdString());
        for (const QJsonValue &chatId : get("chat_ids").toArray()) {
            forward->add_chat_ids(chatId.toString().toStdString());
        }
        return true;
    }
    if (method == QLatin1String("message.mark_played")) {
        request->mutable_message_mark_played()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("message.request_from_phone")) {
        request->mutable_message_request_from_phone()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("poll.vote")) {
        auto *vote = request->mutable_poll_vote();
        vote->set_message_id(get("message_id").toString().toStdString());
        for (const QJsonValue &option : get("option_ids").toArray()) {
            vote->add_option_indexes(static_cast<std::uint32_t>(option.toInt()));
        }
        return true;
    }
    if (method == QLatin1String("event.rsvp")) {
        auto *rsvp = request->mutable_event_rsvp();
        rsvp->set_message_id(get("message_id").toString().toStdString());
        const QString response = get("response").toString();
        if (response == QLatin1String("going")) {
            rsvp->set_response(whatevr::v2::RSVP_GOING);
        } else if (response == QLatin1String("not_going")) {
            rsvp->set_response(whatevr::v2::RSVP_NOT_GOING);
        } else if (response == QLatin1String("maybe")) {
            rsvp->set_response(whatevr::v2::RSVP_MAYBE);
        } else {
            // No wire value means "no answer": fail fast instead of sending
            // UNSPECIFIED for a choice the user never made.
            return false;
        }
        rsvp->set_extra_guests(static_cast<std::uint32_t>(get("extra_guests").toInt()));
        return true;
    }
    if (method == QLatin1String("group.join_invite")) {
        request->mutable_group_join_invite()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }

    // Media.
    if (method == QLatin1String("media.download")) {
        request->mutable_media_download()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("media.cancel_download")) {
        request->mutable_media_cancel_download()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("media.stream")) {
        request->mutable_media_stream()->set_message_id(
            get("message_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("media.fetch_profile_picture")) {
        setAddressId(request->mutable_media_fetch_profile_picture()->mutable_person(), params, "jid");
        return true;
    }
    if (method == QLatin1String("media.save")) {
        auto *save = request->mutable_media_save();
        save->set_message_id(get("message_id").toString().toStdString());
        save->set_status_id(get("status_id").toString().toStdString());
        save->set_jid(get("jid").toString().toStdString());
        save->set_path(get("path").toString().toStdString());
        return true;
    }

    // People and settings.
    if (method == QLatin1String("contact.block")) {
        auto *block = request->mutable_contact_block();
        setAddressId(block->mutable_person(), params, "jid");
        block->set_blocked(get("blocked").toBool());
        return true;
    }
    if (method == QLatin1String("self.set_about")) {
        request->mutable_self_set_about()->set_text(get("text").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("privacy.set")) {
        auto *set = request->mutable_privacy_set();
        const QString category = get("category").toString();
        if (category == QLatin1String("last_seen")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_LAST_SEEN);
        } else if (category == QLatin1String("online")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_ONLINE);
        } else if (category == QLatin1String("profile_photo")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_PROFILE_PHOTO);
        } else if (category == QLatin1String("about")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_ABOUT);
        } else if (category == QLatin1String("group_add")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_GROUP_ADD);
        } else if (category == QLatin1String("call_add")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_CALL_ADD);
        } else if (category == QLatin1String("read_receipts")) {
            set->set_category(whatevr::v2::PRIVACY_CATEGORY_READ_RECEIPTS);
        } else {
            return false;
        }
        const QJsonValue value = get("value");
        if (value.isBool()) {
            set->set_value(value.toBool() ? whatevr::v2::PRIVACY_VALUE_ALL
                                           : whatevr::v2::PRIVACY_VALUE_NOBODY);
        } else {
            const QString text = value.toString();
            if (text == QLatin1String("all")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_ALL);
            } else if (text == QLatin1String("contacts")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_CONTACTS);
            } else if (text == QLatin1String("contact_blacklist")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_CONTACTS_EXCEPT);
            } else if (text == QLatin1String("nobody") || text == QLatin1String("none")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_NOBODY);
            } else if (text == QLatin1String("match_last_seen")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_MATCH_LAST_SEEN);
            } else if (text == QLatin1String("known")) {
                set->set_value(whatevr::v2::PRIVACY_VALUE_KNOWN);
            } else {
                return false;
            }
        }
        return true;
    }
    if (method == QLatin1String("privacy.set_default_timer")) {
        request->mutable_privacy_set_default_timer()->set_seconds(get("seconds").toInteger());
        return true;
    }
    if (method == QLatin1String("preferences.set")) {
        auto *set = request->mutable_preferences_set();
        bool any = false;
        const auto flag = [&](const char *key, auto setter) {
            const QString name = QLatin1StringView(key);
            if (params.contains(name)) {
                (set->*setter)(params.value(name).toBool());
                any = true;
            }
        };
        flag("notifications_enabled", &whatevr::v2::PreferencesSet::set_notifications);
        flag("notification_sound", &whatevr::v2::PreferencesSet::set_notification_sound);
        flag("notification_preview", &whatevr::v2::PreferencesSet::set_notification_preview);
        flag("auto_download_photos", &whatevr::v2::PreferencesSet::set_auto_download_photos);
        flag("auto_download_videos", &whatevr::v2::PreferencesSet::set_auto_download_videos);
        flag("auto_download_audio", &whatevr::v2::PreferencesSet::set_auto_download_audio);
        flag("auto_download_documents", &whatevr::v2::PreferencesSet::set_auto_download_documents);
        flag("auto_download_stickers", &whatevr::v2::PreferencesSet::set_auto_download_stickers);
        flag("mute_archived_chats", &whatevr::v2::PreferencesSet::set_mute_archived_chats);
        flag("anti_delete", &whatevr::v2::PreferencesSet::set_anti_delete);
        flag("keep_chats_archived", &whatevr::v2::PreferencesSet::set_keep_chats_archived);
        if (params.contains(QLatin1String("auto_download_max_bytes"))) {
            set->set_auto_download_max_bytes(
                static_cast<std::uint64_t>(params.value(QLatin1String("auto_download_max_bytes"))
                                                .toVariant()
                                                .toULongLong()));
            any = true;
        }
        // v1-only keys (anti_delete, typing indicators, archived handling)
        // have no v2 field; the covered keys still apply.
        Q_UNUSED(any);
        return true;
    }

    // Queries.
    if (method == QLatin1String("search.chats")) {
        auto *search = request->mutable_search_chats();
        search->set_query(get("query").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("search.messages")) {
        auto *search = request->mutable_search_messages();
        search->set_query(get("query").toString().toStdString());
        search->set_chat_id(get("chat_id").toString().toStdString());
        search->set_limit(static_cast<std::uint32_t>(get("limit").toInt(0)));
        return true;
    }
    if (method == QLatin1String("search.stickers")) {
        auto *search = request->mutable_search_stickers();
        search->set_query(get("query").toString().toStdString());
        search->set_limit(static_cast<std::uint32_t>(get("limit").toInt(0)));
        return true;
    }
    if (method == QLatin1String("contacts.check_phone")) {
        request->mutable_contact_check_phone()->set_phone(get("phone").toString().toStdString());
        return true;
    }

    // Stickers.
    if (method == QLatin1String("sticker.favorite")) {
        auto *favorite = request->mutable_sticker_favorite();
        if (!get("cache_key").toString().isEmpty()) {
            favorite->set_sticker_id(get("cache_key").toString().toStdString());
        } else if (!get("message_id").toString().isEmpty()) {
            favorite->set_message_id(get("message_id").toString().toStdString());
        } else {
            return false;
        }
        favorite->set_favorite(get("favorite").toBool());
        return true;
    }
    if (method == QLatin1String("sticker.download")) {
        request->mutable_sticker_download()->set_sticker_id(
            get("cache_key").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("sticker_packs.refresh")) {
        request->mutable_sticker_packs_refresh();
        return true;
    }
    if (method == QLatin1String("sticker_pack.install")) {
        auto *install = request->mutable_sticker_pack_install();
        install->set_pack_id(get("pack_id").toString().toStdString());
        install->set_installed(get("installed").toBool());
        return true;
    }

    return false;
}

whatevr::v2::ErrorCode v2ErrorCodeFromV1(const QString &code)
{
    if (code == QLatin1String("invalid_request")) {
        return whatevr::v2::ERROR_CODE_INVALID_REQUEST;
    }
    if (code == QLatin1String("unknown_method")) {
        return whatevr::v2::ERROR_CODE_UNKNOWN_METHOD;
    }
    if (code == QLatin1String("invalid_params")) {
        return whatevr::v2::ERROR_CODE_INVALID_PARAMS;
    }
    if (code == QLatin1String("not_found")) {
        return whatevr::v2::ERROR_CODE_NOT_FOUND;
    }
    if (code == QLatin1String("not_logged_in")) {
        return whatevr::v2::ERROR_CODE_NOT_LOGGED_IN;
    }
    if (code == QLatin1String("not_connected")) {
        return whatevr::v2::ERROR_CODE_NOT_CONNECTED;
    }
    if (code == QLatin1String("already_exists")) {
        return whatevr::v2::ERROR_CODE_ALREADY_EXISTS;
    }
    if (code == QLatin1String("expired")) {
        return whatevr::v2::ERROR_CODE_EXPIRED;
    }
    if (code == QLatin1String("rejected")) {
        return whatevr::v2::ERROR_CODE_REJECTED;
    }
    if (code == QLatin1String("io")) {
        return whatevr::v2::ERROR_CODE_IO;
    }
    return whatevr::v2::ERROR_CODE_INTERNAL;
}

void v2ErrorResponse(std::uint64_t id, const QString &code, const QString &message,
                     whatevr::v2::Response *out)
{
    out->set_id(id);
    auto *error = out->mutable_error();
    error->set_code(v2ErrorCodeFromV1(code));
    error->set_message(message.toStdString());
}

bool v2ResponseFromV1(const QString &method, std::uint64_t id, const QJsonObject &result,
                      whatevr::v2::Response *out)
{
    out->set_id(id);
    const auto str = [&](const char *key) {
        return result.value(QLatin1StringView(key)).toString().toStdString();
    };
    if (method == QLatin1String("subscribe")) {
        auto *subscribe = out->mutable_subscribe();
        subscribe->set_sub(v2UInt64(result.value(QStringLiteral("sub"))));
        if (result.contains(QStringLiteral("anchor_id"))) {
            subscribe->set_anchor_id(str("anchor_id"));
        }
        return true;
    }
    if (method == QLatin1String("daemon.backup_export")) {
        auto *exp = out->mutable_daemon_backup_export();
        exp->set_path(str("path"));
        exp->set_size_bytes(static_cast<std::uint64_t>(
            result.value(QStringLiteral("size_bytes")).toInteger()));
        return true;
    }
    if (method == QLatin1String("chat.request_older")) {
        out->mutable_chat_request_older()->set_requested(
            result.value(QStringLiteral("requested")).toBool());
        return true;
    }
    if (method == QLatin1String("chat.mark_all_read")) {
        out->mutable_chat_mark_all_read()->set_count(
            static_cast<std::int32_t>(result.value(QStringLiteral("count")).toInt()));
        return true;
    }
    if (method == QLatin1String("chat.export")) {
        out->mutable_chat_export()->set_path(str("path"));
        return true;
    }
    if (method == QLatin1String("chat_folder.create")) {
        auto *folder = out->mutable_chat_folder_create()->mutable_folder();
        folder->set_id(static_cast<std::int64_t>(result.value(QStringLiteral("id")).toInteger()));
        folder->set_name(str("name"));
        return true;
    }
    if (method == QLatin1String("schedule.text")) {
        out->mutable_schedule_text()->set_scheduled_id(
            static_cast<std::int64_t>(result.value(QStringLiteral("scheduled_id")).toInteger()));
        return true;
    }
    if (method == QLatin1String("schedule.list")) {
        auto *list = out->mutable_schedule_list();
        for (const QJsonValue &row : result.value(QStringLiteral("messages")).toArray()) {
            const QJsonObject item = row.toObject();
            whatevr::v2::ScheduledMessage message;
            message.set_id(static_cast<std::int64_t>(item.value(QStringLiteral("id")).toInteger()));
            message.set_chat_id(item.value(QStringLiteral("chat_id")).toString().toStdString());
            message.set_text(item.value(QStringLiteral("text")).toString().toStdString());
            message.set_send_at(static_cast<std::int64_t>(item.value(QStringLiteral("send_at")).toInteger()));
            *list->add_messages() = message;
        }
        return true;
    }
    if (method == QLatin1String("group.create")) {
        out->mutable_group_create()->set_chat_id(str("chat_id"));
        return true;
    }
    if (method == QLatin1String("group.invite_link")) {
        out->mutable_group_invite_link()->set_link(str("link"));
        return true;
    }
    if (method == QLatin1String("chat.ensure_direct")
        || method == QLatin1String("group.join_invite")) {
        // v1 answers both with `{chat_id}`; v2 has a per-method arm.
        // group.create has its own arm above.
        if (method == QLatin1String("chat.ensure_direct")) {
            out->mutable_chat_ensure_direct()->set_chat_id(str("chat_id"));
        } else {
            out->mutable_group_join_invite()->set_chat_id(str("chat_id"));
        }
        return true;
    }
    if (method == QLatin1String("message.edit_history")) {
        auto *history = out->mutable_message_edit_history();
        for (const QJsonValue &row : result.value(QStringLiteral("edits")).toArray()) {
            const QJsonObject item = row.toObject();
            whatevr::v2::MessageEditVersion version;
            version.set_text(item.value(QStringLiteral("text")).toString().toStdString());
            version.set_edited_at(
                static_cast<std::int64_t>(item.value(QStringLiteral("edited_at")).toInteger()));
            *history->add_edits() = version;
        }
        return true;
    }
    if (method == QLatin1String("message.forward")) {
        auto *forward = out->mutable_message_forward();
        for (const QJsonValue &messageId : result.value(QStringLiteral("message_ids")).toArray()) {
            forward->add_message_ids(messageId.toString().toStdString());
        }
        return true;
    }
    if (method == QLatin1String("message.text")) {
        out->mutable_message_text()->set_text(str("text"));
        return true;
    }
    if (method == QLatin1String("media.stream")) {
        auto *stream = out->mutable_media_stream();
        stream->set_url(str("url"));
        stream->set_stream_id(str("stream_id"));
        return true;
    }
    if (method == QLatin1String("media.fetch_profile_picture")) {
        out->mutable_media_fetch_profile_picture()->set_path(str("path"));
        return true;
    }
    if (method == QLatin1String("media.save")) {
        out->mutable_media_save()->set_path(str("path"));
        return true;
    }
    if (method == QLatin1String("search.chats")) {
        auto *search = out->mutable_search_chats();
        for (const QJsonValue &row : result.value(QStringLiteral("chats")).toArray()) {
            whatevr::v2::ChatRow chat;
            if (v2ChatRowFromJson(row.toObject(), &chat)) {
                *search->add_chats() = chat;
            }
        }
        return true;
    }
    if (method == QLatin1String("search.messages")) {
        auto *search = out->mutable_search_messages();
        for (const QJsonValue &row : result.value(QStringLiteral("messages")).toArray()) {
            whatevr::v2::MessageRow message;
            if (v2MessageRowFromJson(row.toObject(), &message)) {
                *search->add_messages() = message;
            }
        }
        return true;
    }
    if (method == QLatin1String("search.stickers")) {
        auto *search = out->mutable_search_stickers();
        for (const QJsonValue &row : result.value(QStringLiteral("stickers")).toArray()) {
            auto *sticker = search->add_stickers();
            const QJsonObject item = row.toObject();
            sticker->set_id(item.value(QStringLiteral("cache_key")).toString().toStdString());
            sticker->set_path(item.value(QStringLiteral("path")).toString().toStdString());
            sticker->set_mime(item.value(QStringLiteral("mime")).toString().toStdString());
        }
        return true;
    }
    if (method == QLatin1String("contacts.check_phone")) {
        auto *phone = out->mutable_contact_check_phone();
        phone->set_registered(result.value(QStringLiteral("registered")).toBool());
        phone->set_person_id(str("jid"));
        phone->set_name(str("display_name"));
        phone->set_phone(str("phone"));
        return true;
    }
    if (method == QLatin1String("send.text") || method == QLatin1String("send.media")
        || method == QLatin1String("send.sticker") || method == QLatin1String("send.media_batch")
        || method == QLatin1String("send.poll") || method == QLatin1String("send.contact")
        || method == QLatin1String("send.location")) {
        // v1 answers sends with `{message_id}` (batch answers lists, which the
        // controller only reads for failures; fakes answer the single shape).
        if (result.contains(QStringLiteral("message_id"))) {
            out->mutable_send()->set_message_id(str("message_id"));
        } else {
            out->mutable_done();
        }
        return true;
    }
    // Ack-only commands complete with Done.
    out->mutable_done();
    return true;
}

bool v2ChatRowFromJson(const QJsonObject &item, whatevr::v2::ChatRow *out)
{
    if (!item.contains(QStringLiteral("id"))) {
        return false;
    }
    const auto str = [&](const char *key) {
        return item.value(QLatin1StringView(key)).toString().toStdString();
    };
    out->set_id(str("id"));
    out->set_name(str("name"));
    out->set_type(item.value(QStringLiteral("is_group")).toBool()
                      ? whatevr::v2::CHAT_TYPE_GROUP
                      : whatevr::v2::CHAT_TYPE_DIRECT);
    out->mutable_preview()->set_text(str("preview"));
    out->set_last_ms(static_cast<std::int64_t>(item.value(QStringLiteral("last_message_time"))
                                                   .toVariant()
                                                   .toLongLong())
                     * 1000);
    out->set_unread(static_cast<std::uint32_t>(item.value(QStringLiteral("unread")).toInt()));
    out->set_pinned(item.value(QStringLiteral("pinned")).toBool());
    out->set_archived(item.value(QStringLiteral("archived")).toBool());
    out->set_muted(item.value(QStringLiteral("muted")).toBool());
    out->set_history_exhausted(item.value(QStringLiteral("history_exhausted")).toBool());
    out->set_avatar_path(str("avatar_path"));
    return true;
}

namespace
{

void v2MediaFromJson(const QJsonObject &media, whatevr::v2::Media *out)
{
    const auto str = [&](const QJsonObject &obj, const char *key) {
        return obj.value(QLatin1StringView(key)).toString().toStdString();
    };
    out->set_mime(str(media, "mime"));
    out->set_width(static_cast<std::uint32_t>(media.value(QStringLiteral("width")).toInt()));
    out->set_height(static_cast<std::uint32_t>(media.value(QStringLiteral("height")).toInt()));
    out->set_thumbnail_path(str(media, "thumbnail_path"));
    out->set_path(str(media, "path"));
    out->set_download_error(str(media, "download_error"));
    out->set_downloading(media.value(QStringLiteral("downloading")).toBool());
    out->set_size_bytes(
        static_cast<std::uint64_t>(media.value(QStringLiteral("size_bytes")).toVariant().toULongLong()));
    out->set_duration_ms(static_cast<std::int64_t>(media.value(QStringLiteral("duration_secs")).toInt())
                         * 1000);
}

} // namespace

bool v2MessageRowFromJson(const QJsonObject &item, whatevr::v2::MessageRow *out)
{
    if (!item.contains(QStringLiteral("id"))) {
        return false;
    }
    const auto str = [&](const char *key) {
        return item.value(QLatin1StringView(key)).toString().toStdString();
    };
    out->set_id(str("id"));
    out->set_chat_id(str("chat_id"));
    out->set_chat_name(str("chat_name"));
    const QJsonObject sender = item.value(QStringLiteral("sender")).toObject();
    out->mutable_sender()->set_id(sender.value(QStringLiteral("id")).toString().toStdString());
    out->mutable_sender()->set_name(sender.value(QStringLiteral("name")).toString().toStdString());
    out->mutable_sender()->set_avatar_path(
        sender.value(QStringLiteral("avatar_path")).toString().toStdString());
    out->set_from_me(item.value(QStringLiteral("direction")).toString()
                     == QLatin1String("outgoing"));
    out->set_t_ms(static_cast<std::int64_t>(item.value(QStringLiteral("timestamp")).toVariant().toLongLong())
                  * 1000);
    out->set_fallback(str("fallback"));
    out->set_text(str("text"));
    out->set_edited(item.value(QStringLiteral("edited")).toBool());
    out->set_revoked(item.value(QStringLiteral("revoked")).toBool());
    out->set_starred(item.value(QStringLiteral("starred")).toBool());
    out->set_viewed(item.value(QStringLiteral("viewed")).toBool());
    out->set_forwarded(item.value(QStringLiteral("forwarded")).toBool());
    out->set_kept(item.value(QStringLiteral("kept")).toBool());
    if (item.contains(QStringLiteral("reply_to"))) {
        const QJsonObject reply = item.value(QStringLiteral("reply_to")).toObject();
        auto *quote = out->mutable_reply_to();
        quote->set_message_id(reply.value(QStringLiteral("message_id")).toString().toStdString());
        quote->set_text(reply.value(QStringLiteral("text")).toString().toStdString());
    }
    for (const QJsonValue &entry : item.value(QStringLiteral("mentions")).toArray()) {
        const QJsonObject mention = entry.toObject();
        auto *target = out->add_mentions();
        // Fixtures name the field `jid`; the wire carries a person id.
        const QString jid = mention.value(QStringLiteral("jid")).toString();
        target->mutable_person()->set_id(
            (jid.isEmpty() ? mention.value(QStringLiteral("id")).toString() : jid).toStdString());
        target->mutable_person()->set_name(
            mention.value(QStringLiteral("name")).toString().toStdString());
    }
    for (const QJsonValue &entry : item.value(QStringLiteral("reactions")).toArray()) {
        const QJsonObject reaction = entry.toObject();
        auto *target = out->add_reactions();
        target->set_emoji(reaction.value(QStringLiteral("emoji")).toString().toStdString());
        target->mutable_sender()->set_id(
            reaction.value(QStringLiteral("sender_id")).toString().toStdString());
        target->mutable_sender()->set_name(
            reaction.value(QStringLiteral("sender_name")).toString().toStdString());
    }
    const QString kind = item.value(QStringLiteral("kind")).toString();
    if (kind == QLatin1String("image") || kind == QLatin1String("video")
        || kind == QLatin1String("gif") || kind == QLatin1String("voice")
        || kind == QLatin1String("audio") || kind == QLatin1String("document")) {
        v2MediaFromJson(item.value(QStringLiteral("media")).toObject(),
                        out->mutable_image()->mutable_media());
        if (kind != QLatin1String("image")) {
            // The arm names the kind; image is the structural stand-in only
            // when the fixture names one of its siblings, so move it over.
            whatevr::v2::Image image = out->image();
            out->clear_image();
            if (kind == QLatin1String("video")) {
                *out->mutable_video()->mutable_media() = image.media();
            } else if (kind == QLatin1String("gif")) {
                *out->mutable_gif()->mutable_media() = image.media();
            } else if (kind == QLatin1String("voice")) {
                *out->mutable_voice()->mutable_media() = image.media();
            } else if (kind == QLatin1String("audio")) {
                *out->mutable_audio()->mutable_media() = image.media();
            } else if (kind == QLatin1String("document")) {
                *out->mutable_document()->mutable_media() = image.media();
            }
        }
    } else if (kind == QLatin1String("location") && item.contains(QStringLiteral("location"))) {
        const QJsonObject location = item.value(QStringLiteral("location")).toObject();
        auto *target = out->mutable_location();
        target->set_lat(location.value(QStringLiteral("lat")).toDouble());
        target->set_lng(location.value(QStringLiteral("lng")).toDouble());
        target->set_name(location.value(QStringLiteral("name")).toString().toStdString());
        target->set_address(location.value(QStringLiteral("address")).toString().toStdString());
    } else if (kind == QLatin1String("poll") && item.contains(QStringLiteral("poll"))) {
        const QJsonObject poll = item.value(QStringLiteral("poll")).toObject();
        auto *target = out->mutable_poll();
        target->set_question(poll.value(QStringLiteral("question")).toString().toStdString());
        target->set_selectable(static_cast<std::uint32_t>(
            poll.value(QStringLiteral("selectable_count")).toInt()));
        for (const QJsonValue &entry : poll.value(QStringLiteral("options")).toArray()) {
            const QJsonObject option = entry.toObject();
            auto *targetOption = target->add_options();
            targetOption->set_index(static_cast<std::uint32_t>(option.value(QStringLiteral("index")).toInt()));
            targetOption->set_name(option.value(QStringLiteral("name")).toString().toStdString());
        }
    } else {
        out->mutable_text_body();
    }
    return true;
}

// The reverse direction, for test doubles: the fake daemon keeps its JSON
// core and only translates at the wire.
namespace
{

void setJsonAddress(QJsonObject &params, const char *key, const whatevr::v2::Address &address)
{
    // Person/chat ids round-trip through `id`; phone lookups through `phone`.
    if (!address.id().empty()) {
        params.insert(QLatin1StringView(key), v2s(address.id()));
    } else if (!address.phone().empty()) {
        params.insert(QLatin1StringView(key), v2s(address.phone()));
    }
}

} // namespace

bool v2RequestToV1(const whatevr::v2::Request &request, V2RequestV1 *out)
{
    using Method = whatevr::v2::Request::MethodCase;
    QJsonObject params;
    switch (request.method_case()) {
    case Method::kHello:
        out->method = QStringLiteral("hello");
        params.insert(QStringLiteral("client"), v2s(request.hello().client()));
        params.insert(QStringLiteral("protocol"), static_cast<qint64>(request.hello().protocol()));
        break;
    case Method::kSubscribe: {
        out->method = QStringLiteral("subscribe");
        const auto &subscribe = request.subscribe();
        if (subscribe.limit() > 0) {
            params.insert(QStringLiteral("limit"), static_cast<qint64>(subscribe.limit()));
        }
        using View = whatevr::v2::Subscribe::ViewCase;
        switch (subscribe.view_case()) {
        case View::kConnection:
            params.insert(QStringLiteral("view"), QStringLiteral("connection"));
            break;
        case View::kLogin:
            params.insert(QStringLiteral("view"), QStringLiteral("login"));
            break;
        case View::kSync:
            params.insert(QStringLiteral("view"), QStringLiteral("sync"));
            break;
        case View::kChats:
            params.insert(QStringLiteral("view"), QStringLiteral("chats"));
            switch (subscribe.chats().filter()) {
            case whatevr::v2::CHAT_FILTER_DIRECT:
                params.insert(QStringLiteral("filter"), QStringLiteral("direct"));
                break;
            case whatevr::v2::CHAT_FILTER_GROUPS:
                params.insert(QStringLiteral("filter"), QStringLiteral("groups"));
                break;
            case whatevr::v2::CHAT_FILTER_FAVORITE:
                params.insert(QStringLiteral("filter"), QStringLiteral("favorite"));
                break;
            default:
                params.insert(QStringLiteral("filter"), QStringLiteral("all"));
                break;
            }
            params.insert(QStringLiteral("archived"), subscribe.chats().archived());
            if (subscribe.chats().folder_id() > 0) {
                params.insert(QStringLiteral("folder_id"),
                              static_cast<qint64>(subscribe.chats().folder_id()));
            }
            break;
        case View::kChat:
            params.insert(QStringLiteral("view"), QStringLiteral("chat"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.chat().chat_id()));
            break;
        case View::kMessages: {
            params.insert(QStringLiteral("view"), QStringLiteral("messages"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.messages().chat_id()));
            using Anchor = whatevr::v2::MessagesView::AnchorCase;
            switch (subscribe.messages().anchor_case()) {
            case Anchor::kUnread:
                params.insert(QStringLiteral("anchor"), QStringLiteral("unread"));
                break;
            case Anchor::kMessageId:
                params.insert(QStringLiteral("anchor"), v2s(subscribe.messages().message_id()));
                break;
            default:
                params.insert(QStringLiteral("anchor"), QStringLiteral("latest"));
                break;
            }
            break;
        }
        case View::kTyping:
            params.insert(QStringLiteral("view"), QStringLiteral("typing"));
            break;
        case View::kPresence:
            params.insert(QStringLiteral("view"), QStringLiteral("presence"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.presence().chat_id()));
            break;
        case View::kReceipts:
            params.insert(QStringLiteral("view"), QStringLiteral("receipts"));
            params.insert(QStringLiteral("message_id"), v2s(subscribe.receipts().message_id()));
            break;
        case View::kSelf:
            params.insert(QStringLiteral("view"), QStringLiteral("self"));
            break;
        case View::kContact:
            params.insert(QStringLiteral("view"), QStringLiteral("contact"));
            setJsonAddress(params, "jid", subscribe.contact().person());
            break;
        case View::kGroup:
            params.insert(QStringLiteral("view"), QStringLiteral("group"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.group().chat_id()));
            break;
        case View::kGroupMembers:
            params.insert(QStringLiteral("view"), QStringLiteral("group_members"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.group_members().chat_id()));
            break;
        case View::kPrivacy:
            params.insert(QStringLiteral("view"), QStringLiteral("privacy"));
            break;
        case View::kPreferences:
            params.insert(QStringLiteral("view"), QStringLiteral("preferences"));
            break;
        case View::kBlocklist:
            params.insert(QStringLiteral("view"), QStringLiteral("blocklist"));
            break;
        case View::kStarred:
            params.insert(QStringLiteral("view"), QStringLiteral("starred"));
            if (!subscribe.starred().chat_id().empty()) {
                params.insert(QStringLiteral("chat_id"), v2s(subscribe.starred().chat_id()));
            }
            break;
        case View::kPinned:
            params.insert(QStringLiteral("view"), QStringLiteral("pinned"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.pinned().chat_id()));
            break;
        case View::kLiveLocations:
            params.insert(QStringLiteral("view"), QStringLiteral("live_locations"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.live_locations().chat_id()));
            break;
        case View::kChatMedia:
            params.insert(QStringLiteral("view"), QStringLiteral("chat_media"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.chat_media().chat_id()));
            break;
        case View::kChatLinks:
            params.insert(QStringLiteral("view"), QStringLiteral("chat_links"));
            params.insert(QStringLiteral("chat_id"), v2s(subscribe.chat_links().chat_id()));
            break;
        case View::kChatFolders:
            params.insert(QStringLiteral("view"), QStringLiteral("chat_folders"));
            break;
        case View::kStatus:
            params.insert(QStringLiteral("view"), QStringLiteral("status"));
            break;
        case View::kStatusMuted:
            params.insert(QStringLiteral("view"), QStringLiteral("status.muted"));
            break;
        case View::kStickers:
            params.insert(QStringLiteral("view"), QStringLiteral("stickers"));
            switch (subscribe.stickers().source()) {
            case whatevr::v2::STICKER_SOURCE_RECENT:
                params.insert(QStringLiteral("source"), QStringLiteral("recent"));
                break;
            case whatevr::v2::STICKER_SOURCE_FAVORITE:
                params.insert(QStringLiteral("source"), QStringLiteral("favorite"));
                break;
            default:
                params.insert(QStringLiteral("source"), QStringLiteral("all"));
                break;
            }
            break;
        case View::kStickerPacks:
            params.insert(QStringLiteral("view"), QStringLiteral("sticker_packs"));
            break;
        case View::kStickerPack:
            params.insert(QStringLiteral("view"), QStringLiteral("sticker_pack"));
            params.insert(QStringLiteral("pack_id"), v2s(subscribe.sticker_pack().pack_id()));
            break;
        case View::kTransfers:
            params.insert(QStringLiteral("view"), QStringLiteral("transfers"));
            break;
        case View::kLogs:
            params.insert(QStringLiteral("view"), QStringLiteral("daemon.logs"));
            break;
        default:
            return false;
        }
        break;
    }
    case Method::kExtend: {
        out->method = QStringLiteral("extend");
        params.insert(QStringLiteral("sub"), static_cast<qint64>(request.extend().sub()));
        params.insert(QStringLiteral("count"), static_cast<qint64>(request.extend().count()));
        params.insert(QStringLiteral("direction"),
                      request.extend().direction() == whatevr::v2::DIRECTION_NEWER
                          ? QStringLiteral("newer")
                          : QStringLiteral("older"));
        break;
    }
    case Method::kUnsubscribe:
        out->method = QStringLiteral("unsubscribe");
        params.insert(QStringLiteral("sub"), static_cast<qint64>(request.unsubscribe().sub()));
        break;
    case Method::kSessionUpdate:
        out->method = QStringLiteral("session.update");
        params.insert(QStringLiteral("focused"), request.session_update().focused());
        params.insert(QStringLiteral("active_chat_id"),
                      v2s(request.session_update().active_chat_id()));
        break;
    case Method::kDaemonBackupExport:
        out->method = QStringLiteral("daemon.backup_export");
        params.insert(QStringLiteral("path"), v2s(request.daemon_backup_export().path()));
        params.insert(QStringLiteral("passphrase"), v2s(request.daemon_backup_export().passphrase()));
        params.insert(QStringLiteral("use_keyring"), request.daemon_backup_export().use_keyring());
        break;
    case Method::kDaemonBackupSetPassphrase:
        out->method = QStringLiteral("daemon.backup_set_passphrase");
        params.insert(QStringLiteral("passphrase"),
                      v2s(request.daemon_backup_set_passphrase().passphrase()));
        break;
    case Method::kDaemonShutdown:
        out->method = QStringLiteral("daemon.shutdown");
        break;
    case Method::kDaemonReconnect:
        out->method = QStringLiteral("daemon.reconnect");
        break;
    case Method::kLogMessage:
        out->method = QStringLiteral("daemon.log");
        params.insert(QStringLiteral("message"), v2s(request.log_message().message()));
        break;
    case Method::kAccountLogout:
        out->method = QStringLiteral("account.logout");
        break;
    case Method::kChatMarkRead: {
        out->method = QStringLiteral("chat.mark_read");
        const auto &mark = request.chat_mark_read();
        params.insert(QStringLiteral("chat_id"), v2s(mark.chat_id()));
        params.insert(QStringLiteral("up_to_message_id"), v2s(mark.up_to_message_id()));
        break;
    }
    case Method::kChatMarkAllRead:
        out->method = QStringLiteral("chat.mark_all_read");
        break;
    case Method::kChatFolderCreate:
        out->method = QStringLiteral("chat_folder.create");
        params.insert(QStringLiteral("name"), v2s(request.chat_folder_create().name()));
        break;
    case Method::kChatFolderRename:
        out->method = QStringLiteral("chat_folder.rename");
        params.insert(QStringLiteral("id"), static_cast<qint64>(request.chat_folder_rename().id()));
        params.insert(QStringLiteral("name"), v2s(request.chat_folder_rename().name()));
        break;
    case Method::kChatFolderDelete:
        out->method = QStringLiteral("chat_folder.delete");
        params.insert(QStringLiteral("id"), static_cast<qint64>(request.chat_folder_delete().id()));
        break;
    case Method::kChatFolderSetChat: {
        out->method = QStringLiteral("chat_folder.set_chat");
        const auto &set = request.chat_folder_set_chat();
        params.insert(QStringLiteral("chat_id"), v2s(set.chat_id()));
        if (set.folder_id() > 0) {
            params.insert(QStringLiteral("folder_id"), static_cast<qint64>(set.folder_id()));
        }
        break;
    }
    case Method::kChatExport:
        out->method = QStringLiteral("chat.export");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_export().chat_id()));
        params.insert(QStringLiteral("path"), v2s(request.chat_export().path()));
        break;
    case Method::kChatPin:
        out->method = QStringLiteral("chat.pin");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_pin().chat_id()));
        params.insert(QStringLiteral("pinned"), request.chat_pin().pinned());
        break;
    case Method::kChatFavorite:
        out->method = QStringLiteral("chat.favorite");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_favorite().chat_id()));
        params.insert(QStringLiteral("favorite"), request.chat_favorite().favorite());
        break;
    case Method::kChatArchive:
        out->method = QStringLiteral("chat.archive");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_archive().chat_id()));
        params.insert(QStringLiteral("archived"), request.chat_archive().archived());
        break;
    case Method::kChatMute:
        out->method = QStringLiteral("chat.mute");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_mute().chat_id()));
        params.insert(QStringLiteral("muted"), request.chat_mute().muted());
        params.insert(QStringLiteral("duration_secs"),
                      static_cast<qint64>(request.chat_mute().duration_ms() / 1000));
        break;
    case Method::kChatTyping:
        out->method = QStringLiteral("chat.typing");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_typing().chat_id()));
        params.insert(QStringLiteral("composing"), request.chat_typing().composing());
        break;
    case Method::kChatRequestOlder:
        out->method = QStringLiteral("chat.request_older");
        params.insert(QStringLiteral("chat_id"), v2s(request.chat_request_older().chat_id()));
        break;
    case Method::kChatEnsureDirect:
        out->method = QStringLiteral("chat.ensure_direct");
        setJsonAddress(params, "jid", request.chat_ensure_direct().person());
        break;
    case Method::kSendText: {
        out->method = QStringLiteral("send.text");
        const auto &send = request.send_text();
        params.insert(QStringLiteral("chat_id"), v2s(send.chat_id()));
        params.insert(QStringLiteral("text"), v2s(send.text()));
        if (!send.reply_to().empty()) {
            params.insert(QStringLiteral("reply_to"), v2s(send.reply_to()));
        }
        if (!send.mentions().empty()) {
            QJsonArray mentions;
            for (const auto &mention : send.mentions()) {
                mentions.append(v2s(mention.id()));
            }
            params.insert(QStringLiteral("mentions"), mentions);
        }
        break;
    }
    case Method::kSendMedia: {
        out->method = QStringLiteral("send.media");
        const auto &send = request.send_media();
        params.insert(QStringLiteral("chat_id"), v2s(send.chat_id()));
        params.insert(QStringLiteral("path"), v2s(send.path()));
        params.insert(QStringLiteral("caption"), v2s(send.caption()));
        if (send.as_document()) {
            params.insert(QStringLiteral("kind"), QStringLiteral("document"));
        }
        if (send.view_once()) {
            params.insert(QStringLiteral("view_once"), true);
        }
        break;
    }
    case Method::kSendSticker:
        out->method = QStringLiteral("send.sticker");
        params.insert(QStringLiteral("chat_id"), v2s(request.send_sticker().chat_id()));
        params.insert(QStringLiteral("cache_key"), v2s(request.send_sticker().sticker_id()));
        if (!request.send_sticker().reply_to().empty()) {
            params.insert(QStringLiteral("reply_to"), v2s(request.send_sticker().reply_to()));
        }
        break;
    case Method::kSendPoll: {
        out->method = QStringLiteral("send.poll");
        const auto &send = request.send_poll();
        params.insert(QStringLiteral("chat_id"), v2s(send.chat_id()));
        params.insert(QStringLiteral("question"), v2s(send.question()));
        QJsonArray options;
        for (const auto &option : send.options()) {
            options.append(v2s(option));
        }
        params.insert(QStringLiteral("options"), options);
        params.insert(QStringLiteral("multi"), send.multi());
        if (!send.reply_to().empty()) {
            params.insert(QStringLiteral("reply_to"), v2s(send.reply_to()));
        }
        break;
    }
    case Method::kSendContact: {
        out->method = QStringLiteral("send.contact");
        const auto &send = request.send_contact();
        params.insert(QStringLiteral("chat_id"), v2s(send.chat_id()));
        params.insert(QStringLiteral("name"), v2s(send.name()));
        params.insert(QStringLiteral("phone"), v2s(send.phone()));
        if (!send.reply_to().empty()) {
            params.insert(QStringLiteral("reply_to"), v2s(send.reply_to()));
        }
        break;
    }
    case Method::kSendLocation: {
        out->method = QStringLiteral("send.location");
        const auto &send = request.send_location();
        params.insert(QStringLiteral("chat_id"), v2s(send.chat_id()));
        params.insert(QStringLiteral("lat"), send.lat());
        params.insert(QStringLiteral("long"), send.lng());
        params.insert(QStringLiteral("name"), v2s(send.name()));
        params.insert(QStringLiteral("address"), v2s(send.address()));
        if (!send.reply_to().empty()) {
            params.insert(QStringLiteral("reply_to"), v2s(send.reply_to()));
        }
        break;
    }
    case Method::kSendCancel:
        out->method = QStringLiteral("send.cancel");
        params.insert(QStringLiteral("message_id"), v2s(request.send_cancel().message_id()));
        break;
    case Method::kGroupCreate: {
        out->method = QStringLiteral("group.create");
        const auto &create = request.group_create();
        params.insert(QStringLiteral("name"), v2s(create.name()));
        QJsonArray members;
        for (const auto &member : create.members()) {
            members.append(v2s(member));
        }
        params.insert(QStringLiteral("members"), members);
        params.insert(QStringLiteral("photo_path"), v2s(create.photo_path()));
        break;
    }
    case Method::kGroupLeave:
        out->method = QStringLiteral("group.leave");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_leave().chat_id()));
        break;
    case Method::kGroupSetName:
        out->method = QStringLiteral("group.set_name");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_set_name().chat_id()));
        params.insert(QStringLiteral("name"), v2s(request.group_set_name().name()));
        break;
    case Method::kGroupSetTopic:
        out->method = QStringLiteral("group.set_topic");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_set_topic().chat_id()));
        params.insert(QStringLiteral("description"), v2s(request.group_set_topic().description()));
        break;
    case Method::kGroupSetPhoto:
        out->method = QStringLiteral("group.set_photo");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_set_photo().chat_id()));
        params.insert(QStringLiteral("path"), v2s(request.group_set_photo().path()));
        break;
    case Method::kGroupInviteLink:
        out->method = QStringLiteral("group.invite_link");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_invite_link().chat_id()));
        if (request.group_invite_link().reset()) {
            params.insert(QStringLiteral("reset"), true);
        }
        break;
    case Method::kGroupMembers: {
        out->method = QStringLiteral("group.members");
        const auto &members = request.group_members();
        params.insert(QStringLiteral("chat_id"), v2s(members.chat_id()));
        params.insert(QStringLiteral("action"), v2s(members.action()));
        QJsonArray ids;
        for (const auto &member : members.members()) {
            ids.append(v2s(member));
        }
        params.insert(QStringLiteral("members"), ids);
        break;
    }
    case Method::kGroupSetAnnounce:
        out->method = QStringLiteral("group.set_announce");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_set_announce().chat_id()));
        params.insert(QStringLiteral("enabled"), request.group_set_announce().enabled());
        break;
    case Method::kGroupSetLocked:
        out->method = QStringLiteral("group.set_locked");
        params.insert(QStringLiteral("chat_id"), v2s(request.group_set_locked().chat_id()));
        params.insert(QStringLiteral("enabled"), request.group_set_locked().enabled());
        break;
    case Method::kCommunityLink:
        out->method = QStringLiteral("community.link");
        params.insert(QStringLiteral("community_id"), v2s(request.community_link().community_id()));
        params.insert(QStringLiteral("group_id"), v2s(request.community_link().group_id()));
        break;
    case Method::kCommunityUnlink:
        out->method = QStringLiteral("community.unlink");
        params.insert(QStringLiteral("community_id"), v2s(request.community_unlink().community_id()));
        params.insert(QStringLiteral("group_id"), v2s(request.community_unlink().group_id()));
        break;
    case Method::kScheduleText:
        out->method = QStringLiteral("schedule.text");
        params.insert(QStringLiteral("chat_id"), v2s(request.schedule_text().chat_id()));
        params.insert(QStringLiteral("text"), v2s(request.schedule_text().text()));
        params.insert(QStringLiteral("send_at"),
                      static_cast<qint64>(request.schedule_text().send_at()));
        break;
    case Method::kScheduleList:
        out->method = QStringLiteral("schedule.list");
        params.insert(QStringLiteral("chat_id"), v2s(request.schedule_list().chat_id()));
        break;
    case Method::kScheduleCancel:
        out->method = QStringLiteral("schedule.cancel");
        params.insert(QStringLiteral("id"),
                      static_cast<qint64>(request.schedule_cancel().id()));
        break;
    case Method::kMessageReact:
        out->method = QStringLiteral("message.react");
        params.insert(QStringLiteral("message_id"), v2s(request.message_react().message_id()));
        params.insert(QStringLiteral("emoji"), v2s(request.message_react().emoji()));
        break;
    case Method::kMessageEditHistory:
        out->method = QStringLiteral("message.edit_history");
        params.insert(QStringLiteral("message_id"), v2s(request.message_edit_history().message_id()));
        break;
    case Method::kMessageEdit:
        out->method = QStringLiteral("message.edit");
        params.insert(QStringLiteral("message_id"), v2s(request.message_edit().message_id()));
        params.insert(QStringLiteral("text"), v2s(request.message_edit().text()));
        break;
    case Method::kMessageRevoke:
        out->method = QStringLiteral("message.revoke");
        params.insert(QStringLiteral("message_id"), v2s(request.message_revoke().message_id()));
        break;
    case Method::kMessageDelete:
        out->method = QStringLiteral("message.delete");
        params.insert(QStringLiteral("message_id"), v2s(request.message_delete().message_id()));
        break;
    case Method::kMessageStar:
        out->method = QStringLiteral("message.star");
        params.insert(QStringLiteral("message_id"), v2s(request.message_star().message_id()));
        params.insert(QStringLiteral("starred"), request.message_star().starred());
        break;
    case Method::kMessagePin:
        out->method = QStringLiteral("message.pin");
        params.insert(QStringLiteral("message_id"), v2s(request.message_pin().message_id()));
        params.insert(QStringLiteral("pinned"), request.message_pin().pinned());
        if (request.message_pin().duration_ms() > 0) {
            params.insert(QStringLiteral("duration_secs"),
                          static_cast<qint64>(request.message_pin().duration_ms() / 1000));
        }
        break;
    case Method::kMessageForward: {
        out->method = QStringLiteral("message.forward");
        params.insert(QStringLiteral("message_id"), v2s(request.message_forward().message_id()));
        QJsonArray chats;
        for (const auto &chatId : request.message_forward().chat_ids()) {
            chats.append(v2s(chatId));
        }
        params.insert(QStringLiteral("chat_ids"), chats);
        break;
    }
    case Method::kMessageMarkPlayed:
        out->method = QStringLiteral("message.mark_played");
        params.insert(QStringLiteral("message_id"), v2s(request.message_mark_played().message_id()));
        break;
    case Method::kMessageRequestFromPhone:
        out->method = QStringLiteral("message.request_from_phone");
        params.insert(QStringLiteral("message_id"),
                      v2s(request.message_request_from_phone().message_id()));
        break;
    case Method::kPollVote: {
        out->method = QStringLiteral("poll.vote");
        params.insert(QStringLiteral("message_id"), v2s(request.poll_vote().message_id()));
        QJsonArray options;
        for (const auto option : request.poll_vote().option_indexes()) {
            options.append(static_cast<qint64>(option));
        }
        params.insert(QStringLiteral("option_ids"), options);
        break;
    }
    case Method::kEventRsvp: {
        out->method = QStringLiteral("event.rsvp");
        params.insert(QStringLiteral("message_id"), v2s(request.event_rsvp().message_id()));
        switch (request.event_rsvp().response()) {
        case whatevr::v2::RSVP_GOING:
            params.insert(QStringLiteral("response"), QStringLiteral("going"));
            break;
        case whatevr::v2::RSVP_NOT_GOING:
            params.insert(QStringLiteral("response"), QStringLiteral("not_going"));
            break;
        case whatevr::v2::RSVP_MAYBE:
            params.insert(QStringLiteral("response"), QStringLiteral("maybe"));
            break;
        default:
            break;
        }
        params.insert(QStringLiteral("extra_guests"),
                      static_cast<qint64>(request.event_rsvp().extra_guests()));
        break;
    }
    case Method::kGroupJoinInvite:
        out->method = QStringLiteral("group.join_invite");
        params.insert(QStringLiteral("message_id"), v2s(request.group_join_invite().message_id()));
        break;
    case Method::kMediaDownload:
        out->method = QStringLiteral("media.download");
        params.insert(QStringLiteral("message_id"), v2s(request.media_download().message_id()));
        break;
    case Method::kMediaCancelDownload:
        out->method = QStringLiteral("media.cancel_download");
        params.insert(QStringLiteral("message_id"),
                      v2s(request.media_cancel_download().message_id()));
        break;
    case Method::kMediaStream:
        out->method = QStringLiteral("media.stream");
        params.insert(QStringLiteral("message_id"), v2s(request.media_stream().message_id()));
        break;
    case Method::kMediaFetchProfilePicture:
        out->method = QStringLiteral("media.fetch_profile_picture");
        setJsonAddress(params, "jid", request.media_fetch_profile_picture().person());
        break;
    case Method::kMediaSave: {
        out->method = QStringLiteral("media.save");
        const auto &save = request.media_save();
        if (!save.message_id().empty()) {
            params.insert(QStringLiteral("message_id"), v2s(save.message_id()));
        }
        if (!save.status_id().empty()) {
            params.insert(QStringLiteral("status_id"), v2s(save.status_id()));
        }
        if (!save.jid().empty()) {
            params.insert(QStringLiteral("jid"), v2s(save.jid()));
        }
        params.insert(QStringLiteral("path"), v2s(save.path()));
        break;
    }
    case Method::kContactBlock:
        out->method = QStringLiteral("contact.block");
        setJsonAddress(params, "jid", request.contact_block().person());
        params.insert(QStringLiteral("blocked"), request.contact_block().blocked());
        break;
    case Method::kSelfSetAbout:
        out->method = QStringLiteral("self.set_about");
        params.insert(QStringLiteral("text"), v2s(request.self_set_about().text()));
        break;
    case Method::kPrivacySet: {
        out->method = QStringLiteral("privacy.set");
        switch (request.privacy_set().category()) {
        case whatevr::v2::PRIVACY_CATEGORY_LAST_SEEN:
            params.insert(QStringLiteral("category"), QStringLiteral("last_seen"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_ONLINE:
            params.insert(QStringLiteral("category"), QStringLiteral("online"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_PROFILE_PHOTO:
            params.insert(QStringLiteral("category"), QStringLiteral("profile_photo"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_ABOUT:
            params.insert(QStringLiteral("category"), QStringLiteral("about"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_GROUP_ADD:
            params.insert(QStringLiteral("category"), QStringLiteral("group_add"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_CALL_ADD:
            params.insert(QStringLiteral("category"), QStringLiteral("call_add"));
            break;
        case whatevr::v2::PRIVACY_CATEGORY_READ_RECEIPTS:
            params.insert(QStringLiteral("category"), QStringLiteral("read_receipts"));
            break;
        default:
            return false;
        }
        params.insert(QStringLiteral("value"), v2PrivacyValue(request.privacy_set().value()));
        break;
    }
    case Method::kPrivacySetDefaultTimer:
        out->method = QStringLiteral("privacy.set_default_timer");
        params.insert(QStringLiteral("seconds"),
                      static_cast<qint64>(request.privacy_set_default_timer().seconds()));
        break;
    case Method::kPreferencesSet: {
        out->method = QStringLiteral("preferences.set");
        const auto &set = request.preferences_set();
        if (set.has_notifications()) {
            params.insert(QStringLiteral("notifications_enabled"), set.notifications());
        }
        if (set.has_notification_sound()) {
            params.insert(QStringLiteral("notification_sound"), set.notification_sound());
        }
        if (set.has_notification_preview()) {
            params.insert(QStringLiteral("notification_preview"), set.notification_preview());
        }
        if (set.has_auto_download_photos()) {
            params.insert(QStringLiteral("auto_download_photos"), set.auto_download_photos());
        }
        if (set.has_auto_download_videos()) {
            params.insert(QStringLiteral("auto_download_videos"), set.auto_download_videos());
        }
        if (set.has_auto_download_audio()) {
            params.insert(QStringLiteral("auto_download_audio"), set.auto_download_audio());
        }
        if (set.has_auto_download_documents()) {
            params.insert(QStringLiteral("auto_download_documents"), set.auto_download_documents());
        }
        if (set.has_auto_download_stickers()) {
            params.insert(QStringLiteral("auto_download_stickers"), set.auto_download_stickers());
        }
        if (set.has_auto_download_max_bytes()) {
            params.insert(QStringLiteral("auto_download_max_bytes"),
                          static_cast<qint64>(set.auto_download_max_bytes()));
        }
        if (set.has_mute_archived_chats()) {
            params.insert(QStringLiteral("mute_archived_chats"), set.mute_archived_chats());
        }
        if (set.has_anti_delete()) {
            params.insert(QStringLiteral("anti_delete"), set.anti_delete());
        }
        if (set.has_keep_chats_archived()) {
            params.insert(QStringLiteral("keep_chats_archived"), set.keep_chats_archived());
        }
        break;
    }
    case Method::kSearchChats:
        out->method = QStringLiteral("search.chats");
        params.insert(QStringLiteral("query"), v2s(request.search_chats().query()));
        break;
    case Method::kSearchMessages:
        out->method = QStringLiteral("search.messages");
        params.insert(QStringLiteral("query"), v2s(request.search_messages().query()));
        if (!request.search_messages().chat_id().empty()) {
            params.insert(QStringLiteral("chat_id"), v2s(request.search_messages().chat_id()));
        }
        if (request.search_messages().limit() > 0) {
            params.insert(QStringLiteral("limit"),
                          static_cast<qint64>(request.search_messages().limit()));
        }
        break;
    case Method::kSearchStickers:
        out->method = QStringLiteral("search.stickers");
        params.insert(QStringLiteral("query"), v2s(request.search_stickers().query()));
        if (request.search_stickers().limit() > 0) {
            params.insert(QStringLiteral("limit"),
                          static_cast<qint64>(request.search_stickers().limit()));
        }
        break;
    case Method::kContactCheckPhone:
        out->method = QStringLiteral("contacts.check_phone");
        params.insert(QStringLiteral("phone"), v2s(request.contact_check_phone().phone()));
        break;
    case Method::kStickerFavorite: {
        out->method = QStringLiteral("sticker.favorite");
        const auto &favorite = request.sticker_favorite();
        if (favorite.has_sticker_id()) {
            params.insert(QStringLiteral("cache_key"), v2s(favorite.sticker_id()));
        } else if (favorite.has_message_id()) {
            params.insert(QStringLiteral("message_id"), v2s(favorite.message_id()));
        }
        params.insert(QStringLiteral("favorite"), favorite.favorite());
        break;
    }
    case Method::kStickerDownload:
        out->method = QStringLiteral("sticker.download");
        params.insert(QStringLiteral("cache_key"), v2s(request.sticker_download().sticker_id()));
        break;
    case Method::kStickerPackInstall:
        out->method = QStringLiteral("sticker_pack.install");
        params.insert(QStringLiteral("pack_id"), v2s(request.sticker_pack_install().pack_id()));
        params.insert(QStringLiteral("installed"), request.sticker_pack_install().installed());
        break;
    case Method::kStickerPacksRefresh:
        out->method = QStringLiteral("sticker_packs.refresh");
        break;
    default:
        return false;
    }
    out->params = params;
    return true;
}

namespace
{

QJsonObject translateV2Person(const whatevr::v2::Person &person)
{
    QJsonObject sender;
    sender.insert(QStringLiteral("id"), v2s(person.id()));
    sender.insert(QStringLiteral("name"), v2s(person.name()));
    if (!person.avatar_path().empty()) {
        sender.insert(QStringLiteral("avatar_path"), v2s(person.avatar_path()));
    }
    return sender;
}

QJsonObject translateV2MediaBody(const whatevr::v2::Media &media)
{
    QJsonObject out;
    if (!media.mime().empty()) {
        out.insert(QStringLiteral("mime"), v2s(media.mime()));
    }
    if (media.width() > 0) {
        out.insert(QStringLiteral("width"), static_cast<qint64>(media.width()));
    }
    if (media.height() > 0) {
        out.insert(QStringLiteral("height"), static_cast<qint64>(media.height()));
    }
    if (!media.thumbnail_path().empty()) {
        out.insert(QStringLiteral("thumbnail_path"), v2s(media.thumbnail_path()));
    }
    if (!media.path().empty()) {
        out.insert(QStringLiteral("path"), v2s(media.path()));
    }
    if (!media.download_error().empty()) {
        out.insert(QStringLiteral("download_error"), v2s(media.download_error()));
    }
    if (media.downloading()) {
        out.insert(QStringLiteral("downloading"), true);
    }
    if (media.size_bytes() > 0) {
        out.insert(QStringLiteral("size_bytes"), static_cast<qint64>(media.size_bytes()));
    }
    if (media.duration_ms() > 0) {
        out.insert(QStringLiteral("duration_secs"),
                   static_cast<qint64>(media.duration_ms() / 1000));
    }
    return out;
}

QJsonObject translateV2LocationBody(const whatevr::v2::Location &location)
{
    QJsonObject out;
    out.insert(QStringLiteral("lat"), location.lat());
    out.insert(QStringLiteral("lng"), location.lng());
    if (!location.name().empty()) {
        out.insert(QStringLiteral("name"), v2s(location.name()));
    }
    if (!location.address().empty()) {
        out.insert(QStringLiteral("address"), v2s(location.address()));
    }
    if (!location.url().empty()) {
        out.insert(QStringLiteral("url"), v2s(location.url()));
    }
    return out;
}

QJsonObject translateV2PollBody(const whatevr::v2::Poll &poll)
{
    QJsonObject out;
    if (!poll.question().empty()) {
        out.insert(QStringLiteral("question"), v2s(poll.question()));
    }
    if (poll.selectable() > 0) {
        out.insert(QStringLiteral("selectable_count"), static_cast<qint64>(poll.selectable()));
    }
    if (poll.quiz()) {
        out.insert(QStringLiteral("quiz"), true);
    }
    if (poll.allow_add_option()) {
        out.insert(QStringLiteral("allow_add_option"), true);
    }
    if (poll.ends_ms() > 0) {
        out.insert(QStringLiteral("ends_at"), static_cast<qint64>(poll.ends_ms() / 1000));
    }
    QJsonArray options;
    for (const auto &option : poll.options()) {
        QJsonObject opt;
        opt.insert(QStringLiteral("index"), static_cast<qint64>(option.index()));
        opt.insert(QStringLiteral("name"), v2s(option.name()));
        if (option.self_voted()) {
            opt.insert(QStringLiteral("self_voted"), true);
        }
        QJsonArray voters;
        for (const auto &voter : option.voters()) {
            QJsonObject vote;
            vote.insert(QStringLiteral("name"), v2s(voter.person().name()));
            vote.insert(QStringLiteral("timestamp"),
                       static_cast<qint64>(voter.t_ms() / 1000));
            voters.append(vote);
        }
        if (!voters.isEmpty()) {
            opt.insert(QStringLiteral("voters"), voters);
        }
        options.append(opt);
    }
    out.insert(QStringLiteral("options"), options);
    if (poll.voters() > 0) {
        out.insert(QStringLiteral("total_voters"), static_cast<qint64>(poll.voters()));
    }
    if (poll.self_voted()) {
        out.insert(QStringLiteral("self_voted"), true);
    }
    return out;
}

QJsonObject translateV2ContactsBody(const whatevr::v2::Contacts &contacts)
{
    QJsonObject out;
    if (!contacts.display_name().empty()) {
        out.insert(QStringLiteral("display_name"), v2s(contacts.display_name()));
    }
    QJsonArray cards;
    for (const auto &card : contacts.cards()) {
        QJsonObject entry;
        entry.insert(QStringLiteral("display_name"), v2s(card.display_name()));
        if (!card.org().empty()) {
            entry.insert(QStringLiteral("org"), v2s(card.org()));
        }
        QJsonArray phones;
        for (const auto &phone : card.phones()) {
            QJsonObject field;
            field.insert(QStringLiteral("label"), v2s(phone.label()));
            field.insert(QStringLiteral("value"), v2s(phone.value()));
            phones.append(field);
        }
        if (!phones.isEmpty()) {
            entry.insert(QStringLiteral("phones"), phones);
        }
        if (!card.vcard().empty()) {
            entry.insert(QStringLiteral("vcard"), v2s(card.vcard()));
        }
        cards.append(entry);
    }
    out.insert(QStringLiteral("cards"), cards);
    return out;
}

QJsonObject translateV2EventBody(const whatevr::v2::ScheduledEvent &event)
{
    QJsonObject out;
    if (!event.name().empty()) {
        out.insert(QStringLiteral("name"), v2s(event.name()));
    }
    if (!event.description().empty()) {
        out.insert(QStringLiteral("description"), v2s(event.description()));
    }
    if (event.starts_ms() > 0) {
        out.insert(QStringLiteral("starts_at"), static_cast<qint64>(event.starts_ms() / 1000));
    }
    if (event.ends_ms() > 0) {
        out.insert(QStringLiteral("ends_at"), static_cast<qint64>(event.ends_ms() / 1000));
    }
    if (event.canceled()) {
        out.insert(QStringLiteral("canceled"), true);
    }
    if (!event.join_link().empty()) {
        out.insert(QStringLiteral("join_link"), v2s(event.join_link()));
    }
    if (event.has_location()) {
        out.insert(QStringLiteral("location"), translateV2LocationBody(event.location()));
    }
    return out;
}

QJsonObject translateV2GroupInviteBody(const whatevr::v2::GroupInvite &invite)
{
    QJsonObject out;
    if (!invite.chat_id().empty()) {
        out.insert(QStringLiteral("group_jid"), v2s(invite.chat_id()));
    }
    if (!invite.code().empty()) {
        out.insert(QStringLiteral("code"), v2s(invite.code()));
    }
    if (invite.expires_ms() > 0) {
        out.insert(QStringLiteral("expires_at"),
                   static_cast<qint64>(invite.expires_ms() / 1000));
    }
    if (!invite.name().empty()) {
        out.insert(QStringLiteral("name"), v2s(invite.name()));
    }
    if (!invite.caption().empty()) {
        out.insert(QStringLiteral("caption"), v2s(invite.caption()));
    }
    return out;
}

// Message body arm to (v1 kind, v1 body object). Kinds without a v1
// counterpart keep an empty kind so the row renders its fallback.
QPair<QString, QJsonObject> translateV2MessageBody(const whatevr::v2::MessageRow &row)
{
    using Body = whatevr::v2::MessageRow::BodyCase;
    switch (row.body_case()) {
    case Body::kTextBody: {
        if (row.text_body().has_link_preview()) {
            const auto &preview = row.text_body().link_preview();
            QJsonObject card;
            card.insert(QStringLiteral("url"), v2s(preview.url()));
            if (!preview.title().empty()) {
                card.insert(QStringLiteral("title"), v2s(preview.title()));
            }
            if (!preview.description().empty()) {
                card.insert(QStringLiteral("description"), v2s(preview.description()));
            }
            QJsonObject body;
            body.insert(QStringLiteral("link_preview"), card);
            return {QStringLiteral("text"), body};
        }
        return {QStringLiteral("text"), QJsonObject{}};
    }
    case Body::kImage: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), translateV2MediaBody(row.image().media()));
        return {QStringLiteral("image"), wrap};
    }
    case Body::kVideo: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), translateV2MediaBody(row.video().media()));
        return {QStringLiteral("video"), wrap};
    }
    case Body::kGif: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), translateV2MediaBody(row.gif().media()));
        return {QStringLiteral("gif"), wrap};
    }
    case Body::kVoice: {
        QJsonObject media = translateV2MediaBody(row.voice().media());
        if (!row.voice().waveform().empty()) {
            QJsonArray waveform;
            for (const unsigned char bucket : row.voice().waveform()) {
                waveform.append(static_cast<int>(bucket));
            }
            media.insert(QStringLiteral("waveform"), waveform);
        }
        if (row.voice().played()) {
            media.insert(QStringLiteral("played"), true);
        }
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), media);
        return {QStringLiteral("voice"), wrap};
    }
    case Body::kAudio: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), translateV2MediaBody(row.audio().media()));
        return {QStringLiteral("audio"), wrap};
    }
    case Body::kDocument: {
        QJsonObject media = translateV2MediaBody(row.document().media());
        if (!row.document().filename().empty()) {
            media.insert(QStringLiteral("filename"), v2s(row.document().filename()));
        }
        if (row.document().page_count() > 0) {
            media.insert(QStringLiteral("page_count"),
                         static_cast<qint64>(row.document().page_count()));
        }
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), media);
        return {QStringLiteral("document"), wrap};
    }
    case Body::kVideoNote: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("media"), translateV2MediaBody(row.video_note().media()));
        return {QStringLiteral("video_note"), wrap};
    }
    case Body::kSticker: {
        QJsonObject sticker;
        sticker.insert(QStringLiteral("cache_key"), v2s(row.sticker().sticker_id()));
        if (row.sticker().has_media() && !row.sticker().media().path().empty()) {
            sticker.insert(QStringLiteral("path"), v2s(row.sticker().media().path()));
        }
        if (row.sticker().animated() || row.sticker().lottie()) {
            sticker.insert(QStringLiteral("is_animated"), true);
        }
        QJsonObject wrap;
        wrap.insert(QStringLiteral("sticker"), sticker);
        // The message model reads the library key off the top-level row for
        // favorite/download actions; the v2 sticker id is that key.
        wrap.insert(QStringLiteral("media_cache_key"), v2s(row.sticker().sticker_id()));
        return {QStringLiteral("sticker"), wrap};
    }
    case Body::kLocation: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("location"), translateV2LocationBody(row.location()));
        return {QStringLiteral("location"), wrap};
    }
    case Body::kLiveLocation: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("location"), translateV2LocationBody(row.live_location().location()));
        return {QStringLiteral("live_location"), wrap};
    }
    case Body::kContacts: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("contacts"), translateV2ContactsBody(row.contacts()));
        return {QStringLiteral("contacts"), wrap};
    }
    case Body::kPoll: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("poll"), translateV2PollBody(row.poll()));
        return {QStringLiteral("poll"), wrap};
    }
    case Body::kGroupInvite: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("invite"), translateV2GroupInviteBody(row.group_invite()));
        return {QStringLiteral("invite"), wrap};
    }
    case Body::kEvent: {
        QJsonObject wrap;
        wrap.insert(QStringLiteral("event"), translateV2EventBody(row.event()));
        return {QStringLiteral("event"), wrap};
    }
    case Body::kAlbum: {
        QJsonArray items;
        for (const auto &child : row.album().items()) {
            items.append(translateV2MessageRow(child));
        }
        QJsonObject album;
        album.insert(QStringLiteral("items"), items);
        QJsonObject wrap;
        wrap.insert(QStringLiteral("album"), album);
        return {QStringLiteral("album"), wrap};
    }
    case Body::kCallLog:
        return {QStringLiteral("call_log"), {}};
    case Body::kSystem:
        return {QStringLiteral("system"), {}};
    case Body::kWaiting:
        return {QStringLiteral("waiting"), {}};
    case Body::kUnsupported:
        return {QStringLiteral("unsupported"), {}};
    default:
        return {{}, {}};
    }
}

} // namespace

QJsonObject translateV2SelfRow(const whatevr::v2::SelfRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    item.insert(QStringLiteral("jid"), v2s(row.id()));
    if (!row.phone().empty()) {
        item.insert(QStringLiteral("phone"), v2s(row.phone()));
    }
    if (!row.push_name().empty()) {
        item.insert(QStringLiteral("push_name"), v2s(row.push_name()));
    }
    if (!row.about().empty()) {
        item.insert(QStringLiteral("about"), v2s(row.about()));
    }
    if (!row.avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.avatar_path()));
    }
    return item;
}

QJsonObject translateV2ContactRow(const whatevr::v2::ContactRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    item.insert(QStringLiteral("jid"), v2s(row.id()));
    if (!row.phone().empty()) {
        item.insert(QStringLiteral("phone"), v2s(row.phone()));
    }
    if (!row.saved_name().empty()) {
        item.insert(QStringLiteral("saved_name"), v2s(row.saved_name()));
    }
    if (!row.push_name().empty()) {
        item.insert(QStringLiteral("push_name"), v2s(row.push_name()));
    }
    if (!row.business_name().empty()) {
        item.insert(QStringLiteral("business_name"), v2s(row.business_name()));
    }
    if (row.business()) {
        item.insert(QStringLiteral("is_business"), true);
    }
    if (!row.about().empty()) {
        item.insert(QStringLiteral("about"), v2s(row.about()));
    }
    if (!row.avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.avatar_path()));
    }
    return item;
}

QJsonObject translateV2GroupRow(const whatevr::v2::GroupRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.chat_id()));
    if (!row.subject().empty()) {
        item.insert(QStringLiteral("subject"), v2s(row.subject()));
    }
    if (!row.description().empty()) {
        item.insert(QStringLiteral("description"), v2s(row.description()));
    }
    if (!row.avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.avatar_path()));
    }
    if (row.created_ms() > 0) {
        item.insert(QStringLiteral("created_unix"), static_cast<qint64>(row.created_ms() / 1000));
    }
    if (row.has_owner()) {
        item.insert(QStringLiteral("owner"), v2s(row.owner().id()));
    }
    item.insert(QStringLiteral("member_count"), static_cast<qint64>(row.member_count()));
    switch (row.my_role()) {
    case whatevr::v2::GROUP_ROLE_ADMIN:
        item.insert(QStringLiteral("my_role"), QStringLiteral("admin"));
        break;
    case whatevr::v2::GROUP_ROLE_SUPERADMIN:
        item.insert(QStringLiteral("my_role"), QStringLiteral("superadmin"));
        break;
    case whatevr::v2::GROUP_ROLE_MEMBER:
        item.insert(QStringLiteral("my_role"), QStringLiteral("member"));
        break;
    default:
        break;
    }
    if (row.announce()) {
        item.insert(QStringLiteral("announce"), true);
    }
    if (row.locked()) {
        item.insert(QStringLiteral("locked"), true);
    }
    return item;
}

QJsonObject translateV2GroupMemberRow(const whatevr::v2::GroupMemberRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.person().id()));
    item.insert(QStringLiteral("jid"), v2s(row.person().id()));
    if (!row.person().name().empty()) {
        item.insert(QStringLiteral("display_name"), v2s(row.person().name()));
    }
    if (!row.person().phone().empty()) {
        item.insert(QStringLiteral("phone"), v2s(row.person().phone()));
    }
    if (!row.person().avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.person().avatar_path()));
    }
    switch (row.role()) {
    case whatevr::v2::GROUP_ROLE_ADMIN:
        item.insert(QStringLiteral("role"), QStringLiteral("admin"));
        break;
    case whatevr::v2::GROUP_ROLE_SUPERADMIN:
        item.insert(QStringLiteral("role"), QStringLiteral("superadmin"));
        break;
    case whatevr::v2::GROUP_ROLE_LEFT:
        item.insert(QStringLiteral("role"), QStringLiteral("left"));
        break;
    case whatevr::v2::GROUP_ROLE_MEMBER:
    case whatevr::v2::GROUP_ROLE_UNSPECIFIED:
    default:
        item.insert(QStringLiteral("role"), QStringLiteral("member"));
        break;
    }
    return item;
}

QJsonObject translateV2PresenceRow(const whatevr::v2::PresenceRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.person().id()));
    switch (row.availability()) {
    case whatevr::v2::AVAILABILITY_ONLINE:
        item.insert(QStringLiteral("availability"), QStringLiteral("online"));
        break;
    case whatevr::v2::AVAILABILITY_OFFLINE:
        item.insert(QStringLiteral("availability"), QStringLiteral("offline"));
        break;
    default:
        item.insert(QStringLiteral("availability"), QStringLiteral("unknown"));
        break;
    }
    if (row.last_seen_ms() > 0) {
        item.insert(QStringLiteral("last_seen_unix"),
                    static_cast<qint64>(row.last_seen_ms() / 1000));
    }
    return item;
}

QJsonObject translateV2ReceiptRow(const whatevr::v2::ReceiptRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.person().id()));
    item.insert(QStringLiteral("name"), v2s(row.person().name()));
    if (!row.person().avatar_path().empty()) {
        item.insert(QStringLiteral("avatar_path"), v2s(row.person().avatar_path()));
    }
    if (row.delivered_ms() > 0) {
        item.insert(QStringLiteral("delivered_ts_unix"),
                    static_cast<qint64>(row.delivered_ms() / 1000));
    }
    if (row.read_ms() > 0) {
        item.insert(QStringLiteral("read_ts_unix"), static_cast<qint64>(row.read_ms() / 1000));
    }
    if (row.played_ms() > 0) {
        item.insert(QStringLiteral("played_ts_unix"),
                    static_cast<qint64>(row.played_ms() / 1000));
    }
    return item;
}

QJsonObject translateV2PrivacyRow(const whatevr::v2::PrivacyRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), QStringLiteral("self"));
    item.insert(QStringLiteral("last_seen"), v2PrivacyValue(row.last_seen()));
    item.insert(QStringLiteral("online"), v2PrivacyValue(row.online()));
    item.insert(QStringLiteral("profile_photo"), v2PrivacyValue(row.profile_photo()));
    item.insert(QStringLiteral("about"), v2PrivacyValue(row.about()));
    item.insert(QStringLiteral("group_add"), v2PrivacyValue(row.group_add()));
    item.insert(QStringLiteral("call_add"), v2PrivacyValue(row.call_add()));
    // group_add/call_add cross as their v2 enums; the settings page reads the
    // same vocabulary it sends.
    item.insert(QStringLiteral("read_receipts"), row.read_receipts());
    item.insert(QStringLiteral("default_timer_seconds"), static_cast<qint64>(row.default_timer_secs()));
    return item;
}

QJsonObject translateV2Preferences(const whatevr::v2::Preferences &prefs)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), QStringLiteral("self"));
    item.insert(QStringLiteral("notifications_enabled"), prefs.notifications());
    item.insert(QStringLiteral("notification_sound"), prefs.notification_sound());
    item.insert(QStringLiteral("notification_preview"), prefs.notification_preview());
    item.insert(QStringLiteral("auto_download_photos"), prefs.auto_download_photos());
    item.insert(QStringLiteral("auto_download_videos"), prefs.auto_download_videos());
    item.insert(QStringLiteral("auto_download_audio"), prefs.auto_download_audio());
    item.insert(QStringLiteral("auto_download_documents"), prefs.auto_download_documents());
    item.insert(QStringLiteral("auto_download_stickers"), prefs.auto_download_stickers());
    item.insert(QStringLiteral("auto_download_max_bytes"),
                static_cast<qint64>(prefs.auto_download_max_bytes()));
    item.insert(QStringLiteral("auto_fetch_maps"), prefs.auto_fetch_maps());
    item.insert(QStringLiteral("mute_archived_chats"), prefs.mute_archived_chats());
    item.insert(QStringLiteral("anti_delete"), prefs.anti_delete());
    item.insert(QStringLiteral("keep_chats_archived"), prefs.keep_chats_archived());
    return item;
}

QJsonObject translateV2BlocklistRow(const whatevr::v2::ContactRow &row)
{
    QJsonObject item = translateV2ContactRow(row);
    item.insert(QStringLiteral("name"), v2s(row.saved_name().empty() ? row.push_name()
                                                                      : row.saved_name()));
    return item;
}

QJsonObject translateV2SyncRow(const whatevr::v2::SyncRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), QStringLiteral("sync"));
    // v1 vocabulary (historySyncTypeString/historySyncPhaseString); v2 enums
    // are coarser, so map explicitly rather than lowercasing.
    switch (row.type()) {
    case whatevr::v2::SYNC_TYPE_INITIAL:
        item.insert(QStringLiteral("type"), QStringLiteral("initial_bootstrap"));
        break;
    case whatevr::v2::SYNC_TYPE_RECENT:
        item.insert(QStringLiteral("type"), QStringLiteral("recent"));
        break;
    case whatevr::v2::SYNC_TYPE_FULL:
        item.insert(QStringLiteral("type"), QStringLiteral("full"));
        break;
    case whatevr::v2::SYNC_TYPE_ON_DEMAND:
        item.insert(QStringLiteral("type"), QStringLiteral("on_demand"));
        break;
    case whatevr::v2::SYNC_TYPE_PUSH_NAMES:
        item.insert(QStringLiteral("type"), QStringLiteral("push_name"));
        break;
    default:
        item.insert(QStringLiteral("type"), QStringLiteral("unspecified"));
        break;
    }
    switch (row.phase()) {
    case whatevr::v2::SYNC_PHASE_IDLE:
        item.insert(QStringLiteral("phase"), QStringLiteral("idle"));
        break;
    case whatevr::v2::SYNC_PHASE_RUNNING:
        item.insert(QStringLiteral("phase"), QStringLiteral("processing"));
        break;
    case whatevr::v2::SYNC_PHASE_STALLED:
        item.insert(QStringLiteral("phase"), QStringLiteral("stalled"));
        break;
    case whatevr::v2::SYNC_PHASE_DONE:
        item.insert(QStringLiteral("phase"), QStringLiteral("complete"));
        item.insert(QStringLiteral("is_complete"), true);
        break;
    default:
        item.insert(QStringLiteral("phase"), QStringLiteral("unspecified"));
        break;
    }
    item.insert(QStringLiteral("progress_percent"), static_cast<qint64>(row.percent()));
    item.insert(QStringLiteral("chunk_order"), static_cast<qint64>(row.chunk()));
    item.insert(QStringLiteral("conversations_in_chunk"), static_cast<qint64>(row.chunk_chats()));
    item.insert(QStringLiteral("messages_in_chunk"), static_cast<qint64>(row.chunk_messages()));
    item.insert(QStringLiteral("processed_conversations"), static_cast<qint64>(row.done_chats()));
    item.insert(QStringLiteral("processed_messages"), static_cast<qint64>(row.done_messages()));
    return item;
}

QJsonObject translateV2TypingRow(const whatevr::v2::TypingRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.chat_id()));
    QJsonArray senders;
    for (const auto &typist : row.typists()) {
        QJsonObject sender;
        sender.insert(QStringLiteral("jid"), v2s(typist.person().id()));
        if (!typist.person().name().empty()) {
            sender.insert(QStringLiteral("name"), v2s(typist.person().name()));
        }
        senders.append(sender);
    }
    item.insert(QStringLiteral("senders"), senders);
    return item;
}

QJsonObject translateV2TransferRow(const whatevr::v2::TransferRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.message_id()));
    item.insert(QStringLiteral("message_id"), v2s(row.message_id()));
    if (!row.chat_id().empty()) {
        item.insert(QStringLiteral("chat_id"), v2s(row.chat_id()));
    }
    switch (row.direction()) {
    case whatevr::v2::TRANSFER_DIRECTION_DOWNLOAD:
        item.insert(QStringLiteral("direction"), QStringLiteral("download"));
        break;
    case whatevr::v2::TRANSFER_DIRECTION_UPLOAD:
        item.insert(QStringLiteral("direction"), QStringLiteral("upload"));
        break;
    default:
        break;
    }
    item.insert(QStringLiteral("received_bytes"), static_cast<qint64>(row.done_bytes()));
    item.insert(QStringLiteral("total_bytes"), static_cast<qint64>(row.total_bytes()));
    if (!row.error().empty()) {
        item.insert(QStringLiteral("error"), v2s(row.error()));
    }
    return item;
}

QJsonObject translateV2StickerRow(const whatevr::v2::StickerRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    item.insert(QStringLiteral("cache_key"), v2s(row.id()));
    if (!row.path().empty()) {
        item.insert(QStringLiteral("local_path"), v2s(row.path()));
    }
    if (!row.mime().empty()) {
        item.insert(QStringLiteral("mime_type"), v2s(row.mime()));
    }
    if (row.animated() || row.lottie()) {
        item.insert(QStringLiteral("is_animated"), true);
    }
    if (row.width() > 0) {
        item.insert(QStringLiteral("width"), static_cast<qint64>(row.width()));
    }
    if (row.height() > 0) {
        item.insert(QStringLiteral("height"), static_cast<qint64>(row.height()));
    }
    return item;
}

QJsonObject translateV2StickerPackRow(const whatevr::v2::StickerPackRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    if (!row.name().empty()) {
        item.insert(QStringLiteral("name"), v2s(row.name()));
    }
    if (!row.publisher().empty()) {
        item.insert(QStringLiteral("publisher"), v2s(row.publisher()));
    }
    if (!row.description().empty()) {
        item.insert(QStringLiteral("description"), v2s(row.description()));
    }
    if (row.animated() || row.lottie()) {
        item.insert(QStringLiteral("animated"), true);
    }
    if (row.lottie()) {
        item.insert(QStringLiteral("lottie"), true);
    }
    if (!row.tray_path().empty()) {
        item.insert(QStringLiteral("tray_local_path"), v2s(row.tray_path()));
    }
    item.insert(QStringLiteral("sticker_count"), static_cast<qint64>(row.count()));
    item.insert(QStringLiteral("installed"), row.installed());
    item.insert(QStringLiteral("contents_fetched"), row.fetched());
    return item;
}

QJsonObject translateV2LogRow(const whatevr::v2::LogRow &row)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.id()));
    if (row.t_ms() > 0) {
        item.insert(QStringLiteral("time"),
                    QDateTime::fromMSecsSinceEpoch(row.t_ms()).toString(QStringLiteral("HH:mm:ss.zzz")));
    }
    item.insert(QStringLiteral("level"), v2s(row.level()));
    item.insert(QStringLiteral("text"), v2s(row.text()));
    return item;
}

QJsonObject translateV2LiveLocationRow(const whatevr::v2::LiveLocationRow &row, const QString &chatId)
{
    QJsonObject item;
    item.insert(QStringLiteral("id"), v2s(row.message_id()));
    if (!chatId.isEmpty()) {
        item.insert(QStringLiteral("chat_id"), chatId);
    }
    QJsonObject sender = translateV2Person(row.sender());
    item.insert(QStringLiteral("sender"), sender);
    item.insert(QStringLiteral("started_at"), static_cast<qint64>(row.started_ms() / 1000));
    if (row.expires_ms() > 0) {
        item.insert(QStringLiteral("expires_at"), static_cast<qint64>(row.expires_ms() / 1000));
    }
    if (row.updated_ms() > 0) {
        item.insert(QStringLiteral("updated_at"), static_cast<qint64>(row.updated_ms() / 1000));
    }
    if (row.has_location()) {
        item.insert(QStringLiteral("location"), translateV2LocationBody(row.location()));
    }
    return item;
}

namespace
{

void v2PersonFromJson(const QJsonObject &person, whatevr::v2::Person *out)
{
    out->set_id(person.value(QStringLiteral("id")).toString().toStdString());
    out->set_name(person.value(QStringLiteral("name")).toString().toStdString());
    out->set_phone(person.value(QStringLiteral("phone")).toString().toStdString());
    out->set_avatar_path(person.value(QStringLiteral("avatar_path")).toString().toStdString());
}

} // namespace

bool v2UpsertRowFromJson(const QString &view, const QJsonObject &item, whatevr::v2::Upsert *out)
{
    const auto str = [&](const char *key) {
        return item.value(QLatin1StringView(key)).toString().toStdString();
    };
    if (view == QLatin1String("chats") || view == QLatin1String("chat")) {
        whatevr::v2::ChatRow row;
        if (!v2ChatRowFromJson(item, &row)) {
            return false;
        }
        *out->mutable_chat() = row;
        return true;
    }
    if (view == QLatin1String("status")) {
        whatevr::v2::MessageRow row;
        if (!v2MessageRowFromJson(item, &row)) {
            return false;
        }
        *out->mutable_message() = row;
        return true;
    }
    if (view == QLatin1String("status.muted")) {
        auto *row = out->mutable_status_muted();
        row->set_sender_id(str("id"));
        return true;
    }
    if (view == QLatin1String("messages") || view == QLatin1String("starred")
        || view == QLatin1String("pinned") || view == QLatin1String("chat_media")
        || view == QLatin1String("chat_links")) {
        whatevr::v2::MessageRow row;
        if (!v2MessageRowFromJson(item, &row)) {
            return false;
        }
        *out->mutable_message() = row;
        return true;
    }
    if (view == QLatin1String("connection")) {
        auto *row = out->mutable_connection();
        const QString state = item.value(QStringLiteral("state")).toString();
        if (state == QLatin1String("need_login")) {
            row->set_state(whatevr::v2::CONNECTION_STATE_NEED_LOGIN);
        } else if (state == QLatin1String("connecting")) {
            row->set_state(whatevr::v2::CONNECTION_STATE_CONNECTING);
        } else if (state == QLatin1String("online")) {
            row->set_state(whatevr::v2::CONNECTION_STATE_ONLINE);
        } else if (state == QLatin1String("reconnecting")) {
            row->set_state(whatevr::v2::CONNECTION_STATE_WAITING);
        } else if (state == QLatin1String("offline")) {
            row->set_state(whatevr::v2::CONNECTION_STATE_OFFLINE);
        } else {
            row->set_state(whatevr::v2::CONNECTION_STATE_STARTING);
        }
        row->set_detail(str("detail"));
        row->set_attempt(static_cast<std::uint32_t>(item.value(QStringLiteral("retry_attempt")).toInt()));
        row->set_can_reconnect(item.value(QStringLiteral("can_reconnect")).toBool());
        return true;
    }
    if (view == QLatin1String("login")) {
        auto *row = out->mutable_login();
        const QString state = item.value(QStringLiteral("state")).toString();
        if (state == QLatin1String("logged_in")) {
            row->set_state(whatevr::v2::LOGIN_STATE_LOGGED_IN);
        } else if (state == QLatin1String("qr")) {
            row->set_state(whatevr::v2::LOGIN_STATE_QR);
        } else if (state == QLatin1String("pairing")) {
            row->set_state(whatevr::v2::LOGIN_STATE_PAIRING);
        } else if (state == QLatin1String("failed")) {
            row->set_state(whatevr::v2::LOGIN_STATE_FAILED);
        }
        const QJsonObject qr = item.value(QStringLiteral("qr")).toObject();
        row->set_qr(qr.value(QStringLiteral("code")).toString().toStdString());
        const QDateTime expires =
            QDateTime::fromString(qr.value(QStringLiteral("expires_at")).toString(), Qt::ISODateWithMs);
        if (expires.isValid()) {
            row->set_qr_expires_ms(expires.toMSecsSinceEpoch());
        }
        row->set_detail(str("detail"));
        return true;
    }
    if (view == QLatin1String("chat_folders")) {
        auto *row = out->mutable_chat_folder();
        // The daemon keys rows by string id; the test fake may use numbers.
        row->set_id(static_cast<std::int64_t>(item.value(QStringLiteral("id")).toVariant().toLongLong()));
        row->set_name(str("name"));
        return true;
    }
    if (view == QLatin1String("typing")) {
        auto *row = out->mutable_typing();
        row->set_chat_id(str("id"));
        const QJsonArray senders = item.value(QStringLiteral("senders")).toArray();
        if (senders.isEmpty() && item.contains(QStringLiteral("id"))) {
            auto *typist = row->add_typists();
            typist->mutable_person()->set_id(str("id"));
        }
        for (const QJsonValue &entry : senders) {
            const QJsonObject sender = entry.toObject();
            auto *typist = row->add_typists();
            typist->mutable_person()->set_id(
                sender.value(QStringLiteral("jid")).toString().toStdString());
            typist->mutable_person()->set_name(
                sender.value(QStringLiteral("name")).toString().toStdString());
        }
        return true;
    }
    if (view == QLatin1String("presence")) {
        auto *row = out->mutable_presence();
        v2PersonFromJson(item, row->mutable_person());
        if (row->person().id().empty()) {
            row->mutable_person()->set_id(str("id"));
        }
        row->set_availability(item.value(QStringLiteral("availability")).toString()
                                      == QLatin1String("online")
                                  ? whatevr::v2::AVAILABILITY_ONLINE
                                  : whatevr::v2::AVAILABILITY_OFFLINE);
        row->set_last_seen_ms(static_cast<std::int64_t>(
            item.value(QStringLiteral("last_seen_unix")).toVariant().toLongLong() * 1000));
        return true;
    }
    if (view == QLatin1String("receipts")) {
        auto *row = out->mutable_receipt();
        v2PersonFromJson(item, row->mutable_person());
        if (row->person().id().empty()) {
            row->mutable_person()->set_id(str("id"));
        }
        if (!item.value(QStringLiteral("name")).toString().isEmpty()) {
            row->mutable_person()->set_name(
                item.value(QStringLiteral("name")).toString().toStdString());
        }
        row->set_delivered_ms(static_cast<std::int64_t>(
            item.value(QStringLiteral("delivered_ts_unix")).toVariant().toLongLong() * 1000));
        row->set_read_ms(static_cast<std::int64_t>(
            item.value(QStringLiteral("read_ts_unix")).toVariant().toLongLong() * 1000));
        row->set_played_ms(static_cast<std::int64_t>(
            item.value(QStringLiteral("played_ts_unix")).toVariant().toLongLong() * 1000));
        return true;
    }
    if (view == QLatin1String("self")) {
        auto *row = out->mutable_self();
        row->set_id(str("id"));
        row->set_phone(str("phone"));
        row->set_push_name(str("push_name"));
        row->set_about(str("about"));
        row->set_avatar_path(str("avatar_path"));
        return true;
    }
    if (view == QLatin1String("contact")) {
        auto *row = out->mutable_contact();
        row->set_id(str("id"));
        row->set_phone(str("phone"));
        row->set_saved_name(str("saved_name"));
        row->set_push_name(str("push_name"));
        row->set_business_name(str("business_name"));
        row->set_business(item.value(QStringLiteral("is_business")).toBool());
        row->set_about(str("about"));
        row->set_avatar_path(str("avatar_path"));
        return true;
    }
    if (view == QLatin1String("group")) {
        auto *row = out->mutable_group();
        row->set_chat_id(str("id"));
        row->set_subject(str("subject"));
        row->set_description(str("description"));
        row->set_avatar_path(str("avatar_path"));
        const QString role = item.value(QStringLiteral("my_role")).toString();
        if (role == QLatin1String("admin")) {
            row->set_my_role(whatevr::v2::GROUP_ROLE_ADMIN);
        } else if (role == QLatin1String("superadmin")) {
            row->set_my_role(whatevr::v2::GROUP_ROLE_SUPERADMIN);
        } else if (!role.isEmpty()) {
            row->set_my_role(whatevr::v2::GROUP_ROLE_MEMBER);
        }
        row->set_announce(item.value(QStringLiteral("announce")).toBool());
        row->set_locked(item.value(QStringLiteral("locked")).toBool());
        return true;
    }
    if (view == QLatin1String("group_members")) {
        auto *row = out->mutable_group_member();
        row->mutable_person()->set_id(str("id"));
        const QString jid = item.value(QStringLiteral("jid")).toString();
        if (!jid.isEmpty()) {
            row->mutable_person()->set_id(jid.toStdString());
        }
        row->mutable_person()->set_name(
            item.value(QStringLiteral("display_name")).toString().toStdString());
        row->mutable_person()->set_phone(str("phone"));
        row->mutable_person()->set_avatar_path(str("avatar_path"));
        const QString role = item.value(QStringLiteral("role")).toString();
        if (role == QLatin1String("admin")) {
            row->set_role(whatevr::v2::GROUP_ROLE_ADMIN);
        } else if (role == QLatin1String("superadmin")) {
            row->set_role(whatevr::v2::GROUP_ROLE_SUPERADMIN);
        } else if (role == QLatin1String("left")) {
            row->set_role(whatevr::v2::GROUP_ROLE_LEFT);
        } else {
            row->set_role(whatevr::v2::GROUP_ROLE_MEMBER);
        }
        return true;
    }
    if (view == QLatin1String("privacy")) {
        auto *row = out->mutable_privacy();
        const auto privacyValue = [&](const char *key) {
            const QString text = item.value(QLatin1StringView(key)).toString();
            if (text == QLatin1String("all")) {
                return whatevr::v2::PRIVACY_VALUE_ALL;
            }
            if (text == QLatin1String("contacts")) {
                return whatevr::v2::PRIVACY_VALUE_CONTACTS;
            }
            if (text == QLatin1String("contact_blacklist")) {
                return whatevr::v2::PRIVACY_VALUE_CONTACTS_EXCEPT;
            }
            if (text == QLatin1String("nobody") || text == QLatin1String("none")) {
                return whatevr::v2::PRIVACY_VALUE_NOBODY;
            }
            if (text == QLatin1String("match_last_seen")) {
                return whatevr::v2::PRIVACY_VALUE_MATCH_LAST_SEEN;
            }
            if (text == QLatin1String("known")) {
                return whatevr::v2::PRIVACY_VALUE_KNOWN;
            }
            return whatevr::v2::PRIVACY_VALUE_UNSPECIFIED;
        };
        row->set_last_seen(privacyValue("last_seen"));
        row->set_online(privacyValue("online"));
        row->set_profile_photo(privacyValue("profile_photo"));
        row->set_about(privacyValue("about"));
        row->set_group_add(privacyValue("group_add"));
        row->set_call_add(privacyValue("call_add"));
        row->set_read_receipts(item.value(QStringLiteral("read_receipts")).toBool());
        return true;
    }
    if (view == QLatin1String("preferences")) {
        auto *row = out->mutable_preferences()->mutable_preferences();
        row->set_notifications(item.value(QStringLiteral("notifications_enabled")).toBool());
        row->set_notification_sound(item.value(QStringLiteral("notification_sound")).toBool());
        row->set_notification_preview(item.value(QStringLiteral("notification_preview")).toBool());
        row->set_auto_download_photos(item.value(QStringLiteral("auto_download_photos")).toBool());
        row->set_auto_download_videos(item.value(QStringLiteral("auto_download_videos")).toBool());
        row->set_auto_download_audio(item.value(QStringLiteral("auto_download_audio")).toBool());
        row->set_auto_download_documents(
            item.value(QStringLiteral("auto_download_documents")).toBool());
        row->set_auto_download_stickers(
            item.value(QStringLiteral("auto_download_stickers")).toBool());
        return true;
    }
    if (view == QLatin1String("blocklist")) {
        auto *row = out->mutable_blocked();
        row->mutable_person()->set_id(str("id"));
        const QString jid = item.value(QStringLiteral("jid")).toString();
        if (!jid.isEmpty()) {
            row->mutable_person()->set_id(jid.toStdString());
        }
        row->mutable_person()->set_name(
            item.value(QStringLiteral("name")).toString().toStdString());
        row->mutable_person()->set_phone(str("phone"));
        row->mutable_person()->set_avatar_path(str("avatar_path"));
        return true;
    }
    if (view == QLatin1String("sync")) {
        auto *row = out->mutable_sync();
        const QString type = item.value(QStringLiteral("type")).toString();
        if (type == QLatin1String("recent")) {
            row->set_type(whatevr::v2::SYNC_TYPE_RECENT);
        } else if (type == QLatin1String("full")) {
            row->set_type(whatevr::v2::SYNC_TYPE_FULL);
        } else if (type == QLatin1String("on_demand")) {
            row->set_type(whatevr::v2::SYNC_TYPE_ON_DEMAND);
        } else if (type == QLatin1String("push_name")) {
            row->set_type(whatevr::v2::SYNC_TYPE_PUSH_NAMES);
        } else if (type == QLatin1String("initial_bootstrap")
                   || type == QLatin1String("initial_status_v3")) {
            row->set_type(whatevr::v2::SYNC_TYPE_INITIAL);
        }
        const QString phase = item.value(QStringLiteral("phase")).toString();
        if (phase == QLatin1String("complete") || item.value(QStringLiteral("is_complete")).toBool()) {
            row->set_phase(whatevr::v2::SYNC_PHASE_DONE);
        } else if (phase == QLatin1String("stalled")) {
            row->set_phase(whatevr::v2::SYNC_PHASE_STALLED);
        } else if (phase == QLatin1String("idle")) {
            row->set_phase(whatevr::v2::SYNC_PHASE_IDLE);
        } else if (!phase.isEmpty() && phase != QLatin1String("unspecified")) {
            row->set_phase(whatevr::v2::SYNC_PHASE_RUNNING);
        }
        row->set_percent(static_cast<std::uint32_t>(item.value(QStringLiteral("progress_percent")).toInt()));
        row->set_chunk(static_cast<std::uint32_t>(item.value(QStringLiteral("chunk_order")).toInt()));
        row->set_chunk_chats(
            static_cast<std::uint32_t>(item.value(QStringLiteral("conversations_in_chunk")).toInt()));
        row->set_chunk_messages(
            static_cast<std::uint32_t>(item.value(QStringLiteral("messages_in_chunk")).toInt()));
        row->set_done_chats(
            static_cast<std::uint32_t>(item.value(QStringLiteral("processed_conversations")).toInt()));
        row->set_done_messages(
            static_cast<std::uint32_t>(item.value(QStringLiteral("processed_messages")).toInt()));
        return true;
    }
    if (view == QLatin1String("transfers")) {
        auto *row = out->mutable_transfer();
        row->set_message_id(str("message_id"));
        row->set_chat_id(str("chat_id"));
        row->set_direction(item.value(QStringLiteral("direction")).toString()
                                   == QLatin1String("upload")
                               ? whatevr::v2::TRANSFER_DIRECTION_UPLOAD
                               : whatevr::v2::TRANSFER_DIRECTION_DOWNLOAD);
        row->set_done_bytes(static_cast<std::uint64_t>(
            item.value(QStringLiteral("received_bytes")).toVariant().toULongLong()));
        row->set_total_bytes(static_cast<std::uint64_t>(
            item.value(QStringLiteral("total_bytes")).toVariant().toULongLong()));
        row->set_error(str("error"));
        return true;
    }
    if (view == QLatin1String("stickers") || view == QLatin1String("sticker_pack")) {
        auto *row = out->mutable_sticker();
        row->set_id(str("cache_key"));
        row->set_path(str("local_path"));
        row->set_mime(str("mime_type"));
        row->set_animated(item.value(QStringLiteral("is_animated")).toBool());
        row->set_width(static_cast<std::uint32_t>(item.value(QStringLiteral("width")).toInt()));
        row->set_height(static_cast<std::uint32_t>(item.value(QStringLiteral("height")).toInt()));
        return true;
    }
    if (view == QLatin1String("sticker_packs")) {
        auto *row = out->mutable_sticker_pack();
        row->set_id(str("id"));
        row->set_name(str("name"));
        row->set_publisher(str("publisher"));
        row->set_description(str("description"));
        row->set_animated(item.value(QStringLiteral("animated")).toBool());
        row->set_lottie(item.value(QStringLiteral("lottie")).toBool());
        row->set_tray_path(str("tray_local_path"));
        row->set_count(static_cast<std::uint32_t>(item.value(QStringLiteral("sticker_count")).toInt()));
        row->set_installed(item.value(QStringLiteral("installed")).toBool());
        row->set_fetched(item.value(QStringLiteral("contents_fetched")).toBool());
        return true;
    }
    if (view == QLatin1String("live_locations")) {
        auto *row = out->mutable_live_location();
        row->set_message_id(str("id"));
        const QJsonObject sender = item.value(QStringLiteral("sender")).toObject();
        v2PersonFromJson(sender.isEmpty() ? item : sender, row->mutable_sender());
        return true;
    }
    return false;
}

} // namespace whatevr::proto
