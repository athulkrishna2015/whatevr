// Unit tests for the protocol v2 wire codec: varint-delimited `Frame`
// encode/decode, partial delivery, and the oversized/malformed paths.

#include <QByteArray>
#include <QTest>

#include "v2codec.h"
#include "whatevr/v2/frame.pb.h"

using namespace whatevr::proto;

namespace
{

whatevr::v2::Frame helloRequest(quint64 id)
{
    whatevr::v2::Frame frame;
    auto *request = frame.mutable_request();
    request->set_id(id);
    auto *hello = request->mutable_hello();
    hello->set_client("tst");
    hello->set_protocol(2);
    return frame;
}

} // namespace

class TestV2Codec : public QObject
{
    Q_OBJECT

private Q_SLOTS:
    void roundTrip()
    {
        const whatevr::v2::Frame sent = helloRequest(7);
        const QByteArray wire = encodeV2Frame(sent);

        QByteArray buffer = wire;
        whatevr::v2::Frame got;
        QCOMPARE(popV2Frame(buffer, &got), V2DecodeResult::Frame);
        QVERIFY(buffer.isEmpty());
        QVERIFY(got.has_request());
        QCOMPARE(got.request().id(), quint64(7));
        QVERIFY(got.request().has_hello());
        QCOMPARE(got.request().hello().protocol(), quint32(2));
    }

    void partialDeliveryWaits()
    {
        const QByteArray wire = encodeV2Frame(helloRequest(1));
        QByteArray buffer = wire.left(1);
        whatevr::v2::Frame got;
        QCOMPARE(popV2Frame(buffer, &got), V2DecodeResult::NeedMore);
        QCOMPARE(buffer.size(), 1);

        buffer += wire.mid(1, wire.size() / 2);
        if (buffer.size() < wire.size()) {
            QCOMPARE(popV2Frame(buffer, &got), V2DecodeResult::NeedMore);
            buffer += wire.mid(buffer.size());
        }
        QCOMPARE(popV2Frame(buffer, &got), V2DecodeResult::Frame);
        QVERIFY(got.has_request());
    }

    void backToBackFrames()
    {
        QByteArray buffer = encodeV2Frame(helloRequest(1)) + encodeV2Frame(helloRequest(2));
        whatevr::v2::Frame first;
        QCOMPARE(popV2Frame(buffer, &first), V2DecodeResult::Frame);
        QCOMPARE(first.request().id(), quint64(1));
        whatevr::v2::Frame second;
        QCOMPARE(popV2Frame(buffer, &second), V2DecodeResult::Frame);
        QCOMPARE(second.request().id(), quint64(2));
        QVERIFY(buffer.isEmpty());
    }

    void oversizedPrefixDrops()
    {
        // Length 17 MiB: well-formed prefix, past the cap.
        QByteArray buffer;
        quint32 length = 17u * 1024u * 1024u;
        while (length >= 0x80) {
            buffer.append(char((length & 0x7f) | 0x80));
            length >>= 7;
        }
        buffer.append(char(length));
        QCOMPARE(popV2Frame(buffer, nullptr), V2DecodeResult::Oversized);
    }

    void garbagePayloadIsMalformed()
    {
        // A 4-byte payload that is not a valid Frame.
        const QByteArray payload("\xff\xff\xff\xff", 4);
        QByteArray buffer;
        buffer.append(char(4));
        buffer += payload;
        QCOMPARE(popV2Frame(buffer, nullptr), V2DecodeResult::Malformed);
    }
};

QTEST_MAIN(TestV2Codec)
#include "tst_v2codec.moc"
