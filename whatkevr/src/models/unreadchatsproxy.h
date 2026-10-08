#pragma once

#include <QSortFilterProxyModel>

// Unread-only view over a chats CollectionViewModel. Protocol v2 has no
// server-side unread/favorite filters, so the Unread sidebar filter runs over
// the same `all` subscription the Home filter uses instead of a dedicated
// daemon query. Rows carry their own `unread` counts, so the predicate is
// exact for the loaded window; extending the source extends this view.
class UnreadChatsProxy : public QSortFilterProxyModel
{
    Q_OBJECT

public:
    explicit UnreadChatsProxy(QObject *parent = nullptr);

protected:
    bool filterAcceptsRow(int sourceRow, const QModelIndex &sourceParent) const override;
};
