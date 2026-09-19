pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

Kirigami.Page {
    id: root

    signal closeChatRequested()
    readonly property Item conversationPane: conversation
    property int workspaceIndex: 0
    property string workspaceName: "conversation"
    property string statusSenderId: ""
    property string statusSenderName: ""
    property string channelId: ""
    property string channelName: ""

    padding: 0
    title: stack.currentIndex === 0
        ? (Whatevr.ProtocolController.hasSelectedChat ? Whatevr.ProtocolController.selectedChatName : "")
        : (stack.currentItem ? stack.currentItem.title : "")

    StackLayout {
        id: stack
        anchors.fill: parent
        currentIndex: root.workspaceIndex

        ConversationPane {
            id: conversation
            onCloseChatRequested: root.closeChatRequested()
        }
        StatusPage {}
        CallsPage {}
        ChannelsPage {}
        LogsPage {}
        StarredMessagesPage {
            chatId: ""
            headerTitle: Whatevr.I18n.i18nc("@title", "Starred messages")
        }
        StatusViewerPage {
            senderId: root.statusSenderId
            senderName: root.statusSenderName
        }
        ChannelMessagesPage {
            channelJid: root.channelId
            channelName: root.channelName
        }
    }

    function openConversation() {
        root.workspaceIndex = 0
        root.workspaceName = "conversation"
    }
    function openTab(name) {
        const indexes = {status: 1, calls: 2, channels: 3, logs: 4, starred: 5}
        if (indexes[name] !== undefined) {
            root.workspaceIndex = indexes[name]
            root.workspaceName = name
        }
    }

    function openStatusViewer(senderId, senderName) {
        root.statusSenderId = senderId
        root.statusSenderName = senderName
        root.workspaceIndex = 6
        root.workspaceName = "status-viewer"
    }

    function openChannelMessages(channelId, channelName) {
        root.channelId = channelId
        root.channelName = channelName
        root.workspaceIndex = 7
        root.workspaceName = "channel-messages"
    }
}
