#version 440

// Single-pass rounded-corner image. Samples the source texture directly (the
// Image is a native texture provider) and clips to a per-corner rounded
// rectangle via a signed-distance field. No layer, no mask texture, no
// framebuffer object -- it draws straight into the scene, so nothing is
// allocated or torn down as delegates cross the viewport during a scroll.

layout(location = 0) in vec2 qt_TexCoord0;
layout(location = 0) out vec4 fragColor;

layout(binding = 1) uniform sampler2D source;

layout(std140, binding = 0) uniform buf {
    mat4 qt_Matrix;
    float qt_Opacity;
    vec2 resolution;   // item size in pixels
    vec4 radii;        // x=topLeft, y=topRight, z=bottomRight, w=bottomLeft
    float aa;          // antialiasing band width in pixels
    // Which part of the texture to draw, in normalised coordinates:
    // xy = top-left, zw = size. (0,0,1,1) is the whole thing, which is what
    // every caller that has sized its item to the picture's own aspect ratio
    // wants. Anything else crops, so an item whose shape does not match the
    // picture's can fill itself without stretching what is in it: an Image is
    // a texture provider of the whole decoded picture, and fillMode never
    // reaches this shader.
    vec4 sourceRect;
};

void main() {
    // Centred coordinates: +x right, +y down (qt_TexCoord0 origin is top-left).
    vec2 p = (qt_TexCoord0 - vec2(0.5)) * resolution;

    // Pick the radius for the quadrant this fragment falls in.
    float r = (p.x < 0.0)
        ? (p.y < 0.0 ? radii.x : radii.w)
        : (p.y < 0.0 ? radii.y : radii.z);

    vec2 q = abs(p) - resolution * 0.5 + vec2(r);
    float d = min(max(q.x, q.y), 0.0) + length(max(q, vec2(0.0))) - r;

    // Coverage: 1 inside, fading to 0 across an ~aa-wide band at the edge.
    float coverage = clamp(0.5 - d / max(aa, 0.0001), 0.0, 1.0);

    // The rounding is a property of the item, so the distance field above stays
    // in item space; only the sample is remapped into the visible part of the
    // texture.
    vec2 uv = sourceRect.xy + qt_TexCoord0 * sourceRect.zw;

    // Source texture is premultiplied alpha; scaling the whole vec4 keeps it so.
    fragColor = texture(source, uv) * coverage * qt_Opacity;
}
