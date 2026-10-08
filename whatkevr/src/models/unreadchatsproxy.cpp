#include "unreadchatsproxy.h"

#include <QAbstractItemModel>

#include "collectionviewmodel.h"

namespace
{
int rowUnread(const QAbstractItemModel *model, int row)
{
    if (!model) {
        return 0;
    }
    const QVariantMap item =
        model->data(model->index(row, 0), whatevr::proto::CollectionViewModel::ItemRole).toMap();
    return item.value(QStringLiteral("unread")).toInt();
}
} // namespace

UnreadChatsProxy::UnreadChatsProxy(QObject *parent)
    : QSortFilterProxyModel(parent)
{
    // The source is already daemon-ordered; the proxy must not re-sort.
    setDynamicSortFilter(true);
}

bool UnreadChatsProxy::filterAcceptsRow(int sourceRow, const QModelIndex &sourceParent) const
{
    Q_UNUSED(sourceParent);
    return rowUnread(sourceModel(), sourceRow) > 0;
}
