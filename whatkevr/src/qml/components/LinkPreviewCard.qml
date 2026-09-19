// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * The card a sender's client built for a link in their message.
 *
 * It is the one card that shares its bubble with body text, because the text is
 * still the message: this describes where a link in it goes. So it sits above
 * the words rather than replacing them, and it takes the bubble's content width
 * so it never reads as a narrow box stranded over a wider paragraph.
 *
 * Two layouts, chosen by what the sender said they were previewing. A video or
 * an image gets the picture big, because the picture is the content. Everything
 * else gets a compact row, because there the picture is a logo and the words
 * are the content, and a logo blown up to card width is a worse card than no
 * picture at all.
 *
 * Nothing here is fetched. Every pixel and every word arrived inside the
 * message, which is the whole point of the sender building the preview: opening
 * a chat cannot tell a stranger's server that you read it.
 */
Item {
    id: root

    objectName: "linkPreviewCard"

    required property ChatBubble row

    readonly property var preview: row.linkPreview ?? ({})

    readonly property string url: String(preview.url ?? "")
    readonly property string host: String(preview.host ?? "")
    readonly property string title: String(preview.title ?? "")
    readonly property string description: String(preview.description ?? "")
    readonly property string previewType: String(preview.type ?? "")
    readonly property string thumbnailPath: String(preview.thumbnail_path ?? "")
    readonly property int thumbnailWidth: preview.thumb_width ?? 0
    readonly property int thumbnailHeight: preview.thumb_height ?? 0

    readonly property bool hasThumbnail: thumbnailPath.length > 0

    /// Whether the picture is the point. The sender's client is the only thing
    /// that knows: it fetched the page, and it is the difference between a
    /// video still and a favicon. Guessing from the URL would get it wrong for
    /// every site nobody thought to special-case.
    readonly property bool largeLayout: hasThumbnail
                                        && (previewType === "video" || previewType === "image")
    /// A play glyph over the still, for a link that leads to something to
    /// watch. It opens the page like the rest of the card: whatever plays it,
    /// plays it there.
    readonly property bool showsPlayBadge: largeLayout && previewType === "video"

    /// Padding between the card's edge and its content, counted twice in the
    /// height so the last line does not sit on the bottom edge. The hero
    /// picture ignores it on three sides on purpose: a preview picture inset
    /// from its own card reads as a picture of a card.
    readonly property real contentMargin: Kirigami.Units.smallSpacing
    readonly property real cornerRadius: Kirigami.Units.cornerRadius

    /// The compact layout's picture: a square, because a logo has no aspect
    /// ratio worth honouring and a column of preview rows with square thumbs
    /// shares one left edge.
    readonly property real thumbSize: Kirigami.Units.gridUnit * 3

    /// The picture's own shape, so a still is not letterboxed into a band and
    /// a panorama is not squared off. Floored rather than capped in pixels: a
    /// portrait picture shown at its own shape turns the card into a column
    /// and pushes the words explaining it out of the bubble, so anything
    /// taller than four fifths of its width is cropped to that.
    readonly property real minimumHeroAspect: 1.25
    readonly property real heroAspect: {
        const stated = thumbnailWidth > 0 && thumbnailHeight > 0
            ? thumbnailWidth / thumbnailHeight
            : 16 / 9
        return Math.max(stated, minimumHeroAspect)
    }
    readonly property real heroHeight: largeLayout ? Math.max(1, width / heroAspect) : 0

    implicitWidth: row.attachmentBlockWidth
    // See InteractiveBubble: a layout has no implicit height until it first
    // arranges, and a row that reserves nothing and then grows is a row that
    // shoves the transcript under it.
    readonly property real unmeasuredBodyHeight: Kirigami.Units.gridUnit * 3
    implicitHeight: hero.height + contentMargin * 2
                    + (body.implicitHeight > 0 ? body.implicitHeight : unmeasuredBodyHeight)

    /// The width this card would rather be. The hero layout wants the bubble,
    /// so it asks for nothing and fills. The compact one asks for its text,
    /// because a two-word title and a hostname stretched across the ceiling is
    /// a card that is mostly nothing.
    ///
    /// Measured from the words rather than from the laid-out labels, so it
    /// never depends on the width it is helping decide.
    readonly property real preferredWidth: {
        if (largeLayout) {
            return 0
        }
        let text = Math.max(titleMetrics.advanceWidth, descriptionMetrics.advanceWidth)
        text = Math.max(text, hostMetrics.advanceWidth + hostChipPadding * 2)
        return contentMargin * 2 + (hasThumbnail ? thumbSize + Kirigami.Units.smallSpacing : 0) + text
    }

    /// A fully rounded chip pads its sides to its own height, not to a flat
    /// unit, or the text sits in the flat middle with the round ends crowding
    /// it.
    readonly property real hostChipPadding: Math.round(hostMetrics.height * 0.42)

    /// Room the words have, worked out from the card's own width rather than
    /// read back off the laid-out column.
    ///
    /// The column's width is decided by its children, so a child that bounds
    /// itself by that width is asking the layout a question whose answer it is
    /// part of. Everything in here that has to elide needs a ceiling that does
    /// not depend on it, and the card's width is one: it is handed down by the
    /// row before any of this arranges.
    readonly property real textColumnWidth: Math.max(0, width - contentMargin * 2
                                                        - (largeLayout
                                                           ? 0
                                                           : thumbSize + Kirigami.Units.smallSpacing))

    function openLink() {
        if (url.length > 0) {
            Qt.openUrlExternally(url)
        }
    }

    TextMetrics {
        id: titleMetrics

        font: titleLabel.font
        text: root.title
    }

    TextMetrics {
        id: descriptionMetrics

        font: descriptionLabel.font
        text: root.description
    }

    TextMetrics {
        id: hostMetrics

        font: hostLabel.font
        text: root.host
    }

    Rectangle {
        anchors.fill: parent
        radius: root.cornerRadius
        // The whole card answers the pointer, because the whole card is one
        // link: there is no part of it that does something else.
        color: Qt.alpha(Kirigami.Theme.textColor, cardHover.hovered ? 0.10 : 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1

        Behavior on color {
            ColorAnimation { duration: Kirigami.Units.shortDuration }
        }
    }

    // The hero picture. Always present, zero-height in the compact layout, so
    // the text block below can anchor to its bottom edge either way.
    Item {
        id: hero

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: root.heroHeight
        visible: root.largeLayout

        Image {
            id: heroSource

            visible: false
            anchors.fill: parent
            source: root.largeLayout
                ? Whatevr.ProtocolController.localFileUrl(root.thumbnailPath)
                : ""
            fillMode: Image.PreserveAspectCrop
            asynchronous: true
            cache: true
            // No sourceSize: this is the small JPEG that arrived inside the
            // message, and pinning the decode to the slot's width upscales a
            // hundred-pixel picture into a megapixel one, again on every
            // relayout. Decoding it at its own size and letting the scene graph
            // scale it up is both cheaper and no blurrier.
        }

        RoundedImage {
            anchors.fill: parent
            visible: heroSource.status === Image.Ready
            source: heroSource
            // The slot already carries the picture's aspect ratio, so the crop
            // only ever trims the rounding error the height cap introduces.
            sourceRect: coverRect(width, height, root.thumbnailWidth, root.thumbnailHeight)
            // Square along the bottom: the picture meets the text, and a
            // rounded edge there would leave two slivers of card showing
            // through under its corners.
            topLeftRadius: root.cornerRadius
            topRightRadius: root.cornerRadius
        }

        Rectangle {
            anchors.centerIn: parent
            visible: root.showsPlayBadge
            width: Kirigami.Units.gridUnit * 2.4
            height: width
            radius: width / 2
            color: Qt.rgba(0, 0, 0, 0.55)

            Kirigami.Icon {
                anchors.centerIn: parent
                // Nudged right by the glyph's own optical centre: a triangle
                // centred on its bounding box reads as sitting left of centre
                // in a circle.
                anchors.horizontalCenterOffset: Math.round(Kirigami.Units.smallSpacing / 2)
                implicitWidth: Kirigami.Units.iconSizes.smallMedium
                implicitHeight: implicitWidth
                source: "media-playback-start-symbolic"
                color: "white"
            }
        }
    }

    RowLayout {
        id: body

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: hero.bottom
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        // The compact layout's picture, or the placeholder that stands in for
        // it. A link with no picture still gets the square, so a run of
        // previews down a chat shares one left edge instead of stepping in and
        // out by a thumbnail's width.
        Rectangle {
            id: thumbBox

            /// The picture's own shape. A square is the guess for one whose
            /// dimensions never arrived, because that is what a site icon is.
            readonly property real pictureAspect: root.thumbnailWidth > 0 && root.thumbnailHeight > 0
                ? root.thumbnailWidth / root.thumbnailHeight
                : 1
            /// The picture drawn whole, at its own shape, as large as fits. It
            /// is a logo or a piece of cover art: cropping one to a square cuts
            /// the edges off the thing that identifies the site.
            readonly property real pictureWidth: Math.min(width, height * pictureAspect)
            readonly property real pictureHeight: Math.min(height, width / pictureAspect)

            /// A fixed square, deliberately not one that grows with the words
            /// beside it.
            ///
            /// Following the text's height reads well at a comfortable width and
            /// runs away at a narrow one: a taller column makes a wider square,
            /// a wider square leaves less room for the words, less room wraps
            /// them onto more lines, and the column gets taller again. In a
            /// narrow pane that settles with an enormous placeholder beside a
            /// sliver of text, which is the broken preview people actually see.
            /// The height cap on the labels does not bound it, because each pass
            /// through the loop re-wraps before the cap applies.
            readonly property real side: root.thumbSize

            Layout.alignment: Qt.AlignTop
            Layout.preferredWidth: side
            Layout.preferredHeight: side
            visible: !root.largeLayout
            implicitWidth: side
            implicitHeight: side
            radius: Math.round(root.cornerRadius * 0.75)
            color: Qt.alpha(Kirigami.Theme.textColor, 0.06)

            Kirigami.Icon {
                anchors.centerIn: parent
                visible: !root.hasThumbnail
                implicitWidth: Kirigami.Units.iconSizes.medium
                implicitHeight: implicitWidth
                source: "globe-symbolic"
                color: Kirigami.Theme.disabledTextColor
            }

            Image {
                id: thumbSource

                visible: false
                anchors.fill: parent
                source: !root.largeLayout && root.hasThumbnail
                    ? Whatevr.ProtocolController.localFileUrl(root.thumbnailPath)
                    : ""
                fillMode: Image.PreserveAspectFit
                asynchronous: true
                cache: true
            }

            RoundedImage {
                anchors.centerIn: parent
                width: thumbBox.pictureWidth
                height: thumbBox.pictureHeight
                visible: thumbSource.status === Image.Ready
                source: thumbSource
                topLeftRadius: parent.radius
                topRightRadius: parent.radius
                bottomLeftRadius: parent.radius
                bottomRightRadius: parent.radius
            }
        }

        ColumnLayout {
            id: previewText

            Layout.fillWidth: true
            Layout.alignment: Qt.AlignTop
            spacing: Kirigami.Units.smallSpacing / 2

            Controls.Label {
                id: titleLabel

                Layout.fillWidth: true
                Layout.maximumWidth: root.textColumnWidth
                visible: root.title.length > 0
                text: root.title
                wrapMode: Text.Wrap
                elide: Text.ElideRight
                maximumLineCount: 2
                font.pointSize: root.row.bodyPointSize
                font.bold: true
            }

            // The site, right under the claim its title makes. It is the one
            // part of a URL worth reading, and reading it beside the title is
            // how somebody notices that a headline and its address disagree.
            Rectangle {
                id: hostChip

                Layout.alignment: Qt.AlignLeft
                // Bounded by the column it sits in. A long host on a narrow card
                // otherwise pushed the chip straight out past the bubble edge:
                // the label asks to elide, but neither it nor the chip had a
                // width to elide against.
                Layout.maximumWidth: root.textColumnWidth
                visible: root.host.length > 0
                implicitWidth: Math.min(hostLabel.implicitWidth + root.hostChipPadding * 2,
                                        root.textColumnWidth)
                implicitHeight: hostLabel.implicitHeight + Math.round(hostMetrics.height * 0.18) * 2
                radius: height / 2
                color: Qt.alpha(Kirigami.Theme.textColor, 0.07)

                Controls.Label {
                    id: hostLabel

                    anchors.centerIn: parent
                    width: Math.min(implicitWidth,
                                    Math.max(0, hostChip.width - root.hostChipPadding * 2))
                    text: root.host
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }

            Controls.Label {
                id: descriptionLabel

                Layout.fillWidth: true
                Layout.maximumWidth: root.textColumnWidth
                visible: root.description.length > 0
                text: root.description
                color: Kirigami.Theme.disabledTextColor
                wrapMode: Text.Wrap
                elide: Text.ElideRight
                maximumLineCount: 2
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }
        }
    }

    // Above the card's own background and below nothing: the whole card is the
    // link, so there is no inner target to keep clear of.
    TapHandler {
        enabled: !root.row.selectionModeActive
        acceptedButtons: Qt.LeftButton
        exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
        onSingleTapped: root.openLink()
    }

    HoverHandler {
        id: cardHover

        enabled: !root.row.selectionModeActive
        cursorShape: Qt.PointingHandCursor
    }
}
