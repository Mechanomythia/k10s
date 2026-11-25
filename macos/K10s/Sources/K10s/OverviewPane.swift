import K10sCore
import SwiftUI

/// Centre slot shown instead of ListPane while tab.showingOverview.
struct OverviewPane: View {
    var tab: TabModel

    var body: some View {
        Group {
            if tab.scope.contexts.count > 1 {
                OverviewFleet(tab: tab)
            } else {
                OverviewPulse(tab: tab)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Hy.sky)
    }
}
