// SPDX-License-Identifier: BSD-3-Clause
#pragma once

#include <QAbstractItemModel>
#include <QObject>
#include <QQmlEngine>
#include <QString>

namespace whatevr::proto
{
class CollectionViewModel;
class Subscription;
}

class ProtocolMessageModel;

/**
 * ConversationSession is one open transcript: a chat at an anchor, the two
 * models behind it, its subscription, and everything about where the reader
 * was left.
 *
 * All of that used to live in single-valued members on ProtocolController and
 * was copied in and out of a per-window struct on every chat switch, which is
 * why seven separate handlers had to ask "is this window the one on screen?"
 * before writing anything. A session writes only its own fields, so there is
 * nothing to guard against and a reply that arrives after a switch lands where
 * it belongs.
 *
 * The pool owns these; QML binds to the one the controller names as active.
 */
class ConversationSession : public QObject
{
    Q_OBJECT
    QML_ELEMENT
    QML_UNCREATABLE("Conversation sessions are owned by ProtocolController")

    Q_PROPERTY(QString chatId MEMBER chatId CONSTANT FINAL)
    Q_PROPERTY(QString anchor MEMBER anchor CONSTANT FINAL)
    Q_PROPERTY(QAbstractItemModel *model READ messageModel CONSTANT FINAL)

    Q_PROPERTY(bool messagesLoading READ messagesLoading NOTIFY changed FINAL)
    Q_PROPERTY(bool messagesEmpty READ messagesEmpty NOTIFY changed FINAL)
    Q_PROPERTY(bool olderMessagesLoading MEMBER olderLoading NOTIFY changed FINAL)
    Q_PROPERTY(bool newerMessagesLoading MEMBER newerLoading NOTIFY changed FINAL)
    Q_PROPERTY(bool canLoadOlderMessages READ canLoadOlderMessages NOTIFY changed FINAL)
    Q_PROPERTY(bool canLoadNewerMessages READ canLoadNewerMessages NOTIFY changed FINAL)
    Q_PROPERTY(bool olderMessagesFailed MEMBER olderFailed NOTIFY changed FINAL)
    Q_PROPERTY(bool newerMessagesFailed MEMBER newerFailed NOTIFY changed FINAL)
    Q_PROPERTY(bool messagesAtLiveEdge MEMBER atLiveEdge NOTIFY changed FINAL)
    Q_PROPERTY(bool phoneHistoryRequesting MEMBER phoneHistoryRequesting NOTIFY changed FINAL)
    Q_PROPERTY(bool messagesReloading MEMBER reloading NOTIFY changed FINAL)
    Q_PROPERTY(QString displayedMessagesChatId MEMBER displayedChatId NOTIFY changed FINAL)
    Q_PROPERTY(QString messageErrorText MEMBER errorText NOTIFY changed FINAL)

    Q_PROPERTY(QString unreadAnchorMessageId MEMBER unreadAnchorMessageId NOTIFY unreadAnchorChanged FINAL)
    Q_PROPERTY(int unreadAnchorCount MEMBER unreadAnchorCount NOTIFY unreadAnchorChanged FINAL)
    Q_PROPERTY(bool unreadAnchorResolving MEMBER unreadAnchorResolving NOTIFY unreadAnchorChanged FINAL)

public:
    explicit ConversationSession(QObject *parent = nullptr);
    ~ConversationSession() override;

    // Identity. A chat pinned at the unread divider and the same chat at the
    // live edge put the reader in different places, so the anchor is part of it.
    QString chatId;
    QString anchor;

    whatevr::proto::CollectionViewModel *source = nullptr;
    ProtocolMessageModel *presentation = nullptr;
    whatevr::proto::Subscription *sub = nullptr;

    // The daemon rejected this subscribe. The rows will never arrive, so the
    // session is not warm: a re-open has to build a fresh subscription rather
    // than take this one back and show its error again.
    bool failed = false;

    QString displayedChatId;
    QString requestedAnchor;
    QString effectiveAnchor;
    QString pendingJumpMessageId;
    QString jumpFallbackAnchor;
    QString pendingExtendDirection;
    QString errorText;
    QString unreadAnchorMessageId;
    QString pendingReadWatermark;
    QString lastReadWatermark;
    QString phoneHistoryOldestId;
    int unreadAnchorCount = 0;
    // Bumped by every (re)subscribe, so a reply to a superseded request for
    // this same session can be told apart from one for the request in flight.
    int generation = 0;
    int phoneHistoryGeneration = 0;
    bool unreadAnchorResolving = false;
    bool waitingInitialMessages = false;
    bool reloading = false;
    bool refillingAfterReset = false;
    bool olderLoading = false;
    bool newerLoading = false;
    bool canLoadOlder = false;
    bool canLoadNewer = false;
    bool olderFailed = false;
    bool newerFailed = false;
    bool atLiveEdge = false;
    bool phoneHistoryRequesting = false;

    [[nodiscard]] QAbstractItemModel *messageModel() const;
    [[nodiscard]] bool messagesLoading() const;
    [[nodiscard]] bool messagesEmpty() const;
    [[nodiscard]] bool canLoadOlderMessages() const { return canLoadOlder && !olderFailed; }
    [[nodiscard]] bool canLoadNewerMessages() const { return canLoadNewer && !newerFailed; }

    // The two notifications QML binds to. The controller emits them after a
    // handler has finished writing, not per field.
    void notifyChanged() { Q_EMIT changed(); }
    void notifyUnreadAnchorChanged() { Q_EMIT unreadAnchorChanged(); }

Q_SIGNALS:
    void changed();
    void unreadAnchorChanged();
};
