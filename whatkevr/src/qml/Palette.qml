pragma Singleton

import QtQuick
import org.kde.kirigami as Kirigami

// One app-wide SystemPalette pinned to the Active colour group.
//
// Kirigami.Theme.highlightColor follows the window's active/inactive palette
// group and greys out when the window loses focus, so accent surfaces (bubble
// fill, read ticks) read from here instead to stay vivid regardless of focus.
//
// This used to be a `SystemPalette` declared inside ChatBubble, i.e. one more
// QObject on every message row for a value that is identical across the whole
// app (DN9).
SystemPalette {
    colorGroup: SystemPalette.Active

    // DESIGN.md action tokens (see DESIGN.md): WhatsApp green is the single
    // action color, so anything that means send/read/selected/unread reads
    // from here instead of the system highlight (which follows the user's
    // Breeze accent and greys out off-focus). Chat surfaces follow the
    // Breeze light/dark scheme via the theme background luminance.
    readonly property bool darkScheme: Kirigami.Theme.backgroundColor.hslLightness < 0.5
    // Badge and fill green; always paired with waGreenContent text.
    readonly property color waGreen: "#25D366"
    // Text-safe action green: send icons, read ticks, selected states.
    readonly property color waGreenInk: darkScheme ? "#00A884" : "#0B6B5E"
    // Dark text for use on waGreen fills (unread badges).
    readonly property color waGreenContent: "#0B141A"
    // Message bubble fills, per direction and scheme.
    readonly property color chatIn: darkScheme ? "#202C33" : "#ffffff"
    readonly property color chatOut: darkScheme ? "#005C4B" : "#D9FDD3"
    // Failed sends and destructive confirms.
    readonly property color errorRed: "#c41c1c"
}
