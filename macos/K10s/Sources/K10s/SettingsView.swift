import K10sCore
import SwiftUI

/// Settings: the one pane, "Protected contexts" (ADR 0004).
struct SettingsView: View {
    @Environment(EngineClient.self) private var engine
    @Environment(Protection.self) private var protection
    @State private var words = ""
    @State private var contexts: [ContextInfo] = []
    @FocusState private var wordsFocused: Bool

    private var protectedCount: Int { contexts.filter { protection.isProtected($0.name) }.count }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            Text("Protected contexts").font(Hy.display(22)).foregroundStyle(Hy.starlight)
            Text("Writes in a protected context ask you to type the object’s name. By default a context is protected when its name contains one of these words. Set any context by hand to override that.")
                .font(Hy.ui(13.5)).foregroundStyle(Hy.dust)
                .fixedSize(horizontal: false, vertical: true)
            VStack(alignment: .leading, spacing: 6) {
                HyLabel("Words, comma separated")
                TextField("", text: $words)
                    .textFieldStyle(.plain)
                    .font(Hy.mono(13))
                    .foregroundStyle(Hy.starlight)
                    .focused($wordsFocused)
                    .padding(.horizontal, Hy.s3)
                    .frame(height: 34)
                    .background(Hy.sky)
                    .overlay(RoundedRectangle(cornerRadius: Hy.r1).stroke(Hy.neatlineStrong, lineWidth: 1))
                    .accessibilityLabel("Protected words")
                    .onSubmit { protection.setWords(words) }
                    .onChange(of: wordsFocused) { _, focused in
                        if !focused { protection.setWords(words) }
                    }
            }
            HyLabel("\(protectedCount) of \(contexts.count) contexts protected")
            ScrollView {
                VStack(spacing: 0) {
                    ForEach(Array(contexts.enumerated()), id: \.element.name) { i, c in
                        if i > 0 { Neatline() }
                        row(c.name)
                    }
                }
            }
            .background(Hy.sky)
            .overlay(Rectangle().strokeBorder(Hy.neatline, lineWidth: 1))
            Text("Also: right-click a context in the toolbar → Protect / Stop protecting.")
                .font(Hy.ui(12)).foregroundStyle(Hy.faint)
        }
        .padding(Hy.s5)
        .frame(width: 640, height: 560)
        .background(Hy.skyRaised)
        .environment(\.colorScheme, .dark)
        .onAppear { words = protection.words.joined(separator: ", ") }
        .task(id: engine.restarts) {
            if let r: ContextsListResult = try? await engine.request(Method.contextsList, Empty()) {
                contexts = r.contexts
            }
        }
    }

    private func row(_ name: String) -> some View {
        let pick = Binding<Protection.Override?>(
            get: { protection.overrides[name] },
            set: { protection.setOverride(name, $0) }
        )
        let lower = name.lowercased()
        let word = protection.words.first { !$0.isEmpty && lower.contains($0.lowercased()) }
        return HStack(spacing: 10) {
            Text(name).font(Hy.mono(12)).foregroundStyle(Hy.starlight)
                .lineLimit(1).truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            Group {
                if protection.isProtected(name) { HyTag(text: "protected") } else { Color.clear }
            }
            .frame(width: 92, alignment: .leading)
            Picker(name, selection: pick) {
                Text(word.map { "Automatic · \($0)" } ?? "Automatic").tag(Protection.Override?.none)
                Text("Protected").tag(Protection.Override?.some(.protected))
                Text("Not protected").tag(Protection.Override?.some(.notProtected))
            }
            .labelsHidden()
            .pickerStyle(.menu)
            .font(Hy.ui(12))
            .frame(width: 150)
        }
        .padding(.horizontal, Hy.s3)
        .frame(height: 34)
    }
}
