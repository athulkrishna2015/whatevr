pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr
import "Initials.js" as Initials
import "MediaFormat.js" as MediaFormat

// Calls tab: what is ringing right now, with Reject per call and the honest
// caveat that answering happens on the phone, then the recent-call log under
// it. Ringing rides the list header rather than a second list because it is
// live state rather than history, and a Kirigami.ScrollablePage scrolls
// exactly one flickable. Missed calls still land in their chats as tombstone
// messages (badge + preview); the section below is the cross-chat list of
// every call the daemon logged.
Kirigami.ScrollablePage {
    id: root

    title: Whatevr.I18n.i18nc("@title", "Calls")
    Kirigami.Theme.colorSet: Kirigami.Theme.View

    Component.onCompleted: {
        Whatevr.ProtocolController.openCalls()
        Whatevr.ProtocolController.openCallHistory()
    }
    // Guarded: at engine teardown the singleton may already be null.
    Component.onDestruction: {
        const c = Whatevr.ProtocolController
        if (c) {
            c.closeCalls()
            c.closeCallHistory()
        }
    }

    function callerLabel(item) {
        const caller = item.caller || {}
        if (caller.name && caller.name.length > 0) {
            return caller.name
        }
        return caller.id || item.chat_id
    }

    function callKindLabel(item) {
        if (item.video) {
            return Whatevr.I18n.i18nc("@info call kind", "Incoming video call")
        }
        return Whatevr.I18n.i18nc("@info call kind", "Incoming voice call")
    }

    function openCallChat(chatId) {
        Whatevr.ProtocolController.selectChat(chatId)
        applicationWindow().showConversation(chatId)
    }

    /// Which side the call went, whether it was video, and how long it lasted.
    /// The same words and the same i18n context as the in-chat call pill, so
    /// the two renderings can never drift apart — composed here for the same
    /// reason it is composed there: the daemon's one-line copy is not
    /// translatable, and telling somebody they missed a call they placed
    /// themselves is an accusation in the wrong direction.
    function callSummary(log, isOutgoing) {
        const video = Boolean(log.video)
        const outcome = String(log.outcome || "")
        const duration = Number(log.duration_secs || 0)

        if (outcome === "missed" || outcome === "silenced") {
            if (isOutgoing) {
                return video
                    ? Whatevr.I18n.i18nc("@label call log", "Unanswered video call")
                    : Whatevr.I18n.i18nc("@label call log", "Unanswered voice call")
            }
            return video
                ? Whatevr.I18n.i18nc("@label call log", "Missed video call")
                : Whatevr.I18n.i18nc("@label call log", "Missed voice call")
        }
        if (outcome === "rejected") {
            return video
                ? Whatevr.I18n.i18nc("@label call log", "Declined video call")
                : Whatevr.I18n.i18nc("@label call log", "Declined voice call")
        }
        if (outcome === "failed") {
            return Whatevr.I18n.i18nc("@label call log", "Call failed")
        }
        if (outcome === "ongoing") {
            return Whatevr.I18n.i18nc("@label call log", "Call in progress")
        }
        if (outcome === "accepted_elsewhere") {
            return Whatevr.I18n.i18nc("@label call log", "Answered on another device")
        }

        const medium = log.group
            ? (video
                ? Whatevr.I18n.i18nc("@label call log", "Group video call")
                : Whatevr.I18n.i18nc("@label call log", "Group voice call"))
            : (video
                ? Whatevr.I18n.i18nc("@label call log", "Video call")
                : Whatevr.I18n.i18nc("@label call log", "Voice call"))
        if (duration > 0) {
            return Whatevr.I18n.i18nc("@label call log, %1 is a call description and %2 its length",
                                      "%1 · %2", medium, MediaFormat.clockTime(duration))
        }
        return medium
    }

    /// The arrow says which way the call went, which is the fact a glance is
    /// after. A missed one gets its own glyph rather than a coloured incoming
    /// arrow, because that distinction has to survive being read in a hurry.
    function callGlyph(log, isOutgoing) {
        const outcome = String(log.outcome || "")
        if (outcome === "missed" || outcome === "silenced") {
            return "call-missed-symbolic"
        }
        if (outcome === "rejected" || outcome === "failed") {
            return "call-stop-symbolic"
        }
        return isOutgoing ? "call-outgoing-symbolic" : "call-incoming-symbolic"
    }

    function callAccent(log) {
        const outcome = String(log.outcome || "")
        if (outcome === "missed" || outcome === "silenced" || outcome === "failed") {
            return Kirigami.Theme.negativeTextColor
        }
        if (outcome === "rejected") {
            return Kirigami.Theme.neutralTextColor
        }
        return Kirigami.Theme.disabledTextColor
    }

    ListView {
        id: callsList

        model: Whatevr.ProtocolController.callHistoryModel
        currentIndex: -1
        reuseItems: true

        // The window grows as the user reaches its end (PROTOCOL.md "Windows":
        // a live-edge window only extends `older`).
        onAtYEndChanged: if (atYEnd) {
            Whatevr.ProtocolController.loadMoreCallHistory()
        }

        // On teardown the delegates are still bound to the shared model; detach
        // first so there are no live delegates to cancel.
        Component.onDestruction: callsList.model = null

        header: Component {
            ColumnLayout {
                id: ringingSection

                width: ListView.view ? ListView.view.width : 0
                spacing: 0

                Repeater {
                    id: ringingRepeater

                    model: Whatevr.ProtocolController.callsModel
                    Component.onDestruction: ringingRepeater.model = null

                    delegate: QQC2.ItemDelegate {
                        id: callDelegate

                        required property var item

                        Layout.fillWidth: true
                        Layout.preferredHeight: implicitHeight

                        contentItem: RowLayout {
                            spacing: Kirigami.Units.largeSpacing

                            AvatarImage {
                                Layout.preferredWidth: Kirigami.Units.gridUnit * 2
                                Layout.preferredHeight: Kirigami.Units.gridUnit * 2
                                initials: {
                                    const name = callDelegate.item.caller ? (callDelegate.item.caller.name || "") : ""
                                    return Initials.firstTwo(name)
                                }
                                backgroundColor: Qt.alpha(Kirigami.Theme.highlightColor, 0.18)
                            }

                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: Kirigami.Units.smallSpacing / 2

                                QQC2.Label {
                                    Layout.fillWidth: true
                                    text: root.callerLabel(callDelegate.item)
                                    font.weight: Font.DemiBold
                                    elide: Text.ElideRight
                                }

                                QQC2.Label {
                                    Layout.fillWidth: true
                                    text: root.callKindLabel(callDelegate.item)
                                    color: Kirigami.Theme.disabledTextColor
                                    elide: Text.ElideRight
                                }
                            }

                            QQC2.Button {
                                text: Whatevr.I18n.i18nc("@action:button reject the call", "Reject")
                                icon.name: "call-stop-symbolic"
                                onClicked: Whatevr.ProtocolController.rejectCall(callDelegate.item.chat_id)
                            }
                        }

                        QQC2.Menu {
                            id: callContextMenu

                            QQC2.MenuItem {
                                text: Whatevr.I18n.i18nc("@action:menu reject call", "Reject call")
                                icon.name: "call-stop-symbolic"
                                onTriggered: Whatevr.ProtocolController.rejectCall(callDelegate.item.chat_id)
                            }
                        }

                        TapHandler {
                            acceptedButtons: Qt.RightButton
                            onTapped: callContextMenu.popup()
                        }
                    }
                }

                // Collapses to nothing when there is no history, so an empty
                // calls tab still reaches its placeholder unobstructed.
                Kirigami.ListSectionHeader {
                    Layout.fillWidth: true
                    visible: callsList.count > 0
                    text: Whatevr.I18n.i18nc("@title:section recent calls", "Recent")
                }
            }
        }

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.callHistoryLoading && callsList.count === 0
            visible: running
        }

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: callsList.count === 0
                     && Whatevr.ProtocolController.callsRingingCount === 0
                     && !Whatevr.ProtocolController.callHistoryLoading
            icon.name: "call-start-symbolic"
            text: Whatevr.I18n.i18nc("@info placeholder for the calls list", "No calls yet")
            explanation: Whatevr.I18n.i18nc("@info:placeholder", "Incoming calls ring here with a Reject button. Answer on your phone — the desktop cannot pick up. Calls you have made or missed are listed here and in their chats.")
        }

        delegate: QQC2.ItemDelegate {
            id: historyDelegate

            // The whole daemon row, plus the display strings derived from it
            // (chat name, local time, direction).
            required property var item
            readonly property var row: Whatevr.ProtocolController.messageRowDisplay(item)
            readonly property var log: item.call_log ?? ({})

            width: ListView.view.width

            contentItem: RowLayout {
                spacing: Kirigami.Units.largeSpacing

                AvatarImage {
                    Layout.preferredWidth: Kirigami.Units.gridUnit * 2
                    Layout.preferredHeight: Kirigami.Units.gridUnit * 2
                    initials: Initials.firstTwo(historyDelegate.row.chatName.length > 0
                                                ? historyDelegate.row.chatName
                                                : historyDelegate.row.senderName)
                    backgroundColor: Qt.alpha(Kirigami.Theme.highlightColor, 0.18)
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Kirigami.Units.smallSpacing / 2

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Kirigami.Units.smallSpacing

                        QQC2.Label {
                            Layout.fillWidth: true
                            text: historyDelegate.row.chatName.length > 0
                                  ? historyDelegate.row.chatName
                                  : historyDelegate.row.senderName
                            font.weight: Font.DemiBold
                            elide: Text.ElideRight
                        }

                        QQC2.Label {
                            text: historyDelegate.row.timeText
                            color: Kirigami.Theme.disabledTextColor
                            font: Kirigami.Theme.smallFont
                        }
                    }

                    RowLayout {
                        Layout.fillWidth: true
                        spacing: Kirigami.Units.smallSpacing

                        Kirigami.Icon {
                            implicitWidth: Kirigami.Units.iconSizes.small
                            implicitHeight: Kirigami.Units.iconSizes.small
                            source: root.callGlyph(historyDelegate.log, historyDelegate.row.isOutgoing)
                            color: root.callAccent(historyDelegate.log)
                            isMask: true
                        }

                        Kirigami.Icon {
                            implicitWidth: Kirigami.Units.iconSizes.small
                            implicitHeight: Kirigami.Units.iconSizes.small
                            source: historyDelegate.log.video
                                    ? "camera-video-symbolic"
                                    : "audio-input-microphone-symbolic"
                            color: Kirigami.Theme.disabledTextColor
                            isMask: true
                        }

                        QQC2.Label {
                            Layout.fillWidth: true
                            text: root.callSummary(historyDelegate.log, historyDelegate.row.isOutgoing)
                            color: root.callAccent(historyDelegate.log)
                            elide: Text.ElideRight
                        }
                    }
                }
            }

            onClicked: root.openCallChat(historyDelegate.row.chatId)
        }
    }
}
