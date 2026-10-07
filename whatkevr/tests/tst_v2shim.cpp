// Unit tests for the v1/v2 compatibility shim: request building, error and
// hello translation, and row shapes the models consume.

#include <QByteArray>
#include <QDateTime>
#include <QJsonObject>
#include <QTest>

#include "protocolclient.h"
#include "v2shim.h"
#include "whatevr/v2/frame.pb.h"

using namespace whatevr::proto;

namespace
{

class StubSink : public ViewSink
{
public:
    QList<QPair<QString, QJsonObject>> upserts;
    QList<QString> removed;
    QList<QPair<bool, bool>> readies;
    int resets = 0;

    void onUpsert(const QString &sort, const QJsonObject &item) override
    {
        upserts.append({sort, item});
    }
    void onRemove(const QString &id) override { removed.append(id); }
    void onReady(bool exhausted, bool hasExhausted) override
    {
        readies.append({exhausted, hasExhausted});
    }
    void onReset() override { ++resets; }
};

whatevr::v2::ChatRow chatRow(const std::string &id, whatevr::v2::ChatType type)
{
    whatevr::v2::ChatRow row;
    row.set_id(id);
    row.set_name("N");
    row.set_type(type);
    row.mutable_preview()->set_text("hi");
    row.mutable_preview()->set_from_me(true);
    row.mutable_preview()->set_status(whatevr::v2::MESSAGE_STATUS_READ);
    row.set_last_ms(1700000000000LL);
    row.set_unread(3);
    return row;
}

} // namespace

class TestV2Shim : public QObject
{
    Q_OBJECT

private Q_SLOTS:
    void errorCodes()
    {
        QCOMPARE(v2ErrorCode(whatevr::v2::ERROR_CODE_NOT_FOUND), QStringLiteral("not_found"));
        QCOMPARE(v2ErrorCode(whatevr::v2::ERROR_CODE_UNKNOWN_METHOD),
                 QStringLiteral("unknown_method"));
        QCOMPARE(v2ErrorCode(whatevr::v2::ERROR_CODE_IO), QStringLiteral("io"));
        QCOMPARE(v2ErrorCode(whatevr::v2::ERROR_CODE_GUARDED), QStringLiteral("rejected"));
    }

    void helloRoundTrip()
    {
        whatevr::v2::Request request;
        buildV2Hello(9, QStringLiteral("whatkevr"), &request);
        QCOMPARE(request.id(), quint64(9));
        QVERIFY(request.has_hello());
        QCOMPARE(request.hello().protocol(), quint32(2));
        QCOMPARE(QString::fromUtf8(request.hello().client().data(),
                                      static_cast<int>(request.hello().client().size())),
                 QStringLiteral("whatkevr"));

        whatevr::v2::HelloResult hello;
        hello.set_daemon("whatevrd");
        hello.set_version("0.9.1");
        hello.set_protocol(2);
        hello.add_features("chats");
        hello.set_data_dir("/d");
        const QVariantMap info = translateV2HelloResult(hello);
        QCOMPARE(info.value(QStringLiteral("daemon")).toString(), QStringLiteral("whatevrd"));
        QCOMPARE(info.value(QStringLiteral("protocol")).toUInt(), quint32(2));
        QCOMPARE(info.value(QStringLiteral("features")).toStringList(),
                 QStringList{QStringLiteral("chats")});
    }

    void subscribeChats()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Subscribe(3, QStringLiteral("chats"),
                                 QJsonObject{{QStringLiteral("filter"), QStringLiteral("direct")},
                                             {QStringLiteral("archived"), true},
                                             {QStringLiteral("limit"), 24}},
                                 &request));
        QCOMPARE(request.id(), quint64(3));
        QVERIFY(request.has_subscribe());
        QCOMPARE(request.subscribe().limit(), quint32(24));
        QVERIFY(request.subscribe().has_chats());
        QCOMPARE(request.subscribe().chats().filter(), whatevr::v2::CHAT_FILTER_DIRECT);
        QVERIFY(request.subscribe().chats().archived());
    }

    void subscribeUnknownView()
    {
        whatevr::v2::Request request;
        QVERIFY(!buildV2Subscribe(1, QStringLiteral("chat_folders"), {}, &request));
        QVERIFY(!buildV2Subscribe(1, QStringLiteral("messages"), {}, &request));
        QVERIFY(!buildV2Subscribe(1, QStringLiteral("chats"),
                                   QJsonObject{{QStringLiteral("filter"), QStringLiteral("unread")}},
                                   &request));
    }

    void extendUnsubscribe()
    {
        whatevr::v2::Request extend;
        buildV2Extend(5, 11, 24, QStringLiteral("older"), &extend);
        QVERIFY(extend.has_extend());
        QCOMPARE(extend.extend().sub(), quint64(11));
        QCOMPARE(extend.extend().count(), quint32(24));
        QCOMPARE(extend.extend().direction(), whatevr::v2::DIRECTION_OLDER);

        whatevr::v2::Request unsub;
        buildV2Unsubscribe(6, 11, &unsub);
        QVERIFY(unsub.has_unsubscribe());
        QCOMPARE(unsub.unsubscribe().sub(), quint64(11));
    }

    void chatRowShape()
    {
        const QJsonObject direct = translateV2ChatRow(chatRow("a@s", whatevr::v2::CHAT_TYPE_DIRECT));
        QCOMPARE(direct.value(QStringLiteral("id")).toString(), QStringLiteral("a@s"));
        QCOMPARE(direct.value(QStringLiteral("is_group")).toBool(), false);
        QCOMPARE(direct.value(QStringLiteral("preview")).toString(), QStringLiteral("hi"));
        QCOMPARE(direct.value(QStringLiteral("last_message_time")).toInt(), 1700000000);
        QCOMPARE(direct.value(QStringLiteral("last_message_direction")).toString(),
                 QStringLiteral("outgoing"));
        QCOMPARE(direct.value(QStringLiteral("last_message_status")).toString(),
                 QStringLiteral("read"));
        QCOMPARE(direct.value(QStringLiteral("unread")).toInt(), 3);

        const QJsonObject group = translateV2ChatRow(chatRow("g", whatevr::v2::CHAT_TYPE_GROUP));
        QCOMPARE(group.value(QStringLiteral("is_group")).toBool(), true);
    }

    void loginRowExpiryParses()
    {
        whatevr::v2::LoginRow row;
        row.set_state(whatevr::v2::LOGIN_STATE_QR);
        row.set_qr("QRDATA");
        row.set_qr_expires_ms(1700000000123LL);
        const QJsonObject item = translateV2LoginRow(row);
        const QVariantMap qr = item.value(QStringLiteral("qr")).toObject().toVariantMap();
        QCOMPARE(qr.value(QStringLiteral("code")).toString(), QStringLiteral("QRDATA"));
        // The QML countdown parses this with Qt::ISODateWithMs.
        const QDateTime parsed =
            QDateTime::fromString(qr.value(QStringLiteral("expires_at")).toString(), Qt::ISODateWithMs);
        QVERIFY(parsed.isValid());
        QCOMPARE(parsed.toMSecsSinceEpoch(), 1700000000123LL);
    }

    void viewUpdateApplies()
    {
        whatevr::v2::ViewUpdate update;
        update.set_reset(true);
        auto *upsert = update.add_changes()->mutable_upsert();
        upsert->set_id("a@s");
        upsert->set_sort("b");
        *upsert->mutable_chat() = chatRow("a@s", whatevr::v2::CHAT_TYPE_DIRECT);
        update.add_changes()->mutable_remove()->set_id("gone");
        update.mutable_ready()->set_exhausted(true);

        StubSink sink;
        applyV2ViewUpdate(update, &sink);
        QCOMPARE(sink.resets, 1);
        QCOMPARE(sink.upserts.size(), 1);
        // Order-preserving hex: "b" sorts after "a".
        QVERIFY(sink.upserts.first().first > v2SortKey("a"));
        QCOMPARE(sink.upserts.first().second.value(QStringLiteral("id")).toString(),
                 QStringLiteral("a@s"));
        QCOMPARE(sink.removed, QList<QString>{QStringLiteral("gone")});
        QCOMPARE(sink.readies.size(), 1);
        QVERIFY(sink.readies.first().first);
    }
};

QTEST_MAIN(TestV2Shim)
#include "tst_v2shim.moc"
