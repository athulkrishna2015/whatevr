// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A poll you can actually answer.
 *
 * Tapping a row votes. The bar behind it fills to that option's share, and the
 * people who chose it stack up as avatars at its right end, which is strictly
 * more than WhatsApp shows: it gives a bare count and hides the names behind a
 * separate screen.
 *
 * The selection is whole rather than incremental, because that is what the wire
 * format means: a vote message carries a voter's entire current choice. So
 * tapping your own answer in a single-answer poll takes it back, and in a
 * multi-answer poll each row toggles.
 */
Item {
    id: root

    required property ChatBubble row

    readonly property var poll: row.poll ?? ({})
    readonly property var options: poll.options ?? []
    readonly property int totalVoters: poll.total_voters ?? 0
    readonly property bool selfVoted: poll.self_voted ?? false
    /// 1 is the ordinary radio-button poll. 0 means WhatsApp did not say, which
    /// in practice also means one.
    readonly property int selectableCount: Math.max(1, poll.selectable_count ?? 1)
    readonly property bool multipleAllowed: selectableCount > 1
    readonly property bool isQuiz: poll.quiz ?? false

    readonly property bool ended: {
        const endsAt = poll.ends_at ?? 0
        return endsAt > 0 && endsAt <= Math.floor(Date.now() / 1000)
    }

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: content.implicitHeight

    /** The largest vote count, so bars are relative to the leader, not the total. */
    readonly property int leadingCount: {
        let most = 0
        for (let i = 0; i < options.length; ++i)
            most = Math.max(most, (options[i].voters ?? []).length)
        return most
    }

    /** The indexes we have chosen right now. */
    function selectedIndexes() {
        const chosen = []
        for (let i = 0; i < options.length; ++i) {
            if (options[i].self_voted)
                chosen.push(options[i].index)
        }
        return chosen
    }

    /**
     * Toggling a row sends the *whole* new selection: with one answer allowed
     * that is either this option or nothing, and with several it is the old set
     * with this one added or removed.
     */
    function toggle(index) {
        if (row.selectionModeActive || ended)
            return
        const chosen = selectedIndexes()
        const at = chosen.indexOf(index)
        let next
        if (multipleAllowed) {
            next = chosen.slice()
            if (at >= 0)
                next.splice(at, 1)
            else
                next.push(index)
        } else {
            next = at >= 0 ? [] : [index]
        }
        pendingSelection = next
        Whatevr.ProtocolController.votePoll(row.messageId, next)
    }

    /// Held only until the daemon's answer arrives, so the row responds to the
    /// tap rather than waiting on a round trip.
    property var pendingSelection: null

    onPollChanged: pendingSelection = null

    function isChosen(option) {
        if (pendingSelection !== null)
            return pendingSelection.indexOf(option.index) >= 0
        return option.self_voted ?? false
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
        spacing: Kirigami.Units.smallSpacing / 2

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                Layout.alignment: Qt.AlignTop
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: Kirigami.Units.iconSizes.small
                source: root.isQuiz ? "quiz-symbolic" : "office-chart-bar-symbolic"
                fallback: "view-statistics"
                color: Kirigami.Theme.disabledTextColor
            }

            Controls.Label {
                Layout.fillWidth: true
                text: String(root.poll.question ?? "")
                wrapMode: Text.Wrap
                font.pointSize: root.row.bodyPointSize
                font.bold: true
            }
        }

        // What the poll's rules are, said once rather than discovered by
        // tapping: WhatsApp puts this here too and it is genuinely needed.
        Controls.Label {
            Layout.fillWidth: true
            Layout.leftMargin: Kirigami.Units.iconSizes.small + Kirigami.Units.smallSpacing
            text: root.ended
                ? Whatevr.I18n.i18nc("@label a poll that has closed", "Poll ended")
                : (root.multipleAllowed
                    ? Whatevr.I18n.i18ncp("@label poll rules", "Select up to %1", "Select up to %1", root.selectableCount)
                    : Whatevr.I18n.i18nc("@label poll rules", "Select one"))
            color: Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Repeater {
            model: root.options

            delegate: PollOptionRow {
                required property var modelData

                Layout.fillWidth: true
                Layout.topMargin: Kirigami.Units.smallSpacing / 2
                option: modelData
                chosen: root.isChosen(modelData)
                leadingCount: root.leadingCount
                totalVoters: root.totalVoters
                multipleAllowed: root.multipleAllowed
                interactive: !root.row.selectionModeActive && !root.ended
                bodyPointSize: root.row.bodyPointSize
                onToggled: root.toggle(modelData.index)
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            Layout.rightMargin: root.row.tntReserveWidth
            spacing: Kirigami.Units.smallSpacing

            Controls.Label {
                text: root.totalVoters > 0
                    ? Whatevr.I18n.i18ncp("@label poll turnout", "%1 vote", "%1 votes", root.totalVoters)
                    : Whatevr.I18n.i18nc("@label a poll nobody has answered", "No votes yet")
                color: Kirigami.Theme.disabledTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }

            Controls.Label {
                visible: !root.selfVoted && !root.ended
                text: Whatevr.I18n.i18nc("@label prompt to answer a poll", "· tap a row to vote")
                color: Kirigami.Theme.disabledTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }

            Item {
                Layout.fillWidth: true
            }
        }
    }
}
