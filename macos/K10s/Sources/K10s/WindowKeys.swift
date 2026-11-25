import AppKit
import K10sCore
import SwiftUI

/// Window-wide k9s keys that work wherever focus is, as long as you are not typing in a
/// text field. Per-view key handlers only fire when that exact view has focus, which is
/// why d/y/e/l used to beep after clicking the list.
struct WindowKeys: NSViewRepresentable {
    let tab: TabModel

    func makeNSView(context: Context) -> KeyMonitorView {
        let view = KeyMonitorView()
        view.tab = tab
        return view
    }

    func updateNSView(_ view: KeyMonitorView, context: Context) {
        view.tab = tab
    }

    /// Returns true when the key was handled (the event is then swallowed, so no beep).
    @MainActor
    static func handle(_ event: NSEvent, in window: NSWindow, tab: TabModel) -> Bool {
        // The command bar and the new-tab sheet handle their own keys.
        if tab.commandBarOpen || tab.newTabOpen { return false }
        // ⌘, ⌥ and ⌃ combinations belong to the menus.
        if !event.modifierFlags.intersection([.command, .option, .control]).isEmpty { return false }

        let isEscape = event.keyCode == 53
        let isReturn = event.keyCode == 36 || event.keyCode == 76

        // Typing in a text field (the filter, the log find field): Esc or Return leaves the
        // field and keeps its text; every other key is the field's.
        if let editor = window.firstResponder as? NSTextView, editor.isEditable {
            guard isEscape || isReturn else { return false }
            window.makeFirstResponder(nil)
            return true
        }

        if isEscape {
            if tab.inspectorOpen { tab.inspectorOpen = false; return true }
            if !tab.filter.isEmpty { tab.filter = ""; return true }
            return false
        }
        if isReturn {
            _ = tab.openInspector(.describe)
            return true
        }
        switch event.keyCode {
        case 125: tab.moveSelection(1); return true  // ↓
        case 126: tab.moveSelection(-1); return true // ↑
        default: break
        }
        switch event.characters {
        case ":": tab.commandBarOpen = true
        case "/": tab.focusFilter = true
        case "j": tab.moveSelection(1)
        case "k": tab.moveSelection(-1)
        case "d": _ = tab.openInspector(.describe)
        case "y": _ = tab.openInspector(.yaml)
        case "e": _ = tab.openInspector(.events)
        case "l": _ = tab.openInspector(.logs)
        default: return false
        }
        return true
    }
}

/// Holds the local key monitor for the window it lives in.
final class KeyMonitorView: NSView {
    weak var tab: TabModel?
    private var monitor: Any?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if let monitor {
            NSEvent.removeMonitor(monitor)
            self.monitor = nil
        }
        guard window != nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            MainActor.assumeIsolated {
                guard let self, let window = self.window, event.window === window,
                      let tab = self.tab else { return event }
                return WindowKeys.handle(event, in: window, tab: tab) ? nil : event
            }
        }
    }
}

extension TabModel {
    /// Moves the selection through the visible rows; selects the first (or last) when none is selected.
    func moveSelection(_ delta: Int) {
        let rows = table.rows
        guard !rows.isEmpty else { return }
        let current = selection.flatMap { s in
            rows.firstIndex { $0.context == s.context && $0.uid == s.uid }
        }
        let next = current.map { min(max($0 + delta, 0), rows.count - 1) } ?? (delta > 0 ? 0 : rows.count - 1)
        selection = rows[next]
    }

    /// Opens the inspector on a view for the selected row; Logs only for pods.
    @discardableResult
    func openInspector(_ view: InspectorTab) -> Bool {
        guard selection != nil else { return false }
        if view == .logs, !(kind?.kind == "Pod" && kind?.group.isEmpty == true) { return false }
        inspectorTab = view
        inspectorOpen = true
        return true
    }
}
