// SPDX-License-Identifier: BSD-3-Clause
#include <QQmlComponent>
#include <QQmlEngine>
#include <QQuickItem>
#include <QQuickWindow>
#include <QStringListModel>
#include <QTest>

#include "transcriptview.h"

/**
 * TranscriptView's height index.
 *
 * The transcript is drawn newest-first from the bottom, and the whole reason
 * this item exists is that a ListView owns contentHeight and revises it every
 * time another row materialises. A placement written as a pixel coordinate was
 * therefore stale by the next frame, which is what the settle timers, the repin
 * retries and the deadline properties in MessageView.qml were for.
 *
 * What these pin down is the property that replaces all of that: measuring a row
 * up in history changes the total without moving anything on screen.
 */
class TestTranscriptView : public QObject
{
    Q_OBJECT

private:
    // Rows whose delegate is exactly as tall as the number in the row, so a
    // test can state the geometry it expects rather than measure it.
    QQuickItem *build(QQmlEngine *engine, QQuickWindow *window, const QStringList &heights,
                      qreal viewHeight = 200, qreal spacing = 0)
    {
        auto *model = new QStringListModel(heights, engine);
        QQmlComponent component(engine);
        component.setData(QByteArrayLiteral(R"(
            import QtQuick
            import TranscriptViewTest
            TranscriptView {
                width: 100
                delegate: Item {
                    required property string display
                    height: Number(display)
                }
            }
        )"), QUrl());
        auto *view = qobject_cast<QQuickItem *>(component.create());
        if (!view) {
            qWarning("%s", qPrintable(component.errorString()));
            return nullptr;
        }
        view->setParentItem(window->contentItem());
        view->setHeight(viewHeight);
        view->setProperty("spacing", spacing);
        view->setProperty("model", QVariant::fromValue<QObject *>(model));
        QMetaObject::invokeMethod(view, "forceLayout");
        return view;
    }

private Q_SLOTS:
    void initTestCase()
    {
        qmlRegisterType<TranscriptView>("TranscriptViewTest", 1, 0, "TranscriptView");
    }

    // Row 0 is the newest and sits against the bottom; history climbs away
    // from it. That is the whole layout, and every other number follows.
    void theNewestRowSitsAtTheBottomAndHistoryClimbsFromIt()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        auto *view = build(&engine, &window, {QStringLiteral("50"), QStringLiteral("30"),
                                              QStringLiteral("20")});
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QCOMPARE(transcript->count(), 3);
        // Distance from the bottom to each row's bottom edge: its own height
        // plus everything newer than it.
        QCOMPARE(transcript->offsetFromBottom(0), 50.0);
        QCOMPARE(transcript->offsetFromBottom(1), 80.0);
        QCOMPARE(transcript->offsetFromBottom(2), 100.0);
        QCOMPARE(transcript->indexedHeight(), 100.0);
    }

    // Spacing is between rows, not after the last one.
    void spacingSitsBetweenRowsOnly()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        auto *view = build(&engine, &window,
                           {QStringLiteral("10"), QStringLiteral("10"), QStringLiteral("10")},
                           200, 4);
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QCOMPARE(transcript->offsetFromBottom(0), 10.0);
        QCOMPARE(transcript->offsetFromBottom(1), 24.0);
        QCOMPARE(transcript->offsetFromBottom(2), 38.0);
        QCOMPARE(transcript->indexedHeight(), 38.0);
    }

    // The defect this item exists to remove: a row up in history turning out
    // to be taller than estimated must not move the rows on screen.
    void measuringHistoryDoesNotMoveWhatIsOnScreen()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        for (int i = 0; i < 60; ++i) {
            // The rows past the viewport are much taller, so the estimate the
            // view starts with is badly wrong and correcting it moves the
            // total a long way.
            heights.append(i < 10 ? QStringLiteral("20") : QStringLiteral("120"));
        }
        auto *view = build(&engine, &window, heights);
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QMetaObject::invokeMethod(view, "positionViewAtBeginning");
        QTest::qWait(50);
        QMetaObject::invokeMethod(view, "forceLayout");

        // Parked at the newest message: whatever history turns out to weigh,
        // row 0's bottom edge is the bottom of the viewport.
        const qreal bottomOfRowZero = transcript->contentHeight() - transcript->offsetFromBottom(0);
        QCOMPARE(bottomOfRowZero + 20.0, transcript->contentY() + transcript->height());

        const qreal beforeTotal = transcript->contentHeight();
        // Walk into history so the tall rows are measured.
        transcript->setContentY(std::max(qreal(0), transcript->contentHeight() - 600));
        QTest::qWait(50);
        QMetaObject::invokeMethod(view, "forceLayout");
        QVERIFY2(transcript->contentHeight() > beforeTotal,
                 "the estimate was never corrected, so this proves nothing");

        // Back to the bottom, and the newest row is still exactly there.
        QMetaObject::invokeMethod(view, "positionViewAtBeginning");
        QMetaObject::invokeMethod(view, "forceLayout");
        QCOMPARE(transcript->contentHeight() - transcript->offsetFromBottom(0) + 20.0,
                 transcript->contentY() + transcript->height());
    }

    // originY stays zero, which is the number the old positioning could not
    // rely on: a ListView lays its built rows out from an arbitrary origin and
    // revises it with the running average.
    void theOriginNeverMoves()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        for (int i = 0; i < 40; ++i) {
            heights.append(QStringLiteral("%1").arg(20 + (i % 7) * 15));
        }
        auto *view = build(&engine, &window, heights);
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));

        auto *transcript = qobject_cast<TranscriptView *>(view);
        for (int step = 0; step < 6; ++step) {
            transcript->setContentY(step * 120);
            QTest::qWait(20);
            QMetaObject::invokeMethod(view, "forceLayout");
            QCOMPARE(transcript->originY(), 0.0);
        }
    }

    // indexAt answers in content coordinates and refuses the gap between rows,
    // the way ListView's does: the callers read a miss as "keep the last
    // answer" so the floating date pill does not flicker.
    void indexAtFindsARowAndRefusesTheGap()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        auto *view = build(&engine, &window,
                           {QStringLiteral("40"), QStringLiteral("40"), QStringLiteral("40")},
                           200, 10);
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        const qreal total = transcript->contentHeight();
        QCOMPARE(transcript->indexAt(10, total - 1), 0);
        QCOMPARE(transcript->indexAt(10, total - 39), 0);
        // Five pixels into the gap above row 0.
        QCOMPARE(transcript->indexAt(10, total - 45), -1);
        QCOMPARE(transcript->indexAt(10, total - 55), 1);
    }
};

QTEST_MAIN(TestTranscriptView)
#include "tst_transcriptview.moc"
