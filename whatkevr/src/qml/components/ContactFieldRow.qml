// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * One labelled field on a shared contact card, with the action that field is
 * for. This is the part a phone cannot do well and a desktop can: an address
 * goes to the map application, an email to the mail composer, a number to
 * whatever holds `tel:`, and any of them to the clipboard.
 */
RowLayout {
    id: root

    required property string iconName
    required property string value
    property string label: ""
    /// Set when WhatsApp vouched for this number, which makes it a chat rather
    /// than a dial.
    property string jid: ""
    /// One of "phone", "email", "address", "url".
    property string actionKind: "phone"
    property bool selectionModeActive: false

    spacing: Kirigami.Units.smallSpacing

    readonly property bool onWhatsApp: jid.length > 0

    function activate() {
        if (value.length === 0)
            return
        switch (actionKind) {
        case "phone":
            if (onWhatsApp) {
                Whatevr.ProtocolController.startDirectChat(jid)
                return
            }
            Qt.openUrlExternally("tel:" + value.replace(/[^\d+]/g, ""))
            return
        case "email":
            Qt.openUrlExternally("mailto:" + value)
            return
        case "address":
            Qt.openUrlExternally("geo:0,0?q=" + encodeURIComponent(value))
            return
        case "url":
            Qt.openUrlExternally(value.match(/^[a-z][a-z0-9+.-]*:/i) ? value : "https://" + value)
            return
        }
    }

    Kirigami.Icon {
        Layout.alignment: Qt.AlignTop
        Layout.topMargin: Kirigami.Units.smallSpacing / 2
        implicitWidth: Kirigami.Units.iconSizes.small
        implicitHeight: Kirigami.Units.iconSizes.small
        source: root.iconName
        fallback: "dialog-information-symbolic"
        color: Kirigami.Theme.disabledTextColor
    }

    ColumnLayout {
        Layout.fillWidth: true
        spacing: 0

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            Controls.Label {
                Layout.fillWidth: true
                text: root.value
                elide: Text.ElideRight
                maximumLineCount: 2
                wrapMode: Text.Wrap
                color: root.selectionModeActive ? Kirigami.Theme.textColor : Kirigami.Theme.linkColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }

            // The badge is free: WhatsApp told us, in the vCard itself, which
            // numbers are reachable.
            Rectangle {
                visible: root.onWhatsApp
                implicitWidth: badgeLabel.implicitWidth + Kirigami.Units.smallSpacing
                implicitHeight: badgeLabel.implicitHeight + Kirigami.Units.smallSpacing / 2
                radius: height / 2
                color: Qt.alpha(Whatevr.Palette.highlight, 0.22)

                Controls.Label {
                    id: badgeLabel

                    anchors.centerIn: parent
                    text: Whatevr.I18n.i18nc("@label the number is reachable on WhatsApp", "on WhatsApp")
                    font.pointSize: Kirigami.Theme.smallFont.pointSize * 0.9
                }
            }

            Controls.ToolButton {
                visible: !root.selectionModeActive
                icon.name: "edit-copy-symbolic"
                display: Controls.AbstractButton.IconOnly
                Accessible.name: Whatevr.I18n.i18nc("@action", "Copy")
                onClicked: Whatevr.ProtocolController.copyToClipboard(root.value)
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.label.length > 0
            text: root.label
            color: Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize * 0.9
        }
    }

    TapHandler {
        enabled: !root.selectionModeActive
        exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
        onSingleTapped: root.activate()
    }

    HoverHandler {
        enabled: !root.selectionModeActive
        cursorShape: Qt.PointingHandCursor
    }
}
