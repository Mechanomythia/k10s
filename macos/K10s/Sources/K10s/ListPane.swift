import K10sCore
import SwiftUI

/// Center slot (T4). Subscribes table.subscribe for tab.scope/kind/namespace, feeds tab.table,
/// forwards context.state events to tab.apply(_:), sets tab.selection / tab.inspectorOpen.
struct ListPane: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine

    var body: some View {
        Text(tab.kind?.plural ?? "")
            .font(Hy.mono(12)).foregroundStyle(Hy.faint)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Hy.sky)
    }
}
