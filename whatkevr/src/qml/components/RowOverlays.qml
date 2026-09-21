pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// The three things that sit on top of a message row rather than in it: the
// multi-select chrome, the jump-to-reply glow, and the hover reply button.
//
// They live together in one file because they are loaded together, by one
// Loader on the row. Separately they were three Loaders and three components on
// every delegate in the chat, six objects, and a row that is not selected, not
// glowing and not under the pointer (which is every row, almost always) paid
// all six of them. One Loader with nothing to load costs one.
//
// The trade is that a row in any of these three states now pays for the two
// that it is not, plus this container. That is the right way round: the states
// are rare and short, and at most one row is under the pointer at a time.
Item {
    id: overlays

    required property ChatBubble row

    anchors.fill: parent

        // Everything multi-select mode needs, the covering click surface, the row
        // tint and the check circle, is built only while that mode is on. It used
        // to be three permanently instantiated (and normally invisible) subtrees on
        // every row, worth roughly seven objects each time (DN9).
        Loader {
            id: selectionChromeLoader

            anchors.fill: parent
            active: overlays.row.selectionModeActive

            sourceComponent: Item {
                anchors.fill: parent

                // Selection-mode click surface: every left click toggles this
                // message and nothing underneath (links, reply button, image
                // buttons) reacts.
                MouseArea {
                    anchors.fill: parent
                    acceptedButtons: Qt.LeftButton
                    z: 10
                    cursorShape: Qt.PointingHandCursor
                    onClicked: overlays.row.selectionToggleRequested()
                }

                // Selection tint over the message row, excluding the date-pill
                // region at the top so the day separator is never highlighted.
                Rectangle {
                    anchors.fill: parent
                    anchors.topMargin: overlays.row.dateSeparatorHeight
                    z: 6
                    visible: overlays.row.selected
                    color: Qt.alpha(Kirigami.Theme.highlightColor, 0.14)
                    radius: Kirigami.Units.cornerRadius
                }

                // Selection check circle in the free space opposite the bubble
                // (mirrors the hover reply button's placement), so nothing shifts.
                Rectangle {
                    id: selectionCheck

                    readonly property real desiredX: overlays.row.isOutgoing
                                                     ? overlays.row.visualX - width - Kirigami.Units.smallSpacing
                                                     : overlays.row.visualX + overlays.row.visualWidth + Kirigami.Units.smallSpacing

                    z: 11
                    x: Math.round(Math.max(overlays.row.outerMargin,
                                           Math.min(overlays.row.width - overlays.row.outerMargin - width, desiredX)))
                    y: Math.round(overlays.row.visualY + Math.max(0, overlays.row.visualHeight - height) / 2)
                    width: Kirigami.Units.iconSizes.smallMedium + Kirigami.Units.smallSpacing
                    height: width
                    radius: width / 2
                    color: overlays.row.selected ? Kirigami.Theme.highlightColor : Qt.alpha(Kirigami.Theme.backgroundColor, 0.92)
                    border.color: overlays.row.selected ? Kirigami.Theme.highlightColor : Qt.alpha(Kirigami.Theme.textColor, 0.38)
                    border.width: 1

                    Behavior on color {
                        ColorAnimation {
                            duration: Kirigami.Units.shortDuration
                            easing.type: Easing.OutCubic
                        }
                    }

                    Kirigami.Icon {
                        anchors.centerIn: parent
                        visible: overlays.row.selected
                        source: overlays.row.tickSource
                        width: Math.round(parent.width * 0.62)
                        height: width
                        color: Kirigami.Theme.highlightedTextColor
                        isMask: true
                    }
                }
            }
        }

        // Instantiated only while the jump-to-reply glow animation is running.
        Loader {
            active: overlays.row.replyGlowOpacity > 0
            x: Math.round(overlays.row.replyGlowLeft - overlays.row.replyGlowPadding)
            y: Math.round(overlays.row.replyGlowTop - overlays.row.replyGlowPadding)
            z: 7
            width: Math.max(0, Math.round(overlays.row.replyGlowRight - overlays.row.replyGlowLeft + overlays.row.replyGlowPadding * 2))
            height: Math.max(0, Math.round(overlays.row.replyGlowBottom - overlays.row.replyGlowTop + overlays.row.replyGlowPadding * 2))

            sourceComponent: Item {
                id: replyGlowOverlay

                readonly property real innerMargin: Math.max(1, Math.round(Kirigami.Units.smallSpacing / 2))

                anchors.fill: parent
                opacity: overlays.row.replyGlowOpacity

                Rectangle {
                    id: replyGlowOuter

                    anchors.fill: parent
                    radius: Kirigami.Units.cornerRadius + overlays.row.replyGlowPadding
                    color: Qt.alpha(Kirigami.Theme.highlightColor, 0.06)
                    border.color: Qt.alpha(Kirigami.Theme.highlightColor, 0.72)
                    border.width: Math.max(2, Math.round(Kirigami.Units.smallSpacing / 2))
                }

                Rectangle {
                    anchors.fill: parent
                    anchors.margins: replyGlowOverlay.innerMargin
                    radius: Math.max(0, replyGlowOuter.radius - replyGlowOverlay.innerMargin)
                    color: "transparent"
                    border.color: Qt.alpha(Kirigami.Theme.highlightColor, 0.28)
                    border.width: 1
                }
            }
        }

        // Built lazily on first hover of the row (hoverLatched): scrolling never
        // pays for the button, only the rows the pointer actually visits do. Off
        // the frame's critical path too, for the same reason as the selection
        // surface above: rows crossing an idle cursor must not each cost a stall.
        Loader {
            anchors.fill: parent
            asynchronous: true
            active: overlays.row.hoverLatched && overlays.row.canReply && !overlays.row.pooled
            z: 8

            sourceComponent: Item {
                ToolButton {
                    id: replyButton

                    readonly property real desiredX: overlays.row.isOutgoing
                                                     ? overlays.row.visualX - width - Kirigami.Units.smallSpacing
                                                     : overlays.row.visualX + overlays.row.visualWidth + Kirigami.Units.smallSpacing

                    enabled: opacity > 0.01
                    opacity: (rowHoverHandler.hovered || hovered || pressed) ? 1 : 0
                    x: Math.round(Math.max(overlays.row.outerMargin,
                                           Math.min(overlays.row.width - overlays.row.outerMargin - width, desiredX)))
                    y: Math.round(overlays.row.visualY + Math.max(0, overlays.row.visualHeight - height) / 2)
                    width: Math.round(Math.max(Kirigami.Units.iconSizes.smallMedium + Kirigami.Units.smallSpacing,
                                               Math.min(Kirigami.Units.gridUnit * 1.45,
                                                        overlays.row.visualHeight - Kirigami.Units.smallSpacing)))
                    height: width
                    icon.name: "smiley-add-symbolic"
                    // Set both dimensions to the constant directly; binding icon.height to
                    // icon.width loops through the control's implicit-size machinery.
                    icon.width: Kirigami.Units.iconSizes.smallMedium
                    icon.height: Kirigami.Units.iconSizes.smallMedium
                    text: Whatevr.I18n.i18nc("@action:button", "React")
                    display: AbstractButton.IconOnly
                    focusPolicy: Qt.NoFocus
                    hoverEnabled: true
                    onClicked: overlays.row.reactionPickerRequested(x + width / 2, y)

                    contentItem: Item {
                        Kirigami.Icon {
                            anchors.centerIn: parent
                            source: replyButton.icon.name
                            width: replyButton.icon.width
                            height: replyButton.icon.height
                            color: Kirigami.Theme.textColor
                            isMask: true
                        }
                    }

                    background: Rectangle {
                        radius: width / 2
                        color: Qt.alpha(Kirigami.Theme.backgroundColor, replyButton.hovered || replyButton.pressed ? 0.98 : 0.9)
                        border.color: Qt.alpha(Kirigami.Theme.textColor, replyButton.hovered || replyButton.pressed ? 0.24 : 0.14)
                        border.width: 1
                    }

                    Behavior on opacity {
                        NumberAnimation {
                            duration: Kirigami.Units.shortDuration
                            easing.type: Easing.OutCubic
                        }
                    }
                }
            }
        }
}
