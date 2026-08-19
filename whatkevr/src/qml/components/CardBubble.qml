// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick

import org.kde.kirigami as Kirigami

/**
 * The card family's dispatcher.
 *
 * A "card" is a message kind whose bubble is a block of its own with a height
 * its content decides: a location's map, a contact's details, a poll's options.
 * They differ from the voice/audio/document rows, which are a fixed height, and
 * from photos, which are sized by their image.
 *
 * ChatBubble instantiates exactly one Loader for the whole family, and this
 * picks the card inside it. That matters: an inactive Loader costs two objects
 * on every row in the timeline, including the plain text ones, so one Loader
 * per kind would tax every message in every chat for kinds it is not. Adding a
 * kind here is a branch, not another Loader.
 *
 * The contract with ChatBubble: width flows down (the row sets it), height
 * flows up (`implicitHeight`, which the row reads back as cardBlockHeight).
 * A card must never derive its implicitHeight from its own height.
 */
Item {
    id: root

    required property ChatBubble row

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: cardLoader.item ? cardLoader.item.implicitHeight : Kirigami.Units.gridUnit * 12

    /// The width the loaded card would rather have, or 0 for "fill the bubble".
    /// A card opts in by declaring its own `preferredWidth`; one that does not
    /// reports 0 here and keeps the full content width. This is the width half
    /// of the same contract as implicitHeight, and it is deliberately a request
    /// rather than a setting: the row still caps it at what it has to give.
    readonly property real preferredWidth:
        cardLoader.item && cardLoader.item.preferredWidth !== undefined
            ? cardLoader.item.preferredWidth
            : 0

    // The row's own right-click surface stops at the card's edge, so that it
    // cannot take hover away from the buttons and fields inside. Right-clicking
    // a card must still open the message's menu, so it is handed back here.
    // Below everything else in the card, so an action that wants the press
    // (nothing does today, but a map drag would) gets it first.
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.RightButton
        z: -1
        onPressed: mouse => {
            const p = mapToItem(root.row, mouse.x, mouse.y)
            root.row.contextMenuRequested(p.x, p.y)
            mouse.accepted = true
        }
    }

    Loader {
        id: cardLoader

        width: root.width
        sourceComponent: {
            // Keyed on the payload rather than the kind, because a link
            // preview's kind is `text`: the words are still the message and
            // only the card beside them is new.
            if (root.row.isLinkPreview) {
                return linkPreviewCard
            }
            switch (root.row.mediaKind) {
            case "location":
            case "live_location":
                return locationCard
            case "contact":
            case "contacts":
                return contactCard
            case "poll":
                return pollCard
            case "group_invite":
                return groupInviteCard
            case "event":
                return eventCard
            case "album":
                return albumCard
            case "interactive":
                return interactiveCard
            case "product":
            case "order":
            case "payment":
                return commerceCard
            case "sticker_pack":
                return stickerPackCard
            default:
                return null
            }
        }
    }

    Component {
        id: locationCard

        LocationBubble {
            row: root.row
        }
    }

    Component {
        id: contactCard

        ContactCardBubble {
            row: root.row
        }
    }

    Component {
        id: pollCard

        PollBubble {
            row: root.row
        }
    }

    Component {
        id: groupInviteCard

        GroupInviteBubble {
            row: root.row
        }
    }

    Component {
        id: eventCard

        EventBubble {
            row: root.row
        }
    }

    Component {
        id: albumCard

        AlbumBubble {
            row: root.row
        }
    }

    Component {
        id: linkPreviewCard

        LinkPreviewCard {
            row: root.row
        }
    }

    Component {
        id: interactiveCard

        InteractiveBubble {
            row: root.row
        }
    }

    Component {
        id: commerceCard

        CommerceCardBubble {
            row: root.row
        }
    }

    Component {
        id: stickerPackCard

        StickerPackBubble {
            row: root.row
        }
    }
}
