import QtQuick

// A texture-sampling rounded rectangle. Feed it an Image (or any texture
// provider) via `source` and it draws that texture clipped to per-corner radii
// in a single shader pass -- no layer.enabled, no maskSource, no FBO. This
// replaces the MultiEffect mask stack that used to thrash framebuffers as image
// delegates scrolled in and out of the viewport.
ShaderEffect {
    id: root

    // The Image whose texture is sampled. Keep it `visible: false`; it stays a
    // texture provider regardless of visibility.
    property Item source

    property real topLeftRadius: 0
    property real topRightRadius: 0
    property real bottomRightRadius: 0
    property real bottomLeftRadius: 0

    // Edge antialiasing band, in device pixels.
    property real aa: Math.max(1, Screen.devicePixelRatio)

    // The part of the source to draw, normalised: x, y, width, height. The
    // default draws all of it, which is right for every caller that sizes
    // itself to the picture's aspect ratio (a photo bubble, a map). A caller
    // whose shape is its own (an album's tiles) sets this to the covering
    // rectangle instead: `source` hands over the whole decoded picture as a
    // texture and its fillMode never reaches the shader, so without this the
    // picture is simply stretched to the item.
    property vector4d sourceRect: Qt.vector4d(0, 0, 1, 1)

    /// The sourceRect that fills an item of `itemWidth` x `itemHeight` with a
    /// picture of `pictureWidth` x `pictureHeight` without distorting it,
    /// centring what is cropped. Unknown picture dimensions draw the whole
    /// texture, which is the same thing this did before it could crop.
    function coverRect(itemWidth, itemHeight, pictureWidth, pictureHeight) {
        if (!(itemWidth > 0 && itemHeight > 0 && pictureWidth > 0 && pictureHeight > 0)) {
            return Qt.vector4d(0, 0, 1, 1)
        }
        const item = itemWidth / itemHeight
        const picture = pictureWidth / pictureHeight
        if (picture > item) {
            const u = item / picture
            return Qt.vector4d((1 - u) / 2, 0, u, 1)
        }
        const v = picture / item
        return Qt.vector4d(0, (1 - v) / 2, 1, v)
    }

    readonly property vector2d resolution: Qt.vector2d(Math.max(1, width), Math.max(1, height))
    readonly property vector4d radii: Qt.vector4d(topLeftRadius, topRightRadius,
                                                  bottomRightRadius, bottomLeftRadius)

    fragmentShader: "qrc:/qt/qml/Whatevr/shaders/roundedimage.frag.qsb"
}
