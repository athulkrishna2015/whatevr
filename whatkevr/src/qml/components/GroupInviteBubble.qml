// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * An invitation to a group.
 *
 * The message itself carries only the sender's snapshot: a name their client
 * held at the moment they hit share, a thumbnail, a code and an expiry. The
 * daemon resolves that code against WhatsApp, so this card shows the group's
 * real subject, its topic, how many people are in it, and the one thing a
 * phone's invite card never tells you: whether you are already a member.
 *
 * That last fact is what decides the button. There is nothing to join in a
 * group you are already in, so the card offers to open the chat instead of
 * offering an action that would do nothing.
 *
 * The expiry is a live countdown rather than a timestamp, because a code that
 * lapses while you are looking at it should say so, and a card whose door has
 * closed should stop offering to walk you through it.
 */
Item {
    id: root

    objectName: "groupInviteBubble"

    required property ChatBubble row

    readonly property var invite: row.invite ?? ({})

    readonly property string groupJID: String(invite.group_jid ?? "")
    readonly property string subject: String(invite.subject ?? "")
    readonly property string senderName: String(invite.name ?? "")
    readonly property string topic: String(invite.topic ?? "")
    readonly property string photoPath: String(invite.photo_path ?? "")
    readonly property int memberCount: invite.member_count ?? 0
    readonly property int expiresAt: invite.expires_at ?? 0
    readonly property bool joined: invite.joined ?? false
    readonly property bool resolved: (invite.resolved_at ?? 0) > 0
    readonly property string resolveError: String(invite.resolve_error ?? "")

    /// Padding between the card's edge and its content, on every side, counted
    /// twice in the height: a card that reports only its content's height puts
    /// its last row over its own bottom edge.
    readonly property real contentMargin: Kirigami.Units.largeSpacing

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: content.implicitHeight + contentMargin * 2

    /// The best name the card has: what the lookup said, else the sender's copy.
    readonly property string displayName: {
        if (subject.length > 0)
            return subject
        if (senderName.length > 0)
            return senderName
        return Whatevr.I18n.i18nc("@label a group invite with no name", "Group")
    }

    readonly property bool expired: expiresAt > 0 && clock.now >= expiresAt

    /// The door is closed: nothing to press, and the card says why.
    readonly property bool actionable: !expired && groupJID.length > 0

    function initialsFor(name) {
        const trimmed = String(name ?? "").trim()
        if (trimmed.length === 0)
            return "?"
        const words = trimmed.split(/\s+/)
        return words.length === 1
            ? words[0].charAt(0).toUpperCase()
            : (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase()
    }

    /// How long the code has left, phrased at the coarseness a reader needs.
    /// Days for a fresh invite, minutes for one about to lapse: "expires in
    /// 4320 minutes" is a number, not an answer.
    readonly property string remainingText: {
        if (expiresAt <= 0 || expired)
            return ""
        const seconds = expiresAt - clock.now
        if (seconds < 60)
            return Whatevr.I18n.i18nc("@label group invite expiry", "Expires in under a minute")
        const minutes = Math.floor(seconds / 60)
        if (minutes < 60)
            return Whatevr.I18n.i18ncp("@label group invite expiry", "Expires in %1 minute", "Expires in %1 minutes", minutes)
        const hours = Math.floor(minutes / 60)
        if (hours < 24)
            return Whatevr.I18n.i18ncp("@label group invite expiry", "Expires in %1 hour", "Expires in %1 hours", hours)
        return Whatevr.I18n.i18ncp("@label group invite expiry", "Expires in %1 day", "Expires in %1 days", Math.floor(hours / 24))
    }

    /// The one line under the name, in the order a reader needs it: what has
    /// gone wrong, then where you already stand, then how long you have.
    readonly property string statusText: {
        if (joined)
            return Whatevr.I18n.i18nc("@label group invite state", "You are already in this group")
        if (expired)
            return Whatevr.I18n.i18nc("@label group invite state", "This invite has expired")
        if (resolveError.length > 0)
            return Whatevr.I18n.i18nc("@label group invite state", "This invite could not be checked")
        return remainingText
    }

    readonly property string memberText:
        memberCount > 0
            ? Whatevr.I18n.i18ncp("@label group size", "%1 member", "%1 members", memberCount)
            : ""

    readonly property bool showsTopic: topic.length > 0
    readonly property bool showsAction: actionable && !row.selectionModeActive

    // The row draws the time and ticks over the bottom right of the block, so
    // whichever line the card ends on has to leave room for them. Which line
    // that is changes with the invite's state, so the reserve follows it: with
    // an action row the spacer beside the button carries it, and without one it
    // falls to the topic, or to the subtitle when there is no topic either.
    readonly property real subtitleReserve: showsTopic || showsAction ? 0 : row.tntReserveWidth
    readonly property real topicReserve: showsAction ? 0 : row.tntReserveWidth

    /// The width this card would rather be. A few lines and a button do not
    /// need the whole bubble, and taking it leaves the action stranded at the
    /// far left of a mostly empty plate. The row caps this at what it has.
    readonly property real preferredWidth: content.implicitWidth + contentMargin * 2

    readonly property real avatarSize: Kirigami.Units.gridUnit * 2.2
    /// Distance from the card's content edge to the text column. Everything
    /// below the picture lines up on this, so the card reads as two columns
    /// rather than as a header with an unrelated button under it.
    readonly property real textColumnX: avatarSize + Kirigami.Units.smallSpacing

    QtObject {
        id: clock

        property int now: Math.floor(Date.now() / 1000)
    }

    // A card counting down in days does not need a tick a second, and one about
    // to lapse does. The interval follows what the card is actually saying, so
    // the text is never more than one unit stale and never costs more than the
    // unit it shows.
    Timer {
        interval: root.expiresAt - clock.now < 3600 ? 1000 : 60000
        running: root.expiresAt > 0 && !root.expired && root.row.activeInViewport
        repeat: true
        onTriggered: clock.now = Math.floor(Date.now() / 1000)
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

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            AvatarImage {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: root.avatarSize
                implicitHeight: implicitWidth
                // The invite carries the group's picture inline, so there is
                // nothing to fetch and nothing to tell the server about who
                // was offered which group.
                avatarLocalPath: root.photoPath
                initials: root.initialsFor(root.displayName)
                // An expired invite is a record, not an offer, and the whole
                // card reads that way.
                opacity: root.expired ? 0.6 : 1
            }

            // Only the two lines the picture is meant to sit beside. Putting
            // the topic and the action in here as well made the column three
            // and four lines tall, and the picture, centred on all of it,
            // drifted down until it was level with the subtitle instead of the
            // name. They are indented to this column's left edge instead.
            ColumnLayout {
                Layout.fillWidth: true
                spacing: 0

                Controls.Label {
                    Layout.fillWidth: true
                    text: root.displayName
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: root.row.bodyPointSize
                    font.bold: true
                    opacity: root.expired ? 0.6 : 1
                }

                Controls.Label {
                    objectName: "inviteSubtitle"

                    Layout.fillWidth: true
                    Layout.rightMargin: root.subtitleReserve
                    visible: text.length > 0
                    text: {
                        // Two facts, one line, joined only when both are there:
                        // a lone separator on a card that resolved nothing is
                        // punctuation with nothing to punctuate.
                        const parts = []
                        if (root.memberText.length > 0)
                            parts.push(root.memberText)
                        if (root.statusText.length > 0)
                            parts.push(root.statusText)
                        return parts.join(" · ")
                    }
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        // The group's own description, when the lookup found one. It is the
        // best evidence a reader has for whether they want in at all.
        Controls.Label {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            Layout.leftMargin: root.textColumnX
            Layout.rightMargin: root.topicReserve
            visible: root.showsTopic
            text: root.topic
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 3
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            // Indented to the text column, then pulled back by the button's own
            // padding, so its glyph starts on the same edge as the name above
            // it. Left at the card's edge it starts under the picture and lines
            // up with nothing.
            Layout.leftMargin: root.textColumnX - Kirigami.Units.smallSpacing
            visible: root.showsAction
            spacing: 0

            CardActionButton {
                text: root.joined
                    ? Whatevr.I18n.i18nc("@action open the chat for a group already joined", "Open chat")
                    : Whatevr.I18n.i18nc("@action accept a group invitation", "Join group")
                // A speech balloon rather than a chevron. A chevron is a thin
                // mark drawn in the middle of its box, so next to a name it
                // reads as indented even when its box is exactly aligned, and
                // it says "forward" rather than "this is a conversation".
                iconName: root.joined ? "view-conversation-balloon-symbolic" : "list-add-symbolic"
                onClicked: Whatevr.ProtocolController.joinGroupInvite(root.row.messageId)
            }

            // Where the row's time and ticks land when the card ends on its
            // action. A minimum rather than a plain filler, because this card
            // asks to be only as wide as its content: with nothing claiming the
            // space, the card would shrink to the button and the timestamp
            // would sit on top of it.
            Item {
                Layout.fillWidth: true
                Layout.minimumWidth: root.row.tntReserveWidth
            }
        }
    }
}
