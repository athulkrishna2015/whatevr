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
#include <QDeadlineTimer>
#include <QElapsedTimer>
#include <QImage>
#include <QJsonObject>
#include <QQmlComponent>
#include <QQmlEngine>
#include <QQuickItem>
#include <QQuickStyle>
#include <QQuickWindow>
#include <QRegularExpression>
#include <QSet>
#include <QSignalSpy>
#include <QDir>
#include <QStandardPaths>
#include <QTemporaryDir>
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
        {QStringLiteral("linkPreview"), QVariantMap()},
        {QStringLiteral("interactive"), QVariantMap()},
        {QStringLiteral("commerce"), QVariantMap()},
        {QStringLiteral("stickerPack"), QVariantMap()},
        {QStringLiteral("callLog"), QVariantMap()},
        {QStringLiteral("system"), QVariantMap()},
        {QStringLiteral("waiting"), QVariantMap()},
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

// Every item under `root` with this objectName, in visual-tree order.
//
// It walks childItems() rather than findChildren(), for the same reason
// findVisualChild does: a Repeater's delegates are parented into the visual
// tree but not necessarily into the QObject tree, so a QObject walk misses
// exactly the rows a Repeater built.
void collectVisualChildren(QQuickItem *item, const QString &name, QList<QQuickItem *> &out)
{
    if (!item) {
        return;
    }
    const auto children = item->childItems();
    for (QQuickItem *child : children) {
        if (child->objectName() == name) {
            out.append(child);
        }
        collectVisualChildren(child, name, out);
    }
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
    void linkPreviewSitsAboveTheWordsAndFitsTheBubble();
    void businessCardDrawsALockedButtonAsLocked();
    void aCallLogIsAPillInTheMiddleWithNoPlate();
    void aSystemEventIsAPillThatSaysWhatHappened();
    void anUnknownSystemEventFallsBackToTheDaemonsWords();
    void aWaitingRowSaysWhetherItIsStillTrying();
    void aShrinkingPaneDoesNotDragTheTranscriptWithIt();
    void openingAChatBuildsItsWindowWithinBudget();
    void theNewestMessageIsDrawnAtTheBottomAndHistoryClimbsAwayFromIt();
    void aHiddenTranscriptKeepsItsRowsSoComingBackToItIsFree();
    void flingingThroughAMediaHeavyChatReusesItsRows();

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

    // Every budget below came down twice, both times for the same reason: an
    // inactive Loader holding an inline component is two objects on every
    // delegate in the chat, and a plain text row was carrying twenty of them
    // for kinds and states it is not. Once for the media slot (pictures,
    // players, audio rows and documents are mutually exclusive, so they are one
    // Loader choosing a URL now), once for the row's overlays (selection
    // chrome, jump glow, hover reply, none of them showing on almost any row at
    // any moment, so they share one Loader and one file). The saving lands on
    // every kind, which is the point.
    //
    // A dump of what is left is a standing invitation to keep going. Run
    // WHATEVR_DN9_DUMP=1 on any row here: what remains that could merge the
    // same way is the row modes (frameless against centered pill) and the body
    // extras (the selectable copy, the read-more button).
    const QList<Sample> samples = {
        {"plain-text-incoming",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m1")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("status"), 4}}), 55},
        {"plain-text-outgoing",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m2")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("isOutgoing"), true},
                    {QStringLiteral("status"), 4}}), 62},
        {"multiline-text",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m3")},
                    {QStringLiteral("text"), longBody},
                    {QStringLiteral("layoutText"), longBody},
                    {QStringLiteral("status"), 3}}), 55},
        {"text-with-reply",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m4")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("replyToMessageId"), QStringLiteral("m1")},
                    {QStringLiteral("replyToSenderName"), QStringLiteral("Aditi")},
                    {QStringLiteral("replyToText"), shortBody}}), 85},
        {"text-with-sender-header",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m5")},
                    {QStringLiteral("text"), shortBody},
                    {QStringLiteral("layoutText"), shortBody},
                    {QStringLiteral("showSenderHeader"), true},
                    {QStringLiteral("showSenderAvatar"), true},
                    {QStringLiteral("showSenderGutter"), true}}), 85},
        {"image",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m6")},
                    {QStringLiteral("mediaKind"), QStringLiteral("image")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("image/jpeg")},
                    {QStringLiteral("mediaWidth"), 1280},
                    {QStringLiteral("mediaHeight"), 720}}), 113},
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
                    {QStringLiteral("mediaDurationSecs"), 12}}), 137},
        {"sticker",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m7")},
                    {QStringLiteral("mediaKind"), QStringLiteral("sticker")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("image/webp")}}), 145},
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
                    {QStringLiteral("mediaDurationSecs"), 6}}), 131},
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
                    {QStringLiteral("mediaDurationSecs"), 204}}), 137},
        {"video-note",
         withProps(baseProps(),
                   {{QStringLiteral("messageId"), QStringLiteral("m9")},
                    {QStringLiteral("mediaKind"), QStringLiteral("video_note")},
                    {QStringLiteral("hasMedia"), true},
                    {QStringLiteral("mediaMimeType"), QStringLiteral("video/mp4")},
                    {QStringLiteral("mediaWidth"), 480},
                    {QStringLiteral("mediaHeight"), 480},
                    {QStringLiteral("mediaDurationSecs"), 11}}), 172},
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

// A link preview is the one card that shares its bubble with body text, so it
// is the only one that can get the two wrong relative to each other: drawn
// under the words it describes, or drawn narrower than them. Both layouts are
// asked, because which one a link gets is the sender's word and neither is the
// safe default.
void ChatBubblePerf::linkPreviewSitsAboveTheWordsAndFitsTheBubble()
{
    QTemporaryDir cache;
    QVERIFY(cache.isValid());
    const QString thumbPath = cache.filePath(QStringLiteral("preview.png"));
    QImage thumb(320, 180, QImage::Format_RGB32);
    thumb.fill(QColor(60, 110, 170));
    QVERIFY(thumb.save(thumbPath));

    const QString body = QStringLiteral("worth a read https://example.com/road");

    struct Case {
        const char *name;
        QString type;
        QString thumbnail;
        bool large;
    };
    const QList<Case> cases{
        // A still from something to watch: the picture is the content, so it
        // gets the card's width and a play glyph over it.
        {"video", QStringLiteral("video"), thumbPath, true},
        {"image", QStringLiteral("image"), thumbPath, true},
        // A page whose thumbnail is a logo. Blown up to card width a logo is a
        // worse card than none, so it stays a square beside the words.
        {"page with a logo", QString(), thumbPath, false},
        // And a page that carried no picture at all.
        {"page with nothing", QString(), QString(), false},
    };

    for (const Case &sample : cases) {
        const QVariantMap preview{
            {QStringLiteral("url"), QStringLiteral("https://example.com/road")},
            {QStringLiteral("host"), QStringLiteral("example.com")},
            {QStringLiteral("title"), QStringLiteral("The road to nowhere")},
            {QStringLiteral("description"),
             QStringLiteral("A page about a road, and where it does not go.")},
            {QStringLiteral("type"), sample.type},
            {QStringLiteral("thumbnail_path"), sample.thumbnail},
            {QStringLiteral("thumb_width"), 320},
            {QStringLiteral("thumb_height"), 180},
        };
        const QVariantMap props =
            withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("lp-1")},
                                    {QStringLiteral("text"), body},
                                    {QStringLiteral("layoutText"), body},
                                    {QStringLiteral("linkPreview"), preview}});

        QQmlComponent component(
            m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
        QVERIFY2(!component.isError(), qPrintable(component.errorString()));
        std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
        QVERIFY2(bubble, qPrintable(component.errorString()));
        auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
        bubbleItem->setParentItem(m_window->contentItem());
        m_window->show();
        QVERIFY(QTest::qWaitForWindowExposed(m_window));

        const QString what = QString::fromLatin1(sample.name);

        // The row is still a text row. Nothing about its kind changed, which is
        // what keeps the chat list, reply quotes and search working.
        QVERIFY2(bubbleItem->property("isLinkPreview").toBool(),
                 qPrintable(what + QStringLiteral(": the row did not recognise its preview")));
        QVERIFY(bubbleItem->property("mediaKind").toString().isEmpty());
        QVERIFY2(bubbleItem->property("hasBody").toBool(),
                 qPrintable(what + QStringLiteral(": the message lost its own text")));

        QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("linkPreviewCard"));
        QVERIFY2(card, qPrintable(what + QStringLiteral(": no card was drawn")));
        QTRY_VERIFY(card->width() > 0 && card->height() > 0);
        QCOMPARE(card->property("largeLayout").toBool(), sample.large);

        // Above the words, not under them: the card describes where the text
        // points, and a caption reads as belonging to what is above it.
        QQuickItem *bodyText = nullptr;
        const auto descendants = bubbleItem->findChildren<QQuickItem *>();
        for (QQuickItem *child : descendants) {
            if (child->property("text").toString() == body) {
                bodyText = child;
                break;
            }
        }
        QVERIFY2(bodyText, qPrintable(what + QStringLiteral(": the body text is not on screen")));
        const qreal cardBottom = card->mapToItem(bubbleItem, QPointF(0, card->height())).y();
        const qreal bodyTop = bodyText->mapToItem(bubbleItem, QPointF(0, 0)).y();
        QVERIFY2(cardBottom <= bodyTop + 0.5,
                 qPrintable(what + QStringLiteral(": the card ends at %1, below the text at %2")
                                       .arg(cardBottom)
                                       .arg(bodyTop)));

        // As wide as the words got. A card narrower than the paragraph under it
        // reads as a mistake, and this is the one card that can be.
        const qreal contentWidth = bubbleItem->property("contentBlockWidth").toReal();
        QVERIFY2(qAbs(card->width() - contentWidth) < 0.5,
                 qPrintable(what + QStringLiteral(": card is %1 wide in a %2 bubble")
                                       .arg(card->width())
                                       .arg(contentWidth)));

        // And it reports what it drew, like every other card.
        QVERIFY2(deepestBottom(card, card) <= card->height() + 0.5,
                 qPrintable(what + QStringLiteral(": the card draws %1 past its own bottom")
                                       .arg(deepestBottom(card, card) - card->height())));

        // The host chip is fully rounded, so its sides are padded to its own
        // height rather than to a flat unit: half-height ends crowding text
        // that was inset by a fixed few pixels is what a pill looks like when
        // it is padded wrong.
        QQuickItem *chip = nullptr;
        for (QQuickItem *child : descendants) {
            if (child->property("radius").toReal() > 0
                && qAbs(child->property("radius").toReal() - child->height() / 2) < 0.5
                && child->height() > 0 && child->height() < 100
                && child->parentItem() != nullptr) {
                const auto labels = child->childItems();
                for (QQuickItem *label : labels) {
                    if (label->property("text").toString() == QStringLiteral("example.com")) {
                        chip = child;
                    }
                }
            }
            if (chip) {
                break;
            }
        }
        QVERIFY2(chip, qPrintable(what + QStringLiteral(": the host is not named")));
        const qreal sidePadding = (chip->width() - chip->childItems().first()->width()) / 2;
        QVERIFY2(sidePadding >= chip->height() * 0.3,
                 qPrintable(what + QStringLiteral(": a %1-tall pill pads its sides by %2")
                                       .arg(chip->height())
                                       .arg(sidePadding)));

        if (sample.large) {
            // A hero keeps the picture's own shape rather than being squeezed
            // into a band: a 16:9 still arrived, so a 16:9 slot is drawn.
            const qreal hero = card->property("heroHeight").toReal();
            QVERIFY(hero > 0);
            QVERIFY2(qAbs(hero - card->width() * 180.0 / 320.0) < 1.0,
                     qPrintable(what + QStringLiteral(": hero is %1 tall for a %2-wide card")
                                           .arg(hero)
                                           .arg(card->width())));
            QCOMPARE(card->property("showsPlayBadge").toBool(),
                     sample.type == QStringLiteral("video"));
        } else {
            // The compact card asks for its content rather than the ceiling: a
            // short title stretched across the whole bubble is mostly nothing.
            QVERIFY2(card->property("preferredWidth").toReal() > 0,
                     qPrintable(what + QStringLiteral(": the compact card asked to fill")));
        }

        const QString shot = qEnvironmentVariable("WHATKEVR_LINKPREVIEW_SCREENSHOT");
        if (!shot.isEmpty()) {
            QTest::qWait(300);
            QVERIFY(m_window->grabWindow().save(
                QStringLiteral("%1-%2.png").arg(shot, QString::fromLatin1(sample.name).replace(
                                                          QLatin1Char(' '), QLatin1Char('-')))));
        }

        m_window->hide();
        bubbleItem->setParentItem(nullptr);
    }
}

// The timeline is bottom-anchored and only as tall as its content, so the pane
// shrinking from below (a composer that grew a reply strip, or a wrapping line)
// moves the viewport's top edge up. The height has to follow in the same turn:
// a turn's delay draws the whole transcript one strip too high and then snaps
// it back, which is the flicker a reply lands with while scrolled up. At the
// bottom it hides, because a bottom-pinned viewport moves the content by that
// much anyway, so this asserts the scrolled-up case the eye actually catches.
// A business message offers things to do, and only some of them can be done
// here: a link and a phone number are a handoff, a quick reply is a message
// back to a business, which whatevr cannot send yet. The card has to draw that
// difference, because the alternative is somebody pressing a button and
// learning by nothing happening.
void ChatBubblePerf::businessCardDrawsALockedButtonAsLocked()
{
    const QVariantList buttons{
        QVariantMap{{QStringLiteral("kind"), QStringLiteral("url")},
                    {QStringLiteral("label"), QStringLiteral("Track parcel")},
                    {QStringLiteral("url"), QStringLiteral("https://example.com/t")},
                    {QStringLiteral("live"), true}},
        QVariantMap{{QStringLiteral("kind"), QStringLiteral("call")},
                    {QStringLiteral("label"), QStringLiteral("Call the driver")},
                    {QStringLiteral("phone"), QStringLiteral("+911234567890")},
                    {QStringLiteral("live"), true}},
        QVariantMap{{QStringLiteral("kind"), QStringLiteral("reply")},
                    {QStringLiteral("label"), QStringLiteral("Leave with a neighbour")},
                    {QStringLiteral("live"), false}},
    };
    // With a header picture, which is the case that broke: the block of words
    // is anchored under the hero, so it is laid out while the hero still has
    // no decoded size, and a card that never arranged again drew every line on
    // top of the first.
    QTemporaryDir cache;
    QVERIFY(cache.isValid());
    const QString headerPath = cache.filePath(QStringLiteral("header.png"));
    QImage header(320, 168, QImage::Format_RGB32);
    header.fill(QColor(80, 120, 60));
    QVERIFY(header.save(headerPath));

    const QVariantMap card{
        {QStringLiteral("source"), QStringLiteral("template")},
        {QStringLiteral("title"), QStringLiteral("Your parcel is out for delivery")},
        {QStringLiteral("body"), QStringLiteral("BLR-4471 left the hub at 08:12 and is the "
                                                "fourth stop on today's route.")},
        {QStringLiteral("footer"), QStringLiteral("Sent by Bluedart")},
        {QStringLiteral("thumbnail_path"), headerPath},
        {QStringLiteral("buttons"), buttons},
    };
    const QVariantMap props =
        withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("biz-1")},
                                {QStringLiteral("mediaKind"), QStringLiteral("interactive")},
                                {QStringLiteral("interactive"), card}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QVERIFY(bubbleItem->property("isInteractive").toBool());
    QVERIFY(bubbleItem->property("isCardBlock").toBool());
    // Nothing on a business card is downloadable, and a kind that looks
    // fetchable puts marketing broadcasts on the auto-download path.
    QVERIFY(!bubbleItem->property("hasDownloadableMedia").toBool());

    QQuickItem *cardItem = findVisualChild(bubbleItem, QStringLiteral("interactiveBubble"));
    QVERIFY2(cardItem, "no business card was drawn");
    QTRY_VERIFY(cardItem->width() > 0 && cardItem->height() > 0);

    QList<QQuickItem *> rows;
    collectVisualChildren(cardItem, QStringLiteral("interactiveActionRow"), rows);
    QCOMPARE(rows.size(), 3);

    // Two of the three can be pressed here. The third says so by being drawn
    // disabled rather than by failing when it is.
    QVERIFY2(rows.at(0)->isEnabled(), "the link button is a handoff and must work");
    QVERIFY2(rows.at(1)->isEnabled(), "the call button is a handoff and must work");
    QVERIFY2(!rows.at(2)->isEnabled(), "a quick reply cannot be sent yet and must not look like it can");

    // Stacked, in order, with nothing sitting on top of anything else. A
    // collapsed layout still passes every check above: the buttons exist, they
    // are the right width and they report the right enabled state, all while
    // drawn one on top of the other at the top of the card.
    const QQuickItem *cardTitle = findVisualChild(cardItem, QStringLiteral("cardContent"));
    QVERIFY(cardTitle);
    for (int i = 1; i < rows.size(); ++i) {
        const qreal previousBottom = rows.at(i - 1)->y() + rows.at(i - 1)->height();
        QVERIFY2(rows.at(i)->y() >= previousBottom,
                 qPrintable(QStringLiteral("button %1 starts at %2, inside the one above it ending at %3")
                                .arg(i)
                                .arg(rows.at(i)->y())
                                .arg(previousBottom)));
    }

    // Every button spans the card, so three of them stack instead of wrapping
    // into a ragged block.
    for (QQuickItem *row : rows) {
        QVERIFY2(qAbs(row->width() - (cardItem->width() - 2 * cardItem->property("contentMargin").toReal())) < 1.0,
                 qPrintable(QStringLiteral("a button is %1 wide in a %2 card")
                                .arg(row->width())
                                .arg(cardItem->width())));
    }

    // And the card reports the height it drew, like every other card: width
    // flows down, height flows up.
    QVERIFY2(deepestBottom(cardItem, cardItem) <= cardItem->height() + 0.5,
             qPrintable(QStringLiteral("the card draws %1 past its own bottom")
                            .arg(deepestBottom(cardItem, cardItem) - cardItem->height())));
    QCOMPARE(cardItem->width(), bubbleItem->property("attachmentBlockWidth").toReal());
}

// A call is not a message anybody wrote. It draws as a pill in the middle of
// the row with no plate behind it and no side of the transcript claimed,
// because putting it in a bubble would say somebody spoke.
void ChatBubblePerf::aCallLogIsAPillInTheMiddleWithNoPlate()
{
    const QVariantMap log{
        {QStringLiteral("video"), true},
        {QStringLiteral("outcome"), QStringLiteral("missed")},
        {QStringLiteral("duration_secs"), 0},
    };
    const QVariantMap props =
        withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("call-1")},
                                {QStringLiteral("mediaKind"), QStringLiteral("call_log")},
                                {QStringLiteral("callLog"), log},
                                // A group chat, so the sender header and avatar
                                // would be drawn if the pill did not suppress
                                // them.
                                {QStringLiteral("showSenderHeader"), true},
                                {QStringLiteral("showSenderAvatar"), true},
                                {QStringLiteral("showSenderGutter"), true}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QVERIFY(bubbleItem->property("centeredPill").toBool());
    // No sender name and no avatar gutter: both would be labelling a message
    // nobody sent.
    QCOMPARE(bubbleItem->property("senderHeaderHeight").toReal(), 0.0);
    QCOMPARE(bubbleItem->property("senderGutterWidth").toReal(), 0.0);
    // And nothing to reply to. A call is not a thing you quote.
    QVERIFY(!bubbleItem->property("canReply").toBool());

    QQuickItem *pill = findVisualChild(bubbleItem, QStringLiteral("callLogPill"));
    QVERIFY2(pill, "no call pill was drawn");
    QTRY_VERIFY(pill->width() > 0 && pill->height() > 0);

    // Centered on the whole row rather than on either bubble column, because a
    // call belongs to neither side of the conversation.
    const qreal pillCentre = pill->mapToItem(bubbleItem, QPointF(pill->width() / 2, 0)).x();
    QVERIFY2(qAbs(pillCentre - bubbleItem->width() / 2) < 1.5,
             qPrintable(QStringLiteral("the pill is centred at %1 in a %2-wide row")
                            .arg(pillCentre)
                            .arg(bubbleItem->width())));

    // A fully rounded pill pads its sides to its own height, not to a flat
    // unit, or the words sit in the flat middle with the round ends crowding.
    const qreal hPadding = pill->property("hPadding").toReal();
    QVERIFY2(hPadding >= pill->height() * 0.3,
             qPrintable(QStringLiteral("a %1-tall pill pads its sides by %2")
                            .arg(pill->height())
                            .arg(hPadding)));

    // The row is the pill plus its own bottom gap and nothing else. A plate
    // reserved behind it, or a footer under it, would show up here as a row
    // several times the pill's height.
    QVERIFY2(bubbleItem->height() < pill->height() * 1.6,
             qPrintable(QStringLiteral("a %1-tall pill sits in a %2-tall row")
                            .arg(pill->height())
                            .arg(bubbleItem->height())));
}

// A system event is not a message anybody wrote either. It draws as the same
// centered pill a call log does, says its whole sentence from the parts the
// daemon sent rather than from the daemon's own English, and suppresses the
// sender header a group chat would otherwise put above it.
void ChatBubblePerf::aSystemEventIsAPillThatSaysWhatHappened()
{
    const QVariantMap event{
        {QStringLiteral("type"), QStringLiteral("group_join")},
        // Deliberately wrong, so a pill that printed it instead of composing
        // its own sentence fails here rather than silently shipping English.
        {QStringLiteral("text"), QStringLiteral("DAEMON FALLBACK")},
        {QStringLiteral("actor"), QVariantMap{{QStringLiteral("jid"), QStringLiteral("cy@s.whatsapp.net")},
                                              {QStringLiteral("name"), QStringLiteral("Cy")}}},
        {QStringLiteral("names"), QVariantList{
                                      QVariantMap{{QStringLiteral("jid"), QStringLiteral("ana@s.whatsapp.net")},
                                                  {QStringLiteral("name"), QStringLiteral("Ana")}},
                                      QVariantMap{{QStringLiteral("jid"), QStringLiteral("bo@s.whatsapp.net")},
                                                  {QStringLiteral("name"), QStringLiteral("Bo")}}}},
    };
    const QVariantMap props =
        withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("sys-1")},
                                {QStringLiteral("mediaKind"), QStringLiteral("system")},
                                {QStringLiteral("system"), event},
                                // A group chat, so the sender header and avatar
                                // would be drawn if the pill did not suppress
                                // them.
                                {QStringLiteral("showSenderHeader"), true},
                                {QStringLiteral("showSenderAvatar"), true},
                                {QStringLiteral("showSenderGutter"), true}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QVERIFY(bubbleItem->property("centeredPill").toBool());
    QCOMPARE(bubbleItem->property("senderHeaderHeight").toReal(), 0.0);
    QCOMPARE(bubbleItem->property("senderGutterWidth").toReal(), 0.0);
    QVERIFY(!bubbleItem->property("canReply").toBool());

    QQuickItem *pill = findVisualChild(bubbleItem, QStringLiteral("systemPill"));
    QVERIFY2(pill, "no system pill was drawn");
    QTRY_VERIFY(pill->width() > 0 && pill->height() > 0);

    // The sentence is built here, from the flags, so it can be translated. The
    // daemon's own line is the fallback for a type this build never heard of.
    QCOMPARE(pill->property("summary").toString(), QStringLiteral("Cy added Ana and Bo"));

    const qreal pillCentre = pill->mapToItem(bubbleItem, QPointF(pill->width() / 2, 0)).x();
    QVERIFY2(qAbs(pillCentre - bubbleItem->width() / 2) < 1.5,
             qPrintable(QStringLiteral("the pill is centred at %1 in a %2-wide row")
                            .arg(pillCentre)
                            .arg(bubbleItem->width())));

    const qreal hPadding = pill->property("hPadding").toReal();
    QVERIFY2(hPadding >= pill->height() * 0.3,
             qPrintable(QStringLiteral("a %1-tall pill pads its sides by %2")
                            .arg(pill->height())
                            .arg(hPadding)));

    QVERIFY2(bubbleItem->height() < pill->height() * 1.6,
             qPrintable(QStringLiteral("a %1-tall pill sits in a %2-tall row")
                            .arg(pill->height())
                            .arg(bubbleItem->height())));
}

// An event type this build has never heard of still says something: the daemon
// already wrote a sentence for it, and printing that beats printing nothing.
void ChatBubblePerf::anUnknownSystemEventFallsBackToTheDaemonsWords()
{
    const QVariantMap event{
        {QStringLiteral("type"), QStringLiteral("something_from_the_future")},
        {QStringLiteral("text"), QStringLiteral("Cy did something new")},
    };
    const QVariantMap props =
        withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("sys-2")},
                                {QStringLiteral("mediaKind"), QStringLiteral("system")},
                                {QStringLiteral("system"), event}});

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> bubble(component.createWithInitialProperties(props));
    QVERIFY2(bubble, qPrintable(component.errorString()));
    auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
    bubbleItem->setParentItem(m_window->contentItem());
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *pill = findVisualChild(bubbleItem, QStringLiteral("systemPill"));
    QVERIFY2(pill, "no system pill was drawn");
    QCOMPARE(pill->property("summary").toString(), QStringLiteral("Cy did something new"));
}

// A message that would not decrypt says two different things depending on
// whether anything is still expected to happen. While the daemon is still
// asking it counts down and offers nothing; once that has come and gone it says
// so and offers the button.
void ChatBubblePerf::aWaitingRowSaysWhetherItIsStillTrying()
{
    const auto buildWaiting = [this](qint64 retryAt, bool asked) {
        const QVariantMap wait{
            {QStringLiteral("first_seen"), 1'700'000'000},
            {QStringLiteral("retry_at"), retryAt},
            {QStringLiteral("requests"), 1},
            {QStringLiteral("asked"), asked},
        };
        const QVariantMap props =
            withProps(baseProps(), {{QStringLiteral("messageId"), QStringLiteral("wait-1")},
                                    {QStringLiteral("mediaKind"), QStringLiteral("waiting")},
                                    {QStringLiteral("waiting"), wait}});
        QQmlComponent component(
            m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/ChatBubble.qml")));
        return std::pair{component.createWithInitialProperties(props), component.errorString()};
    };

    // Still trying: a countdown, and no button to press over the top of it.
    {
        const qint64 soon = QDateTime::currentSecsSinceEpoch() + 5;
        auto [object, error] = buildWaiting(soon, false);
        std::unique_ptr<QObject> bubble(object);
        QVERIFY2(bubble, qPrintable(error));
        auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
        bubbleItem->setParentItem(m_window->contentItem());
        m_window->show();
        QVERIFY(QTest::qWaitForWindowExposed(m_window));

        QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("waitingBubble"));
        QVERIFY2(card, "no waiting card was drawn");
        QVERIFY(card->property("stillTrying").toBool());
        QVERIFY2(card->property("statusText").toString().contains(QStringLiteral("Asking again")),
                 qPrintable(card->property("statusText").toString()));

        QQuickItem *button = findVisualChild(card, QStringLiteral("waitingAskAgainButton"));
        QVERIFY2(!button || !button->isVisible(),
                 "the ask-again button was offered while the daemon was already asking");
    }

    // Spent: the honest terminal state, and the button.
    {
        auto [object, error] = buildWaiting(1'700'000'005, true);
        std::unique_ptr<QObject> bubble(object);
        QVERIFY2(bubble, qPrintable(error));
        auto *bubbleItem = qobject_cast<QQuickItem *>(bubble.get());
        bubbleItem->setParentItem(m_window->contentItem());
        m_window->show();
        QVERIFY(QTest::qWaitForWindowExposed(m_window));

        QQuickItem *card = findVisualChild(bubbleItem, QStringLiteral("waitingBubble"));
        QVERIFY2(card, "no waiting card was drawn");
        QVERIFY(!card->property("stillTrying").toBool());
        QVERIFY2(card->property("statusText").toString().contains(QStringLiteral("phone")),
                 qPrintable(card->property("statusText").toString()));

        QQuickItem *button = findVisualChild(card, QStringLiteral("waitingAskAgainButton"));
        QVERIFY2(button && button->isVisible(), "no way left to ask again");
    }
}

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

// The whole point of the delegate budget above is what it costs to open a chat,
// and until now nothing measured that directly: delegateCost() builds rows by
// hand, one at a time, outside any view. This builds the real thing — a real
// MessageView over a real ProtocolMessageModel, sized like the app — and fills
// it the way a subscribe fill does, in one batch, then measures what the open
// actually cost.
//
// Two numbers come out. Objects is the gate: it is deterministic, and it is the
// product of the per-row cost and how many rows the view decides to materialise,
// which are exactly the two things worth holding down. Wall time is reported but
// not asserted, for the same reason delegateCost does not assert it.
void ChatBubblePerf::openingAChatBuildsItsWindowWithinBudget()
{
    // The window a `messages` subscribe asks for (kMessagePageSize).
    constexpr int kWindowRows = 80;
    // A ceiling on the settled band, not a target, set just above what this
    // measures today so regrowth is a build failure. Lower it whenever a
    // landing earns the lower number.
    //
    // Baselines this was set from, Release, eighty plain text rows carrying no
    // media at all, on the settled band of 55 rows. Time to glass with the
    // cache band left open during the open was ~197ms for all 55 of those rows;
    // shutting the band for the open brought it to ~70ms for the 19 rows the
    // viewport actually shows.
    //
    //   band open, one Loader per media kind   4180 settled (76/row), 1462 painted, ~197ms
    //   band shut, one Loader for the family   3740 settled (68/row), 1310 painted,  ~65ms
    //   one Loader for the row's overlays      3473 settled (63/row), 1223 painted,  ~52ms
    constexpr int kMaxObjects = 3580;

    CollectionViewModel source;
    ProtocolMessageModel model(&source);

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/MessageView.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    // Built with no chat, so that selecting one below goes through the same
    // path a switch does. Handing it a chat as an initial property would skip
    // onChatIdChanged, and with it everything an open arms.
    std::unique_ptr<QObject> view(component.createWithInitialProperties(
        {{QStringLiteral("model"), QVariant::fromValue<QObject *>(&model)}}));
    QVERIFY2(view, qPrintable(component.errorString()));
    auto *viewItem = qobject_cast<QQuickItem *>(view.get());
    viewItem->setParentItem(m_window->contentItem());
    viewItem->setWidth(900);
    viewItem->setHeight(700);
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));

    QQuickItem *list = findVisualChild(viewItem, QStringLiteral("messageList"));
    QVERIFY2(list, "the timeline has no list");

    // Settle the empty view first, so the measurement below covers the fill and
    // not the one-time cost of compiling and instantiating MessageView itself.
    QTest::qWait(200);

    // A fill crosses as one batch, which is the shape CollectionViewModel
    // collapses into a single insert transaction. Building the JSON inside the
    // timed section would measure the test harness, so it is built first.
    QList<QJsonObject> rows;
    rows.reserve(kWindowRows);
    QList<QString> sorts;
    sorts.reserve(kWindowRows);
    for (int i = 0; i < kWindowRows; ++i) {
        const QString id = QStringLiteral("m%1").arg(i, 4, 10, QLatin1Char('0'));
        // A mix that looks like a real transcript rather than eighty identical
        // rows: both directions, a sender change every few rows so headers and
        // group boundaries are exercised, and bodies that wrap.
        const bool outgoing = (i / 3) % 2 == 0;
        rows.append(QJsonObject{
            {QStringLiteral("id"), id},
            {QStringLiteral("chat_id"), QStringLiteral("open@g.us")},
            {QStringLiteral("kind"), QStringLiteral("text")},
            {QStringLiteral("text"),
             i % 5 == 0 ? QStringLiteral("a longer line that has to wrap across the bubble "
                                         "because it says considerably more than the others (%1)")
                              .arg(i)
                        : QStringLiteral("row %1").arg(i)},
            {QStringLiteral("fallback"), QStringLiteral("row %1").arg(i)},
            {QStringLiteral("timestamp"), 1'700'000'000 + i * 60},
            {QStringLiteral("direction"),
             outgoing ? QStringLiteral("outgoing") : QStringLiteral("incoming")},
            {QStringLiteral("status"), QStringLiteral("read")},
            {QStringLiteral("sender"),
             QJsonObject{{QStringLiteral("id"),
                          outgoing ? QStringLiteral("me@s.whatsapp.net")
                                   : QStringLiteral("%1@s.whatsapp.net").arg(i / 3)},
                         {QStringLiteral("name"), QStringLiteral("Person %1").arg(i / 3)}}},
        });
        sorts.append(QStringLiteral("%1").arg(1'700'000'000 + i * 60, 20, 10, QLatin1Char('0')));
    }

    // The chat is selected and its window arrives, which is an open. Selecting
    // it arms everything an open arms (the cache band shuts); openingChat is
    // latched here because in the app the controller does it, and released
    // after the window is up.
    QElapsedTimer timer;
    timer.start();

    viewItem->setProperty("chatId", QStringLiteral("open@g.us"));
    viewItem->setProperty("openingChat", true);

    source.onBatchBegin();
    for (int i = 0; i < kWindowRows; ++i) {
        source.onUpsert(sorts.at(i), rows.at(i));
    }
    source.onBatchEnd();
    source.onReady(true, true);
    QCOMPARE(model.rowCount(), kWindowRows);

    // Spin until the view stops building rows. This is the number that answers
    // "how long until the transcript is on screen", and it has to be quiescence
    // rather than "the first delegate exists": the list's own height is a
    // delayed binding on its content height, so the viewport it is filling is
    // still growing for several turns after the rows land, and each turn
    // materialises more of them.
    //
    // Deliberately not QTRY_VERIFY: it sleeps 50ms between polls, which would
    // quantise a 20ms fill and a 69ms one to the same reading. A tight
    // processEvents loop resolves to the turn the building actually stopped on.
    QCOMPARE(list->property("count").toInt(), kWindowRows);
    const auto builtRows = [&] {
        int n = 0;
        const auto children =
            list->property("contentItem").value<QQuickItem *>()->childItems();
        for (QQuickItem *child : children) {
            if (!child->property("messageId").toString().isEmpty()) {
                ++n;
            }
        }
        return n;
    };
    constexpr int kQuietTurns = 8;
    int quiet = 0;
    int lastRows = -1;
    QDeadlineTimer deadline(5000);
    while (quiet < kQuietTurns && !deadline.hasExpired()) {
        QCoreApplication::processEvents();
        const int now = builtRows();
        quiet = (now == lastRows && now > 0) ? quiet + 1 : 0;
        lastRows = now;
    }
    const double fillMs = timer.nsecsElapsed() / 1e6;
    QVERIFY2(lastRows > 0, "the view never laid the window out");

    // What the user waited for: the rows the viewport actually needed. The
    // cache band is opened below, exactly as afterModelReset() does it, and
    // what it costs afterwards is off the critical path.
    QQuickItem *content = list->property("contentItem").value<QQuickItem *>();
    QVERIFY2(content, "the list has no content item");
    // Rows, and what those rows cost. Summed per delegate rather than taken
    // from the content item in one go, for the reason findVisualChild exists:
    // QQmlDelegateModel owns the delegates, so they are visual children of the
    // content item without being QObject children of it, and a single
    // findChildren() walk from the top misses every one of them.
    //
    // Scoped to the delegates deliberately: the pane also owns its context
    // menus, dialogs, scrollbars and wheel scroller, a fixed cost paid once per
    // pane that has nothing to do with how expensive a window of rows is.
    const auto measureDelegates = [&] {
        int rowCount = 0;
        int objects = 0;
        const auto children = content->childItems();
        for (QQuickItem *child : children) {
            if (child->property("messageId").toString().isEmpty()) {
                continue;
            }
            ++rowCount;
            objects += objectCount(child);
        }
        return std::pair<int, int>{rowCount, objects};
    };
    const auto [paintedRows, paintedObjects] = measureDelegates();

    // The open settles and the band opens. Let it finish incubating, so the
    // steady-state numbers describe what an open leaves behind rather than
    // whichever frame we happened to catch.
    viewItem->setProperty("openingChat", false);
    QTest::qWait(600);

    const auto [materialised, objects] = measureDelegates();
    QVERIFY2(paintedRows > 0, "the list materialised no rows at all");
    qInfo("DN9 %-24s painted: %5d objects over %3d rows in %6.1f ms | "
          "settled: %5d objects over %3d/%d rows (%.0f/row)",
          "chat-open", paintedObjects, paintedRows, fillMs,
          objects, materialised, kWindowRows, double(objects) / materialised);

    m_window->hide();
    viewItem->setParentItem(nullptr);

    QVERIFY2(objects <= kMaxObjects,
             qPrintable(QStringLiteral("opening a chat built %1 objects, over the budget of %2 "
                                       "(%3 rows materialised)")
                            .arg(objects)
                            .arg(kMaxObjects)
                            .arg(materialised)));
}

// The transcript is drawn bottom-up over a model held newest-first, which is
// two inversions that have to cancel exactly. If either one is applied without
// the other the conversation renders upside down, and nothing else in the suite
// would notice: the rows would all be present, correctly grouped and correctly
// counted, just in the wrong order on the glass.
void ChatBubblePerf::theNewestMessageIsDrawnAtTheBottomAndHistoryClimbsAwayFromIt()
{
    CollectionViewModel source;
    source.setReverseOrder(true);
    ProtocolMessageModel model(&source);

    constexpr int kRows = 40;
    for (int i = 0; i < kRows; ++i) {
        const QString id = QStringLiteral("m%1").arg(i, 3, 10, QLatin1Char('0'));
        source.onUpsert(QStringLiteral("%1").arg(1'700'000'000 + i * 60, 20, 10, QLatin1Char('0')),
                        QJsonObject{
                            {QStringLiteral("id"), id},
                            {QStringLiteral("chat_id"), QStringLiteral("order@g.us")},
                            {QStringLiteral("kind"), QStringLiteral("text")},
                            {QStringLiteral("text"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("fallback"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("timestamp"), 1'700'000'000 + i * 60},
                            {QStringLiteral("direction"), QStringLiteral("incoming")},
                            {QStringLiteral("status"), QStringLiteral("read")},
                        });
    }
    // Newest last in time, first in the model.
    QCOMPARE(model.rowCount(), kRows);
    QCOMPARE(model.newestMessageId(), QStringLiteral("m039"));
    QCOMPARE(model.messageIdAt(0), QStringLiteral("m039"));

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/MessageView.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> view(component.createWithInitialProperties(
        {{QStringLiteral("model"), QVariant::fromValue<QObject *>(&model)}}));
    QVERIFY2(view, qPrintable(component.errorString()));
    auto *viewItem = qobject_cast<QQuickItem *>(view.get());
    viewItem->setParentItem(m_window->contentItem());
    viewItem->setWidth(700);
    viewItem->setHeight(420);
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));
    viewItem->setProperty("chatId", QStringLiteral("order@g.us"));
    QTest::qWait(600);

    QQuickItem *list = findVisualChild(viewItem, QStringLiteral("messageList"));
    QVERIFY2(list, "the timeline has no list");
    QQuickItem *content = list->property("contentItem").value<QQuickItem *>();
    QVERIFY2(content, "the list has no content item");

    // Where each message actually landed, by id.
    QHash<QString, qreal> bottomOf;
    const auto children = content->childItems();
    for (QQuickItem *child : children) {
        const QString id = child->property("messageId").toString();
        if (!id.isEmpty()) {
            bottomOf.insert(id, child->y());
        }
    }
    QVERIFY2(bottomOf.size() >= 3, "too few rows materialised to judge the order");

    // The newest message is on screen, and it is the lowest thing on screen.
    QVERIFY2(bottomOf.contains(QStringLiteral("m039")),
             "the newest message was not materialised, so the view did not open at it");
    const qreal newestY = bottomOf.value(QStringLiteral("m039"));
    for (auto it = bottomOf.cbegin(); it != bottomOf.cend(); ++it) {
        if (it.key() == QStringLiteral("m039")) {
            continue;
        }
        QVERIFY2(it.value() < newestY,
                 qPrintable(QStringLiteral("%1 is drawn below the newest message (y %2 vs %3): the "
                                           "transcript is upside down")
                                .arg(it.key())
                                .arg(it.value())
                                .arg(newestY)));
    }

    // And within the history above it, older is higher: the ids sort in the
    // order the messages were sent, so their y coordinates must increase along
    // with them.
    QStringList onScreen = bottomOf.keys();
    std::sort(onScreen.begin(), onScreen.end());
    for (int i = 1; i < onScreen.size(); ++i) {
        QVERIFY2(bottomOf.value(onScreen.at(i - 1)) < bottomOf.value(onScreen.at(i)),
                 qPrintable(QStringLiteral("%1 should sit above %2, but is drawn at y %3 against %4")
                                .arg(onScreen.at(i - 1), onScreen.at(i))
                                .arg(bottomOf.value(onScreen.at(i - 1)))
                                .arg(bottomOf.value(onScreen.at(i)))));
    }

    m_window->hide();
    viewItem->setParentItem(nullptr);
}

// Whether a transcript that is merely hidden keeps the delegates it built.
//
// The whole cost of opening a chat is building rows, so a re-open is only free
// if the rows of the chat you left are still there when you come back. Keeping
// the *model* warm is not enough: handing a ListView a different model destroys
// every delegate it holds. Keeping the whole view warm and just hiding it is
// the only arrangement that can avoid the rebuild, and this pins down whether
// hiding actually preserves them.
void ChatBubblePerf::aHiddenTranscriptKeepsItsRowsSoComingBackToItIsFree()
{
    CollectionViewModel source;
    source.setReverseOrder(true);
    ProtocolMessageModel model(&source);
    for (int i = 0; i < 40; ++i) {
        const QString id = QStringLiteral("h%1").arg(i, 3, 10, QLatin1Char('0'));
        source.onUpsert(QStringLiteral("%1").arg(1'700'000'000 + i * 60, 20, 10, QLatin1Char('0')),
                        QJsonObject{
                            {QStringLiteral("id"), id},
                            {QStringLiteral("chat_id"), QStringLiteral("warm@g.us")},
                            {QStringLiteral("kind"), QStringLiteral("text")},
                            {QStringLiteral("text"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("fallback"), QStringLiteral("row %1").arg(i)},
                            {QStringLiteral("timestamp"), 1'700'000'000 + i * 60},
                            {QStringLiteral("direction"), QStringLiteral("incoming")},
                            {QStringLiteral("status"), QStringLiteral("read")},
                        });
    }

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/MessageView.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> view(component.createWithInitialProperties(
        {{QStringLiteral("model"), QVariant::fromValue<QObject *>(&model)}}));
    QVERIFY2(view, qPrintable(component.errorString()));
    auto *viewItem = qobject_cast<QQuickItem *>(view.get());
    viewItem->setParentItem(m_window->contentItem());
    viewItem->setWidth(700);
    viewItem->setHeight(420);
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));
    viewItem->setProperty("chatId", QStringLiteral("warm@g.us"));
    QTest::qWait(700);

    QQuickItem *list = findVisualChild(viewItem, QStringLiteral("messageList"));
    QVERIFY2(list, "the timeline has no list");
    QQuickItem *content = list->property("contentItem").value<QQuickItem *>();
    QVERIFY2(content, "the list has no content item");

    const auto builtRows = [&] {
        int n = 0;
        const auto children = content->childItems();
        for (QQuickItem *child : children) {
            if (!child->property("messageId").toString().isEmpty()) {
                ++n;
            }
        }
        return n;
    };

    const int whileShown = builtRows();
    QVERIFY2(whileShown > 0, "the transcript built no rows while visible");

    viewItem->setVisible(false);
    QTest::qWait(400);
    const int whileHidden = builtRows();

    viewItem->setVisible(true);
    QTest::qWait(400);
    const int afterReturning = builtRows();

    qInfo("DN9 %-24s shown=%d hidden=%d back=%d", "hidden-transcript",
          whileShown, whileHidden, afterReturning);

    QVERIFY2(afterReturning > 0, "the transcript came back empty");

    // The finding, and the licence for the warm pane pool: hiding a transcript
    // keeps every row it had built. ConversationPane carried a comment claiming
    // the opposite for a long time, which is why keeping panes per chat was
    // never tried; it is measurably untrue.
    //
    // If this ever starts failing, the pool has stopped paying for itself and
    // ChatPanes should go back to a single pane, because it would then be
    // holding N conversations' worth of objects for no saving at all.
    QCOMPARE(whileHidden, whileShown);
    QCOMPARE(afterReturning, whileShown);

    m_window->hide();
    viewItem->setParentItem(nullptr);
}

// What a fast scroll through a chat full of pictures, video and voice notes
// actually costs.
//
// The delegate budgets above measure one row at a time, and the chat-open
// benchmark measures building a window once. Neither catches the case people
// complain about most, which is flinging: rows leaving one end of the viewport
// and arriving at the other as fast as the view can manage. That is supposed to
// be nearly free, because ListView pools a row that scrolls out and hands it
// straight back with new data (reuseItems). If reuse breaks, every row on the
// way past is built from nothing instead, and on a media row that is upwards of
// a hundred objects apiece.
//
// So the number this asserts is not time, it is how many delegates were ever
// created. A working fling creates roughly a viewport's worth and then reuses
// them forever.
void ChatBubblePerf::flingingThroughAMediaHeavyChatReusesItsRows()
{
    CollectionViewModel source;
    source.setReverseOrder(true);
    ProtocolMessageModel model(&source);

    constexpr int kRows = 200;
    for (int i = 0; i < kRows; ++i) {
        const QString id = QStringLiteral("f%1").arg(i, 4, 10, QLatin1Char('0'));
        QJsonObject row{
            {QStringLiteral("id"), id},
            {QStringLiteral("chat_id"), QStringLiteral("heavy@g.us")},
            {QStringLiteral("timestamp"), 1'700'000'000 + i * 60},
            {QStringLiteral("direction"), (i % 3 == 0) ? QStringLiteral("outgoing")
                                                       : QStringLiteral("incoming")},
            {QStringLiteral("status"), QStringLiteral("read")},
            {QStringLiteral("fallback"), QStringLiteral("row %1").arg(i)},
            {QStringLiteral("sender"), QJsonObject{
                 {QStringLiteral("id"), QStringLiteral("%1@s.whatsapp.net").arg(i / 4)},
                 {QStringLiteral("name"), QStringLiteral("Person %1").arg(i / 4)}}},
        };
        // A real content-heavy conversation: mostly talk, with pictures, video
        // and voice notes through it.
        switch (i % 5) {
        case 0:
            row.insert(QStringLiteral("kind"), QStringLiteral("image"));
            row.insert(QStringLiteral("media"), QJsonObject{
                {QStringLiteral("mime"), QStringLiteral("image/jpeg")},
                {QStringLiteral("width"), 1280}, {QStringLiteral("height"), 720}});
            break;
        case 1:
            row.insert(QStringLiteral("kind"), QStringLiteral("video"));
            row.insert(QStringLiteral("media"), QJsonObject{
                {QStringLiteral("mime"), QStringLiteral("video/mp4")},
                {QStringLiteral("width"), 1280}, {QStringLiteral("height"), 720},
                {QStringLiteral("duration_secs"), 12}});
            break;
        case 2:
            row.insert(QStringLiteral("kind"), QStringLiteral("voice"));
            row.insert(QStringLiteral("media"), QJsonObject{
                {QStringLiteral("mime"), QStringLiteral("audio/ogg")},
                {QStringLiteral("duration_secs"), 7}});
            break;
        default:
            row.insert(QStringLiteral("kind"), QStringLiteral("text"));
            row.insert(QStringLiteral("text"),
                       QStringLiteral("a line of conversation that is long enough to wrap "
                                      "onto a second line in this pane (%1)").arg(i));
            break;
        }
        source.onUpsert(QStringLiteral("%1").arg(1'700'000'000 + i * 60, 20, 10, QLatin1Char('0')),
                        row);
    }
    QCOMPARE(model.rowCount(), kRows);

    QQmlComponent component(
        m_engine, QUrl(QStringLiteral("qrc:/qt/qml/Whatevr/qml/components/MessageView.qml")));
    QVERIFY2(!component.isError(), qPrintable(component.errorString()));
    std::unique_ptr<QObject> view(component.createWithInitialProperties(
        {{QStringLiteral("model"), QVariant::fromValue<QObject *>(&model)}}));
    QVERIFY2(view, qPrintable(component.errorString()));
    auto *viewItem = qobject_cast<QQuickItem *>(view.get());
    viewItem->setParentItem(m_window->contentItem());
    viewItem->setWidth(900);
    viewItem->setHeight(700);
    m_window->show();
    QVERIFY(QTest::qWaitForWindowExposed(m_window));
    viewItem->setProperty("chatId", QStringLiteral("heavy@g.us"));
    QTest::qWait(800);

    QQuickItem *list = findVisualChild(viewItem, QStringLiteral("messageList"));
    QVERIFY2(list, "the timeline has no list");
    QQuickItem *content = list->property("contentItem").value<QQuickItem *>();
    QVERIFY2(content, "the list has no content item");

    // Every distinct delegate this scroll ever puts on screen. Pointers can be
    // recycled by the allocator after a delete, which would undercount; the
    // objectName each row is stamped with makes an identity that cannot be.
    QSet<QString> everSeen;
    int settledRows = 0;
    const auto sample = [&] {
        int n = 0;
        const auto children = content->childItems();
        for (QQuickItem *child : children) {
            const QString id = child->property("messageId").toString();
            if (id.isEmpty()) {
                continue;
            }
            ++n;
            everSeen.insert(QStringLiteral("%1@%2")
                                .arg(QString::number(reinterpret_cast<quintptr>(child), 16), id));
        }
        return n;
    };
    settledRows = sample();
    QVERIFY2(settledRows > 0, "the transcript built no rows");

    // Fling up through history and back, in the steps a real fling moves in.
    const qreal top = list->property("contentY").toReal();
    const qreal step = list->height() * 0.6;
    QElapsedTimer timer;
    timer.start();
    for (int pass = 0; pass < 2; ++pass) {
        for (int i = 0; i < 24; ++i) {
            const qreal target = top - step * (pass == 0 ? (i + 1) : (24 - i));
            list->setProperty("contentY", target);
            QCoreApplication::processEvents();
            sample();
        }
    }
    const double flingMs = timer.nsecsElapsed() / 1e6;

    // Every (delegate, message) pairing the scroll produced. A row that is
    // reused keeps its pointer and gets a new id, so this grows by one per
    // *reuse*, which is what makes the ratio below meaningful.
    QSet<QString> pointers;
    for (const QString &seen : everSeen) {
        pointers.insert(seen.section(QLatin1Char('@'), 0, 0));
    }

    qInfo("DN9 %-24s viewport=%d  delegates=%d  pairings=%d  fling=%6.1f ms",
          "media-fling", settledRows, pointers.size(), everSeen.size(), flingMs);

    // The gate. Reuse working means the number of delegate objects stays close
    // to what the band holds, however far the scroll travels; reuse broken
    // means it climbs toward the number of rows passed.
    QVERIFY2(pointers.size() < kRows,
             qPrintable(QStringLiteral("a fling built %1 delegates for a %2-row chat: rows are "
                                       "not being reused, so every one that scrolls past is "
                                       "constructed from nothing")
                            .arg(pointers.size())
                            .arg(kRows)));

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
