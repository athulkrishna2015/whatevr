// SPDX-License-Identifier: BSD-3-Clause
#include "transcriptview.h"

#include <QQmlContext>
#include <QQmlInfo>
#include <private/qqmlchangeset_p.h>

#include <algorithm>
#include <cmath>

namespace
{
// What an unmeasured row is assumed to be before anything has been measured:
// one line of text in a bubble at a default font. Only ever used for the very
// first frame of a cold open; after that the running average of the rows this
// window has actually built is the estimate.
constexpr qreal kSeedRowHeight = 44;

} // namespace

TranscriptViewAttached::TranscriptViewAttached(QObject *parent)
    : QObject(parent)
{
}

void TranscriptViewAttached::setView(TranscriptView *view)
{
    if (m_view == view) {
        return;
    }
    m_view = view;
    Q_EMIT viewChanged();
}

TranscriptView::TranscriptView(QQuickItem *parent)
    : QQuickFlickable(parent)
{
    setFlickableDirection(QQuickFlickable::VerticalFlick);
    setBoundsBehavior(QQuickFlickable::StopAtBounds);
    setBoundsMovement(QQuickFlickable::FollowBoundsBehavior);
    setFlag(ItemIsFocusScope);
}

TranscriptView::~TranscriptView()
{
    releaseAllItems();
}

TranscriptViewAttached *TranscriptView::qmlAttachedProperties(QObject *object)
{
    return new TranscriptViewAttached(object);
}

// ---------------------------------------------------------------- model

void TranscriptView::setModel(const QVariant &model)
{
    if (m_modelVariant == model) {
        return;
    }
    QQmlComponent *keepDelegate = delegate();

    if (m_delegateModel) {
        disconnect(m_delegateModel, nullptr, this, nullptr);
        releaseAllItems();
        if (m_ownsDelegateModel) {
            delete m_delegateModel;
        }
        m_delegateModel = nullptr;
    }

    m_modelVariant = model;
    m_rows.clear();
    m_tree.clear();
    m_measuredTotal = 0;
    m_measuredCount = 0;
    m_offsetFromBottom = 0;

    QObject *object = qvariant_cast<QObject *>(model);
    if (auto *provided = qobject_cast<QQmlDelegateModel *>(object)) {
        m_delegateModel = provided;
        m_ownsDelegateModel = false;
    } else {
        auto *owned = new QQmlDelegateModel(qmlContext(this), this);
        owned->setModel(model);
        if (keepDelegate) {
            owned->setDelegate(keepDelegate);
        }
        m_delegateModel = owned;
        m_ownsDelegateModel = true;
    }

    connect(m_delegateModel, &QQmlInstanceModel::modelUpdated, this, &TranscriptView::onModelUpdated);
    connect(m_delegateModel, &QQmlInstanceModel::initItem, this, &TranscriptView::onInitItem);

    if (isComponentComplete()) {
        m_delegateModel->componentComplete();
    }

    m_rows.fill(Row{estimatedRowHeight(), false}, count());
    rebuildTree();
    syncContentHeight();
    scheduleLayout();

    Q_EMIT modelChanged();
    Q_EMIT countChanged();
}

QQmlComponent *TranscriptView::delegate() const
{
    return m_delegateModel ? m_delegateModel->delegate() : nullptr;
}

void TranscriptView::setDelegate(QQmlComponent *delegate)
{
    if (!m_delegateModel) {
        auto *owned = new QQmlDelegateModel(qmlContext(this), this);
        m_delegateModel = owned;
        m_ownsDelegateModel = true;
        connect(m_delegateModel, &QQmlInstanceModel::modelUpdated, this, &TranscriptView::onModelUpdated);
        connect(m_delegateModel, &QQmlInstanceModel::initItem, this, &TranscriptView::onInitItem);
        if (isComponentComplete()) {
            m_delegateModel->componentComplete();
        }
    }
    if (m_delegateModel->delegate() == delegate) {
        return;
    }
    releaseAllItems();
    m_delegateModel->setDelegate(delegate);
    scheduleLayout();
    Q_EMIT delegateChanged();
}

int TranscriptView::count() const
{
    return m_delegateModel ? m_delegateModel->count() : 0;
}

void TranscriptView::setSpacing(qreal spacing)
{
    if (qFuzzyCompare(m_spacing, spacing)) {
        return;
    }
    m_spacing = spacing;
    syncContentHeight();
    scheduleLayout();
    Q_EMIT spacingChanged();
}

void TranscriptView::setCacheBuffer(qreal buffer)
{
    buffer = std::max(qreal(0), buffer);
    if (qFuzzyCompare(m_cacheBuffer, buffer)) {
        return;
    }
    m_cacheBuffer = buffer;
    scheduleLayout();
    Q_EMIT cacheBufferChanged();
}

void TranscriptView::setFooterItem(QQuickItem *item)
{
    if (m_footerItem == item) {
        return;
    }
    m_footerItem = item;
    if (m_footerItem) {
        m_footerItem->setParentItem(contentItem());
    }
    syncContentHeight();
    scheduleLayout();
    Q_EMIT footerItemChanged();
}

// ---------------------------------------------------------------- height index

// A Fenwick tree is one-based; index i of m_rows lives at tree slot i + 1.
void TranscriptView::rebuildTree()
{
    const int n = m_rows.size();
    m_tree.assign(n + 1, 0);
    for (int i = 0; i < n; ++i) {
        int slot = i + 1;
        m_tree[slot] += m_rows.at(i).height;
        const int parent = slot + (slot & -slot);
        if (parent <= n) {
            m_tree[parent] += m_tree[slot];
        }
    }
}

void TranscriptView::treeAdd(int index, qreal delta)
{
    const int n = m_rows.size();
    for (int slot = index + 1; slot <= n; slot += slot & -slot) {
        m_tree[slot] += delta;
    }
}

qreal TranscriptView::treePrefix(int count) const
{
    qreal sum = 0;
    for (int slot = std::min(count, int(m_rows.size())); slot > 0; slot -= slot & -slot) {
        sum += m_tree.at(slot);
    }
    return sum;
}

// The largest row count whose heights sum to at most `offset`. That count is
// also the index of the row the offset lands in.
int TranscriptView::treeFind(qreal offset) const
{
    const int n = m_rows.size();
    int pos = 0;
    int step = 1;
    while ((step << 1) <= n) {
        step <<= 1;
    }
    for (; step > 0; step >>= 1) {
        const int next = pos + step;
        if (next <= n && m_tree.at(next) <= offset) {
            pos = next;
            offset -= m_tree.at(next);
        }
    }
    return pos;
}

qreal TranscriptView::estimatedRowHeight() const
{
    if (m_measuredCount > 0) {
        return m_measuredTotal / m_measuredCount;
    }
    return kSeedRowHeight;
}

void TranscriptView::setRowHeight(int index, qreal height, bool measured)
{
    if (index < 0 || index >= m_rows.size()) {
        return;
    }
    Row &row = m_rows[index];
    if (measured && !row.measured) {
        m_measuredCount += 1;
        m_measuredTotal += height;
    } else if (measured && row.measured) {
        m_measuredTotal += height - row.height;
    }
    const qreal delta = height - row.height;
    row.height = height;
    row.measured = measured;
    if (!qFuzzyIsNull(delta)) {
        treeAdd(index, delta);
        syncContentHeight();
    }
}

qreal TranscriptView::indexedHeight() const
{
    const int n = m_rows.size();
    if (n == 0) {
        return m_footerItem ? m_footerItem->height() : 0;
    }
    qreal total = treePrefix(n) + m_spacing * (n - 1);
    if (m_footerItem) {
        total += m_footerItem->height();
    }
    return total;
}

// Distance from the bottom of the content up to this row's top edge: its own
// height, everything newer than it, and the gaps between them.
qreal TranscriptView::offsetFromBottom(int index) const
{
    if (m_rows.isEmpty()) {
        return 0;
    }
    index = std::clamp(index, 0, int(m_rows.size()) - 1);
    return treePrefix(index + 1) + m_spacing * index;
}

qreal TranscriptView::rowTop(int index) const
{
    // Row 0's bottom edge is the bottom of the content; history climbs upward
    // from it. This is the whole layout: no row's position depends on any row
    // older than itself, so measuring history never moves what is on screen.
    return contentHeight() - offsetFromBottom(index);
}

void TranscriptView::syncContentHeight()
{
    const qreal total = std::max(indexedHeight(), height());
    if (qFuzzyCompare(contentHeight(), total)) {
        return;
    }
    setContentHeight(total);
    // Hold the distance from the bottom, not contentY. History being measured
    // changes the total; pinning contentY instead would slide every row on
    // screen by the same amount, which is the shove this view exists to avoid.
    const qreal wanted = std::max(qreal(0), total - height() - m_offsetFromBottom);
    if (!qFuzzyCompare(contentY(), wanted)) {
        m_settingContentY = true;
        setContentY(wanted);
        m_settingContentY = false;
    }
}

// ---------------------------------------------------------------- items

void TranscriptView::onInitItem(int index, QObject *object)
{
    Q_UNUSED(index)
    if (auto *item = qobject_cast<QQuickItem *>(object)) {
        item->setParentItem(contentItem());
    }
}

QQuickItem *TranscriptView::requireItem(int index, bool async)
{
    if (auto *existing = m_items.value(index, nullptr)) {
        return existing;
    }
    if (!m_delegateModel || index < 0 || index >= count()) {
        return nullptr;
    }
    QObject *object = m_delegateModel->object(
        index, async ? QQmlIncubator::Asynchronous : QQmlIncubator::AsynchronousIfNested);
    if (!object) {
        return nullptr; // still incubating; a later polish picks it up
    }
    auto *item = qobject_cast<QQuickItem *>(object);
    if (!item) {
        m_delegateModel->release(object);
        return nullptr;
    }
    item->setParentItem(contentItem());
    if (auto *attached = qobject_cast<TranscriptViewAttached *>(
            qmlAttachedPropertiesObject<TranscriptView>(item, false))) {
        attached->setView(this);
        attached->emitReused();
    }
    m_items.insert(index, item);
    return item;
}

void TranscriptView::releaseItem(int index)
{
    QQuickItem *item = m_items.take(index);
    if (!item) {
        return;
    }
    if (auto *attached = qobject_cast<TranscriptViewAttached *>(
            qmlAttachedPropertiesObject<TranscriptView>(item, false))) {
        attached->emitPooled();
    }
    // Pooled rather than destroyed: the reuse pool is what makes flicking
    // through a media-heavy chat cost nothing per row after the first screen.
    if (m_delegateModel->release(item, QQmlInstanceModel::Reusable)
        & QQmlInstanceModel::Destroyed) {
        return;
    }
    item->setVisible(false);
}

void TranscriptView::releaseAllItems()
{
    const QList<int> indexes = m_items.keys();
    for (int index : indexes) {
        releaseItem(index);
    }
    m_items.clear();
}

void TranscriptView::layoutRow(int index, QQuickItem *item)
{
    item->setWidth(width());
    item->setVisible(true);
    const qreal measured = item->height();
    if (measured > 0) {
        setRowHeight(index, measured, true);
    }
    item->setY(rowTop(index));
}

// ---------------------------------------------------------------- layout

void TranscriptView::scheduleLayout()
{
    if (m_layoutScheduled || m_inLayout) {
        return;
    }
    m_layoutScheduled = true;
    polish();
}

void TranscriptView::updatePolish()
{
    m_layoutScheduled = false;
    applyLayout();
}

void TranscriptView::forceLayout()
{
    m_layoutScheduled = false;
    applyLayout();
}

void TranscriptView::applyLayout()
{
    if (m_inLayout || !isComponentComplete() || !m_delegateModel || width() <= 0) {
        return;
    }
    m_inLayout = true;

    const int n = m_rows.size();
    if (n == 0) {
        releaseAllItems();
        syncContentHeight();
        if (m_footerItem) {
            m_footerItem->setWidth(width());
            m_footerItem->setY(0);
        }
        m_inLayout = false;
        return;
    }

    // Two passes. The first builds and measures, which can move contentHeight
    // under the band it was chosen from; the second settles on the corrected
    // index. Two is enough because a measurement only moves rows older than
    // itself, so the band's far edge is the only thing that can shift.
    for (int pass = 0; pass < 2; ++pass) {
        const qreal viewTop = contentY() - m_cacheBuffer;
        const qreal viewBottom = contentY() + height() + m_cacheBuffer;

        // Content coordinates run top-down, row indexes run bottom-up, so the
        // top of the viewport is the *oldest* row in the band.
        const qreal bottomOffset = std::max(qreal(0), contentHeight() - viewBottom);
        const qreal topOffset = std::max(qreal(0), contentHeight() - viewTop);
        int first = treeFind(bottomOffset);
        int last = treeFind(topOffset);
        first = std::clamp(first, 0, n - 1);
        last = std::clamp(last, first, n - 1);

        const QList<int> held = m_items.keys();
        for (int index : held) {
            if (index < first || index > last || index >= n) {
                releaseItem(index);
            }
        }

        // The viewport is built synchronously so the first frame is complete;
        // the cache band incubates, which is what keeps it off the open's
        // critical path.
        const qreal visibleTop = contentY();
        const qreal visibleBottom = contentY() + height();
        for (int index = first; index <= last; ++index) {
            const qreal top = rowTop(index);
            const bool visible = top < visibleBottom && top + m_rows.at(index).height > visibleTop;
            if (QQuickItem *item = requireItem(index, !visible)) {
                layoutRow(index, item);
            }
        }
    }

    if (m_footerItem) {
        m_footerItem->setWidth(width());
        m_footerItem->setY(rowTop(n - 1) - m_spacing - m_footerItem->height());
    }
    syncContentHeight();

    m_inLayout = false;
}

// ---------------------------------------------------------------- queries

int TranscriptView::indexAt(qreal x, qreal y) const
{
    Q_UNUSED(x)
    const int n = m_rows.size();
    if (n == 0) {
        return -1;
    }
    const qreal offset = contentHeight() - y;
    if (offset < 0) {
        return -1;
    }
    const int index = std::clamp(treeFind(offset), 0, n - 1);
    // Reject the gap between two rows, the way ListView's indexAt does: the
    // callers use a miss to mean "keep the last answer" rather than to move.
    const qreal top = rowTop(index);
    if (y < top || y > top + m_rows.at(index).height) {
        return -1;
    }
    return index;
}

QQuickItem *TranscriptView::itemAtIndex(int index) const
{
    return m_items.value(index, nullptr);
}

void TranscriptView::positionViewAtIndex(int index, int mode)
{
    if (index < 0 || index >= m_rows.size()) {
        return;
    }
    // Twice, and the second pass is what makes this exact. A row that has not
    // been built yet is in the index at an estimate, so the first placement is
    // approximate; it is also what brings the row into the band, so the layout
    // it triggers measures it. The second placement reads the corrected height.
    // Note that only the row's own height was wrong: a row's top edge does not
    // depend on anything older than it, so nothing else in the band moved.
    for (int pass = 0; pass < 2; ++pass) {
        applyContentY(targetContentYFor(index, mode));
        forceLayout();
    }
}

// Where contentY has to be for this row to sit where `mode` asks.
qreal TranscriptView::targetContentYFor(int index, int mode) const
{
    const qreal top = rowTop(index);
    const qreal rowHeight = m_rows.at(index).height;
    const qreal bottom = top + rowHeight;
    qreal target = contentY();
    switch (mode) {
    case TranscriptView::Beginning:
        target = top;
        break;
    case TranscriptView::Center:
        target = top - (height() - rowHeight) / 2;
        break;
    case TranscriptView::End:
        target = bottom - height();
        break;
    case TranscriptView::Visible:
    case TranscriptView::Contain:
    default:
        if (top < contentY()) {
            target = top;
        } else if (bottom > contentY() + height()) {
            target = bottom - height();
        }
        break;
    }
    return std::clamp(target, qreal(0), std::max(qreal(0), contentHeight() - height()));
}

// Move the viewport without the move being read back as the reader scrolling.
void TranscriptView::applyContentY(qreal y)
{
    m_settingContentY = true;
    setContentY(y);
    m_settingContentY = false;
    m_offsetFromBottom = std::max(qreal(0), contentHeight() - height() - y);
}

void TranscriptView::positionViewAtBeginning()
{
    // The model's beginning is the newest message, which is the visual bottom.
    applyContentY(std::max(qreal(0), contentHeight() - height()));
    forceLayout();
    // Whatever the layout measured, parked at the bottom means parked at the
    // bottom; the total may have moved under the first write.
    applyContentY(std::max(qreal(0), contentHeight() - height()));
}

void TranscriptView::positionViewAtEnd()
{
    applyContentY(0);
    forceLayout();
    applyContentY(0);
}

// ---------------------------------------------------------------- plumbing

void TranscriptView::geometryChange(const QRectF &newGeometry, const QRectF &oldGeometry)
{
    QQuickFlickable::geometryChange(newGeometry, oldGeometry);
    if (newGeometry.size() != oldGeometry.size()) {
        if (!qFuzzyCompare(newGeometry.width(), oldGeometry.width())) {
            // Every row has to be re-measured at the new width, so the index is
            // estimates again until they are.
            for (int i = 0; i < m_rows.size(); ++i) {
                m_rows[i].measured = false;
            }
            m_measuredTotal = 0;
            m_measuredCount = 0;
        }
        syncContentHeight();
        scheduleLayout();
    }
}

void TranscriptView::viewportMoved(Qt::Orientations orient)
{
    QQuickFlickable::viewportMoved(orient);
    if (!m_settingContentY && !m_inLayout) {
        m_offsetFromBottom = std::max(qreal(0), contentHeight() - height() - contentY());
    }
    scheduleLayout();
}

void TranscriptView::componentComplete()
{
    if (m_delegateModel) {
        m_delegateModel->componentComplete();
        m_rows.fill(Row{estimatedRowHeight(), false}, count());
        rebuildTree();
    }
    QQuickFlickable::componentComplete();
    syncContentHeight();
    scheduleLayout();
}

void TranscriptView::onModelUpdated(const QQmlChangeSet &changeSet, bool reset)
{
    const qreal estimate = estimatedRowHeight();

    if (reset) {
        releaseAllItems();
        m_rows.fill(Row{estimate, false}, count());
        rebuildTree();
        m_offsetFromBottom = 0;
        syncContentHeight();
        scheduleLayout();
        Q_EMIT countChanged();
        return;
    }

    // Structural changes renumber the rows the built items are keyed by, so the
    // band is dropped and rebuilt from the new numbering. The reuse pool keeps
    // the delegates themselves, which is the cost that matters.
    bool structural = false;
    for (const QQmlChangeSet::Change &remove : changeSet.removes()) {
        structural = true;
        for (int i = 0; i < remove.count; ++i) {
            const int index = remove.index;
            if (index >= 0 && index < m_rows.size()) {
                if (m_rows.at(index).measured) {
                    m_measuredCount -= 1;
                    m_measuredTotal -= m_rows.at(index).height;
                }
                m_rows.remove(index);
            }
        }
    }
    for (const QQmlChangeSet::Change &insert : changeSet.inserts()) {
        structural = true;
        for (int i = 0; i < insert.count; ++i) {
            const int index = std::clamp(insert.index + i, 0, int(m_rows.size()));
            m_rows.insert(index, Row{estimate, false});
        }
    }
    for (const QQmlChangeSet::Change &change : changeSet.changes()) {
        for (int i = 0; i < change.count; ++i) {
            const int index = change.index + i;
            if (index >= 0 && index < m_rows.size()) {
                // Its content changed, so its height is an estimate again until
                // the delegate is re-measured.
                if (m_rows.at(index).measured) {
                    m_measuredCount -= 1;
                    m_measuredTotal -= m_rows.at(index).height;
                    m_rows[index].measured = false;
                }
            }
        }
    }

    if (m_rows.size() != count()) {
        const int had = m_rows.size();
        m_rows.resize(count());
        for (int i = had; i < m_rows.size(); ++i) {
            m_rows[i] = Row{estimate, false};
        }
        structural = true;
    }
    if (structural) {
        releaseAllItems();
    }
    rebuildTree();
    syncContentHeight();
    scheduleLayout();
    Q_EMIT countChanged();
}
