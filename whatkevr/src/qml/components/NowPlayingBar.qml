// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * The transport for whatever audio is playing, sitting at the top of the chat
 * list.
 *
 * Playback follows you out of the chat it started in, so the controls have to
 * follow it: once you have left that chat its bubble is not on screen (its
 * subscription may be gone entirely), and without this there would be no way to
 * pause, scrub or even tell what is making noise.
 *
 * Deliberately shaped like HistorySyncStrip, the other thing that appears above
 * the list to report on something running in the background.
 */
Controls.Frame {
    id: root

    readonly property bool active: Whatevr.AudioPlayer.messageId.length > 0
    readonly property bool isVoice: Whatevr.AudioPlayer.isVoice
    readonly property real totalSeconds: Whatevr.AudioPlayer.duration
    readonly property real progress: totalSeconds > 0
        ? Math.min(1, Math.max(0, Whatevr.AudioPlayer.position / totalSeconds))
        : 0

    readonly property string titleText: isVoice || Whatevr.AudioPlayer.fileName.length === 0
        ? Whatevr.I18n.i18nc("@title now playing", "Voice message")
        : Whatevr.AudioPlayer.fileName
    /// Who it came from and where. In a direct chat the two are the same name,
    /// so only one of them is worth the line.
    readonly property string subtitleText: {
        const sender = Whatevr.AudioPlayer.senderName
        const chat = Whatevr.AudioPlayer.chatName
        if (sender.length === 0 || sender === chat)
            return chat
        return Whatevr.I18n.i18nc("@info sender and chat of the playing message", "%1 · %2", sender, chat)
    }

    /// Asked for when the strip is clicked: open the chat this came from and
    /// scroll to the message.
    signal revealRequested(string chatId, string messageId)

    function formatTime(seconds) {
        const whole = Math.max(0, Math.floor(seconds))
        const minutes = Math.floor(whole / 60)
        const rest = whole % 60
        return minutes + ":" + (rest < 10 ? "0" : "") + rest
    }

    visible: active
    Layout.fillWidth: true
    padding: Kirigami.Units.smallSpacing

    background: Rectangle {
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Kirigami.Theme.highlightColor, 0.08)
        border.color: Qt.alpha(Kirigami.Theme.highlightColor, 0.18)
    }

    // The played receipt lives here rather than on the bubble: a note that
    // starts while its chat is closed has no bubble to send it, and this is the
    // one thing on screen for every note that plays. Repeat calls and non-voice
    // kinds are no-ops on the daemon.
    Connections {
        target: Whatevr.AudioPlayer

        function onStarted(messageId) {
            if (Whatevr.AudioPlayer.isVoice && !Whatevr.AudioPlayer.isOutgoing)
                Whatevr.ProtocolController.markMessagePlayed(messageId)
        }
    }

    contentItem: RowLayout {
        spacing: Kirigami.Units.smallSpacing

        AvatarImage {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 2.1
            implicitHeight: implicitWidth
            avatarLocalPath: Whatevr.AudioPlayer.avatarPath
            initials: {
                const name = (Whatevr.AudioPlayer.senderName.length > 0
                    ? Whatevr.AudioPlayer.senderName
                    : Whatevr.AudioPlayer.chatName).trim()
                return name.length > 0 ? name.charAt(0).toUpperCase() : "?"
            }
        }

        // Round for a voice note, squared off for a file: the same shape
        // language the two bubbles use, so the strip names its kind without
        // spelling it out.
        Controls.AbstractButton {
            id: playButton

            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 2.1
            implicitHeight: implicitWidth
            hoverEnabled: true
            text: Whatevr.AudioPlayer.playing
                ? Whatevr.I18n.i18nc("@action:button", "Pause")
                : Whatevr.I18n.i18nc("@action:button", "Play")
            Accessible.name: text
            Controls.ToolTip.text: text
            Controls.ToolTip.visible: hovered
            Controls.ToolTip.delay: Kirigami.Units.toolTipDelay
            onClicked: Whatevr.AudioPlayer.togglePlayPause()

            background: Rectangle {
                radius: root.isVoice ? width / 2 : Kirigami.Units.cornerRadius
                color: Qt.alpha(Kirigami.Theme.highlightColor, playButton.hovered ? 0.30 : 0.18)
            }

            contentItem: Kirigami.Icon {
                source: Whatevr.AudioPlayer.playing
                    ? "media-playback-pause-symbolic"
                    : "media-playback-start-symbolic"
                color: Kirigami.Theme.highlightColor
                isMask: true
                implicitWidth: Kirigami.Units.iconSizes.smallMedium
                implicitHeight: Kirigami.Units.iconSizes.smallMedium
            }
        }

        ColumnLayout {
            Layout.fillWidth: true
            spacing: 0

            // Only the text block takes the click through to the chat. The
            // scrub control below owns its own taps, and running both off one
            // handler made every attempt to seek also change chats.
            Item {
                Layout.fillWidth: true
                implicitHeight: labels.implicitHeight

                ColumnLayout {
                    id: labels

                    anchors.left: parent.left
                    anchors.right: parent.right
                    spacing: 0

                    Controls.Label {
                        Layout.fillWidth: true
                        text: root.titleText
                        font.weight: Font.DemiBold
                        elide: Text.ElideMiddle
                        maximumLineCount: 1
                    }

                    Controls.Label {
                        Layout.fillWidth: true
                        text: root.subtitleText.length > 0
                            ? Whatevr.I18n.i18nc("@info playing message, position and origin", "%1 / %2 · %3",
                                                 root.formatTime(Whatevr.AudioPlayer.position),
                                                 root.formatTime(root.totalSeconds),
                                                 root.subtitleText)
                            : root.formatTime(Whatevr.AudioPlayer.position) + " / " + root.formatTime(root.totalSeconds)
                        color: Kirigami.Theme.disabledTextColor
                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                        elide: Text.ElideRight
                        maximumLineCount: 1
                    }
                }

                TapHandler {
                    onTapped: root.revealRequested(Whatevr.AudioPlayer.chatId, Whatevr.AudioPlayer.messageId)
                }

                HoverHandler {
                    cursorShape: Qt.PointingHandCursor
                }
            }

            // The same two scrub controls the bubbles use, chosen the same way:
            // a recording gets its waveform, a track gets a plain line.
            Loader {
                Layout.fillWidth: true
                Layout.topMargin: Kirigami.Units.smallSpacing / 2
                sourceComponent: root.isVoice ? waveformScrub : lineScrub
            }
        }

        Controls.AbstractButton {
            id: speedPill

            Layout.alignment: Qt.AlignVCenter
            hoverEnabled: true
            implicitWidth: speedLabel.implicitWidth + Kirigami.Units.smallSpacing * 2
            implicitHeight: speedLabel.implicitHeight + Kirigami.Units.smallSpacing / 2
            text: Whatevr.I18n.i18nc("@action:button", "Playback speed")
            Accessible.name: text
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

        Controls.ToolButton {
            Layout.alignment: Qt.AlignTop
            icon.name: "window-close-symbolic"
            display: Controls.AbstractButton.IconOnly
            text: Whatevr.I18n.i18nc("@action:button stop playback and hide the now-playing strip", "Dismiss")
            Accessible.name: text
            Controls.ToolTip.text: text
            Controls.ToolTip.visible: hovered
            Controls.ToolTip.delay: Kirigami.Units.toolTipDelay
            onClicked: Whatevr.AudioPlayer.stop()
        }
    }

    Component {
        id: waveformScrub

        Whatevr.Waveform {
            id: barWaveform

            implicitHeight: Kirigami.Units.gridUnit
            values: Whatevr.AudioPlayer.waveform.length > 0 ? Whatevr.AudioPlayer.waveform : flatWaveform
            progress: root.progress
            playedColor: Kirigami.Theme.highlightColor
            pendingColor: Qt.alpha(Kirigami.Theme.textColor, 0.28)
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

            function scrubTo(x) {
                if (root.totalSeconds > 0)
                    Whatevr.AudioPlayer.seek(barWaveform.fractionAt(x) * root.totalSeconds)
            }

            TapHandler {
                onTapped: eventPoint => barWaveform.scrubTo(eventPoint.position.x)
            }

            DragHandler {
                target: null
                xAxis.enabled: true
                yAxis.enabled: false
                onCentroidChanged: {
                    if (active)
                        barWaveform.scrubTo(centroid.position.x)
                }
            }
        }
    }

    Component {
        id: lineScrub

        AudioSeekLine {
            progress: root.progress
            playedColor: Kirigami.Theme.highlightColor
            onScrubbed: fraction => {
                if (root.totalSeconds > 0)
                    Whatevr.AudioPlayer.seek(fraction * root.totalSeconds)
            }
        }
    }
}
