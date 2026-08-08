package com.ymca.mess.ui.screens.member

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
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.History
import androidx.compose.material.icons.filled.Receipt
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.Checkbox
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
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
import com.ymca.mess.model.EntryDto
import com.ymca.mess.model.MealType
import com.ymca.mess.model.RegisterLeaveRequest
import com.ymca.mess.model.SubmitEntryRequest
import com.ymca.mess.network.ApiClient
import com.ymca.mess.network.ApiException
import com.ymca.mess.ui.components.AsyncContent
import com.ymca.mess.util.formatShort
import com.ymca.mess.util.todayIso
import com.ymca.mess.util.todayLocalDate
import kotlinx.coroutines.launch

private enum class Tab { ENTRY, LEAVE, HISTORY, BILL }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun MemberHomeScreen(api: ApiClient, onLogout: () -> Unit) {
    var tab by remember { mutableStateOf(Tab.ENTRY) }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("YMCA Mess") },
                actions = { TextButton(onClick = onLogout) { Text("Log out") } }
            )
        },
        bottomBar = {
            NavigationBar {
                NavigationBarItem(tab == Tab.ENTRY, { tab = Tab.ENTRY }, { Icon(Icons.Default.CheckCircle, null) }, label = { Text("Today") })
                NavigationBarItem(tab == Tab.LEAVE, { tab = Tab.LEAVE }, { Icon(Icons.Default.CalendarMonth, null) }, label = { Text("Leave") })
                NavigationBarItem(tab == Tab.HISTORY, { tab = Tab.HISTORY }, { Icon(Icons.Default.History, null) }, label = { Text("History") })
                NavigationBarItem(tab == Tab.BILL, { tab = Tab.BILL }, { Icon(Icons.Default.Receipt, null) }, label = { Text("Bill") })
            }
        }
    ) { padding ->
        Column(Modifier.fillMaxSize().padding(padding)) {
            when (tab) {
                Tab.ENTRY -> EntryTab(api)
                Tab.LEAVE -> LeaveTab(api)
                Tab.HISTORY -> HistoryTab(api)
                Tab.BILL -> BillTab(api)
            }
        }
    }
}

@Composable
private fun EntryTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var meal by remember { mutableStateOf(MealType.BREAKFAST) }
    var checkedItems by remember { mutableStateOf(setOf<String>()) }
    var nonVeg by remember { mutableStateOf(false) }
    var submitting by remember { mutableStateOf(false) }
    var result by remember { mutableStateOf<String?>(null) }
    var submitTick by remember { mutableStateOf(0) }

    AsyncContent(refreshKey = Unit, loader = { api.myPolicy() }) { policy, _ ->
        val todayMenuItem = remember(policy) {
            val dayIndex = (todayLocalDate().dayOfWeek.ordinal) % 7 // 0 = Monday, matches menu_days[0]
            policy.menu_days.getOrNull(dayIndex).orEmpty()
        }

        Column(Modifier.fillMaxSize().padding(16.dp)) {
            Text("What did you have today?", style = MaterialTheme.typography.titleLarge)
            Text(todayIso(), style = MaterialTheme.typography.bodySmall, modifier = Modifier.padding(bottom = 16.dp))

            Row {
                RadioRow("Breakfast", meal == MealType.BREAKFAST) { meal = MealType.BREAKFAST }
                RadioRow("Dinner", meal == MealType.DINNER) { meal = MealType.DINNER }
            }

            if (meal == MealType.BREAKFAST) {
                if (todayMenuItem.isNotEmpty()) {
                    Text("Today's menu: $todayMenuItem", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(top = 12.dp, bottom = 4.dp))
                }
                Text("Optional add-ons", style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(top = 12.dp))
                policy.optional_items.forEach { item ->
                    Row {
                        Checkbox(
                            checked = checkedItems.contains(item.id),
                            onCheckedChange = { checked ->
                                checkedItems = if (checked) checkedItems + item.id else checkedItems - item.id
                            }
                        )
                        Text("${item.name} (${item.price_inr})", modifier = Modifier.padding(top = 12.dp))
                    }
                }
            } else {
                Text("Dinner type", style = MaterialTheme.typography.labelLarge, modifier = Modifier.padding(top = 12.dp))
                RadioRow("Veg", !nonVeg) { nonVeg = false }
                RadioRow("Non-veg", nonVeg) { nonVeg = true }
            }

            Button(
                onClick = {
                    submitting = true
                    result = null
                    scope.launch {
                        try {
                            api.submitEntry(
                                SubmitEntryRequest(
                                    date = todayIso(),
                                    meal_type = meal.name,
                                    optional_item_ids = if (meal == MealType.BREAKFAST) checkedItems.toList() else emptyList(),
                                    non_veg = meal == MealType.DINNER && nonVeg
                                )
                            )
                            result = "Saved. You can edit this again any time before midnight."
                            submitTick++
                        } catch (e: ApiException) {
                            result = e.message
                        } finally {
                            submitting = false
                        }
                    }
                },
                enabled = !submitting,
                modifier = Modifier.fillMaxWidth().padding(top = 20.dp)
            ) { Text(if (submitting) "Saving..." else "Save") }

            result?.let { Text(it, modifier = Modifier.padding(top = 12.dp)) }
        }
    }
}

@Composable
private fun RadioRow(label: String, selected: Boolean, onClick: () -> Unit) {
    Row {
        RadioButton(selected = selected, onClick = onClick)
        Text(label, modifier = Modifier.padding(start = 4.dp, top = 12.dp, end = 16.dp))
    }
}

@Composable
private fun LeaveTab(api: ApiClient) {
    val scope = rememberCoroutineScope()
    var startDate by remember { mutableStateOf(todayIso()) }
    var endDate by remember { mutableStateOf(todayIso()) }
    var submitting by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }
    var refreshKey by remember { mutableStateOf(0) }

    Column(Modifier.fillMaxSize().padding(16.dp)) {
        Text("Register leave", style = MaterialTheme.typography.titleLarge, modifier = Modifier.padding(bottom = 4.dp))
        Text(
            "No approval needed — registering is enough. Short leaves are still billed as present; long leaves (per your hostel's threshold) are excused from billing.",
            style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.padding(bottom = 16.dp)
        )

        OutlinedTextField(startDate, { startDate = it }, label = { Text("Start date (YYYY-MM-DD)") }, modifier = Modifier.fillMaxWidth())
        OutlinedTextField(endDate, { endDate = it }, label = { Text("End date (YYYY-MM-DD)") }, modifier = Modifier.fillMaxWidth().padding(top = 8.dp))

        Button(
            onClick = {
                submitting = true
                message = null
                scope.launch {
                    try {
                        api.registerLeave(RegisterLeaveRequest(startDate, endDate))
                        message = "Leave registered."
                        refreshKey++
                    } catch (e: ApiException) {
                        message = e.message
                    } finally {
                        submitting = false
                    }
                }
            },
            enabled = !submitting,
            modifier = Modifier.fillMaxWidth().padding(top = 12.dp)
        ) { Text(if (submitting) "Saving..." else "Register leave") }

        message?.let { Text(it, modifier = Modifier.padding(top = 8.dp, bottom = 12.dp)) }

        HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
        Text("My leaves", style = MaterialTheme.typography.titleMedium)

        AsyncContent(refreshKey = refreshKey, loader = { api.listMyLeaves() }) { leaves, _ ->
            if (leaves.isEmpty()) {
                Text("No leave on file.", modifier = Modifier.padding(top = 8.dp))
            } else {
                LazyColumn {
                    items(leaves) { leave ->
                        Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                            Column(Modifier.padding(12.dp)) {
                                Text("${formatShort(leave.start_date)} - ${formatShort(leave.end_date)}")
                                Text(leave.type, style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun HistoryTab(api: ApiClient) {
    val today = remember { todayLocalDate() }
    AsyncContent(loader = { api.listMyEntries(today.year, today.monthNumber) }) { entries: List<EntryDto>, _ ->
        if (entries.isEmpty()) {
            Column(Modifier.fillMaxSize().padding(16.dp)) { Text("No entries yet this month.") }
        } else {
            LazyColumn(contentPadding = PaddingValues(16.dp)) {
                items(entries) { e ->
                    Card(modifier = Modifier.fillMaxWidth().padding(vertical = 4.dp)) {
                        Column(Modifier.padding(12.dp)) {
                            Text("${formatShort(e.date)} — ${e.meal_type}", style = MaterialTheme.typography.titleSmall)
                            if (e.meal_type == "DINNER") {
                                Text(if (e.non_veg) "Non-veg" else "Veg", style = MaterialTheme.typography.bodySmall)
                            } else if (e.optional_item_ids.isNotEmpty()) {
                                Text("${e.optional_item_ids.size} add-on(s)", style = MaterialTheme.typography.bodySmall)
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun BillTab(api: ApiClient) {
    val today = remember { todayLocalDate() }
    AsyncContent(loader = { api.myBill(today.year, today.monthNumber) }) { bill, _ ->
        Column(Modifier.fillMaxSize().padding(16.dp)) {
            Text("Bill for ${today.monthNumber}/${today.year}", style = MaterialTheme.typography.titleLarge)
            Text(
                "Settlement is offline (cash/bank transfer) — this is just the computed total.",
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(bottom = 16.dp)
            )
            bill.lines.forEach { line ->
                Row(Modifier.fillMaxWidth().padding(vertical = 4.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                    Text(line.label)
                    Text(line.amount_inr)
                }
            }
            HorizontalDivider(modifier = Modifier.padding(vertical = 12.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text("Total", style = MaterialTheme.typography.titleMedium)
                Text(bill.total_inr, style = MaterialTheme.typography.titleMedium)
            }
        }
    }
}
