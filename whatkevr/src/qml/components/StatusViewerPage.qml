pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts
import Qt.labs.platform as Platform
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// One contact's statuses, newest first, stepped with back/next. The first
// shown status is marked viewed on arrival (and each one as it appears), so
// the contact's ring clears as you watch. Photo statuses render
// thumbnail-first, then full-bleed once the auto-download lands (a "Load"
// button retries a failed fetch); text statuses render
// centered; anything else shows its fallback line with a Save action.
Kirigami.ScrollablePage {
    id: root

    required property string senderId
    property string senderName: ""

    title: senderName.length > 0 ? senderName : senderId
    Kirigami.Theme.colorSet: Kirigami.Theme.View

    property var statusIds: []
    property int currentIndex: -1

    readonly property var currentItem: currentIndex >= 0 && currentIndex < statusIds.length
        ? Whatevr.ProtocolController.statusModel.itemById(statusIds[currentIndex])
        : null

    function collectStatuses() {
        const model = Whatevr.ProtocolController.statusModel
        const ids = []
        const count = model ? model.count : 0
        for (let i = 0; i < count; ++i) {
            const item = model.itemById(model.idAt(i))
            if (item && item.id && item.sender && item.sender.id === root.senderId) {
                ids.push(item.id)
            }
        }
        // The model is newest-first; the viewer walks oldest first so "next"
        // moves towards the present.
        ids.reverse()
        root.statusIds = ids
        if (root.currentIndex < 0 && ids.length > 0) {
            root.currentIndex = 0
        } else if (root.currentIndex >= ids.length) {
            root.currentIndex = ids.length - 1
        }
    }

    function markCurrentViewed() {
        const item = root.currentItem
        if (item && item.id && !item.viewed) {
            Whatevr.ProtocolController.markStatusViewed(item.id)
        }
    }

    function ensureDownloaded() {
        const item = root.currentItem
        const media = item ? item.media : null
        if (item && media && !media.path) {
            Whatevr.ProtocolController.downloadStatus(item.id)
        }
    }

    Component.onCompleted: {
        root.collectStatuses()
        root.markCurrentViewed()
        root.ensureDownloaded()
    }

    Connections {
        target: Whatevr.ProtocolController

        function onStatusChanged() {
            root.collectStatuses()
            root.markCurrentViewed()
            root.ensureDownloaded()
        }
    }

    onCurrentIndexChanged: {
        root.markCurrentViewed()
        root.ensureDownloaded()
    }

    header: RowLayout {
        width: parent.width

        QQC2.ToolButton {
            icon.name: "go-previous-symbolic"
            text: Whatevr.I18n.i18nc("@action:button previous status", "Previous")
            display: QQC2.AbstractButton.IconOnly
            enabled: root.currentIndex > 0
            onClicked: root.currentIndex -= 1
        }

        QQC2.Label {
            Layout.fillWidth: true
            horizontalAlignment: Text.AlignHCenter
            text: root.statusIds.length > 0
                ? Whatevr.I18n.i18nc("@info status position", "%1 of %2", root.currentIndex + 1, root.statusIds.length)
                : ""
        }

        QQC2.ToolButton {
            icon.name: "go-next-symbolic"
            text: Whatevr.I18n.i18nc("@action:button next status", "Next")
            display: QQC2.AbstractButton.IconOnly
            enabled: root.currentIndex >= 0 && root.currentIndex < root.statusIds.length - 1
            onClicked: root.currentIndex += 1
        }
    }

    ColumnLayout {
        width: parent.width
        spacing: Kirigami.Units.largeSpacing

        // Text status: centered large label.
        QQC2.Label {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.gridUnit * 2
            visible: root.currentItem && root.currentItem.kind === "text"
            text: root.currentItem ? (root.currentItem.text || "") : ""
            wrapMode: Text.WordWrap
            horizontalAlignment: Text.AlignHCenter
            font.pointSize: Kirigami.Theme.defaultFont.pointSize * 1.4
        }

        // Photo status: thumbnail-first — the sender thumbnail cached at
        // ingest renders instantly; the full image replaces it once the
        // auto-download (ensureDownloaded) lands. Load button stays for
        // retrying a failed fetch.
        Image {
            Layout.fillWidth: true
            Layout.preferredHeight: Math.min(implicitHeight > 0 ? implicitHeight : 0, root.height * 0.6)
            readonly property var statusMedia: root.currentItem ? root.currentItem.media : null
            readonly property string fullPath: statusMedia ? (statusMedia.path ?? "") : ""
            readonly property string thumbPath: statusMedia ? (statusMedia.thumbnail_path ?? "") : ""
            visible: root.currentItem && (root.currentItem.kind === "image" || root.currentItem.kind === "gif")
                      && (fullPath.length > 0 || thumbPath.length > 0)
            source: {
                if (fullPath.length > 0) {
                    return Whatevr.ProtocolController.localFileUrl(fullPath)
                }
                if (thumbPath.length > 0) {
                    return Whatevr.ProtocolController.localFileUrl(thumbPath)
                }
                return ""
            }
            fillMode: Image.PreserveAspectFit
            asynchronous: true
        }

        QQC2.Button {
            Layout.alignment: Qt.AlignHCenter
            visible: root.currentItem && root.currentItem.media && !root.currentItem.media.path
            text: Whatevr.I18n.i18nc("@action:button download the status media", "Load")
            icon.name: "cloud-download-symbolic"
            onClicked: root.ensureDownloaded()
        }

        // Video status: thumbnail-first poster with a play button; the full
        // clip plays in the shared MediaViewer popup once downloaded.
        Item {
            Layout.fillWidth: true
            Layout.preferredHeight: root.height * 0.6
            readonly property var videoMedia: root.currentItem ? root.currentItem.media : null
            readonly property string videoPath: videoMedia ? (videoMedia.path ?? "") : ""
            readonly property string videoThumb: videoMedia ? (videoMedia.thumbnail_path ?? "") : ""
            visible: root.currentItem && root.currentItem.kind === "video"
                      && (videoPath.length > 0 || videoThumb.length > 0)

            Image {
                anchors.fill: parent
                source: parent.videoPath.length > 0
                        ? Whatevr.ProtocolController.localFileUrl(parent.videoPath)
                        : (parent.videoThumb.length > 0
                           ? Whatevr.ProtocolController.localFileUrl(parent.videoThumb) : "")
                fillMode: Image.PreserveAspectFit
                asynchronous: true
            }

            QQC2.Button {
                anchors.centerIn: parent
                enabled: parent.videoPath.length > 0
                text: Whatevr.I18n.i18nc("@action:button play the status video", "Play")
                icon.name: "media-playback-start-symbolic"
                onClicked: {
                    const item = root.currentItem
                    statusMediaViewer.showVideo(item.id, parent.videoPath, "", "",
                                                "video", item.media.duration_secs ?? 0, 0,
                                                "", item.timestamp ?? 0)
                }
            }
        }

        // Voice/audio status: play through the shared AudioPlayer singleton
        // (same one the chat bubbles use, so playback is exclusive). The row
        // auto-downloads on open like every other kind.
        RowLayout {
            Layout.alignment: Qt.AlignHCenter
            Layout.topMargin: Kirigami.Units.gridUnit * 2
            readonly property var audioMedia: root.currentItem ? root.currentItem.media : null
            readonly property string audioPath: audioMedia ? (audioMedia.path ?? "") : ""
            readonly property bool isCurrent: Whatevr.AudioPlayer.messageId.length > 0
                                              && root.currentItem
                                              && Whatevr.AudioPlayer.messageId === root.currentItem.id
            readonly property real totalSecs: isCurrent && Whatevr.AudioPlayer.duration > 0
                                              ? Whatevr.AudioPlayer.duration
                                              : (root.currentItem && root.currentItem.media
                                                 ? (root.currentItem.media.duration_secs ?? 0) : 0)
            readonly property real elapsedSecs: isCurrent ? Whatevr.AudioPlayer.position : 0
            visible: root.currentItem && (root.currentItem.kind === "voice" || root.currentItem.kind === "audio")

            QQC2.Button {
                icon.name: parent.isCurrent && Whatevr.AudioPlayer.playing
                           ? "media-playback-pause-symbolic" : "media-playback-start-symbolic"
                text: Whatevr.I18n.i18nc("@action:button play the status audio", "Play")
                display: QQC2.AbstractButton.IconOnly
                enabled: parent.audioPath.length > 0 && Whatevr.AudioPlayer.available
                onClicked: {
                    if (root.currentItem) {
                        Whatevr.AudioPlayer.toggle(root.currentItem.id,
                                                   Whatevr.ProtocolController.localFileUrl(parent.audioPath),
                                                   parent.totalSecs)
                    }
                }
            }

            QQC2.Label {
                text: MediaFormat.clockTime(parent.elapsedSecs) + " / " + MediaFormat.clockTime(parent.totalSecs)
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                color: Kirigami.Theme.disabledTextColor
            }
        }

        // Anything left (document and the unknown): fallback line + Save.
        QQC2.Label {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.gridUnit * 2
            visible: root.currentItem && root.currentItem.kind !== "text"
                      && root.currentItem.kind !== "image" && root.currentItem.kind !== "gif"
                      && root.currentItem.kind !== "video"
                      && root.currentItem.kind !== "voice" && root.currentItem.kind !== "audio"
            text: root.currentItem ? (root.currentItem.fallback || "") : ""
            wrapMode: Text.WordWrap
            horizontalAlignment: Text.AlignHCenter
            color: Kirigami.Theme.disabledTextColor
        }

        QQC2.Button {
            Layout.alignment: Qt.AlignHCenter
            visible: root.currentItem && root.currentItem.kind !== "text"
            text: Whatevr.I18n.i18nc("@action:button save the status media", "Save…")
            icon.name: "document-save-symbolic"
            onClicked: saveDialog.open()
        }

        Kirigami.PlaceholderMessage {
            Layout.fillWidth: true
            visible: root.statusIds.length === 0
            icon.name: "camera-photo-symbolic"
            text: Whatevr.I18n.i18nc("@info placeholder", "No statuses")
            explanation: Whatevr.I18n.i18nc("@info:placeholder", "This contact's statuses expired or were removed.")
        }
    }

    Platform.FileDialog {
        id: saveDialog

        title: Whatevr.I18n.i18nc("@title:window save a status", "Save status")
        fileMode: Platform.FileDialog.SaveFile
        onAccepted: {
            if (root.currentItem) {
                Whatevr.ProtocolController.saveRemoteMedia("", root.currentItem.id, "", file)
            }
        }
    }

    // Full-screen player for downloaded video statuses (shared component,
    // also used by chat bubbles and the media gallery).
    MediaViewer {
        id: statusMediaViewer

        onSaveRequested: (localPath, kind, fileName, timestampUnix) => saveDialog.open()
    }

    Shortcut {
        sequences: [Qt.Key_Left]
        enabled: root.currentIndex > 0
        onActivated: root.currentIndex -= 1
    }

    Shortcut {
        sequences: [Qt.Key_Right]
        enabled: root.currentIndex >= 0 && root.currentIndex < root.statusIds.length - 1
        onActivated: root.currentIndex += 1
    }
}
