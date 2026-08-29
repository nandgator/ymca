package com.ymca.mess

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import com.ymca.mess.model.Role
import com.ymca.mess.nav.Screen
import com.ymca.mess.network.ApiClient
import com.ymca.mess.network.SessionStore
import com.ymca.mess.ui.screens.LoginScreen
import com.ymca.mess.ui.screens.OtpVerifyScreen
import com.ymca.mess.ui.screens.admin.AdminHomeScreen
import com.ymca.mess.ui.screens.member.MemberHomeScreen
import com.ymca.mess.ui.screens.secretary.SecretaryHomeScreen
import com.ymca.mess.ui.theme.MessTheme

@Composable
fun App() {
    MessTheme {
        val sessionStore = remember { SessionStore() }
        val api = remember { ApiClient(sessionStore) }

        var screen by remember {
            mutableStateOf(
                if (sessionStore.isLoggedIn) homeScreenFor(sessionStore.role) else Screen.Login
            )
        }

        fun logout() {
            api.logout()
            screen = Screen.Login
        }

        when (val current = screen) {
            is Screen.Login -> LoginScreen(
                onOtpRequested = { role, loginId, channel -> screen = Screen.OtpVerify(role, loginId, channel) }
            )

            is Screen.OtpVerify -> OtpVerifyScreen(
                api = api,
                role = current.role,
                loginId = current.loginId,
                channel = current.channel,
                onBack = { screen = Screen.Login },
                onVerified = { role -> screen = homeScreenFor(role) }
            )

            is Screen.MemberHome -> MemberHomeScreen(api = api, onLogout = ::logout)
            is Screen.SecretaryHome -> SecretaryHomeScreen(api = api, onLogout = ::logout)
            is Screen.AdminHome -> AdminHomeScreen(api = api, onLogout = ::logout)
        }
    }
}

private fun homeScreenFor(role: Role?): Screen = when (role) {
    Role.MEMBER -> Screen.MemberHome
    Role.SECRETARY -> Screen.SecretaryHome
    Role.CENTRAL_ADMIN -> Screen.AdminHome
    null -> Screen.Login
}
