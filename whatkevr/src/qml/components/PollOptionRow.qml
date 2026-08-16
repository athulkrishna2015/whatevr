// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * One answer on a poll: a tick, the option, the bar behind it, and the faces of
 * the people who chose it.
 *
 * The bar is relative to the leading option rather than to the total, so a
 * three-way split reads as a race rather than as three short stubs. The avatar
 * stack is the part WhatsApp does not do at all: it shows a count and hides the
 * names behind another screen, and on a desktop there is room to just say who.
 */
Item {
    id: root

    required property var option
    required property bool chosen
    required property int leadingCount
    required property int totalVoters
    required property bool multipleAllowed
    required property bool interactive
    required property real bodyPointSize

    signal toggled()

    readonly property var voters: option.voters ?? []
    readonly property int count: voters.length
    readonly property real share: leadingCount > 0 ? count / leadingCount : 0

    /// How many faces fit before the row starts counting instead.
    readonly property int maxFaces: 3
    readonly property int hiddenFaces: Math.max(0, count - maxFaces)

    implicitHeight: layout.implicitHeight + Kirigami.Units.smallSpacing

    // The bar. It sits behind the row's content rather than beside it, so the
    // option's text stays where it is as the numbers move.
    Rectangle {
        anchors.left: parent.left
        anchors.verticalCenter: parent.verticalCenter
        height: parent.height
        width: parent.width * root.share
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Whatevr.Palette.highlight, root.chosen ? 0.30 : 0.16)

        Behavior on width {
            NumberAnimation { duration: Kirigami.Units.longDuration; easing.type: Easing.OutCubic }
        }
        Behavior on color {
            ColorAnimation { duration: Kirigami.Units.shortDuration }
        }
    }

    RowLayout {
        id: layout

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        anchors.leftMargin: Kirigami.Units.smallSpacing / 2
        anchors.rightMargin: Kirigami.Units.smallSpacing / 2
        spacing: Kirigami.Units.smallSpacing

        // A circle for one answer, a square for several: the same shorthand
        // every form uses, so the poll's rules are legible before you tap.
        Rectangle {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.iconSizes.small * 0.85
            implicitHeight: implicitWidth
            radius: root.multipleAllowed ? Kirigami.Units.cornerRadius / 2 : width / 2
            color: root.chosen ? Whatevr.Palette.highlight : "transparent"
            border.color: root.chosen
                ? Whatevr.Palette.highlight
                : Qt.alpha(Kirigami.Theme.textColor, 0.4)
            border.width: 1

            Kirigami.Icon {
                anchors.centerIn: parent
                visible: root.chosen
                width: parent.width * 0.7
                height: width
                source: "checkmark-symbolic"
                fallback: "dialog-ok"
                color: Whatevr.Palette.highlightedText
                isMask: true
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            text: String(root.option.name ?? "")
            wrapMode: Text.Wrap
            maximumLineCount: 3
            elide: Text.ElideRight
            font.pointSize: root.bodyPointSize
            font.bold: root.chosen
        }

        // Who chose it. Overlapping so a crowded option stays one compact
        // cluster rather than pushing the count off the row.
        Item {
            Layout.alignment: Qt.AlignVCenter
            visible: root.count > 0
            implicitHeight: Kirigami.Units.gridUnit * 1.1
            implicitWidth: {
                const shown = Math.min(root.count, root.maxFaces)
                if (shown === 0)
                    return 0
                return Kirigami.Units.gridUnit * 1.1 + (shown - 1) * Kirigami.Units.gridUnit * 0.72
            }

            Repeater {
                model: Math.min(root.count, root.maxFaces)

                delegate: AvatarImage {
                    required property int index

                    readonly property var voter: root.voters[index] ?? ({})

                    x: index * Kirigami.Units.gridUnit * 0.72
                    width: Kirigami.Units.gridUnit * 1.1
                    height: width
                    z: root.maxFaces - index
                    avatarLocalPath: String(voter.avatar_path ?? "")
                    // Our own vote is labelled rather than named: the account
                    // has no sender row of its own, so there is nothing to
                    // resolve, and "You" is what the reader wants anyway.
                    initials: {
                        const name = voter.from_me
                            ? Whatevr.I18n.i18nc("@label the account's own poll vote", "You")
                            : String(voter.name ?? "")
                        if (name.length === 0)
                            return "?"
                        const words = name.trim().split(/\s+/)
                        return words.length === 1
                            ? words[0].charAt(0).toUpperCase()
                            : (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase()
                    }
                }
            }
        }

        Controls.Label {
            Layout.alignment: Qt.AlignVCenter
            visible: root.hiddenFaces > 0
            text: Whatevr.I18n.i18nc("@label voters beyond the ones shown", "+%1", root.hiddenFaces)
            color: Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.alignment: Qt.AlignVCenter
            Layout.minimumWidth: Kirigami.Units.gridUnit
            horizontalAlignment: Text.AlignRight
            text: String(root.count)
            color: root.chosen ? Kirigami.Theme.textColor : Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
            font.bold: root.chosen
        }
    }

    TapHandler {
        enabled: root.interactive
        exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
        onSingleTapped: root.toggled()
    }

    HoverHandler {
        enabled: root.interactive
        cursorShape: Qt.PointingHandCursor
    }
}
