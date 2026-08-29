# Mobile — YMCA Mess Management

Kotlin Multiplatform + Compose Multiplatform: one shared UI codebase
(`composeApp/src/commonMain`) targeting both Android and iOS. Role-based —
the same app serves Member, Secretary, and Central Admin, per
`/CONTEXT.md`.

## Layout

```
composeApp/
  build.gradle.kts          KMP targets (Android + iOS), dependencies
  src/
    commonMain/kotlin/com/ymca/mess/
      model/Dto.kt            wire types — mirrors backend/internal/httpapi/dto.go exactly
      network/ApiClient.kt    Ktor client, one function per backend endpoint
      network/SessionStore.kt bearer-token persistence (multiplatform-settings)
      nav/Screen.kt            top-level navigation state (no nav library needed at this size)
      ui/screens/              LoginScreen, OtpVerifyScreen, member/, secretary/, admin/
      ui/components/           AsyncContent — shared loading/error/content wrapper
      App.kt                   root composable, owns SessionStore + ApiClient + screen state
    androidMain/kotlin/...     MainActivity.kt, PlatformConfig.kt (default API URL)
    iosMain/kotlin/...         MainViewController.kt, PlatformConfig.kt
iosApp/iosApp/                 Swift wrapper (see "Running on iOS" below)
```

## Before you build: point it at your backend

`ApiClient`'s default base URL is `http://10.0.2.2:8080` on Android (the
emulator's alias for your host machine) and `http://localhost:8080` on iOS
simulator — both assume the backend from `../deploy/docker-compose.yml` is
running locally. For a real device or your EC2 deployment, pass an explicit
URL when constructing `ApiClient` in `App.kt`:

```kotlin
val api = remember { ApiClient(sessionStore, baseUrl = "https://mess.yourdomain.com") }
```

## Running on Android

1. Open the `mobile/` folder in Android Studio (Ladybug or newer — needs
   Kotlin 2.1 / AGP 8.7 support).
2. Android Studio will offer to generate the Gradle wrapper JAR/scripts on
   first sync if they're missing — accept it (this repo ships
   `gradle-wrapper.properties` but not the wrapper binary itself, since it
   was generated in a network-sandboxed environment). Alternatively, if you
   have a system Gradle install: `gradle wrapper` from `mobile/`.
3. Run the `composeApp` configuration on an emulator or device.

## Running on iOS

This repo ships the Kotlin side of the iOS integration (`iosMain/`,
`MainViewController.kt`) and the Swift source
(`iosApp/iosApp/iOSApp.swift`, `ContentView.swift`, `Info.plist`) — but
**not** a `.xcodeproj` file. Hand-authoring Xcode's project file outside
Xcode is a common source of silently-broken projects, so instead:

1. In Xcode: **File → New → Project → iOS → App**. Product name `iosApp`,
   interface **SwiftUI**, language **Swift**, organization identifier
   `com.ymca`. Save it as `mobile/iosApp` (replacing the placeholder
   `iosApp/iosApp/` folder this repo ships, or merging into it).
2. Replace the generated `iOSApp.swift`, `ContentView.swift`, and
   `Info.plist` with the three files already in `iosApp/iosApp/`.
3. Add a **Run Script** build phase (Build Phases → + → New Run Script
   Phase), placed before "Compile Sources", running:
   ```
   cd "$SRCROOT/.."
   ./gradlew :composeApp:embedAndSignAppleFrameworkForXcode
   ```
4. In the target's **General → Frameworks, Libraries, and Embedded
   Content**, this script produces `ComposeApp.framework` under
   `composeApp/build/xcode-frameworks/` — add it there, or let the script
   phase above handle embedding (that's what
   `embedAndSignAppleFrameworkForXcode` is for).
5. Build and run on a simulator or device (iOS 15+).

This is the same procedure JetBrains' own Kotlin Multiplatform wizard
generates a project around — steps 1–4 are one-time setup.

## What's shared vs. platform-specific

Shared (commonMain): every screen, every network call, all business-facing
logic, the whole navigation state machine. Platform-specific: just the
`MainActivity`/`MainViewController` entry points and the two-line
`defaultApiBaseUrl()` actuals (`PlatformConfig.kt` on each platform) — that
10.0.2.2-vs-localhost distinction is the one thing that has to differ.
