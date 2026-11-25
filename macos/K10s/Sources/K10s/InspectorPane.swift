import K10sCore
import SwiftUI

/// Right slot (T5), shown when tab.inspectorOpen. The shell sets its width (480, 720 for logs); fill the height.
/// Reads tab.selection and tab.inspectorTab.
struct InspectorPane: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine

    var body: some View {
        Text(tab.inspectorTab.rawValue)
            .font(Hy.mono(12)).foregroundStyle(Hy.faint)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Hy.skyRaised)
    }
}
