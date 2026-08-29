package com.ymca.mess.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.ymca.mess.model.OtpChannel
import com.ymca.mess.model.Role
import com.ymca.mess.network.ApiClient
import com.ymca.mess.network.ApiException
import kotlinx.coroutines.launch
import androidx.compose.runtime.rememberCoroutineScope

@Composable
fun LoginScreen(onOtpRequested: (role: String, loginId: String, channel: String) -> Unit) {
    var role by remember { mutableStateOf(Role.MEMBER) }
    var loginId by remember { mutableStateOf("") }
    var channel by remember { mutableStateOf(OtpChannel.EMAIL) }

    Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center) {
        Text("YMCA Mess", style = MaterialTheme.typography.headlineMedium)
        Text(
            "Log in with the Member ID or Staff ID you were given — no account to create.",
            style = MaterialTheme.typography.bodyMedium,
            modifier = Modifier.padding(top = 8.dp, bottom = 24.dp)
        )

        Text("I am a...", style = MaterialTheme.typography.labelLarge)
        RoleOption("Member", role == Role.MEMBER) { role = Role.MEMBER }
        RoleOption("Hostel Secretary", role == Role.SECRETARY) { role = Role.SECRETARY }
        RoleOption("Central Admin", role == Role.CENTRAL_ADMIN) { role = Role.CENTRAL_ADMIN }

        OutlinedTextField(
            value = loginId,
            onValueChange = { loginId = it },
            label = { Text(if (role == Role.MEMBER) "Member ID" else "Staff ID") },
            modifier = Modifier.fillMaxWidth().padding(top = 16.dp)
        )

        Text("Send code via", style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(top = 16.dp))
        Row {
            RoleOption("Email", channel == OtpChannel.EMAIL) { channel = OtpChannel.EMAIL }
            RoleOption("SMS", channel == OtpChannel.SMS) { channel = OtpChannel.SMS }
        }

        Button(
            onClick = { onOtpRequested(role.name, loginId.trim(), channel.name) },
            enabled = loginId.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 24.dp)
        ) { Text("Send code") }
    }
}

@Composable
private fun RoleOption(label: String, selected: Boolean, onClick: () -> Unit) {
    Row {
        RadioButton(selected = selected, onClick = onClick)
        Text(label, modifier = Modifier.padding(start = 4.dp, top = 12.dp))
    }
}

@Composable
fun OtpVerifyScreen(
    api: ApiClient,
    role: String,
    loginId: String,
    channel: String,
    onBack: () -> Unit,
    onVerified: (Role) -> Unit
) {
    val scope = rememberCoroutineScope()
    var code by remember { mutableStateOf("") }
    var status by remember { mutableStateOf("Sending code...") }
    var error by remember { mutableStateOf<String?>(null) }
    var verifying by remember { mutableStateOf(false) }

    LaunchedEffect(role, loginId, channel) {
        try {
            api.requestOtp(Role.valueOf(role), loginId, OtpChannel.valueOf(channel))
            status = "Code sent via $channel. Enter it below."
        } catch (e: ApiException) {
            error = e.message
        }
    }

    Column(Modifier.fillMaxSize().padding(24.dp), verticalArrangement = Arrangement.Center) {
        Text("Enter code", style = MaterialTheme.typography.headlineMedium)
        Text(status, modifier = Modifier.padding(top = 8.dp, bottom = 24.dp))

        OutlinedTextField(
            value = code,
            onValueChange = { code = it },
            label = { Text("6-digit code") },
            modifier = Modifier.fillMaxWidth()
        )

        error?.let { Text(it, color = MaterialTheme.colorScheme.error, modifier = Modifier.padding(top = 8.dp)) }

        Button(
            onClick = {
                verifying = true
                error = null
                scope.launch {
                    try {
                        val resp = api.verifyOtp(Role.valueOf(role), loginId, code.trim())
                        onVerified(Role.valueOf(resp.role))
                    } catch (e: ApiException) {
                        error = e.message
                    } finally {
                        verifying = false
                    }
                }
            },
            enabled = code.isNotBlank() && !verifying,
            modifier = Modifier.fillMaxWidth().padding(top = 16.dp)
        ) { Text(if (verifying) "Verifying..." else "Verify") }

        TextButton(onClick = onBack, modifier = Modifier.padding(top = 8.dp)) { Text("Back") }
    }
}
