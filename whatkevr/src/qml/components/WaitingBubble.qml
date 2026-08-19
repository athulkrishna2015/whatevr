// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A message that arrived and would not decrypt.
 *
 * It happens: a device rotated its keys, a session went stale, a message was
 * sent while this one was offline. WhatsApp's answer is to ask the sender to
 * send it again, and failing that to ask your own phone, and the daemon does
 * both. What was missing was any sign of it: the message simply was not there,
 * which reads exactly like nobody having said anything.
 *
 * So the hole gets a row that says it is a hole, says what is being done, and
 * turns into the real message in place when the resend lands. The card never
 * lies about the outcome: once the automatic attempts are spent it says the
 * phone did not answer rather than shimmering forever.
 */
Item {
    id: root

    objectName: "waitingBubble"

    required property ChatBubble row

    readonly property var wait: row.waiting ?? ({})

    readonly property int retryAt: wait.retry_at ?? 0
    readonly property int requests: wait.requests ?? 0
    readonly property bool askedByHand: wait.asked ?? false

    /// Seconds until the next attempt happens by itself. The clock is read from
    /// a ticking property rather than from Date.now() directly, so the label
    /// actually counts down instead of freezing at whatever it first rendered.
    property int nowSecs: Math.floor(Date.now() / 1000)

    readonly property int secondsLeft: Math.max(0, root.retryAt - root.nowSecs)
    readonly property bool stillTrying: root.retryAt > 0 && root.secondsLeft > 0

    readonly property real contentMargin: Kirigami.Units.smallSpacing
    readonly property real cornerRadius: Kirigami.Units.cornerRadius

    implicitWidth: row.attachmentBlockWidth
    // A layout has no implicit height until it first arranges, and a row that
    // reserves nothing and then grows shoves the transcript under it.
    readonly property real unmeasuredBodyHeight: Kirigami.Units.gridUnit * 3
    implicitHeight: contentMargin * 2
                    + (body.implicitHeight > 0 ? body.implicitHeight : unmeasuredBodyHeight)

    /// What is happening, in one line under the headline.
    readonly property string statusText: {
        if (root.stillTrying) {
            return Whatevr.I18n.i18ncp("@info how long until the message is asked for again",
                                       "Asking again in %1 second", "Asking again in %1 seconds",
                                       root.secondsLeft)
        }
        if (root.askedByHand) {
            return Whatevr.I18n.i18nc("@info the phone was asked for a message and did not answer",
                                      "Your phone did not answer. Open WhatsApp on your phone to let it catch up.")
        }
        return Whatevr.I18n.i18nc("@info the sender and the phone were both asked and neither answered",
                                  "Nobody answered yet. Open WhatsApp on your phone, or ask again.")
    }

    // One timer for the countdown, and only while there is one to count. A
    // transcript full of settled placeholders must not tick.
    Timer {
        running: root.stillTrying
        interval: 1000
        repeat: true
        onTriggered: root.nowSecs = Math.floor(Date.now() / 1000)
    }

    Rectangle {
        anchors.fill: parent
        radius: root.cornerRadius
        color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
        border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
        border.width: 1

        // A slow breath while something is still expected to happen, and a
        // still card once nothing is. It is the difference between "working on
        // it" and "this is where it stopped", which is the whole point of the
        // row.
        SequentialAnimation on opacity {
            running: root.stillTrying
            loops: Animation.Infinite
            alwaysRunToEnd: true

            NumberAnimation { from: 1.0; to: 0.55; duration: Kirigami.Units.veryLongDuration }
            NumberAnimation { from: 0.55; to: 1.0; duration: Kirigami.Units.veryLongDuration }
        }
    }

    ColumnLayout {
        id: body

        objectName: "cardContent"

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: root.contentMargin
        spacing: Kirigami.Units.smallSpacing

        RowLayout {
            Layout.fillWidth: true
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                Layout.alignment: Qt.AlignTop
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: "content-loading-symbolic"
                color: Kirigami.Theme.disabledTextColor
                isMask: true
            }

            Controls.Label {
                Layout.fillWidth: true
                text: Whatevr.I18n.i18nc("@info a message that could not be decrypted",
                                         "Waiting for this message")
                color: Kirigami.Theme.disabledTextColor
                wrapMode: Text.Wrap
                font.italic: true
                font.pointSize: root.row.bodyPointSize
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            text: root.statusText
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // Only once the automatic attempt has come and gone. Offering the
        // button while the daemon is already asking would invite a second
        // request nobody needs, and make the first one look like it failed.
        CardActionButton {
            Layout.alignment: Qt.AlignLeft
            objectName: "waitingAskAgainButton"
            visible: !root.stillTrying
            enabled: !root.row.selectionModeActive
            text: Whatevr.I18n.i18nc("@action ask the phone again for a message that would not decrypt",
                                     "Ask again")
            iconName: "view-refresh-symbolic"
            onClicked: Whatevr.ProtocolController.requestMessageFromPhone(root.row.messageId)
        }
    }
}
