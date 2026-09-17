pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Status tab: one row per contact with an unexpired status, newest ring first.
// The page owns the daemon `status` subscription while on screen and groups
// the flat status rows per contact itself (presentation-side, over rows it
// already holds). Tapping a contact opens its statuses in the viewer; the "+"
// action posts a text or photo status of your own.
Kirigami.ScrollablePage {
    id: root

    title: Whatevr.I18n.i18nc("@title", "Status")
    Kirigami.Theme.colorSet: Kirigami.Theme.View

    Component.onCompleted: {
        Whatevr.ProtocolController.openStatus()
        root.rebuildGroups()
    }
    Component.onDestruction: Whatevr.ProtocolController.closeStatus()

    // Contact groups rebuilt from the flat model: newest status first, so the
    // first time a sender appears is its recency rank. Each entry: {senderId,
    // senderName, latest, total, unviewed, statusIds, section, kept}. Recent
    // contacts (anything newer than 24h) sort under "Recent"; kept contacts
    // whose statuses all expired move to "Archived" instead of vanishing.
    property var contactGroups: []
    // Keep-enabled sender ids from the `status.kept` view, as a lookup map.
    property var keptSenders: ({})

    // WhatsApp statuses live 24 hours; older rows are archive material.
    readonly property int statusExpirySecs: 24 * 60 * 60

    function rebuildGroups() {
        const kept = {}
        const kmodel = Whatevr.ProtocolController.keptStatusModel
        const kcount = kmodel ? kmodel.count : 0
        for (let i = 0; i < kcount; ++i) {
            kept[kmodel.idAt(i)] = true
        }
        root.keptSenders = kept

        const model = Whatevr.ProtocolController.statusModel
        const groups = []
        const bySender = {}
        const count = model ? model.count : 0
        for (let i = 0; i < count; ++i) {
            const item = model.itemById(model.idAt(i))
            if (!item || !item.id) {
                continue
            }
            const sender = item.sender || {}
            const senderId = sender.id || ""
            if (!senderId) {
                continue
            }
            let group = bySender[senderId]
            if (!group) {
                group = {
                    "senderId": senderId,
                    "senderName": sender.name || senderId,
                    "avatarPath": sender.avatarPath || "",
                    "latest": 0,
                    "total": 0,
                    "unviewed": 0,
                    "statusIds": []
                }
                bySender[senderId] = group
                groups.push(group)
            }
            // A later row of the same sender may carry an avatar the first one
            // lacked; keep the freshest non-empty value.
            if (!group.avatarPath && sender.avatarPath) {
                group.avatarPath = sender.avatarPath
            }
            group.statusIds.push(item.id)
            group.total += 1
            if (!item.viewed) {
                group.unviewed += 1
            }
            if (item.timestamp > group.latest) {
                group.latest = item.timestamp
            }
        }
        const now = Math.floor(Date.now() / 1000)
        const recent = []
        const archived = []
        for (let i = 0; i < groups.length; ++i) {
            const group = groups[i]
            const expired = (now - group.latest) > root.statusExpirySecs
            group.kept = Boolean(kept[group.senderId])
            if (expired && !group.kept) {
                continue
            }
            if (expired) {
                group.section = Whatevr.I18n.i18nc("@title:section expired kept statuses", "Archived")
                archived.push(group)
            } else {
                group.section = Whatevr.I18n.i18nc("@title:section recent statuses", "Recent")
                recent.push(group)
            }
        }
        root.contactGroups = recent.concat(archived)
    }

    function contactLabel(group) {
        if (group.senderId === "me") {
            return Whatevr.I18n.i18nc("@item status contact", "My status")
        }
        return group.senderName
    }

    function initialsFor(name) {
        const parts = (name || "").trim().split(/\s+/)
        let initials = ""
        for (const part of parts) {
            if (part.length > 0) {
                initials += part[0].toUpperCase()
            }
            if (initials.length >= 2) {
                break
            }
        }
        return initials.length > 0 ? initials : "?"
    }

    Connections {
        target: Whatevr.ProtocolController

        function onStatusChanged() {
            root.rebuildGroups()
        }
    }

    // The status subscription delivers rows through CollectionViewModel after
    // openStatus() resolves; row churn only raises the model's own signals, so
    // a rebuild keyed on statusChanged alone left the page showing whatever
    // was there on the last explicit event (often nothing, right after open).
    Connections {
        target: Whatevr.ProtocolController.statusModel

        function onCountChanged() {
            root.rebuildGroups()
        }

        function onReadyChanged() {
            root.rebuildGroups()
        }
    }

    actions: [
        Kirigami.Action {
            icon.name: "list-add-symbolic"
            text: Whatevr.I18n.i18nc("@action:button post a status", "New status")
            onTriggered: postDialog.open()
        }
    ]

    ListView {
        id: statusList

        model: root.contactGroups
        currentIndex: -1
        reuseItems: true

        section.property: "section"
        section.delegate: QQC2.Label {
            required property string section

            text: section
            font.weight: Font.DemiBold
            color: Kirigami.Theme.disabledTextColor
            leftPadding: Kirigami.Units.largeSpacing
            topPadding: Kirigami.Units.largeSpacing
        }

        onAtYEndChanged: if (atYEnd) {
            Whatevr.ProtocolController.loadMoreStatus()
        }
        Component.onDestruction: statusList.model = null

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.statusLoading && statusList.count === 0
            visible: running
        }

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: statusList.count === 0 && !Whatevr.ProtocolController.statusLoading
            icon.name: "camera-photo-symbolic"
            text: Whatevr.I18n.i18nc("@info placeholder for the status list", "No recent statuses")
            explanation: Whatevr.I18n.i18nc("@info:placeholder", "Statuses from your contacts appear here for 24 hours.")
        }

        delegate: QQC2.ItemDelegate {
            id: statusDelegate

            required property var modelData
            readonly property var group: modelData

            width: ListView.view.width

            onClicked: {
                applicationWindow().pageStack.layers.push(Qt.resolvedUrl("StatusViewerPage.qml"), {
                    "senderId": statusDelegate.group.senderId,
                    "senderName": root.contactLabel(statusDelegate.group)
                })
            }

            contentItem: RowLayout {
                spacing: Kirigami.Units.largeSpacing

                // Ring: highlighted while any of the contact's statuses is
                // unviewed, plain once all are seen.
                Rectangle {
                    Layout.preferredWidth: Kirigami.Units.gridUnit * 2.4
                    Layout.preferredHeight: Kirigami.Units.gridUnit * 2.4
                    radius: width / 2
                    color: "transparent"
                    border.width: statusDelegate.group.unviewed > 0 ? Math.max(2, Kirigami.Units.smallSpacing / 2) : 1
                    border.color: statusDelegate.group.unviewed > 0
                        ? Kirigami.Theme.highlightColor
                        : Qt.alpha(Kirigami.Theme.textColor, 0.25)

                    AvatarImage {
                        anchors.fill: parent
                        anchors.margins: parent.border.width + 1
                        avatarLocalPath: statusDelegate.group.avatarPath
                        initials: root.initialsFor(statusDelegate.group.senderName)
                        backgroundColor: Qt.alpha(Kirigami.Theme.highlightColor, 0.18)
                    }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Kirigami.Units.smallSpacing / 2

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: root.contactLabel(statusDelegate.group)
                        elide: Text.ElideRight
                        font.weight: statusDelegate.group.unviewed > 0 ? Font.DemiBold : Font.Normal
                    }

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: statusDelegate.group.unviewed > 0
                            ? Whatevr.I18n.i18nc("@info status count", "%1 new", statusDelegate.group.unviewed)
                            : Whatevr.I18n.i18nc("@info status count", "%1 total", statusDelegate.group.total)
                        color: Kirigami.Theme.disabledTextColor
                        elide: Text.ElideRight
                    }
                }

                // Per-contact keep: expired statuses of kept contacts collect
                // under Archived instead of vanishing after 24 hours.
                QQC2.ToolButton {
                    icon.name: statusDelegate.group.kept ? "bookmark-symbolic" : "bookmark-new-symbolic"
                    text: Whatevr.I18n.i18nc("@action:button keep a contact's expired statuses", "Keep")
                    display: QQC2.AbstractButton.IconOnly
                    checkable: true
                    checked: statusDelegate.group.kept
                    onToggled: Whatevr.ProtocolController.setStatusKeepSender(statusDelegate.group.senderId, checked)
                }
            }
        }
    }

    StatusPostDialog {
        id: postDialog
    }
}
