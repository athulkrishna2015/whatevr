// DN9 delegate-construction benchmark.
//
// The column slide hitches because materialising a `messages` window builds a
// ChatBubble per row, and a ChatBubble builds far more than the row shows. The
// numbers that matter are therefore per-row *object count* (what the scene
// graph and QML engine have to allocate, bind and lay out) and per-row
// construction wall time. Both are asserted against a budget so a future
// delegate regrowth fails the build instead of quietly returning the stutter.
//
// Object count is the primary metric: it is deterministic, machine-independent
// and is exactly the quantity Layer 1 of DN9 attacks. Wall time is recorded
// too, but its budget is deliberately loose — CI machines vary, and this test
// is built Debug (-O0) like the field-test build it exists to protect.

#include <QApplication>
#include <QDateTime>
#include <QElapsedTimer>
#include <QImage>
#include <QJsonObject>
#include <QQmlComponent>
#include <QQmlEngine>
#include <QQuickItem>
#include <QQuickStyle>
#include <QQuickWindow>
#include <QRegularExpression>
#include <QSignalSpy>
#include <QDir>
#include <QStandardPaths>
#include <QTest>

#include "collectionviewmodel.h"
#include "protocolcontroller.h"
#include "protocolmessagemodel.h"
#include "settings.h"

using whatevr::proto::CollectionViewModel;

namespace
{

// One representative row per rendering path through the delegate.
struct Sample {
    const char *name;
    QVariantMap props;
    // Ceiling on the per-row object tree. Set just above what DN9 measured, so
    // the delegate regrowing is a build failure rather than a slow return of
    // the column-slide stutter. Raise deliberately, never reflexively.
    //
    // Raised by 2 across the board when the card family landed: an inactive
    // Loader costs two objects on every row, and cards (location, and the
    // contact/poll/event kinds after it) share exactly one Loader dispatched by
    // CardBubble. That is why it is a one-time +2 and not +2 per kind.
    //
    // Raised by a further 1 for the row's containment mask. The row's
    // right-click MouseArea covers everything and, being hover-enabled, took
    // hover away from every button and field inside a card; the mask cuts the
    // card's rectangle out of it. One object per row buys working hover for
    // every card kind there will ever be, so it is a one-time +1 as well.
    int maxObjects;
};

// ChatBubble declares every model role it renders as a `required property`, so
// a delegate cannot be built without all of them — exactly the contract
// ListView satisfies from the model. This mirrors that contract, which is also
// why the benchmark catches a role being renamed out from under the view.
QVariantMap baseProps()
{
    return {
        // View state (not model roles).
        {QStringLiteral("listWidth"), 900},
        {QStringLiteral("readMoreTextWidth"), 60},

        // Model roles.
        {QStringLiteral("messageId"), QStringLiteral("m0")},
        {QStringLiteral("timeText"), QStringLiteral("14:22")},
        {QStringLiteral("dateSeparatorText"), QString()},
        {QStringLiteral("status"), 0},
        {QStringLiteral("isOutgoing"), false},
        {QStringLiteral("senderName"), QStringLiteral("Aditi")},
        {QStringLiteral("senderAvatarLocalPath"), QString()},
        {QStringLiteral("senderInitials"), QStringLiteral("A")},
        {QStringLiteral("showSenderHeader"), false},
        {QStringLiteral("showSenderAvatar"), false},
        {QStringLiteral("showSenderGutter"), false},
        {QStringLiteral("groupStart"), true},
        {QStringLiteral("groupEnd"), true},
        {QStringLiteral("mediaKind"), QString()},
        {QStringLiteral("mediaMimeType"), QString()},
        {QStringLiteral("mediaLocalPath"), QString()},
        {QStringLiteral("mediaThumbnailLocalPath"), QString()},
        {QStringLiteral("mediaCacheKey"), QString()},
        {QStringLiteral("mediaWidth"), 0},
        {QStringLiteral("mediaHeight"), 0},
        {QStringLiteral("mediaAnimated"), false},
        {QStringLiteral("mediaSizeBytes"), 0.0},
        {QStringLiteral("mediaDurationSecs"), 0},
        {QStringLiteral("mediaFileName"), QString()},
        {QStringLiteral("mediaPageCount"), 0},
        {QStringLiteral("mediaWaveform"), QVariantList()},
        {QStringLiteral("mediaPlayed"), false},
        // A kind is not a promise of bytes: rows whose kind has nothing to
        // fetch leave this false, and the media samples below set it.
        {QStringLiteral("hasMedia"), false},
        {QStringLiteral("isKept"), false},
        {QStringLiteral("location"), QVariantMap()},
        {QStringLiteral("liveShare"), QVariantMap()},
        {QStringLiteral("contacts"), QVariantMap()},
        {QStringLiteral("poll"), QVariantMap()},
        {QStringLiteral("invite"), QVariantMap()},
        {QStringLiteral("eventInfo"), QVariantMap()},
        {QStringLiteral("album"), QVariantMap()},
        {QStringLiteral("isRevoked"), false},
        {QStringLiteral("isEdited"), false},
        {QStringLiteral("isStarred"), false},
        {QStringLiteral("isPinned"), false},
        {QStringLiteral("mediaDownloading"), false},
        {QStringLiteral("mediaDownloadError"), QString()},
        {QStringLiteral("mediaDownloadProgress"), -1.0},
        {QStringLiteral("replyToMessageId"), QString()},
        {QStringLiteral("replyToSenderName"), QString()},
        {QStringLiteral("replyToText"), QString()},
        {QStringLiteral("replyToMediaKind"), QString()},
        {QStringLiteral("replyToMediaMimeType"), QString()},
        {QStringLiteral("replyToIsOutgoing"), false},
        {QStringLiteral("widestLineWidth"), 0.0},
        {QStringLiteral("lastLineWidth"), 0.0},
        {QStringLiteral("reactions"), QVariantList()},
        {QStringLiteral("text"), QString()},
        {QStringLiteral("textPreview"), QString()},
        {QStringLiteral("layoutText"), QString()},
        {QStringLiteral("layoutTextPreview"), QString()},
        {QStringLiteral("hasRichText"), false},
        {QStringLiteral("previewHasRichText"), false},
        {QStringLiteral("richText"), QString()},
        {QStringLiteral("previewRichText"), QString()},
        {QStringLiteral("emojiOnlyCount"), 0},
        {QStringLiteral("textTruncated"), false},
    };
}

QVariantMap withProps(QVariantMap base, const QVariantMap &extra)
{
    for (auto it = extra.cbegin(); it != extra.cend(); ++it) {
        base.insert(it.key(), it.value());
    }
    return base;
}

// Counts the whole object tree the delegate owns, including everything its
// Loaders instantiated.
int objectCount(QObject *root)
{
    return 1 + root->findChildren<QObject *>(Qt::FindChildrenRecursively).size();
}

// findChild walks QObject parentage, and a Repeater gives its delegates a
// visual parent without a QObject one, so anything a Repeater built is
// invisible to it. Walking childItems finds the whole rendered tree.
QQuickItem *findVisualChild(QQuickItem *root, const QString &name)
{
    if (!root) {
        return nullptr;
    }
    const auto children = root->childItems();
    for (QQuickItem *child : children) {
        if (child->objectName() == name) {
            return child;
        }
        if (QQuickItem *found = findVisualChild(child, name)) {
            return found;
        }
    }
    return nullptr;
}

// The bottom-most edge of anything the item draws, in the item's own
// coordinates. A card reports its height to the row, and the row clips and
// positions everything else from it, so content reaching past that height is
// content hanging outside its own card.
qreal deepestBottom(QQuickItem *root, QQuickItem *item)
{
    qreal bottom = 0;
    const auto children = item->childItems();
    for (QQuickItem *child : children) {
        if (!child->isVisible() || child->width() <= 0 || child->height() <= 0) {
            continue;
        }
        const QPointF corner = child->mapToItem(root, QPointF(0, child->height()));
        bottom = std::max(bottom, corner.y());
        bottom = std::max(bottom, deepestBottom(root, child));
    }
    return bottom;
}

} // namespace

class ChatBubblePerf : public QObject
{
    Q_OBJECT

private Q_SLOTS:
    void initTestCase();
    void cleanupTestCase();
    void delegateCost_data();
    void delegateCost();
    void idleVideoDefersItsBackendAndUsesASharpPoster();
    void rectangularVideoStreamsOnceAndLatchesItsSource();
    void rejectedStreamFallsBackAndStartsDownloadedFile();
    void videoDelegateReuseClearsPlaybackState();
    void endOfFileReturnsToThePosterAndCanReplay();
    void tappingAPollOptionReachesTheController();
    void cardButtonsReceiveHover();
    void pollVotersDialogReadsTheVotesBothWays();
    void groupInviteCardAnswersWhereYouAlreadyStand();
    void eventCardShowsWhoIsComingAndAnswersOnTheTap();
    void albumMosaicTilesTheWholeWidthWithoutOverlapping();
    void eventResponsesDialogNamesEveryoneWhoAnswered();
    void aShrinkingPaneDoesNotDragTheTranscriptWithIt();

private:
    QQuickWindow *m_window = nullptr;
    QQuickItem *m_host = nullptr;
    QQmlEngine *m_engine = nullptr;
    std::unique_ptr<Settings> m_settings;
    std::unique_ptr<ProtocolController> m_controller;
};

void ChatBubblePerf::initTestCase()
{
    // The QML singletons assert on a live instance, and both must exist before
    // the first `Whatevr.Settings` / `Whatevr.ProtocolController` resolution.
    // The socket path is deliberately dead: the controller must not need a
    // daemon to hand the delegate its preference/emoji-font reads.
    m_settings = std::make_unique<Settings>(nullptr);
    Settings::setInstance(m_settings.get());

    const QString deadSocket =
        QDir::temp().filePath(QStringLiteral("whatevr-dn9-benchmark-absent.sock"));
    m_controller = std::make_unique<ProtocolController>(deadSocket, nullptr);
    ProtocolController::setInstance(m_controller.get());

    m_engine = new QQmlEngine(this);
    m_window = new QQuickWindow();
    m_window->resize(1000, 800);
    m_host = new QQuickItem(m_window->contentItem());
    m_host->setWidth(900);
    m_host->setHeight(700);
}

void ChatBubblePerf::cleanupTestCase()
{
    delete m_window;
    m_window = nullptr;
    ProtocolController::setInstance(nullptr);
    Settings::setInstance(nullptr);
}

void ChatBubblePerf::delegateCost_data()
{
    QTest::addColumn<QVariantMap>("props");
    // Budgets are ceilings, not targets: they exist to catch regrowth.
    QTest::addColumn<int>("maxObjects");

    const QString shortBody = QStringLiteral("see you then");
    const QString longBody = QStringLiteral(
        "the whole point of this row is that it wraps across several lines so "
        "the body text actually has to lay out more than one line of content");

    // The text rows below are each one object up on their old ceiling, and all
    // for the same reason: the media slot's audio loader now picks between a
    // voice note's row and a shared audio file's, so it carries a Component for
    // each instead of one inline. Two loaders would have cost twice that on
    // every delegate in the chat, this one included.
    const QList<Sample> samples = {
        {"plain-text-incoming",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m1")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("status"), 4}}), 64},
        {"plain-text-outgoing",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m2")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("isOutgoing"), true},
                    {QStringLiteral("status"), 4}}), 71},
        {"multiline-text",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m3")},
                    {QStringLiteral("text"), longBody},
                    {QStringLiteral("layoutText"), longBody},
                    {QStringLiteral("status"), 3}}), 64},
        {"text-with-reply",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m4")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("replyToMessageId"), QStringLiteral("m1")},
                    {QStringLiteral("replyToSenderName"), QStringLiteral("Aditi")},
                    {QStringLiteral("replyToText"), shortBody}}), 94},
        {"text-with-sender-header",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m5")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("showSenderHeader"), true},
                    {QStringLiteral("showSenderAvatar"), true},
                    {QStringLiteral("showSenderGutter"), true}}), 96},
        {"image",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m6")},
                    {QStringLiteral("mediaKind"), QStringLiteral("image")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("image/jpeg")},
                    {QStringLiteral("mediaWidth"), 1280},
                    {QStringLiteral("mediaHeight"), 720}}), 124},
        {"video",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m6-video")},
                    {QStringLiteral("mediaKind"), QStringLiteral("video")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
                    {QStringLiteral("mediaWidth"), 1280},
                    {QStringLiteral("mediaHeight"), 720},
                    // Raised from 135 when video rows took on the same bottom
                    // vignette photos use (a loader, a gradient and its two
                    // stops, less the pill it replaced) and the bubble gained
                    // the two grace timers that keep a handoff and a scroll
                    // from flashing a spinner or a stale poster.
                    {QStringLiteral("mediaDurationSecs"), 12}}), 143},
        {"sticker",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m7")},
                    {QStringLiteral("mediaKind"), QStringLiteral("sticker")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("image/webp")}}), 154},
        // A voice note and a video note are the two kinds whose layout is not a
        // picture: one is a fixed-height row inside the bubble, the other a
        // frameless circle with no bubble at all. Both are here so the cost of
        // their subtrees is tracked, and because constructing them at all is
        // what catches a delegate that silently renders nothing.
        {"voice",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m8")},
                    {QStringLiteral("mediaKind"), QStringLiteral("voice")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("audio/ogg")},
                    {QStringLiteral("mediaDurationSecs"), 6}}), 137},
        // The other audio row: a shared track, which is a squared-off tile, a
        // filename and a plain seek line rather than a disc and a waveform.
        {"audio-file",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m8-audio")},
                    {QStringLiteral("mediaKind"), QStringLiteral("audio")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("audio/mpeg")},
                    {QStringLiteral("mediaFileName"), QStringLiteral("Interstellar - Main.mp3")},
                    {QStringLiteral("mediaSizeBytes"), 4.2 * 1024 * 1024},
                    {QStringLiteral("mediaDurationSecs"), 204}}), 143},
        {"video-note",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m9")},
                    {QStringLiteral("mediaKind"), QStringLiteral("video_note")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
                    {QStringLiteral("mediaWidth"), 480},
                    {QStringLiteral("mediaHeight"), 480},
                    {QStringLiteral("mediaDurationSecs"), 11}}), 192},
    };

    for (const Sample &s : samples) {
        QTest::newRow(s.name) << s.props << s.maxObjects;
    }
}

void ChatBubblePerf::delegateCost()
{
    QFETCH(QVariantMap, props);
    QFETCH(int, maxObjects);

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));

    // Warm the component cache and the shared type data so the measured run
    // times construction, not first-time compilation.
    {
        std::unique_ptr<QObject> warm(component.createWithInitialProperties(props));
        QVERIFY2(warm, qPrintable(component.errorString()));
        if (auto *item = qobject_cast<QQuickItem *>(warm.get())) {
            item->setParentItem(m_host);
            // Touch the geometry so every layout binding is forced to evaluate.
            (void)item->height();
        }
    }

    constexpr int kRows = 120;
    QList<QObject *> built;
    built.reserve(kRows);

    QElapsedTimer timer;
    timer.start();
    for (int i = 0; i < kRows; ++i) {
        QObject *obj = component.createWithInitialProperties(props);
        QVERIFY2(obj, qPrintable(component.errorString()));
        if (auto *item = qobject_cast<QQuickItem *>(obj)) {
            item->setParentItem(m_host);
            (void)item->height();
        }
        built.append(obj);
    }
    const qint64 elapsedUs = timer.nsecsElapsed() / 1000;

    const int objects = objectCount(built.first());
    const double usPerRow = double(elapsedUs) / kRows;

    qInfo("DN9 %-24s objects/row=%3d  construct=%7.1f us/row", QTest::currentDataTag(), objects,
          usPerRow);

    // WHATEVR_DN9_DUMP=1 prints the per-row object tree as a class histogram,
    // which is how you find what a row is actually paying for.
    if (qEnvironmentVariableIsSet("WHATEVR_DN9_DUMP")) {
        QMap<QString, int> histogram;
        const QList<QObject *> all =
            built.first()->findChildren<QObject *>(Qt::FindChildrenRecursively);
        for (const QObject *o : all) {
            QString cls = QString::fromLatin1(o->metaObject()->className());
            cls.remove(QLatin1String("QQuick"));
            cls.replace(QRegularExpression(QStringLiteral("_QMLTYPE_\\d+")), QString());
            cls.replace(QRegularExpression(QStringLiteral("_QML_\\d+")), QString());
            histogram[cls] += 1;
        }
        QStringList lines;
        for (auto it = histogram.cbegin(); it != histogram.cend(); ++it) {
            lines << QStringLiteral("%1x %2").arg(it.value(), 3).arg(it.key());
        }
        qInfo().noquote() << "DN9 tree" << QTest::currentDataTag() << "\n  "
                          << lines.join(QStringLiteral("\n  "));
    }

    qDeleteAll(built);

    QVERIFY2(objects <= maxObjects,
             qPrintable(QStringLiteral("%1: %2 objects per row exceeds the budget of %3 — the "
                                       "delegate regrew; see MIGRATION.md DN9")
                            .arg(QLatin1StringView(QTest::currentDataTag()))
                            .arg(objects)
                            .arg(maxObjects)));
}

void ChatBubblePerf::idleVideoDefersItsBackendAndUsesASharpPoster()
{
    QTemporaryDir mediaDir;
    QVERIFY(mediaDir.isValid());
    QImage thumbnail(1280, 720, QImage::Format_RGB32);
    for (int y = 0; y < thumbnail.height(); ++y) {
        const int green = 55 + (y * 150 / thumbnail.height());
        for (int x = 0; x < thumbnail.width(); ++x) {
            const int red = 35 + (x * 180 / thumbnail.width());
            thumbnail.setPixelColor(x, y, QColor(red, green, 190));
        }
    }
    const QString thumbnailPath = mediaDir.filePath(QStringLiteral("video-poster.png"));
    QVERIFY(thumbnail.save(thumbnailPath));

    QVariantMap props = withProps(
        baseProps(),
        {{QStringLiteral("messageId"), QStringLiteral("video-idle")},
         {QStringLiteral("mediaKind"), QStringLiteral("video")},
         {QStringLiteral("hasMedia"), true},
         {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
         {QStringLiteral("mediaLocalPath"), QStringLiteral("/tmp/whatkevr-video.mp4")},
         {QStringLiteral("mediaThumbnailLocalPath"), thumbnailPath},
         {QStringLiteral("mediaWidth"), 1280},
         {QStringLiteral("mediaHeight"), 720},
         {QStringLiteral("mediaDurationSecs"), 12}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *videoBubble = bubble->findChild<QObject *>(QStringLiteral("videoBubble"));
    QObject *backendLoader = bubble->findChild<QObject *>(QStringLiteral("videoSurface.backendLoader"));
    QObject *poster = bubble->findChild<QObject *>(QStringLiteral("videoBubble.poster"));
    QVERIFY(videoBubble);
    QVERIFY(backendLoader);
    QVERIFY(poster);

    QCOMPARE(videoBubble->property("phase").toString(), QStringLiteral("idle"));
    QVERIFY(backendLoader->property("item").value<QObject *>() == nullptr);

    const QSize posterDecode = poster->property("sourceSize").toSize();
    QCOMPARE(posterDecode.width(), bubble->property("imageDecodeWidth").toInt());
    QCOMPARE(posterDecode.height(), bubble->property("imageDecodeHeight").toInt());
    QVERIFY(posterDecode.width() > bubble->property("thumbnailDecodeWidth").toInt());

    // Materialize the playing presentation without needing a codec fixture. The
    // backend interface is deliberately writable, so the test can report the
    // same first-frame boundary that mpv and Qt Multimedia report at runtime.
    QVERIFY(QMetaObject::invokeMethod(videoBubble, "beginPlayback"));
    QObject *backend = backendLoader->property("item").value<QObject *>();
    QVERIFY(backend);
    QVERIFY(backend->setProperty("surfaceDuration", 12.0));
    QVERIFY(backend->setProperty("surfacePosition", 4.0));
    QVERIFY(backend->setProperty("surfaceHasFrame", true));
    QTRY_COMPARE(videoBubble->property("phase").toString(), QStringLiteral("playing"));

    QVERIFY(!bubble->findChild<QQuickItem *>(QStringLiteral("videoBubble.transportStrip")));
    QObject *fullscreen = bubble->findChild<QObject *>(QStringLiteral("videoBubble.fullscreenButton"));
    QObject *audio = bubble->findChild<QObject *>(QStringLiteral("videoBubble.audioButton"));
    QVERIFY(fullscreen);
    QVERIFY(audio);
    QVERIFY(fullscreen->property("visible").toBool());
    QVERIFY(audio->property("visible").toBool());
    QCOMPARE(backend->property("muted").toBool(), true);
    QVERIFY(QMetaObject::invokeMethod(audio, "clicked"));
    QCOMPARE(backend->property("muted").toBool(), false);

    const QString screenshotPath = qEnvironmentVariable("WHATKEVR_DN20_SCREENSHOT");
    if (!screenshotPath.isEmpty()) {
        m_window->show();
        QTest::qWait(100);
        QVERIFY(m_window->grabWindow().save(screenshotPath));
    }
}

void ChatBubblePerf::rectangularVideoStreamsOnceAndLatchesItsSource()
{
    QVariantMap props = withProps(
        baseProps(),
        {{QStringLiteral("messageId"), QStringLiteral("video-stream")},
         {QStringLiteral("mediaKind"), QStringLiteral("video")},
         {QStringLiteral("hasMedia"), true},
         {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
         {QStringLiteral("mediaWidth"), 1280},
         {QStringLiteral("mediaHeight"), 720},
         {QStringLiteral("mediaDurationSecs"), 12},
         {QStringLiteral("mediaSizeBytes"), 1024.0}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *videoBubble = bubble->findChild<QObject *>(QStringLiteral("videoBubble"));
    QObject *backendLoader = bubble->findChild<QObject *>(QStringLiteral("videoSurface.backendLoader"));
    QVERIFY(videoBubble);
    QVERIFY(backendLoader);
    QSignalSpy requestSpy(videoBubble, SIGNAL(playbackRequestDispatched(QString)));

    QVERIFY(QMetaObject::invokeMethod(videoBubble, "activate"));
    QCOMPARE(requestSpy.count(), 1);
    QCOMPARE(requestSpy.first().first().toString(), QStringLiteral("stream"));
    QCOMPARE(videoBubble->property("requestPending").toBool(), true);
    QCOMPARE(videoBubble->property("phase").toString(), QStringLiteral("buffering"));
    QVERIFY(QMetaObject::invokeMethod(videoBubble, "requestPlayback"));
    QCOMPARE(requestSpy.count(), 1);

    const QUrl streamUrl(QStringLiteral("http://127.0.0.1:7777/media/video-stream?t=test"));
    QVERIFY(QMetaObject::invokeMethod(m_controller.get(), "mediaStreamReady", Qt::DirectConnection,
                                      Q_ARG(QString, QStringLiteral("video-stream")),
                                      Q_ARG(QString, QStringLiteral("stream-video-stream")),
                                      Q_ARG(QUrl, streamUrl)));
    QCOMPARE(videoBubble->property("requestPending").toBool(), false);
    QCOMPARE(videoBubble->property("playbackSource").toUrl(), streamUrl);

    QObject *backend = backendLoader->property("item").value<QObject *>();
    QVERIFY(backend);
    QVERIFY(backend->setProperty("surfaceHasFrame", true));
    QTRY_COMPARE(videoBubble->property("phase").toString(), QStringLiteral("playing"));

    // Transfer progress must not cover a frame that is already decoded.
    QVERIFY(bubble->setProperty("mediaDownloading", true));
    QCOMPARE(videoBubble->property("phase").toString(), QStringLiteral("playing"));

    // Publishing media.path promotes future sessions only. This active one
    // stays on the stream URL and therefore does not restart.
    QVERIFY(bubble->setProperty("mediaLocalPath", QStringLiteral("/tmp/video-stream.mp4")));
    QCOMPARE(videoBubble->property("playbackSource").toUrl(), streamUrl);
    QCOMPARE(requestSpy.count(), 1);

    QObject *surface = bubble->findChild<QObject *>(QStringLiteral("videoBubble.surface"));
    QVERIFY(surface);
    QVERIFY(QMetaObject::invokeMethod(surface, "endOfFile"));
    QCOMPARE(videoBubble->property("playbackSource").toUrl(),
             QUrl::fromLocalFile(QStringLiteral("/tmp/video-stream.mp4")));
}

void ChatBubblePerf::rejectedStreamFallsBackAndStartsDownloadedFile()
{
    QVariantMap props = withProps(
        baseProps(),
        {{QStringLiteral("messageId"), QStringLiteral("video-fallback")},
         {QStringLiteral("mediaKind"), QStringLiteral("video")},
         {QStringLiteral("hasMedia"), true},
         {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
         {QStringLiteral("mediaWidth"), 1280},
         {QStringLiteral("mediaHeight"), 720}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *videoBubble = bubble->findChild<QObject *>(QStringLiteral("videoBubble"));
    QObject *backendLoader = bubble->findChild<QObject *>(QStringLiteral("videoSurface.backendLoader"));
    QVERIFY(videoBubble);
    QVERIFY(backendLoader);
    QSignalSpy requestSpy(videoBubble, SIGNAL(playbackRequestDispatched(QString)));

    QVERIFY(QMetaObject::invokeMethod(videoBubble, "activate"));
    QVERIFY(QMetaObject::invokeMethod(m_controller.get(), "mediaStreamFailed", Qt::DirectConnection,
                                      Q_ARG(QString, QStringLiteral("video-fallback")),
                                      Q_ARG(QString, QStringLiteral("ranges unsupported"))));
    QCOMPARE(requestSpy.count(), 2);
    QCOMPARE(requestSpy.at(0).first().toString(), QStringLiteral("stream"));
    QCOMPARE(requestSpy.at(1).first().toString(), QStringLiteral("download"));

    QVERIFY(bubble->setProperty("mediaDownloading", true));
    QCOMPARE(videoBubble->property("requestPending").toBool(), false);
    QVERIFY(bubble->setProperty("mediaLocalPath", QStringLiteral("/tmp/video-fallback.mp4")));
    QVERIFY(bubble->setProperty("mediaDownloading", false));
    QCOMPARE(videoBubble->property("playbackSource").toUrl(),
             QUrl::fromLocalFile(QStringLiteral("/tmp/video-fallback.mp4")));

    QObject *backend = backendLoader->property("item").value<QObject *>();
    QVERIFY(backend);
    QVERIFY(backend->setProperty("surfaceHasFrame", true));
    QTRY_COMPARE(videoBubble->property("phase").toString(), QStringLiteral("playing"));
}

void ChatBubblePerf::videoDelegateReuseClearsPlaybackState()
{
    QVariantMap props = withProps(
        baseProps(),
        {{QStringLiteral("messageId"), QStringLiteral("video-old")},
         {QStringLiteral("mediaKind"), QStringLiteral("video")},
         {QStringLiteral("hasMedia"), true},
         {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
         {QStringLiteral("mediaWidth"), 1280},
         {QStringLiteral("mediaHeight"), 720}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *videoBubble = bubble->findChild<QObject *>(QStringLiteral("videoBubble"));
    QVERIFY(videoBubble);
    QVERIFY(videoBubble->setProperty("intent", QStringLiteral("playing")));
    QVERIFY(videoBubble->setProperty("streamUrl", QUrl(QStringLiteral(
                                                     "http://127.0.0.1/media/video-old"))));
    QVERIFY(videoBubble->setProperty("sessionSource", QUrl(QStringLiteral(
                                                         "http://127.0.0.1/media/video-old"))));
    QVERIFY(videoBubble->setProperty("requestPending", true));
    QVERIFY(videoBubble->setProperty("playAfterDownload", true));
    QVERIFY(videoBubble->setProperty("retryDownloadOnly", true));
    QVERIFY(videoBubble->setProperty("streamFailed", true));
    QVERIFY(videoBubble->setProperty("stalledOut", true));
    QVERIFY(videoBubble->setProperty("userMuted", true));

    QVERIFY(bubble->setProperty("messageId", QStringLiteral("video-new")));
    QCoreApplication::processEvents();

    QCOMPARE(videoBubble->property("intent").toString(), QStringLiteral("stopped"));
    QCOMPARE(videoBubble->property("streamUrl").toUrl(), QUrl());
    QCOMPARE(videoBubble->property("sessionSource").toUrl(), QUrl());
    QCOMPARE(videoBubble->property("requestPending").toBool(), false);
    QCOMPARE(videoBubble->property("playAfterDownload").toBool(), false);
    QCOMPARE(videoBubble->property("retryDownloadOnly").toBool(), false);
    QCOMPARE(videoBubble->property("streamFailed").toBool(), false);
    QCOMPARE(videoBubble->property("stalledOut").toBool(), false);
    QCOMPARE(videoBubble->property("playbackFailed").toBool(), false);
    QCOMPARE(videoBubble->property("userMuted").toBool(), true);
}

void ChatBubblePerf::endOfFileReturnsToThePosterAndCanReplay()
{
    QVariantMap props = withProps(
        baseProps(),
        {{QStringLiteral("messageId"), QStringLiteral("video-replay")},
         {QStringLiteral("mediaKind"), QStringLiteral("video_note")},
         {QStringLiteral("hasMedia"), true},
         {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
         {QStringLiteral("mediaLocalPath"), QStringLiteral("/tmp/whatkevr-video-note.mp4")},
         {QStringLiteral("mediaWidth"), 480},
         {QStringLiteral("mediaHeight"), 480},
         {QStringLiteral("mediaDurationSecs"), 5}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *videoBubble = bubble->findChild<QObject *>(QStringLiteral("videoBubble"));
    QObject *surface = bubble->findChild<QObject *>(QStringLiteral("videoBubble.surface"));
    QObject *backendLoader = bubble->findChild<QObject *>(QStringLiteral("videoSurface.backendLoader"));
    QVERIFY(videoBubble);
    QVERIFY(surface);
    QVERIFY(backendLoader);

    QVERIFY(QMetaObject::invokeMethod(videoBubble, "beginPlayback"));
    QCOMPARE(videoBubble->property("intent").toString(), QStringLiteral("playing"));
    QVERIFY(backendLoader->property("item").value<QObject *>() != nullptr);
    QCOMPARE(backendLoader->property("item").value<QObject *>()->property("muted").toBool(), false);

    // EOF must release and destroy the exhausted backend, then expose an
    // ordinary Play state.
    QVERIFY(QMetaObject::invokeMethod(surface, "endOfFile"));
    QCOMPARE(videoBubble->property("intent").toString(), QStringLiteral("stopped"));
    QCOMPARE(videoBubble->property("phase").toString(), QStringLiteral("idle"));
    QVERIFY(backendLoader->property("item").value<QObject *>() == nullptr);

    QVERIFY(QMetaObject::invokeMethod(videoBubble, "activate"));
    QCOMPARE(videoBubble->property("intent").toString(), QStringLiteral("playing"));
    QVERIFY(backendLoader->property("item").value<QObject *>() != nullptr);
}

// A poll row's tap goes through QML into ProtocolController. Nothing in C++
// references that call, so a missing or renamed method is not a build error:
// it is a runtime TypeError, and the poll silently stops working. Driving the
// real bubble against the real controller is what notices.
void ChatBubblePerf::tappingAPollOptionReachesTheController()
{
    QVariantMap poll{
        {QStringLiteral("question"), QStringLiteral("Where for dinner?")},
        {QStringLiteral("selectable_count"), 1},
        {QStringLiteral("total_voters"), 0},
        {QStringLiteral("options"),
         QVariantList{QVariantMap{{QStringLiteral("index"), 0}, {QStringLiteral("name"), QStringLiteral("Thai")}},
                      QVariantMap{{QStringLiteral("index"), 1}, {QStringLiteral("name"), QStringLiteral("Pizza")}}}},
    };

    const QVariantMap props = withProps(baseProps(),
                                        {{QStringLiteral("messageId"), QStringLiteral("poll-1")},
                                         {QStringLiteral("mediaKind"), QStringLiteral("poll")},
                                         {QStringLiteral("poll"), poll}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    qobject_cast<QQuickItem *>(bubble.get())->setParentItem(m_host);

    QObject *pollBubble = bubble->findChild<QObject *>(QStringLiteral("pollBubble"));
    QVERIFY2(pollBubble, "the poll kind did not reach a poll bubble");

    QVERIFY(!m_controller->pendingPollSelection(QStringLiteral("poll-1")).isValid());

    QVERIFY(QMetaObject::invokeMethod(pollBubble, "toggle", Q_ARG(QVariant, QVariant(1))));
    QCOMPARE(m_controller->pendingPollSelection(QStringLiteral("poll-1")).toList(),
             (QVariantList{1}));

    // Tapping the same answer again takes the vote back rather than sending it
    // twice: the wire carries a whole selection, and an empty one is how a
    // voter withdraws. The bubble reads its own pending echo to know that.
    QVERIFY(QMetaObject::invokeMethod(pollBubble, "toggle", Q_ARG(QVariant, QVariant(1))));
    QVERIFY(m_controller->pendingPollSelection(QStringLiteral("poll-1")).toList().isEmpty());
}

// Cards put real buttons inside a message row, and a row is a dense stack of
// pointer surfaces: a right-click MouseArea covering everything, text edits
// that want the I-beam, handlers for tap-to-reply. A button that never sees a
// hover event looks like a label, which is what these cards shipped as.
void ChatBubblePerf::cardButtonsReceiveHover()
{
    QVariantMap contacts{
        {QStringLiteral("display_name"), QStringLiteral("Shared contact")},
        {QStringLiteral("cards"),
         QVariantList{QVariantMap{
             {QStringLiteral("display_name"), QStringLiteral("Ana Costa")},
             {QStringLiteral("vcard"), QStringLiteral("BEGIN:VCARD\nEND:VCARD")},
             {QStringLiteral("phones"),
              QVariantList{QVariantMap{{QStringLiteral("value"), QStringLiteral("+91 70600 29183")},
                                       {QStringLiteral("label"), QStringLiteral("Mobile")},
                                       {QStringLiteral("jid"), QStringLiteral("917060029183@s.whatsapp.net")}}}}}}},
    };

    const QVariantMap props = withProps(baseProps(),
                                        {{QStringLiteral("messageId"), QStringLiteral("card-1")},
                                         {QStringLiteral("mediaKind"), QStringLiteral("contact")},
                                         {QStringLiteral("contacts"), contacts}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *buttonItem = findVisualChild(bubbleItem, QStringLiteral("cardActionButton"));
    QVERIFY2(buttonItem, "the contact card built no action button");
    QTRY_VERIFY(buttonItem->width() > 0 && buttonItem->height() > 0);

    QVERIFY(!buttonItem->property("highlighted").toBool());

    const QPointF centre =
        buttonItem->mapToScene(QPointF(buttonItem->width() / 2, buttonItem->height() / 2));
    QTest::mouseMove(m_window, centre.toPoint());
    QTRY_VERIFY2(buttonItem->property("highlighted").toBool(),
                 "a button inside a card never saw the pointer: something above it in the row "
                 "is swallowing hover");

    // And it lets go again, so the highlight does not stick to the last button
    // the pointer crossed.
    QTest::mouseMove(m_window, QPoint(2, 2));
    QTRY_VERIFY(!buttonItem->property("highlighted").toBool());

    // The card must be tall enough for what it drew. A card that insets its
    // content from the top but reports only the content's height puts its last
    // row on the bottom edge, with that row's hover plate outside the card.
    QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("contactCardBubble"));
    QVERIFY(card);
    const qreal padding = card->property("contentMargin").toReal();
    QVERIFY(padding > 0);
    QQuickItem *content = findVisualChild(card, QStringLiteral("cardContent"));
    QVERIFY(content);
    const qreal bottom = content->mapToItem(card, QPointF(0, content->height())).y();
    QVERIFY2(card->height() - bottom >= padding - 0.5,
             qPrintable(QStringLiteral("content ends %1 from the card's bottom, want %2 of padding")
                            .arg(card->height() - bottom)
                            .arg(padding)));
    // And nothing inside the content spills past it either.
    QVERIFY2(deepestBottom(content, content) <= content->height() + 0.5,
             "something inside the card reaches past the content block");

    m_window->hide();
    bubbleItem->setParentItem(nullptr);
}

// The wire gives a poll answer-first: each option carrying its voters. That
// answers "what is winning" and cannot answer "what did Bo pick", which in a
// poll allowing several answers is the more useful question. The dialog turns
// the table on its side to get there, and that regrouping is the one piece of
// real logic in it.
void ChatBubblePerf::pollVotersDialogReadsTheVotesBothWays()
{
    const QVariantMap ana{{QStringLiteral("jid"), QStringLiteral("ana@s")},
                          {QStringLiteral("name"), QStringLiteral("Ana")},
                          {QStringLiteral("timestamp"), 200}};
    const QVariantMap bo{{QStringLiteral("jid"), QStringLiteral("bo@s")},
                         {QStringLiteral("name"), QStringLiteral("Bo")},
                         {QStringLiteral("timestamp"), 100}};
    const QVariantMap me{{QStringLiteral("jid"), QStringLiteral("me@s")},
                         {QStringLiteral("from_me"), true},
                         {QStringLiteral("timestamp"), 300}};

    const QVariantMap poll{
        {QStringLiteral("question"), QStringLiteral("Which days work?")},
        {QStringLiteral("selectable_count"), 3},
        {QStringLiteral("total_voters"), 3},
        {QStringLiteral("options"),
         QVariantList{
             QVariantMap{{QStringLiteral("index"), 0},
                         {QStringLiteral("name"), QStringLiteral("Mon")},
                         {QStringLiteral("voters"), QVariantList{bo, ana, me}}},
             QVariantMap{{QStringLiteral("index"), 1},
                         {QStringLiteral("name"), QStringLiteral("Tue")},
                         {QStringLiteral("voters"), QVariantList{ana}}},
             QVariantMap{{QStringLiteral("index"), 2},
                         {QStringLiteral("name"), QStringLiteral("Wed")},
                         {QStringLiteral("voters"), QVariantList{}}}}},
    };

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/PollVotersDialog.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> dialog(component.create());
    QVERIFY2(dialog, qPrintable(component.errorString()));

    dialog->setProperty("poll", poll);

    const QVariantList people = dialog->property("people").toList();
    QCOMPARE(people.size(), 3);

    // Ourselves first, then whoever answered earliest: the reader is looking
    // for their own answer, and after that for the order people arrived in.
    QCOMPARE(people.at(0).toMap().value(QStringLiteral("jid")).toString(), QStringLiteral("me@s"));
    QCOMPARE(people.at(1).toMap().value(QStringLiteral("jid")).toString(), QStringLiteral("bo@s"));
    QCOMPARE(people.at(2).toMap().value(QStringLiteral("jid")).toString(), QStringLiteral("ana@s"));

    // Ana chose two days, and both of them travel with her rather than being
    // split across two rows the way the wire has them.
    const QVariantList anaChoices =
        people.at(2).toMap().value(QStringLiteral("choices")).toList();
    QCOMPARE(anaChoices.size(), 2);
    QCOMPARE(anaChoices.at(0).toString(), QStringLiteral("Mon"));
    QCOMPARE(anaChoices.at(1).toString(), QStringLiteral("Tue"));

    // An answer nobody chose is still an answer, and the option-first reading
    // keeps it: "nobody picked Wednesday" is a result.
    QCOMPARE(dialog->property("visibleOptions").toList().size(), 3);

    // Opening on one answer narrows to it, and only in the option-first reading.
    dialog->setProperty("filterIndex", 1);
    const QVariantList narrowed = dialog->property("visibleOptions").toList();
    QCOMPARE(narrowed.size(), 1);
    QCOMPARE(narrowed.at(0).toMap().value(QStringLiteral("name")).toString(), QStringLiteral("Tue"));
}

// A group invite's card has one button and three different jobs for it: join a
// group, open one you are already in, or nothing at all once the code has
// lapsed. Getting that wrong offers an action that cannot work, which is the
// one failure mode a card like this has.
void ChatBubblePerf::groupInviteCardAnswersWhereYouAlreadyStand()
{
    const qint64 now = QDateTime::currentSecsSinceEpoch();
    const QVariantMap open{{QStringLiteral("group_jid"), QStringLiteral("1203630001@g.us")},
                           {QStringLiteral("code"), QStringLiteral("CODE123")},
                           {QStringLiteral("name"), QStringLiteral("sender's copy")},
                           {QStringLiteral("subject"), QStringLiteral("Wow3")},
                           {QStringLiteral("member_count"), 12},
                           {QStringLiteral("resolved_at"), now},
                           {QStringLiteral("expires_at"), now + 3600 * 30}};

    const QVariantMap props = withProps(baseProps(),
                                        {{QStringLiteral("messageId"), QStringLiteral("inv-1")},
                                         {QStringLiteral("mediaKind"), QStringLiteral("group_invite")},
                                         {QStringLiteral("invite"), open}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("groupInviteBubble"));
    QVERIFY2(card, "a group invite row built no invite card");
    QTRY_VERIFY(card->width() > 0 && card->height() > 0);

    // The resolved subject beats the name the sender's client happened to hold.
    QCOMPARE(card->property("displayName").toString(), QStringLiteral("Wow3"));
    QVERIFY(!card->property("expired").toBool());
    QVERIFY(!card->property("joined").toBool());
    // 30 hours out reads in days, not in 1800 minutes.
    QVERIFY2(card->property("remainingText").toString().contains(QStringLiteral("1")),
             qPrintable(card->property("remainingText").toString()));

    QQuickItem *button = findVisualChild(card, QStringLiteral("cardActionButton"));
    QVERIFY2(button, "an invite that can still be accepted offered no way to accept it");
    QVERIFY(button->isVisible());
    const QString joinText = button->property("text").toString();
    QVERIFY2(!joinText.isEmpty(), "the invite's action has no label");

    // The card is meant to be looked at, and the three states differ in more
    // than a word. Point this at a directory prefix to get all three out.
    const QString shotPrefix = qEnvironmentVariable("WHATKEVR_INVITE_SCREENSHOT");
    const auto shoot = [&](const QString &state) {
        if (shotPrefix.isEmpty()) {
            return;
        }
        QTest::qWait(120);
        QVERIFY(m_window->grabWindow().save(shotPrefix + state + QStringLiteral(".png")));
    };
    shoot(QStringLiteral("open"));

    // The card must be tall enough for what it drew, like every other card.
    const qreal padding = card->property("contentMargin").toReal();
    QVERIFY(padding > 0);
    QQuickItem *content = findVisualChild(card, QStringLiteral("cardContent"));
    QVERIFY(content);
    const qreal bottom = content->mapToItem(card, QPointF(0, content->height())).y();
    QVERIFY2(card->height() - bottom >= padding - 0.5,
             qPrintable(QStringLiteral("content ends %1 from the card's bottom, want %2 of padding")
                            .arg(card->height() - bottom)
                            .arg(padding)));

    // Already a member: there is nothing to join, so the button changes what it
    // says rather than doing nothing when pressed.
    QVariantMap joined = open;
    joined[QStringLiteral("joined")] = true;
    bubbleItem->setProperty("invite", joined);
    QTRY_VERIFY(card->property("joined").toBool());
    QTRY_VERIFY2(button->property("text").toString() != joinText,
                 "a group already joined still offered to join it");
    shoot(QStringLiteral("joined"));

    // Lapsed: the door is closed and the card stops offering to walk through it.
    QVariantMap expired = open;
    expired[QStringLiteral("expires_at")] = now - 1;
    bubbleItem->setProperty("invite", expired);
    QTRY_VERIFY(card->property("expired").toBool());
    QTRY_VERIFY2(!button->isVisible(), "an expired invite still offered its Join button");

    // With the action row gone the header is the card's last line, and the row
    // draws the time and ticks over exactly that corner. The text has to give
    // way, or the timestamp lands on top of "This invite has expired".
    QQuickItem *subtitle = findVisualChild(card, QStringLiteral("inviteSubtitle"));
    QVERIFY(subtitle);
    const qreal reserve = bubbleItem->property("tntReserveWidth").toReal();
    QVERIFY2(reserve > 0, "the row reserved no room for its own footer");
    // Waited on rather than read once: the visibility flip and the relayout it
    // causes are two different passes of the event loop.
    const auto rightGap = [&] {
        return card->width() - subtitle->mapToItem(card, QPointF(subtitle->width(), 0)).x();
    };
    QTRY_VERIFY2(rightGap() >= reserve - 0.5,
                 qPrintable(QStringLiteral("the last line ends %1 from the card's right edge, and "
                                           "the footer needs %2")
                                .arg(rightGap())
                                .arg(reserve)));
    shoot(QStringLiteral("expired"));

    m_window->hide();
    bubbleItem->setParentItem(nullptr);
}

// An event's card has to answer three things a count cannot: when it is, who is
// coming, and which chip is ours. The chip has to light on the tap rather than
// one round trip later, which is the part that needs the controller.
void ChatBubblePerf::eventCardShowsWhoIsComingAndAnswersOnTheTap()
{
    const qint64 start = QDateTime::currentSecsSinceEpoch() + 3600 * 26;
    const QVariantMap ana{{QStringLiteral("jid"), QStringLiteral("ana@s")},
                          {QStringLiteral("name"), QStringLiteral("Ana")},
                          {QStringLiteral("response"), QStringLiteral("going")}};
    const QVariantMap bo{{QStringLiteral("jid"), QStringLiteral("bo@s")},
                         {QStringLiteral("name"), QStringLiteral("Bo")},
                         {QStringLiteral("response"), QStringLiteral("going")},
                         {QStringLiteral("extra_guests"), 2}};
    const QVariantMap cy{{QStringLiteral("jid"), QStringLiteral("cy@s")},
                         {QStringLiteral("name"), QStringLiteral("Cy")},
                         {QStringLiteral("response"), QStringLiteral("not_going")}};

    const QVariantMap plan{
        {QStringLiteral("name"), QStringLiteral("Team dinner")},
        {QStringLiteral("starts_at"), start},
        {QStringLiteral("ends_at"), start + 7200},
        {QStringLiteral("responders"), QVariantList{ana, bo, cy}},
        // Heads, not answers: Bo brings two, so four people are at the door.
        {QStringLiteral("going_count"), 4},
    };

    const QVariantMap props = withProps(baseProps(),
                                        {{QStringLiteral("messageId"), QStringLiteral("ev-1")},
                                         {QStringLiteral("mediaKind"), QStringLiteral("event")},
                                         {QStringLiteral("eventInfo"), plan}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("eventBubble"));
    QVERIFY2(card, "an event row built no event card");
    QTRY_VERIFY(card->width() > 0 && card->height() > 0);

    // The calendar leaf is what a reader finds without reading.
    QVERIFY2(findVisualChild(card, QStringLiteral("calendarLeaf")), "the event card drew no date");
    QVERIFY(!card->property("past").toBool());
    QVERIFY(card->property("answerable").toBool());
    QVERIFY2(!card->property("whenText").toString().isEmpty(), "the event card said nothing about when");

    // Three chips, and the counts split by answer rather than by head count.
    QQuickItem *chips = findVisualChild(card, QStringLiteral("rsvpChips"));
    QVERIFY(chips);
    QCOMPARE(card->property("selfResponse").toString(), QString());

    const auto chipCount = [&](const QString &response) {
        QVariant out;
        QMetaObject::invokeMethod(card, "chipCount", Q_RETURN_ARG(QVariant, out),
                                  Q_ARG(QVariant, response));
        return out.toInt();
    };
    // Two people said yes and one said no. The chips count answers; the line
    // under them counts heads, and the two are deliberately different numbers.
    QCOMPARE(chipCount(QStringLiteral("going")), 2);
    QCOMPARE(chipCount(QStringLiteral("not_going")), 1);
    QCOMPARE(chipCount(QStringLiteral("maybe")), 0);
    QCOMPARE(card->property("goingCount").toInt(), 4);

    // Answering paints now, not when the daemon echoes: the chip reads its own
    // answer out of the controller's in-flight map.
    m_controller->respondToEvent(QStringLiteral("ev-1"), QStringLiteral("going"), 0);
    QTRY_COMPARE(card->property("selfResponse").toString(), QStringLiteral("going"));
    // And our own pending answer is counted, so the chip does not read one
    // short until the echo arrives.
    QCOMPARE(chipCount(QStringLiteral("going")), 3);

    // The answer we have given stops taking taps. There is no way to withdraw
    // an RSVP on the wire, and the chip that tried to offer one did it by
    // quietly answering "maybe" instead.
    QQuickItem *chosen = nullptr;
    for (QQuickItem *child : chips->childItems()) {
        if (child->objectName() == QLatin1String("eventRSVPChip")
            && child->property("chosen").toBool()) {
            chosen = child;
        }
    }
    QVERIFY2(chosen, "no chip showed our own answer");
    QCOMPARE(chosen->property("response").toString(), QStringLiteral("going"));
    QVERIFY2(!chosen->property("interactive").toBool(),
             "the answer already given still offered to be tapped");

    // It still has three faces and a count on it, though, and that is a
    // question. A chip with no answer left to give answers that one instead.
    QVERIFY2(chosen->property("showsDetail").toBool(),
             "the answered chip went dead rather than offering its own list");
    QSignalSpy detail(bubble.get(), SIGNAL(eventResponsesRequested(QString)));
    QVERIFY(QMetaObject::invokeMethod(chosen, "detailRequested"));
    QCOMPARE(detail.count(), 1);
    QCOMPARE(detail.at(0).at(0).toString(), QStringLiteral("going"));

    // And the count under the chips opens the whole list, which is the one way
    // in that does not depend on having already answered.
    QQuickItem *summary = findVisualChild(card, QStringLiteral("rsvpSummary"));
    QVERIFY2(summary, "the card counts the answers but offers no way to read them");
    QTRY_VERIFY(summary->isVisible() && summary->width() > 0);
    QTest::mouseClick(m_window, Qt::LeftButton, {},
                      summary->mapToScene(QPointF(summary->width() / 2, summary->height() / 2))
                          .toPoint());
    QTRY_COMPARE(detail.count(), 2);
    QCOMPARE(detail.at(1).at(0).toString(), QString());

    // Only the answered chip carries faces, so it is the tallest thing in the
    // row. All three have to match it, or the row reads as three unrelated
    // buttons of three different sizes.
    QList<QQuickItem *> chipItems;
    for (QQuickItem *child : chips->childItems()) {
        if (child->objectName() == QLatin1String("eventRSVPChip")) {
            chipItems.append(child);
        }
    }
    QCOMPARE(chipItems.size(), 3);
    QTRY_VERIFY(chipItems.at(0)->height() > 0);
    for (QQuickItem *chip : std::as_const(chipItems)) {
        QVERIFY2(qFuzzyCompare(chip->height(), chipItems.at(0)->height()),
                 qPrintable(QStringLiteral("chip heights differ: %1 vs %2")
                                .arg(chip->height())
                                .arg(chipItems.at(0)->height())));
        // And tall enough for what is inside it. A chip one line high with a
        // row of faces under it draws the faces over its own bottom edge.
        QVERIFY2(chip->height() >= chip->implicitHeight() - 0.5,
                 qPrintable(QStringLiteral("a chip is %1 tall for %2 of content")
                                .arg(chip->height())
                                .arg(chip->implicitHeight())));
        QVERIFY2(deepestBottom(chip, chip) <= chip->height() + 0.5,
                 qPrintable(QStringLiteral("a chip draws %1 past its own bottom")
                                .arg(deepestBottom(chip, chip) - chip->height())));
    }

    // The card must be tall enough for what it drew, like every other card.
    const qreal padding = card->property("contentMargin").toReal();
    QVERIFY(padding > 0);
    QQuickItem *content = findVisualChild(card, QStringLiteral("cardContent"));
    QVERIFY(content);
    const qreal bottom = content->mapToItem(card, QPointF(0, content->height())).y();
    QVERIFY2(card->height() - bottom >= padding - 0.5,
             qPrintable(QStringLiteral("content ends %1 from the card's bottom, want %2 of padding")
                            .arg(card->height() - bottom)
                            .arg(padding)));

    m_window->hide();
    bubbleItem->setParentItem(nullptr);
}

// A mosaic has one job the eye checks instantly: no gaps, no overlaps, and
// nothing sticking out past the bubble. The layout is arithmetic over rounded
// pixels, which is exactly the kind of thing that is a pixel off in one of the
// five shapes and nowhere else, so all five are asked.
void ChatBubblePerf::albumMosaicTilesTheWholeWidthWithoutOverlapping()
{
    for (int count : {2, 3, 4, 5, 9}) {
        QVariantList tiles;
        for (int i = 0; i < count; ++i) {
            tiles.append(QVariantMap{
                {QStringLiteral("id"), QStringLiteral("al-1-p%1").arg(i)},
                {QStringLiteral("kind"), QStringLiteral("image")},
                {QStringLiteral("media"),
                 QVariantMap{{QStringLiteral("thumbnail_path"), QString()},
                             {QStringLiteral("width"), 1200},
                             {QStringLiteral("height"), 900}}},
            });
        }
        const QVariantMap album{{QStringLiteral("items"), tiles}};
        const QVariantMap props =
            withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("al-1")},
                                    {QStringLiteral("mediaKind"), QStringLiteral("album")},
                                    {QStringLiteral("album"), album}});

        QQmlComponent component(
            m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
        QVERIFY2(!component.isError(), qPrintable(component.errorString()));
        std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
        QVERIFY2(bubble, qPrintable(component.errorString()));
        auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
        bubbleItem->setParentItem(m_window->contentItem());
        m_window->show();
        QVERIFY(QTest::qWaitForWindowExposed(m_window));

        QQuickItem *mosaic = findVisualChild(bubbleItem, QStringLiteral("albumBubble"));
        QVERIFY2(mosaic, "an album row drew no mosaic");
        QTRY_VERIFY(mosaic->width() > 0 && mosaic->height() > 0);

        // Past six pictures the mosaic stops at six and the last cell counts
        // the rest, so a forty-picture album is not forty rows tall.
        const int cells = mosaic->property("cellCount").toInt();
        QCOMPARE(cells, std::min(count, 6));
        if (count > 6) {
            QCOMPARE(mosaic->property("overflow").toInt(), count - 6);
        }

        QList<QRectF> rects;
        const QVariantList raw = mosaic->property("rects").toList();
        QCOMPARE(raw.size(), cells);
        for (const QVariant &entry : raw) {
            const QVariantMap cell = entry.toMap();
            rects.append(QRectF(cell.value(QStringLiteral("x")).toReal(),
                                cell.value(QStringLiteral("y")).toReal(),
                                cell.value(QStringLiteral("w")).toReal(),
                                cell.value(QStringLiteral("h")).toReal()));
        }

        const QString shape = QStringLiteral("%1 pictures").arg(count);
        qreal widest = 0;
        for (int i = 0; i < rects.size(); ++i) {
            QVERIFY2(rects.at(i).width() > 0 && rects.at(i).height() > 0,
                     qPrintable(shape + QStringLiteral(": a cell has no area")));
            QVERIFY2(rects.at(i).right() <= mosaic->width() + 0.5,
                     qPrintable(shape + QStringLiteral(": a cell runs past the mosaic")));
            widest = std::max(widest, rects.at(i).right());
            for (int j = i + 1; j < rects.size(); ++j) {
                QVERIFY2(!rects.at(i).intersects(rects.at(j)),
                         qPrintable(shape + QStringLiteral(": cells %1 and %2 overlap")
                                                .arg(i)
                                                .arg(j)));
            }
        }
        // The mosaic fills the width it was given: a shape that stops short
        // leaves a stripe of wallpaper down one side of the bubble.
        QVERIFY2(widest >= mosaic->width() - 0.5,
                 qPrintable(shape + QStringLiteral(": the mosaic is %1 wide but only fills %2")
                                        .arg(mosaic->width())
                                        .arg(widest)));
        // And the card reports exactly what it drew, which is the height half
        // of the contract with the row.
        qreal bottom = 0;
        for (const QRectF &rect : std::as_const(rects)) {
            bottom = std::max(bottom, rect.bottom());
        }
        QVERIFY2(qAbs(mosaic->height() - bottom) < 0.5,
                 qPrintable(shape + QStringLiteral(": mosaic is %1 tall for %2 of tiles")
                                        .arg(mosaic->height())
                                        .arg(bottom)));

        // The time and ticks sit on the mosaic, so they get what they get on a
        // photo: one scrim across the bottom of the whole thing and the same
        // inset from its corner. Flush in the corner with no scrim was a grey
        // timestamp on whatever the last picture happened to be.
        QVERIFY2(bubbleItem->property("footerOverPicture").toBool(),
                 "an album's footer does not know it is sitting on a picture");
        QQuickItem *footer = findVisualChild(bubbleItem, QStringLiteral("chatBubble.footerSlot"));
        QVERIFY(footer);
        const qreal inset = bubbleItem->property("footerInset").toReal();
        QVERIFY(inset > 0);
        const QPointF footerEnd =
            footer->mapToItem(mosaic, QPointF(footer->width(), footer->height()));
        QVERIFY2(qAbs(mosaic->width() - footerEnd.x() - inset) < 0.5,
                 qPrintable(shape + QStringLiteral(": footer ends %1 from the mosaic's right edge")
                                        .arg(mosaic->width() - footerEnd.x())));
        QVERIFY2(qAbs(mosaic->height() - footerEnd.y() - inset) < 0.5,
                 qPrintable(shape + QStringLiteral(": footer ends %1 from the mosaic's bottom")
                                        .arg(mosaic->height() - footerEnd.y())));

        m_window->hide();
        bubbleItem->setParentItem(nullptr);
    }
}

// The card holds three faces per answer, which is enough to recognise a plan
// and not enough to plan around it. The dialog is the rest: every answer with
// everyone who gave it, the guests they are bringing, and the head count those
// guests make different from the number of answers.
void ChatBubblePerf::eventResponsesDialogNamesEveryoneWhoAnswered()
{
    const QVariantMap ana{{QStringLiteral("jid"), QStringLiteral("ana@s")},
                          {QStringLiteral("name"), QStringLiteral("Ana")},
                          {QStringLiteral("response"), QStringLiteral("going")},
                          {QStringLiteral("timestamp"), 1'700'000'100}};
    const QVariantMap bo{{QStringLiteral("jid"), QStringLiteral("bo@s")},
                         {QStringLiteral("name"), QStringLiteral("Bo")},
                         {QStringLiteral("response"), QStringLiteral("going")},
                         {QStringLiteral("extra_guests"), 2},
                         {QStringLiteral("timestamp"), 1'700'000'200}};
    const QVariantMap cy{{QStringLiteral("jid"), QStringLiteral("cy@s")},
                         {QStringLiteral("name"), QStringLiteral("Cy")},
                         {QStringLiteral("response"), QStringLiteral("not_going")},
                         {QStringLiteral("timestamp"), 1'700'000'300}};
    const QVariantMap me{{QStringLiteral("jid"), QStringLiteral("me")},
                         {QStringLiteral("from_me"), true},
                         {QStringLiteral("response"), QStringLiteral("going")},
                         {QStringLiteral("extra_guests"), 1},
                         {QStringLiteral("timestamp"), 1'700'000'400}};

    const QVariantMap plan{
        {QStringLiteral("name"), QStringLiteral("Team dinner")},
        {QStringLiteral("responders"), QVariantList{ana, bo, cy, me}},
        {QStringLiteral("self_response"), QStringLiteral("going")},
        {QStringLiteral("self_guests"), 1},
        {QStringLiteral("going_count"), 6},
    };

    QQmlComponent component(
        m_engine,
        QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/EventResponsesDialog.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> dialog(component.create());
    QVERIFY2(dialog, qPrintable(component.errorString()));

    dialog->setProperty("messageId", QStringLiteral("ev-2"));
    dialog->setProperty("plan", plan);

    // The dialog is meant to be read, so it can be looked at: point this at a
    // file to get the real thing out, the way the invite card does.
    const QString shot = qEnvironmentVariable("WHATKEVR_RSVP_SCREENSHOT");
    if (!shot.isEmpty()) {
        m_window->show();
        QVERIFY(QTest::qWaitForWindowExposed(m_window));
        dialog->setProperty("parent", QVariant::fromValue(m_window->contentItem()));
        QVERIFY(QMetaObject::invokeMethod(dialog.get(), "open"));
        QTest::qWait(400);
        QVERIFY(m_window->grabWindow().save(shot));
        QVERIFY(QMetaObject::invokeMethod(dialog.get(), "close"));
        QTest::qWait(200);
        m_window->hide();
    }

    const auto people = [&](const QString &response) {
        QVariant out;
        QMetaObject::invokeMethod(dialog.get(), "peopleWhoAnswered", Q_RETURN_ARG(QVariant, out),
                                  Q_ARG(QVariant, response));
        return out.toList();
    };
    const auto heads = [&](const QString &response) {
        QVariant out;
        QMetaObject::invokeMethod(dialog.get(), "headsFor", Q_RETURN_ARG(QVariant, out),
                                  Q_ARG(QVariant, response));
        return out.toInt();
    };

    // Grouped by answer, and inside a group still in the daemon's order: it
    // holds them oldest answer first, and nothing here re-sorts them.
    const QVariantList going = people(QStringLiteral("going"));
    QCOMPARE(going.size(), 3);
    QCOMPARE(going.at(0).toMap().value(QStringLiteral("jid")).toString(), QStringLiteral("ana@s"));
    QCOMPARE(going.at(1).toMap().value(QStringLiteral("jid")).toString(), QStringLiteral("bo@s"));
    QCOMPARE(people(QStringLiteral("not_going")).size(), 1);
    QCOMPARE(people(QStringLiteral("maybe")).size(), 0);
    QCOMPARE(dialog->property("answeredCount").toInt(), 4);

    // Three said yes and two of them are bringing somebody, so six people are
    // at the door. Answers and heads are different questions.
    QCOMPARE(heads(QStringLiteral("going")), 6);

    // An answer nobody gave is still an answer, so all three groups are kept
    // until a chip narrows the dialog to one of them.
    QCOMPARE(dialog->property("visibleAnswers").toList().size(), 3);
    dialog->setProperty("filterResponse", QStringLiteral("not_going"));
    const QVariantList narrowed = dialog->property("visibleAnswers").toList();
    QCOMPARE(narrowed.size(), 1);
    QCOMPARE(narrowed.at(0).toMap().value(QStringLiteral("value")).toString(),
             QStringLiteral("not_going"));
    dialog->setProperty("filterResponse", QString());

    // Answering and opening the list straight after shows the answer just
    // given, not the one the daemon has not echoed yet: our old row leaves the
    // group it was in and a fresh one appears in the new group, guests and all.
    m_controller->respondToEvent(QStringLiteral("ev-2"), QStringLiteral("maybe"), 0);
    QTRY_COMPARE(dialog->property("selfResponse").toString(), QStringLiteral("maybe"));
    QCOMPARE(people(QStringLiteral("going")).size(), 2);
    const QVariantList maybe = people(QStringLiteral("maybe"));
    QCOMPARE(maybe.size(), 1);
    QVERIFY(maybe.at(0).toMap().value(QStringLiteral("from_me")).toBool());
    // Our two heads left the door with us.
    QCOMPARE(heads(QStringLiteral("going")), 4);
    QCOMPARE(dialog->property("answeredCount").toInt(), 4);
}

// The timeline is bottom-anchored and only as tall as its content, so the pane
// shrinking from below (a composer that grew a reply strip, or a wrapping line)
// moves the viewport's top edge up. The height has to follow in the same turn:
// a turn's delay draws the whole transcript one strip too high and then snaps
// it back, which is the flicker a reply lands with while scrolled up. At the
// bottom it hides, because a bottom-pinned viewport moves the content by that
// much anyway, so this asserts the scrolled-up case the eye actually catches.
void ChatBubblePerf::aShrinkingPaneDoesNotDragTheTranscriptWithIt()
{
    CollectionViewModel source;
    ProtocolMessageModel model(&source);
    for (int i = 0; i < 40; ++i) {
        const QString id = QStringLiteral("m%1").arg(i, 3, 10, QLatin1Char('0'));
        source.onUpsert(id,
                        QJsonObject{
                            {QStringLiteral("id"), id},
                            {QStringLiteral("chat_id"), QStringLiteral("wow3@g.us")},
                            {QStringLiteral("kind"), QStringLiteral("text")},
                            {QStringLiteral("text"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("fallback"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("timestamp"), 1'700'000'000 + i},
                            {QStringLiteral("direction"), QStringLiteral("incoming")},
                            {QStringLiteral("status"), QStringLiteral("delivered")},
                        });
    }
    QCOMPARE(model.rowCount(), 40);

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/MessageView.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> view(component.createWithInitialProperties(
        {{QStringLiteral("chatId"), QStringLiteral("wow3@g.us")},
         {QStringLiteral("model"), QVariant::fromValue<QObject *>(&model)}}));
    QVERIFY2(view, qPrintable(component.errorString()));
    auto *viewItem = qobject_cast<QQuickItem *>(view.get());
    viewItem->setParentItem(m_window->contentItem());
    viewItem->setWidth(700);
    viewItem->setHeight(420);
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *list = findVisualChild(viewItem, QStringLiteral("messageList"));
    QVERIFY2(list, "the timeline has no list");
    // Enough rows to overflow the pane, which is what makes it scrollable and
    // is the only state where being scrolled up means anything.
    QTRY_VERIFY(list->property("contentHeight").toReal() > viewItem->height() * 2);
    // An open positions itself and then keeps re-pinning while rows settle from
    // their estimated heights to their real ones. Let that finish, or the scroll
    // below is undone by a re-pin that was already owed.
    QTest::qWait(800);

    // Away from the newest message, and staying there: following it is a
    // separate path that legitimately re-pins on a height change.
    viewItem->setProperty("followNewest", false);
    const qreal parked = list->property("contentY").toReal() - 200;
    list->setProperty("contentY", parked);
    QTest::qWait(100);
    QVERIFY2(qAbs(list->property("contentY").toReal() - parked) < 1,
             qPrintable(QStringLiteral("the timeline would not stay scrolled up: went to %1, not %2")
                            .arg(list->property("contentY").toReal())
                            .arg(parked)));

    // Where the row under the pointer is drawn, in the pane's coordinates: the
    // viewport's own top edge, less how far the content is scrolled through it.
    const auto contentTop = [&] { return list->y() - list->property("contentY").toReal(); };
    const qreal before = contentTop();

    // The composer grows. No event processing after it: the flicker is exactly
    // the state the scene is left in for the turn between the two.
    viewItem->setHeight(viewItem->height() - 48);
    QVERIFY2(qAbs(contentTop() - before) < 0.5,
             qPrintable(QStringLiteral("the transcript jumped %1px when the pane shrank")
                            .arg(contentTop() - before)));

    // And it is still there once everything has settled, so nothing above put
    // it back a frame later.
    QTest::qWait(50);
    QVERIFY2(qAbs(contentTop() - before) < 0.5,
             qPrintable(QStringLiteral("the transcript settled %1px from where it was")
                            .arg(contentTop() - before)));

    m_window->hide();
    viewItem->setParentItem(nullptr);
}

int main(int argc, char *argv[])
{
    qputenv("QT_QPA_PLATFORM", "offscreen");
    QStandardPaths::setTestModeEnabled(true);
    QQuickStyle::setStyle(QStringLiteral("org.kde.desktop"));

    QApplication app(argc, argv);
    ChatBubblePerf tc;
    return QTest::qExec(&tc, argc, argv);
}

#include "tst_chatbubbleperf.moc"
