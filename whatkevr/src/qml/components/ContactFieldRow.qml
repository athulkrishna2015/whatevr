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
 *
 * The layout is a two-line grid rather than a single row. Line one carries the
 * glyph, the value, the badge and the copy button, all centred on each other,
 * so the glyph sits on the value's own line instead of floating between it and
 * the caption. Line two is the caption, indented to the value's left edge so
 * every field in the card shares one text column.
 */
Item {
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

    /// Distance from the field's left edge to its text column. Every caption
    /// and every value in the card lines up on this.
    readonly property real textColumnX: Kirigami.Units.iconSizes.small + Kirigami.Units.smallSpacing

    // An Item rather than a Layout, because the hover plate has to sit behind
    // the whole field and a Layout may not manage an anchored child.
    implicitWidth: layout.implicitWidth
    implicitHeight: layout.implicitHeight

    readonly property bool onWhatsApp: jid.length > 0
    readonly property bool interactive: !selectionModeActive

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

    // The whole field lights up on hover, which is what says the value is the
    // thing you click. Without it only the cursor changed, and a cursor is not
    // an affordance you can see coming.
    Rectangle {
        anchors.fill: parent
        anchors.leftMargin: -Kirigami.Units.smallSpacing / 2
        anchors.rightMargin: -Kirigami.Units.smallSpacing / 2
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Whatevr.Palette.highlight, fieldHover.hovered ? 0.10 : 0)

        Behavior on color {
            ColorAnimation { duration: Kirigami.Units.shortDuration }
        }
    }

    ColumnLayout {
        id: layout

        anchors.fill: parent
        spacing: 0

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: Kirigami.Units.iconSizes.small
                source: root.iconName
                fallback: "dialog-information-symbolic"
                color: fieldHover.hovered ? Whatevr.Palette.highlight : Kirigami.Theme.disabledTextColor
            }

            Controls.Label {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignVCenter
                text: root.value
                elide: Text.ElideRight
                maximumLineCount: 2
                wrapMode: Text.Wrap
                color: root.selectionModeActive ? Kirigami.Theme.textColor : Kirigami.Theme.linkColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                font.underline: fieldHover.hovered
            }

            // The badge is free: WhatsApp told us, in the vCard itself, which
            // numbers are reachable.
            Rectangle {
                Layout.alignment: Qt.AlignVCenter
                visible: root.onWhatsApp
                // The ends are fully round, and a round end eats the space next
                // to the text it curves past. Side padding is therefore scaled
                // to the pill's own height rather than set to a flat unit, which
                // is what left the words touching the curve.
                implicitHeight: badgeLabel.implicitHeight + Kirigami.Units.smallSpacing
                implicitWidth: badgeLabel.implicitWidth + implicitHeight * 0.8
                radius: height / 2
                color: Qt.alpha(Whatevr.Palette.highlight, 0.22)

                Controls.Label {
                    id: badgeLabel

                    anchors.centerIn: parent
                    text: Whatevr.I18n.i18nc("@label the number is reachable on WhatsApp", "on WhatsApp")
                    font.pointSize: Kirigami.Theme.smallFont.pointSize * 0.9
                }
            }

            CardActionButton {
                Layout.alignment: Qt.AlignVCenter
                visible: !root.selectionModeActive
                iconOnly: true
                iconName: "edit-copy-symbolic"
                text: Whatevr.I18n.i18nc("@action", "Copy")
                onClicked: Whatevr.ProtocolController.copyToClipboard(root.value)
            }
        }

        // What kind of field this is, under the value and indented to it, so it
        // reads as a caption on the value rather than as another value.
        Controls.Label {
            Layout.fillWidth: true
            Layout.leftMargin: root.textColumnX
            Layout.bottomMargin: Kirigami.Units.smallSpacing / 2
            visible: root.label.length > 0
            text: root.label
            color: Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize * 0.9
        }
    }

    TapHandler {
        enabled: root.interactive
        exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
        onSingleTapped: root.activate()
    }

    HoverHandler {
        id: fieldHover

        enabled: root.interactive
        cursorShape: Qt.PointingHandCursor
    }
}
