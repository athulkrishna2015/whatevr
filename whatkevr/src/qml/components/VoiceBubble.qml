// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A recorded voice note: a round play disc, the waveform of the recording,
 * elapsed time and a speed pill.
 *
 * Round and nameless on purpose. A voice note is someone talking, so it gets
 * the personal shape (the same reasoning that draws a video note as a circle on
 * the wallpaper) and the picture of its own sound. A shared audio file is an
 * object with a name and renders as AudioFileBubble instead.
 *
 * Playback lives on the AudioTransport below, which is the one AudioPlayer
 * singleton seen through this message.
 */
Item {
    id: root

    // The ChatBubble this belongs to, for the message fields and download state.
    required property ChatBubble row

    readonly property bool isPlaying: transport.isPlaying

    AudioTransport {
        id: transport

        row: root.row
    }

    MediaDragArea {
        anchors.fill: parent
        z: -1
        localPath: root.row.mediaLocalPath
        blocked: root.row.selectionModeActive
    }

    RowLayout {
        anchors.fill: parent
        spacing: Kirigami.Units.largeSpacing

        Controls.AbstractButton {
            id: playButton

            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 2.1
            implicitHeight: implicitWidth
            hoverEnabled: true
            enabled: root.row.messageId.length > 0
            text: {
                if (root.row.mediaDownloading)
                    return Whatevr.I18n.i18nc("@action:button", "Cancel")
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

            // A filled disc rather than a bare glyph: it is the one control in
            // the row, and it reads as a target at a glance.
            background: Rectangle {
                radius: width / 2
                color: root.row.isOutgoing
                    ? Qt.alpha(Kirigami.Theme.highlightColor, playButton.hovered ? 0.34 : 0.22)
                    : Qt.alpha(Whatevr.Palette.highlight, playButton.hovered ? 0.30 : 0.18)
            }

            contentItem: Kirigami.Icon {
                source: {
                    if (root.row.mediaDownloading)
                        return ""
                    if (!transport.hasFile)
                        return "folder-download-symbolic"
                    return root.isPlaying ? "media-playback-pause-symbolic" : "media-playback-start-symbolic"
                }
                color: root.row.isOutgoing ? Kirigami.Theme.highlightColor : Whatevr.Palette.highlight
                isMask: true
                implicitWidth: Kirigami.Units.iconSizes.smallMedium
                implicitHeight: Kirigami.Units.iconSizes.smallMedium
            }

            // A downloading note shows how far along it is, like every other
            // media bubble. The ring rides the button's rim rather than sitting
            // inside it, so the glyph underneath stays readable.
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
            spacing: 0

            Item {
                Layout.fillWidth: true
                Layout.fillHeight: true

                Whatevr.Waveform {
                    id: waveform

                    // Symmetric insets: an uneven pair shifted the drawn centre
                    // off the middle of the row.
                    anchors.fill: parent
                    anchors.topMargin: Kirigami.Units.smallSpacing / 2
                    anchors.bottomMargin: Kirigami.Units.smallSpacing / 2
                    // A sender that shipped no waveform (and no ffmpeg on the
                    // daemon to derive one) still gets a usable scrub bar,
                    // drawn as an even envelope.
                    values: root.row.mediaWaveform && root.row.mediaWaveform.length > 0
                        ? root.row.mediaWaveform
                        : flatWaveform
                    progress: transport.progress
                    playedColor: root.row.isOutgoing
                        ? Kirigami.Theme.highlightColor
                        : Whatevr.Palette.highlight
                    pendingColor: Qt.alpha(Kirigami.Theme.textColor, 0.28)
                    // Wide enough to read as a bar rather than a hairline: a
                    // 2/2 pitch packed 130 bars into the row and drew a fence.
                    barWidth: 3
                    barSpacing: 3

                    Accessible.role: Accessible.Slider
                    Accessible.name: Whatevr.I18n.i18nc("@info:whatsthis", "Playback position")

                    readonly property var flatWaveform: {
                        const bars = []
                        for (let i = 0; i < 48; ++i)
                            bars.push(34)
                        return bars
                    }

                    // Where this note will pick up if it is resumed, so a long
                    // one does not silently restart somewhere in the middle.
                    // On the centre line, which is the waveform's own baseline:
                    // hanging it below the bars read as a stray dot.
                    Rectangle {
                        visible: !transport.isCurrent && transport.elapsedSeconds > 0 && transport.totalSeconds > 0
                        width: Math.max(3, Math.round(Kirigami.Units.smallSpacing * 0.75))
                        height: width
                        radius: width / 2
                        color: root.row.isOutgoing ? Kirigami.Theme.highlightColor : Whatevr.Palette.highlight
                        x: Math.round(transport.progress * (parent.width - width))
                        anchors.verticalCenter: parent.verticalCenter
                    }

                    TapHandler {
                        enabled: transport.canScrub
                        onTapped: eventPoint => transport.scrubTo(waveform.fractionAt(eventPoint.position.x))
                    }

                    // Dragging the waveform scrubs continuously; a tap alone
                    // makes finding a spot in a two-minute note a guessing game.
                    DragHandler {
                        id: scrubHandler

                        enabled: transport.canScrub
                        target: null
                        xAxis.enabled: true
                        yAxis.enabled: false
                        onCentroidChanged: {
                            if (active)
                                transport.scrubTo(waveform.fractionAt(centroid.position.x))
                        }
                    }
                }
            }

            RowLayout {
                Layout.fillWidth: true
                // The time and ticks ride the end of this line rather than
                // taking a row of their own; the row keeps their width clear.
                Layout.rightMargin: root.row.tntReserveWidth
                spacing: Kirigami.Units.smallSpacing

                Controls.Label {
                    Layout.fillWidth: !transport.available
                    // A dead audio engine used to leave the play button doing
                    // nothing at all, with no way to tell why.
                    text: {
                        if (!transport.available)
                            return Whatevr.I18n.i18nc("@info", "Playback unavailable")
                        if (transport.isCurrent || transport.elapsedSeconds > 0)
                            return transport.formatTime(transport.elapsedSeconds) + " / " + transport.formatTime(transport.totalSeconds)
                        return transport.formatTime(transport.totalSeconds)
                    }
                    color: transport.available ? Kirigami.Theme.disabledTextColor : Kirigami.Theme.negativeTextColor
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                    elide: Text.ElideRight

                    Controls.ToolTip.text: Whatevr.I18n.i18nc("@info", "The mpv audio engine could not be initialized")
                    Controls.ToolTip.visible: !transport.available && errorHover.hovered
                    Controls.ToolTip.delay: Kirigami.Units.toolTipDelay

                    HoverHandler {
                        id: errorHover
                    }
                }

                Item {
                    Layout.fillWidth: true
                }

                // The speed pill only appears while this note is the one
                // playing: it is a control for the current stream, not a
                // per-message setting. Its width is held even when it is
                // hidden, so pressing play does not shove the elapsed time
                // sideways at the moment you are reading it.
                Controls.AbstractButton {
                    id: speedPill

                    // Faded rather than hidden: a Layout skips an invisible
                    // item outright, so `visible` would give the width back and
                    // reintroduce the shift this is here to stop.
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
