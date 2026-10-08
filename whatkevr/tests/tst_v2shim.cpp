// Unit tests for the v1/v2 compatibility shim: request building, error and
// hello translation, and row shapes the models consume.

#include <QByteArray>
#include <QDateTime>
#include <QJsonArray>
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
        QVERIFY(!buildV2Subscribe(1, QStringLiteral("status"), {}, &request));
        QVERIFY(!buildV2Subscribe(1, QStringLiteral("chats"),
                                   QJsonObject{{QStringLiteral("filter"), QStringLiteral("unread")}},
                                   &request));
    }

    void subscribeMessagesAnchor()
    {
        whatevr::v2::Request latest;
        QVERIFY(buildV2Subscribe(1, QStringLiteral("messages"),
                                 QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")}},
                                 &latest));
        QVERIFY(latest.subscribe().messages().has_latest());

        whatevr::v2::Request unread;
        QVERIFY(buildV2Subscribe(2, QStringLiteral("messages"),
                                 QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                             {QStringLiteral("anchor"), QStringLiteral("unread")}},
                                 &unread));
        QVERIFY(unread.subscribe().messages().has_unread());

        whatevr::v2::Request atMessage;
        QVERIFY(buildV2Subscribe(3, QStringLiteral("messages"),
                                 QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                             {QStringLiteral("anchor"), QStringLiteral("m9")}},
                                 &atMessage));
        QCOMPARE(v2s(atMessage.subscribe().messages().message_id()), QStringLiteral("m9"));
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
    {        whatevr::v2::ViewUpdate update;
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

    void requestUnknownMethod()
    {
        whatevr::v2::Request request;
        QVERIFY(!buildV2Request(1, QStringLiteral("chat_folder.create"), {}, &request));
        QVERIFY(!buildV2Request(1, QStringLiteral("status.post"), {}, &request));
        QVERIFY(!buildV2Request(1, QStringLiteral("daemon.shutdown"), {}, &request));
    }

    void requestSendText()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Request(4, QStringLiteral("send.text"),
                               QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                           {QStringLiteral("text"), QStringLiteral("hi")},
                                           {QStringLiteral("reply_to"), QStringLiteral("m1")},
                                           {QStringLiteral("mentions"),
                                            QJsonArray{QStringLiteral("j@s")}}
                                           },
                               &request));
        QVERIFY(request.has_send_text());
        QCOMPARE(v2s(request.send_text().chat_id()), QStringLiteral("c"));
        QCOMPARE(v2s(request.send_text().text()), QStringLiteral("hi"));
        QCOMPARE(request.send_text().mentions_size(), 1);
        QCOMPARE(v2s(request.send_text().mentions(0).id()), QStringLiteral("j@s"));
    }

    void requestSendPollContactLocation()
    {
        whatevr::v2::Request poll;
        QVERIFY(buildV2Request(10, QStringLiteral("send.poll"),
                               QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                           {QStringLiteral("question"), QStringLiteral("lunch?")},
                                           {QStringLiteral("options"),
                                            QJsonArray{QStringLiteral("a"), QStringLiteral("b")}},
                                           {QStringLiteral("multi"), true}},
                               &poll));
        QVERIFY(poll.has_send_poll());
        QCOMPARE(v2s(poll.send_poll().question()), QStringLiteral("lunch?"));
        QCOMPARE(poll.send_poll().options_size(), 2);
        QVERIFY(poll.send_poll().multi());

        whatevr::v2::Request contact;
        QVERIFY(buildV2Request(11, QStringLiteral("send.contact"),
                               QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                           {QStringLiteral("name"), QStringLiteral("Asha")},
                                           {QStringLiteral("phone"), QStringLiteral("+1555")}},
                               &contact));
        QVERIFY(contact.has_send_contact());
        QCOMPARE(v2s(contact.send_contact().name()), QStringLiteral("Asha"));

        whatevr::v2::Request location;
        QVERIFY(buildV2Request(12, QStringLiteral("send.location"),
                               QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                           {QStringLiteral("lat"), 12.5},
                                           {QStringLiteral("long"), 77.5},
                                           {QStringLiteral("name"), QStringLiteral("office")}},
                               &location));
        QVERIFY(location.has_send_location());
        QCOMPARE(location.send_location().lat(), 12.5);
        QCOMPARE(location.send_location().lng(), 77.5);

        // The reverse trip keeps the v1 names the controller speaks.
        V2RequestV1 decoded;
        QVERIFY(v2RequestToV1(poll, &decoded));
        QCOMPARE(decoded.method, QStringLiteral("send.poll"));
        QVERIFY(v2RequestToV1(contact, &decoded));
        QCOMPARE(decoded.method, QStringLiteral("send.contact"));
        QVERIFY(v2RequestToV1(location, &decoded));
        QCOMPARE(decoded.method, QStringLiteral("send.location"));
        QCOMPARE(decoded.params.value(QStringLiteral("long")).toDouble(), 77.5);
    }

    void requestMuteConvertsToMillis()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Request(5, QStringLiteral("chat.mute"),
                               QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("c")},
                                           {QStringLiteral("muted"), true},
                                           {QStringLiteral("duration_secs"), 60}},
                               &request));
        QCOMPARE(request.chat_mute().duration_ms(), std::int64_t(60000));
    }

    void requestDefaultTimer()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Request(13, QStringLiteral("privacy.set_default_timer"),
                               QJsonObject{{QStringLiteral("seconds"), 604800}},
                               &request));
        QVERIFY(request.has_privacy_set_default_timer());
        QCOMPARE(request.privacy_set_default_timer().seconds(), std::int64_t(604800));

        V2RequestV1 decoded;
        QVERIFY(v2RequestToV1(request, &decoded));
        QCOMPARE(decoded.method, QStringLiteral("privacy.set_default_timer"));
        QCOMPARE(decoded.params.value(QStringLiteral("seconds")).toInt(), 604800);
    }

    void requestPrivacyMapping()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Request(6, QStringLiteral("privacy.set"),
                               QJsonObject{{QStringLiteral("category"), QStringLiteral("about")},
                                           {QStringLiteral("value"), QStringLiteral("contacts")}},
                               &request));
        QCOMPARE(request.privacy_set().category(), whatevr::v2::PRIVACY_CATEGORY_ABOUT);
        QCOMPARE(request.privacy_set().value(), whatevr::v2::PRIVACY_VALUE_CONTACTS);

        whatevr::v2::Request receipts;
        QVERIFY(buildV2Request(7, QStringLiteral("privacy.set"),
                               QJsonObject{{QStringLiteral("category"),
                                            QStringLiteral("read_receipts")},
                                           {QStringLiteral("value"), false}},
                               &receipts));
        QCOMPARE(receipts.privacy_set().value(), whatevr::v2::PRIVACY_VALUE_NOBODY);

        whatevr::v2::Request bad;
        QVERIFY(!buildV2Request(8, QStringLiteral("privacy.set"),
                                QJsonObject{{QStringLiteral("category"), QStringLiteral("nope")},
                                            {QStringLiteral("value"), QStringLiteral("all")}},
                                &bad));
    }

    void requestPreferencesPresence()
    {
        whatevr::v2::Request request;
        QVERIFY(buildV2Request(9, QStringLiteral("preferences.set"),
                               QJsonObject{{QStringLiteral("notifications_enabled"), false}},
                               &request));
        QVERIFY(request.preferences_set().has_notifications());
        QVERIFY(!request.preferences_set().notifications());
        QVERIFY(!request.preferences_set().has_notification_sound());
    }

    void responseResults()
    {
        whatevr::v2::Response older;
        older.set_id(3);
        older.mutable_chat_request_older()->set_requested(true);
        const V2ResponseTranslation translated = translateV2Response(older);
        QVERIFY(!translated.isError());
        QVERIFY(translated.result.value(QStringLiteral("requested")).toBool());

        whatevr::v2::Response failed;
        failed.set_id(4);
        failed.mutable_error()->set_code(whatevr::v2::ERROR_CODE_NOT_FOUND);
        failed.mutable_error()->set_message("nope");
        const V2ResponseTranslation problem = translateV2Response(failed);
        QVERIFY(problem.isError());
        QCOMPARE(problem.errorCode, QStringLiteral("not_found"));
    }
};

QTEST_MAIN(TestV2Shim)
#include "tst_v2shim.moc"
