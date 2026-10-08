#pragma once

#include <QByteArray>
#include <QJsonObject>
#include <QString>
#include <QVariantMap>

#include <cstdint>
#include <string_view>

#include "whatevr/v2/frame.pb.h"

namespace whatevr::proto
{

class ViewSink;

// JSON-compatibility shim between the v1 JSON API the frontend speaks and
// the v2 protobuf wire. ProtocolClient keeps its public JSON surface
// (request/subscribe/ViewSink) while every byte on the socket becomes a
// size-delimited v2 `Frame`. Translators are added view by view; anything
// without a v2 arm reports `unknown_method`, and the controller's existing
// error paths (plus the v2 hello `features` list) decide what the UI shows.
//
// v1 shapes reproduced here are the daemon's (`whatevrd/internal/protocol`
// at `backup/pre-upstream-merge`): field names, state strings, and the
// RFC3339 expiry format the QML countdown parses.

// Protobuf v36 string getters return string_view.
[[nodiscard]] QString v2s(std::string_view view);

// QJsonValue::toString() only returns strings as-is (numbers become ""): use
// this for ids and counts that may arrive as JSON numbers.
[[nodiscard]] inline std::uint64_t v2UInt64(const QJsonValue &value)
{
    return value.toVariant().toULongLong();
}

// v2 `ErrorCode` to the v1 stable machine-readable code.
[[nodiscard]] QString v2ErrorCode(whatevr::v2::ErrorCode code);

// The first request on a connection: `hello` with `protocol: 2`.
void buildV2Hello(std::uint64_t id, const QString &clientName, whatevr::v2::Request *out);

// v2 `HelloResult` to the v1 hello result map
// (`daemon`, `version`, `protocol`, `data_dir`, `cache_dir`).
[[nodiscard]] QVariantMap translateV2HelloResult(const whatevr::v2::HelloResult &result);

// v1 `(view, params)` subscribe to a v2 `Subscribe`. False when the view has
// no v2 arm (chat_folders, status, calls, channels, ...): the caller reports
// `unknown_method` exactly as a daemon that never served the view would.
bool buildV2Subscribe(std::uint64_t id, const QString &view, const QJsonObject &params,
                      whatevr::v2::Request *out);
// A v1 `(method, params)` command to a v2 `Request`. False for v1-only
// surface with no v2 arm (folders, schedules, status, calls, channels,
// groups, backups, ...): same `unknown_method` contract as above.
bool buildV2Request(std::uint64_t id, const QString &method, const QJsonObject &params,
                    whatevr::v2::Request *out);
// The reverse direction, for test doubles: a v2 `Request` back to the v1
// `(method, params)` the frontend's JSON core speaks. False for arms the
// frontend never sends (responses never appear here).
struct V2RequestV1 {
    QString method;
    QJsonObject params;
};
bool v2RequestToV1(const whatevr::v2::Request &request, V2RequestV1 *out);
// Result builder for test doubles: a v1 result object to the v2 `Response`
// the method answers. False for methods with no v2 result arm (the client
// never sends them, so a fake never answers them either).
bool v2ResponseFromV1(const QString &method, std::uint64_t id, const QJsonObject &result,
                      whatevr::v2::Response *out);
// Error builder for test doubles.
void v2ErrorResponse(std::uint64_t id, const QString &code, const QString &message,
                     whatevr::v2::Response *out);
// Fixture rows to v2 rows, for test doubles serving v1 JSON fixtures.
bool v2ChatRowFromJson(const QJsonObject &item, whatevr::v2::ChatRow *out);
bool v2MessageRowFromJson(const QJsonObject &item, whatevr::v2::MessageRow *out);
// A fixture item for `view` to its v2 upsert row. False for views the fake
// never serves (v1-only views fail at subscribe time instead).
bool v2UpsertRowFromJson(const QString &view, const QJsonObject &item, whatevr::v2::Upsert *out);
// `extend`/`unsubscribe` carry only the daemon-assigned sub id.
void buildV2Extend(std::uint64_t id, std::uint64_t sub, int count, const QString &direction,
                   whatevr::v2::Request *out);
void buildV2Unsubscribe(std::uint64_t id, std::uint64_t sub, whatevr::v2::Request *out);

// A v2 `Response` to the v1 `(result, error)` pair the callbacks take.
struct V2ResponseTranslation {
    QJsonObject result;
    QString errorCode;
    QString errorMessage;
    [[nodiscard]] bool isError() const { return !errorCode.isEmpty(); }
};
[[nodiscard]] V2ResponseTranslation translateV2Response(const whatevr::v2::Response &response);

// Apply a v2 `ViewUpdate` (reset, upserts, removes, ready) to a v1 sink.
// Sort bytes become order-preserving hex; rows become v1-shaped JSON.
// `extraFields` (e.g. the subscribing chat_id) are merged into every upsert.
void applyV2ViewUpdate(const whatevr::v2::ViewUpdate &update, ViewSink *sink,
                       const QJsonObject &extraFields = {});

// Row translators to v1 JSON shapes. Unknown item arms produce an empty
// object, which sinks treat like an unknown row, never a crash.
[[nodiscard]] QJsonObject translateV2ConnectionRow(const whatevr::v2::ConnectionRow &row);
[[nodiscard]] QJsonObject translateV2LoginRow(const whatevr::v2::LoginRow &row);
[[nodiscard]] QJsonObject translateV2ChatRow(const whatevr::v2::ChatRow &row);
// Message rows: top-level scalars plus sender/fallback/text. Rich bodies
// (media, polls, location, ...) gain translators with the views that need
// them; every row carries a fallback either way.
[[nodiscard]] QJsonObject translateV2MessageRow(const whatevr::v2::MessageRow &row);
// Remaining row translators to v1 JSON shapes.
[[nodiscard]] QJsonObject translateV2SelfRow(const whatevr::v2::SelfRow &row);
[[nodiscard]] QJsonObject translateV2ContactRow(const whatevr::v2::ContactRow &row);
[[nodiscard]] QJsonObject translateV2GroupRow(const whatevr::v2::GroupRow &row);
[[nodiscard]] QJsonObject translateV2GroupMemberRow(const whatevr::v2::GroupMemberRow &row);
[[nodiscard]] QJsonObject translateV2PresenceRow(const whatevr::v2::PresenceRow &row);
[[nodiscard]] QJsonObject translateV2ReceiptRow(const whatevr::v2::ReceiptRow &row);
[[nodiscard]] QJsonObject translateV2PrivacyRow(const whatevr::v2::PrivacyRow &row);
[[nodiscard]] QJsonObject translateV2Preferences(const whatevr::v2::Preferences &prefs);
[[nodiscard]] QJsonObject translateV2SyncRow(const whatevr::v2::SyncRow &row);
[[nodiscard]] QJsonObject translateV2TypingRow(const whatevr::v2::TypingRow &row);
[[nodiscard]] QJsonObject translateV2TransferRow(const whatevr::v2::TransferRow &row);
[[nodiscard]] QJsonObject translateV2StickerRow(const whatevr::v2::StickerRow &row);
[[nodiscard]] QJsonObject translateV2StickerPackRow(const whatevr::v2::StickerPackRow &row);
[[nodiscard]] QJsonObject translateV2LiveLocationRow(const whatevr::v2::LiveLocationRow &row,
                                                      const QString &chatId);

// Opaque sort bytes to an order-preserving string for the keyed models.
[[nodiscard]] QString v2SortKey(std::string_view sort);

// Message status enum to the v1 lowercase string.
[[nodiscard]] inline QString v2MessageStatus(whatevr::v2::MessageStatus status)
{
    return v2s(whatevr::v2::MessageStatus_Name(status))
        .remove(QStringLiteral("MESSAGE_STATUS_"))
        .toLower();
}

} // namespace whatevr::proto
