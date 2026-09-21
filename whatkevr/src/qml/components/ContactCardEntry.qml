// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * One person inside a shared contact card.
 *
 * Compact, it is a name and a number, which is all a stack of contacts has room
 * for. Expanded, every field the vCard carried gets a row, and every row does
 * the thing that field is for: a number that WhatsApp vouched for opens a chat,
 * one it did not offers `tel:`, an email opens the composer, an address opens
 * the map, a URL opens the browser. Everything can be copied.
 */
ColumnLayout {
    id: root

    required property var card
    /// One line only, for a stack that has not been expanded.
    property bool compact: false
    property bool expanded: false
    property bool selectionModeActive: false
    /// Drawn above this entry when it follows another one. Several contacts
    /// otherwise read as one unbroken column of fields, with nothing saying
    /// where one person stops and the next starts.
    property bool showSeparator: false
    /// Whether this entry names the person it belongs to. Off for a lone
    /// contact, whose name is already the card's own heading: repeating it
    /// under itself says nothing and reads as a duplicate.
    property bool showPersonHeader: false

    spacing: 0

    readonly property var phones: card.phones ?? []
    readonly property var emails: card.emails ?? []
    readonly property var urls: card.urls ?? []
    readonly property var addresses: card.addresses ?? []

    /** The number the card leads with: one WhatsApp vouched for, else the first. */
    readonly property var primaryPhone: {
        for (let i = 0; i < phones.length; ++i) {
            if (String(phones[i].jid ?? "").length > 0)
                return phones[i]
        }
        return phones.length > 0 ? phones[0] : null
    }

    readonly property string reachableJID: primaryPhone ? String(primaryPhone.jid ?? "") : ""

    function initialsFor(name) {
        const trimmed = String(name ?? "").trim()
        if (trimmed.length === 0)
            return "?"
        const words = trimmed.split(/\s+/)
        return words.length === 1
            ? words[0].charAt(0).toUpperCase()
            : (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase()
    }

    Item {
        Layout.fillWidth: true
        visible: root.showSeparator
        implicitHeight: Kirigami.Units.largeSpacing

        Rectangle {
            anchors.verticalCenter: parent.verticalCenter
            anchors.left: parent.left
            anchors.right: parent.right
            height: 1
            color: Qt.alpha(Kirigami.Theme.textColor, 0.15)
        }
    }

    // ---- compact: the stack's one-line-per-person form ----

    RowLayout {
        Layout.fillWidth: true
        Layout.topMargin: Kirigami.Units.smallSpacing / 2
        Layout.bottomMargin: Kirigami.Units.smallSpacing / 2
        visible: root.compact
        spacing: Kirigami.Units.smallSpacing

        Kirigami.Icon {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.iconSizes.small
            implicitHeight: Kirigami.Units.iconSizes.small
            source: "user-symbolic"
            fallback: "user"
            color: Kirigami.Theme.disabledTextColor
        }

        Controls.Label {
            Layout.fillWidth: true
            Layout.alignment: Qt.AlignVCenter
            text: String(root.card.display_name ?? "")
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.alignment: Qt.AlignVCenter
            visible: text.length > 0
            text: root.primaryPhone ? String(root.primaryPhone.value ?? "") : ""
            color: Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }

    // ---- expanded: every field, each actionable ----

    ColumnLayout {
        Layout.fillWidth: true
        visible: !root.compact
        spacing: 0

        // Whose fields these are. Only when the card holds several people:
        // see showPersonHeader.
        RowLayout {
            Layout.fillWidth: true
            Layout.bottomMargin: Kirigami.Units.smallSpacing / 2
            visible: root.showPersonHeader
            spacing: Kirigami.Units.smallSpacing

            AvatarImage {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                // A shared card carries no picture, and looking one up would
                // tell the server whose contacts got forwarded to us.
                initials: root.initialsFor(String(root.card.display_name ?? ""))
            }

            Controls.Label {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignVCenter
                text: String(root.card.display_name ?? "")
                elide: Text.ElideRight
                maximumLineCount: 1
                font.bold: true
            }
        }

        Repeater {
            model: root.expanded ? root.phones : (root.primaryPhone ? [root.primaryPhone] : [])

            delegate: ContactFieldRow {
                required property var modelData

                Layout.fillWidth: true
                iconName: "phone-symbolic"
                label: String(modelData.label ?? "")
                value: String(modelData.value ?? "")
                jid: String(modelData.jid ?? "")
                selectionModeActive: root.selectionModeActive
                actionKind: "phone"
            }
        }

        Repeater {
            model: root.expanded ? root.emails : []

            delegate: ContactFieldRow {
                required property var modelData

                Layout.fillWidth: true
                iconName: "mail-message-symbolic"
                label: String(modelData.label ?? "")
                value: String(modelData.value ?? "")
                selectionModeActive: root.selectionModeActive
                actionKind: "email"
            }
        }

        Repeater {
            model: root.expanded ? root.addresses : []

            delegate: ContactFieldRow {
                required property var modelData

                Layout.fillWidth: true
                iconName: "mark-location-symbolic"
                label: String(modelData.label ?? "")
                value: String(modelData.value ?? "")
                selectionModeActive: root.selectionModeActive
                actionKind: "address"
            }
        }

        Repeater {
            model: root.expanded ? root.urls : []

            delegate: ContactFieldRow {
                required property var modelData

                Layout.fillWidth: true
                iconName: "link-symbolic"
                label: String(modelData.label ?? "")
                value: String(modelData.value ?? "")
                selectionModeActive: root.selectionModeActive
                actionKind: "url"
            }
        }

        ContactFieldRow {
            Layout.fillWidth: true
            visible: root.expanded && value.length > 0
            iconName: "view-calendar-birthday-symbolic"
            value: String(root.card.birthday ?? "")
            label: Whatevr.I18n.i18nc("@label a contact's birthday", "Birthday")
            selectionModeActive: true
        }

        // The actions. They exist only when WhatsApp vouched for a number on
        // this card, because without that a Message button would be a guess
        // that fails after a round trip.
        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            // Pulled left by a button's own padding so the glyphs land on the
            // same column as the field icons above them.
            Layout.leftMargin: -Kirigami.Units.smallSpacing
            visible: !root.selectionModeActive
            spacing: 0

            CardActionButton {
                visible: root.reachableJID.length > 0
                text: Whatevr.I18n.i18nc("@action open a chat with a shared contact", "Message")
                iconName: "mail-message-new-symbolic"
                onClicked: Whatevr.ProtocolController.startDirectChat(root.reachableJID)
            }

            CardActionButton {
                visible: root.reachableJID.length > 0
                text: Whatevr.I18n.i18nc("@action open a shared contact's info card", "Info")
                iconName: "documentinfo-symbolic"
                onClicked: Whatevr.ProtocolController.openContactCard(root.reachableJID)
            }

            CardActionButton {
                visible: String(root.card.vcard ?? "").length > 0
                text: Whatevr.I18n.i18nc("@action save a shared contact to a file", "Save card")
                iconName: "document-save-symbolic"
                onClicked: Whatevr.ProtocolController.saveContactCard(
                    String(root.card.display_name ?? ""), String(root.card.vcard ?? ""))
            }

            Item {
                Layout.fillWidth: true
            }
        }
    }
}
