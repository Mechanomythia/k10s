import K10sCore
import SwiftUI

/// Overlay slot (T6), shown (top-aligned) when tab.commandBarOpen. Sets tab.commandBarOpen = false to close.
/// Uses tab.catalog.complete/resolve, tab.select(kind:), tab.namespace.
struct CommandBar: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine

    var body: some View {
        Text(":")
            .font(Hy.mono(13)).foregroundStyle(Hy.starlight)
            .frame(width: 480, height: 40, alignment: .leading)
            .padding(.horizontal, Hy.s3)
            .background(Hy.skyHigh)
            .padding(.top, 60)
    }
}
