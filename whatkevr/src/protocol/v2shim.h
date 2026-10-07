#pragma once

#include <QByteArray>
#include <QJsonObject>
#include <QString>
#include <QVariantMap>

#include <cstdint>

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
void applyV2ViewUpdate(const whatevr::v2::ViewUpdate &update, ViewSink *sink);

// Row translators to v1 JSON shapes. Unknown item arms produce an empty
// object, which sinks treat like an unknown row, never a crash.
[[nodiscard]] QJsonObject translateV2ConnectionRow(const whatevr::v2::ConnectionRow &row);
[[nodiscard]] QJsonObject translateV2LoginRow(const whatevr::v2::LoginRow &row);
[[nodiscard]] QJsonObject translateV2ChatRow(const whatevr::v2::ChatRow &row);

// Opaque sort bytes to an order-preserving string for the keyed models.
[[nodiscard]] QString v2SortKey(std::string_view sort);

} // namespace whatevr::proto
