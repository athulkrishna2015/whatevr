// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A shared contact.
 *
 * Collapsed it is what you need to decide whether you care: avatar, name, and
 * the primary number. Expanded it is every field the vCard carried, each one
 * actionable in the way that field deserves, because a desktop can do that: a
 * number dials through `tel:`, an address opens the map, an email opens the
 * composer, a URL opens the browser, and anything can be copied.
 *
 * The "on WhatsApp" badge is free. WhatsApp hangs a `waid=` parameter off the
 * numbers it knows, so the daemon can say which ones are reachable without a
 * single lookup, which also means we never tell the server which contacts
 * somebody forwarded us.
 *
 * Several contacts arrive as one message, so they render as a stack that
 * expands into the list rather than as one bubble per person.
 */
Item {
    id: root

    required property ChatBubble row

    readonly property var payload: row.contacts ?? ({})
    readonly property var cards: payload.cards ?? []
    readonly property int cardCount: cards.length
    readonly property bool isStack: cardCount > 1

    /// Collapsed by default: a card is a summary until you ask for the detail.
    property bool expanded: false

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: content.implicitHeight

    function cardAt(index) {
        return index >= 0 && index < cards.length ? cards[index] : ({})
    }

    /** The number the card leads with: one WhatsApp vouched for, else the first. */
    function primaryPhone(card) {
        const phones = card.phones ?? []
        for (let i = 0; i < phones.length; ++i) {
            if (String(phones[i].jid ?? "").length > 0)
                return phones[i]
        }
        return phones.length > 0 ? phones[0] : null
    }

    function initialsFor(name) {
        const trimmed = String(name ?? "").trim()
        if (trimmed.length === 0)
            return ""
        const words = trimmed.split(/\s+/)
        if (words.length === 1)
            return words[0].charAt(0).toUpperCase()
        return (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase()
    }

    Rectangle {
        anchors.fill: parent
        radius: Kirigami.Units.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    ColumnLayout {
        id: content

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Kirigami.Units.smallSpacing
        spacing: Kirigami.Units.smallSpacing

        // The header names what arrived. For a single contact it is the person;
        // for several it is the count, and the names are in the list below.
        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            AvatarImage {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.gridUnit * 2.2
                implicitHeight: implicitWidth
                visible: !root.isStack
                // A shared card carries no avatar: the sender's vCard has no
                // picture and looking one up would tell the server whose
                // contacts got forwarded. Initials are the honest answer.
                initials: root.isStack ? "?" : root.initialsFor(root.cardAt(0).display_name)
            }

            Kirigami.Icon {
                Layout.alignment: Qt.AlignVCenter
                visible: root.isStack
                implicitWidth: Kirigami.Units.gridUnit * 2.2
                implicitHeight: implicitWidth
                source: "group-symbolic"
                fallback: "system-users-symbolic"
                color: Kirigami.Theme.disabledTextColor
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.rightMargin: root.row.tntReserveWidth
                spacing: 0

                Controls.Label {
                    Layout.fillWidth: true
                    text: root.isStack
                        ? Whatevr.I18n.i18ncp("@title shared contact cards", "%1 contacts", "%1 contacts", root.cardCount)
                        : String(root.cardAt(0).display_name ?? "")
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: root.row.bodyPointSize
                    font.bold: true
                }

                Controls.Label {
                    Layout.fillWidth: true
                    visible: text.length > 0
                    text: {
                        if (root.isStack)
                            return String(root.payload.display_name ?? "")
                        const card = root.cardAt(0)
                        const org = String(card.org ?? "")
                        const title = String(card.title ?? "")
                        if (title.length > 0 && org.length > 0)
                            return title + " · " + org
                        if (title.length > 0)
                            return title
                        if (org.length > 0)
                            return org
                        const phone = root.primaryPhone(card)
                        return phone ? String(phone.value ?? "") : ""
                    }
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        // Every card, each with its own fields. A single contact shows its
        // primary action collapsed and everything when expanded; a stack shows
        // one row per person until expanded.
        Repeater {
            model: root.expanded ? root.cardCount : Math.min(root.cardCount, root.isStack ? 3 : 1)

            delegate: ContactCardEntry {
                required property int index

                Layout.fillWidth: true
                card: root.cardAt(index)
                compact: root.isStack && !root.expanded
                expanded: root.expanded
                selectionModeActive: root.row.selectionModeActive
            }
        }

        // The affordance to see the rest. A single contact with nothing beyond
        // its primary number has nothing to expand into, so it says nothing.
        Controls.ToolButton {
            Layout.fillWidth: true
            visible: root.isStack || root.hasHiddenDetail
            text: root.expanded
                ? Whatevr.I18n.i18nc("@action collapse a shared contact card", "Show less")
                : (root.isStack && root.cardCount > 3
                    ? Whatevr.I18n.i18ncp("@action expand a stack of shared contacts",
                                          "Show all %1 contacts", "Show all %1 contacts", root.cardCount)
                    : Whatevr.I18n.i18nc("@action expand a shared contact card", "Show details"))
            font.pointSize: Kirigami.Theme.smallFont.pointSize
            onClicked: root.expanded = !root.expanded
        }
    }

    /** Whether a single card carries anything past the line already shown. */
    readonly property bool hasHiddenDetail: {
        if (isStack)
            return false
        const card = cardAt(0)
        const phones = card.phones ?? []
        const emails = card.emails ?? []
        const urls = card.urls ?? []
        const addresses = card.addresses ?? []
        return phones.length + emails.length + urls.length + addresses.length > 1
    }
}
