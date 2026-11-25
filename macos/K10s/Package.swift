// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "K10s",
    platforms: [.macOS(.v14)],
    dependencies: [
        // Auto-update (EdDSA-signed appcast on the GitHub release).
        .package(url: "https://github.com/sparkle-project/Sparkle", .upToNextMajor(from: "2.6.0")),
    ],
    targets: [
        // The engine protocol and client; no UI and no Sparkle, so tests stay small.
        .target(name: "K10sCore", path: "Sources/K10sCore"),
        .executableTarget(
            name: "K10s",
            dependencies: [
                "K10sCore",
                .product(name: "Sparkle", package: "Sparkle"),
            ],
            path: "Sources/K10s",
            linkerSettings: [
                // Sparkle.framework ships in K10s.app/Contents/Frameworks.
                .unsafeFlags(["-Xlinker", "-rpath", "-Xlinker", "@executable_path/../Frameworks"]),
            ]
        ),
        .testTarget(name: "K10sTests", dependencies: ["K10sCore"], path: "Tests/K10sTests"),
    ]
)
