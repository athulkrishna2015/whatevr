#pragma once

#include <QByteArray>

#include <cstdint>

#include "whatevr/v2/frame.pb.h"

// Size-delimited `Frame` codec for protocol v2 (PROTOCOL.md "Framing").
// Every frame on the socket is one protobuf `Frame`, preceded by its length
// as a varint. Pure functions over bytes; the socket stays in ProtocolClient.
namespace whatevr::proto
{

// The daemon closes a connection that sends a bigger frame.
inline constexpr int kV2MaxFrameBytes = 16 * 1024 * 1024;

// Encode one frame to its wire form: varint length followed by the payload.
[[nodiscard]] QByteArray encodeV2Frame(const whatevr::v2::Frame &frame);

enum class V2DecodeResult : std::uint8_t {
    // A complete frame was parsed into `out` and consumed from `buffer`.
    Frame,
    // Not enough bytes yet; `buffer` is untouched.
    NeedMore,
    // The length prefix is malformed or names a frame past the 16 MiB cap.
    // The connection must be dropped, like the v1 oversized-line path.
    Oversized,
    // A complete frame's worth of bytes does not parse as a `Frame`.
    Malformed,
};

// Pop one frame off the front of `buffer` when a whole one is available.
// Consumes exactly the frame's bytes on Frame; consumes nothing otherwise.
V2DecodeResult popV2Frame(QByteArray &buffer, whatevr::v2::Frame *out);

} // namespace whatevr::proto
