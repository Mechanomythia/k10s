import K10sCore
import SwiftUI

private struct LogLine: Identifiable {
    let id: Int
    let text: String
}

/// Following logs of the selected pod (T5).
struct LogsView: View {
    let row: Row
    @Environment(EngineClient.self) private var engine

    @State private var names: [String] = []
    /// "" = all containers.
    @State private var container = ""
    @State private var containersLoaded = false
    @State private var follow = true
    @State private var previous = false
    @State private var timestamps = false
    @State private var wrap = true
    @State private var find = ""
    @State private var lines: [LogLine] = []
    @State private var nextID = 0
    @State private var status = "Following. Last 500 lines, then new lines as they arrive."
    @State private var atBottom = true
    @FocusState private var findFocused: Bool

    private struct Key: Hashable {
        var uid: String, container: String, previous: Bool, timestamps: Bool, follow: Bool, loaded: Bool
    }
    private var key: Key {
        Key(uid: row.uid, container: container, previous: previous, timestamps: timestamps, follow: follow, loaded: containersLoaded)
    }

    var body: some View {
        VStack(spacing: 0) {
            bar
            Neatline()
            ScrollViewReader { proxy in
                ScrollView(wrap ? .vertical : [.vertical, .horizontal]) {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        ForEach(lines) { l in
                            Text(styled(l.text))
                                .font(Hy.mono(12))
                                .fixedSize(horizontal: !wrap, vertical: true)
                                .frame(maxWidth: wrap ? .infinity : nil, alignment: .leading)
                                .textSelection(.enabled)
                                .onAppear { if l.id == lines.last?.id { atBottom = true } }
                                .onDisappear { if l.id == lines.last?.id { atBottom = false } }
                        }
                    }
                    .padding(Hy.s3)
                }
                .onChange(of: lines.count) {
                    if follow, atBottom, let last = lines.last { proxy.scrollTo(last.id, anchor: .bottom) }
                }
            }
            Neatline()
            footer
        }
        .task(id: row.uid) { await loadContainers() }
        .task(id: key) { await stream() }
    }

    private var bar: some View {
        HStack(spacing: Hy.s3) {
            if names.count > 1 {
                Picker("", selection: $container) {
                    Text("All containers").tag("")
                    ForEach(names, id: \.self) { Text($0).tag($0) }
                }
                .labelsHidden().fixedSize()
            }
            Toggle("Follow", isOn: $follow)
            Toggle("Previous run", isOn: $previous)
            Toggle("Timestamps", isOn: $timestamps)
            Toggle("Wrap", isOn: $wrap)
            Spacer(minLength: 0)
            TextField("Find", text: $find)
                .textFieldStyle(.plain).font(Hy.mono(12)).foregroundStyle(Hy.starlight)
                .focused($findFocused)
                .padding(.horizontal, Hy.s2).frame(width: 120, height: 24)
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
            Button("") { findFocused = true }.keyboardShortcut("f").opacity(0).frame(width: 0, height: 0)
        }
        .toggleStyle(.checkbox)
        .font(Hy.ui(12.5))
        .foregroundStyle(Hy.dust)
        .padding(.horizontal, Hy.s3).padding(.vertical, Hy.s2)
    }

    private var footer: some View {
        HStack(spacing: Hy.s2) {
            if status.hasPrefix("Following") { StarGlyph(state: .building, size: 9) }
            Text(status).font(Hy.mono(11.5)).foregroundStyle(Hy.faint).lineLimit(1)
            Spacer()
            Text("\(lines.count) lines").font(Hy.mono(11.5)).foregroundStyle(Hy.faint)
        }
        .padding(.horizontal, Hy.s3).padding(.vertical, Hy.s2)
    }

    // MARK: styling

    private func styled(_ s: String) -> AttributedString {
        var a = AttributedString(s)
        let lower = s.lowercased()
        let color: Color = ["error", "fatal", "panic"].contains(where: { lower.contains($0) }) ? Hy.classM
            : lower.contains("warn") ? Hy.classK : Hy.starlight
        a.foregroundColor = color
        if timestamps, let sp = s.firstIndex(of: " "),
           let lo = AttributedString.Index(s.startIndex, within: a), let hi = AttributedString.Index(sp, within: a) {
            a[lo..<hi].foregroundColor = Hy.faint
        }
        if !find.isEmpty {
            var from = s.startIndex
            while from < s.endIndex, let r = s.range(of: find, options: .caseInsensitive, range: from..<s.endIndex) {
                if let lo = AttributedString.Index(r.lowerBound, within: a), let hi = AttributedString.Index(r.upperBound, within: a) {
                    a[lo..<hi].backgroundColor = Hy.classG.opacity(0.35)
                }
                from = r.upperBound
            }
        }
        return a
    }

    // MARK: engine

    private func loadContainers() async {
        containersLoaded = false
        names = []
        let p = InspectorContainersParams(context: row.context, namespace: row.namespace ?? "", name: row.name)
        if let r: PodContainersResult = try? await engine.request(Method.podContainers, p) {
            names = r.containers + r.initContainers
            container = r.containers.first ?? names.first ?? ""
        } else {
            container = ""
        }
        containersLoaded = true
    }

    private func stream() async {
        guard containersLoaded else { return }
        lines = []
        status = follow ? "Following. Last 500 lines, then new lines as they arrive." : "Not following."
        let all = container.isEmpty && names.count > 1
        let params = InspectorLogsParams(
            context: row.context, namespace: row.namespace ?? "", pod: row.name,
            container: container.isEmpty ? nil : container, allContainers: all ? true : nil,
            previous: previous, tailLines: 500, follow: follow, timestamps: timestamps)
        do {
            for try await event in try await engine.subscribe(Method.logsSubscribe, params: params) {
                switch event.name {
                case EventName.lines:
                    guard let l = try? event.decode(Lines.self) else { continue }
                    for t in l.lines { lines.append(LogLine(id: nextID, text: t)); nextID += 1 }
                    if lines.count > 20_000 { lines.removeFirst(lines.count - 20_000) }
                case EventName.end:
                    let e = try? event.decode(End.self)
                    status = e?.error ?? "Stream ended."
                default:
                    break
                }
            }
            if !Task.isCancelled, status.hasPrefix("Following") { status = "Stream ended." }
        } catch {
            if !Task.isCancelled { status = error.localizedDescription }
        }
    }
}

/// PodContainersParams has no public initialiser in K10sCore, so the request body is built here.
struct InspectorContainersParams: Encodable {
    let context: String
    let namespace: String
    let name: String
}
