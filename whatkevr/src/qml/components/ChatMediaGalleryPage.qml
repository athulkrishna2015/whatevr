pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

// Every photo, video, voice note, link and document in one chat, newest first.
//
// The rows are the same `messages` items the conversation renders, delivered
// through the `chat_media` and `chat_links` views, so a thumbnail here is the
// same file the bubble shows and a download landing updates both at once.
Kirigami.ScrollablePage {
    id: root

    required property string chatId
    property string chatName: ""
    property string mediaFilter: ""
    property bool linksVisible: false

    title: chatName.length > 0
        ? Whatevr.I18n.i18nc("@title:window", "Media, links and documents in %1", chatName)
        : Whatevr.I18n.i18nc("@title:window", "Media, links and documents")

    Component.onCompleted: {
        Whatevr.ProtocolController.openChatMedia(chatId)
        Whatevr.ProtocolController.openChatLinks(chatId)
    }
    // Guarded: at engine teardown the singleton may already be null.
    Component.onDestruction: {
        const c = Whatevr.ProtocolController
        if (c) {
            c.closeChatMedia()
            c.closeChatLinks()
        }
    }

    function applyFilter(kind) {
        mediaFilter = kind
        linksVisible = false
        Whatevr.ProtocolController.closeChatMedia()
        Whatevr.ProtocolController.openChatMedia(chatId, kind)
    }

    header: Row {
        spacing: Kirigami.Units.smallSpacing
        padding: Kirigami.Units.smallSpacing
        leftPadding: Kirigami.Units.largeSpacing

        Repeater {
            model: [
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "All"),    kind: "" },
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "Photos"), kind: "image" },
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "Videos"), kind: "video" },
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "Voice"),  kind: "voice" },
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "Audio"),  kind: "audio" },
                { label: Whatevr.I18n.i18nc("@action:button gallery filter", "Docs"),   kind: "document" }
            ]

            delegate: QQC2.ToolButton {
                required property var modelData
                text: modelData.label
                checked: !root.linksVisible && root.mediaFilter === modelData.kind
                checkable: true
                autoExclusive: true
                onClicked: root.applyFilter(modelData.kind)
                font.weight: checked ? Font.DemiBold : Font.Normal
            }
        }

        QQC2.ToolButton {
            text: Whatevr.I18n.i18nc("@action:button gallery links", "Links")
            checked: root.linksVisible
            checkable: true
            autoExclusive: true
            onClicked: root.linksVisible = !root.linksVisible
            font.weight: checked ? Font.DemiBold : Font.Normal
        }
    }

    GridView {
        id: grid

        visible: !root.linksVisible
        readonly property int columns: Math.max(2, Math.floor(width / (Kirigami.Units.gridUnit * 7)))

        model: Whatevr.ProtocolController.chatMediaModel
        cellWidth: Math.floor(width / columns)
        cellHeight: cellWidth
        cacheBuffer: cellHeight * 2

        // The grid was mouse-only: no way to reach a tile from the keyboard and
        // nothing on screen to say which one was current.
        focus: true
        keyNavigationEnabled: true
        activeFocusOnTab: true
        highlightMoveDuration: Kirigami.Units.shortDuration
        highlight: Rectangle {
            color: "transparent"
            radius: Kirigami.Units.cornerRadius
            border.width: 2
            border.color: Kirigami.Theme.highlightColor
        }


        // The window grows into older media as the grid nears its end, the
        // same "extend older" a live-edge window always uses.
        onContentYChanged: {
            if (root.linksVisible || contentHeight <= 0 || Whatevr.ProtocolController.chatMediaExhausted)
                return
            if (contentY + height > contentHeight - cellHeight * 2)
                Whatevr.ProtocolController.extendChatMedia(60)
        }

        delegate: Item {
            id: cell

            required property int index
            required property var model

            readonly property var item: model.item ?? ({})
            readonly property var media: item.media ?? ({})
            readonly property string kind: item.kind ?? ""
            readonly property string thumbnailPath: media.thumbnail_path ?? ""
            readonly property string localPath: media.path ?? ""
            readonly property bool isVisual: kind === "image" || kind === "video" || kind === "gif" || kind === "video_note"

            width: grid.cellWidth
            height: grid.cellHeight

            // A GridView hands focus to whichever delegate declares it, which
            // is what puts the key handlers below on the current tile. Gated
            // on being the current item; an unconditional `true` handed focus
            // to whichever tile was instantiated last during a scroll.
            focus: GridView.isCurrentItem
            Keys.onReturnPressed: cell.activate()
            Keys.onEnterPressed: cell.activate()
            Keys.onSpacePressed: cell.activate()

            /// Opening a tile: fetch it if we do not have it, otherwise show it
            /// the way its kind wants to be shown. Shared by the tap handler
            /// and the keyboard, so the two can never drift apart.
            function activate() {
                if (cell.localPath.length === 0) {
                    Whatevr.ProtocolController.downloadMessageMedia(cell.item.id ?? "")
                    return
                }
                if (cell.kind === "image") {
                    galleryViewer.showImage(cell.localPath)
                } else if (cell.isVisual) {
                    galleryViewer.showVideo(cell.item.id ?? "", cell.localPath, "", "", cell.kind,
                                            cell.media.duration_secs ?? 0,
                                            Whatevr.VideoPlayback.resumePosition(cell.item.id ?? ""))
                } else {
                    Whatevr.ProtocolController.openLocalFile(cell.localPath)
                }
            }

            Accessible.role: Accessible.Button
            Accessible.name: {
                if (cell.item.fallback && cell.item.fallback.length > 0)
                    return cell.item.fallback
                switch (cell.kind) {
                case "image":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Photo")
                case "video":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Video")
                case "gif":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "GIF")
                case "video_note":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Instant video")
                case "voice":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Voice message")
                case "audio":
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Audio")
                default:
                    return Whatevr.I18n.i18nc("@info:whatsthis gallery tile", "Document")
                }
            }
            Accessible.onPressAction: cell.activate()

            Rectangle {
                anchors.fill: parent
                anchors.margins: 1
                color: Qt.alpha(Kirigami.Theme.textColor, 0.06)

                Image {
                    anchors.fill: parent
                    visible: cell.isVisual && source.toString().length > 0
                    source: {
                        if (cell.localPath.length > 0 && cell.kind === "image")
                            return Whatevr.ProtocolController.localFileUrl(cell.localPath)
                        if (cell.thumbnailPath.length > 0)
                            return Whatevr.ProtocolController.localFileUrl(cell.thumbnailPath)
                        return ""
                    }
                    fillMode: Image.PreserveAspectCrop
                    asynchronous: true
                    cache: true
                    sourceSize.width: grid.cellWidth
                    sourceSize.height: grid.cellHeight
                }

                // Non-visual kinds (voice notes, audio, documents) get a glyph
                // and a label rather than a blank tile.
                ColumnLayout {
                    anchors.centerIn: parent
                    width: parent.width - Kirigami.Units.smallSpacing * 2
                    visible: !cell.isVisual
                    spacing: Kirigami.Units.smallSpacing

                    Kirigami.Icon {
                        Layout.alignment: Qt.AlignHCenter
                        implicitWidth: Kirigami.Units.iconSizes.large
                        implicitHeight: Kirigami.Units.iconSizes.large
                        source: {
                            if (cell.kind === "voice")
                                return "audio-input-microphone-symbolic"
                            if (cell.kind === "audio")
                                return "audio-x-generic"
                            if (cell.kind === "poll")
                                return "view-list-symbolic"
                            if (cell.kind === "contact")
                                return "im-user-symbolic"
                            if (cell.kind === "location")
                                return "mark-location-symbolic"
                            return "text-x-generic"
                        }
                    }

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: cell.item.fallback ?? ""
                        horizontalAlignment: Text.AlignHCenter
                        elide: Text.ElideRight
                        maximumLineCount: 2
                        wrapMode: Text.Wrap
                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                    }
                }

                Kirigami.Icon {
                    anchors.centerIn: parent
                    visible: cell.kind === "video" || cell.kind === "gif" || cell.kind === "video_note"
                    width: Kirigami.Units.iconSizes.medium
                    height: width
                    source: "media-playback-start-symbolic"
                    color: "white"
                }

                TapHandler {
                    acceptedButtons: Qt.LeftButton | Qt.RightButton
                    onTapped: {
                        if (point.device && point.device.type === PointerDevice.Mouse
                                && point.button === Qt.RightButton) {
                            galleryContextMenu.popup()
                            return
                        }
                        // Move the keyboard's idea of "current" to whatever was
                        // clicked, so arrowing on from here starts in the right
                        // place.
                        grid.currentIndex = cell.index
                        cell.activate()
                    }
                }

                QQC2.Menu {
                    id: galleryContextMenu

                    QQC2.MenuItem {
                        text: Whatevr.I18n.i18nc("@action:menu save gallery media", "Save as…")
                        icon.name: "document-save-symbolic"
                        enabled: cell.localPath.length > 0
                        onTriggered: Whatevr.ProtocolController.openLocalFile(cell.localPath)
                    }
                    QQC2.MenuItem {
                        text: Whatevr.I18n.i18nc("@action:menu copy gallery filename", "Copy path")
                        icon.name: "edit-copy-symbolic"
                        enabled: cell.localPath.length > 0
                        onTriggered: Whatevr.ProtocolController.copyToClipboard(cell.localPath)
                    }
                }
            }
        }
    }

    ListView {
        id: linksList

        visible: root.linksVisible
        model: Whatevr.ProtocolController.chatLinksModel
        currentIndex: -1
        reuseItems: true
        spacing: Kirigami.Units.smallSpacing

        onContentYChanged: {
            if (!root.linksVisible || contentHeight <= 0 || Whatevr.ProtocolController.chatLinksExhausted)
                return
            if (contentY + height > contentHeight - Kirigami.Units.gridUnit * 2)
                Whatevr.ProtocolController.extendChatLinks(60)
        }

        Component.onDestruction: linksList.model = null

        QQC2.BusyIndicator {
            anchors.centerIn: parent
            running: Whatevr.ProtocolController.chatLinksLoading && linksList.count === 0
            visible: running
        }

        Kirigami.PlaceholderMessage {
            anchors.centerIn: parent
            width: parent.width - Kirigami.Units.gridUnit * 4
            visible: linksList.count === 0 && !Whatevr.ProtocolController.chatLinksLoading
            icon.name: "applications-internet-symbolic"
            text: Whatevr.I18n.i18nc("@info:placeholder", "No links in this chat yet")
        }

        delegate: QQC2.ItemDelegate {
            id: linkDelegate

            required property var item
            readonly property var row: Whatevr.ProtocolController.messageRowDisplay(item)
            readonly property var preview: item.link_preview ?? ({})
            readonly property string url: String(preview.url ?? "")

            width: ListView.view.width
            padding: Kirigami.Units.largeSpacing

            contentItem: ColumnLayout {
                spacing: Kirigami.Units.smallSpacing / 2

                RowLayout {
                    Layout.fillWidth: true
                    spacing: Kirigami.Units.smallSpacing

                    QQC2.Label {
                        Layout.fillWidth: true
                        text: linkDelegate.row.senderName
                        font.weight: Font.DemiBold
                        elide: Text.ElideRight
                    }

                    QQC2.Label {
                        text: linkDelegate.row.timeText
                        color: Kirigami.Theme.disabledTextColor
                        font: Kirigami.Theme.smallFont
                    }
                }

                QQC2.Label {
                    Layout.fillWidth: true
                    text: linkDelegate.row.preview
                    wrapMode: Text.Wrap
                    maximumLineCount: 3
                    elide: Text.ElideRight
                }

                QQC2.Label {
                    Layout.fillWidth: true
                    text: linkDelegate.url
                    visible: linkDelegate.url.length > 0
                    color: Kirigami.Theme.linkColor
                    elide: Text.ElideRight
                }
            }

            Accessible.role: Accessible.Button
            Accessible.name: linkDelegate.row.preview

            onClicked: {
                if (linkDelegate.url.length > 0)
                    Qt.openUrlExternally(linkDelegate.url)
                else
                    Whatevr.ProtocolController.showMessageInChat(root.chatId, linkDelegate.item.id)
            }
        }
    }

    MediaViewer {
        id: galleryViewer
    }

    QQC2.BusyIndicator {
        anchors.centerIn: parent
        running: !root.linksVisible && Whatevr.ProtocolController.chatMediaLoading
        visible: running
    }

    Kirigami.PlaceholderMessage {
        anchors.centerIn: parent
        width: parent.width - Kirigami.Units.gridUnit * 4
        visible: !root.linksVisible && !Whatevr.ProtocolController.chatMediaLoading && grid.count === 0
        icon.name: "folder-images-symbolic"
        text: Whatevr.I18n.i18nc("@info:placeholder", "No media in this chat yet")
    }
}
