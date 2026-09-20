// SPDX-License-Identifier: BSD-3-Clause
#include "conversationsession.h"

#include "protocolmessagemodel.h"
#include "collectionviewmodel.h"

ConversationSession::ConversationSession(QObject *parent)
    : QObject(parent)
{
}

ConversationSession::~ConversationSession() = default;

QAbstractItemModel *ConversationSession::messageModel() const
{
    return presentation;
}

bool ConversationSession::messagesLoading() const
{
    return !chatId.isEmpty() && (waitingInitialMessages || !source || !source->isReady());
}

bool ConversationSession::messagesEmpty() const
{
    return !source || source->count() == 0;
}
