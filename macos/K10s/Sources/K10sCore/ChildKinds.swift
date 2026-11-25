import Foundation

/// What a kind's children are shown as when diving into a row (ADR 0006).
public enum ChildKind: Equatable {
    /// A table of objects, found with object.children.
    case table
    /// The pod's containers, from pod.containers.
    case containers
}

extension KindEntry {
    /// The children this kind has, or nil. Matches group and kind, so a CRD
    /// named Deployment in another group has none.
    public var childKind: ChildKind? {
        switch (group, kind) {
        case ("apps", "Deployment"), ("apps", "StatefulSet"), ("apps", "DaemonSet"), ("apps", "ReplicaSet"),
             ("batch", "Job"), ("batch", "CronJob"), ("", "Service"), ("", "Node"):
            return .table
        case ("", "Pod"):
            return .containers
        default:
            return nil
        }
    }
}
