// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * The full result of a poll: who chose what, readable from either end.
 *
 * WhatsApp answers exactly one question here, "who picked this option", and it
 * answers it one option at a time. That leaves the other question unanswerable:
 * in a poll that allows several answers, there is no way to see what any one
 * person actually chose. So this dialog has two readings of the same votes.
 *
 * By option is the familiar one, every answer with its voters under it. By
 * person turns the table on its side: one row per voter with their choices as
 * chips, which is the only way to read a multi-answer poll as a set of
 * opinions rather than as a set of columns.
 */
CenteredDialog {
    id: root

    /// The `poll` object straight off the message item.
    property var poll: ({})
    property string messageId: ""
    /// Which reading is showing. Option-first is the default because it is the
    /// one that answers "what is winning".
    property bool byPerson: false
    /// Option index the list is narrowed to, or -1 for all of them. Only
    /// meaningful in the option-first reading.
    property int filterIndex: -1

    readonly property var options: poll.options ?? []
    readonly property int totalVoters: poll.total_voters ?? 0
    readonly property bool multipleAllowed: (poll.selectable_count ?? 1) > 1

    /// Every vote as a flat list, so the person-first reading can regroup them.
    /// One entry per person, carrying every option they chose.
    readonly property var people: {
        const byJid = {}
        const out = []
        for (let i = 0; i < options.length; ++i) {
            const voters = options[i].voters ?? []
            for (let v = 0; v < voters.length; ++v) {
                const jid = String(voters[v].jid ?? "")
                if (byJid[jid] === undefined) {
                    byJid[jid] = out.length
                    out.push({
                        jid: jid,
                        name: String(voters[v].name ?? ""),
                        avatar_path: String(voters[v].avatar_path ?? ""),
                        from_me: Boolean(voters[v].from_me),
                        timestamp: Number(voters[v].timestamp ?? 0),
                        choices: []
                    })
                }
                const person = out[byJid[jid]]
                person.choices.push(String(options[i].name ?? ""))
                // A multi-answer vote lands as one message, so the latest
                // stamp on any of their choices is when they last answered.
                person.timestamp = Math.max(person.timestamp, Number(voters[v].timestamp ?? 0))
            }
        }
        // Ourselves first, then whoever answered earliest.
        out.sort((a, b) => Number(b.from_me) - Number(a.from_me) || a.timestamp - b.timestamp)
        return out
    }

    /// The options the list is showing, under the current filter.
    readonly property var visibleOptions: {
        if (filterIndex < 0) {
            return options
        }
        return options.filter(option => Number(option.index) === filterIndex)
    }

    title: Whatevr.I18n.i18nc("@title:dialog the result of a poll", "Poll results")
    standardButtons: Kirigami.Dialog.Close
    padding: Kirigami.Units.largeSpacing
    preferredWidth: Kirigami.Units.gridUnit * 24
    maximumHeight: Kirigami.Units.gridUnit * 28

    function openFor(pollData, msgId, optionIndex) {
        poll = pollData || ({})
        messageId = msgId
        filterIndex = optionIndex === undefined ? -1 : optionIndex
        byPerson = false
        open()
    }

    function nameFor(voter) {
        if (voter.from_me) {
            return Whatevr.I18n.i18nc("@label the account's own vote", "You")
        }
        const name = String(voter.name ?? "")
        return name.length > 0
            ? name
            : Whatevr.I18n.i18nc("@label a voter whose name we do not know", "Someone")
    }

    function initialsFor(voter) {
        const name = nameFor(voter).trim()
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

    /// One voter, the same shape in both readings: face, name, and whatever
    /// the reading wants said on the right.
    component VoterRow: RowLayout {
        id: voterRow

        required property var voter
        property string trailingText: ""

        Layout.fillWidth: true
        spacing: Kirigami.Units.smallSpacing

        AvatarImage {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.gridUnit * 1.6
            implicitHeight: implicitWidth
            avatarLocalPath: String(voterRow.voter.avatar_path ?? "")
            initials: root.initialsFor(voterRow.voter)
        }

        Label {
            Layout.fillWidth: true
            text: root.nameFor(voterRow.voter)
            elide: Text.ElideRight
            font.bold: Boolean(voterRow.voter.from_me)
        }

        Label {
            visible: voterRow.trailingText.length > 0
            text: voterRow.trailingText
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
            text: String(root.poll.question ?? "")
            wrapMode: Text.Wrap
            font.bold: true
        }

        Label {
            Layout.fillWidth: true
            Layout.bottomMargin: Kirigami.Units.smallSpacing
            text: root.totalVoters > 0
                ? Whatevr.I18n.i18ncp("@label poll turnout", "%1 person answered", "%1 people answered", root.totalVoters)
                : Whatevr.I18n.i18nc("@label a poll nobody has answered", "Nobody has answered yet")
            color: Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // The two readings. Offered only when there is more than one option to
        // regroup, because with a single option they say the same thing.
        TabBar {
            id: readingTabs

            Layout.fillWidth: true
            visible: root.options.length > 1 && root.totalVoters > 0
            currentIndex: root.byPerson ? 1 : 0
            onCurrentIndexChanged: root.byPerson = currentIndex === 1

            TabButton {
                text: Whatevr.I18n.i18nc("@title:tab poll results grouped by answer", "By option")
            }

            TabButton {
                text: Whatevr.I18n.i18nc("@title:tab poll results grouped by voter", "By person")
            }
        }

        // Narrowing to one answer, when the bubble opened us on one. The chip
        // is a way back to the whole poll rather than a dead label.
        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing
            visible: !root.byPerson && root.filterIndex >= 0
            spacing: Kirigami.Units.smallSpacing

            Label {
                text: Whatevr.I18n.i18nc("@label the poll result list is narrowed to one answer", "Showing one answer")
                color: Kirigami.Theme.disabledTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }

            Button {
                text: Whatevr.I18n.i18nc("@action:button widen the poll result list to every answer", "Show all")
                flat: true
                onClicked: root.filterIndex = -1
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

                width: root.preferredWidth - Kirigami.Units.largeSpacing
                spacing: Kirigami.Units.smallSpacing

                // Option-first: every answer, with its voters under it. Options
                // nobody chose are kept, because an empty answer is a result.
                Repeater {
                    model: root.byPerson ? [] : root.visibleOptions

                    delegate: ColumnLayout {
                        id: optionGroup

                        required property var modelData

                        readonly property var voters: modelData.voters ?? []

                        Layout.fillWidth: true
                        Layout.bottomMargin: Kirigami.Units.smallSpacing
                        spacing: Kirigami.Units.smallSpacing / 2

                        RowLayout {
                            Layout.fillWidth: true
                            spacing: Kirigami.Units.smallSpacing

                            Label {
                                Layout.fillWidth: true
                                text: String(optionGroup.modelData.name ?? "")
                                wrapMode: Text.Wrap
                                font.bold: true
                            }

                            Label {
                                text: Whatevr.I18n.i18ncp("@label how many chose one poll answer", "%1 vote", "%1 votes", optionGroup.voters.length)
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

                        Label {
                            Layout.fillWidth: true
                            visible: optionGroup.voters.length === 0
                            text: Whatevr.I18n.i18nc("@label an answer nobody chose", "No votes")
                            color: Kirigami.Theme.disabledTextColor
                            font.pointSize: Kirigami.Theme.smallFont.pointSize
                        }

                        Repeater {
                            model: optionGroup.voters

                            delegate: VoterRow {
                                required property var modelData

                                voter: modelData
                                trailingText: root.formatTimestamp(modelData.timestamp)
                            }
                        }
                    }
                }

                // Person-first: one row per voter, their answers as chips. This
                // is the reading a multi-answer poll has no other way to give.
                Repeater {
                    model: root.byPerson ? root.people : []

                    delegate: ColumnLayout {
                        id: personGroup

                        required property var modelData

                        Layout.fillWidth: true
                        Layout.bottomMargin: Kirigami.Units.smallSpacing
                        spacing: Kirigami.Units.smallSpacing / 2

                        VoterRow {
                            voter: personGroup.modelData
                            trailingText: root.formatTimestamp(personGroup.modelData.timestamp)
                        }

                        Flow {
                            Layout.fillWidth: true
                            Layout.leftMargin: Kirigami.Units.gridUnit * 1.6 + Kirigami.Units.smallSpacing
                            spacing: Kirigami.Units.smallSpacing / 2

                            Repeater {
                                model: personGroup.modelData.choices

                                delegate: Rectangle {
                                    id: choiceChip

                                    required property string modelData

                                    implicitWidth: choiceLabel.implicitWidth + Kirigami.Units.smallSpacing * 2
                                    implicitHeight: choiceLabel.implicitHeight + Kirigami.Units.smallSpacing
                                    radius: height / 2
                                    color: Qt.alpha(Whatevr.Palette.highlight, 0.16)

                                    Label {
                                        id: choiceLabel

                                        anchors.centerIn: parent
                                        text: choiceChip.modelData
                                        font.pointSize: Kirigami.Theme.smallFont.pointSize
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}
