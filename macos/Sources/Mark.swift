import AppKit

// The aibase mark: a spark (the AI) resting on two stacked layers (the base).
// Drawn as vectors so the menu bar glyph and app icon stay sharp at any scale.
enum Mark {
    static func spark(center c: CGPoint, radius r: CGFloat) -> NSBezierPath {
        let p = NSBezierPath()
        let w = r * 0.28
        p.move(to: CGPoint(x: c.x, y: c.y + r))
        p.curve(to: CGPoint(x: c.x + r, y: c.y), controlPoint1: CGPoint(x: c.x + w * 0.35, y: c.y + w), controlPoint2: CGPoint(x: c.x + w, y: c.y + w * 0.35))
        p.curve(to: CGPoint(x: c.x, y: c.y - r), controlPoint1: CGPoint(x: c.x + w, y: c.y - w * 0.35), controlPoint2: CGPoint(x: c.x + w * 0.35, y: c.y - w))
        p.curve(to: CGPoint(x: c.x - r, y: c.y), controlPoint1: CGPoint(x: c.x - w * 0.35, y: c.y - w), controlPoint2: CGPoint(x: c.x - w, y: c.y - w * 0.35))
        p.curve(to: CGPoint(x: c.x, y: c.y + r), controlPoint1: CGPoint(x: c.x - w, y: c.y + w * 0.35), controlPoint2: CGPoint(x: c.x - w * 0.35, y: c.y + w))
        p.close()
        return p
    }

    // Layers and spark laid out in a unit square (origin bottom-left).
    static func parts(in rect: CGRect) -> (bottom: NSBezierPath, middle: NSBezierPath, spark: NSBezierPath) {
        let s = rect.width
        func r(_ x: CGFloat, _ y: CGFloat, _ w: CGFloat, _ h: CGFloat) -> NSBezierPath {
            let box = CGRect(x: rect.minX + x * s, y: rect.minY + y * s, width: w * s, height: h * s)
            return NSBezierPath(roundedRect: box, xRadius: box.height / 2, yRadius: box.height / 2)
        }
        return (r(0.08, 0.10, 0.84, 0.19), r(0.21, 0.37, 0.58, 0.17), spark(center: CGPoint(x: rect.midX, y: rect.minY + 0.76 * s), radius: 0.22 * s))
    }

    static let template: NSImage = {
        let img = NSImage(size: NSSize(width: 18, height: 18), flipped: false) { rect in
            NSColor.black.setFill()
            let p = parts(in: rect.insetBy(dx: 0.5, dy: 0.5))
            p.bottom.fill()
            p.middle.fill()
            p.spark.fill()
            return true
        }
        img.isTemplate = true
        return img
    }()

    static func drawAppIcon(in rect: CGRect) {
        let inset = rect.width * 0.1
        let tile = rect.insetBy(dx: inset, dy: inset)
        let radius = tile.width * 0.225
        let shape = NSBezierPath(roundedRect: tile, xRadius: radius, yRadius: radius)
        NSGraphicsContext.saveGraphicsState()
        let shadow = NSShadow()
        shadow.shadowColor = NSColor.black.withAlphaComponent(0.28)
        shadow.shadowBlurRadius = rect.width * 0.03
        shadow.shadowOffset = NSSize(width: 0, height: -rect.width * 0.012)
        shadow.set()
        NSColor.black.setFill()
        shape.fill()
        NSGraphicsContext.restoreGraphicsState()

        NSGradient(colors: [Palette.indigo, Palette.violet, Palette.magenta], atLocations: [0, 0.6, 1], colorSpace: .sRGB)!.draw(in: shape, angle: -55)
        NSGraphicsContext.saveGraphicsState()
        shape.addClip()
        NSGradient(colors: [NSColor.white.withAlphaComponent(0.13), NSColor.white.withAlphaComponent(0)])!
            .draw(in: NSBezierPath(ovalIn: CGRect(x: tile.minX - tile.width * 0.2, y: tile.midY, width: tile.width * 1.4, height: tile.height * 0.9)), angle: -90)
        NSGraphicsContext.restoreGraphicsState()

        let p = parts(in: tile.insetBy(dx: tile.width * 0.17, dy: tile.width * 0.17))
        NSColor.white.withAlphaComponent(0.62).setFill()
        p.bottom.fill()
        NSColor.white.withAlphaComponent(0.82).setFill()
        p.middle.fill()
        NSGraphicsContext.saveGraphicsState()
        let glow = NSShadow()
        glow.shadowColor = NSColor.white.withAlphaComponent(0.7)
        glow.shadowBlurRadius = rect.width * 0.04
        glow.set()
        NSColor.white.setFill()
        p.spark.fill()
        NSGraphicsContext.restoreGraphicsState()
    }

    static func appIcon(size: CGFloat) -> NSImage {
        NSImage(size: NSSize(width: size, height: size), flipped: false) { rect in
            drawAppIcon(in: rect)
            return true
        }
    }

    // Writes an .iconset for iconutil (used by the build script).
    static func renderIconset(to dir: String) throws {
        try FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        for base in [16, 32, 128, 256, 512] {
            for scale in [1, 2] {
                let px = base * scale
                guard let rep = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: px, pixelsHigh: px, bitsPerSample: 8, samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0) else { continue }
                NSGraphicsContext.saveGraphicsState()
                NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
                drawAppIcon(in: CGRect(x: 0, y: 0, width: px, height: px))
                NSGraphicsContext.restoreGraphicsState()
                let name = scale == 1 ? "icon_\(base)x\(base).png" : "icon_\(base)x\(base)@2x.png"
                try rep.representation(using: .png, properties: [:])?.write(to: URL(fileURLWithPath: dir).appendingPathComponent(name))
            }
        }
    }
}

enum Palette {
    static let indigo = NSColor(srgbRed: 0.31, green: 0.27, blue: 0.90, alpha: 1)
    static let violet = NSColor(srgbRed: 0.55, green: 0.24, blue: 0.93, alpha: 1)
    static let magenta = NSColor(srgbRed: 0.85, green: 0.27, blue: 0.75, alpha: 1)

    static func client(_ name: String) -> NSColor {
        switch name {
        case "claude": return NSColor(srgbRed: 0.85, green: 0.47, blue: 0.34, alpha: 1)
        case "codex": return NSColor(srgbRed: 0.06, green: 0.64, blue: 0.50, alpha: 1)
        case "gemini": return NSColor(srgbRed: 0.26, green: 0.52, blue: 0.96, alpha: 1)
        case "cursor", "cursor-agent": return NSColor(srgbRed: 0.55, green: 0.55, blue: 0.60, alpha: 1)
        case "opencode": return NSColor(srgbRed: 0.96, green: 0.62, blue: 0.04, alpha: 1)
        default: return NSColor.systemGray
        }
    }

    static func clientName(_ name: String) -> String {
        switch name {
        case "claude": return "Claude"
        case "codex": return "Codex"
        case "gemini": return "Gemini"
        case "cursor", "cursor-agent": return "Cursor"
        case "opencode": return "opencode"
        default: return name.capitalized
        }
    }
}
