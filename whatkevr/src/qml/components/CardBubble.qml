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

    Loader {
        id: cardLoader

        width: root.width
        sourceComponent: {
            switch (root.row.mediaKind) {
            case "location":
            case "live_location":
                return locationCard
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
}
