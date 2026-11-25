import Combine
import Foundation
import Sparkle

/// Sparkle auto-update. Checks the signed appcast on the latest GitHub release
/// (SUFeedURL) every 6 hours, downloads and installs automatically, and
/// verifies each archive's EdDSA signature against SUPublicEDKey on top of
/// Apple's notarization. Builds without the key leave the updater off.
@MainActor
final class Updater: ObservableObject {
    @Published private(set) var canCheck = false
    @Published private(set) var automatic = true
    private var controller: SPUStandardUpdaterController?
    private var cancellables = Set<AnyCancellable>()

    var enabled: Bool { controller != nil }

    init() {
        guard let key = Bundle.main.object(forInfoDictionaryKey: "SUPublicEDKey") as? String, !key.isEmpty else {
            return // dev / unsigned build: no updates
        }
        let c = SPUStandardUpdaterController(startingUpdater: true, updaterDelegate: nil, userDriverDelegate: nil)
        controller = c
        automatic = c.updater.automaticallyDownloadsUpdates
        c.updater.publisher(for: \.canCheckForUpdates)
            .receive(on: DispatchQueue.main)
            .sink { [weak self] v in self?.canCheck = v }
            .store(in: &cancellables)
    }

    func checkNow() {
        controller?.checkForUpdates(nil)
    }

    func setAutomatic(_ on: Bool) {
        guard let u = controller?.updater else { return }
        u.automaticallyChecksForUpdates = true
        u.automaticallyDownloadsUpdates = on
        automatic = on
    }
}
