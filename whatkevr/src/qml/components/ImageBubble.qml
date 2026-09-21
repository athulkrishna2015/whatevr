pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import org.kde.kirigami as Kirigami
import Whatevr as Whatevr

// The image stack: the plate, the blur-up thumbnail, the full-resolution
// picture sampled through a rounded shader, and the chrome that covers all
// three while there is nothing to look at.
//
// Split out of ChatBubble's media slot so the slot can be one Loader for the
// whole media family rather than one per kind. Inline, each kind's component
// was an object on every delegate in the chat, so a text row was carrying a
// picture, a player, an audio row and a document around unbuilt.
//
// `row` is the owning ChatBubble, the same one typed back-reference every other
// bubble in this family takes.
Item {
    id: imageBubble

    required property ChatBubble row

    anchors.fill: parent

    // Reported up so the footer knows whether it is sitting on a picture or on
    // an empty plate. A Binding rather than an assignment because it is
    // released with the stack, which is exactly when the answer goes back to
    // false.
    Binding {
        target: imageBubble.row
        property: "mediaArtworkShown"
        restoreMode: Binding.RestoreBindingOrValue
        value: (imageBubble.row.hasLocalImage && img.status === Image.Ready)
               || (imageBubble.row.hasThumbnailImage && thumb.status === Image.Ready)
    }

    Kirigami.ShadowedRectangle {
        id: mediaBackground

        anchors.fill: parent
        corners.topLeftRadius: imageBubble.row.mediaTopLeftRadius
        corners.topRightRadius: imageBubble.row.mediaTopRightRadius
        corners.bottomLeftRadius: imageBubble.row.mediaBottomLeftRadius
        corners.bottomRightRadius: imageBubble.row.mediaBottomRightRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    // Low-resolution placeholder, drawn with rounded corners in a single shader
    // pass. The tiny decode cap upscales into the blur-up look without a blur
    // shader.
    RoundedImage {
        id: roundedThumb

        anchors.fill: parent
        // Stay up as the blur-up placeholder until the full image has decoded,
        // so a fast fling (which holds off the full-res decode) always has the
        // cheap thumbnail to show.
        //
        // Held until the full image is *fully* opaque, not until it reports
        // Ready: the image below fades in over shortDuration, so cutting on
        // Ready left the bubble showing its empty plate for the whole of that
        // fade. That is the blink a completed download used to end with.
        //
        // Latched from the fade's end rather than bound to the opacity, which
        // re-evaluated this per animation frame and dropped the thumbnail for
        // good; reset on delegate reuse so the next decode has its blur-up
        // again.
        property bool fullImageShown: false
        visible: imageBubble.row.hasThumbnailImage
                 && (!imageBubble.row.hasLocalImage || !fullImageShown)
        opacity: thumb.status === Image.Ready ? 0.78 : 0
        source: thumb
        topLeftRadius: imageBubble.row.mediaTopLeftRadius
        topRightRadius: imageBubble.row.mediaTopRightRadius
        bottomRightRadius: imageBubble.row.mediaBottomRightRadius
        bottomLeftRadius: imageBubble.row.mediaBottomLeftRadius

        Image {
            id: thumb

            anchors.fill: parent
            visible: false
            source: imageBubble.row.mediaSourceActive && imageBubble.row.hasThumbnailImage
                    && !roundedThumb.fullImageShown
                    ? Whatevr.ProtocolController.localFileUrl(imageBubble.row.mediaThumbnailLocalPath) : ""
            asynchronous: true
            cache: true
            sourceSize.width: imageBubble.row.thumbnailDecodeWidth
            sourceSize.height: imageBubble.row.thumbnailDecodeHeight
        }

        Behavior on opacity {
            NumberAnimation {
                duration: Kirigami.Units.shortDuration
                easing.type: Easing.OutCubic
            }
        }
    }

    // Full-resolution image. Sampled straight from the (hidden) Image texture
    // provider, so there is no layer/mask/FBO to allocate or tear down as the
    // delegate scrolls through the viewport.
    RoundedImage {
        id: roundedImg

        anchors.fill: parent
        visible: imageBubble.row.hasLocalImage
        opacity: img.status === Image.Ready ? 1 : 0
        onOpacityChanged: {
            if (opacity >= 1 && img.status === Image.Ready) {
                roundedThumb.fullImageShown = true
            }
        }
        source: img
        topLeftRadius: imageBubble.row.mediaTopLeftRadius
        topRightRadius: imageBubble.row.mediaTopRightRadius
        bottomRightRadius: imageBubble.row.mediaBottomRightRadius
        bottomLeftRadius: imageBubble.row.mediaBottomLeftRadius

        Image {
            id: img

            // Latched readiness, set from onStatusChanged rather than read off
            // `status` inside the source binding (which would make source
            // depend on its own load state and loop). Reset when the underlying
            // file changes on delegate reuse.
            property bool everDecoded: false
            readonly property string targetPath: imageBubble.row.mediaLocalPath
            onTargetPathChanged: {
                everDecoded = false
                roundedThumb.fullImageShown = false
            }
            onStatusChanged: if (status === Image.Ready) everDecoded = true

            anchors.fill: parent
            visible: false
            // Hold the full-res decode while flinging (unless it is already
            // decoded), letting the thumbnail carry the scroll.
            source: imageBubble.row.mediaSourceActive && imageBubble.row.hasLocalImage
                    && (!imageBubble.row.fastFlicking || img.everDecoded)
                    ? Whatevr.ProtocolController.localFileUrl(imageBubble.row.mediaLocalPath) : ""
            asynchronous: true
            cache: true
            sourceSize.width: imageBubble.row.imageDecodeWidth
            sourceSize.height: imageBubble.row.imageDecodeHeight
        }

        Behavior on opacity {
            NumberAnimation {
                duration: Kirigami.Units.shortDuration
                easing.type: Easing.OutCubic
            }
        }

        // Click-to-open lightbox. SingleTap is made exclusive with DoubleTap so
        // double-tap-to-reply on the photo still wins.
        TapHandler {
            acceptedButtons: Qt.LeftButton
            enabled: imageBubble.row.hasLocalImage && !imageBubble.row.isSticker
                     && !imageBubble.row.selectionModeActive
            exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
            onSingleTapped: imageBubble.row.imageActivated(imageBubble.row.messageId,
                                                          imageBubble.row.mediaLocalPath)
        }

        HoverHandler {
            enabled: imageBubble.row.hasLocalImage && !imageBubble.row.selectionModeActive
            cursorShape: Qt.PointingHandCursor
        }
    }

    Item {
        id: imageOverlay

        anchors.fill: parent

        // A decode in progress is not, by itself, a reason to cover the bubble:
        // the thumbnail below is already showing the picture. Only a row with
        // nothing to look at, an active download, or a failure gets chrome, so
        // an ordinary decode (including a re-decode after a fling settles) no
        // longer darkens and un-darkens the image.
        readonly property bool hasPicture: imageBubble.row.hasThumbnailImage
                                           && thumb.status === Image.Ready
        visible: !imageBubble.row.hasLocalImage
                 || imageBubble.row.mediaDownloading
                 || thumb.status === Image.Loading
                 || (img.status === Image.Loading && !hasPicture)
                 || img.status === Image.Error
                 || imageBubble.row.mediaDownloadError.length > 0

        Kirigami.ShadowedRectangle {
            anchors.fill: parent
            corners.topLeftRadius: imageBubble.row.mediaTopLeftRadius
            corners.topRightRadius: imageBubble.row.mediaTopRightRadius
            corners.bottomLeftRadius: imageBubble.row.mediaBottomLeftRadius
            corners.bottomRightRadius: imageBubble.row.mediaBottomRightRadius
            color: Qt.alpha(Kirigami.Theme.backgroundColor,
                            imageBubble.row.hasLocalImage || imageBubble.row.hasThumbnailImage ? 0.34 : 0.0)
        }

        Column {
            anchors.centerIn: parent
            width: Math.max(0, parent.width - Kirigami.Units.largeSpacing * 2)
            spacing: Kirigami.Units.smallSpacing

            BusyIndicator {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: (imageBubble.row.mediaDownloading && imageBubble.row.mediaDownloadProgress < 0)
                         || (!imageBubble.row.mediaDownloading && !imageBubble.row.hasLocalImage
                             && imageBubble.row.hasThumbnailImage && thumb.status === Image.Loading)
                         || (imageBubble.row.hasLocalImage && img.status === Image.Loading)
                running: visible
                implicitWidth: Kirigami.Units.gridUnit * 2
                implicitHeight: Kirigami.Units.gridUnit * 2
            }

            ProgressCircle {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: imageBubble.row.mediaDownloading && imageBubble.row.mediaDownloadProgress >= 0
                progress: Math.max(0, imageBubble.row.mediaDownloadProgress)
                width: Kirigami.Units.gridUnit * 2
                height: width
            }

            Button {
                anchors.horizontalCenter: parent.horizontalCenter
                visible: !imageBubble.row.hasLocalImage || imageBubble.row.mediaDownloading
                icon.name: imageBubble.row.mediaDownloading
                           ? "process-stop-symbolic"
                           : "folder-download-symbolic"
                text: imageBubble.row.mediaDownloading
                       ? Whatevr.I18n.i18nc("@action:button", "Cancel")
                       : Whatevr.I18n.i18nc("@action:button", "Load image")
                enabled: imageBubble.row.messageId.length > 0
                onClicked: {
                    if (imageBubble.row.mediaDownloading)
                        Whatevr.ProtocolController.cancelMessageMediaDownload(imageBubble.row.messageId)
                    else
                        Whatevr.ProtocolController.downloadMessageMedia(imageBubble.row.messageId)
                    imageBubble.row.conversationFocusRequested()
                }
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                width: parent.width
                visible: img.status === Image.Error && imageBubble.row.hasLocalImage
                text: Whatevr.I18n.i18nc("@info", "Image could not be displayed")
                color: Kirigami.Theme.negativeTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
            }

            Label {
                anchors.horizontalCenter: parent.horizontalCenter
                width: parent.width
                visible: !imageBubble.row.mediaDownloading && imageBubble.row.mediaDownloadError.length > 0
                text: imageBubble.row.mediaDownloadError
                color: Kirigami.Theme.negativeTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                wrapMode: Text.Wrap
                horizontalAlignment: Text.AlignHCenter
            }
        }
    }
}
