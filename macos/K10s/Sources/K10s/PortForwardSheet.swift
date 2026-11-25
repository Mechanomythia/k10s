import Darwin
import K10sCore
import SwiftUI

/// Port-forward: two ports and the live kubectl command; opens it in your terminal (ADR 0005, decision 2).
struct PortForwardSheet: View {
    let row: Row
    let kind: KindEntry
    let cells: [String: String]
    @Environment(\.dismiss) private var dismiss
    @State private var local = ""
    @State private var remote = ""
    @State private var localEdited = false
    @State private var error: String?

    private static func port(_ s: String) -> Int? {
        Int(s.trimmingCharacters(in: .whitespaces)).flatMap { (1...65535).contains($0) ? $0 : nil }
    }

    private var targetLabel: String { kind.isPod ? "Container port" : "Service port" }

    private var argv: [String] {
        TerminalCommand.portForwardArgv(
            context: row.context, namespace: row.namespace ?? "", kind: kind.noun, name: row.name,
            local: Self.port(local) ?? 0, remote: Self.port(remote) ?? 0)
    }

    /// True when 127.0.0.1:<port> can be bound.
    private static func isFree(_ port: Int) -> Bool {
        let fd = socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { return true }
        defer { close(fd) }
        var addr = sockaddr_in()
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = in_port_t(UInt16(port)).bigEndian
        addr.sin_addr.s_addr = inet_addr("127.0.0.1")
        let rc = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        return rc == 0
    }

    private func go() {
        guard let l = Self.port(local), Self.port(remote) != nil else { return }
        guard Self.isFree(l) else { error = "Port \(l) is in use on your Mac."; return }
        do {
            try TerminalLauncher.open(argv: argv)
            dismiss()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func field(_ label: String, _ text: Binding<String>) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HyLabel(label)
            TextField("", text: text)
                .textFieldStyle(.plain)
                .font(Hy.mono(13)).foregroundStyle(Hy.starlight)
                .padding(.horizontal, Hy.s3).frame(height: 34)
                .background(Hy.sky)
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
                .onSubmit { go() }
        }
    }

    var body: some View {
        let valid = Self.port(local) != nil && Self.port(remote) != nil
        VStack(alignment: .leading, spacing: Hy.s3) {
            Text("Port-forward \(row.name)").font(Hy.display(22)).foregroundStyle(Hy.starlight)
            Text("\(kind.kind) · \(row.namespace ?? "") · \(row.context)")
                .font(Hy.mono(12)).foregroundStyle(Hy.faint)
            HStack(alignment: .bottom, spacing: Hy.s3) {
                field("Local port", Binding(get: { local }, set: { local = $0; localEdited = true }))
                Text("→").font(Hy.ui(15)).foregroundStyle(Hy.dust).padding(.bottom, 7)
                field(targetLabel, Binding(get: { remote }, set: {
                    remote = $0
                    if !localEdited { local = $0 }
                }))
            }
            Text(argv.joined(separator: " "))
                .font(Hy.mono(12)).foregroundStyle(Hy.dust).textSelection(.enabled)
                .padding(Hy.s3).frame(maxWidth: .infinity, alignment: .leading)
                .background(Hy.sky)
                .overlay(RoundedRectangle(cornerRadius: Hy.r1).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
            if let error {
                Text(error).font(Hy.ui(13)).foregroundStyle(Hy.classM).textSelection(.enabled)
            }
            HStack(spacing: Hy.s2) {
                Spacer()
                Button("Cancel") { dismiss() }
                    .buttonStyle(HyButton(kind: .quiet))
                    .keyboardShortcut(.cancelAction)
                Button("Open in Terminal") { go() }
                    .buttonStyle(HyButton(kind: .primary))
                    .disabled(!valid)
            }
            .padding(.top, Hy.s2)
        }
        .padding(Hy.s5)
        .frame(width: 520)
        .background(Hy.skyRaised)
        .overlay(RoundedRectangle(cornerRadius: Hy.r2).strokeBorder(Hy.neatlineStrong, lineWidth: 1))
        .clipShape(RoundedRectangle(cornerRadius: Hy.r2))
        .onAppear {
            guard kind.isService, remote.isEmpty else { return }
            let col = cells.first { $0.key.contains("PORT") }?.value ?? ""
            let digits = col.split(whereSeparator: { !$0.isNumber }).first.map(String.init) ?? ""
            if let n = Self.port(digits) { remote = String(n); local = String(n) }
        }
    }
}
