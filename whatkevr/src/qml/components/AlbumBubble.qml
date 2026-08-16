// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * Several pictures sent as one thing, drawn as one mosaic.
 *
 * The grouping is entirely the daemon's: it delivers one album row carrying its
 * pictures as whole message items, so nothing here merges, sorts or dedupes
 * anything (rule 3). A tile is a real message with a real id, which is what
 * lets it download itself, open in the viewer, and be forwarded or saved on its
 * own.
 *
 * The layout answers to the count, the way every client's does, but the shape
 * of the cells answers to the pictures: a set of portraits gets a taller mosaic
 * than a set of landscapes rather than everything being forced into the same
 * rectangle. Tiles crop into their cells, because a mosaic of letterboxed
 * pictures is mostly background.
 */
Item {
    id: root

    objectName: "albumBubble"

    required property ChatBubble row

    readonly property var album: row.album ?? ({})
    readonly property var tiles: album.items ?? []
    /// How many pictures the sender promised beyond the ones that arrived, 0
    /// once the album is whole. An album still filling says so rather than
    /// quietly showing fewer pictures than were sent.
    readonly property int missing: Math.max(0, (album.expected ?? 0) - tiles.length)

    /// Cells drawn at once. Past this the last cell counts what is left, which
    /// is what keeps a forty-picture album from being forty rows tall.
    readonly property int maxCells: 6
    readonly property int cellCount: Math.min(tiles.length, maxCells)
    /// Pictures the mosaic does not draw, plus the ones that have not arrived.
    /// They share one "+N" because from the reader's side they are the same
    /// thing: more of this than is on screen.
    readonly property int overflow: tiles.length - cellCount + missing

    readonly property real gap: Math.round(Kirigami.Units.smallSpacing / 2)
    readonly property real cornerRadius: Kirigami.Units.cornerRadius

    /// The shape the cells take, as height over width. Derived from the
    /// pictures rather than fixed, and clamped: one extreme panorama in a set
    /// must not flatten the whole mosaic, and one very tall picture must not
    /// take over the bubble.
    readonly property real cellAspect: {
        let total = 0
        let counted = 0
        for (let i = 0; i < cellCount; ++i) {
            const media = tiles[i].media ?? {}
            const w = media.width ?? 0
            const h = media.height ?? 0
            if (w > 0 && h > 0) {
                total += h / w
                counted += 1
            }
        }
        const average = counted > 0 ? total / counted : 1
        return Math.max(0.7, Math.min(1.35, average))
    }

    /**
     * The mosaic, as a cell rectangle per tile.
     *
     * One function rather than nested layouts because the shapes are not a
     * grid: three pictures are one large and two stacked beside it, and five
     * are two over three. Expressed as rows of weights, where a row's weight is
     * its share of the height and a cell's is its share of that row's width.
     */
    function cellRects(count, width, aspect) {
        const g = gap
        if (count <= 0 || width <= 0)
            return []

        if (count === 1) {
            return [{x: 0, y: 0, w: width, h: Math.round(width * aspect)}]
        }
        // Three pictures are the one shape that is not rows: a large picture
        // beside a stack of two, which is how every client draws it and the
        // only layout that gives a set of three a dominant picture.
        if (count === 3) {
            const bigW = Math.round((width - g) * 0.62)
            const smallW = width - g - bigW
            const height = Math.round(bigW * aspect)
            const smallH = Math.round((height - g) / 2)
            return [
                {x: 0, y: 0, w: bigW, h: height},
                {x: bigW + g, y: 0, w: smallW, h: smallH},
                {x: bigW + g, y: smallH + g, w: smallW, h: height - smallH - g},
            ]
        }

        const rows = count === 2 ? [2]
                   : count === 4 ? [2, 2]
                   : count === 5 ? [2, 3]
                   : [3, 3]
        // Every row is as tall as a cell in the widest row would be, so cells
        // across a mosaic stay close to the same size instead of the two-cell
        // row towering over the three-cell one.
        const widest = Math.max(...rows)
        const rowHeight = Math.round(((width - g * (widest - 1)) / widest) * aspect)

        const rects = []
        let y = 0
        for (const cells of rows) {
            const cellWidth = (width - g * (cells - 1)) / cells
            for (let i = 0; i < cells; ++i) {
                rects.push({
                    x: Math.round(i * (cellWidth + g)),
                    y: y,
                    // Round the right edge rather than the width, so rounding
                    // never leaves a hairline of background between cells or
                    // pushes the last one past the mosaic.
                    w: Math.round((i + 1) * (cellWidth + g) - g) - Math.round(i * (cellWidth + g)),
                    h: rowHeight,
                })
            }
            y += rowHeight + g
        }
        return rects
    }

    readonly property var rects: cellRects(cellCount, width, cellAspect)

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: rects.length > 0
        ? rects[rects.length - 1].y + rects[rects.length - 1].h
        : 0

    /// How many tiles have something on screen. The row reads it back as
    /// "there is artwork here", which is what puts the time's scrim and its
    /// light tones on: white on nothing under a scrim would be neither
    /// readable nor honest about what is there.
    property int drawnTiles: 0

    Binding {
        target: root.row
        property: "mediaArtworkShown"
        restoreMode: Binding.RestoreBindingOrValue
        value: root.drawnTiles > 0
    }

    /// Whether a tile may fetch itself when the album scrolls into view. Same
    /// question ChatBubble asks for a lone photo, asked per picture: a tile is
    /// a message with a download of its own, and the album row it hangs off has
    /// nothing to fetch.
    function tileAutoDownloadWanted(tile) {
        const media = tile.media ?? {}
        if (String(media.path ?? "").length > 0 || (media.downloading ?? false))
            return false
        if (String(media.download_error ?? "").length > 0)
            return false
        const ceiling = row.autoDownloadSizeCeiling
        if (ceiling > 0 && (media.size_bytes ?? 0) > ceiling)
            return false
        const prefs = Whatevr.ProtocolController.appPreferences
        if (tile.kind === "video" || tile.kind === "gif")
            return prefs.auto_download_videos ?? false
        return prefs.auto_download_photos ?? false
    }

    function maybeAutoDownloadTiles() {
        if (!row.activeInViewport || row.fastFlicking)
            return
        for (let i = 0; i < cellCount; ++i) {
            const tile = tiles[i]
            if (tileAutoDownloadWanted(tile))
                Whatevr.ProtocolController.downloadMessageMedia(String(tile.id))
        }
    }

    Component.onCompleted: maybeAutoDownloadTiles()
    Connections {
        target: root.row

        function onActiveInViewportChanged() {
            root.maybeAutoDownloadTiles()
        }
    }

    Repeater {
        model: root.cellCount

        delegate: Item {
            id: cell

            required property int index

            readonly property var tile: root.tiles[index] ?? ({})
            readonly property var tileMedia: tile.media ?? ({})
            readonly property var rect: root.rects[index] ?? ({x: 0, y: 0, w: 0, h: 0})
            readonly property string tileId: String(tile.id ?? "")
            readonly property bool isVideo: tile.kind === "video" || tile.kind === "gif"
            readonly property string localPath: String(tileMedia.path ?? "")
            readonly property string thumbnailPath: String(tileMedia.thumbnail_path ?? "")
            readonly property bool downloading: tileMedia.downloading ?? false
            readonly property bool hasFile: localPath.length > 0
            /// The last cell counts what the mosaic is not showing.
            readonly property bool isOverflowCell: root.overflow > 0 && index === root.cellCount - 1

            x: rect.x
            y: rect.y
            width: rect.w
            height: rect.h

            // Corners are rounded only where the cell meets the outside of the
            // mosaic, so the tiles read as one picture cut up rather than as a
            // handful of separate cards.
            readonly property bool atLeft: rect.x <= 0
            readonly property bool atRight: rect.x + rect.w >= root.width - 1
            readonly property bool atTop: rect.y <= 0
            readonly property bool atBottom: rect.y + rect.h >= root.height - 1

            Rectangle {
                anchors.fill: parent
                color: Qt.alpha(Kirigami.Theme.textColor, 0.08)
                topLeftRadius: cell.atTop && cell.atLeft ? root.cornerRadius : 0
                topRightRadius: cell.atTop && cell.atRight ? root.cornerRadius : 0
                bottomLeftRadius: cell.atBottom && cell.atLeft ? root.cornerRadius : 0
                bottomRightRadius: cell.atBottom && cell.atRight ? root.cornerRadius : 0
            }

            // The full picture once it is here, the sender's inline thumbnail
            // until then. The thumbnail is tiny and arrives with the message,
            // so a mosaic is never a grid of empty boxes waiting on a network.
            Image {
                id: tileSource

                visible: false
                anchors.fill: parent
                source: cell.hasFile
                    ? Whatevr.ProtocolController.localFileUrl(cell.localPath)
                    : (cell.thumbnailPath.length > 0
                        ? Whatevr.ProtocolController.localFileUrl(cell.thumbnailPath)
                        : "")
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                // Only the width, so the decode keeps the picture's own aspect
                // ratio: the crop happens in the shader, and a decode that had
                // already cropped would be cropped twice. Wide enough to cover
                // the cell in both directions, so a tall picture in a wide cell
                // is not upscaled to fill it.
                sourceSize.width: {
                    const aspect = (cell.tileMedia.width ?? 0) > 0 && (cell.tileMedia.height ?? 0) > 0
                        ? cell.tileMedia.width / cell.tileMedia.height
                        : 1
                    return Math.max(1, Math.ceil(Math.max(width, height * aspect) * Screen.devicePixelRatio))
                }

                property bool counted: false

                onStatusChanged: {
                    const drawn = status === Image.Ready
                    if (drawn === counted)
                        return
                    counted = drawn
                    root.drawnTiles += drawn ? 1 : -1
                }
                Component.onDestruction: if (counted) root.drawnTiles -= 1
            }

            RoundedImage {
                id: tilePicture

                anchors.fill: parent
                visible: tileSource.status === Image.Ready
                source: tileSource
                // A cell's shape comes from the mosaic, not from the picture in
                // it, so the picture is cropped to fill rather than squashed to
                // fit. The decoded size is the truth once it is known; the
                // sender's dimensions cover the moment before that.
                sourceRect: coverRect(width, height,
                                      tileSource.implicitWidth > 0
                                          ? tileSource.implicitWidth
                                          : (cell.tileMedia.width ?? 0),
                                      tileSource.implicitHeight > 0
                                          ? tileSource.implicitHeight
                                          : (cell.tileMedia.height ?? 0))
                topLeftRadius: cell.atTop && cell.atLeft ? root.cornerRadius : 0
                topRightRadius: cell.atTop && cell.atRight ? root.cornerRadius : 0
                bottomLeftRadius: cell.atBottom && cell.atLeft ? root.cornerRadius : 0
                bottomRightRadius: cell.atBottom && cell.atRight ? root.cornerRadius : 0
            }

            // A picture that is only a thumbnail so far is dimmed under its
            // affordance, so "there is more of this to fetch" is visible
            // without reading the button.
            Rectangle {
                anchors.fill: parent
                visible: !cell.hasFile || cell.isOverflowCell
                color: Qt.rgba(0, 0, 0, cell.isOverflowCell ? 0.55 : 0.25)
                topLeftRadius: cell.atTop && cell.atLeft ? root.cornerRadius : 0
                topRightRadius: cell.atTop && cell.atRight ? root.cornerRadius : 0
                bottomLeftRadius: cell.atBottom && cell.atLeft ? root.cornerRadius : 0
                bottomRightRadius: cell.atBottom && cell.atRight ? root.cornerRadius : 0
            }

            Text {
                anchors.centerIn: parent
                visible: cell.isOverflowCell
                text: "+" + root.overflow
                color: "white"
                font.pointSize: Kirigami.Theme.defaultFont.pointSize * 1.6
                font.bold: true
            }

            // The per-tile fetch. A picture in an album downloads exactly like
            // a picture on its own, because it is one. The spinner is
            // indeterminate rather than a ring: the byte counters live in the
            // `transfers` view keyed by message id, and a tile is not a row
            // this model can join that against, so a ring here would be a
            // percentage nobody measured.
            Controls.BusyIndicator {
                anchors.centerIn: parent
                width: Math.round(Math.min(cell.width, cell.height) * 0.35)
                height: width
                visible: cell.downloading && !cell.isOverflowCell
                running: visible
            }

            MediaOverlayButton {
                anchors.centerIn: parent
                visible: !cell.hasFile && !cell.downloading && !cell.isOverflowCell
                         && !root.row.selectionModeActive
                diameter: Math.round(Math.min(cell.width, cell.height, Kirigami.Units.gridUnit * 3) * 0.7)
                iconName: "download-symbolic"
                text: Whatevr.I18n.i18nc("@action:button", "Download")
                onClicked: Whatevr.ProtocolController.downloadMessageMedia(cell.tileId)
            }

            // A video tile says so even before it is fetched, the same way a
            // video bubble does.
            Kirigami.Icon {
                anchors.centerIn: parent
                visible: cell.isVideo && cell.hasFile && !cell.isOverflowCell
                width: Math.round(Math.min(cell.width, cell.height) * 0.3)
                height: width
                source: "media-playback-start-symbolic"
                color: "white"
            }

            TapHandler {
                enabled: !root.row.selectionModeActive
                exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
                onSingleTapped: {
                    // Nothing on disk yet: the tap is the fetch, which is what
                    // a photo bubble does too. Opening an empty viewer over a
                    // thumbnail would be a worse answer than starting the
                    // download the reader plainly wants.
                    if (!cell.hasFile) {
                        if (!cell.downloading)
                            Whatevr.ProtocolController.downloadMessageMedia(cell.tileId)
                        return
                    }
                    root.row.albumItemActivated(root.row.messageId, cell.index)
                }
            }

            HoverHandler {
                enabled: !root.row.selectionModeActive
                cursorShape: Qt.PointingHandCursor
            }
        }
    }
}
