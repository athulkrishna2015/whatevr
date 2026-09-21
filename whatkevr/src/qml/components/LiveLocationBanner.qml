// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as QQC2
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A thin strip above the timeline while somebody in this chat is sharing their
 * location live.
 *
 * A live share is the one thing in a conversation that is still happening while
 * you are not looking at it, and a bubble scrolled out of view says nothing.
 * The strip names who is sharing and how long is left, and clicking it jumps to
 * the bubble.
 *
 * It reads the `live_locations` view rather than the transcript: the position
 * moves every few seconds, and re-rendering a message list for that would be
 * absurd.
 */
Item {
    id: root

    readonly property int count: Whatevr.ProtocolController.liveLocationsCount
    /// The share currently named, from ProtocolController::liveLocationAt.
    property var current: null

    signal messageActivated(string messageId)

    implicitHeight: Math.max(Kirigami.Units.gridUnit * 2.25,
                             Kirigami.Units.iconSizes.small + Kirigami.Units.smallSpacing * 2)

    // Seconds tick while a share is running; nothing ticks when none is.
    QtObject {
        id: clock

        property int now: Math.floor(Date.now() / 1000)
    }

    Timer {
        interval: 1000
        running: root.count > 0 && root.visible
        repeat: true
        onTriggered: clock.now = Math.floor(Date.now() / 1000)
    }

    function refresh() {
        current = count > 0 ? Whatevr.ProtocolController.liveLocationAt(0) : null
    }

    Component.onCompleted: refresh()

    Connections {
        target: Whatevr.ProtocolController

        function onLiveLocationsChanged() {
            root.refresh()
        }
    }

    /**
     * "Ana is sharing live", or a count when more than one person is. The
     * plural form deliberately does not name anybody: a strip that listed four
     * names would be a paragraph.
     */
    readonly property string headline: {
        if (count > 1)
            return Whatevr.I18n.i18ncp("@info live location banner",
                                       "%1 people are sharing live location",
                                       "%1 people are sharing live location", count)
        if (!current)
            return ""
        const name = String(current.senderName ?? "")
        if (name.length === 0)
            return Whatevr.I18n.i18nc("@info live location banner", "Sharing live location")
        return Whatevr.I18n.i18nc("@info live location banner, %1 is a contact name",
                                  "%1 is sharing live location", name)
    }

    readonly property string remainingText: {
        if (count !== 1 || !current)
            return ""
        const expiresAt = Number(current.expiresAt ?? 0)
        if (expiresAt <= 0)
            return ""
        const seconds = expiresAt - clock.now
        if (seconds <= 0)
            return ""
        if (seconds < 60)
            return Whatevr.I18n.i18nc("@label live location time left", "under a minute left")
        const minutes = Math.round(seconds / 60)
        if (minutes < 60)
            return Whatevr.I18n.i18ncp("@label live location time left", "%1 min left", "%1 min left", minutes)
        return Whatevr.I18n.i18ncp("@label live location time left", "%1 hr left", "%1 hr left",
                                   Math.round(minutes / 60))
    }

    Rectangle {
        anchors.fill: parent
        color: Kirigami.Theme.backgroundColor

        Kirigami.Separator {
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.bottom: parent.bottom
        }
    }

    RowLayout {
        anchors.fill: parent
        anchors.leftMargin: Kirigami.Units.largeSpacing
        anchors.rightMargin: Kirigami.Units.smallSpacing
        spacing: Kirigami.Units.smallSpacing

        // The pulse is the whole point of the strip: it says "now".
        Rectangle {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.smallSpacing
            implicitHeight: Kirigami.Units.smallSpacing
            radius: width / 2
            color: Kirigami.Theme.negativeTextColor

            SequentialAnimation on opacity {
                running: root.visible
                loops: Animation.Infinite
                NumberAnimation { to: 0.25; duration: 900; easing.type: Easing.InOutQuad }
                NumberAnimation { to: 1.0; duration: 900; easing.type: Easing.InOutQuad }
            }
        }

        QQC2.Label {
            Layout.fillWidth: true
            text: root.headline
            elide: Text.ElideRight
            maximumLineCount: 1
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        QQC2.Label {
            visible: text.length > 0
            text: root.remainingText
            color: Kirigami.Theme.disabledTextColor
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        Kirigami.Icon {
            Layout.alignment: Qt.AlignVCenter
            implicitWidth: Kirigami.Units.iconSizes.small
            implicitHeight: Kirigami.Units.iconSizes.small
            source: "go-next-symbolic"
            color: Kirigami.Theme.disabledTextColor
        }
    }

    TapHandler {
        onSingleTapped: {
            if (root.current)
                root.messageActivated(String(root.current.messageId))
        }
    }

    HoverHandler {
        cursorShape: Qt.PointingHandCursor
    }
}
