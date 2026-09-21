// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A shared place: a map, what it is called, and somewhere to go with it.
 *
 * The map is a real one. The daemon fetches OSM tiles and stitches them, so
 * this is an ordinary downloaded image rather than the ~200px JPEG WhatsApp
 * embeds, and it arrives through the same download lifecycle as a photo (the
 * embedded thumbnail shows instantly, blurred, until the real map lands).
 *
 * The desktop payoff is the handoff: a `geo:` URI goes to whatever the system
 * registered for it, which on a Linux desktop is GNOME Maps, Marble or KDE's
 * own handler, with OpenStreetMap in a browser as the fallback. Nobody else
 * does this; every other client opens a web page.
 *
 * A live share renders in the same card with its state on top: a pulsing dot,
 * how long is left, when it last moved, and how fast. When it ends the card
 * settles into a summary of where the share went instead of a stale pin.
 */
Item {
    id: root

    required property ChatBubble row

    readonly property var place: row.location ?? ({})
    readonly property var live: row.liveShare ?? ({})

    readonly property bool isLive: row.mediaKind === "live_location"
    readonly property bool liveActive: isLive && (live.active ?? false)

    readonly property real latitude: place.lat ?? 0
    readonly property real longitude: place.lng ?? 0
    readonly property string placeName: String(place.name ?? "")
    readonly property string placeAddress: String(place.address ?? "")

    readonly property bool hasMap: row.mediaLocalPath.length > 0
    readonly property bool hasThumbnail: row.mediaThumbnailLocalPath.length > 0

    // The map is 640x400 (see the daemon's maps.go), so the card keeps that
    // aspect and lets the caption block below it grow as it needs to.
    readonly property real mapHeight: Math.round(width * (400 / 640))

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: mapHeight + captionBlock.implicitHeight + Kirigami.Units.smallSpacing * 2

    /** The title line: the place's name, its address, or its coordinates. */
    readonly property string headline: {
        if (placeName.length > 0)
            return placeName
        if (placeAddress.length > 0)
            return placeAddress
        return coordinateText
    }

    readonly property string coordinateText:
        latitude === 0 && longitude === 0
            ? ""
            : latitude.toFixed(5) + ", " + longitude.toFixed(5)

    /** How long a running share has left, as a short human phrase. */
    readonly property string remainingText: {
        const expiresAt = live.expires_at ?? 0
        if (!liveActive || expiresAt <= 0)
            return ""
        const seconds = expiresAt - clock.now
        if (seconds <= 0)
            return ""
        if (seconds < 60)
            return Whatevr.I18n.i18nc("@label live location time left", "under a minute left")
        const minutes = Math.round(seconds / 60)
        if (minutes < 60)
            return Whatevr.I18n.i18ncp("@label live location time left", "%1 min left", "%1 min left", minutes)
        return Whatevr.I18n.i18ncp("@label live location time left", "%1 hr left", "%1 hr left", Math.round(minutes / 60))
    }

    /** When the pin last moved, so a stale position never looks current. */
    readonly property string freshnessText: {
        const updatedAt = live.updated_at ?? 0
        if (!liveActive || updatedAt <= 0)
            return ""
        const seconds = Math.max(0, clock.now - updatedAt)
        if (seconds < 15)
            return Whatevr.I18n.i18nc("@label live location freshness", "updated just now")
        if (seconds < 60)
            return Whatevr.I18n.i18nc("@label live location freshness", "updated %1s ago", seconds)
        return Whatevr.I18n.i18ncp("@label live location freshness", "updated %1 min ago", "updated %1 min ago",
                                   Math.round(seconds / 60))
    }

    /** Speed, when the sender reported one and it is not a rounding artefact. */
    readonly property string speedText: {
        const speed = live.speed_mps ?? 0
        if (!liveActive || speed < 0.5)
            return ""
        return Whatevr.I18n.i18nc("@label live location speed", "%1 km/h", Math.round(speed * 3.6))
    }

    /** What a finished share is worth saying: how long it ran. */
    readonly property string endedText: {
        if (!isLive || liveActive)
            return ""
        const startedAt = live.started_at ?? 0
        const updatedAt = live.updated_at ?? 0
        if (startedAt <= 0 || updatedAt <= startedAt)
            return Whatevr.I18n.i18nc("@label live location ended", "Live location ended")
        const minutes = Math.max(1, Math.round((updatedAt - startedAt) / 60))
        return Whatevr.I18n.i18ncp("@label live location ended",
                                   "Shared live location for %1 min", "Shared live location for %1 min", minutes)
    }

    // A running share needs the seconds to tick; a settled card needs nothing.
    QtObject {
        id: clock

        property int now: Math.floor(Date.now() / 1000)
    }

    Timer {
        interval: 1000
        running: root.liveActive && root.row.activeInViewport
        repeat: true
        onTriggered: clock.now = Math.floor(Date.now() / 1000)
    }

    function openInMaps() {
        if (latitude === 0 && longitude === 0)
            return
        Whatevr.ProtocolController.openLocation(latitude, longitude, headline)
    }

    // ---- the map ----

    Item {
        id: mapArea

        width: parent.width
        height: root.mapHeight
        clip: true

        // A flat plate under everything, so a card with nothing decoded yet is
        // a surface rather than a hole in the bubble.
        Rectangle {
            anchors.fill: parent
            color: Qt.alpha(Kirigami.Theme.textColor, 0.06)

            Kirigami.Icon {
                anchors.centerIn: parent
                visible: !root.hasThumbnail && !root.hasMap
                width: Kirigami.Units.iconSizes.medium
                height: width
                source: "mark-location-symbolic"
                fallback: "gps"
                color: Kirigami.Theme.disabledTextColor
            }
        }

        // RoundedImage samples a texture provider rather than loading a URL, so
        // the actual Image stays hidden beside it. This is the same one-shader-
        // pass rounding photo bubbles use, with no layer or FBO behind it.
        Image {
            id: thumbnailSource

            visible: false
            source: root.hasThumbnail && !root.hasMap
                ? Whatevr.ProtocolController.localFileUrl(root.row.mediaThumbnailLocalPath)
                : ""
            fillMode: Image.PreserveAspectCrop
            asynchronous: true
            sourceSize.width: Math.max(1, Math.round(mapArea.width))
        }

        // The sender's embedded thumbnail, standing in until the real map
        // lands. It is around 200px wide, so it upscales soft: a placeholder
        // that says "somewhere", not a picture of it.
        RoundedImage {
            anchors.fill: parent
            visible: root.hasThumbnail && !root.hasMap && thumbnailSource.status === Image.Ready
            opacity: 0.85
            source: thumbnailSource
            // The map area's shape is the bubble's, and the sender's thumbnail
            // is whatever shape their phone cut, so crop to fill rather than
            // squash to fit.
            sourceRect: coverRect(width, height,
                                  thumbnailSource.implicitWidth,
                                  thumbnailSource.implicitHeight)
            topLeftRadius: Kirigami.Units.cornerRadius
            topRightRadius: Kirigami.Units.cornerRadius
        }

        Image {
            id: mapSource

            visible: false
            source: root.hasMap ? Whatevr.ProtocolController.localFileUrl(root.row.mediaLocalPath) : ""
            fillMode: Image.PreserveAspectCrop
            asynchronous: true
            // A live share redraws over the same path, so a cached decode would
            // keep showing the pin where it used to be.
            cache: !root.isLive
            sourceSize.width: Math.max(1, Math.round(mapArea.width * Screen.devicePixelRatio))
        }

        RoundedImage {
            anchors.fill: parent
            visible: root.hasMap && mapSource.status === Image.Ready
            source: mapSource
            sourceRect: coverRect(width, height,
                                  mapSource.implicitWidth, mapSource.implicitHeight)
            topLeftRadius: Kirigami.Units.cornerRadius
            topRightRadius: Kirigami.Units.cornerRadius
        }

        // The live badge: a dot that breathes while the share is running.
        Rectangle {
            id: liveBadge

            visible: root.isLive
            x: Kirigami.Units.smallSpacing
            y: Kirigami.Units.smallSpacing
            width: liveRow.implicitWidth + Kirigami.Units.smallSpacing * 2
            height: liveRow.implicitHeight + Kirigami.Units.smallSpacing
            radius: height / 2
            color: Qt.alpha(Kirigami.Theme.backgroundColor, 0.82)

            RowLayout {
                id: liveRow

                anchors.centerIn: parent
                spacing: Kirigami.Units.smallSpacing

                Rectangle {
                    Layout.alignment: Qt.AlignVCenter
                    width: Kirigami.Units.smallSpacing
                    height: width
                    radius: width / 2
                    color: root.liveActive
                        ? Kirigami.Theme.negativeTextColor
                        : Kirigami.Theme.disabledTextColor

                    SequentialAnimation on opacity {
                        running: root.liveActive && root.row.activeInViewport
                        loops: Animation.Infinite
                        NumberAnimation { to: 0.25; duration: 900; easing.type: Easing.InOutQuad }
                        NumberAnimation { to: 1.0; duration: 900; easing.type: Easing.InOutQuad }
                    }
                }

                Controls.Label {
                    text: root.liveActive
                        ? Whatevr.I18n.i18nc("@label live location badge", "Live")
                        : Whatevr.I18n.i18nc("@label live location badge", "Ended")
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                    font.bold: root.liveActive
                }
            }
        }

        // The download affordance, in the same shape every other kind uses.
        // While downloading it becomes the cancel affordance instead.
        MediaOverlayButton {
            anchors.centerIn: parent
            visible: (!root.hasMap && !root.row.mediaDownloading && root.row.mediaDownloadError.length === 0)
                     || root.row.mediaDownloading
            text: root.row.mediaDownloading
                   ? Whatevr.I18n.i18nc("@action cancel the map download", "Cancel")
                   : Whatevr.I18n.i18nc("@action fetch the map for a shared location", "Load map")
            onClicked: {
                if (root.row.messageId.length > 0) {
                    if (root.row.mediaDownloading)
                        Whatevr.ProtocolController.cancelMessageMediaDownload(root.row.messageId)
                    else
                        Whatevr.ProtocolController.downloadMessageMedia(root.row.messageId)
                }
            }
        }

        ProgressCircle {
            anchors.centerIn: parent
            visible: root.row.mediaDownloading
            width: Kirigami.Units.iconSizes.medium
            height: width
            progress: Math.max(0, root.row.mediaDownloadProgress)
        }

        TapHandler {
            enabled: !root.row.selectionModeActive && root.hasMap
            exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
            onSingleTapped: {
                root.openInMaps()
                root.row.conversationFocusRequested()
            }
        }
    }

    // ---- the caption ----

    ColumnLayout {
        id: captionBlock

        anchors.top: mapArea.bottom
        anchors.topMargin: Kirigami.Units.smallSpacing
        anchors.left: parent.left
        anchors.right: parent.right
        spacing: 0

        Controls.Label {
            Layout.fillWidth: true
            visible: text.length > 0
            text: root.headline
            elide: Text.ElideRight
            maximumLineCount: 2
            wrapMode: Text.Wrap
            font.pointSize: root.row.bodyPointSize
            font.bold: true
        }

        Controls.Label {
            Layout.fillWidth: true
            // The address only earns its line when the headline is not already
            // showing it.
            visible: text.length > 0
            text: root.placeName.length > 0 ? root.placeAddress : ""
            color: Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            maximumLineCount: 2
            wrapMode: Text.Wrap
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.fillWidth: true
            Layout.topMargin: visible ? Kirigami.Units.smallSpacing / 2 : 0
            visible: text.length > 0
            // The live line, or what a finished share amounted to.
            text: {
                if (root.liveActive) {
                    const parts = []
                    if (root.freshnessText.length > 0)
                        parts.push(root.freshnessText)
                    if (root.remainingText.length > 0)
                        parts.push(root.remainingText)
                    if (root.speedText.length > 0)
                        parts.push(root.speedText)
                    return parts.join(" · ")
                }
                return root.endedText
            }
            color: root.liveActive ? Kirigami.Theme.neutralTextColor : Kirigami.Theme.disabledTextColor
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: text.length > 0
            text: root.row.mediaDownloadError
            color: Kirigami.Theme.negativeTextColor
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            // Leave the corner free for the time and ticks.
            Layout.rightMargin: root.row.tntReserveWidth
            spacing: Kirigami.Units.smallSpacing
            visible: !root.row.selectionModeActive

            Controls.ToolButton {
                text: Whatevr.I18n.i18nc("@action open a shared location in the system's map application", "Open in Maps")
                icon.name: "mark-location-symbolic"
                display: Controls.AbstractButton.TextBesideIcon
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                enabled: root.latitude !== 0 || root.longitude !== 0
                onClicked: root.openInMaps()
            }

            Controls.ToolButton {
                Layout.alignment: Qt.AlignVCenter
                visible: root.coordinateText.length > 0
                icon.name: "edit-copy-symbolic"
                display: Controls.AbstractButton.IconOnly
                Accessible.name: Whatevr.I18n.i18nc("@action", "Copy coordinates")
                Controls.ToolTip.text: root.coordinateText
                Controls.ToolTip.visible: hovered
                Controls.ToolTip.delay: Kirigami.Units.toolTipDelay
                onClicked: Whatevr.ProtocolController.copyToClipboard(root.coordinateText)
            }

            Item {
                Layout.fillWidth: true
            }
        }
    }
}
