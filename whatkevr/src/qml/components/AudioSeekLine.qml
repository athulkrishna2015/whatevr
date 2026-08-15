// SPDX-License-Identifier: BSD-3-Clause
pragma ComponentBehavior: Bound

import QtQuick

import org.kde.kirigami as Kirigami

/**
 * The slim scrub track a shared audio file uses in place of a voice note's
 * waveform, in the bubble and again in the now-playing bar.
 *
 * A drawn envelope says something about a recording of a voice and nothing at
 * all about a three-minute song, so a track that long gets a plain line. Tap to
 * jump, drag to scrub, same as the waveform.
 */
Item {
    id: root

    /// 0-1 along the track.
    property real progress: 0
    property bool interactive: true
    /// The knob is for a track that has been started; an untouched one is a
    /// bar, not a slider with its handle parked at the left edge.
    property bool showKnob: true
    property color playedColor: Kirigami.Theme.highlightColor
    property color pendingColor: Qt.alpha(Kirigami.Theme.textColor, 0.28)

    /// Emitted with a 0-1 position when the track is tapped or dragged.
    signal scrubbed(real fraction)

    readonly property real trackHeight: Math.max(3, Math.round(Kirigami.Units.smallSpacing * 0.75))
    readonly property real knobSize: trackHeight * 3

    implicitHeight: knobSize

    function fractionAt(x) {
        if (width <= 0)
            return 0
        return Math.min(1, Math.max(0, x / width))
    }

    Rectangle {
        id: track

        anchors.left: parent.left
        anchors.right: parent.right
        anchors.verticalCenter: parent.verticalCenter
        height: root.trackHeight
        radius: height / 2
        color: root.pendingColor

        Rectangle {
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.bottom: parent.bottom
            width: Math.round(Math.min(1, Math.max(0, root.progress)) * parent.width)
            radius: parent.radius
            color: root.playedColor
        }
    }

    Rectangle {
        visible: root.showKnob
        width: root.knobSize
        height: width
        radius: width / 2
        color: root.playedColor
        anchors.verticalCenter: parent.verticalCenter
        x: Math.round(Math.min(1, Math.max(0, root.progress)) * (parent.width - width))
    }

    TapHandler {
        enabled: root.interactive
        onTapped: eventPoint => root.scrubbed(root.fractionAt(eventPoint.position.x))
    }

    DragHandler {
        enabled: root.interactive
        target: null
        xAxis.enabled: true
        yAxis.enabled: false
        onCentroidChanged: {
            if (active)
                root.scrubbed(root.fractionAt(centroid.position.x))
        }
    }
}
