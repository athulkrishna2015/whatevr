// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * Every answer an event has, in full.
 *
 * The card can only show three faces per chip, which is enough to recognise a
 * plan and not enough to plan around it. This is the rest: each answer with
 * everyone who gave it, who is bringing guests and how many, and when each
 * person answered, which is the difference between "four are coming" and
 * "four were coming as of Tuesday".
 *
 * The order is the daemon's, oldest answer first, and nothing here re-sorts it.
 * The only regrouping is by answer, which is what the reader came for.
 */
CenteredDialog {
    id: root

    /// The `event` object straight off the message item.
    property var plan: ({})
    property string messageId: ""
    /// Narrowed to one answer ("going", "maybe", "not_going"), or empty for all
    /// three. Tapping a chip opens the answer it stands for.
    property string filterResponse: ""

    readonly property var responders: plan.responders ?? []
    /// Heads rather than answers: somebody bringing two guests is three people
    /// at the door, and the difference is the number a host is planning for.
    readonly property int goingCount: plan.going_count ?? 0

    /// Our own answer, in flight or confirmed, exactly as the card reads it, so
    /// opening the list straight after answering shows the answer you just gave
    /// rather than the one the daemon has not echoed yet.
    readonly property var pendingRSVP: Whatevr.ProtocolController.pendingEventRSVPs[messageId]
    readonly property bool selfPending: pendingRSVP !== undefined && pendingRSVP !== null
    readonly property string selfResponse: selfPending
        ? String(pendingRSVP.response)
        : String(plan.self_response ?? "")

    readonly property var answers: [
        {"value": "going", "label": Whatevr.I18n.i18nc("@title:group rsvp", "Going"),
         "icon": "checkmark-symbolic"},
        {"value": "maybe", "label": Whatevr.I18n.i18nc("@title:group rsvp", "Maybe"),
         "icon": "question-symbolic"},
        {"value": "not_going", "label": Whatevr.I18n.i18nc("@title:group rsvp", "Can't go"),
         "icon": "dialog-close"},
    ]

    readonly property var visibleAnswers: filterResponse.length === 0
        ? answers
        : answers.filter(answer => answer.value === filterResponse)

    /// Everyone who gave one answer, in the order the daemon holds them. Our own
    /// pending answer is folded in here and the one it replaced is dropped, for
    /// the same reason the card corrects its counts: the alternative is the list
    /// disagreeing with the chips for as long as the send takes.
    function peopleWhoAnswered(response) {
        const confirmed = String(plan.self_response ?? "")
        const changed = selfResponse !== confirmed
        const out = []
        for (let i = 0; i < responders.length; ++i) {
            const responder = responders[i]
            if (changed && Boolean(responder.from_me)) {
                continue
            }
            if (String(responder.response) === response) {
                out.push(responder)
            }
        }
        if (changed && response === selfResponse) {
            out.push({
                "jid": "me",
                "from_me": true,
                "response": selfResponse,
                "extra_guests": selfPending ? Number(pendingRSVP.extra_guests ?? 0) : 0,
                "timestamp": Math.floor(Date.now() / 1000),
            })
        }
        return out
    }

    readonly property int answeredCount: peopleWhoAnswered("going").length
        + peopleWhoAnswered("maybe").length
        + peopleWhoAnswered("not_going").length

    title: Whatevr.I18n.i18nc("@title:dialog who answered an event", "Who is coming")
    standardButtons: Kirigami.Dialog.Close
    padding: Kirigami.Units.largeSpacing
    preferredWidth: Kirigami.Units.gridUnit * 24
    maximumHeight: Kirigami.Units.gridUnit * 28

    function openFor(planData, msgId, response) {
        plan = planData || ({})
        messageId = msgId
        filterResponse = response === undefined || response === null ? "" : String(response)
        open()
    }

    function nameFor(responder) {
        if (responder.from_me) {
            return Whatevr.I18n.i18nc("@label the account's own rsvp", "You")
        }
        const name = String(responder.name ?? "")
        return name.length > 0
            ? name
            : Whatevr.I18n.i18nc("@label somebody whose name we do not know", "Someone")
    }

    function initialsFor(responder) {
        const name = nameFor(responder).trim()
        if (name.length === 0) {
            return "?"
        }
        const words = name.split(/\s+/)
        return words.length === 1
            ? words[0].charAt(0).toUpperCase()
            : (words[0].charAt(0) + words[words.length - 1].charAt(0)).toUpperCase()
    }

    function formatTimestamp(ts) {
        const value = Number(ts)
        if (!value || value <= 0) {
            return ""
        }
        const date = new Date(value * 1000)
        const today = new Date()
        const sameDay = date.getFullYear() === today.getFullYear()
                        && date.getMonth() === today.getMonth()
                        && date.getDate() === today.getDate()
        return sameDay
            ? Qt.formatTime(date, Qt.locale().timeFormat(Locale.ShortFormat))
            : Qt.formatDateTime(date, Qt.locale().dateTimeFormat(Locale.ShortFormat))
    }

    /// How many people one answer accounts for, guests included. Only worth
    /// saying when it differs from the number of answers.
    function headsFor(response) {
        const people = peopleWhoAnswered(response)
        let heads = people.length
        for (let i = 0; i < people.length; ++i) {
            heads += Number(people[i].extra_guests ?? 0)
        }
        return heads
    }

    component ResponderRow: RowLayout {
        id: responderRow

        required property var responder

        readonly property int guests: Number(responder.extra_guests ?? 0)

        Layout.fillWidth: true
        spacing: Kirigami.Units.smallSpacing

        AvatarImage {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 1.6
            implicitHeight: implicitWidth
            avatarLocalPath: String(responderRow.responder.avatar_path ?? "")
            initials: root.initialsFor(responderRow.responder)
        }

        Label {
            Layout.fillWidth: true
            text: root.nameFor(responderRow.responder)
            elide: Text.ElideRight
            font.bold: Boolean(responderRow.responder.from_me)
        }

        // Guests are the reason a head count and an answer count differ, so the
        // person bringing them says so rather than leaving the totals to argue.
        Rectangle {
            Layout.alignment: Qt.AlignVCenter
            // Its own gap from the time beside it: a pill and a timestamp
            // separated by one unit of ordinary row spacing read as one run of
            // text with a coloured start.
            Layout.rightMargin: Kirigami.Units.smallSpacing
            visible: responderRow.guests > 0
            implicitWidth: guestLabel.implicitWidth + Kirigami.Units.smallSpacing * 2
            implicitHeight: guestLabel.implicitHeight + Kirigami.Units.smallSpacing
            radius: height / 2
            color: Qt.alpha(Whatevr.Palette.highlight, 0.16)

            Label {
                id: guestLabel

                anchors.centerIn: parent
                text: Whatevr.I18n.i18ncp("@label how many extra people somebody is bringing",
                                          "+%1 guest", "+%1 guests", responderRow.guests)
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }
        }

        Label {
            visible: text.length > 0
            text: root.formatTimestamp(responderRow.responder.timestamp)
            color: Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }
    }

    ColumnLayout {
        // Kirigami.Dialog gives a bare Layout no width, which collapses every
        // fillWidth row inside it; pin it like the other dialogs do.
        implicitWidth: root.preferredWidth
        spacing: Kirigami.Units.smallSpacing

        Label {
            Layout.fillWidth: true
            visible: text.length > 0
            text: String(root.plan.name ?? "")
            wrapMode: Text.Wrap
            font.bold: true
        }

        Label {
            Layout.fillWidth: true
            Layout.bottomMargin: Kirigami.Units.smallSpacing
            text: {
                if (root.answeredCount === 0) {
                    return Whatevr.I18n.i18nc("@label an event nobody has answered",
                                              "Nobody has answered yet")
                }
                const answered = Whatevr.I18n.i18ncp("@label how many people answered an event",
                                                     "%1 answered", "%1 answered", root.answeredCount)
                const heads = root.headsFor("going")
                if (heads === 0) {
                    return answered
                }
                // Two numbers, because they are two different questions: how
                // many people replied, and how many are turning up.
                return Whatevr.I18n.i18nc("@label answers, then the head count going",
                                          "%1 · %2", answered,
                                          Whatevr.I18n.i18ncp("@label how many people are coming",
                                                              "%1 going", "%1 going", heads))
            }
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // Narrowing to one answer, when a chip opened us on one. The way back to
        // all three is a button rather than a second trip through the card.
        RowLayout {
            Layout.fillWidth: true
            visible: root.filterResponse.length > 0
            spacing: Kirigami.Units.smallSpacing

            Label {
                text: Whatevr.I18n.i18nc("@label the rsvp list is narrowed to one answer",
                                         "Showing one answer")
                color: Kirigami.Theme.disabledTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }

            Button {
                text: Whatevr.I18n.i18nc("@action:button widen the rsvp list to every answer",
                                         "Show all")
                flat: true
                onClicked: root.filterResponse = ""
            }

            Item {
                Layout.fillWidth: true
            }
        }

        ScrollView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            Layout.topMargin: Kirigami.Units.smallSpacing
            Layout.preferredHeight: Math.min(body.implicitHeight, Kirigami.Units.gridUnit * 18)
            contentWidth: availableWidth
            clip: true

            ColumnLayout {
                id: body

                objectName: "responseSections"

                width: root.preferredWidth - Kirigami.Units.largeSpacing
                spacing: Kirigami.Units.smallSpacing

                Repeater {
                    model: root.visibleAnswers

                    delegate: ColumnLayout {
                        id: answerGroup

                        required property var modelData

                        readonly property var people: root.peopleWhoAnswered(modelData.value)

                        objectName: "responseGroup." + modelData.value

                        Layout.fillWidth: true
                        // Enough air that the next answer's heading reads as a
                        // heading rather than as another name in this one.
                        Layout.bottomMargin: Kirigami.Units.largeSpacing
                        spacing: Kirigami.Units.smallSpacing / 2

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Kirigami.Units.smallSpacing

                            Kirigami.Icon {
                                Layout.alignment: Qt.AlignVCenter
                                implicitWidth: Kirigami.Units.iconSizes.small
                                implicitHeight: implicitWidth
                                source: answerGroup.modelData.icon
                                fallback: "dialog-information-symbolic"
                                color: Kirigami.Theme.textColor
                            }

                            Label {
                                Layout.fillWidth: true
                                text: answerGroup.modelData.label
                                elide: Text.ElideRight
                                font.bold: true
                            }

                            Label {
                                text: Whatevr.I18n.i18ncp("@label how many gave one answer",
                                                          "%1 person", "%1 people",
                                                          answerGroup.people.length)
                                color: Kirigami.Theme.disabledTextColor
                                font.pointSize: Kirigami.Theme.smallFont.pointSize
                            }
                        }

                        Rectangle {
                            Layout.fillWidth: true
                            Layout.bottomMargin: Kirigami.Units.smallSpacing / 2
                            implicitHeight: 1
                            color: Qt.alpha(Kirigami.Theme.textColor, 0.1)
                        }

                        // An answer nobody gave is kept, because "nobody said
                        // maybe" is as much a result as the other two.
                        Label {
                            Layout.fillWidth: true
                            visible: answerGroup.people.length === 0
                            text: Whatevr.I18n.i18nc("@label an answer nobody gave", "Nobody")
                            color: Kirigami.Theme.disabledTextColor
                            font.pointSize: Kirigami.Theme.smallFont.pointSize
                        }

                        Repeater {
                            model: answerGroup.people

                            delegate: ResponderRow {
                                required property var modelData

                                responder: modelData
                            }
                        }
                    }
                }
            }
        }
    }
}
