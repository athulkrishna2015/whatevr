// Unit tests for the whatevr protocol client core (D1): the ProtocolClient
// transport/dispatcher and the generic collection/object view models, exercised
// end-to-end against an in-process fake daemon speaking real v2 frames over a
// real Unix socket. No GUI, no daemon binary.

#include <QCoreApplication>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTest>
#include <QTimer>
#include <functional>
#include <memory>

#include <utility>

#include "collectionviewmodel.h"
#include "objectviewmodel.h"
#include "protocolclient.h"
#include "v2codec.h"
#include "whatevr/v2/frame.pb.h"

using namespace whatevr::proto;

namespace
{
// A minimal, programmable daemon stand-in. It auto-answers `hello`, assigns
// `sub` ids to `subscribe`, and acks everything else, while letting the test
// push arbitrary view events down to the client.
class FakeDaemon : public QObject
{
    Q_OBJECT

public:
    explicit FakeDaemon(QString path, QObject *parent = nullptr)
        : QObject(parent)
        , m_server(new QLocalServer(this))
    {
        QLocalServer::removeServer(path);
        m_server->listen(path);
        connect(m_server, &QLocalServer::newConnection, this, [this] {
            m_conn = m_server->nextPendingConnection();
            connect(m_conn, &QLocalSocket::readyRead, this, &FakeDaemon::onReadyRead);
        });
    }

    void sendUpdate(int sub, const whatevr::v2::ViewUpdate &update)
    {
        whatevr::v2::Frame frame;
        auto *out = frame.mutable_event()->mutable_update();
        *out = update;
        out->set_sub(static_cast<std::uint64_t>(sub));
        writeFrame(frame);
    }

    void sendUpsert(int sub, const QString &sort, const QJsonObject &item)
    {
        whatevr::v2::ViewUpdate update;
        auto *upsert = update.add_changes()->mutable_upsert();
        upsert->set_id(item.value(QStringLiteral("id")).toString().toStdString());
        upsert->set_sort(sort.toUtf8().toStdString());
        auto *chat = upsert->mutable_chat();
        chat->set_id(item.value(QStringLiteral("id")).toString().toStdString());
        chat->set_name(item.value(QStringLiteral("name")).toString().toStdString());
        sendUpdate(sub, update);
    }

    void sendRemove(int sub, const QString &id)
    {
        whatevr::v2::ViewUpdate update;
        update.add_changes()->mutable_remove()->set_id(id.toStdString());
        sendUpdate(sub, update);
    }

    void sendReady(int sub, bool exhausted, bool includeFlag = true)
    {
        Q_UNUSED(includeFlag);
        whatevr::v2::ViewUpdate update;
        update.mutable_ready()->set_exhausted(exhausted);
        sendUpdate(sub, update);
    }

    // Several frames in one write, so the client sees them as a single drain.
    // Two separate writes usually coalesce, but "usually" is not a test.
    void writeTogether(const QList<QByteArray> &frames)
    {
        QByteArray payload;
        for (const QByteArray &frame : frames) {
            payload += frame;
        }
        if (m_conn && m_conn->state() == QLocalSocket::ConnectedState) {
            m_conn->write(payload);
        }
    }

    static QByteArray upsertFrame(int sub, const QString &sort, const QJsonObject &item)
    {
        whatevr::v2::Frame frame;
        auto *update = frame.mutable_event()->mutable_update();
        update->set_sub(static_cast<std::uint64_t>(sub));
        auto *upsert = update->add_changes()->mutable_upsert();
        upsert->set_id(item.value(QStringLiteral("id")).toString().toStdString());
        upsert->set_sort(sort.toUtf8().toStdString());
        auto *chat = upsert->mutable_chat();
        chat->set_id(item.value(QStringLiteral("id")).toString().toStdString());
        chat->set_name(item.value(QStringLiteral("name")).toString().toStdString());
        return encodeV2Frame(frame);
    }

    void sendReset(int sub)
    {
        whatevr::v2::ViewUpdate update;
        update.set_reset(true);
        sendUpdate(sub, update);
    }

    void sendOpenChat(const QString &chatId)
    {
        whatevr::v2::Frame frame;
        frame.mutable_event()->mutable_open_chat()->set_chat_id(chatId.toStdString());
        writeFrame(frame);
    }

    // Subscribe metadata the fake returns for the next subscribe.
    QJsonObject nextSubscribeMeta;
    // Records of what the client sent, for assertions.
    QString lastMethod;
    QJsonObject lastSubscribeParams;
    int lastExtendCount = 0;
    QString lastExtendDirection;
    int subscribeCount = 0;
    int unsubscribeCount = 0;
    bool rejectNextExtend = false;
    bool holdNextSubscribe = false;
    // Accept the connection and never answer the handshake.
    bool swallowHello = false;
    // Answer the handshake, then never answer anything else.
    bool swallowOthers = false;

    void releaseHeldSubscribe()
    {
        if (m_heldSubscribeId == 0) {
            return;
        }
        const int sub = ++subscribeCount;
        whatevr::v2::Response response;
        response.set_id(std::exchange(m_heldSubscribeId, quint64(0)));
        auto *result = response.mutable_subscribe();
        result->set_sub(static_cast<std::uint64_t>(sub));
        if (nextSubscribeMeta.contains(QStringLiteral("anchor_id"))) {
            result->set_anchor_id(nextSubscribeMeta.value(QStringLiteral("anchor_id"))
                                      .toString()
                                      .toStdString());
        }
        writeResponse(response);
        Q_EMIT subscribed(sub);
    }

    void replyHello(quint64 id)
    {
        whatevr::v2::Response response;
        response.set_id(id);
        auto *hello = response.mutable_hello();
        hello->set_daemon("whatevrd");
        hello->set_version("0.7.0");
        hello->set_protocol(2);
        writeResponse(response);
    }

    void reply(quint64 id)
    {
        whatevr::v2::Response response;
        response.set_id(id);
        response.mutable_done();
        writeResponse(response);
    }

    void error(quint64 id, const QString &code, const QString &message)
    {
        whatevr::v2::Response response;
        response.set_id(id);
        auto *error = response.mutable_error();
        if (code == QLatin1String("invalid_params")) {
            error->set_code(whatevr::v2::ERROR_CODE_INVALID_PARAMS);
        } else {
            error->set_code(whatevr::v2::ERROR_CODE_INTERNAL);
        }
        error->set_message(message.toStdString());
        writeResponse(response);
    }

    void writeResponse(const whatevr::v2::Response &response)
    {
        whatevr::v2::Frame frame;
        *frame.mutable_response() = response;
        writeFrame(frame);
    }

    void writeFrame(const whatevr::v2::Frame &frame)
    {
        if (m_conn && m_conn->state() == QLocalSocket::ConnectedState) {
            m_conn->write(encodeV2Frame(frame));
        }
    }

Q_SIGNALS:
    void helloSwallowed();
    void subscribed(int sub);
    void extended();
    void subscribeHeld();

private:
    void onReadyRead()
    {
        m_buf += m_conn->readAll();
        for (;;) {
            whatevr::v2::Frame frame;
            const V2DecodeResult decoded = popV2Frame(m_buf, &frame);
            if (decoded != V2DecodeResult::Frame) {
                break;
            }
            if (frame.has_request()) {
                handleRequest(frame.request());
            }
        }
    }

    void handleRequest(const whatevr::v2::Request &request)
    {
        using Method = whatevr::v2::Request::MethodCase;
        const quint64 id = request.id();
        switch (request.method_case()) {
        case Method::kHello:
            lastMethod = QStringLiteral("hello");
            if (swallowHello) {
                Q_EMIT helloSwallowed();
                return;
            }
            replyHello(id);
            return;
        case Method::kSubscribe: {
            const auto &subscribe = request.subscribe();
            lastMethod = QStringLiteral("subscribe");
            lastSubscribeParams = subscribeParamsJson(subscribe);
            if (std::exchange(holdNextSubscribe, false)) {
                m_heldSubscribeId = id;
                Q_EMIT subscribeHeld();
                return;
            }
            const int sub = ++subscribeCount;
            whatevr::v2::Response response;
            response.set_id(id);
            auto *result = response.mutable_subscribe();
            result->set_sub(static_cast<std::uint64_t>(sub));
            if (nextSubscribeMeta.contains(QStringLiteral("anchor_id"))) {
                result->set_anchor_id(nextSubscribeMeta.value(QStringLiteral("anchor_id"))
                                          .toString()
                                          .toStdString());
            }
            writeResponse(response);
            Q_EMIT subscribed(sub);
            return;
        }
        case Method::kExtend: {
            const auto &extend = request.extend();
            lastMethod = QStringLiteral("extend");
            lastExtendCount = static_cast<int>(extend.count());
            lastExtendDirection = extend.direction() == whatevr::v2::DIRECTION_NEWER
                ? QStringLiteral("newer")
                : QStringLiteral("older");
            if (std::exchange(rejectNextExtend, false)) {
                error(id, QStringLiteral("invalid_params"), QStringLiteral("bad direction"));
            } else {
                reply(id);
            }
            Q_EMIT extended();
            return;
        }
        case Method::kUnsubscribe:
            lastMethod = QStringLiteral("unsubscribe");
            ++unsubscribeCount;
            reply(id);
            return;
        default:
            break;
        }
        if (swallowOthers) {
            return;
        }
        lastMethod = QStringLiteral("other");
        reply(id);
    }

    // The v2 subscribe back to the v1 params the assertions read.
    static QJsonObject subscribeParamsJson(const whatevr::v2::Subscribe &subscribe)
    {
        QJsonObject params;
        params.insert(QStringLiteral("limit"), static_cast<qint64>(subscribe.limit()));
        using View = whatevr::v2::Subscribe::ViewCase;
        switch (subscribe.view_case()) {
        case View::kChats:
            params.insert(QStringLiteral("view"), QStringLiteral("chats"));
            params.insert(QStringLiteral("archived"), subscribe.chats().archived());
            break;
        case View::kMessages:
            params.insert(QStringLiteral("view"), QStringLiteral("messages"));
            break;
        default:
            params.insert(QStringLiteral("view"), QStringLiteral("other"));
            break;
        }
        return params;
    }

    QLocalServer *m_server;
    QLocalSocket *m_conn = nullptr;
    QByteArray m_buf;
    quint64 m_heldSubscribeId = 0;
};

// What the eviction test below watches. It outlives the sink it describes, so a
// sink destroyed mid-drain can still be asked what happened to it.
struct BatchLog {
    int victimBegan = 0;
    int victimEnded = 0;
    int survivorEnded = 0;
    bool victimDestroyed = false;
};

// A sink that runs an arbitrary action the first time it is given a row. Stands
// in for a handler that reacts to an event by evicting a warm window.
class ActingSink final : public ViewSink
{
public:
    std::function<void()> action;
    std::shared_ptr<BatchLog> log;

    void onUpsert(const QString &, const QJsonObject &) override
    {
        if (action) {
            auto once = std::exchange(action, {});
            once();
        }
    }
    void onRemove(const QString &) override {}
    void onReady(bool, bool) override {}
    void onReset() override {}
    void onBatchEnd() override
    {
        if (log) {
            ++log->survivorEnded;
        }
    }
};

// The sink the action above destroys, part way through the drain that batched it.
class VictimSink final : public ViewSink
{
public:
    std::shared_ptr<BatchLog> log;

    ~VictimSink() override
    {
        if (log) {
            log->victimDestroyed = true;
        }
    }
    void onUpsert(const QString &, const QJsonObject &) override {}
    void onRemove(const QString &) override {}
    void onReady(bool, bool) override {}
    void onReset() override {}
    void onBatchBegin() override
    {
        if (log) {
            ++log->victimBegan;
        }
    }
    void onBatchEnd() override
    {
        if (log) {
            ++log->victimEnded;
        }
    }
};

QJsonObject item(const QString &id, const QString &name)
{
    return QJsonObject{{QStringLiteral("id"), id}, {QStringLiteral("name"), name}};
}
} // namespace

class TestProtocolCore : public QObject
{
    Q_OBJECT

private Q_SLOTS:
    void init()
    {
        m_dir = new QTemporaryDir;
        m_path = m_dir->filePath(QStringLiteral("test.sock"));
        m_daemon = new FakeDaemon(m_path);
        m_client = new ProtocolClient(m_path, QStringLiteral("test"));
    }

    void cleanup()
    {
        delete m_client;
        delete m_daemon;
        delete m_dir;
        m_client = nullptr;
        m_daemon = nullptr;
        m_dir = nullptr;
    }

    void helloHandshake()
    {
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());
        QVERIFY(m_client->isReady());
        QCOMPARE(m_client->serverInfo().value(QStringLiteral("daemon")).toString(),
                 QStringLiteral("whatevrd"));
    }

    void collectionFillIsSortedAndReady()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("chats"));

        const int sub = waitForSub();
        // Deliver out of sort order; the model must render sorted ascending.
        m_daemon->sendUpsert(sub, QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        m_daemon->sendUpsert(sub, QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        m_daemon->sendUpsert(sub, QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cy")));
        m_daemon->sendReady(sub, true);

        QTRY_COMPARE(model.count(), 3);
        QTRY_VERIFY(model.isReady());
        QVERIFY(model.isExhausted());
        QCOMPARE(rowName(model, 0), QStringLiteral("Ann"));
        QCOMPARE(rowName(model, 1), QStringLiteral("Bob"));
        QCOMPARE(rowName(model, 2), QStringLiteral("Cy"));
        // itemById exposes the full row map.
        QCOMPARE(model.itemById(QStringLiteral("2")).value(QStringLiteral("name")).toString(),
                 QStringLiteral("Bob"));
    }

    void upsertReplaceInPlace()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("chats"));
        const int sub = waitForSub();
        m_daemon->sendUpsert(sub, QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        QTRY_COMPARE(model.count(), 1);
        // Same sort key, new data: replace, no reorder, no count change.
        m_daemon->sendUpsert(sub, QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Annie")));
        QTRY_COMPARE(rowName(model, 0), QStringLiteral("Annie"));
        QCOMPARE(model.count(), 1);
    }

    void upsertWithChangedSortMoves()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("chats"));
        const int sub = waitForSub();
        m_daemon->sendUpsert(sub, QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        m_daemon->sendUpsert(sub, QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        m_daemon->sendUpsert(sub, QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cy")));
        QTRY_COMPARE(model.count(), 3);
        // Move Ann to the end by changing its sort key.
        m_daemon->sendUpsert(sub, QStringLiteral("z"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        QTRY_COMPARE(rowName(model, 2), QStringLiteral("Ann"));
        QCOMPARE(rowName(model, 0), QStringLiteral("Bob"));
        QCOMPARE(rowName(model, 1), QStringLiteral("Cy"));
        QCOMPARE(model.count(), 3);
        QCOMPARE(model.indexOfId(QStringLiteral("1")), 2);
    }

    void removeAndReset()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("chats"));
        const int sub = waitForSub();
        m_daemon->sendUpsert(sub, QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        m_daemon->sendUpsert(sub, QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        m_daemon->sendReady(sub, false);
        QTRY_COMPARE(model.count(), 2);

        m_daemon->sendRemove(sub, QStringLiteral("1"));
        QTRY_COMPARE(model.count(), 1);
        QCOMPARE(rowName(model, 0), QStringLiteral("Bob"));
        QCOMPARE(model.indexOfId(QStringLiteral("1")), -1);

        // reset discards the local copy; ready falls back to false.
        m_daemon->sendReset(sub);
        QTRY_COMPARE(model.count(), 0);
        QVERIFY(!model.isReady());
    }

    // An observer that reacts to a row signal by looking rows up by id must see
    // an index that already matches the list. ProtocolMessageModel does exactly
    // this for the `transfers` view, and before the index was repaired inside
    // the begin/end pair the last removal (a finished download) left it
    // pointing at a row that no longer existed — QList::at aborted.
    void idIndexIsConsistentInsideRowSignals()
    {
        CollectionViewModel model;
        QStringList mismatches;
        const auto audit = [&] {
            for (int row = 0; row < model.rowCount(); ++row) {
                const QString id = model.data(model.index(row), CollectionViewModel::IdRole).toString();
                if (model.indexOfId(id) != row) {
                    mismatches << QStringLiteral("%1@%2").arg(id).arg(row);
                }
            }
            // Ids the index still knows about must be addressable in the list.
            for (const QString &id : {QStringLiteral("1"), QStringLiteral("2"), QStringLiteral("3")}) {
                const int row = model.indexOfId(id);
                if (row >= 0 && row >= model.rowCount()) {
                    mismatches << QStringLiteral("stale:%1->%2").arg(id).arg(row);
                }
            }
        };
        connect(&model, &QAbstractItemModel::rowsInserted, this, audit);
        connect(&model, &QAbstractItemModel::rowsRemoved, this, audit);
        connect(&model, &QAbstractItemModel::rowsMoved, this, audit);

        // Driven without a batch bracket, so each call applies on the spot and
        // emits its own row signals — the case this audits.
        model.onUpsert(QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onUpsert(QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cid")));
        // Re-sort the first row to the end: the move must re-key before it emits.
        model.onUpsert(QStringLiteral("z"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onRemove(QStringLiteral("2"));
        model.onRemove(QStringLiteral("3"));
        // The final removal empties the list — the crashing case.
        model.onRemove(QStringLiteral("1"));

        QCOMPARE(mismatches, QStringList());
        QCOMPARE(model.count(), 0);
        QCOMPARE(model.itemById(QStringLiteral("1")), QVariantMap());
    }

    // A fill arrives as one upsert per item. Applying each on arrival cost a
    // model transaction — and a QML layout pass — per message, which is what
    // made opening a chat stall. Everything delivered in one drain must land as
    // a single contiguous insert.
    void batchedFillIsOneInsert()
    {
        CollectionViewModel model;
        QList<QPair<int, int>> inserts;
        connect(&model, &QAbstractItemModel::rowsInserted, this,
                [&](const QModelIndex &, int first, int last) { inserts.append({first, last}); });

        // Delivered out of order, as the daemon may emit them.
        model.onBatchBegin();
        model.onUpsert(QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cy")));
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onUpsert(QStringLiteral("d"), item(QStringLiteral("4"), QStringLiteral("Dee")));
        model.onUpsert(QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        QCOMPARE(inserts.size(), 0); // nothing applied until the batch closes
        model.onBatchEnd();

        QCOMPARE(inserts.size(), 1);
        QCOMPARE(inserts.first(), qMakePair(0, 3));
        QCOMPARE(model.count(), 4);
        QCOMPARE(rowName(model, 0), QStringLiteral("Ann"));
        QCOMPARE(rowName(model, 3), QStringLiteral("Dee"));
    }

    // Scrolling up pages older history in. Those rows land ahead of everything
    // already held, and must stay one insert so the view can keep its viewport
    // anchored instead of rebuilding.
    void batchedPrependIsOneInsert()
    {
        CollectionViewModel model;
        model.onBatchBegin();
        model.onUpsert(QStringLiteral("m"), item(QStringLiteral("5"), QStringLiteral("Mid")));
        model.onUpsert(QStringLiteral("n"), item(QStringLiteral("6"), QStringLiteral("New")));
        model.onBatchEnd();
        QCOMPARE(model.count(), 2);

        QList<QPair<int, int>> inserts;
        connect(&model, &QAbstractItemModel::rowsInserted, this,
                [&](const QModelIndex &, int first, int last) { inserts.append({first, last}); });

        model.onBatchBegin();
        model.onUpsert(QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onUpsert(QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cy")));
        model.onBatchEnd();

        QCOMPARE(inserts.size(), 1);
        QCOMPARE(inserts.first(), qMakePair(0, 2));
        QCOMPARE(model.count(), 5);
        QCOMPARE(rowName(model, 0), QStringLiteral("Ann"));
        QCOMPARE(rowName(model, 3), QStringLiteral("Mid"));
        QCOMPARE(model.indexOfId(QStringLiteral("6")), 4);
    }

    // Rows that interleave with what is already held cannot be one run; the
    // model must still place every one of them correctly.
    void batchedInterleavedInsertsStaySorted()
    {
        CollectionViewModel model;
        model.onBatchBegin();
        model.onUpsert(QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        model.onUpsert(QStringLiteral("d"), item(QStringLiteral("4"), QStringLiteral("Dee")));
        model.onBatchEnd();

        model.onBatchBegin();
        model.onUpsert(QStringLiteral("e"), item(QStringLiteral("5"), QStringLiteral("Eve")));
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onUpsert(QStringLiteral("c"), item(QStringLiteral("3"), QStringLiteral("Cy")));
        model.onBatchEnd();

        QCOMPARE(model.count(), 5);
        QCOMPARE(rowName(model, 0), QStringLiteral("Ann"));
        QCOMPARE(rowName(model, 1), QStringLiteral("Bob"));
        QCOMPARE(rowName(model, 2), QStringLiteral("Cy"));
        QCOMPARE(rowName(model, 3), QStringLiteral("Dee"));
        QCOMPARE(rowName(model, 4), QStringLiteral("Eve"));
        for (int row = 0; row < model.rowCount(); ++row) {
            const QString id = model.data(model.index(row), CollectionViewModel::IdRole).toString();
            QCOMPARE(model.indexOfId(id), row);
        }
    }

    // A remove and a re-upsert of the same id inside one batch must resolve to
    // whichever came last on the wire.
    void batchedRemoveThenUpsertKeepsTheRow()
    {
        CollectionViewModel model;
        model.onBatchBegin();
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onBatchEnd();

        model.onBatchBegin();
        model.onRemove(QStringLiteral("1"));
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Annie")));
        model.onBatchEnd();
        QCOMPARE(model.count(), 1);
        QCOMPARE(rowName(model, 0), QStringLiteral("Annie"));

        model.onBatchBegin();
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onRemove(QStringLiteral("1"));
        model.onBatchEnd();
        QCOMPARE(model.count(), 0);
    }

    // `ready` closes a fill, so anything still buffered has to be in the model
    // before it is announced — a handler reacting to readyReceived reads the
    // rows straight away.
    void readyFlushesPendingRows()
    {
        CollectionViewModel model;
        int countAtReady = -1;
        connect(&model, &CollectionViewModel::readyReceived, this,
                [&](bool) { countAtReady = model.count(); });

        model.onBatchBegin();
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onUpsert(QStringLiteral("b"), item(QStringLiteral("2"), QStringLiteral("Bob")));
        model.onReady(true, true);

        QCOMPARE(countAtReady, 2);
        QVERIFY(model.isReady());
    }

    // A reset discards the copy being rebuilt; rows buffered for it must go too.
    void resetDropsBufferedRows()
    {
        CollectionViewModel model;
        model.onBatchBegin();
        model.onUpsert(QStringLiteral("a"), item(QStringLiteral("1"), QStringLiteral("Ann")));
        model.onReset();
        model.onBatchEnd();
        QCOMPARE(model.count(), 0);
    }

    void readyWithoutFlagIsNotExhausted()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("chats"));
        const int sub = waitForSub();
        m_daemon->sendReady(sub, false, /*includeFlag=*/false);
        QTRY_VERIFY(model.isReady());
        QVERIFY(!model.isExhausted());
    }

    void everyReadyCompletionIsObservable()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("messages"));
        const int sub = waitForSub();
        QSignalSpy completionSpy(&model, &CollectionViewModel::readyReceived);

        m_daemon->sendReady(sub, false);
        m_daemon->sendReady(sub, false);

        QTRY_COMPARE(completionSpy.count(), 2);
        QCOMPARE(completionSpy.at(0).first().toBool(), false);
        QCOMPARE(completionSpy.at(1).first().toBool(), false);
    }

    void extendCarriesDirection()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("messages"));
        Subscription *sub = m_sub;
        waitForSub();
        QSignalSpy extendSpy(m_daemon, &FakeDaemon::extended);
        sub->extend(25, QStringLiteral("older"));
        QVERIFY(extendSpy.wait());
        QCOMPARE(m_daemon->lastExtendCount, 25);
        QCOMPARE(m_daemon->lastExtendDirection, QStringLiteral("older"));
    }

    void extendFailureIsObservable()
    {
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("messages"));
        Subscription *sub = m_sub;
        waitForSub();
        m_daemon->rejectNextExtend = true;
        QSignalSpy failureSpy(sub, &Subscription::extendFailed);

        sub->extend(25, QStringLiteral("newer"));

        QVERIFY(failureSpy.wait());
        QCOMPARE(failureSpy.first().at(0).toString(), QStringLiteral("invalid_params"));
        QCOMPARE(failureSpy.first().at(1).toString(), QStringLiteral("bad direction"));
    }

    void subscribeMetaIsExposed()
    {
        m_daemon->nextSubscribeMeta = QJsonObject{{QStringLiteral("anchor_id"), QStringLiteral("m42")}};
        CollectionViewModel model;
        connectAndSubscribe(&model, QStringLiteral("messages"));
        QSignalSpy subSpy(m_sub, &Subscription::subscribed);
        QVERIFY(subSpy.wait());
        const QVariantMap meta = subSpy.first().first().toMap();
        QCOMPARE(meta.value(QStringLiteral("anchor_id")).toString(), QStringLiteral("m42"));
        QVERIFY(!meta.contains(QStringLiteral("sub")));
        QCOMPARE(m_sub->meta().value(QStringLiteral("anchor_id")).toString(), QStringLiteral("m42"));
    }

    void staleSubscribeReplyCannotAttachToReplacement()
    {
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());
        CollectionViewModel firstModel;
        CollectionViewModel secondModel;
        m_daemon->holdNextSubscribe = true;
        QSignalSpy heldSpy(m_daemon, &FakeDaemon::subscribeHeld);
        Subscription *first = m_client->subscribe(QStringLiteral("messages"), QJsonObject{}, &firstModel);
        QVERIFY(heldSpy.wait());
        delete first;

        Subscription *second = m_client->subscribe(QStringLiteral("messages"), QJsonObject{}, &secondModel);
        QSignalSpy secondSpy(second, &Subscription::subscribed);
        QVERIFY(secondSpy.wait());
        QVERIFY(second->isActive());

        m_daemon->releaseHeldSubscribe();
        QTRY_COMPARE(m_daemon->unsubscribeCount, 1);
        QVERIFY(second->isActive());
    }

    void objectView()
    {
        ObjectViewModel obj;
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());
        m_sub = m_client->subscribe(QStringLiteral("self"), QJsonObject{}, &obj);
        const int sub = waitForSub();

        QVERIFY(!obj.isPresent());
        m_daemon->sendUpsert(sub, QString(), item(QStringLiteral("self"), QStringLiteral("Me")));
        m_daemon->sendReady(sub, true);
        QTRY_VERIFY(obj.isPresent());
        QVERIFY(obj.isReady());
        QCOMPARE(obj.value().value(QStringLiteral("name")).toString(), QStringLiteral("Me"));

        m_daemon->sendReset(sub);
        QTRY_VERIFY(!obj.isPresent());
        QVERIFY(obj.value().isEmpty());
    }

    void openChatRouted()
    {
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());
        QSignalSpy openSpy(m_client, &ProtocolClient::openChatRequested);
        m_daemon->sendOpenChat(QStringLiteral("123@g.us"));
        QVERIFY(openSpy.wait());
        QCOMPARE(openSpy.first().first().toString(), QStringLiteral("123@g.us"));
    }

    void queuedRequestFlushesAfterHello()
    {
        // A request issued before the connection is ready is queued and sent
        // once hello lands; its callback fires with the ack.
        bool called = false;
        m_client->request(QStringLiteral("chat.pin"),
                          QJsonObject{{QStringLiteral("chat_id"), QStringLiteral("x")}},
                          [&called](const QJsonObject &, const ProtocolError &error) {
                              called = !error.isError();
                          });
        m_client->start();
        QTRY_VERIFY(called);
    }

    // A sink destroyed part way through a drain must not be called at the end of
    // it, and must not take the surviving sinks down with it.
    //
    // The client batches every sink a drain touches and closes them all once the
    // socket buffer is empty, so it holds those pointers across every handler
    // that runs in between. One of those handlers evicting a warm window frees a
    // sink that is already on that list, and closing its batch afterwards reads
    // freed memory. Nothing in the suite could reach this before: it needs two
    // sinks in one drain and a handler that deletes one of them.
    void aSinkDestroyedMidDrainIsNotClosedAfterwards()
    {
        auto log = std::make_shared<BatchLog>();

        ActingSink survivor;
        survivor.log = log;
        auto victim = std::make_unique<VictimSink>();
        victim->log = log;

        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());

        Subscription *survivorSub = m_client->subscribe(QStringLiteral("chats"), QJsonObject{}, &survivor);
        Subscription *victimSub = m_client->subscribe(QStringLiteral("messages"), QJsonObject{}, victim.get());
        QVERIFY(survivorSub);
        QVERIFY(victimSub);
        QTRY_COMPARE(m_daemon->subscribeCount, 2);

        // The eviction: the subscription goes first, then the sink, which is the
        // order ProtocolController tears a warm window down in.
        survivor.action = [&] {
            delete victimSub;
            victimSub = nullptr;
            victim.reset();
        };

        // One write, so one drain. The victim is batched first, then the row
        // that reaches the handler which frees it.
        m_daemon->writeTogether({
            FakeDaemon::upsertFrame(2, QStringLiteral("0001"), item(QStringLiteral("v"), QStringLiteral("victim"))),
            FakeDaemon::upsertFrame(1, QStringLiteral("0001"), item(QStringLiteral("s"), QStringLiteral("survivor"))),
        });

        QTRY_VERIFY(log->victimDestroyed);
        QTRY_COMPARE(log->survivorEnded, 1);

        // The victim opened a batch and was freed before the drain ended, so the
        // close must never have been delivered.
        QCOMPARE(log->victimBegan, 1);
        QVERIFY2(log->victimEnded == 0,
                 "a batch was closed on a sink that had already been destroyed");

        delete survivorSub;
    }

    // A daemon that accepts the socket and then says nothing must not wedge the
    // client for ever.
    //
    // Reconnection only ever ran off a disconnect, and a socket that is open and
    // silent never produces one, so the client sat in Handshaking with every
    // caller queued behind a hello that was never coming.
    void aSilentHandshakeIsGivenUpOnAndRetried()
    {
        m_daemon->swallowHello = true;

        auto *deadline = m_client->findChild<QTimer *>(QStringLiteral("protocolHandshakeTimer"));
        QVERIFY2(deadline, "the client has no handshake deadline");
        deadline->setInterval(150);

        QSignalSpy errorSpy(m_client, &ProtocolClient::errorOccurred);
        QSignalSpy swallowedSpy(m_daemon, &FakeDaemon::helloSwallowed);
        m_client->start();
        QVERIFY(swallowedSpy.wait());
        QVERIFY(!m_client->isReady());

        // The deadline expires and the connection is torn down and retried.
        QTRY_VERIFY_WITH_TIMEOUT(errorSpy.count() > 0, 5000);
        QVERIFY(!m_client->isReady());

        // And the retry actually gets somewhere once the daemon answers again.
        m_daemon->swallowHello = false;
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        QTRY_VERIFY_WITH_TIMEOUT(m_client->isReady() || readySpy.count() > 0, 10000);
        QVERIFY(m_client->isReady());
    }

    // Requests made while the daemon is unreachable are queued, and the queue is
    // bounded. What must never happen is a caller left waiting for a callback
    // that was quietly thrown away.
    void anOverflowingPreHelloQueueAnswersTheCallersItDrops()
    {
        // Never started, so every request below is queued rather than sent.
        int answered = 0;
        int failed = 0;
        const int overflow = 40;
        const int total = 1024 + overflow;
        for (int i = 0; i < total; ++i) {
            m_client->request(QStringLiteral("daemon.reconnect"), QJsonObject{},
                              [&answered, &failed](const QJsonObject &, const ProtocolError &error) {
                                  ++answered;
                                  if (error.isError()) {
                                      ++failed;
                                  }
                              });
        }

        // The oldest give way, and each of them is told so.
        QTRY_COMPARE(answered, overflow);
        QCOMPARE(failed, overflow);
    }

    // The same contract for requests already on the wire: the ceiling refuses
    // the newcomer rather than growing without limit, and answers it.
    void anOverflowingInFlightSetRefusesNewRequestsAudibly()
    {
        m_daemon->swallowOthers = true;
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());

        int answered = 0;
        int failed = 0;
        const auto note = [&answered, &failed](const QJsonObject &, const ProtocolError &error) {
            ++answered;
            if (error.isError()) {
                ++failed;
            }
        };
        // Fill the in-flight set; the daemon answers none of these.
        for (int i = 0; i < 4096; ++i) {
            m_client->request(QStringLiteral("daemon.reconnect"), QJsonObject{}, note);
        }
        QCOMPARE(answered, 0);

        // One past the ceiling is refused, and the refusal reaches its caller.
        m_client->request(QStringLiteral("daemon.reconnect"), QJsonObject{}, note);
        QTRY_COMPARE(answered, 1);
        QCOMPARE(failed, 1);
    }

private:
    void connectAndSubscribe(ViewSink *sink, const QString &view)
    {
        QSignalSpy readySpy(m_client, &ProtocolClient::ready);
        m_client->start();
        QVERIFY(readySpy.wait());
        m_sub = m_client->subscribe(view, QJsonObject{}, sink);
    }

    int waitForSub()
    {
        QSignalSpy subSpy(m_daemon, &FakeDaemon::subscribed);
        if (m_daemon->subscribeCount == 0) {
            subSpy.wait();
        }
        return m_daemon->subscribeCount;
    }

    static QString rowName(const CollectionViewModel &model, int row)
    {
        return model.data(model.index(row), CollectionViewModel::ItemRole)
            .toMap()
            .value(QStringLiteral("name"))
            .toString();
    }

    QTemporaryDir *m_dir = nullptr;
    QString m_path;
    FakeDaemon *m_daemon = nullptr;
    ProtocolClient *m_client = nullptr;
    Subscription *m_sub = nullptr;
};

QTEST_MAIN(TestProtocolCore)
#include "tst_protocolcore.moc"
