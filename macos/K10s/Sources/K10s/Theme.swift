import AppKit
import CoreText
import SwiftUI

// The hyad design system (night theme), translated to SwiftUI: a star chart
// read at night. Flat grounds, 1px neatlines, square plates, colour only for
// state, and light only when something is true (a live grant).
// Source of truth: hyad docs/v2/design-system/tokens.json. Copied verbatim from iris for k10s.

extension Color {
    init(hex: UInt32, alpha: Double = 1) {
        self.init(.sRGB,
                  red: Double((hex >> 16) & 0xff) / 255,
                  green: Double((hex >> 8) & 0xff) / 255,
                  blue: Double(hex & 0xff) / 255,
                  opacity: alpha)
    }
}

enum Hy {
    // Grounds: sky -> sky-raised -> sky-high.
    static let sky = Color(hex: 0x0a0e13)
    static let skyRaised = Color(hex: 0x10161d)
    static let skyHigh = Color(hex: 0x18212b)
    static let neatline = Color(hex: 0x27323f)
    static let neatlineStrong = Color(hex: 0x66788c)
    // Text.
    static let starlight = Color(hex: 0xece7dd)
    static let starlightPress = Color(hex: 0xcfc9bd)
    static let onStarlight = Color(hex: 0x0a0e13)
    static let dust = Color(hex: 0xa3aebb)
    static let faint = Color(hex: 0x7d8997)
    // Spectral classes: the only status palette.
    static let classA = Color(hex: 0xbdd1ff) // live
    static let classG = Color(hex: 0xf1e1b0) // pending
    static let classK = Color(hex: 0xf2a660) // degraded
    static let classM = Color(hex: 0xf4806c) // failed / destructive
    static let brown = Color(hex: 0x7d8997)  // gives no light
    static let tintA = Color(hex: 0xbdd1ff, alpha: 0.12)
    static let tintG = Color(hex: 0xf1e1b0, alpha: 0.12)
    static let tintK = Color(hex: 0xf2a660, alpha: 0.12)
    static let tintM = Color(hex: 0xf4806c, alpha: 0.12)
    static let halo = 0.14

    // Spacing (4px base) and radii.
    static let s1: CGFloat = 4, s2: CGFloat = 8, s3: CGFloat = 12, s4: CGFloat = 16, s5: CGFloat = 24, s6: CGFloat = 32
    static let r1: CGFloat = 2, r2: CGFloat = 6

    // MARK: fonts

    /// Registers the bundled variable fonts (Contents/Resources/Fonts).
    static func registerFonts() {
        guard let dir = Bundle.main.resourceURL?.appendingPathComponent("Fonts"),
              let files = try? FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil) else { return }
        for f in files where f.pathExtension == "ttf" {
            CTFontManagerRegisterFontsForURL(f as CFURL, .process, nil)
        }
    }

    private static let wghtAxis = 0x7767_6874 // 'wght'

    private static func variable(_ postscript: String, size: CGFloat, weight: CGFloat, fallback: NSFont.Weight, mono: Bool = false) -> Font {
        guard NSFont(name: postscript, size: size) != nil else {
            return mono ? .system(size: size, weight: Font.Weight(fallback), design: .monospaced)
                        : .system(size: size, weight: Font.Weight(fallback))
        }
        let attrs: [NSFontDescriptor.AttributeName: Any] = [
            .name: postscript,
            NSFontDescriptor.AttributeName(rawValue: kCTFontVariationAttribute as String): [wghtAxis: weight],
        ]
        let nsFont = NSFont(descriptor: NSFontDescriptor(fontAttributes: attrs), size: size) ?? .systemFont(ofSize: size)
        return Font(nsFont as CTFont)
    }

    /// Commissioner: everything people read and press.
    static func ui(_ size: CGFloat, _ weight: CGFloat = 400) -> Font {
        variable("CommissionerThin-Regular", size: size, weight: weight, fallback: weight >= 600 ? .semibold : .regular)
    }
    /// Spline Sans Mono: targets, commands, figures.
    static func mono(_ size: CGFloat, _ weight: CGFloat = 400) -> Font {
        variable("SplineSansMono-Regular", size: size, weight: weight, fallback: .regular, mono: true)
    }
    /// Literata: titles only.
    static func display(_ size: CGFloat, _ weight: CGFloat = 400) -> Font {
        variable("Literata-Regular", size: size, weight: weight, fallback: .regular)
    }
}

private extension Font.Weight {
    init(_ w: NSFont.Weight) {
        switch w {
        case .semibold: self = .semibold
        case .bold: self = .bold
        case .light: self = .light
        default: self = .regular
        }
    }
}

// MARK: - Components

/// Uppercase, tracked 0.12em, dust: field labels and plate titles.
struct HyLabel: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Text(text.uppercased())
            .font(Hy.ui(11, 600))
            .tracking(11 * 0.12)
            .foregroundStyle(Hy.dust)
    }
}

/// State glyphs: shape carries the state, colour repeats it.
struct StarGlyph: View {
    enum State { case live, building, degraded, failed, stopped }
    let state: State
    var size: CGFloat = 12

    var body: some View {
        Canvas { ctx, sz in
            let c = CGPoint(x: sz.width / 2, y: sz.height / 2)
            let r = min(sz.width, sz.height) / 2
            func disc(_ rad: CGFloat) -> Path { Path(ellipseIn: CGRect(x: c.x - rad, y: c.y - rad, width: 2 * rad, height: 2 * rad)) }
            switch state {
            case .live:
                ctx.fill(disc(r), with: .color(Hy.classA.opacity(Hy.halo)))
                ctx.fill(disc(r * 0.5), with: .color(Hy.classA))
            case .building:
                ctx.stroke(disc(r * 0.8), with: .color(Hy.classG), lineWidth: 1.25)
                ctx.fill(disc(r * 0.4), with: .color(Hy.classG))
            case .degraded:
                ctx.stroke(disc(r * 0.6), with: .color(Hy.classK), lineWidth: 1.5)
                var half = Path()
                half.addArc(center: c, radius: r * 0.6, startAngle: .degrees(90), endAngle: .degrees(270), clockwise: false)
                half.closeSubpath()
                ctx.fill(half, with: .color(Hy.classK))
            case .failed:
                var d = Path()
                let k = r * 0.6
                d.move(to: CGPoint(x: c.x, y: c.y - k))
                d.addLine(to: CGPoint(x: c.x + k, y: c.y))
                d.addLine(to: CGPoint(x: c.x, y: c.y + k))
                d.addLine(to: CGPoint(x: c.x - k, y: c.y))
                d.closeSubpath()
                ctx.fill(d, with: .color(Hy.classM))
            case .stopped:
                ctx.stroke(disc(r * 0.5), with: .color(Hy.brown), lineWidth: 1.5)
            }
        }
        .frame(width: size, height: size)
    }
}

/// Tag: 20px, uppercase, tinted by spectral class.
struct HyTag: View {
    enum Kind { case a, g, k, m, plain }
    let text: String
    var kind: Kind = .plain

    var body: some View {
        let (fg, bg): (Color, Color) = {
            switch kind {
            case .a: return (Hy.classA, Hy.tintA)
            case .g: return (Hy.classG, Hy.tintG)
            case .k: return (Hy.classK, Hy.tintK)
            case .m: return (Hy.classM, Hy.tintM)
            case .plain: return (Hy.dust, .clear)
            }
        }()
        Text(text.uppercased())
            .font(Hy.ui(10, 600))
            .tracking(10 * 0.12)
            .foregroundStyle(fg)
            .padding(.horizontal, Hy.s2)
            .frame(height: 18)
            .background(RoundedRectangle(cornerRadius: Hy.r1).fill(bg))
            .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(kind == .plain ? Hy.neatline : .clear, lineWidth: 1))
    }
}

/// hyad buttons: never pills, radius 2, 36px (28px small).
struct HyButton: ButtonStyle {
    enum Kind { case primary, secondary, quiet, danger }
    var kind: Kind = .secondary
    var small = false

    func makeBody(configuration: Configuration) -> some View {
        HyButtonBody(configuration: configuration, kind: kind, small: small)
    }

    struct HyButtonBody: View {
        let configuration: Configuration
        let kind: Kind
        let small: Bool
        @State private var hover = false
        @Environment(\.isEnabled) private var enabled

        var body: some View {
            let pressed = configuration.isPressed
            configuration.label
                .font(Hy.ui(small ? 12.5 : 13.5, 600))
                .lineLimit(1)
                .foregroundStyle(fg)
                .padding(.horizontal, small ? Hy.s3 : Hy.s4)
                .frame(minHeight: small ? 26 : 34)
                .background(RoundedRectangle(cornerRadius: Hy.r1).fill(bg(hover: hover || pressed)))
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(edge, lineWidth: 1))
                .contentShape(Rectangle())
                .opacity(enabled ? 1 : 0.45)
                .onHover { h in withAnimation(.easeOut(duration: 0.12)) { hover = h } }
        }

        private var fg: Color {
            switch kind {
            case .primary: return Hy.onStarlight
            case .secondary: return Hy.starlight
            case .quiet: return hover ? Hy.starlight : Hy.dust
            case .danger: return Hy.classM
            }
        }

        private func bg(hover: Bool) -> Color {
            switch kind {
            case .primary: return hover ? Hy.starlightPress : Hy.starlight
            case .secondary, .quiet: return hover ? Hy.skyHigh : .clear
            case .danger: return hover ? Hy.tintM : .clear
            }
        }

        private var edge: Color {
            switch kind {
            case .primary, .quiet: return .clear
            case .secondary: return Hy.neatlineStrong
            case .danger: return Hy.classM
            }
        }
    }
}

/// hyad toggle: off is a dim brown knob, on is a lit class-a knob; the word states the value.
struct HyToggle: ToggleStyle {
    var onLabel = "On"
    var offLabel = "Off"

    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: Hy.s3) {
            configuration.label
                .font(Hy.ui(13))
                .foregroundStyle(Hy.starlight)
            Spacer(minLength: Hy.s2)
            Text(configuration.isOn ? onLabel : offLabel)
                .font(Hy.ui(13))
                .foregroundStyle(Hy.dust)
            ZStack(alignment: configuration.isOn ? .trailing : .leading) {
                Capsule().strokeBorder(configuration.isOn ? Hy.classA : Hy.neatlineStrong, lineWidth: 1)
                Circle()
                    .fill(configuration.isOn ? Hy.classA : Hy.brown)
                    .frame(width: 12, height: 12)
                    .padding(3)
            }
            .frame(width: 36, height: 20)
            .contentShape(Rectangle())
            .onTapGesture { withAnimation(.easeOut(duration: 0.12)) { configuration.isOn.toggle() } }
        }
    }
}

/// Plate: a printed rectangle with corner registration marks.
struct Plate<Content: View>: View {
    let title: String
    var caption: String? = nil
    var trailing: AnyView? = nil
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(alignment: .center, spacing: Hy.s3) {
                HyLabel(title)
                Spacer()
                if let caption {
                    Text(caption).font(Hy.mono(11)).foregroundStyle(Hy.faint)
                }
                if let trailing { trailing }
            }
            .padding(.horizontal, Hy.s4)
            .frame(minHeight: 36)
            Rectangle().fill(Hy.neatline).frame(height: 1)
            content
        }
        .background(Hy.skyRaised)
        .overlay(Rectangle().strokeBorder(Hy.neatline, lineWidth: 1))
        .overlay(RegistrationMarks())
    }
}

struct RegistrationMarks: View {
    var body: some View {
        GeometryReader { g in
            Path { p in
                let w = g.size.width, h = g.size.height, k: CGFloat = 5
                let corners: [(CGFloat, CGFloat, CGFloat, CGFloat)] = [(0, 0, 1, 1), (w, 0, -1, 1), (0, h, 1, -1), (w, h, -1, -1)]
                for (x, y, dx, dy) in corners {
                    p.move(to: CGPoint(x: x - dx * k, y: y))
                    p.addLine(to: CGPoint(x: x + dx * k, y: y))
                    p.move(to: CGPoint(x: x, y: y - dy * k))
                    p.addLine(to: CGPoint(x: x, y: y + dy * k))
                }
            }
            .stroke(Hy.neatlineStrong, lineWidth: 1)
        }
        .allowsHitTesting(false)
    }
}

/// Hairline divider between rows.
struct Neatline: View {
    var body: some View { Rectangle().fill(Hy.neatline).frame(height: 1) }
}
