package com.ymca.mess.ui.screens.admin

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Business
import androidx.compose.material.icons.filled.PersonAdd
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.ymca.mess.model.CreateHostelRequest
import com.ymca.mess.model.CreateSecretaryRequest
import com.ymca.mess.network.ApiClient
import com.ymca.mess.network.ApiException
import com.ymca.mess.ui.components.AsyncContent
import kotlinx.coroutines.launch

private enum class Tab { HOSTELS, SECRETARIES }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AdminHomeScreen(api: ApiClient, onLogout: () -> Unit) {
    var tab by remember { mutableStateOf(Tab.HOSTELS) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Central Admin") },
                actions = { TextButton(onClick = onLogout) { Text("Log out") } }
            )
        },
        bottomBar = {
            NavigationBar {
                NavigationBarItem(tab == Tab.HOSTELS, { tab = Tab.HOSTELS }, { Icon(Icons.Default.Business, null) }, label = { Text("Hostels") })
                NavigationBarItem(tab == Tab.SECRETARIES, { tab = Tab.SECRETARIES }, { Icon(Icons.Default.PersonAdd, null) }, label = { Text("Secretaries") })
            }
        }
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            when (tab) {
                Tab.HOSTELS -> HostelsTab(api)
                Tab.SECRETARIES -> SecretariesTab(api)
            }
        }
    }
}

@Composable
private fun HostelsTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var feeRupees by remember { mutableStateOf("6000") }
    var surchargeRupees by remember { mutableStateOf("50") }
    var deductionRupees by remember { mutableStateOf("150") }
    var threshold by remember { mutableStateOf("7") }
    var creating by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("New hostel", style = MaterialTheme.typography.titleLarge)
        Text(
            "You're setting starting values only — the hostel's own Secretary can tune all of this afterward.",
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(bottom = 12.dp)
        )
        OutlinedTextField(name, { name = it }, label = { Text("Hostel name") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(feeRupees, { feeRupees = it }, label = { Text("Starting flat monthly fee (₹)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(surchargeRupees, { surchargeRupees = it }, label = { Text("Starting non-veg surcharge (₹)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(deductionRupees, { deductionRupees = it }, label = { Text("Starting long-leave deduction (₹/day)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(threshold, { threshold = it }, label = { Text("Starting long-leave threshold (days)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))

        Button(
            onClick = {
                creating = true
                message = null
                scope.launch {
                    try {
                        api.createHostel(
                            CreateHostelRequest(
                                name = name.trim(),
                                flat_monthly_fee_paise = (feeRupees.toLongOrNull() ?: 0) * 100,
                                non_veg_surcharge_paise = (surchargeRupees.toLongOrNull() ?: 0) * 100,
                                daily_deduction_paise = (deductionRupees.toLongOrNull() ?: 0) * 100,
                                long_leave_threshold_days = threshold.toIntOrNull() ?: 7,
                                menu_days = List(7) { "" }
                            )
                        )
                        message = "Created. The hostel's Secretary can fill in the weekly menu."
                        name = ""
                        refreshKey++
                    } catch (e: ApiException) {
                        message = e.message
                    } finally {
                        creating = false
                    }
                }
            },
            enabled = !creating && name.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp)
        ) { Text(if (creating) "Creating..." else "Create hostel") }

        message?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }

        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
        Text("All hostels", style = MaterialTheme.typography.titleMedium)

        AsyncContent(refreshKey = refreshKey, loader = { api.listHostels() }) { hostels, _ ->
            LazyColumn {
                items(hostels) { h ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Text(h.name, modifier = Modifier.padding(12.dp))
                    }
                }
            }
        }
    }
}

@Composable
private fun SecretariesTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var hostelId by remember { mutableStateOf("") }
    var staffId by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var email by remember { mutableStateOf("") }
    var creating by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("New secretary", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(bottom = 12.dp))

        AsyncContent(refreshKey = refreshKey, loader = { api.listHostels() }) { hostels, _ ->
            Column {
                Text("Pick a hostel ID from below, or paste it in:", style = MaterialTheme.typography.bodySmall)
                hostels.forEach { h ->
                    Row(Modifier.fillMaxWidth().padding(vertical = 2.dp)) {
                        TextButton(onClick = { hostelId = h.id }) { Text(h.name) }
                    }
                }
            }
        }

        OutlinedTextField(hostelId, { hostelId = it }, label = { Text("Hostel ID") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(staffId, { staffId = it }, label = { Text("Staff ID (e.g. SEC-002)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(name, { name = it }, label = { Text("Name") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(email, { email = it }, label = { Text("Email") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))

        Button(
            onClick = {
                creating = true
                message = null
                scope.launch {
                    try {
                        api.createSecretary(CreateSecretaryRequest(hostelId.trim(), staffId.trim(), name.trim(), email.trim().ifBlank { null }))
                        message = "Created."
                        staffId = ""; name = ""; email = ""
                    } catch (e: ApiException) {
                        message = e.message
                    } finally {
                        creating = false
                    }
                }
            },
            enabled = !creating && hostelId.isNotBlank() && staffId.isNotBlank() && name.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp)
        ) { Text(if (creating) "Creating..." else "Create secretary") }

        message?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }
    }
}
