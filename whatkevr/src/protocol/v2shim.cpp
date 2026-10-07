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
    } else {
        // unread/favorite filters are v1-only; the daemon never served them
        // either (normalizeChatFilter rejected everything past groups).
        return false;
    }
    return true;
}

} // namespace

bool buildV2Subscribe(std::uint64_t id, const QString &view, const QJsonObject &params,
                      whatevr::v2::Request *out)
{
    auto *subscribe = out->mutable_subscribe();
    subscribe->set_limit(static_cast<std::uint32_t>(params.value(QStringLiteral("limit")).toInt(0)));
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
    } else if (view == QLatin1String("typing")) {
        subscribe->mutable_typing();
    } else if (view == QLatin1String("transfers")) {
        subscribe->mutable_transfers();
    } else if (view == QLatin1String("self")) {
        subscribe->mutable_self();
    } else if (view == QLatin1String("preferences")) {
        subscribe->mutable_preferences();
    } else if (view == QLatin1String("privacy")) {
        subscribe->mutable_privacy();
    } else if (view == QLatin1String("blocklist")) {
        subscribe->mutable_blocklist();
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
    case whatevr::v2::Response::kChatEnsureDirect: {
        QJsonObject result;
        result.insert(QStringLiteral("chat_id"), v2s(response.chat_ensure_direct().chat_id()));
        out.result = result;
        break;
    }
    case whatevr::v2::Response::kMessageForward: {
        QJsonObject result;
        if (!response.message_forward().message_ids().empty()) {
            result.insert(QStringLiteral("message_id"),
                          v2s(response.message_forward().message_ids(0)));
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
            item.insert(QStringLiteral("cache_key"), v2s(row.id()));
            item.insert(QStringLiteral("path"), v2s(row.path()));
            item.insert(QStringLiteral("mime"), v2s(row.mime()));
            item.insert(QStringLiteral("animated"), row.animated());
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
    if (row.kept()) {
        item.insert(QStringLiteral("kept"), true);
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
    default:
        return {};
    }
}

} // namespace

void applyV2ViewUpdate(const whatevr::v2::ViewUpdate &update, ViewSink *sink)
{
    if (!sink) {
        return;
    }
    if (update.reset()) {
        sink->onReset();
    }
    for (const auto &change : update.changes()) {
        switch (change.change_case()) {
        case whatevr::v2::Change::kUpsert: {
            const auto &upsert = change.upsert();
            sink->onUpsert(v2SortKey(upsert.sort()), translateV2Item(upsert));
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

// v1 `chat_id`/`jid` params to a v2 person address.
void setAddressId(whatevr::v2::Address *address, const QJsonObject &params, const char *key)
{
    address->set_id(params.value(QLatin1StringView(key)).toString().toStdString());
}

QStringView methodName(const QString &method)
{
    return QStringView(method);
}

} // namespace

bool buildV2Request(std::uint64_t id, const QString &method, const QJsonObject &params,
                    whatevr::v2::Request *out)
{
    const auto get = [&](const char *key) { return params.value(QLatin1StringView(key)); };
    auto *request = out;
    request->set_id(id);

    // Session and account.
    if (method == QLatin1String("session.update")) {
        auto *update = request->mutable_session_update();
        update->set_focused(get("focused").toBool());
        update->set_active_chat_id(get("active_chat_id").toString().toStdString());
        return true;
    }
    if (method == QLatin1String("daemon.reconnect")) {
        request->mutable_daemon_reconnect();
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
    if (method == QLatin1String("chat.pin")) {
        auto *pin = request->mutable_chat_pin();
        pin->set_chat_id(get("chat_id").toString().toStdString());
        pin->set_pinned(get("pinned").toBool());
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

    // Messages.
    if (method == QLatin1String("message.react")) {
        auto *react = request->mutable_message_react();
        react->set_message_id(get("message_id").toString().toStdString());
        react->set_emoji(get("emoji").toString().toStdString());
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
            } else if (text == QLatin1String("nobody")) {
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
    if (method == QLatin1String("sticker_pack.install")) {
        auto *install = request->mutable_sticker_pack_install();
        install->set_pack_id(get("pack_id").toString().toStdString());
        install->set_installed(get("installed").toBool());
        return true;
    }

    Q_UNUSED(methodName);
    return false;
}

} // namespace whatevr::proto
