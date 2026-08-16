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

    // ---- compact: the stack's one-line-per-person form ----

    RowLayout {
        Layout.fillWidth: true
        Layout.topMargin: Kirigami.Units.smallSpacing / 2
        visible: root.compact
        spacing: Kirigami.Units.smallSpacing

        Kirigami.Icon {
            implicitWidth: Kirigami.Units.iconSizes.small
            implicitHeight: Kirigami.Units.iconSizes.small
            source: "user-symbolic"
            fallback: "user"
            color: Kirigami.Theme.disabledTextColor
        }

        Controls.Label {
            Layout.fillWidth: true
            text: String(root.card.display_name ?? "")
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
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

        // The name repeats here only when this entry is one of several: a
        // single card already has it in the bubble's header.
        Controls.Label {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing
            visible: root.expanded && text.length > 0 && root.card.display_name !== undefined
            text: String(root.card.display_name ?? "")
            elide: Text.ElideRight
            font.bold: true
            font.pointSize: Kirigami.Theme.smallFont.pointSize
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

        Controls.Label {
            Layout.fillWidth: true
            visible: root.expanded && text.length > 0
            text: {
                const birthday = String(root.card.birthday ?? "")
                return birthday.length > 0
                    ? Whatevr.I18n.i18nc("@label a contact's birthday", "Birthday: %1", birthday)
                    : ""
            }
            color: Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // The primary action. It only exists when WhatsApp vouched for a number
        // on this card, because without that a "Message" button would be a
        // guess that fails after a round trip.
        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            visible: !root.selectionModeActive
            spacing: Kirigami.Units.smallSpacing

            Controls.ToolButton {
                visible: root.primaryPhone && String(root.primaryPhone.jid ?? "").length > 0
                text: Whatevr.I18n.i18nc("@action open a chat with a shared contact", "Message")
                icon.name: "mail-message-new-symbolic"
                display: Controls.AbstractButton.TextBesideIcon
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                onClicked: {
                    if (root.primaryPhone)
                        Whatevr.ProtocolController.startDirectChat(String(root.primaryPhone.jid))
                }
            }

            Controls.ToolButton {
                visible: root.primaryPhone && String(root.primaryPhone.jid ?? "").length > 0
                text: Whatevr.I18n.i18nc("@action open a shared contact's info card", "Info")
                icon.name: "documentinfo-symbolic"
                display: Controls.AbstractButton.TextBesideIcon
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                onClicked: {
                    if (root.primaryPhone)
                        Whatevr.ProtocolController.openContactCard(String(root.primaryPhone.jid))
                }
            }

            Controls.ToolButton {
                visible: String(root.card.vcard ?? "").length > 0
                text: Whatevr.I18n.i18nc("@action save a shared contact to a file", "Save card")
                icon.name: "document-save-symbolic"
                display: Controls.AbstractButton.TextBesideIcon
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                onClicked: Whatevr.ProtocolController.saveContactCard(
                    String(root.card.display_name ?? ""), String(root.card.vcard ?? ""))
            }

            Item {
                Layout.fillWidth: true
            }
        }
    }
}
