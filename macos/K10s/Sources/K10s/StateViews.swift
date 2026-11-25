import K10sCore
import SwiftUI

/// Shared pane frame: left-aligned, max 560 wide, padding 32/48, vertically centered.
private struct StatePane<Content: View>: View {
    @ViewBuilder let content: Content
    var body: some View {
        VStack(alignment: .leading, spacing: Hy.s3) { content }
            .frame(maxWidth: 560, alignment: .leading)
            .padding(.vertical, Hy.s6)
            .padding(.horizontal, 48)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .background(Hy.sky)
    }
}

private struct StateTitle: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View { Text(text).font(Hy.display(22)).foregroundStyle(Hy.starlight) }
}

private struct StateText: View {
    let text: Text
    var body: some View { text.font(Hy.ui(13.5)).foregroundStyle(Hy.dust).fixedSize(horizontal: false, vertical: true) }
}

private func code(_ s: String) -> Text {
    Text(s).font(Hy.mono(12.5)).foregroundColor(Hy.starlight)
}

private struct ErrorBox: View {
    let text: String
    var body: some View {
        Text(text)
            .font(Hy.mono(12)).foregroundStyle(Hy.dust)
            .textSelection(.enabled)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(Hy.s3)
            .background(Hy.skyRaised)
            .overlay(Rectangle().strokeBorder(Hy.neatline, lineWidth: 1))
    }
}

/// Whole window when contexts.list is empty.
struct NoKubeconfigView: View {
    var body: some View {
        StatePane {
            StarGlyph(state: .stopped, size: 20)
            StateTitle("No kube contexts found")
            StateText(text: Text("k10s reads ") + code("~/.kube/config") + Text(" and every file in ") + code("KUBECONFIG")
                + Text(" from your login shell. Neither has a context yet."))
            StateText(text: Text("Add a cluster with your provider\u{2019}s CLI, for example ") + code("gcloud container clusters get-credentials")
                + Text(". k10s picks it up as soon as the file changes."))
        }
    }
}

/// Pane for an unreachable or unauthorized context. Retry posts `Notification.Name("k10sRetryContext")`
/// with the context name as `object`.
struct ContextErrorView: View {
    let context: String
    let status: ContextStatus

    private var unauthorized: Bool { status.state == .unauthorized }
    private var errorText: String { status.error ?? status.state.rawValue }
    private var viaExec: Bool { (status.error ?? "").lowercased().contains("exec") }

    var body: some View {
        StatePane {
            HStack(spacing: Hy.s2) {
                StarGlyph(state: .failed, size: 12)
                HyLabel("\(context) \u{00b7} \(status.state.rawValue)")
            }
            if unauthorized {
                StateTitle("\(context) refused your credentials")
                StateText(text: Text(viaExec
                    ? "The exec plugin for this context could not get a token. Other contexts in this tab keep working."
                    : "The cluster rejected your credentials. Other contexts in this tab keep working."))
                ErrorBox(text: errorText)
                StateText(text: Text("Install the plugin, or sign in again with ") + code("gcloud auth login") + Text(", then retry."))
            } else {
                StateTitle("\(context) does not answer")
                StateText(text: Text("k10s keeps trying every 30 seconds and shows the rows again when it answers. Check your VPN if this cluster has a private endpoint."))
                ErrorBox(text: errorText)
            }
            Button(unauthorized ? "Retry" : "Retry now") {
                NotificationCenter.default.post(name: Notification.Name("k10sRetryContext"), object: context)
            }
            .buttonStyle(HyButton(kind: .secondary))
            .padding(.top, Hy.s1)
        }
    }
}

/// A kind with no objects in the namespace.
struct EmptyKindView: View {
    let kind: KindEntry
    let namespace: String?

    private var title: String {
        if !kind.namespaced { return "No \(kind.plural)" }
        if let namespace { return "No \(kind.plural) in \(namespace)" }
        return "No \(kind.plural) in any namespace"
    }

    var body: some View {
        StatePane {
            StateTitle(title)
            StateText(text: Text("The namespace exists and k10s is watching it. New \(kind.plural) appear here as they are created."))
        }
    }
}
