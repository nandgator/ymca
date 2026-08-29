import SwiftUI

@main
struct iOSApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
                .ignoresSafeArea(.keyboard) // Compose handles its own keyboard insets
        }
    }
}
