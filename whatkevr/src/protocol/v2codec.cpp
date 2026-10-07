#include "v2codec.h"

#include <string>

namespace whatevr::proto
{

namespace
{

// Append `value` as a protobuf varint.
void appendVarint(QByteArray &out, std::uint32_t value)
{
    while (value >= 0x80) {
        out.append(static_cast<char>(static_cast<unsigned char>(value) | 0x80));
        value >>= 7;
    }
    out.append(static_cast<char>(static_cast<unsigned char>(value)));
}

} // namespace

QByteArray encodeV2Frame(const whatevr::v2::Frame &frame)
{
    std::string payload;
    frame.SerializeToString(&payload);
    QByteArray out;
    out.reserve(static_cast<int>(payload.size()) + 5);
    appendVarint(out, static_cast<std::uint32_t>(payload.size()));
    out.append(payload.data(), static_cast<int>(payload.size()));
    return out;
}

V2DecodeResult popV2Frame(QByteArray &buffer, whatevr::v2::Frame *out)
{
    // A 16 MiB cap fits in 4 varint bytes; anything longer is malformed.
    std::uint32_t length = 0;
    int shift = 0;
    int prefixBytes = 0;
    for (; prefixBytes < 5 && prefixBytes < buffer.size(); ++prefixBytes) {
        const auto byte = static_cast<unsigned char>(buffer.at(prefixBytes));
        length |= static_cast<std::uint32_t>(byte & 0x7f) << shift;
        shift += 7;
        if ((byte & 0x80) == 0) {
            ++prefixBytes;
            if (length > static_cast<std::uint32_t>(kV2MaxFrameBytes)) {
                return V2DecodeResult::Oversized;
            }
            const int total = prefixBytes + static_cast<int>(length);
            if (buffer.size() < total) {
                return V2DecodeResult::NeedMore;
            }
            whatevr::v2::Frame frame;
            if (!frame.ParseFromArray(buffer.constData() + prefixBytes, static_cast<int>(length))) {
                return V2DecodeResult::Malformed;
            }
            if (out) {
                *out = std::move(frame);
            }
            buffer.remove(0, total);
            return V2DecodeResult::Frame;
        }
    }
    // Ran out of bytes mid-prefix (or hit the 5-byte sanity bound): wait for
    // more unless the buffer already proves nobody is framing.
    if (prefixBytes >= 5 || buffer.size() > kV2MaxFrameBytes) {
        return V2DecodeResult::Oversized;
    }
    return V2DecodeResult::NeedMore;
}

} // namespace whatevr::proto
