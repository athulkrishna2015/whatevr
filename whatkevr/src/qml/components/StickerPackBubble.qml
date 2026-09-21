// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A sticker pack somebody shared.
 *
 * The one card in this family that does something rather than describing
 * something: whatevr already keeps a sticker library and already has a command
 * to put a pack in it, so a share is a real offer rather than a notice that
 * an offer exists somewhere else.
 *
 * Whether it can be taken up is the daemon's answer, joined from the library at
 * read time. A pack somebody assembled on their own phone has no entry to
 * install, so the card says so instead of offering a button that fails; a pack
 * already in the picker says that instead of offering to add it twice.
 */
Item {
    id: root

    objectName: "stickerPackBubble"

    required property ChatBubble row

    readonly property var pack: row.stickerPack ?? ({})

    readonly property string packId: String(pack.pack_id ?? "")
    readonly property string name: String(pack.name ?? "")
    readonly property string publisher: String(pack.publisher ?? "")
    readonly property string description: String(pack.description ?? "")
    readonly property string caption: String(pack.caption ?? "")
    readonly property int count: pack.count ?? 0
    readonly property bool installable: pack.installable ?? false
    readonly property bool installed: pack.installed ?? false

    readonly property real contentMargin: Kirigami.Units.smallSpacing
    readonly property real cornerRadius: Kirigami.Units.cornerRadius
    readonly property real thumbSize: Kirigami.Units.gridUnit * 3.4

    readonly property real preferredWidth: {
        const text = Math.max(nameMetrics.advanceWidth, byMetrics.advanceWidth)
        return contentMargin * 2 + thumbSize + Kirigami.Units.smallSpacing
            + Math.max(text, Kirigami.Units.gridUnit * 9)
    }

    implicitWidth: row.attachmentBlockWidth
    // See InteractiveBubble: a layout has no implicit height until it first
    // arranges, and a row that reserves nothing and then grows is a row that
    // shoves the transcript under it.
    readonly property real unmeasuredBodyHeight: Kirigami.Units.gridUnit * 4
    implicitHeight: contentMargin * 2
                    + (body.implicitHeight > 0 ? body.implicitHeight : unmeasuredBodyHeight)

    /// Who made it and how many there are, on one line: both are what somebody
    /// decides on, and neither is worth a line of its own.
    readonly property string byLine: {
        const size = root.count > 0
            ? Whatevr.I18n.i18ncp("@label sticker count", "%1 sticker", "%1 stickers", root.count)
            : ""
        if (root.publisher.length > 0 && size.length > 0) {
            return Whatevr.I18n.i18nc("@label pack author and size, %1 is a name and %2 a count",
                                      "%1 · %2", root.publisher, size)
        }
        return root.publisher.length > 0 ? root.publisher : size
    }

    TextMetrics {
        id: nameMetrics

        font: nameLabel.font
        text: root.name
    }

    TextMetrics {
        id: byMetrics

        font: byLabel.font
        text: root.byLine
    }

    Rectangle {
        anchors.fill: parent
        radius: root.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    ColumnLayout {
        id: body

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            // No picture: a pack share carries none, and the stickers
            // themselves are behind a download this card does not make. The
            // glyph is honest about that.
            Rectangle {
                Layout.alignment: Qt.AlignTop
                implicitWidth: root.thumbSize
                implicitHeight: root.thumbSize
                radius: Math.round(root.cornerRadius * 0.75)
                color: Qt.alpha(Kirigami.Theme.textColor, 0.06)

                Kirigami.Icon {
                    anchors.centerIn: parent
                    implicitWidth: Kirigami.Units.iconSizes.medium
                    implicitHeight: implicitWidth
                    source: "smiley-symbolic"
                    color: Kirigami.Theme.disabledTextColor
                    isMask: true
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignTop
                spacing: Kirigami.Units.smallSpacing / 2

                Controls.Label {
                    id: nameLabel

                    Layout.fillWidth: true
                    visible: root.name.length > 0
                    text: root.name
                    wrapMode: Text.Wrap
                    elide: Text.ElideRight
                    maximumLineCount: 2
                    font.pointSize: root.row.bodyPointSize
                    font.bold: true
                }

                Controls.Label {
                    id: byLabel

                    Layout.fillWidth: true
                    visible: root.byLine.length > 0
                    text: root.byLine
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.description.length > 0
            text: root.description
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.caption.length > 0
            text: root.caption
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        CardActionButton {
            Layout.alignment: Qt.AlignLeft
            objectName: "stickerPackAddButton"
            visible: root.installable && !root.installed
            enabled: !root.row.selectionModeActive
            text: Whatevr.I18n.i18nc("@action add a shared sticker pack to the library", "Add pack")
            iconName: "list-add-symbolic"
            onClicked: Whatevr.ProtocolController.stickers.setPackInstalled(root.packId, true)
        }

        RowLayout {
            Layout.fillWidth: true
            visible: root.installed
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: "checkmark-symbolic"
                color: Kirigami.Theme.positiveTextColor
                isMask: true
            }

            Controls.Label {
                Layout.fillWidth: true
                text: Whatevr.I18n.i18nc("@info the shared pack is already in the library", "In your stickers")
                color: Kirigami.Theme.disabledTextColor
                elide: Text.ElideRight
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }
        }

        // A pack that is nobody's but its author's. There is no entry to
        // install, so saying so beats a button that would fail.
        Controls.Label {
            Layout.fillWidth: true
            visible: !root.installable && !root.installed
            text: Whatevr.I18n.i18nc("@info a shared pack that is not in WhatsApp's own catalogue",
                                     "This pack is not in the sticker store, so it can only be added on your phone")
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }
}
