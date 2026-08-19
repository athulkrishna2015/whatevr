import QtQuick
import QtQuick.Controls
import org.kde.kirigami as Kirigami

// A scrollbar that stays out of the way: a hairline until the pointer is near
// it, a grabbable bar once it is.
//
// Works either way round. `orientation` and the `horizontal` flag that follows
// from it are ScrollBar's own, so a sideways one is
// `DiscreetScrollBar { orientation: Qt.Horizontal }` and everything below reads
// "thickness" and "length" rather than width and height. The vertical case is
// the common one and is what the naming follows.
ScrollBar {
    id: root

    policy: ScrollBar.AlwaysOn
    hoverEnabled: true
    interactive: true

    /// How thick the bar is across its own axis: a hairline at rest, wide
    /// enough to grab once the pointer is on it. Writable so the Behavior
    /// below can animate the widening rather than snapping it.
    property real thickness: hovered || pressed || active ? Kirigami.Units.smallSpacing * 1.5 : 2

    implicitWidth: horizontal ? 0 : thickness
    implicitHeight: horizontal ? thickness : 0

    // The cross axis is ours to set and only ours; the long axis is tracked
    // from the flickable by the Connections below, so it is left alone here
    // rather than bound to something that would fight it.
    Binding on width {
        when: !root.horizontal
        value: root.thickness
    }

    Binding on height {
        when: root.horizontal
        value: root.thickness
    }

    // Smallest the visible thumb is allowed to get; keeps it grabbable/visible
    // even when the proportional size would shrink to a couple of pixels.
    readonly property real minThumb: Kirigami.Units.gridUnit
    readonly property real trackLength: horizontal ? root.width : root.height
    readonly property real thumbLength: Math.max(root.size * trackLength, minThumb)

    Behavior on thickness {
        NumberAnimation {
            duration: Kirigami.Units.shortDuration
            easing.type: Easing.OutCubic
        }
    }

    // The attached-ScrollBar helper positions the bar imperatively and only
    // re-runs that layout on the bar's own implicit-size change, not when
    // the flickable resizes or the thickness animates on hover: the bar
    // drifted off its edge in both cases. An anchor tracks every geometry
    // change.
    anchors.right: parent && !horizontal ? parent.right : undefined
    anchors.bottom: parent && horizontal ? parent.bottom : undefined

    Connections {
        target: root.parent
        function onHeightChanged() {
            if (!root.horizontal) {
                root.height = root.parent.height
            }
        }
        function onWidthChanged() {
            if (root.horizontal) {
                root.width = root.parent.width
            }
        }
    }

    // Control-managed drag/hit target; not used for visuals.
    background: Item {}
    contentItem: Item {}

    // Visible thumb, positioned by reactive bindings (not the control's
    // imperative resizeContent) so it refreshes immediately on window resize.
    Rectangle {
        id: thumb

        // Remap position (0..1-size) onto the reduced travel of a min-clamped
        // thumb, then clamp into the track so it never floats off-screen.
        readonly property real offset: {
            const travel = root.trackLength - (root.horizontal ? width : height)
            if (travel <= 0) {
                return 0
            }
            const denom = 1 - root.size
            const frac = denom > 0 ? root.position / denom : 0
            return Math.max(0, Math.min(travel, frac * travel))
        }

        // size >= 1 means content fully fits -> nothing to scroll.
        visible: root.size > 0 && root.size < 1
        width: root.horizontal ? root.thumbLength : root.thickness
        height: root.horizontal ? root.thickness : root.thumbLength
        x: root.horizontal ? offset : (root.width - width) / 2
        y: root.horizontal ? (root.height - height) / 2 : offset
        radius: Math.min(width, height) / 2

        color: Qt.alpha(Kirigami.Theme.textColor,
                        root.hovered || root.pressed || root.active ? 0.42 : 0.22)

        // Only the cross axis animates. The long axis is the thumb's size,
        // which tracks how much content there is; easing that makes the thumb
        // breathe every time the view revises its estimate.
        Behavior on width {
            enabled: !root.horizontal
            NumberAnimation {
                duration: Kirigami.Units.shortDuration
                easing.type: Easing.OutCubic
            }
        }

        Behavior on height {
            enabled: root.horizontal
            NumberAnimation {
                duration: Kirigami.Units.shortDuration
                easing.type: Easing.OutCubic
            }
        }
    }
}
