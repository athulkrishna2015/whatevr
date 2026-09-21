// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A business message: a header, some words, and a set of things you are invited
 * to press.
 *
 * WhatsApp has four wire shapes for that one idea and the daemon flattens all
 * four, so this draws one card and never learns which arrived. The parts are
 * always in the same order, because that order is the message: the header says
 * who is talking, the body says the thing, the footer qualifies it, and the
 * buttons are what to do about it.
 *
 * The honest part is the buttons. Opening a link, dialling a number and copying
 * a code are things this machine can do, so those buttons work. A quick reply
 * sends a message back to a business, which whatevr cannot do yet, so those are
 * drawn locked with one line under the group saying where they do work. A phone
 * shows all of them alike and lets you find out by pressing.
 */
Item {
    id: root

    objectName: "interactiveBubble"

    required property ChatBubble row

    readonly property var card: row.interactive ?? ({})

    readonly property string title: String(card.title ?? "")
    readonly property string subtitle: String(card.subtitle ?? "")
    readonly property string bodyText: String(card.body ?? "")
    readonly property string footerText: String(card.footer ?? "")
    readonly property string documentName: String(card.document_name ?? "")
    readonly property string thumbnailPath: String(card.thumbnail_path ?? "")
    readonly property string listLabel: String(card.list_label ?? "")
    readonly property var buttons: card.buttons ?? []
    readonly property var sections: card.sections ?? []
    readonly property var cards: card.cards ?? []

    readonly property bool hasHero: thumbnailPath.length > 0
    readonly property bool isCarousel: cards.length > 0
    /// Whether anything on this card is offered but cannot be carried out. One
    /// line explains the whole group; repeating it per button would be three
    /// copies of the same sentence.
    readonly property bool hasDeadButtons: {
        for (let i = 0; i < buttons.length; i++) {
            if (!(buttons[i].live ?? false)) {
                return true
            }
        }
        return false
    }

    property bool sectionsExpanded: false

    readonly property real contentMargin: Kirigami.Units.smallSpacing
    readonly property real cornerRadius: Kirigami.Units.cornerRadius

    /// The hero keeps its own shape, floored so a portrait header cannot turn
    /// the card into a column and push the words off the bottom of the bubble.
    readonly property real heroAspect: 1.9
    readonly property real heroHeight: hasHero ? Math.max(1, width / heroAspect) : 0

    /// A carousel slide is a fixed size on purpose. Slides that each took their
    /// own height would step up and down as the strip scrolled, and a strip
    /// whose height depends on which slide is showing cannot publish a height
    /// for the row to reserve.
    readonly property real slideWidth: Math.min(Kirigami.Units.gridUnit * 11, Math.max(0, width - contentMargin * 2))
    readonly property real slideHeight: Math.round(slideWidth / 1.6) + Kirigami.Units.gridUnit * 5.4

    implicitWidth: row.attachmentBlockWidth
    // The reserve before the words have been measured is a card's worth of
    // room, not nothing. A layout reports an implicit height of zero until its
    // first arrangement, and a row that reserved eight pixels and then grew to
    // four hundred is what shifts everything under it mid-scroll and leaves
    // jump-to-bottom a little short of the bottom.
    readonly property real unmeasuredBodyHeight: Kirigami.Units.gridUnit * 6
    implicitHeight: hero.height + contentMargin * 2
                    + (body.implicitHeight > 0 ? body.implicitHeight : unmeasuredBodyHeight)

    function activate(button) {
        const kind = String(button.kind ?? "")
        if (kind === "url") {
            Qt.openUrlExternally(String(button.url ?? ""))
        } else if (kind === "call") {
            Qt.openUrlExternally("tel:" + String(button.phone ?? ""))
        } else if (kind === "copy") {
            Whatevr.ProtocolController.copyToClipboard(String(button.copy ?? ""))
        }
    }

    function buttonIcon(kind) {
        switch (kind) {
        case "url":
            return "link-symbolic"
        case "call":
            return "call-start-symbolic"
        case "copy":
            return "edit-copy-symbolic"
        }
        // Anything that would have to send a message back. The padlock is the
        // whole point: it says why nothing happened before it does not happen.
        return "lock-symbolic"
    }

    /// One offered action. Full width and stacked, which is what a business
    /// message's buttons are, and what keeps a three-button card from wrapping
    /// into a ragged block.
    component ActionRow: Controls.AbstractButton {
        id: action

        required property var button
        readonly property bool live: button.live ?? false
        readonly property bool highlighted: hovered || actionHover.hovered

        objectName: "interactiveActionRow"
        enabled: live && !root.row.selectionModeActive
        hoverEnabled: true
        implicitHeight: Math.round(Kirigami.Units.gridUnit * 1.9)
        Accessible.name: text

        HoverHandler {
            id: actionHover

            enabled: action.live && !root.row.selectionModeActive
            cursorShape: Qt.PointingHandCursor
        }

        background: Rectangle {
            radius: Kirigami.Units.cornerRadius
            color: Qt.alpha(Whatevr.Palette.highlight,
                            action.down ? 0.3 : (action.highlighted ? 0.16 : 0.06))

            Behavior on color {
                ColorAnimation { duration: Kirigami.Units.shortDuration }
            }
        }

        // A plain centred Row, not a RowLayout. A layout inside a control
        // inside a layout is the one nesting QtQuick.Layouts does not settle
        // reliably: the outer column arranges against a button whose own
        // arrangement has not run, and the card draws every line on top of the
        // first. CardActionButton has always used a positioner here for the
        // same reason.
        contentItem: Item {
            Row {
                anchors.centerIn: parent
                spacing: Kirigami.Units.smallSpacing

                Kirigami.Icon {
                    anchors.verticalCenter: parent.verticalCenter
                    width: Kirigami.Units.iconSizes.small
                    height: width
                    source: root.buttonIcon(String(action.button.kind ?? ""))
                    color: action.live
                        ? (action.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor)
                        : Kirigami.Theme.disabledTextColor
                    isMask: true
                }

                Controls.Label {
                    anchors.verticalCenter: parent.verticalCenter
                    width: Math.min(implicitWidth,
                                    Math.max(0, action.width - Kirigami.Units.gridUnit * 3))
                    text: action.text
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    color: action.live
                        ? (action.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor)
                        : Kirigami.Theme.disabledTextColor
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        onClicked: root.activate(button)
    }

    Rectangle {
        anchors.fill: parent
        radius: root.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    // The header picture, edge to edge. Always present and zero-height without
    // one, so the column below can anchor to its bottom either way.
    Item {
        id: hero

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        height: root.heroHeight
        visible: root.hasHero

        Image {
            id: heroSource

            visible: false
            anchors.fill: parent
            source: root.hasHero
                ? Whatevr.ProtocolController.localFileUrl(root.thumbnailPath)
                : ""
            fillMode: Image.PreserveAspectCrop
            asynchronous: true
            cache: true
            // No sourceSize: this is the small JPEG that arrived
            // inside the message, and pinning the decode to the slot's
            // width would upscale a hundred-pixel picture into a
            // megapixel one, again on every relayout. Decoding it at
            // its own size and letting the scene graph scale it up is
            // both cheaper and no blurrier.
        }

        RoundedImage {
            anchors.fill: parent
            visible: heroSource.status === Image.Ready
            source: heroSource
            // The header's shape is the card's, not the picture's, so the
            // picture is cropped to fill it rather than squashed to fit. The
            // source Image's own fillMode never reaches the shader: it hands
            // over the whole decoded texture.
            sourceRect: coverRect(width, height,
                                  heroSource.implicitWidth, heroSource.implicitHeight)
            // Square along the bottom: the picture meets the words, and rounding
            // there would leave two slivers of card showing under its corners.
            topLeftRadius: root.cornerRadius
            topRightRadius: root.cornerRadius
        }
    }

    ColumnLayout {
        id: body

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: hero.bottom
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        Controls.Label {
            Layout.fillWidth: true
            visible: root.title.length > 0
            text: root.title
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: root.row.bodyPointSize
            font.bold: true
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.subtitle.length > 0
            text: root.subtitle
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // A plain wrapping Label, like every other card's text. A TextArea here
        // would make the body selectable, which is worth having, but its
        // implicit height is derived from its own width: inside a layout that
        // is itself being measured, it settles at one line and the whole card
        // collapses onto it. The height contract with the row comes first.
        Controls.Label {
            Layout.fillWidth: true
            visible: root.bodyText.length > 0
            text: root.bodyText
            wrapMode: Text.Wrap
            font.pointSize: root.row.bodyPointSize
        }

        // A header that arrived as a document. Nothing is fetched, so the card
        // names the file rather than offering to open something it does not
        // have.
        RowLayout {
            Layout.fillWidth: true
            visible: root.documentName.length > 0
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: "text-x-generic-symbolic"
                color: Kirigami.Theme.disabledTextColor
                isMask: true
            }

            Controls.Label {
                Layout.fillWidth: true
                text: root.documentName
                color: Kirigami.Theme.disabledTextColor
                elide: Text.ElideRight
                maximumLineCount: 1
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }
        }

        // The list, folded away behind the button that opens it. WhatsApp puts
        // a menu behind a sheet; a transcript row cannot, so it expands in
        // place and the row grows, which is the same gesture with the contents
        // staying where you found them.
        Controls.AbstractButton {
            id: sectionsToggle

            readonly property bool highlighted: hovered || sectionsHover.hovered

            objectName: "interactiveListToggle"
            Layout.fillWidth: true
            visible: root.sections.length > 0
            enabled: !root.row.selectionModeActive
            hoverEnabled: true
            implicitHeight: Math.round(Kirigami.Units.gridUnit * 1.9)
            Accessible.name: text
            text: root.listLabel.length > 0
                ? root.listLabel
                : Whatevr.I18n.i18nc("@action open the list inside a business message", "See the options")

            HoverHandler {
                id: sectionsHover

                enabled: !root.row.selectionModeActive
                cursorShape: Qt.PointingHandCursor
            }

            background: Rectangle {
                radius: Kirigami.Units.cornerRadius
                color: Qt.alpha(Whatevr.Palette.highlight,
                                sectionsToggle.down ? 0.3 : (sectionsToggle.highlighted ? 0.16 : 0.06))

                Behavior on color {
                    ColorAnimation { duration: Kirigami.Units.shortDuration }
                }
            }

            // A positioner rather than a layout, for the same reason the action
            // rows above use one.
            contentItem: Item {
                Row {
                    anchors.centerIn: parent
                    spacing: Kirigami.Units.smallSpacing

                    Kirigami.Icon {
                        anchors.verticalCenter: parent.verticalCenter
                        width: Kirigami.Units.iconSizes.small
                        height: width
                        source: root.sectionsExpanded ? "go-up-symbolic" : "format-justify-fill-symbolic"
                        color: sectionsToggle.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
                        isMask: true
                    }

                    Controls.Label {
                        anchors.verticalCenter: parent.verticalCenter
                        width: Math.min(implicitWidth,
                                        Math.max(0, sectionsToggle.width - Kirigami.Units.gridUnit * 3))
                        text: sectionsToggle.text
                        elide: Text.ElideRight
                        maximumLineCount: 1
                        color: sectionsToggle.highlighted ? Whatevr.Palette.highlight : Kirigami.Theme.textColor
                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                    }
                }
            }

            onClicked: root.sectionsExpanded = !root.sectionsExpanded
        }

        Repeater {
            model: root.sectionsExpanded ? root.sections : []

            ColumnLayout {
                id: section

                required property var modelData

                Layout.fillWidth: true
                Layout.leftMargin: Kirigami.Units.smallSpacing
                spacing: Kirigami.Units.smallSpacing / 2

                Controls.Label {
                    Layout.fillWidth: true
                    visible: String(section.modelData.title ?? "").length > 0
                    text: String(section.modelData.title ?? "")
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    color: Kirigami.Theme.disabledTextColor
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                    font.weight: Font.DemiBold
                }

                Repeater {
                    model: section.modelData.rows ?? []

                    RowLayout {
                        id: listRow

                        required property var modelData

                        Layout.fillWidth: true
                        spacing: Kirigami.Units.smallSpacing

                        Rectangle {
                            Layout.alignment: Qt.AlignTop
                            Layout.topMargin: Math.round(Kirigami.Units.smallSpacing * 0.9)
                            implicitWidth: Math.max(2, Math.round(Kirigami.Units.smallSpacing / 2))
                            implicitHeight: implicitWidth
                            radius: width / 2
                            color: Kirigami.Theme.disabledTextColor
                        }

                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 0

                            Controls.Label {
                                Layout.fillWidth: true
                                visible: String(listRow.modelData.title ?? "").length > 0
                                text: String(listRow.modelData.title ?? "")
                                wrapMode: Text.Wrap
                                elide: Text.ElideRight
                                maximumLineCount: 2
                                font.pointSize: Kirigami.Theme.smallFont.pointSize
                            }

                            Controls.Label {
                                Layout.fillWidth: true
                                visible: String(listRow.modelData.description ?? "").length > 0
                                text: String(listRow.modelData.description ?? "")
                                color: Kirigami.Theme.disabledTextColor
                                wrapMode: Text.Wrap
                                elide: Text.ElideRight
                                maximumLineCount: 2
                                font.pointSize: Kirigami.Theme.smallFont.pointSize
                            }
                        }
                    }
                }
            }
        }

        // A carousel. Several cards arrived as one message, so they scroll
        // sideways rather than stacking: stacking them would make one message
        // as tall as five.
        //
        // Behind a Loader rather than merely hidden, because a Flickable in a
        // layout is measured whether or not it is drawn, and almost no business
        // message is a carousel. The strip's height is fixed, so what the row
        // reserves does not depend on which slide happens to be showing.
        Loader {
            id: slides

            objectName: "interactiveCarousel"
            Layout.fillWidth: true
            Layout.preferredHeight: root.slideHeight
            // Both, and neither alone. `active` is what stops a Flickable
            // being built on the cards that are not carousels; `visible` is
            // what stops the layout reserving a slide's height for it anyway,
            // which is a strip of empty card sitting under the words.
            active: root.isCarousel
            visible: root.isCarousel

            sourceComponent: ListView {
                id: strip

                model: root.cards
                orientation: ListView.Horizontal
                spacing: Kirigami.Units.smallSpacing
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                interactive: !root.row.selectionModeActive
                // Room under the slides for the bar, so a thumb never lies
                // across the bottom of the last one.
                bottomMargin: Kirigami.Units.smallSpacing

                Controls.ScrollBar.horizontal: DiscreetScrollBar {
                    orientation: Qt.Horizontal
                }

                // A touchpad gives horizontal deltas of its own, and a mouse
                // wheel gives none: over a strip that only goes sideways, a
                // vertical notch means "move along it". Taking both is the
                // difference between a carousel you can see the end of and one
                // whose last card nobody knows is there.
                WheelHandler {
                    acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
                    property: "contentX"
                    orientation: Qt.Horizontal | Qt.Vertical
                    // A strip already showing every card has nothing to move,
                    // and a notch it swallowed anyway would be a pointer
                    // resting on a carousel pinning the whole transcript.
                    enabled: strip.contentWidth > strip.width
                }

                delegate: Rectangle {
                    id: slide

                    required property var modelData

                    readonly property var slideButtons: slide.modelData.buttons ?? []
                    readonly property string slideThumbnail: String(slide.modelData.thumbnail_path ?? "")

                    width: root.slideWidth
                    height: root.slideHeight
                    radius: root.cornerRadius
                    color: Qt.alpha(Kirigami.Theme.textColor, 0.05)

                    Image {
                        id: slideSource

                        visible: false
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: parent.top
                        height: Math.round(slide.width / 1.6)
                        source: slide.slideThumbnail.length > 0
                            ? Whatevr.ProtocolController.localFileUrl(slide.slideThumbnail)
                            : ""
                        fillMode: Image.PreserveAspectCrop
                        asynchronous: true
                        cache: true
                    // No sourceSize: this is the small JPEG that arrived
                    // inside the message, and pinning the decode to the slot's
                    // width would upscale a hundred-pixel picture into a
                    // megapixel one, again on every relayout. Decoding it at
                    // its own size and letting the scene graph scale it up is
                    // both cheaper and no blurrier.
                    }

                    RoundedImage {
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: parent.top
                        height: slideSource.height
                        visible: slideSource.status === Image.Ready
                        source: slideSource
                        // Every card in a carousel is the same shape whatever
                        // shape its picture is, so crop to fill rather than
                        // squash to fit.
                        sourceRect: coverRect(width, height,
                                              slideSource.implicitWidth,
                                              slideSource.implicitHeight)
                        topLeftRadius: root.cornerRadius
                        topRightRadius: root.cornerRadius
                    }

                    ColumnLayout {
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.top: parent.top
                        anchors.topMargin: slideSource.height + root.contentMargin
                        anchors.margins: root.contentMargin
                        spacing: Kirigami.Units.smallSpacing / 2

                        Controls.Label {
                            Layout.fillWidth: true
                            text: String(slide.modelData.title ?? "")
                            elide: Text.ElideRight
                            maximumLineCount: 1
                            font.pointSize: Kirigami.Theme.smallFont.pointSize
                            font.bold: true
                        }

                        Controls.Label {
                            Layout.fillWidth: true
                            text: String(slide.modelData.subtitle ?? "").length > 0
                                ? String(slide.modelData.subtitle ?? "")
                                : String(slide.modelData.body ?? "")
                            color: Kirigami.Theme.disabledTextColor
                            wrapMode: Text.Wrap
                            elide: Text.ElideRight
                            maximumLineCount: 2
                            font.pointSize: Kirigami.Theme.smallFont.pointSize
                        }

                        ActionRow {
                            Layout.fillWidth: true
                            visible: slide.slideButtons.length > 0
                            button: slide.slideButtons.length > 0 ? slide.slideButtons[0] : ({})
                            text: slide.slideButtons.length > 0 ? String(slide.slideButtons[0].label ?? "") : ""
                        }
                    }
                }
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            visible: root.footerText.length > 0
            text: root.footerText
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 2
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // A hairline between what the message says and what it asks you to do.
        Rectangle {
            Layout.fillWidth: true
            Layout.topMargin: Math.round(Kirigami.Units.smallSpacing / 2)
            visible: root.buttons.length > 0
            implicitHeight: 1
            color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        }

        Repeater {
            model: root.buttons

            ActionRow {
                id: actionRow

                required property var modelData

                Layout.fillWidth: true
                button: actionRow.modelData
                text: String(actionRow.modelData.label ?? "")
            }
        }

        // One line for the whole locked group. A business message's quick
        // replies send a message back, which whatevr cannot do yet, and a
        // button that fails silently is worse than one that says why.
        Controls.Label {
            Layout.fillWidth: true
            visible: root.hasDeadButtons
            text: Whatevr.I18n.i18nc("@info buttons that need the phone app",
                                     "Replying to these needs WhatsApp on your phone")
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            horizontalAlignment: Text.AlignHCenter
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }
}
