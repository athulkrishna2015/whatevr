pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// Channel messages page: a read-only broadcast timeline for one channel.
// Subscribes the `channel_messages` view while on screen.
Kirigami.ScrollablePage {
    id: root

    required property string channelJid
    required property string channelName

    title: root.channelName

    Kirigami.Theme.colorSet: Kirigami.Theme.View

    Component.onCompleted: {
        Whatevr.ProtocolController.openChannelMessages(root.channelJid, root.channelName)
        Qt.callLater(root.markVisibleViewed)
    }
    // Guarded: at engine teardown the singleton may already be null.
    Component.onDestruction: { const c = Whatevr.ProtocolController; if (c) c.closeChannelMessages() }

    property string lastMarkedKey: ""

    onChannelJidChanged: root.lastMarkedKey = ""

    // Read-more expansion, keyed by message id. The revision keeps the
    // per-row `textExpanded` bindings live without rebuilding the map.
    property var expandedIds: ({})
    property int expandRevision: 0

    function toggleExpanded(messageId) {
        const expanded = Object.assign({}, root.expandedIds)
        if (expanded[messageId]) {
            delete expanded[messageId]
        } else {
            expanded[messageId] = true
        }
        root.expandedIds = expanded
        root.expandRevision++
    }

    function rowById(messageId) {
        const model = Whatevr.ProtocolController.channelMessagesModel
        if (!model || !messageId) {
            return null
        }
        return model.itemById(messageId)
    }

    function serverIdOf(messageId) {
        const row = root.rowById(messageId)
        const id = row ? Number(row.server_id || 0) : 0
        return id > 0 ? id : 0
    }

    function openChannelImage(messageId, localPath) {
        const row = root.rowById(messageId)
        channelMediaViewer.showImage(localPath, messageId,
                                     row ? String(row.mediaFileName || "") : "",
                                     row ? Number(row.timestamp || 0) : 0)
    }

    function openChannelGallery(albumMessageId, index) {
        // The gallery is only the pictures actually on disk; the bubble
        // behind is where an undownloaded picture is asked for.
        const model = Whatevr.ProtocolController.channelMessagesModel
        const entries = []
        let start = -1
        if (model) {
            for (let i = 0; i < model.count; ++i) {
                const row = model.itemById(model.idAt(i))
                const media = row ? row.media : null
                const path = media ? String(media.path || "") : ""
                if (path.length === 0) {
                    continue
                }
                if (row && row.id === albumMessageId) {
                    start = entries.length
                }
                entries.push({
                    id: row ? String(row.id || "") : "",
                    kind: "image",
                    path: path,
                    fileName: row ? String(row.mediaFileName || "") : "",
                    timestampUnix: row ? Number(row.timestamp || 0) : 0,
                    width: media ? Number(media.width || 0) : 0,
                    height: media ? Number(media.height || 0) : 0,
                    durationSecs: 0,
                })
            }
        }
        if (start >= 0 && entries.length > 1) {
            channelMediaViewer.showGallery(entries, start)
        } else {
            const row = root.rowById(albumMessageId)
            const media = row ? row.media : null
            root.openChannelImage(albumMessageId, media ? String(media.path || "") : "")
        }
    }

    MediaViewer {
        id: channelMediaViewer
    }

    function markVisibleViewed() {
        if (!root.channelJid || root.channelJid.length === 0)
            return
        const model = Whatevr.ProtocolController.channelMessagesModel
        if (!model)
            return
        // ListView has no first/lastVisibleIndex, so the rendered range is
        // anchored on the two ends of the viewport and then widened to cover
        // every delegate that exists, which is the visible rows plus whatever
        // the cache buffer already built.
        const top = messagesList.indexAt(0, 0)
        const bottom = messagesList.indexAt(0, messagesList.height)
        if (top < 0 && bottom < 0)
            return
        let first = Math.min(top, bottom)
        let last = Math.max(top, bottom)
        if (first < 0)
            first = last
        if (last < 0)
            last = first
        while (messagesList.itemAtIndex(first - 1) !== null)
            first--
        while (messagesList.itemAtIndex(last + 1) !== null)
            last++
        const ids = []
        for (let i = first; i <= last; ++i) {
            const delegate = messagesList.itemAtIndex(i)
            if (delegate) {
                const id = root.serverIdOf(delegate.messageId)
                if (id > 0)
                    ids.push(id)
            }
        }
        if (ids.length === 0)
            return
        const key = root.channelJid + ":" + ids.join(",")
        if (key === root.lastMarkedKey)
            return
        root.lastMarkedKey = key
        Whatevr.ProtocolController.markChannelViewed(root.channelJid, ids)
    }

    Connections {
        target: Whatevr.ProtocolController.channelMessagesModel
        ignoreUnknownSignals: true
        function onReadyChanged() {
            if (Whatevr.ProtocolController.channelMessagesModel.ready)
                root.markVisibleViewed()
        }
        function onCountChanged() { root.markVisibleViewed() }
    }

    actions: [
        Kirigami.Action {
            icon.name: "list-remove-symbolic"
            text: Whatevr.I18n.i18nc("@action:button unfollow channel", "Unfollow")
            onTriggered: {
                Whatevr.ProtocolController.unfollowChannel(root.channelJid)
                applicationWindow().pageStack.layers.pop()
            }
        },
        Kirigami.Action {
            icon.name: "audio-volume-muted-symbolic"
            text: Whatevr.I18n.i18nc("@action:button mute/unmute channel", "Mute / Unmute")
            onTriggered: {
                // Toggle: read current mute state from the channel model if
                // available, otherwise default to muting.
                const model = Whatevr.ProtocolController.channelsModel
                let currentlyMuted = false
                if (model) {
                    const row = model.itemById(root.channelJid)
                    if (row) {
                        currentlyMuted = row.muted === true
                    }
                }
                Whatevr.ProtocolController.muteChannel(root.channelJid, !currentlyMuted)
            }
        }
    ]

    ListView {
        id: messagesList

        model: Whatevr.ProtocolController.channelMessagesModel
        currentIndex: -1
        reuseItems: true
        onContentYChanged: root.markVisibleViewed()
        onCountChanged: root.markVisibleViewed()

        verticalLayoutDirection: ListView.BottomToTop
        Component.onDestruction: messagesList.model = null

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.channelMessagesLoading && messagesList.count === 0
            visible: running
        }

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: messagesList.count === 0 && !Whatevr.ProtocolController.channelMessagesLoading
            icon.name: "rss-symbolic"
            text: Whatevr.I18n.i18nc("@info placeholder for channel messages", "No messages yet")
        }

        delegate: ChatBubble {
            id: msgDelegate

            listWidth: messagesList.width
            textExpanded: !!root.expandedIds[messageId] && root.expandRevision >= 0
            readMoreTextWidth: readMoreMetrics.advanceWidth

            ListView.onPooled: pooled = true
            ListView.onReused: pooled = false

            onReadMoreRequested: id => root.toggleExpanded(id)
            onImageActivated: (messageId, localPath) => root.openChannelImage(messageId, localPath)
            onVideoActivated: (messageId, localPath, streamUrl, streamId, kind, durationSecs, startAt) => {
                channelMediaViewer.showVideo(messageId, localPath, streamUrl, streamId, kind,
                                             durationSecs, startAt, "", 0)
            }
            onAlbumItemActivated: (albumMessageId, index) => root.openChannelGallery(albumMessageId, index)
            onContextMenuRequested: (posX, posY) => messageContextMenu.popup(msgDelegate, posX, posY)
            onReactionToggleRequested: emoji => {
                const serverId = root.serverIdOf(msgDelegate.messageId)
                if (serverId > 0) {
                    Whatevr.ProtocolController.reactToChannelMessage(root.channelJid, serverId, emoji)
                }
            }

            QQC2.Menu {
                id: messageContextMenu

                QQC2.MenuItem {
                    text: Whatevr.I18n.i18nc("@action:menu copy channel message", "Copy text")
                    icon.name: "edit-copy-symbolic"
                    enabled: (msgDelegate.text || "").length > 0
                    onTriggered: Whatevr.ProtocolController.copyToClipboard(msgDelegate.text || "")
                }
                QQC2.MenuSeparator {}
                QQC2.MenuItem {
                    text: Whatevr.I18n.i18nc("@action:menu react to channel message", "Like")
                    icon.name: "heart-symbolic"
                    onTriggered: Whatevr.ProtocolController.reactToChannelMessage(
                        root.channelJid, root.serverIdOf(msgDelegate.messageId), "❤️")
                }
                QQC2.MenuItem {
                    text: Whatevr.I18n.i18nc("@action:menu remove channel reaction", "Remove reaction")
                    icon.name: "edit-clear-symbolic"
                    onTriggered: Whatevr.ProtocolController.reactToChannelMessage(
                        root.channelJid, root.serverIdOf(msgDelegate.messageId), "")
                }
            }
        }

        TextMetrics {
            id: readMoreMetrics

            text: Whatevr.I18n.i18nc("@action:button expand long message", "Read more")
            font.pointSize: Kirigami.Theme.smallFont.pointSize
            font.weight: Font.DemiBold
        }
    }
}
