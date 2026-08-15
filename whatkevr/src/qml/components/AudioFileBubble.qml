// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

import "MediaFormat.js" as MediaFormat

/**
 * A shared audio file: a square play tile, the track's name, a slim seek line
 * and the facts that go with a file (length, size, format).
 *
 * Squared off and named, against the voice note's round disc and waveform. A
 * recording is someone talking and has nothing to call it; a track someone sent
 * is an object on disk, so it reads like the document row it is a sibling of,
 * with a transport bolted on. That, rather than a badge on the glyph, is what
 * tells the two apart at a glance in a scrolling chat.
 */
Item {
    id: root

    required property ChatBubble row

    readonly property bool isPlaying: transport.isPlaying
    readonly property string displayName: row.mediaFileName.length > 0
        ? row.mediaFileName
        : Whatevr.I18n.i18nc("@label unnamed audio attachment", "Audio")

    /// Length, weight and format: the same shape of detail line a document
    /// carries, so the two rows read as the same kind of thing.
    readonly property string detailText: {
        const parts = []
        if (transport.totalSeconds > 0) {
            parts.push(transport.isCurrent || transport.elapsedSeconds > 0
                ? MediaFormat.clockTime(transport.elapsedSeconds) + " / " + MediaFormat.clockTime(transport.totalSeconds)
                : MediaFormat.clockTime(transport.totalSeconds))
        }
        const size = MediaFormat.humanSize(row.mediaSizeBytes)
        if (size.length > 0)
            parts.push(size)
        // "audio/mpeg" → "MPEG". The subtype is what a person would call the
        // format, and anything longer than a short token is codec detail.
        const subtype = row.mediaMimeType.includes("/")
            ? row.mediaMimeType.split("/").pop().split(";")[0].replace("x-", "").toUpperCase()
            : ""
        if (subtype.length > 0 && subtype.length <= 5)
            parts.push(subtype)
        return parts.join(" · ")
    }

    AudioTransport {
        id: transport

        row: root.row
    }

    Rectangle {
        anchors.fill: parent
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    MediaDragArea {
        anchors.fill: parent
        z: -1
        localPath: root.row.mediaLocalPath
        blocked: root.row.selectionModeActive
    }

    RowLayout {
        anchors.fill: parent
        anchors.margins: Kirigami.Units.smallSpacing
        spacing: Kirigami.Units.smallSpacing

        // Square, where a voice note's is a circle. Same tint and same states,
        // so only the shape carries the difference.
        Controls.AbstractButton {
            id: playTile

            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 2.1
            implicitHeight: implicitWidth
            hoverEnabled: true
            enabled: root.row.messageId.length > 0 && !root.row.mediaDownloading
            text: {
                if (!transport.hasFile)
                    return Whatevr.I18n.i18nc("@action:button", "Download")
                return root.isPlaying
                    ? Whatevr.I18n.i18nc("@action:button", "Pause")
                    : Whatevr.I18n.i18nc("@action:button", "Play")
            }
            Accessible.name: text
            Controls.ToolTip.text: text
            Controls.ToolTip.visible: hovered
            Controls.ToolTip.delay: Kirigami.Units.toolTipDelay
            onClicked: {
                transport.activate()
                root.row.conversationFocusRequested()
            }

            background: Rectangle {
                radius: Kirigami.Units.cornerRadius
                color: root.row.isOutgoing
                    ? Qt.alpha(Kirigami.Theme.highlightColor, playTile.hovered ? 0.34 : 0.22)
                    : Qt.alpha(Whatevr.Palette.highlight, playTile.hovered ? 0.30 : 0.18)
            }

            contentItem: Kirigami.Icon {
                // A track at rest shows what it is; hovering or playing turns
                // the tile into a transport control.
                //
                // The resting glyph is an action icon, not the audio mimetype
                // one. A mimetype icon is drawn differently per size in Breeze:
                // a bare pair of notes at 22px, a document card at 64px. A
                // HiDPI screen asks for 44px and so gets the card, which isMask
                // then flattens into a featureless slab. Action icons ship one
                // drawing at every size, so this looks the same everywhere.
                source: {
                    if (root.row.mediaDownloading)
                        return ""
                    if (!transport.hasFile)
                        return "folder-download-symbolic"
                    if (root.isPlaying)
                        return "media-playback-pause-symbolic"
                    if (playTile.hovered || transport.isCurrent)
                        return "media-playback-start-symbolic"
                    return "media-album-track-symbolic"
                }
                // A theme without it falls back to the transport glyph rather
                // than to a mimetype icon, which would reintroduce the slab.
                fallback: "media-playback-start-symbolic"
                color: root.row.isOutgoing ? Kirigami.Theme.highlightColor : Whatevr.Palette.highlight
                isMask: true
                implicitWidth: Kirigami.Units.iconSizes.smallMedium
                implicitHeight: Kirigami.Units.iconSizes.smallMedium
            }

            ProgressCircle {
                anchors.fill: parent
                visible: root.row.mediaDownloading && root.row.mediaDownloadProgress >= 0
                showLabel: false
                lineWidth: Math.max(2, Math.round(Kirigami.Units.smallSpacing / 2))
                progress: Math.max(0, root.row.mediaDownloadProgress)
            }

            Controls.BusyIndicator {
                anchors.centerIn: parent
                visible: root.row.mediaDownloading && root.row.mediaDownloadProgress < 0
                running: visible
                implicitWidth: Kirigami.Units.gridUnit * 1.5
                implicitHeight: implicitWidth
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            // The time and ticks ride the end of the detail line rather than
            // taking a row of their own; the column keeps their width clear.
            Layout.rightMargin: root.row.tntReserveWidth
            spacing: 0

            Controls.Label {
                Layout.fillWidth: true
                text: root.displayName
                // Middle, not right: the extension is half of what names a
                // track's format, and it is at the end.
                elide: Text.ElideMiddle
                maximumLineCount: 1
                font.pointSize: root.row.bodyPointSize
            }

            // A plain line, not a waveform. There is nothing meaningful to draw
            // for a three-minute song at this width, and the flat track is the
            // second half of the shape difference from a voice note.
            AudioSeekLine {
                id: seekLine

                Layout.fillWidth: true
                Layout.topMargin: Kirigami.Units.smallSpacing / 2
                Layout.bottomMargin: Kirigami.Units.smallSpacing / 2
                progress: transport.progress
                interactive: transport.canScrub
                showKnob: transport.isCurrent || transport.elapsedSeconds > 0
                playedColor: root.row.isOutgoing
                    ? Kirigami.Theme.highlightColor
                    : Whatevr.Palette.highlight
                onScrubbed: fraction => transport.scrubTo(fraction)
            }

            RowLayout {
                Layout.fillWidth: true
                spacing: Kirigami.Units.smallSpacing

                Controls.Label {
                    Layout.fillWidth: true
                    text: {
                        if (!transport.available)
                            return Whatevr.I18n.i18nc("@info", "Playback unavailable")
                        if (root.row.mediaDownloadError.length > 0)
                            return root.row.mediaDownloadError
                        return root.detailText
                    }
                    color: !transport.available || root.row.mediaDownloadError.length > 0
                        ? Kirigami.Theme.negativeTextColor
                        : Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize

                    Controls.ToolTip.text: Whatevr.I18n.i18nc("@info", "The mpv audio engine could not be initialized")
                    Controls.ToolTip.visible: !transport.available && errorHover.hovered
                    Controls.ToolTip.delay: Kirigami.Units.toolTipDelay

                    HoverHandler {
                        id: errorHover
                    }
                }

                // Same rule as the voice note's: a control for the current
                // stream, faded rather than hidden so the detail line does not
                // shift sideways when playback starts.
                Controls.AbstractButton {
                    id: speedPill

                    opacity: transport.isCurrent ? 1 : 0
                    enabled: transport.isCurrent
                    hoverEnabled: true
                    implicitWidth: speedLabel.implicitWidth + Kirigami.Units.smallSpacing * 2
                    implicitHeight: speedLabel.implicitHeight + Kirigami.Units.smallSpacing / 2
                    Layout.alignment: Qt.AlignVCenter

                    Behavior on opacity {
                        NumberAnimation { duration: Kirigami.Units.shortDuration }
                    }
                    text: Whatevr.I18n.i18nc("@action:button", "Playback speed")
                    Accessible.name: text
                    Accessible.ignored: !transport.isCurrent
                    Controls.ToolTip.text: text
                    Controls.ToolTip.visible: hovered
                    Controls.ToolTip.delay: Kirigami.Units.toolTipDelay
                    onClicked: Whatevr.AudioPlayer.cycleSpeed()

                    background: Rectangle {
                        radius: height / 2
                        color: Qt.alpha(Kirigami.Theme.textColor, speedPill.hovered ? 0.20 : 0.12)
                    }

                    contentItem: Controls.Label {
                        id: speedLabel

                        text: Whatevr.I18n.i18nc("@label playback speed", "%1x",
                                                 Whatevr.AudioPlayer.speed.toFixed(
                                                     Whatevr.AudioPlayer.speed === Math.round(Whatevr.AudioPlayer.speed) ? 0 : 1))
                        color: Kirigami.Theme.textColor
                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                }
            }
        }
    }
}
