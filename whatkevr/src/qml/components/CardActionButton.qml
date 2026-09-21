// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * An action on a card inside a message bubble: contact, location, poll, and
 * every card kind after them.
 *
 * Two things make a plain ToolButton wrong here. Its hover tint is drawn for
 * the window background rather than for a tinted card, where it is close enough
 * to invisible that the actions read as labels. And a message row stacks a
 * full-bleed right-click MouseArea over its content, which takes item hover
 * events before anything under it sees them, so `hovered` alone never becomes
 * true inside a card. The highlight therefore comes from a HoverHandler as
 * well: pointer handlers are delivered whatever else is layered above.
 */
Controls.AbstractButton {
    id: root

    objectName: "cardActionButton"

    property string iconName: ""
    /// Icon with no text. The text is still used as the accessible name and the
    /// tooltip, because an icon on its own is not a label.
    property bool iconOnly: false

    /// Whether to draw the button as hovered. Not simply `hovered`: see above.
    readonly property bool highlighted: hovered || buttonHover.hovered

    /// How far the button's glyph sits inside its own hover plate. A row of
    /// these pulls itself left by this much, so the glyphs land on the same
    /// column as the field icons above them rather than a padding's width in.
    readonly property real contentInset: Kirigami.Units.smallSpacing

    hoverEnabled: true
    padding: Kirigami.Units.smallSpacing
    leftPadding: contentInset
    rightPadding: contentInset

    Accessible.name: text
    Controls.ToolTip.text: text
    Controls.ToolTip.visible: iconOnly && highlighted
    Controls.ToolTip.delay: Kirigami.Units.toolTipDelay

    HoverHandler {
        id: buttonHover

        cursorShape: Qt.PointingHandCursor
    }

    background: Rectangle {
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Whatevr.Palette.highlight,
                        root.down ? 0.32 : (root.highlighted ? 0.18 : 0))
        border.width: 1
        border.color: root.visualFocus ? Whatevr.Palette.highlight : "transparent"

        Behavior on color {
            ColorAnimation { duration: Kirigami.Units.shortDuration }
        }
    }

    contentItem: Row {
        spacing: Kirigami.Units.smallSpacing

        Kirigami.Icon {
            anchors.verticalCenter: parent.verticalCenter
            visible: root.iconName.length > 0
            width: Kirigami.Units.iconSizes.small
            height: width
            source: root.iconName
            fallback: "dialog-information-symbolic"
            // An action reads at full strength and brightens to the accent
            // under the pointer. Drawing it muted until hovered made the row
            // look disabled, which is the opposite of what it is.
            color: root.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
        }

        Controls.Label {
            anchors.verticalCenter: parent.verticalCenter
            visible: !root.iconOnly
            text: root.text
            color: root.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }
}
