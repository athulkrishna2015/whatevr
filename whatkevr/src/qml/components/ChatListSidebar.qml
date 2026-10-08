pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami

import Whatevr as Whatevr
import "Initials.js" as Initials

// Slim vertical rail on the left of the chat list. Top group switches the chat
// filter (Home / DMs / Groups) and opens the starred-messages view; the bottom
// pins Settings and the logged-in user's avatar (→ Settings → Account).
Item {
    id: root

    // 0 = Home, 1 = DMs, 2 = Groups, 3 = Unread, 4 = Favorites.
    property int activeFilter: 0
    property int activeFolder: 0
    readonly property string activeWorkspace: {
        const left = applicationWindow()?.chatListPageItem?.workspaceMode ?? "chats"
        if (left === "status" || left === "channels")
            return left
        return applicationWindow()?.workspacePageItem?.workspaceName ?? "conversation"
    }

    readonly property string userName: Whatevr.ProtocolController.currentUserName

    // Shared icon size for the rail buttons — bigger than the default small
    // toolbutton glyph so they don't look lost in the bar.
    readonly property int railIconSize: Kirigami.Units.iconSizes.smallMedium

    implicitWidth: Kirigami.Units.gridUnit * 2.5
    Layout.preferredWidth: implicitWidth
    Layout.fillHeight: true

    Kirigami.Theme.inherit: false
    Kirigami.Theme.colorSet: Kirigami.Theme.Window

    Rectangle {
        anchors.fill: parent
        color: Kirigami.Theme.backgroundColor

        // Subtle separator from the chat list on the right.
        Rectangle {
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            anchors.right: parent.right
            width: 1
            color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.topMargin: Kirigami.Units.smallSpacing
        anchors.bottomMargin: Kirigami.Units.largeSpacing
        spacing: Kirigami.Units.smallSpacing

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "go-home-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button chat filter", "Home")
            checkable: true
            checked: root.activeFilter === 0
            onClicked: {
                root.activeFilter = 0
                root.activeFolder = 0
                applicationWindow().openConversation()
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }


        Repeater {
            model: Whatevr.ProtocolController.chatFoldersModel
            delegate: QQC2.ToolButton {
                id: folderDelegate

                required property var item
                Layout.alignment: Qt.AlignHCenter
                icon.name: "folder-symbolic"
                display: QQC2.AbstractButton.IconOnly
                icon.width: root.railIconSize
                icon.height: root.railIconSize
                text: item.name
                checkable: true
                checked: root.activeFolder === Number(item.id)
                onClicked: {
                    root.activeFolder = Number(item.id)
                    root.activeFilter = 0
                }
                QQC2.ToolTip.visible: hovered
                QQC2.ToolTip.text: text
                QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay

                QQC2.Menu {
                    id: folderContextMenu

                    QQC2.MenuItem {
                        text: Whatevr.I18n.i18nc("@action:menu rename a chat list", "Rename…")
                        icon.name: "document-edit-symbolic"
                        onTriggered: folderNameDialog.openFor(Number(folderDelegate.item.id),
                                                              String(folderDelegate.item.name || ""))
                    }
                    QQC2.MenuItem {
                        text: Whatevr.I18n.i18nc("@action:menu delete a chat list", "Delete…")
                        icon.name: "edit-delete-symbolic"
                        onTriggered: deleteFolderDialog.openFor(Number(folderDelegate.item.id),
                                                                String(folderDelegate.item.name || ""))
                    }
                }

                TapHandler {
                    acceptedButtons: Qt.RightButton
                    onTapped: folderContextMenu.popup()
                }
            }
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "list-add-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button create a chat list", "New list")
            onClicked: folderNameDialog.openFor(0, "")

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "user-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button chat filter", "Direct messages")
            checkable: true
            checked: root.activeFilter === 1
            onClicked: { root.activeFilter = 1; root.activeFolder = 0; applicationWindow().openConversation() }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "system-users-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button chat filter", "Groups")
            checkable: true
            checked: root.activeFilter === 2
            onClicked: { root.activeFilter = 2; root.activeFolder = 0; applicationWindow().openConversation() }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "mail-unread-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button chat filter", "Unread")
            checkable: true
            checked: root.activeFilter === 3
            onClicked: { root.activeFilter = 3; root.activeFolder = 0; applicationWindow().openConversation() }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "favorite"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button chat filter", "Favorites")
            checkable: true
            checked: root.activeFilter === 4
            onClicked: { root.activeFilter = 4; root.activeFolder = 0; applicationWindow().openConversation() }
            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "starred-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button open the starred-messages view", "Starred messages")
            checkable: true
            checked: root.activeWorkspace === "starred"
            onClicked: {
                // The page owns its own `starred` subscription for as long as
                // it is on screen; pushing it is all this has to do.
                applicationWindow().openWorkspace("starred")
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "camera-photo-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button open the status tab", "Status")
            checkable: true
            checked: root.activeWorkspace === "status" || root.activeWorkspace === "status-viewer"
            onClicked: {
                // Like starred: the page owns its `status` subscription while
                // on screen, grouped per contact inside the page itself.
                applicationWindow().openWorkspace("status")
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            id: callsButton

            Layout.alignment: Qt.AlignHCenter
            icon.name: "call-start-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.ProtocolController.callsRingingCount > 0
                ? Whatevr.I18n.i18nc("@action:button open the calls tab", "Calls (%1 ringing)", Whatevr.ProtocolController.callsRingingCount)
                : Whatevr.I18n.i18nc("@action:button open the calls tab", "Calls")
            checkable: true
            checked: root.activeWorkspace === "calls"
            onClicked: {
                // The page owns its `calls` subscription while on screen.
                applicationWindow().openWorkspace("calls")
            }

            // Ringing dot while a call is coming in.
            Rectangle {
                anchors.right: parent.right
                anchors.rightMargin: Kirigami.Units.smallSpacing / 2
                anchors.top: parent.top
                anchors.topMargin: Kirigami.Units.smallSpacing / 2
                width: Kirigami.Units.smallSpacing
                height: Kirigami.Units.smallSpacing
                radius: width / 2
                color: Kirigami.Theme.positiveTextColor
                visible: Whatevr.ProtocolController.callsRingingCount > 0
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "rss-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button open the channels tab", "Channels")
            checkable: true
            checked: root.activeWorkspace === "channels" || root.activeWorkspace === "channel-messages"
            onClicked: {
                applicationWindow().openWorkspace("channels")
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        Item {
            Layout.fillHeight: true
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "settings-configure-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button", "Settings")
            onClicked: applicationWindow().openSettings()

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "application-exit-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button quit Whatevr", "Quit Whatevr")
            onClicked: applicationWindow().quitApplication()

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.ToolButton {
            Layout.alignment: Qt.AlignHCenter
            icon.name: "document-properties-symbolic"
            display: QQC2.AbstractButton.IconOnly
            icon.width: root.railIconSize
            icon.height: root.railIconSize
            text: Whatevr.I18n.i18nc("@action:button open the daemon logs view", "Logs")
            checkable: true
            checked: root.activeWorkspace === "logs"
            onClicked: {
                applicationWindow().openWorkspace("logs")
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: text
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }

        QQC2.AbstractButton {
            Layout.alignment: Qt.AlignHCenter
            Layout.topMargin: Kirigami.Units.smallSpacing
            implicitWidth: Kirigami.Units.gridUnit * 1.9
            implicitHeight: implicitWidth
            hoverEnabled: true

            Accessible.role: Accessible.Button
            Accessible.name: Whatevr.I18n.i18nc("@action:button", "Open account settings")

            onClicked: applicationWindow().openSettings("account")

            contentItem: AvatarImage {
                avatarLocalPath: Whatevr.ProtocolController.currentUserAvatarPath
                initials: Initials.firstLast(root.userName)
            }

            QQC2.ToolTip.visible: hovered
            QQC2.ToolTip.text: root.userName.length > 0
                ? root.userName
                : Whatevr.I18n.i18nc("@info placeholder for own profile name", "You")
            QQC2.ToolTip.delay: Kirigami.Units.toolTipDelay
        }
    }

    Kirigami.PromptDialog {
        id: folderNameDialog

        // Centre on the stable implicitHeight: Kirigami.Dialog's own y binding
        // reads the fitted height back and loops.
        y: parent ? Math.round((parent.height - implicitHeight) / 2) : 0

        // 0 = creating a new list, otherwise the list being renamed.
        property int folderId: 0

        function openFor(id, name) {
            folderId = id
            nameInput.text = name
            open()
            nameInput.forceActiveFocus()
            nameInput.selectAll()
        }

        title: folderId > 0
               ? Whatevr.I18n.i18nc("@title:dialog", "Rename list")
               : Whatevr.I18n.i18nc("@title:dialog", "New list")
        standardButtons: Kirigami.Dialog.Ok | Kirigami.Dialog.Cancel
        showCloseButton: false

        QQC2.TextField {
            id: nameInput

            placeholderText: Whatevr.I18n.i18nc("@info:placeholder", "List name")
            onAccepted: folderNameDialog.accept()
        }

        onAccepted: {
            const name = nameInput.text.trim()
            if (name.length > 0) {
                if (folderId > 0) {
                    Whatevr.ProtocolController.renameChatFolder(folderId, name)
                } else {
                    Whatevr.ProtocolController.createChatFolder(name)
                }
            }
            nameInput.clear()
        }
        onRejected: nameInput.clear()
    }

    Kirigami.PromptDialog {
        id: deleteFolderDialog

        y: parent ? Math.round((parent.height - implicitHeight) / 2) : 0

        property int folderId: 0
        property string folderName: ""

        function openFor(id, name) {
            folderId = id
            folderName = name
            open()
        }

        title: Whatevr.I18n.i18nc("@title:dialog", "Delete list “%1”?", folderName)
        subtitle: Whatevr.I18n.i18nc("@info", "Its chats are not deleted — they stay in your chat list.")
        standardButtons: Kirigami.Dialog.Cancel
        showCloseButton: false

        customFooterActions: [
            Kirigami.Action {
                text: Whatevr.I18n.i18nc("@action:button", "Delete list")
                icon.name: "edit-delete-symbolic"
                onTriggered: {
                    // The list filter would otherwise keep asking the daemon
                    // for a folder that no longer exists.
                    if (root.activeFolder === deleteFolderDialog.folderId)
                        root.activeFolder = 0
                    Whatevr.ProtocolController.deleteChatFolder(deleteFolderDialog.folderId)
                    deleteFolderDialog.close()
                }
            }
        ]
    }
}
