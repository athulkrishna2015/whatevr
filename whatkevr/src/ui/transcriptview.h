// SPDX-License-Identifier: BSD-3-Clause
#pragma once

#include <QPointer>
#include <QQmlComponent>
#include <QQmlEngine>
#include <QVariant>
#include <QVector>

#include <QtQuick/private/qquickflickable_p.h>

#include <private/qqmldelegatemodel_p.h>

class TranscriptView;

/**
 * Attached object on each delegate, the way ListView's is: `TranscriptView.view`
 * names the view and `onPooled` / `onReused` bracket a row being recycled.
 */
class TranscriptViewAttached : public QObject
{
    Q_OBJECT
    QML_ANONYMOUS

    Q_PROPERTY(TranscriptView *view READ view NOTIFY viewChanged FINAL)

public:
    explicit TranscriptViewAttached(QObject *parent);

    [[nodiscard]] TranscriptView *view() const { return m_view; }
    void setView(TranscriptView *view);

    void emitPooled() { Q_EMIT pooled(); }
    void emitReused() { Q_EMIT reused(); }

Q_SIGNALS:
    void viewChanged();
    void pooled();
    void reused();

private:
    TranscriptView *m_view = nullptr;
};

/**
 * TranscriptView draws a chat transcript newest-first from the bottom up.
 *
 * It exists because a ListView owns the one number this view cannot let move.
 * A ListView's contentHeight is an estimate built from the rows it has already
 * built and revised every time another one materialises, and originY moves with
 * it, so a placement written as a pixel coordinate is stale by the next frame.
 * That is what the transcript's settle timers, repin retries and deadline
 * properties were for: they re-ran a placement until the number under it stopped
 * moving.
 *
 * Here the view owns the height index instead. Row 0 is the newest message and
 * sits against the bottom; every row's distance from the bottom is a prefix sum
 * over measured heights, contentHeight is that index's total, and originY is
 * always zero. A measurement that corrects an estimate updates the index and
 * holds the scroll distance from the bottom fixed, so the rows on screen do not
 * move: what shifts is history, thousands of pixels up where nobody is looking.
 * Placement is then one arithmetic write with nothing to settle.
 *
 * Delegates go through QQmlDelegateModel, the same machinery ListView uses, so
 * ChatBubble's required properties are still assigned in C++ and its
 * ahead-of-time compilation is untouched.
 */
class TranscriptView : public QQuickFlickable
{
    Q_OBJECT
    QML_NAMED_ELEMENT(TranscriptView)
    QML_ATTACHED(TranscriptViewAttached)

    Q_PROPERTY(QVariant model READ model WRITE setModel NOTIFY modelChanged FINAL)
    Q_PROPERTY(QQmlComponent *delegate READ delegate WRITE setDelegate NOTIFY delegateChanged FINAL)
    Q_PROPERTY(int count READ count NOTIFY countChanged FINAL)
    Q_PROPERTY(qreal spacing READ spacing WRITE setSpacing NOTIFY spacingChanged FINAL)
    /// Rows built beyond each edge of the viewport, in pixels. Zero builds only
    /// what is visible, which is what an open wants.
    Q_PROPERTY(qreal cacheBuffer READ cacheBuffer WRITE setCacheBuffer NOTIFY cacheBufferChanged FINAL)
    /// Drawn above the oldest row (the visual top), where older history would be.
    Q_PROPERTY(QQuickItem *footerItem READ footerItem WRITE setFooterItem NOTIFY footerItemChanged FINAL)

public:
    /// Where positionViewAtIndex puts the row. Same meanings as ListView's.
    enum PositionMode {
        Beginning = 0,
        Center = 1,
        End = 2,
        Visible = 3,
        Contain = 4,
    };
    Q_ENUM(PositionMode)

    explicit TranscriptView(QQuickItem *parent = nullptr);
    ~TranscriptView() override;

    static TranscriptViewAttached *qmlAttachedProperties(QObject *object);

    [[nodiscard]] QVariant model() const { return m_modelVariant; }
    void setModel(const QVariant &model);

    [[nodiscard]] QQmlComponent *delegate() const;
    void setDelegate(QQmlComponent *delegate);

    [[nodiscard]] int count() const;

    [[nodiscard]] qreal spacing() const { return m_spacing; }
    void setSpacing(qreal spacing);

    [[nodiscard]] qreal cacheBuffer() const { return m_cacheBuffer; }
    void setCacheBuffer(qreal buffer);

    [[nodiscard]] QQuickItem *footerItem() const { return m_footerItem; }
    void setFooterItem(QQuickItem *item);

    /// Row at this content coordinate, or -1 in the spacing between two rows.
    Q_INVOKABLE [[nodiscard]] int indexAt(qreal x, qreal y) const;
    /// The built delegate for this row, or null when it is outside the band.
    Q_INVOKABLE [[nodiscard]] QQuickItem *itemAtIndex(int index) const;
    /// Bring a row into view. `mode` takes the same values as ListView's
    /// (Beginning, Center, End, Visible, Contain), which is what the call sites
    /// already pass.
    Q_INVOKABLE void positionViewAtIndex(int index, int mode);
    /// The model's beginning, which on a bottom-up list is the visual bottom.
    Q_INVOKABLE void positionViewAtBeginning();
    Q_INVOKABLE void positionViewAtEnd();
    /// Build and measure now rather than at the next polish.
    Q_INVOKABLE void forceLayout();

    /// The index total, which is what contentHeight is set from. Exposed for
    /// the unit tests; nothing in QML needs it.
    [[nodiscard]] qreal indexedHeight() const;
    /// Distance from the bottom of the content up to this row's top edge.
    [[nodiscard]] qreal offsetFromBottom(int index) const;

Q_SIGNALS:
    void modelChanged();
    void delegateChanged();
    void countChanged();
    void spacingChanged();
    void cacheBufferChanged();
    void footerItemChanged();

protected:
    void geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry) override;
    void updatePolish() override;
    void componentComplete() override;
    void viewportMoved(Qt::Orientations orient) override;

private Q_SLOTS:
    void onModelUpdated(const QQmlChangeSet &changeSet, bool reset);
    void onInitItem(int index, QObject *object);

private:
    struct Row {
        qreal height = 0;
        bool measured = false;
    };

    // Fenwick tree over row heights, so a correction is a log-time update and
    // "which row is this far from the bottom" is a log-time search. A transcript
    // window is capped at a few thousand rows; the tree is what keeps a burst of
    // eighty measurements from being eighty full re-sums.
    void rebuildTree();
    void treeAdd(int index, qreal delta);
    [[nodiscard]] qreal treePrefix(int count) const;
    [[nodiscard]] int treeFind(qreal offset) const;

    void setRowHeight(int index, qreal height, bool measured);
    [[nodiscard]] qreal estimatedRowHeight() const;
    void syncContentHeight();

    void releaseItem(int index);
    void releaseAllItems();
    QQuickItem *requireItem(int index, bool async);
    void layoutRow(int index, QQuickItem *item);
    [[nodiscard]] qreal rowTop(int index) const;

    void scheduleLayout();
    void applyLayout();
    [[nodiscard]] qreal targetContentYFor(int index, int mode) const;
    void applyContentY(qreal y);

    QVariant m_modelVariant;
    QPointer<QQmlDelegateModel> m_delegateModel;
    bool m_ownsDelegateModel = false;

    QVector<Row> m_rows;
    QVector<qreal> m_tree;
    QHash<int, QQuickItem *> m_items;

    qreal m_spacing = 0;
    qreal m_cacheBuffer = 0;
    // Distance the viewport is scrolled up from the bottom. This, not contentY,
    // is what a bottom-anchored layout holds still: contentHeight moves as
    // history is measured, and holding contentY instead would slide the rows on
    // screen by exactly that much.
    qreal m_offsetFromBottom = 0;
    qreal m_measuredTotal = 0;
    int m_measuredCount = 0;
    bool m_layoutScheduled = false;
    bool m_inLayout = false;
    bool m_settingContentY = false;
    QQuickItem *m_footerItem = nullptr;
};
