// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick
import QtQuick.Controls as Controls
import QtQuick.Layouts

import org.kde.kirigami as Kirigami

import Whatevr as Whatevr

/**
 * A scheduled event.
 *
 * The card leads with a calendar leaf, because a date is what somebody is
 * actually looking for when an event scrolls past. Then the plan: what it is,
 * when, and where or on what link.
 *
 * Answering is three chips with the faces of everyone who picked each one,
 * which is strictly more than a count: knowing four people are going is less
 * useful than knowing which four. The answer paints on the tap and the daemon
 * catches up, exactly as a poll vote does.
 *
 * The desktop payoff is **Add to calendar**. A phone can only keep an event
 * inside WhatsApp; a desktop can hand it to whatever calendar the person
 * actually keeps, as an .ics whose UID is the message id, so re-importing
 * updates the entry instead of duplicating it.
 */
Item {
    id: root

    objectName: "eventBubble"

    required property ChatBubble row

    readonly property var plan: row.eventInfo ?? ({})

    readonly property string name: String(plan.name ?? "")
    readonly property string description: String(plan.description ?? "")
    readonly property int startsAt: plan.starts_at ?? 0
    readonly property int endsAt: plan.ends_at ?? 0
    readonly property bool canceled: plan.canceled ?? false
    readonly property string joinLink: String(plan.join_link ?? "")
    readonly property bool scheduleCall: plan.schedule_call ?? false
    readonly property bool extraGuestsAllowed: plan.extra_guests_allowed ?? false
    readonly property var venue: plan.location ?? null
    readonly property var responders: plan.responders ?? []
    readonly property int goingCount: plan.going_count ?? 0

    /// Padding between the card's edge and its content, on every side, counted
    /// twice in the height: a card that reports only its content's height puts
    /// its last row over its own bottom edge.
    readonly property real contentMargin: Kirigami.Units.largeSpacing

    implicitWidth: row.attachmentBlockWidth
    implicitHeight: content.implicitHeight + contentMargin * 2

    // An event that has been and gone is a record, and the whole card reads
    // that way rather than still inviting an answer.
    readonly property bool past: endsAt > 0 ? clock.now >= endsAt : (startsAt > 0 && clock.now >= startsAt)
    readonly property bool answerable: !canceled && !past && !row.selectionModeActive

    /// Our own answer, preferring the one a tap just asked for over the one the
    /// daemon has confirmed, so a chip lights on the same frame as the press.
    readonly property var pendingRSVP: Whatevr.ProtocolController.pendingEventRSVPs[row.messageId]
    readonly property string selfResponse:
        pendingRSVP !== undefined && pendingRSVP !== null
            ? String(pendingRSVP.response)
            : String(plan.self_response ?? "")

    readonly property date startDate: new Date(startsAt * 1000)

    /// The map for the venue: the stitched one once it lands, the sender's
    /// embedded thumbnail until then, and nothing at all for an event that is
    /// a call link or a bare time.
    readonly property url mapSourceUrl: {
        if (!venue)
            return ""
        if (row.mediaLocalPath.length > 0)
            return Whatevr.ProtocolController.localFileUrl(row.mediaLocalPath)
        if (row.mediaThumbnailLocalPath.length > 0)
            return Whatevr.ProtocolController.localFileUrl(row.mediaThumbnailLocalPath)
        return ""
    }
    readonly property bool hasMapImage: String(mapSourceUrl).length > 0

    /// The width this card would rather be. An event with a map wants the whole
    /// content width, because the map is the picture; one that is a time and a
    /// name does not, and taking it leaves the chips stretched across a mostly
    /// empty plate. 0 means fill, per the card contract.
    readonly property real preferredWidth: hasMapImage ? 0 : content.implicitWidth + contentMargin * 2

    QtObject {
        id: clock

        property int now: Math.floor(Date.now() / 1000)
    }

    // A minute is as fine as this card ever needs: it says whether an event has
    // started, not how many seconds ago.
    Timer {
        interval: 60000
        running: root.startsAt > 0 && !root.past && root.row.activeInViewport
        repeat: true
        onTriggered: clock.now = Math.floor(Date.now() / 1000)
    }

    function chipCount(response) {
        let total = 0
        for (let i = 0; i < responders.length; ++i) {
            if (String(responders[i].response) === response)
                ++total
        }
        // Our own pending answer is not in the daemon's list yet, and the one it
        // replaced still is, so the counts are corrected here rather than left
        // to flicker when the echo lands.
        const confirmed = String(plan.self_response ?? "")
        if (selfResponse !== confirmed) {
            if (response === selfResponse)
                ++total
            if (response === confirmed)
                --total
        }
        return Math.max(0, total)
    }

    /// How many answers there are at all, our own in-flight one included.
    readonly property int answeredCount:
        chipCount("going") + chipCount("maybe") + chipCount("not_going")

    /// How many people are coming, guests included, corrected for an answer of
    /// ours the daemon has not echoed yet. The correction is the chips' one: an
    /// answer we just gave counts, and the one it replaced stops counting, along
    /// with the guests it was bringing.
    readonly property int goingHeads: {
        let heads = goingCount
        const confirmed = String(plan.self_response ?? "")
        if (selfResponse !== confirmed) {
            if (selfResponse === "going")
                heads += 1
            if (confirmed === "going")
                heads -= 1 + (plan.self_guests ?? 0)
        }
        return Math.max(0, heads)
    }

    function facesFor(response) {
        const faces = []
        for (let i = 0; i < responders.length; ++i) {
            if (String(responders[i].response) === response)
                faces.push(responders[i])
        }
        return faces
    }

    /// The time range, in the reader's own locale and clock format.
    readonly property string whenText: {
        if (startsAt <= 0)
            return ""
        const start = new Date(startsAt * 1000)
        const startText = start.toLocaleString(Qt.locale(), Locale.ShortFormat)
        if (endsAt <= startsAt)
            return startText
        const end = new Date(endsAt * 1000)
        // Same day: the end needs only its clock time. "Sat 17:00 to Sat 19:00"
        // says the day twice and reads as two events.
        const sameDay = start.getFullYear() === end.getFullYear()
            && start.getMonth() === end.getMonth()
            && start.getDate() === end.getDate()
        const endText = sameDay
            ? end.toLocaleTimeString(Qt.locale(), Locale.ShortFormat)
            : end.toLocaleString(Qt.locale(), Locale.ShortFormat)
        return Whatevr.I18n.i18nc("@label an event's time range, start then end",
                                  "%1 to %2", startText, endText)
    }

    readonly property string placeText: {
        if (!venue)
            return ""
        const parts = []
        const placeName = String(venue.name ?? "")
        const address = String(venue.address ?? "")
        if (placeName.length > 0)
            parts.push(placeName)
        if (address.length > 0 && address !== placeName)
            parts.push(address)
        return parts.join(", ")
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
            spacing: Kirigami.Units.largeSpacing

            // The calendar leaf: a month strip over a large day number. It is
            // the one thing on the card a reader can find without reading.
            Rectangle {
                objectName: "calendarLeaf"

                Layout.alignment: Qt.AlignTop
                implicitWidth: Kirigami.Units.gridUnit * 2.6
                implicitHeight: Kirigami.Units.gridUnit * 2.8
                radius: Kirigami.Units.cornerRadius
                color: Qt.alpha(Kirigami.Theme.textColor, 0.06)
                border.color: Qt.alpha(Kirigami.Theme.textColor, 0.12)
                border.width: 1
                visible: root.startsAt > 0
                opacity: root.canceled || root.past ? 0.55 : 1

                Rectangle {
                    id: monthStrip

                    anchors.left: parent.left
                    anchors.right: parent.right
                    anchors.top: parent.top
                    height: Math.round(parent.height * 0.34)
                    radius: parent.radius
                    color: Whatevr.Palette.highlight

                    // The strip's own bottom corners are square; only the
                    // leaf's top ones are round. Without this the accent band
                    // curves away from the card's edge on all four corners and
                    // reads as a floating pill rather than as a strip.
                    Rectangle {
                        anchors.left: parent.left
                        anchors.right: parent.right
                        anchors.bottom: parent.bottom
                        height: parent.radius
                        color: parent.color
                    }

                    Controls.Label {
                        anchors.centerIn: parent
                        text: root.startsAt > 0
                            ? root.startDate.toLocaleString(Qt.locale(), "MMM").toUpperCase()
                            : ""
                        color: Whatevr.Palette.highlightedText
                        font.pointSize: Kirigami.Theme.smallFont.pointSize * 0.85
                        font.bold: true
                    }
                }

                Controls.Label {
                    anchors.horizontalCenter: parent.horizontalCenter
                    anchors.top: monthStrip.bottom
                    anchors.bottom: parent.bottom
                    verticalAlignment: Text.AlignVCenter
                    text: root.startsAt > 0 ? root.startDate.getDate() : ""
                    font.pointSize: root.row.bodyPointSize * 1.35
                    font.bold: true
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignVCenter
                spacing: 0

                RowLayout {
                    Layout.fillWidth: true
                    spacing: Kirigami.Units.smallSpacing

                    Kirigami.Icon {
                        Layout.alignment: Qt.AlignVCenter
                        visible: root.scheduleCall
                        implicitWidth: Kirigami.Units.iconSizes.small
                        implicitHeight: implicitWidth
                        source: "call-start-symbolic"
                        fallback: "phone-symbolic"
                        color: Kirigami.Theme.disabledTextColor
                    }

                    Controls.Label {
                        Layout.fillWidth: true
                        text: root.name.length > 0
                            ? root.name
                            : Whatevr.I18n.i18nc("@title an event with no name", "Event")
                        elide: Text.ElideRight
                        maximumLineCount: 2
                        wrapMode: Text.Wrap
                        font.pointSize: root.row.bodyPointSize
                        font.bold: true
                        // A cancelled event keeps its row, because "this was
                        // called off" is information and deleting it would
                        // leave people wondering whether it is still on. The
                        // strike is what says so at a glance.
                        font.strikeout: root.canceled
                        opacity: root.canceled || root.past ? 0.6 : 1
                    }
                }

                Controls.Label {
                    Layout.fillWidth: true
                    visible: text.length > 0
                    text: root.whenText
                    color: Kirigami.Theme.disabledTextColor
                    elide: Text.ElideRight
                    maximumLineCount: 1
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }

                Controls.Label {
                    Layout.fillWidth: true
                    visible: root.canceled
                    text: Whatevr.I18n.i18nc("@label an event that was called off", "Canceled")
                    color: Kirigami.Theme.negativeTextColor
                    font.pointSize: Kirigami.Theme.smallFont.pointSize
                }
            }
        }

        Controls.Label {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            visible: root.description.length > 0
            text: root.description
            color: Kirigami.Theme.disabledTextColor
            wrapMode: Text.Wrap
            elide: Text.ElideRight
            maximumLineCount: 4
            font.pointSize: Kirigami.Theme.smallFont.pointSize
        }

        // Where it is. The map itself arrives through the row's ordinary media
        // object, so an event's venue looks exactly like a shared place.
        Item {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            Layout.preferredHeight: root.hasMapImage ? Math.round(width * (400 / 640)) : 0
            visible: root.hasMapImage

            // RoundedImage samples a texture provider rather than loading a
            // URL, so the Image itself stays hidden beside it. Same one-shader
            // pass the location card and the photo bubbles use, and the reason
            // the map does not sit in the card with four square corners.
            Image {
                id: mapSource

                visible: false
                anchors.fill: parent
                source: root.mapSourceUrl
                fillMode: Image.PreserveAspectCrop
                asynchronous: true
                cache: true
                sourceSize.width: Math.max(1, Math.round(width * Screen.devicePixelRatio))
            }

            RoundedImage {
                anchors.fill: parent
                visible: mapSource.status === Image.Ready
                source: mapSource
                // The strip's shape is the card's; crop the map to it rather
                // than stretching whatever the stitcher happened to produce.
                sourceRect: coverRect(width, height,
                                      mapSource.implicitWidth, mapSource.implicitHeight)
                topLeftRadius: Kirigami.Units.cornerRadius
                topRightRadius: Kirigami.Units.cornerRadius
                bottomLeftRadius: Kirigami.Units.cornerRadius
                bottomRightRadius: Kirigami.Units.cornerRadius
            }

            TapHandler {
                enabled: !root.row.selectionModeActive
                exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
                onSingleTapped: Whatevr.ProtocolController.openLocation(
                    root.venue.lat ?? 0, root.venue.lng ?? 0, root.placeText)
            }

            HoverHandler {
                enabled: !root.row.selectionModeActive
                cursorShape: Qt.PointingHandCursor
            }
        }

        RowLayout {
            Layout.fillWidth: true
            visible: root.placeText.length > 0
            spacing: Kirigami.Units.smallSpacing

            Kirigami.Icon {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: "mark-location-symbolic"
                fallback: "gps"
                color: Kirigami.Theme.disabledTextColor
            }

            Controls.Label {
                Layout.fillWidth: true
                Layout.alignment: Qt.AlignVCenter
                text: root.placeText
                color: Kirigami.Theme.disabledTextColor
                elide: Text.ElideRight
                maximumLineCount: 2
                wrapMode: Text.Wrap
                font.pointSize: Kirigami.Theme.smallFont.pointSize
            }
        }

        // The three answers, each with the faces of the people who gave it.
        RowLayout {
            objectName: "rsvpChips"

            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing
            visible: root.answerable || root.responders.length > 0
            spacing: Kirigami.Units.smallSpacing

            Repeater {
                model: [
                    {"value": "going", "label": Whatevr.I18n.i18nc("@action rsvp", "Going"),
                     "icon": "checkmark-symbolic"},
                    {"value": "maybe", "label": Whatevr.I18n.i18nc("@action rsvp", "Maybe"),
                     "icon": "question-symbolic"},
                    {"value": "not_going", "label": Whatevr.I18n.i18nc("@action rsvp", "Can't go"),
                     "icon": "dialog-close"},
                ]

                delegate: EventRSVPChip {
                    required property var modelData

                    Layout.fillWidth: true
                    // All three stretch to the tallest, which is whichever one
                    // carries faces. Left to their own heights the answered
                    // chip stands taller than its neighbours and the row reads
                    // as three unrelated buttons.
                    Layout.fillHeight: true
                    response: modelData.value
                    label: modelData.label
                    iconName: modelData.icon
                    count: root.chipCount(modelData.value)
                    faces: root.facesFor(modelData.value)
                    chosen: root.selfResponse === modelData.value
                    // Tapping the answer you already gave does nothing, and the
                    // chip says so by not offering a pointer. An RSVP has no
                    // way to be withdrawn: a poll vote can, because the wire
                    // says a vote is a whole selection and an empty one is
                    // valid, but the response enum has no "never mind". This
                    // used to send "maybe", which silently changed your answer
                    // to something you had not picked.
                    interactive: root.answerable && !chosen
                    detailAvailable: !root.row.selectionModeActive
                    onPicked: Whatevr.ProtocolController.respondToEvent(
                        root.row.messageId, modelData.value, 0)
                    // A chip with no answer left to give still holds three
                    // faces and a count, and that is a question: which three.
                    onDetailRequested: root.row.eventResponsesRequested(modelData.value)
                }
            }
        }

        // The way to the rest of the answers. Three faces per chip is enough to
        // recognise a plan and not enough to plan around it, so the line that
        // counts them opens the list that names them.
        RowLayout {
            objectName: "rsvpSummary"

            visible: root.answeredCount > 0
            spacing: Kirigami.Units.smallSpacing / 2

            Controls.Label {
                Layout.alignment: Qt.AlignVCenter
                text: root.goingHeads > 0
                    ? Whatevr.I18n.i18ncp("@label how many people are coming, guests included",
                                          "%1 person going", "%1 people going", root.goingHeads)
                    // Nobody coming is still an answered event, and the list is
                    // worth opening to find out who said no.
                    : Whatevr.I18n.i18ncp("@label how many people answered an event",
                                          "%1 answered", "%1 answered", root.answeredCount)
                color: summaryHover.hovered ? Kirigami.Theme.textColor : Kirigami.Theme.disabledTextColor
                font.pointSize: Kirigami.Theme.smallFont.pointSize
                font.underline: summaryHover.hovered
            }

            Kirigami.Icon {
                Layout.alignment: Qt.AlignVCenter
                implicitWidth: Kirigami.Units.iconSizes.small
                implicitHeight: implicitWidth
                source: "go-next-symbolic"
                fallback: "arrow-right"
                color: summaryHover.hovered ? Kirigami.Theme.textColor : Kirigami.Theme.disabledTextColor
            }

            TapHandler {
                enabled: !root.row.selectionModeActive
                exclusiveSignals: TapHandler.SingleTap | TapHandler.DoubleTap
                onSingleTapped: root.row.eventResponsesRequested("")
            }

            HoverHandler {
                id: summaryHover

                enabled: !root.row.selectionModeActive
                cursorShape: Qt.PointingHandCursor
            }
        }

        RowLayout {
            Layout.fillWidth: true
            Layout.topMargin: Kirigami.Units.smallSpacing / 2
            // Pulled left by a button's own padding so its glyph lands on the
            // card's own left edge rather than a padding's width in.
            Layout.leftMargin: -Kirigami.Units.smallSpacing
            visible: !root.row.selectionModeActive
            spacing: 0

            CardActionButton {
                visible: root.joinLink.length > 0
                text: Whatevr.I18n.i18nc("@action open an event's call link", "Join call")
                iconName: "call-start-symbolic"
                onClicked: Qt.openUrlExternally(root.joinLink)
            }

            // The thing a phone cannot do.
            CardActionButton {
                visible: root.startsAt > 0
                text: Whatevr.I18n.i18nc("@action export an event as an .ics file", "Add to calendar")
                iconName: "view-calendar-symbolic"
                onClicked: Whatevr.ProtocolController.saveEventToCalendar(root.row.messageId, root.plan)
            }

            Item {
                Layout.fillWidth: true
                Layout.minimumWidth: root.row.tntReserveWidth
            }
        }
    }
}
