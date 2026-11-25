import K10sCore
import SwiftUI

/// The confirm sheet for writes (ADR 0004, decision 4). Reused by delete, scale, restart and
/// the protected Save in the inspector.
///
/// - `title`: "Delete pod?"; `context`/`namespace` form the faint mono line.
/// - `consequence`: what happens, as Text (callers lead with the name in mono).
/// - `protected` + `name`: when `ConfirmRule.needsTypedName`, shows the PROTECTED tag and a name
///   field, and the action stays disabled until the typed text matches `name`.
/// - `noun`/`verb`: the typed-name prompt reads "Type the <noun>’s name to <verb> it."
/// - `canRun`: extra enable condition (e.g. the scale value is valid).
/// - `content`: extra fields between the consequence and the name field.
/// - `run`: the write. A throw shows its message and keeps the sheet open; success dismisses.
struct ConfirmSheet<Content: View>: View {
    let title: String
    let context: String
    let namespace: String?
    let consequence: Text
    let protected: Bool
    let name: String
    var noun = "object"
    var verb = "delete"
    let actionLabel: String
    var danger = false
    var canRun = true
    @ViewBuilder var content: () -> Content
    let run: () async throws -> Void

    @Environment(\.dismiss) private var dismiss
    @State private var typed = ""
    @State private var busy = false
    @State private var error: String?
    @FocusState private var typedFocused: Bool

    private var needsName: Bool { ConfirmRule.needsTypedName(protected: protected) }
    private var enabled: Bool {
        canRun && !busy && (!needsName || ConfirmRule.matches(typed: typed, name: name))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Hy.s3) {
            HStack {
                Text(title).font(Hy.display(22)).foregroundStyle(Hy.starlight)
                Spacer(minLength: Hy.s3)
                if needsName { HyTag(text: "protected", kind: .m) }
            }
            Text(namespace.map { "\(context) · \($0)" } ?? context)
                .font(Hy.mono(12)).foregroundStyle(Hy.faint)
            consequence.font(Hy.ui(13.5)).foregroundColor(Hy.dust).lineSpacing(4)
            content()
            if needsName {
                Text("This context is protected. Type the \(noun)’s name to \(verb) it.")
                    .font(Hy.ui(13.5)).foregroundStyle(Hy.dust)
                VStack(alignment: .leading, spacing: 6) {
                    HyLabel("Name")
                    TextField("", text: $typed)
                        .textFieldStyle(.plain)
                        .font(Hy.mono(13)).foregroundStyle(Hy.starlight)
                        .padding(.horizontal, Hy.s3).frame(height: 34)
                        .background(Hy.sky)
                        .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
                        .focused($typedFocused)
                        .onSubmit { go() }
                        .accessibilityLabel("Type the name to confirm")
                }
            }
            if let error {
                Text(error).font(Hy.ui(13)).foregroundStyle(Hy.classM).textSelection(.enabled)
            }
            HStack(spacing: Hy.s2) {
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(HyButton(kind: .quiet))
                    .keyboardShortcut(.cancelAction)
                Button(actionLabel) { go() }
                    .buttonStyle(HyButton(kind: danger ? .danger : .primary))
                    .disabled(!enabled)
            }
            .padding(.top, Hy.s2)
        }
        .padding(Hy.s5)
        .frame(width: 520)
        .background(Hy.skyRaised)
        .overlay(RoundedRectangle(cornerRadius: Hy.r2).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: Hy.r2))
        .onAppear { if needsName { typedFocused = true } }
    }

    private func go() {
        guard enabled else { return }
        busy = true
        error = nil
        Task {
            do {
                try await run()
                dismiss()
            } catch {
                self.error = error.localizedDescription
            }
            busy = false
        }
    }
}

extension ConfirmSheet where Content == EmptyView {
    init(title: String, context: String, namespace: String?, consequence: Text, protected: Bool,
         name: String, noun: String = "object", verb: String = "delete", actionLabel: String,
         danger: Bool = false, canRun: Bool = true, run: @escaping () async throws -> Void) {
        self.init(title: title, context: context, namespace: namespace, consequence: consequence,
                  protected: protected, name: name, noun: noun, verb: verb, actionLabel: actionLabel,
                  danger: danger, canRun: canRun, content: { EmptyView() }, run: run)
    }
}
