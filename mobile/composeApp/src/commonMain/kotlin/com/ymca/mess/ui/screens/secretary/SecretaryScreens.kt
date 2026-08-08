package com.ymca.mess.ui.screens.secretary

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ReceiptLong
import androidx.compose.material.icons.filled.Fastfood
import androidx.compose.material.icons.filled.Group
import androidx.compose.material.icons.filled.ReceiptLong
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Today
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.ymca.mess.model.AddMemberRequest
import com.ymca.mess.model.AddOptionalItemRequest
import com.ymca.mess.model.UpdatePolicyRequest
import com.ymca.mess.network.ApiClient
import com.ymca.mess.network.ApiException
import com.ymca.mess.ui.components.AsyncContent
import com.ymca.mess.util.formatShort
import com.ymca.mess.util.todayIso
import com.ymca.mess.util.todayLocalDate
import kotlinx.coroutines.launch

private enum class Tab { ROSTER, POLICY, ITEMS, ENTRIES, BILLING }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SecretaryHomeScreen(api: ApiClient, onLogout: () -> Unit) {
    var tab by remember { mutableStateOf(Tab.ROSTER) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Secretary") },
                actions = { TextButton(onClick = onLogout) { Text("Log out") } }
            )
        },
        bottomBar = {
            NavigationBar {
                NavigationBarItem(tab == Tab.ROSTER, { tab = Tab.ROSTER }, { Icon(Icons.Default.Group, null) }, label = { Text("Roster") })
                NavigationBarItem(tab == Tab.POLICY, { tab = Tab.POLICY }, { Icon(Icons.Default.Settings, null) }, label = { Text("Policy") })
                NavigationBarItem(tab == Tab.ITEMS, { tab = Tab.ITEMS }, { Icon(Icons.Default.Fastfood, null) }, label = { Text("Items") })
                NavigationBarItem(tab == Tab.ENTRIES, { tab = Tab.ENTRIES }, { Icon(Icons.Default.Today, null) }, label = { Text("Entries") })
                NavigationBarItem(tab == Tab.BILLING, { tab = Tab.BILLING }, { Icon(Icons.AutoMirrored.Filled.ReceiptLong, null) }, label = { Text("Billing") })
            }
        }
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            when (tab) {
                Tab.ROSTER -> RosterTab(api)
                Tab.POLICY -> PolicyTab(api)
                Tab.ITEMS -> OptionalItemsTab(api)
                Tab.ENTRIES -> DailyEntriesTab(api)
                Tab.BILLING -> BillingTab(api)
            }
        }
    }
}

@Composable
private fun RosterTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var memberId by remember { mutableStateOf("") }
    var name by remember { mutableStateOf("") }
    var email by remember { mutableStateOf("") }
    var adding by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("Add member", style = MaterialTheme.typography.titleLarge)
        Text(
            "Members never self-register — this is how anyone gets into the system.",
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(bottom = 12.dp)
        )
        OutlinedTextField(memberId, { memberId = it }, label = { Text("Member ID (e.g. YMCA-2026-0002)") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(name, { name = it }, label = { Text("Name") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
        OutlinedTextField(email, { email = it }, label = { Text("Email (or leave blank if using mobile)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))

        Button(
            onClick = {
                adding = true
                message = null
                scope.launch {
                    try {
                        api.addMember(AddMemberRequest(memberId.trim(), name.trim(), email.trim().ifBlank { null }))
                        message = "Added."
                        memberId = ""; name = ""; email = ""
                        refreshKey++
                    } catch (e: ApiException) {
                        message = e.message
                    } finally {
                        adding = false
                    }
                }
            },
            enabled = !adding && memberId.isNotBlank() && name.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp)
        ) { Text(if (adding) "Adding..." else "Add member") }

        message?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }

        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
        Text("Roster", style = MaterialTheme.typography.titleMedium)

        AsyncContent(refreshKey = refreshKey, loader = { api.listRoster() }) { roster, _ ->
            LazyColumn {
                items(roster) { m ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Column(Modifier.padding(12.dp)) {
                            Text(m.name, style = MaterialTheme.typography.titleSmall)
                            Text(m.member_id, style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun PolicyTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var refreshKey by remember { mutableStateOf(0) }
    var message by remember { mutableStateOf<String?>(null) }

    AsyncContent(refreshKey = refreshKey, loader = { api.getPolicy() }) { policy, _ ->
        var feeRupees by remember(policy) { mutableStateOf((policy.flat_monthly_fee_paise / 100).toString()) }
        var surchargeRupees by remember(policy) { mutableStateOf((policy.non_veg_surcharge_paise / 100).toString()) }
        var deductionRupees by remember(policy) { mutableStateOf((policy.daily_deduction_paise / 100).toString()) }
        var threshold by remember(policy) { mutableStateOf(policy.long_leave_threshold_days.toString()) }
        var menuDays by remember(policy) {
            mutableStateOf(if (policy.menu_days.size == 7) policy.menu_days else List(7) { "" })
        }
        var saving by remember { mutableStateOf(false) }

        Column(Modifier.fillMaxSize().padding(16.dp)) {
            Text("Hostel policy", style = MaterialTheme.typography.titleLarge)
            Text(
                "Every rule here is specific to your hostel — nothing is shared across hostels.",
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(bottom = 12.dp)
            )

            OutlinedTextField(feeRupees, { feeRupees = it }, label = { Text("Flat monthly fee (₹)") }, modifier = Modifier.fillMaxWidth())
            OutlinedTextField(surchargeRupees, { surchargeRupees = it }, label = { Text("Non-veg dinner surcharge (₹/day)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
            OutlinedTextField(deductionRupees, { deductionRupees = it }, label = { Text("Long-leave deduction (₹/day)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))
            OutlinedTextField(threshold, { threshold = it }, label = { Text("Long-leave threshold (days)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))

            Text("Weekly menu (breakfast)", style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(top = 16.dp, bottom = 4.dp))
            val dayNames = listOf("Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday")
            dayNames.forEachIndexed { i, dayName ->
                OutlinedTextField(
                    value = menuDays.getOrElse(i) { "" },
                    onValueChange = { v -> menuDays = menuDays.toMutableList().also { it[i] = v } },
                    label = { Text(dayName) },
                    modifier = Modifier.fillMaxWidth().padding(top = 4.dp)
                )
            }

            Button(
                onClick = {
                    saving = true
                    message = null
                    scope.launch {
                        try {
                            api.updatePolicy(
                                UpdatePolicyRequest(
                                    flat_monthly_fee_paise = (feeRupees.toLongOrNull() ?: 0) * 100,
                                    non_veg_surcharge_paise = (surchargeRupees.toLongOrNull() ?: 0) * 100,
                                    daily_deduction_paise = (deductionRupees.toLongOrNull() ?: 0) * 100,
                                    long_leave_threshold_days = threshold.toIntOrNull() ?: 7,
                                    menu_days = menuDays
                                )
                            )
                            message = "Saved."
                            refreshKey++
                        } catch (e: ApiException) {
                            message = e.message
                        } finally {
                            saving = false
                        }
                    }
                },
                enabled = !saving,
                modifier = Modifier.fillMaxWidth().padding(top = 16.dp)
            ) { Text(if (saving) "Saving..." else "Save policy") }

            message?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }
        }
    }
}

@Composable
private fun OptionalItemsTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var name by remember { mutableStateOf("") }
    var priceRupees by remember { mutableStateOf("") }
    var adding by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("Breakfast add-ons", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(bottom = 12.dp))

        Row {
            OutlinedTextField(name, { name = it }, label = { Text("Item name") }, modifier = Modifier.weight(1f))
            OutlinedTextField(priceRupees, { priceRupees = it }, label = { Text("₹") }, modifier = Modifier.weight(0.5f).padding(start = 8.dp))
        }
        Button(
            onClick = {
                adding = true
                message = null
                scope.launch {
                    try {
                        api.addOptionalItem(AddOptionalItemRequest(name.trim(), (priceRupees.toLongOrNull() ?: 0) * 100))
                        name = ""; priceRupees = ""
                        refreshKey++
                    } catch (e: ApiException) {
                        message = e.message
                    } finally {
                        adding = false
                    }
                }
            },
            enabled = !adding && name.isNotBlank(),
            modifier = Modifier.fillMaxWidth().padding(top = 8.dp)
        ) { Text(if (adding) "Adding..." else "Add item") }

        message?.let { Text(it, modifier = Modifier.padding(top = 8.dp)) }

        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))

        AsyncContent(refreshKey = refreshKey, loader = { api.getPolicy() }) { policy, refresh ->
            LazyColumn {
                items(policy.optional_items) { item ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Row(
                            Modifier.fillMaxWidth().padding(12.dp),
                            horizontalArrangement = Arrangement.SpaceBetween
                        ) {
                            Column {
                                Text(item.name, style = MaterialTheme.typography.titleSmall)
                                Text(item.price_inr, style = MaterialTheme.typography.bodySmall)
                            }
                            IconButton(onClick = {
                                scope.launch {
                                    try {
                                        api.deactivateOptionalItem(item.id)
                                        refresh()
                                    } catch (e: ApiException) {
                                        message = e.message
                                    }
                                }
                            }) { Icon(Icons.Default.Delete, contentDescription = "Remove") }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun DailyEntriesTab(api: ApiClient) {
    var date by remember { mutableStateOf(todayIso()) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("Daily roll", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(bottom = 8.dp))
        OutlinedTextField(date, { date = it }, label = { Text("Date (YYYY-MM-DD)") }, modifier = Modifier.fillMaxWidth())
        Button(onClick = { refreshKey++ }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) { Text("Load") }

        AsyncContent(refreshKey = refreshKey, loader = { api.hostelEntriesForDate(date) }) { entries, _ ->
            val breakfastCount = entries.count { it.meal_type == "BREAKFAST" }
            val dinnerVeg = entries.count { it.meal_type == "DINNER" && !it.non_veg }
            val dinnerNonVeg = entries.count { it.meal_type == "DINNER" && it.non_veg }

            Column(Modifier.padding(top = 16.dp)) {
                Text("Breakfast: $breakfastCount", style = MaterialTheme.typography.bodyLarge)
                Text("Dinner (veg): $dinnerVeg", style = MaterialTheme.typography.bodyLarge)
                Text("Dinner (non-veg): $dinnerNonVeg", style = MaterialTheme.typography.bodyLarge)
            }
        }
    }
}

@Composable
private fun BillingTab(api: ApiClient) {
    val today = remember { todayLocalDate() }
    var year by remember { mutableStateOf(today.year.toString()) }
    var month by remember { mutableStateOf(today.monthNumber.toString()) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("Month-end billing", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(bottom = 8.dp))
        Text(
            "Computes every member's bill for the month — replaces the manual notebook tally. Settlement is still offline.",
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(bottom = 12.dp)
        )
        Row {
            OutlinedTextField(year, { year = it }, label = { Text("Year") }, modifier = Modifier.weight(1f))
            OutlinedTextField(month, { month = it }, label = { Text("Month") }, modifier = Modifier.weight(1f).padding(start = 8.dp))
        }
        Button(onClick = { refreshKey++ }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp)) { Text("Run billing") }

        AsyncContent(
            refreshKey = refreshKey,
            loader = { api.runBilling(year.toIntOrNull() ?: today.year, month.toIntOrNull() ?: today.monthNumber) }
        ) { bills, _ ->
            LazyColumn(contentPadding = PaddingValues(top = 16.dp)) {
                items(bills) { bill ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Row(Modifier.fillMaxWidth().padding(12.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                            Text(bill.member_id.take(8) + "...", style = MaterialTheme.typography.bodySmall)
                            Text(bill.total_inr, style = MaterialTheme.typography.titleSmall)
                        }
                    }
                }
            }
        }
    }
}
