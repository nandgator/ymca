import SwiftUI
import ComposeApp

/// Wraps the Kotlin/Compose Multiplatform UI (built by
/// `MainViewController()` in composeApp/src/iosMain) as a SwiftUI view.
/// This is the entire native-iOS-specific UI code in the app — everything
/// else lives once, in Kotlin, shared with Android.
struct ComposeView: UIViewControllerRepresentable {
    func makeUIViewController(context: Context) -> UIViewController {
        MainViewControllerKt.MainViewController()
    }

    func updateUIViewController(_ uiViewController: UIViewController, context: Context) {
        // no-op — state lives inside the Compose tree
    }
}

struct ContentView: View {
    var body: some View {
        ComposeView()
            .ignoresSafeArea(.all)
    }
}
