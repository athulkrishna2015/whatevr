// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * Something the chat did to itself: somebody joined, the subject changed, the
 * disappearing timer moved, a security code changed.
 *
 * Like a call log it is not a message anybody wrote, so it draws as a pill in
 * the middle rather than in a plate on one side. Unlike a call log its sentence
 * can be long, so this one wraps: "Cy added Ana, Bo, Cy2 and 37 others" has to
 * be readable at any window width, and eliding it would cut off the part that
 * says what happened.
 *
 * The words are composed here, from the flags the daemon put on the row, for
 * the same reason the call log's are: the daemon writes English and does not
 * know who is reading. It composes nothing of its own; `system.text` is the
 * daemon's own sentence and stands in whenever this build meets an event type
 * it has never heard of.
 */
Item {
    id: root

    objectName: "systemPill"

    required property ChatBubble row

    readonly property var event: row.system ?? ({})

    readonly property string eventType: String(event.type ?? "")
    readonly property var actor: event.actor ?? null
    readonly property var names: event.names ?? []
    readonly property int overflow: event.overflow ?? 0
    readonly property string value: String(event.value ?? "")
    readonly property bool on: event.on ?? false
    readonly property int seconds: event.seconds ?? 0
    readonly property bool aboutSelf: event.about_self ?? false

    /// One person, as the sentence names them.
    function personName(person: var): string {
        if (!person) {
            return ""
        }
        if (person.self === true) {
            return Whatevr.I18n.i18nc("@label a system event naming the reader, mid-sentence", "you")
        }
        const name = String(person.name ?? "")
        return name.length > 0
            ? name
            : Whatevr.I18n.i18nc("@label a person a system event named but could not identify", "someone")
    }

    /// The people the event named: up to three of them, then a count of the
    /// rest. `capitalized` decides whether a leading reader reads "You" or
    /// "you", which is the difference between "You joined" and "Ana added you".
    function peopleList(capitalized: bool): string {
        const parts = []
        for (let i = 0; i < root.names.length; i++) {
            const person = root.names[i]
            if (capitalized && i === 0 && person && person.self === true) {
                parts.push(Whatevr.I18n.i18nc("@label a system event naming the reader, at the start of a sentence", "You"))
            } else {
                parts.push(root.personName(person))
            }
        }
        if (parts.length === 0) {
            return ""
        }
        if (root.overflow > 0) {
            return Whatevr.I18n.i18ncp("@label %1 is a list of names and %2 the number of people it left out",
                                       "%1 and one other", "%1 and %2 others",
                                       root.overflow, parts.join(", "))
        }
        if (parts.length === 1) {
            return parts[0]
        }
        const last = parts.pop()
        return Whatevr.I18n.i18nc("@label a list of names, %1 all but the last and %2 the last",
                                  "%1 and %2", parts.join(", "), last)
    }

    /// Who did it, as the subject of a sentence, or "" when the server named
    /// nobody. A join through an invite link has no author: nobody added them.
    readonly property string actorLabel: {
        if (!root.actor) {
            return ""
        }
        if (root.actor.self === true) {
            return Whatevr.I18n.i18nc("@label the reader as the subject of a system event", "You")
        }
        return String(root.actor.name ?? "")
    }

    /// A membership change where the only person named is the one who made it:
    /// somebody joining or leaving under their own steam, rather than being
    /// added or removed by anybody.
    readonly property bool actedOnSelf: {
        if (!root.actor) {
            return true
        }
        return root.names.length === 1 && root.overflow === 0
            && root.names[0] && root.names[0].jid === root.actor.jid
    }

    /// The disappearing-message timer, as WhatsApp offers it.
    function timerLabel(secs: int): string {
        if (secs <= 0) {
            return Whatevr.I18n.i18nc("@label disappearing message timer", "off")
        }
        const days = Math.round(secs / 86400)
        if (days >= 1 && secs % 86400 === 0) {
            return Whatevr.I18n.i18ncp("@label disappearing message timer in days", "%1 day", "%1 days", days)
        }
        const hours = Math.round(secs / 3600)
        if (hours >= 1 && secs % 3600 === 0) {
            return Whatevr.I18n.i18ncp("@label disappearing message timer in hours", "%1 hour", "%1 hours", hours)
        }
        return Whatevr.I18n.i18ncp("@label disappearing message timer in minutes", "%1 minute", "%1 minutes",
                                   Math.max(1, Math.round(secs / 60)))
    }

    /// Either "<actor> did something" or, with nobody to credit, a sentence
    /// with no subject at all.
    function bySomebody(withActor: string, authorless: string): string {
        return root.actorLabel.length > 0 ? withActor : authorless
    }

    readonly property string summary: {
        const who = root.peopleList(false)
        const whoCapitalized = root.peopleList(true)
        const actor = root.actorLabel

        switch (root.eventType) {
        case "group_join":
            if (root.actedOnSelf) {
                return Whatevr.I18n.i18nc("@label group event", "%1 joined", whoCapitalized)
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event, %1 who did it and %2 who was added",
                                                      "%1 added %2", actor, who),
                                   Whatevr.I18n.i18nc("@label group event", "%1 joined", whoCapitalized))
        case "group_leave":
            if (root.actedOnSelf) {
                return Whatevr.I18n.i18nc("@label group event", "%1 left", whoCapitalized)
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event, %1 who did it and %2 who was removed",
                                                      "%1 removed %2", actor, who),
                                   Whatevr.I18n.i18nc("@label group event", "%1 left", whoCapitalized))
        case "group_promote":
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event, %1 who did it and %2 who was promoted",
                                                      "%1 made %2 an admin", actor, who),
                                   Whatevr.I18n.i18nc("@label group event", "%1 is now an admin", whoCapitalized))
        case "group_demote":
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event, %1 who did it and %2 who was demoted",
                                                      "%1 removed %2 as admin", actor, who),
                                   Whatevr.I18n.i18nc("@label group event", "%1 is no longer an admin", whoCapitalized))
        case "group_name":
            if (root.value.length === 0) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 changed the group name", actor),
                                       Whatevr.I18n.i18nc("@label group event", "The group name changed"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event, %1 who did it and %2 the new name",
                                                      "%1 changed the group name to “%2”", actor, root.value),
                                   Whatevr.I18n.i18nc("@label group event, %1 the new name",
                                                      "The group name changed to “%1”", root.value))
        case "group_topic":
            if (root.value.length === 0) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 removed the group description", actor),
                                       Whatevr.I18n.i18nc("@label group event", "The group description was removed"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 changed the group description", actor),
                                   Whatevr.I18n.i18nc("@label group event", "The group description changed"))
        case "group_photo":
            if (root.on) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 changed the group photo", actor),
                                       Whatevr.I18n.i18nc("@label group event", "The group photo changed"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 removed the group photo", actor),
                                   Whatevr.I18n.i18nc("@label group event", "The group photo was removed"))
        case "group_locked":
            if (root.on) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 restricted editing the group info to admins", actor),
                                       Whatevr.I18n.i18nc("@label group event", "Only admins can edit the group info"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 allowed everyone to edit the group info", actor),
                                   Whatevr.I18n.i18nc("@label group event", "Everyone can edit the group info"))
        case "group_announce":
            if (root.on) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 restricted messages to admins", actor),
                                       Whatevr.I18n.i18nc("@label group event", "Only admins can send messages"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 allowed everyone to send messages", actor),
                                   Whatevr.I18n.i18nc("@label group event", "Everyone can send messages"))
        case "group_approval":
            if (root.on) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 turned on approval for new members", actor),
                                       Whatevr.I18n.i18nc("@label group event", "New members now need approval"))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 turned off approval for new members", actor),
                                   Whatevr.I18n.i18nc("@label group event", "New members no longer need approval"))
        case "group_invite_link":
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 reset the group invite link", actor),
                                   Whatevr.I18n.i18nc("@label group event", "The group invite link was reset"))
        case "group_delete":
            return root.bySomebody(Whatevr.I18n.i18nc("@label group event", "%1 deleted the group", actor),
                                   Whatevr.I18n.i18nc("@label group event", "The group was deleted"))
        case "ephemeral":
            if (root.on) {
                return root.bySomebody(Whatevr.I18n.i18nc("@label chat event, %1 who did it and %2 the timer",
                                                          "%1 turned on disappearing messages (%2)",
                                                          actor, root.timerLabel(root.seconds)),
                                       Whatevr.I18n.i18nc("@label chat event, %1 the timer",
                                                          "Disappearing messages are on (%1)",
                                                          root.timerLabel(root.seconds)))
            }
            return root.bySomebody(Whatevr.I18n.i18nc("@label chat event", "%1 turned off disappearing messages", actor),
                                   Whatevr.I18n.i18nc("@label chat event", "Disappearing messages are off"))
        case "identity_change":
            if (who.length === 0) {
                return Whatevr.I18n.i18nc("@label chat event", "Your security code changed")
            }
            return Whatevr.I18n.i18nc("@label chat event, %1 the other person",
                                      "Your security code with %1 changed", who)
        }
        // An event type this build has never heard of. The daemon already wrote
        // a sentence for it, and printing that beats printing nothing.
        return String(root.event.text ?? "")
    }

    /// The glyph names the family of change at a glance: people for who is in
    /// the chat, a clock for how long messages last, a shield for the security
    /// code, a picture for the photo.
    readonly property string glyph: {
        switch (root.eventType) {
        case "group_join":
        case "group_leave":
        case "group_promote":
        case "group_demote":
        case "group_approval":
            return "system-users-symbolic"
        case "group_name":
        case "group_topic":
            return "document-edit-symbolic"
        case "group_photo":
            return "insert-image-symbolic"
        case "group_locked":
            return root.on ? "object-locked-symbolic" : "object-unlocked-symbolic"
        case "group_announce":
            return "irc-voice-symbolic"
        case "group_invite_link":
        case "group_link":
        case "group_unlink":
            return "link-symbolic"
        case "group_delete":
            return "edit-delete-symbolic"
        case "ephemeral":
            return "clock-symbolic"
        case "identity_change":
            return "security-medium-symbolic"
        }
        return "dialog-information-symbolic"
    }

    /// An event that named the reader is the one worth noticing in a run of
    /// them, so its glyph takes the accent colour. The words stay plain: this
    /// is a pill, not a warning.
    readonly property color accent: root.aboutSelf
        ? Kirigami.Theme.highlightColor
        : Kirigami.Theme.disabledTextColor

    readonly property real vPadding: Math.round(Kirigami.Units.smallSpacing * 0.75)
    /// A fully rounded pill pads its sides to its own height, not to a flat
    /// unit, or the text sits in the flat middle with the round ends crowding
    /// it. Measured from one line's worth of height, so a pill that wraps to
    /// three lines does not grow absurd ends.
    readonly property real hPadding: Math.round(lineHeight * 0.42)
    readonly property real lineHeight: Math.max(glyphSize, Math.ceil(summaryMetrics.height)) + vPadding * 2

    readonly property real glyphSize: Kirigami.Units.iconSizes.small
    readonly property real innerSpacing: Kirigami.Units.smallSpacing

    /// The widest a pill may get. A sentence longer than this wraps rather than
    /// running the full width of the window, which is what keeps a run of them
    /// reading as a column of pills rather than as paragraphs.
    readonly property real maxPillWidth: Math.max(Kirigami.Units.gridUnit * 8,
                                                  Math.min(row.listWidth - Kirigami.Units.largeSpacing * 2,
                                                           Kirigami.Units.gridUnit * 26))
    readonly property real chromeWidth: hPadding * 2 + glyphSize + innerSpacing
                                        + innerSpacing + Math.ceil(timeMetrics.advanceWidth)
    readonly property real textWidth: Math.min(Math.ceil(summaryMetrics.advanceWidth),
                                               Math.max(1, maxPillWidth - chromeWidth))

    implicitWidth: Math.min(maxPillWidth, chromeWidth + textWidth)
    implicitHeight: Math.max(glyphSize, Math.ceil(summaryLabel.implicitHeight)) + vPadding * 2

    width: implicitWidth
    height: implicitHeight

    TextMetrics {
        id: summaryMetrics

        font: summaryLabel.font
        text: root.summary
    }

    TextMetrics {
        id: timeMetrics

        font: timeLabel.font
        text: root.row.timeText
    }

    // The same plate the day separator and the call log wear, for the same
    // reason: a pill on the wallpaper needs something opaque under it or the
    // doodles read straight through the words.
    Rectangle {
        anchors.fill: parent
        radius: root.lineHeight / 2
        color: Qt.alpha(Kirigami.Theme.backgroundColor, 0.9)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    Kirigami.Icon {
        id: glyphIcon

        anchors.left: parent.left
        anchors.leftMargin: root.hPadding
        anchors.top: parent.top
        anchors.topMargin: root.vPadding + Math.max(0, Math.round((Math.ceil(summaryMetrics.height) - root.glyphSize) / 2))
        implicitWidth: root.glyphSize
        implicitHeight: root.glyphSize
        source: root.glyph
        color: root.accent
        isMask: true
    }

    Controls.Label {
        id: summaryLabel

        anchors.left: glyphIcon.right
        anchors.leftMargin: root.innerSpacing
        anchors.right: timeLabel.left
        anchors.rightMargin: root.innerSpacing
        anchors.top: parent.top
        anchors.topMargin: root.vPadding
        text: root.summary
        wrapMode: Text.Wrap
        maximumLineCount: 3
        elide: Text.ElideRight
        color: Kirigami.Theme.textColor
        font.pointSize: Kirigami.Theme.smallFont.pointSize
    }

    // The time, on the pill. "When was I added" and "when did this code change"
    // are the questions a system event answers, and a day separator only
    // answers half of each.
    //
    // It sits on the last line rather than the first, which is where a bubble's
    // footer puts it and where it reads as the end of the sentence. On the
    // common single-line pill the two are the same position.
    Controls.Label {
        id: timeLabel

        anchors.right: parent.right
        anchors.rightMargin: root.hPadding
        anchors.bottom: parent.bottom
        anchors.bottomMargin: root.vPadding
        text: root.row.timeText
        color: Kirigami.Theme.disabledTextColor
        maximumLineCount: 1
        font.pointSize: Kirigami.Theme.smallFont.pointSize
    }
}
