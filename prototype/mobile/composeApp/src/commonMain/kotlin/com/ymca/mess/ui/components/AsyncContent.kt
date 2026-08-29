package com.ymca.mess.ui.components

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.ymca.mess.network.ApiException

sealed interface UiState<out T> {
    data object Loading : UiState<Nothing>
    data class Failed(val message: String) : UiState<Nothing>
    data class Loaded<T>(val value: T) : UiState<T>
}

/**
 * Loads [loader] on first composition and whenever [refreshKey] changes,
 * showing a spinner while pending and a retry button on failure. Every
 * list/detail screen in this app is built on this instead of hand-rolling
 * the same try/catch + loading flag each time.
 */
@Composable
fun <T> AsyncContent(
    refreshKey: Any? = Unit,
    loader: suspend () -> T,
    content: @Composable (T, refresh: () -> Unit) -> Unit
) {
    var state by remember(refreshKey) { mutableStateOf<UiState<T>>(UiState.Loading) }
    var manualRefreshTick by remember { mutableStateOf(0) }

    LaunchedEffect(refreshKey, manualRefreshTick) {
        state = UiState.Loading
        state = try {
            UiState.Loaded(loader())
        } catch (e: ApiException) {
            UiState.Failed(e.message)
        } catch (e: Exception) {
            UiState.Failed(e.message ?: "something went wrong")
        }
    }

    when (val s = state) {
        is UiState.Loading -> Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        is UiState.Failed -> Column(
            Modifier.fillMaxSize().padding(24.dp),
            verticalArrangement = Arrangement.Center,
            horizontalAlignment = Alignment.CenterHorizontally
        ) {
            Text(s.message, color = MaterialTheme.colorScheme.error)
            Button(onClick = { manualRefreshTick++ }, modifier = Modifier.padding(top = 12.dp)) {
                Text("Retry")
            }
        }
        is UiState.Loaded -> content(s.value) { manualRefreshTick++ }
    }
}
