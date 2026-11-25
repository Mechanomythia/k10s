import K10sCore
import SwiftUI

@main
struct K10sApp: App {
    @State private var engine = EngineClient()
    @StateObject private var updater = Updater()

    init() {
        Hy.registerFonts()
    }

    var body: some Scene {
        WindowGroup {
            StubWindow().environment(engine)
        }
        .commands {
            CommandGroup(after: .appInfo) {
                Button("Check for updates…") { updater.checkNow() }
                    .disabled(!updater.canCheck)
            }
        }
    }
}
