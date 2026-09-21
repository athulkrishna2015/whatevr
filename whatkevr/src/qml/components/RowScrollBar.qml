import QtQuick
import org.kde.kirigami as Kirigami

// Index-based overlay scrollbar for ListViews with variable-height delegates.
//
// Qt's attached ScrollBar maps content geometry (viewport / contentHeight),
// but in a ListView those are *estimates* the view keeps revising as
// delegates materialise: the thumb breathes while scrolling, and during a
// drag the mouse-to-contentY mapping shifts underneath the pointer, which
// reads as stutter. This scrollbar maps rows instead — thumb size and
// position come from visible row indices, which only change when rows
// actually enter or leave the viewport — and while pressed the thumb is
// pointer-bound, so the view chases the thumb and never the reverse.
//
// Everything here counts rows on the screen, never model indices. The
// transcript is held newest-first so that a BottomToTop ListView can pin its
// live edge at row 0, which means a row's index says nothing about where it is
// drawn; the owner does that translation once (see MessageView) and hands this
// two plain quantities: how many rows are above the viewport, and how many it
// shows. A thumb that thought in indices would run backwards.
Item {
    id: root

    // Row-window inputs, bound by the owner (see MessageView.updateScrollState).
    property int count: 0
    // Rows lying entirely above the viewport, plus the fraction of the topmost
    // visible row that is scrolled off above it. Fractional so the thumb sweeps
    // continuously rather than in row-sized steps.
    property real rowsAbove: 0
    // How many rows the viewport is showing.
    property int visibleSpan: 1

    // Put this many rows above the viewport. Fractional, and in the same units
    // as `rowsAbove`, so the owner converts it back to whatever a row index
    // means to it.
    signal dragPositionRequested(real rowsAbove)
    signal jumpToNewestRequested()

    readonly property bool dragging: dragArea.pressed
    readonly property bool hoveredOrActive: hoverHandler.hovered || dragArea.pressed

    readonly property real minThumb: Kirigami.Units.gridUnit
    readonly property real visualWidth: hoveredOrActive ? Kirigami.Units.smallSpacing * 1.5 : 2

    readonly property bool scrollable: count > 0 && visibleSpan < count
    readonly property real denom: Math.max(1, count - visibleSpan)
    readonly property real posFraction: Math.max(0, Math.min(1, rowsAbove / denom))
    readonly property real thumbHeight: Math.max(minThumb, (visibleSpan / Math.max(1, count)) * height)
    readonly property real travel: Math.max(0, height - thumbHeight)

    // Pointer-bound thumb position while dragging.
    property real dragThumbY: 0
    property real pressOffset: 0

    // Constant hit width for grabbability; only the visual thumb animates.
    width: Kirigami.Units.smallSpacing * 2

    function requestFromThumb() {
        // Qt.callLater coalesces repeated calls into one invocation per
        // event-loop pass — the once-per-frame throttle for repositioning.
        Qt.callLater(flushDrag)
    }

    function flushDrag() {
        if (!dragArea.pressed || !scrollable) {
            return
        }
        const frac = travel > 0 ? dragThumbY / travel : 0
        if (frac <= 0.001) {
            // Track top: nothing above the viewport, so the oldest row we hold
            // is flush with its top edge.
            dragPositionRequested(0)
            return
        }
        if (frac >= 0.999) {
            jumpToNewestRequested()
            return
        }
        dragPositionRequested(frac * denom)
    }

    HoverHandler {
        id: hoverHandler
    }

    MouseArea {
        id: dragArea

        anchors.fill: parent
        acceptedButtons: Qt.LeftButton
        enabled: root.scrollable
        // Keep the grab for the whole drag: the message view's full-area
        // drag-eater DragHandler would otherwise take over once the pointer
        // passes the drag threshold, freezing the thumb after a few pixels.
        preventStealing: true

        onPressed: mouse => {
            const within = mouse.y >= thumb.y && mouse.y <= thumb.y + thumb.height
            root.pressOffset = within ? mouse.y - thumb.y : thumb.height / 2
            root.dragThumbY = Math.max(0, Math.min(root.travel, mouse.y - root.pressOffset))
            root.requestFromThumb()
        }
        onPositionChanged: mouse => {
            if (!pressed) {
                return
            }
            root.dragThumbY = Math.max(0, Math.min(root.travel, mouse.y - root.pressOffset))
            root.requestFromThumb()
        }
    }

    Rectangle {
        id: thumb

        visible: root.scrollable
        width: root.visualWidth
        x: parent.width - width - 1
        radius: width / 2
        height: root.thumbHeight
        y: dragArea.pressed ? root.dragThumbY : root.posFraction * root.travel
        color: Qt.alpha(Kirigami.Theme.textColor, root.hoveredOrActive ? 0.42 : 0.22)

        // History pages append 80 rows at once; ease the resulting thumb
        // shrink instead of popping.
        Behavior on height {
            NumberAnimation {
                duration: Kirigami.Units.shortDuration * 2
                easing.type: Easing.OutCubic
            }
        }

        Behavior on width {
            NumberAnimation {
                duration: Kirigami.Units.shortDuration
                easing.type: Easing.OutCubic
            }
        }
    }
}
