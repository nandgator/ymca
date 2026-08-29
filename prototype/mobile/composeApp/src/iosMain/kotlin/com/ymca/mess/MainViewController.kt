package com.ymca.mess

import androidx.compose.ui.window.ComposeUIViewController
import platform.UIKit.UIViewController

/**
 * Called from Swift (iosApp/ContentView.swift) via
 * `ComposeApp.MainViewControllerKt.MainViewController()` — this is the
 * entire bridge between the Kotlin/Compose UI and the native iOS app shell.
 */
fun MainViewController(): UIViewController = ComposeUIViewController { App() }
