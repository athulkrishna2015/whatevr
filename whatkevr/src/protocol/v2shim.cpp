#include "v2shim.h"

#include <QDateTime>

#include <string_view>

#include "protocolclient.h"

namespace whatevr::proto
{

namespace
{

// Protobuf v36 string getters return string_view.
[[nodiscard]] QString v2s(std::string_view view)
{
    return QString::fromUtf8(view.data(), static_cast<int>(view.size()));
}

} // namespace

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

} // namespace whatevr::proto
