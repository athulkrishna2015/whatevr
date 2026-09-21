// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr
import "MediaFormat.js" as MediaFormat

/**
 * A call that happened, recorded in the chat it happened in.
 *
 * It is the one row in the transcript that nobody said. A call is something the
 * two of you did, so putting it in a plate on one side would claim somebody
 * spoke and hanging an avatar off it would name the speaker. It draws as a pill
 * in the middle instead, which is where the day separator already sits and
 * which reads as "this happened" rather than "this was said".
 *
 * The time sits on the pill rather than under it. For an ordinary message the
 * time is a footnote; for a call it is most of the content, because knowing
 * when somebody tried to reach you is what a call log is for.
 */
Item {
    id: root

    objectName: "callLogPill"

    required property ChatBubble row

    readonly property var log: row.callLog ?? ({})

    readonly property bool video: log.video ?? false
    readonly property string outcome: String(log.outcome ?? "")
    readonly property int durationSecs: log.duration_secs ?? 0
    readonly property bool groupCall: log.group ?? false

    /// A call nobody picked up, and the one a reader may still want to do
    /// something about.
    readonly property bool unanswered: outcome === "missed" || outcome === "silenced"
    readonly property bool declined: outcome === "rejected"
    readonly property bool failed: outcome === "failed"

    /// The sentence is composed here rather than taken from the daemon's
    /// one-line rendering, because it is the one thing on this row a reader
    /// reads and the daemon's copy is not translatable. Which words apply
    /// depends on the side we were on: telling somebody they missed a call they
    /// placed themselves is an accusation in the wrong direction.
    readonly property string summary: {
        if (root.unanswered) {
            if (root.row.isOutgoing) {
                return root.video
                    ? Whatevr.I18n.i18nc("@label call log", "Unanswered video call")
                    : Whatevr.I18n.i18nc("@label call log", "Unanswered voice call")
            }
            return root.video
                ? Whatevr.I18n.i18nc("@label call log", "Missed video call")
                : Whatevr.I18n.i18nc("@label call log", "Missed voice call")
        }
        if (root.declined) {
            return root.video
                ? Whatevr.I18n.i18nc("@label call log", "Declined video call")
                : Whatevr.I18n.i18nc("@label call log", "Declined voice call")
        }
        if (root.failed) {
            return Whatevr.I18n.i18nc("@label call log", "Call failed")
        }
        if (root.outcome === "ongoing") {
            return Whatevr.I18n.i18nc("@label call log", "Call in progress")
        }
        if (root.outcome === "accepted_elsewhere") {
            return Whatevr.I18n.i18nc("@label call log", "Answered on another device")
        }

        const medium = root.groupCall
            ? (root.video
                ? Whatevr.I18n.i18nc("@label call log", "Group video call")
                : Whatevr.I18n.i18nc("@label call log", "Group voice call"))
            : (root.video
                ? Whatevr.I18n.i18nc("@label call log", "Video call")
                : Whatevr.I18n.i18nc("@label call log", "Voice call"))
        if (root.durationSecs > 0) {
            return Whatevr.I18n.i18nc("@label call log, %1 is a call description and %2 its length",
                                      "%1 · %2", medium, MediaFormat.clockTime(root.durationSecs))
        }
        return medium
    }

    readonly property real vPadding: Math.round(Kirigami.Units.smallSpacing * 0.75)
    /// A fully rounded pill pads its sides to its own height, not to a flat
    /// unit, or the text sits in the flat middle with the round ends crowding
    /// it.
    readonly property real hPadding: Math.round(implicitHeight * 0.42)

    readonly property color accent: {
        if (root.unanswered || root.failed) {
            return Kirigami.Theme.negativeTextColor
        }
        if (root.declined) {
            return Kirigami.Theme.neutralTextColor
        }
        return Kirigami.Theme.disabledTextColor
    }

    /// The arrow says which way the call went, which is the fact a glance is
    /// after. A missed one gets its own glyph rather than a coloured incoming
    /// arrow, because that distinction has to survive being read in a hurry.
    readonly property string glyph: {
        if (root.unanswered) {
            return "call-missed-symbolic"
        }
        if (root.declined || root.failed) {
            return "call-stop-symbolic"
        }
        return root.row.isOutgoing ? "call-outgoing-symbolic" : "call-incoming-symbolic"
    }

    readonly property real glyphSize: Kirigami.Units.iconSizes.small
    readonly property real innerSpacing: Kirigami.Units.smallSpacing

    implicitWidth: Math.min(Math.max(0, row.listWidth - Kirigami.Units.largeSpacing * 2),
                            hPadding * 2 + glyphSize + innerSpacing
                            + Math.ceil(summaryMetrics.advanceWidth)
                            + innerSpacing + Math.ceil(timeMetrics.advanceWidth))
    implicitHeight: Math.max(glyphSize, Math.ceil(summaryMetrics.height)) + vPadding * 2

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

    // The same plate the day separator wears, for the same reason: a pill on
    // the wallpaper needs to sit on something opaque or the doodles read
    // straight through the words. A tint of the text colour was not enough.
    Rectangle {
        anchors.fill: parent
        radius: height / 2
        color: Qt.alpha(Kirigami.Theme.backgroundColor, 0.9)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1
    }

    Kirigami.Icon {
        id: glyphIcon

        anchors.left: parent.left
        anchors.leftMargin: root.hPadding
        anchors.verticalCenter: parent.verticalCenter
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
        anchors.verticalCenter: parent.verticalCenter
        text: root.summary
        elide: Text.ElideRight
        maximumLineCount: 1
        color: root.unanswered || root.failed ? root.accent : Kirigami.Theme.textColor
        font.pointSize: Kirigami.Theme.smallFont.pointSize
    }

    // The time, on the pill. A run of call logs with no times is a list of
    // things that happened in no particular order, which is the opposite of
    // what one is for.
    Controls.Label {
        id: timeLabel

        anchors.right: parent.right
        anchors.rightMargin: root.hPadding
        anchors.verticalCenter: parent.verticalCenter
        text: root.row.timeText
        color: Kirigami.Theme.disabledTextColor
        maximumLineCount: 1
        font.pointSize: Kirigami.Theme.smallFont.pointSize
    }
}
