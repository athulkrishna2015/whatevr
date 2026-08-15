// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick

import Whatevr as Whatevr

/**
 * The playback state and commands shared by the two audio rows, VoiceBubble and
 * AudioFileBubble. They look nothing alike, but a voice note and a shared track
 * behave identically: the same one player, the same resume point, the same
 * download-then-play button.
 *
 * There is no player here either. Both rows bind to the one AudioPlayer
 * singleton through this and compare messageId, so exactly one thing plays at a
 * time and scrolling a playing row out of view and back costs nothing.
 */
QtObject {
    id: root

    /// The ChatBubble this belongs to, for the message fields and download state.
    required property ChatBubble row

    readonly property bool isCurrent: Whatevr.AudioPlayer.messageId === row.messageId
    readonly property bool isPlaying: isCurrent && Whatevr.AudioPlayer.playing
    readonly property real totalSeconds: {
        if (isCurrent && Whatevr.AudioPlayer.duration > 0)
            return Whatevr.AudioPlayer.duration
        return row.mediaDurationSecs
    }
    readonly property real elapsedSeconds: isCurrent ? Whatevr.AudioPlayer.position : rememberedSeconds

    /// A note stopped halfway keeps its place, so the bar does not lie about
    /// where you left off. Assigned rather than bound: resumePosition() is a
    /// plain call with nothing to notify on, so a binding through it went
    /// stale the moment another row took the player.
    property real rememberedSeconds: 0

    readonly property real progress: totalSeconds > 0
        ? Math.min(1, Math.max(0, elapsedSeconds / totalSeconds))
        : 0
    readonly property bool hasFile: row.mediaLocalPath.length > 0
    readonly property bool canScrub: hasFile
                                     && totalSeconds > 0
                                     && Whatevr.AudioPlayer.available
                                     && !row.selectionModeActive
    readonly property bool available: Whatevr.AudioPlayer.available

    function refreshRemembered() {
        rememberedSeconds = Whatevr.AudioPlayer.resumePosition(row.messageId)
    }

    /// The now-playing snapshot handed to the player. The row that started a
    /// note is gone the moment its chat closes, so the player keeps its own
    /// copy of who and what is playing rather than looking it back up.
    function nowPlayingContext() {
        return {
            "chat_id": Whatevr.ProtocolController.selectedChatId,
            "chat_name": Whatevr.ProtocolController.selectedChatName,
            "sender_name": row.isOutgoing ? "" : row.senderName,
            "avatar_path": row.isOutgoing ? "" : row.senderAvatarLocalPath,
            "file_name": row.mediaFileName,
            "is_voice": row.isVoice,
            "is_outgoing": row.isOutgoing,
            "waveform": row.mediaWaveform ? row.mediaWaveform : []
        }
    }

    /// Seeks to a 0-1 position along the row's scrub control, starting this
    /// message first when something else (or nothing) is playing.
    function scrubTo(fraction) {
        if (!isCurrent) {
            Whatevr.AudioPlayer.play(row.messageId,
                                     Whatevr.ProtocolController.localFileUrl(row.mediaLocalPath),
                                     row.mediaDurationSecs,
                                     nowPlayingContext())
        }
        Whatevr.AudioPlayer.seek(fraction * totalSeconds)
    }

    function formatTime(seconds) {
        const whole = Math.max(0, Math.floor(seconds))
        const minutes = Math.floor(whole / 60)
        const rest = whole % 60
        return minutes + ":" + (rest < 10 ? "0" : "") + rest
    }

    /// Plays from the local file when it exists, otherwise fetches it first.
    function activate() {
        if (row.messageId.length === 0)
            return
        if (hasFile) {
            Whatevr.AudioPlayer.toggle(row.messageId,
                                       Whatevr.ProtocolController.localFileUrl(row.mediaLocalPath),
                                       row.mediaDurationSecs,
                                       nowPlayingContext())
            return
        }
        if (row.mediaDownloading)
            return
        Whatevr.ProtocolController.downloadMessageMedia(row.messageId)
    }

    property list<QtObject> watchers: [
        Connections {
            target: Whatevr.AudioPlayer

            function onResumePositionChanged(messageId) {
                if (messageId === root.row.messageId)
                    root.refreshRemembered()
            }
        },
        Connections {
            target: root.row

            function onMessageIdChanged() {
                root.refreshRemembered()
            }
        }
    ]

    Component.onCompleted: refreshRemembered()
}
