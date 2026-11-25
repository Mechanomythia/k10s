import K10sCore
import SwiftUI

@main
struct K10sApp: App {
    @State private var engine = EngineClient()
    @State private var protection = Protection()
    @StateObject private var updater = Updater()

    init() {
        Hy.registerFonts()
    }

    var body: some Scene {
        WindowGroup(for: TabScope.self) { $scope in
            WindowRoot(scope: $scope).environment(engine).environment(protection)
        }
        .windowStyle(.hiddenTitleBar)
        .commands {
            ShellCommands()
            CommandGroup(after: .appInfo) {
                Button("Check for updates…") { updater.checkNow() }
                    .disabled(!updater.canCheck)
            }
        }

        Settings {
            SettingsView().environment(engine).environment(protection)
        }
    }
}
