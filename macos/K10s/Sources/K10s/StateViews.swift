import K10sCore
import SwiftUI

private struct StatePlaceholder: View {
    let text: String
    var body: some View {
        Text(text)
            .font(Hy.ui(13)).foregroundStyle(Hy.dust)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .background(Hy.sky)
    }
}

/// Whole window when contexts.list is empty (T7 fills).
struct NoKubeconfigView: View {
    var body: some View { StatePlaceholder(text: "No kubeconfig found.") }
}

/// Pane for an unreachable or unauthorized context (T7 fills; the list shows it).
struct ContextErrorView: View {
    let context: String
    let status: ContextStatus
    var body: some View { StatePlaceholder(text: "\(context): \(status.error ?? status.state.rawValue)") }
}

/// A kind with no objects in the namespace (T7 fills; the list shows it).
struct EmptyKindView: View {
    let kind: KindEntry
    let namespace: String?
    var body: some View { StatePlaceholder(text: "No \(kind.plural) in \(namespace ?? "any namespace").") }
}
