import K10sCore
import SwiftUI

/// Sheet slot (T7), presented when tab.newTabOpen. Opens a scope with openWindow(value: TabScope) and
/// sets tab.newTabOpen = false. Prefill with tab.scope (the "Edit set" button opens this same sheet).
struct NewTabSheet: View {
    var tab: TabModel
    @Environment(EngineClient.self) private var engine
    @Environment(\.openWindow) private var openWindow
    @State private var names: [String] = []

    var body: some View {
        VStack(alignment: .leading, spacing: Hy.s3) {
            HyLabel("New tab")
            ForEach(names, id: \.self) { name in
                Button(name) {
                    openWindow(value: TabScope(contexts: [name]))
                    tab.newTabOpen = false
                }
                .buttonStyle(HyButton(kind: .quiet, small: true))
            }
            Button("Cancel") { tab.newTabOpen = false }
                .buttonStyle(HyButton(kind: .secondary, small: true))
        }
        .padding(Hy.s5)
        .frame(minWidth: 360)
        .background(Hy.skyRaised)
        .task {
            let r: ContextsListResult? = try? await engine.request(Method.contextsList, Empty())
            names = r?.contexts.map(\.name) ?? []
        }
    }
}
