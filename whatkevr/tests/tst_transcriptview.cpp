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
    QStringListModel *m_model = nullptr;

    QQuickItem *build(QQmlEngine *engine, QQuickWindow *window, const QStringList &heights,
                      qreal viewHeight = 200, qreal spacing = 0)
    {
        auto *model = new QStringListModel(heights, engine);
        m_model = model;
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

    // Every row in the band is actually placed, including the ones that were
    // incubated rather than built on the spot.
    //
    // Cache-band rows are requested asynchronously, and an async request comes
    // back as null with the model holding a reference. Dropping that request
    // meant the delegate arrived later with nothing to position it: the row
    // took up its height in the index and drew nothing, so a scrolled
    // transcript was half blank space with the messages bunched into whatever
    // was on screen when the band was last built synchronously.
    void everyBuiltRowIsPlacedIncludingIncubatedOnes()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        for (int i = 0; i < 40; ++i) {
            heights.append(QStringLiteral("30"));
        }
        auto *view = build(&engine, &window, heights);
        QVERIFY(view);
        // Two viewports each way, which is what the transcript runs with, so
        // most of the band is incubated rather than built synchronously.
        view->setProperty("cacheBuffer", 400);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(200);
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        int placed = 0;
        for (int i = 0; i < transcript->count(); ++i) {
            QQuickItem *item = transcript->itemAtIndex(i);
            if (!item) {
                continue;
            }
            ++placed;
            const qreal wantedTop = transcript->contentHeight() - transcript->offsetFromBottom(i);
            QVERIFY2(qAbs(item->y() - wantedTop) < 0.5,
                     qPrintable(QStringLiteral("row %1 is drawn at y %2, but the index puts it "
                                               "at %3").arg(i).arg(item->y()).arg(wantedTop)));
        }
        // The viewport alone is about 7 rows; the band has to have built more
        // than that or this proves nothing about incubation.
        QVERIFY2(placed > 10,
                 qPrintable(QStringLiteral("only %1 row(s) were built, so the cache band never "
                                           "incubated anything").arg(placed)));
    }

    // A delegate that settles its height after it was placed moves the rows
    // above it, and nothing else.
    //
    // Images resolve, Loaders complete and text wraps after the row has been
    // measured once. Without watching for that the index keeps the first
    // number forever, and every row above it is drawn at an offset that is
    // wrong by the difference, which is the gap between messages.
    void aRowThatGrowsAfterItWasPlacedMovesOnlyHistory()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        // Comfortably taller than the viewport: a transcript shorter than the
        // screen is bottom-aligned, which moves every row together and would
        // say nothing about what one measurement does.
        for (int i = 0; i < 40; ++i) {
            heights.append(QStringLiteral("30"));
        }
        auto *view = build(&engine, &window, heights, 200);
        QVERIFY(view);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "positionViewAtBeginning");
        QMetaObject::invokeMethod(view, "forceLayout");

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QQuickItem *grower = transcript->itemAtIndex(2);
        QVERIFY2(grower, "row 2 was never built");
        QVERIFY2(transcript->itemAtIndex(1), "row 1 was never built");
        // Screen position, not content position: the total moves when a row is
        // measured, so a content coordinate moves with it while the pixel the
        // reader is looking at does not.
        const qreal newerOnScreen = transcript->itemAtIndex(1)->y() - transcript->contentY();

        // The row turns out to be 70px taller than it first reported. Nothing
        // asks the view to lay out again: noticing is its job, which is the
        // whole point of this test, so no forceLayout here.
        grower->setHeight(100);
        QTRY_COMPARE(transcript->offsetFromBottom(2), 160.0);
        // Rows newer than it are positioned from the bottom and do not move.
        QCOMPARE(transcript->itemAtIndex(1)->y() - transcript->contentY(), newerOnScreen);
        // Rows older than it sit directly above it, with one gap between.
        QQuickItem *older = transcript->itemAtIndex(3);
        QVERIFY2(older, "row 3 was never built");
        QCOMPARE(older->y() + older->height(), grower->y());
    }

    // The model destroying a delegate the view is holding must not leave the
    // view holding it.
    //
    // A reset destroys every built item, and a removed row destroys its own.
    // The view answered neither, so m_items kept pointers into freed memory and
    // the next polish laid them out: opening a chat, which resets the model and
    // refills it, segfaulted in setWidth on a destroyed delegate.
    void aModelResetDoesNotLeaveTheViewHoldingDeadRows()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        for (int i = 0; i < 30; ++i) {
            heights.append(QStringLiteral("30"));
        }
        auto *view = build(&engine, &window, heights);
        QVERIFY(view);
        QVERIFY(m_model);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(50);

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QVERIFY2(transcript->itemAtIndex(transcript->count() - 1)
                     || transcript->itemAtIndex(0),
                 "no rows were built, so this exercises nothing");

        // A refill: exactly what opening a chat does to the transcript model.
        QStringList replacement;
        for (int i = 0; i < 25; ++i) {
            replacement.append(QStringLiteral("40"));
        }
        m_model->setStringList(replacement);
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(50);
        QMetaObject::invokeMethod(view, "forceLayout");

        QCOMPARE(transcript->count(), 25);
        for (int i = 0; i < transcript->count(); ++i) {
            if (QQuickItem *item = transcript->itemAtIndex(i)) {
                // Reaching into it at all is the point: a dangling pointer
                // faults here rather than answering.
                QVERIFY(item->height() >= 0);
            }
        }

        // And a removal, which destroys only the rows it takes.
        m_model->removeRows(0, 5);
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(50);
        QMetaObject::invokeMethod(view, "forceLayout");
        QCOMPARE(transcript->count(), 20);
        for (int i = 0; i < transcript->count(); ++i) {
            if (QQuickItem *item = transcript->itemAtIndex(i)) {
                QVERIFY(item->height() >= 0);
            }
        }
    }

    // A delegate that reaches the model while it is being laid out must not
    // take the layout down with it.
    //
    // Setting a row's width lays the delegate out, and a chat row's layout runs
    // real QML: Loaders instantiate, text shapes, bindings reach the
    // controller. If any of that changes the model, the old code released every
    // built row from inside the model signal, freeing the one the layout was
    // standing on, and returned into it. The backtrace was a fault inside
    // QQuickItem::setWidth, which is where opening a chat crashed.
    void aDelegateThatChangesTheModelMidLayoutDoesNotFreeTheRowUnderIt()
    {
        QQmlEngine engine;
        QQuickWindow window;
        window.resize(100, 200);
        QStringList heights;
        for (int i = 0; i < 30; ++i) {
            heights.append(QStringLiteral("30"));
        }
        auto *view = build(&engine, &window, heights);
        QVERIFY(view);
        QVERIFY(m_model);
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(50);

        auto *transcript = qobject_cast<TranscriptView *>(view);
        QQuickItem *row = nullptr;
        for (int i = 0; i < transcript->count() && !row; ++i) {
            row = transcript->itemAtIndex(i);
        }
        QVERIFY2(row, "no rows were built, so this exercises nothing");

        // Stand in for the delegate's own QML: the moment this row is given a
        // width, the model changes underneath the layout that is setting it.
        int fired = 0;
        QMetaObject::Connection reentry = connect(row, &QQuickItem::widthChanged,
                                                  transcript, [this, &fired] {
            if (fired++ > 0) {
                return;
            }
            QStringList replacement;
            for (int i = 0; i < 18; ++i) {
                replacement.append(QStringLiteral("45"));
            }
            m_model->setStringList(replacement);
        });

        view->setWidth(140);
        QMetaObject::invokeMethod(view, "forceLayout");
        QTest::qWait(100);
        QMetaObject::invokeMethod(view, "forceLayout");
        disconnect(reentry);

        QVERIFY2(fired > 0, "the re-entrant model change never happened");
        QCOMPARE(transcript->count(), 18);
        for (int i = 0; i < transcript->count(); ++i) {
            if (QQuickItem *item = transcript->itemAtIndex(i)) {
                QVERIFY(item->height() >= 0);
            }
        }
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
