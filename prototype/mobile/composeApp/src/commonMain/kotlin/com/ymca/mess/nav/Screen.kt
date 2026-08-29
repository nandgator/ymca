package com.ymca.mess.nav

/**
 * The whole app's navigation is one mutable-state sealed class rather than
 * androidx.navigation — with roughly a dozen screens and no deep linking
 * requirement, a full navigation library is more ceremony than this app
 * needs. See App.kt for how screen transitions are driven.
 */
sealed interface Screen {
    data object Login : Screen
    data class OtpVerify(val role: String, val loginId: String, val channel: String) : Screen

    // Member
    data object MemberHome : Screen

    // Secretary
    data object SecretaryHome : Screen

    // Central admin
    data object AdminHome : Screen
}
