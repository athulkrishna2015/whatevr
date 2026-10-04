import CoreGraphics
import ImageIO
import Foundation
import UniformTypeIdentifiers
let destination = URL(fileURLWithPath: CommandLine.arguments[1], isDirectory: true)
try FileManager.default.createDirectory(at: destination, withIntermediateDirectories: true)
for size in [16, 32, 128, 256, 512] {
    for scale in [1, 2] {
        let pixels = size * scale
        let context = CGContext(data: nil, width: pixels, height: pixels, bitsPerComponent: 8, bytesPerRow: pixels * 4,
                                space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        context.scaleBy(x: CGFloat(pixels) / 1024, y: CGFloat(pixels) / 1024)
        context.setFillColor(CGColor(red: 0.08, green: 0.14, blue: 0.18, alpha: 1))
        context.addPath(CGPath(roundedRect: CGRect(x: 32, y: 32, width: 960, height: 960), cornerWidth: 220, cornerHeight: 220, transform: nil))
        context.fillPath()
        context.setFillColor(CGColor(red: 0.24, green: 0.83, blue: 0.62, alpha: 1))
        context.addPath(CGPath(roundedRect: CGRect(x: 168, y: 250, width: 688, height: 550), cornerWidth: 140, cornerHeight: 140, transform: nil))
        context.fillPath()
        context.move(to: CGPoint(x: 260, y: 290)); context.addLine(to: CGPoint(x: 210, y: 150)); context.addLine(to: CGPoint(x: 450, y: 290)); context.closePath(); context.fillPath()
        context.setFillColor(CGColor(red: 0.08, green: 0.14, blue: 0.18, alpha: 1))
        for x in [320, 512, 704] { context.fillEllipse(in: CGRect(x: x - 44, y: 481, width: 88, height: 88)) }
        let filename = "icon_\(size)x\(size)" + (scale == 2 ? "@2x" : "") + ".png"
        let output = CGImageDestinationCreateWithURL(destination.appendingPathComponent(filename) as CFURL, UTType.png.identifier as CFString, 1, nil)!
        CGImageDestinationAddImage(output, context.makeImage()!, nil)
        guard CGImageDestinationFinalize(output) else { fatalError("Cannot write icon") }
    }
}
