import Foundation
import Observation

/// Which contexts are protected: name words plus a per-context override, kept in UserDefaults.
@Observable
public final class Protection {
    public enum Override: String, Codable { case protected, notProtected }

    public enum Reason: Equatable {
        case word(String), override(Override), none

        /// Text for the Settings picker.
        public var label: String {
            switch self {
            case .word(let w): return "Automatic · \(w)"
            case .override(.protected): return "Protected"
            case .override(.notProtected): return "Not protected"
            case .none: return "Automatic"
            }
        }
    }

    public static let defaultWords = ["prod", "live"]

    public private(set) var words: [String]
    public private(set) var overrides: [String: Override]

    @ObservationIgnored private let defaults: UserDefaults
    @ObservationIgnored private let wordsKey = "protection.words"
    @ObservationIgnored private let overridesKey = "protection.overrides"

    public init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        words = defaults.data(forKey: wordsKey)
            .flatMap { try? JSONDecoder().decode([String].self, from: $0) } ?? Self.defaultWords
        overrides = defaults.data(forKey: overridesKey)
            .flatMap { try? JSONDecoder().decode([String: Override].self, from: $0) } ?? [:]
    }

    public func isProtected(_ context: String) -> Bool {
        switch reason(context) {
        case .override(let o): return o == .protected
        case .word: return true
        case .none: return false
        }
    }

    public func reason(_ context: String) -> Reason {
        if let o = overrides[context] { return .override(o) }
        let name = context.lowercased()
        for word in words {
            let w = word.trimmingCharacters(in: .whitespaces)
            if !w.isEmpty, name.contains(w.lowercased()) { return .word(w) }
        }
        return .none
    }

    /// Comma-separated words; spaces trimmed, empties dropped.
    public func setWords(_ text: String) {
        words = text.split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
        defaults.set(try? JSONEncoder().encode(words), forKey: wordsKey)
    }

    /// nil means automatic. Overrides for contexts missing from the kubeconfig are kept.
    public func setOverride(_ context: String, _ value: Override?) {
        overrides[context] = value
        defaults.set(try? JSONEncoder().encode(overrides), forKey: overridesKey)
    }
}

/// The typed-name rule for write confirmations.
public enum ConfirmRule {
    public static func needsTypedName(protected: Bool) -> Bool { protected }

    public static func matches(typed: String, name: String) -> Bool {
        typed.trimmingCharacters(in: .whitespacesAndNewlines) == name
    }
}
