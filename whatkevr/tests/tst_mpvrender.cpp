// SPDX-License-Identifier: BSD-3-Clause
//
// Does video actually reach the screen, and does it survive being moved between
// views?
//
// Both questions used to be answerable only by running the app and looking at
// it, which is how a whole release shipped playing sound over a black
// rectangle: mpv decides whether a file has video when it opens it, and a file
// opened before its render context exists plays through to the end with the
// video track switched off. This test opens a real engine, renders a real
// frame into a real scene graph and reads the pixels back.
//
// The fixture is uncompressed YUV4MPEG written here rather than an encoded clip
// checked in: it needs no encoder, no fixture file and no codec to be present,
// and mpv demuxes it by content like anything else.

#include <QGuiApplication>
#include <QImage>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSGRendererInterface>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QTest>

#include "mpvcore.h"
#include "playbacksession.h"

namespace
{

constexpr int frameWidth = 64;
constexpr int frameHeight = 64;
constexpr int frameCount = 30;
/// Mid-grey luma, well clear of the black mpv clears an empty framebuffer to.
constexpr char lumaValue = char(0xB0);
constexpr char chromaValue = char(0x80);

/// A grey clip as YUV4MPEG2, the simplest thing a demuxer will accept.
bool writeFixtureSized(const QString &path, int width, int height)
{
    QFile file(path);
    if (!file.open(QIODevice::WriteOnly)) {
        return false;
    }
    file.write(QStringLiteral("YUV4MPEG2 W%1 H%2 F10:1 Ip A1:1 C420\n")
                   .arg(width)
                   .arg(height)
                   .toUtf8());
    const QByteArray luma(width * height, lumaValue);
    const QByteArray chroma((width / 2) * (height / 2), chromaValue);
    for (int i = 0; i < frameCount; ++i) {
        file.write("FRAME\n");
        file.write(luma);
        file.write(chroma);
        file.write(chroma);
    }
    file.close();
    return true;
}

bool writeFixture(const QString &path)
{
    return writeFixtureSized(path, frameWidth, frameHeight);
}

/// Whether anything brighter than the cleared framebuffer was drawn.
bool hasBrightPixels(const QImage &image)
{
    for (int y = 0; y < image.height(); ++y) {
        for (int x = 0; x < image.width(); ++x) {
            const QRgb pixel = image.pixel(x, y);
            if (qRed(pixel) > 60 && qGreen(pixel) > 60 && qBlue(pixel) > 60) {
                return true;
            }
        }
    }
    return false;
}

}

class TestMpvRender : public QObject
{
    Q_OBJECT

private Q_SLOTS:
    void initTestCase()
    {
        // Same pin as main(): libmpv renders through OpenGL and a scene graph on
        // any other RHI backend cannot show a frame at all.
        QQuickWindow::setGraphicsApi(QSGRendererInterface::OpenGL);

        QVERIFY(m_dir.isValid());
        m_fixture = m_dir.filePath(QStringLiteral("grey.y4m"));
        QVERIFY(writeFixture(m_fixture));

        MpvCore probe(MpvCore::Mode::Video);
        if (!probe.isValid()) {
            QSKIP("libmpv could not create an instance here");
        }
    }

    void aSessionDrawsItsClipIntoTheViewThatHoldsIt()
    {
        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            // The offscreen platform falls back to the software renderer, which
            // has no framebuffer objects and so nothing for mpv to draw into.
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(m_fixture), 0.0);
        session.setPlaying(true);

        // hasVideo is mpv reporting a decoded size, which it only has once a
        // frame exists: the property every bubble swaps its poster out on.
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);
        QTRY_VERIFY_WITH_TIMEOUT(hasBrightPixels(window.grabWindow()), 15000);
    }

    // A video note is a circle at a fixed diameter, so a clip that is not square
    // has to fill it. mpv preserves aspect inside whatever item it is given, so
    // an item sized exactly to the square slot gets the clip plus a black matte,
    // and the circle then frames the matte instead of the face. Covering sizes
    // the item to the clip's own shape, large enough to overhang the slot, and
    // centres it; the view's texture capture crops the overhang.
    void aCoveredClipFillsItsSlotInsteadOfBeingMatted()
    {
        const QString wide = m_dir.filePath(QStringLiteral("wide.y4m"));
        QVERIFY(writeFixtureSized(wide, frameWidth, frameHeight / 2));

        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight)); // a square slot
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.setCoverContainer(true);
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("note"), QUrl::fromLocalFile(wide), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        QQuickItem *video = nullptr;
        QTRY_VERIFY_WITH_TIMEOUT([&] {
            for (QQuickItem *child : container->childItems()) {
                if (child->width() > 0 && child->height() > 0) {
                    video = child;
                    return true;
                }
            }
            return false;
        }(), 15000);

        // Both axes are covered, so no part of the circle is left unpainted.
        QVERIFY2(video->width() >= container->width() - 0.5
                     && video->height() >= container->height() - 0.5,
                 qPrintable(QStringLiteral("a %1x%2 clip drew %3x%4 inside a %5x%6 slot, "
                                           "so the slot is not filled")
                                .arg(frameWidth)
                                .arg(frameHeight / 2)
                                .arg(video->width())
                                .arg(video->height())
                                .arg(container->width())
                                .arg(container->height())));

        // Filling by stretching would be worse than the matte, so the clip keeps
        // its own shape: a 2:1 source stays 2:1.
        const qreal aspect = video->width() / video->height();
        QVERIFY2(qAbs(aspect - 2.0) < 0.05,
                 qPrintable(QStringLiteral("the covered clip was drawn at aspect %1, not its own 2.0")
                                .arg(aspect)));

        // Centred, so the crop takes the same amount off each side rather than
        // cutting the whole overhang off one edge.
        QVERIFY2(qAbs(video->x() - (container->width() - video->width()) / 2) < 0.5
                     && qAbs(video->y() - (container->height() - video->height()) / 2) < 0.5,
                 "the covered clip was not centred in its slot");
    }

    // The other half of the same contract: everything that is not a video note
    // keeps mpv's own fit, because those slots already carry the clip's shape
    // and full-screen playback must never crop the picture.
    void anUncoveredClipStillFitsItsView()
    {
        const QString wide = m_dir.filePath(QStringLiteral("wide-fit.y4m"));
        QVERIFY(writeFixtureSized(wide, frameWidth, frameHeight / 2));

        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(wide), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        QQuickItem *video = nullptr;
        for (QQuickItem *child : container->childItems()) {
            if (child->width() > 0 && child->height() > 0) {
                video = child;
                break;
            }
        }
        QVERIFY(video);
        // The item is the container; mpv letterboxes inside it, which is what a
        // rectangular bubble and the full-screen viewer both want.
        QCOMPARE(video->size(), container->size());
    }

    void aStillCanBeTakenFromWhatIsOnScreen()
    {
        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            // The offscreen platform falls back to the software renderer, which
            // has no framebuffer objects and so nothing for mpv to draw into.
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(m_fixture), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        QSignalSpy stills(&session, &PlaybackSession::stillGrabbed);
        session.captureStill();
        // Asked for, not waited for: the request returns at once and mpv
        // answers on its own thread, which is what keeps a clip leaving the
        // viewport from stalling the frame it leaves on.
        QCOMPARE(stills.count(), 0);
        QTRY_VERIFY_WITH_TIMEOUT(stills.count() == 1, 15000);
        QCOMPARE(stills.first().at(0).toString(), QStringLiteral("clip"));
        const QImage still = stills.first().at(1).value<QImage>();
        QCOMPARE(still.size(), QSize(frameWidth, frameHeight));
        QVERIFY(hasBrightPixels(still));
    }

    void movingASessionToAnotherViewKeepsTheClipRunningAndDrawing()
    {
        QQuickWindow window;
        window.resize(frameWidth, frameHeight * 2);
        auto *first = new QQuickItem(window.contentItem());
        first->setSize(QSizeF(frameWidth, frameHeight));
        auto *second = new QQuickItem(window.contentItem());
        second->setY(frameHeight);
        second->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            // The offscreen platform falls back to the software renderer, which
            // has no framebuffer objects and so nothing for mpv to draw into.
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(first);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(m_fixture), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        QSignalSpy sources(&session, &PlaybackSession::sourceChanged);
        // What a handoff does: the view being left goes first (it is revoked
        // before the new one is handed the session), and the file is never
        // reopened.
        session.detachView(first);
        session.attachView(second);
        QCOMPARE(sources.count(), 0);
        QVERIFY(session.playing());

        // The picture comes back in the new view. It may take a moment: leaving
        // the scene frees mpv's render context, which takes the video track
        // with it, and the session reopens the track once a context exists
        // again.
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);
        QTRY_VERIFY_WITH_TIMEOUT(hasBrightPixels(window.grabWindow()), 15000);
    }

    void aViewComingBackDoesNotRebuildTheRenderContext()
    {
        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(m_fixture), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        // A bubble scrolling out of the viewport and back. Rebuilding mpv's
        // render context each time is GPU work on the render thread, which is
        // the frame the scroll cannot spare, so the item waits out of sight in
        // the same scene instead of leaving it.
        session.setPlaying(false);
        session.detachView(container);
        QTest::qWait(200);
        session.attachView(container);
        session.setPlaying(true);

        QVERIFY(session.hasVideo());
        QTRY_VERIFY_WITH_TIMEOUT(hasBrightPixels(window.grabWindow()), 15000);
    }

    void playbackKeepsRunningWhenNobodyIsRendering()
    {
        QQuickWindow window;
        window.resize(frameWidth, frameHeight);
        auto *container = new QQuickItem(window.contentItem());
        container->setSize(QSizeF(frameWidth, frameHeight));
        window.show();
        if (!QTest::qWaitForWindowExposed(&window)) {
            QSKIP("no exposed window on this platform");
        }
        if (window.rendererInterface()->graphicsApi() != QSGRendererInterface::OpenGL) {
            QSKIP("scene graph is not on OpenGL here");
        }

        PlaybackSession session;
        session.attachView(container);
        session.setMuted(true);
        session.configure(QStringLiteral("clip"), QUrl::fromLocalFile(m_fixture), 0.0);
        session.setPlaying(true);
        QTRY_VERIFY_WITH_TIMEOUT(session.hasVideo(), 15000);

        // Rendering under advanced control is a conversation: mpv asks for a
        // frame to be drawn and waits to be asked what to draw. A window that
        // stops rendering (hidden, occluded, another workspace) stops
        // answering, and if that could stall the engine, a minimised window
        // would take the sound with it.
        window.hide();
        const double before = session.position();
        QTest::qWait(1500);
        QVERIFY2(session.position() > before + 0.5,
                 qPrintable(QStringLiteral("position stopped at %1 with the window hidden").arg(session.position())));
    }

private:
    QTemporaryDir m_dir;
    QString m_fixture;
};

QTEST_MAIN(TestMpvRender)
#include "tst_mpvrender.moc"
