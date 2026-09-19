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
    }

    function openConversation() { root.workspaceIndex = 0 }
    function openTab(name) {
        const indexes = {status: 1, calls: 2, channels: 3, logs: 4, starred: 5}
        if (indexes[name] !== undefined)
            root.workspaceIndex = indexes[name]
    }
}
