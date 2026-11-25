import AppKit
import K10sCore
import SwiftUI

/// Per-window root: resolves the tab's scope (first window: kubeconfig's current context), then shows the main window.
struct WindowRoot: View {
    @Binding var scope: TabScope?
    @Environment(EngineClient.self) private var engine
    @State private var noContexts = false

    var body: some View {
        Group {
            if let scope {
                MainWindow(scope: scope)
                    .id(scope)
                    .environment(\.replaceScope, { self.scope = $0 })
            } else if noContexts {
                NoKubeconfigView()
            } else {
                Hy.sky
            }
        }
        .frame(minWidth: 900, minHeight: 560)
        .background(Hy.sky)
        .environment(\.colorScheme, .dark)
        .navigationTitle(scope?.displayName ?? "k10s")
        .background(WindowAccessor(title: scope?.displayName ?? "k10s"))
        .task(id: engine.restarts) {
            guard scope == nil else { return }
            let r: ContextsListResult? = try? await engine.request(Method.contextsList, Empty())
            let list = r?.contexts ?? []
            if let c = list.first(where: { $0.current }) ?? list.first {
                scope = TabScope(contexts: [c.name])
            } else {
                noContexts = true
            }
        }
    }
}

/// Sets native tabbing and the window title.
struct WindowAccessor: NSViewRepresentable {
    let title: String

    final class Hook: NSView {
        var title = ""
        override func viewDidMoveToWindow() { apply() }
        func apply() {
            guard let w = window else { return }
            w.tabbingMode = .preferred
            w.tabbingIdentifier = "k10s.main"
            w.title = title
        }
    }

    func makeNSView(context: Context) -> Hook { Hook() }
    func updateNSView(_ view: Hook, context: Context) {
        view.title = title
        view.apply()
    }
}

struct MainWindow: View {
    @Environment(EngineClient.self) private var engine
    @State private var tab: TabModel

    init(scope: TabScope) {
        _tab = State(initialValue: TabModel(scope: scope))
    }

    var body: some View {
        VStack(spacing: 0) {
            ToolbarView(tab: tab)
            Neatline()
            HStack(spacing: 0) {
                if tab.sidebarVisible {
                    KindsSidebar(tab: tab)
                    Rectangle().fill(Hy.neatline).frame(width: 1)
                }
                ListPane(tab: tab)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
                if tab.inspectorOpen {
                    Rectangle().fill(Hy.neatline).frame(width: 1)
                    InspectorPane(tab: tab)
                        .frame(width: tab.inspectorTab == .logs ? 720 : 480)
                }
            }
            Neatline()
            StatusBar(tab: tab)
        }
        .background(Hy.sky)
        .modifier(CommandKeys(tab: tab))
        .overlay(alignment: .top) {
            if tab.commandBarOpen { CommandBar(tab: tab) }
        }
        .sheet(isPresented: $tab.newTabOpen) { NewTabSheet(tab: tab) }
        .focusedSceneValue(\.tabModel, tab)
        .task(id: engine.restarts) { await tab.load(engine) }
        .task(id: engine.restarts) { await tab.watchNamespaces(engine) }
    }
}

// MARK: - Menu commands

private struct TabModelKey: FocusedValueKey { typealias Value = TabModel }

extension FocusedValues {
    var tabModel: TabModel? {
        get { self[TabModelKey.self] }
        set { self[TabModelKey.self] = newValue }
    }
}

struct ShellCommands: Commands {
    @FocusedValue(\.tabModel) private var tab

    var body: some Commands {
        CommandGroup(replacing: .newItem) {
            Button("New Tab") { tab?.newTabOpen = true }
                .keyboardShortcut("t", modifiers: .command)
                .disabled(tab == nil)
        }
        CommandGroup(after: .sidebar) {
            Button(tab?.sidebarVisible == false ? "Show Kinds" : "Hide Kinds") { tab?.sidebarVisible.toggle() }
                .keyboardShortcut("0", modifiers: .command)
                .disabled(tab == nil)
        }
        CommandGroup(after: .windowArrangement) {
            ForEach(1...9, id: \.self) { n in
                Button("Tab \(n)") {
                    if let ws = NSApp.keyWindow?.tabbedWindows, ws.indices.contains(n - 1) {
                        ws[n - 1].makeKeyAndOrderFront(nil)
                    }
                }
                .keyboardShortcut(KeyEquivalent(Character(String(n))), modifiers: .command)
            }
        }
    }
}

// MARK: - Replacing a tab's scope in place (`:ctx` and `:set` without ⌘)

private struct ReplaceScopeKey: EnvironmentKey {
    static let defaultValue: (TabScope) -> Void = { _ in }
}

extension EnvironmentValues {
    /// Replaces this window's scope; the main window is rebuilt for the new contexts.
    var replaceScope: (TabScope) -> Void {
        get { self[ReplaceScopeKey.self] }
        set { self[ReplaceScopeKey.self] = newValue }
    }
}
