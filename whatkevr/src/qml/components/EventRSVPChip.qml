// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * One of an event's three answers, with the faces of the people who gave it.
 *
 * The faces are the point. A count says four people are going; the faces say
 * which four, which is the question somebody deciding whether to go is actually
 * asking. WhatsApp shows a number.
 */
Item {
    id: root

    objectName: "eventRSVPChip"

    required property string response
    required property string label
    required property string iconName
    required property int count
    /// The responders who gave this answer, newest last.
    required property var faces
    /// Whether this is our own answer.
    required property bool chosen
    property bool interactive: true

    /// How many faces fit before the cluster is more crowding than information.
    readonly property int maxFaces: 3

    signal picked()

    implicitHeight: layout.implicitHeight + Kirigami.Units.smallSpacing * 2
    implicitWidth: layout.implicitWidth + Kirigami.Units.smallSpacing * 2

    Rectangle {
        anchors.fill: parent
        radius: Kirigami.Units.cornerRadius
        // Our own answer is filled; the others are outlined. A chip that
        // changed only its border on being chosen was impossible to find at a
        // glance in a row of three.
        color: root.chosen
            ? Qt.alpha(Whatevr.Palette.highlight, 0.22)
            : Qt.alpha(Kirigami.Theme.textColor, chipHover.hovered && root.interactive ? 0.10 : 0.04)
        border.width: 1
        border.color: root.chosen
            ? Whatevr.Palette.highlight
            : Qt.alpha(Kirigami.Theme.textColor, 0.12)

        Behavior on color {
            ColorAnimation { duration: Kirigami.Units.shortDuration }
        }
    }

    ColumnLayout {
        id: layout

        anchors.centerIn: parent
        spacing: Kirigami.Units.smallSpacing / 2

        RowLayout {
            Layout.alignment: Qt.AlignHCenter
            spacing: Kirigami.Units.smallSpacing / 2

            Kirigami.Icon {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: root.iconName
                fallback: "dialog-information-symbolic"
                color: root.chosen ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
            }

            Controls.Label {
                Layout.alignment: Qt.AlignVCenter
                text: root.count > 0
                    ? Whatevr.I18n.i18nc("@label an rsvp answer and how many gave it, e.g. Going 4",
                                         "%1 %2", root.label, root.count)
                    : root.label
                color: root.chosen ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
                elide: Text.ElideRight
                maximumLineCount: 1
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                font.bold: root.chosen
            }
        }

        // The cluster, overlapping so a row of three chips still fits a bubble.
        Item {
            Layout.alignment: Qt.AlignHCenter
            visible: root.count > 0
            implicitHeight: Kirigami.Units.gridUnit
            implicitWidth: {
                const shown = Math.min(root.count, root.maxFaces)
                if (shown === 0)
                    return 0
                return Kirigami.Units.gridUnit + (shown - 1) * Kirigami.Units.gridUnit * 0.66
            }

            Repeater {
                model: Math.min(root.count, root.maxFaces)

                delegate: AvatarImage {
                    required property int index

                    readonly property var responder: root.faces[index] ?? ({})

                    x: index * Kirigami.Units.gridUnit * 0.66
                    width: Kirigami.Units.gridUnit
                    height: width
                    z: root.maxFaces - index
                    avatarLocalPath: String(responder.avatar_path ?? "")
                    // Our own answer is labelled rather than named: the account
                    // has no sender row of its own, so there is nothing to
                    // resolve, and "You" is what the reader wants anyway.
                    initials: {
                        const name = responder.from_me
                            ? Whatevr.I18n.i18nc("@label the account's own rsvp", "You")
                            : String(responder.name ?? "")
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
    }

    TapHandler {
        enabled: root.interactive
        exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
        onSingleTapped: root.picked()
    }

    HoverHandler {
        id: chipHover

        enabled: root.interactive
        cursorShape: Qt.PointingHandCursor
    }
}
