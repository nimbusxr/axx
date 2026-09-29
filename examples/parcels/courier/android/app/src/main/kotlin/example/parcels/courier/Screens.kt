// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.ListItem
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp

@Composable
fun CourierApp(model: CourierViewModel) {
    MaterialTheme {
        Surface(Modifier.fillMaxSize()) {
            Box(Modifier.safeDrawingPadding()) {
                when (val screen = model.screen) {
                    Screen.SignIn -> SignInScreen(
                        busy = model.signingIn,
                        error = model.signInError,
                        onSignIn = model::signIn,
                    )

                    Screen.Deliveries -> {
                        AskForNotifications(model)
                        DeliveriesScreen(
                        name = model.session?.name.orEmpty(),
                        deliveries = model.deliveries,
                        refreshing = model.refreshing,
                        error = model.deliveriesError,
                        onRefresh = model::refresh,
                        onOpen = model::openDelivery,
                        onSignOut = model::signOut,
                    )
                    }

                    is Screen.Delivery -> {
                        BackHandler(onBack = model::backToDeliveries)
                        DeliveryScreen(
                            reference = screen.reference,
                            delivery = model.deliveries?.find { it.reference == screen.reference },
                            loading = model.deliveries == null || model.refreshing,
                            posting = model.posting,
                            delivered = model.delivered,
                            error = model.deliveryError,
                            onConfirm = { signedBy -> model.markDelivered(screen.reference, signedBy) },
                            onBack = model::backToDeliveries,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun Heading(text: String) {
    Text(
        text,
        style = MaterialTheme.typography.headlineMedium,
        modifier = Modifier.semantics { heading() },
    )
}

@Composable
private fun ErrorText(text: String) {
    Text(
        text,
        color = MaterialTheme.colorScheme.error,
        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
    )
}

@Composable
fun SignInScreen(busy: Boolean, error: String?, onSignIn: (courier: String, pin: String) -> Unit) {
    var courier by rememberSaveable { mutableStateOf("") }
    var pin by rememberSaveable { mutableStateOf("") }
    Column(
        Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(16.dp),
    ) {
        Heading("Sign in")
        OutlinedTextField(
            value = courier,
            onValueChange = { courier = it },
            label = { Text("Courier ID") },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        OutlinedTextField(
            value = pin,
            onValueChange = { pin = it },
            label = { Text("PIN") },
            singleLine = true,
            visualTransformation = PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword),
            modifier = Modifier.fillMaxWidth(),
        )
        if (error != null) ErrorText(error)
        Button(
            onClick = { onSignIn(courier.trim(), pin) },
            enabled = courier.isNotBlank() && pin.isNotEmpty() && !busy,
            modifier = Modifier.fillMaxWidth(),
        ) {
            Text("Sign in")
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DeliveriesScreen(
    name: String,
    deliveries: List<Delivery>?,
    refreshing: Boolean,
    error: String?,
    onRefresh: () -> Unit,
    onOpen: (reference: String) -> Unit,
    onSignOut: () -> Unit,
) {
    Column(Modifier.fillMaxSize()) {
        Column(
            Modifier.padding(start = 24.dp, end = 12.dp, top = 24.dp, bottom = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Heading("Today's deliveries")
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text("Hello, $name", Modifier.weight(1f))
                TextButton(onClick = onSignOut) { Text("Sign out") }
            }
            if (error != null) ErrorText(error)
        }
        HorizontalDivider()
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = onRefresh,
            modifier = Modifier.fillMaxSize(),
        ) {
            LazyColumn(Modifier.fillMaxSize()) {
                if (deliveries != null && deliveries.isEmpty()) {
                    item {
                        Text("No deliveries left today.", Modifier.padding(24.dp))
                    }
                }
                items(deliveries.orEmpty(), key = { it.reference }) { delivery ->
                    ListItem(
                        headlineContent = { Text(delivery.reference) },
                        supportingContent = { Text(delivery.address) },
                        modifier = Modifier.clickable { onOpen(delivery.reference) },
                    )
                    HorizontalDivider()
                }
            }
        }
    }
}

@Composable
fun DeliveryScreen(
    reference: String,
    delivery: Delivery?,
    loading: Boolean,
    posting: Boolean,
    delivered: Boolean,
    error: String?,
    onConfirm: (signedBy: String) -> Unit,
    onBack: () -> Unit,
) {
    var signedBy by rememberSaveable(reference) { mutableStateOf("") }
    var confirming by rememberSaveable(reference) { mutableStateOf(false) }
    Column(
        Modifier
            .fillMaxSize()
            .padding(24.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Heading(reference)
        when {
            loading -> CircularProgressIndicator()

            delivery == null -> {
                Text(CourierViewModel.notWithYou(reference))
                TextButton(onClick = onBack) { Text("Back to deliveries") }
            }

            else -> {
                Text(delivery.recipient.name, style = MaterialTheme.typography.titleMedium)
                Text(delivery.address)
                Text(delivery.serviceLevel)
                if (delivered) {
                    Text(
                        "Delivered",
                        style = MaterialTheme.typography.titleLarge,
                        color = MaterialTheme.colorScheme.primary,
                        modifier = Modifier.semantics { liveRegion = LiveRegionMode.Polite },
                    )
                } else {
                    OutlinedTextField(
                        value = signedBy,
                        onValueChange = { signedBy = it },
                        label = { Text("Signed by") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth(),
                    )
                    if (error != null) ErrorText(error)
                    Button(
                        onClick = { confirming = true },
                        enabled = signedBy.isNotBlank() && !posting,
                        modifier = Modifier.fillMaxWidth(),
                    ) {
                        Text("Mark delivered")
                    }
                }
            }
        }
    }
    if (confirming) {
        val who = signedBy.trim()
        AlertDialog(
            onDismissRequest = { confirming = false },
            title = { Text("Mark $reference delivered?") },
            text = { Text("Signed by $who.") },
            confirmButton = {
                TextButton(onClick = {
                    confirming = false
                    onConfirm(who)
                }) { Text("Confirm") }
            },
            dismissButton = {
                TextButton(onClick = { confirming = false }) { Text("Cancel") }
            },
        )
    }
}

/**
 * Asks, once while the app runs, for leave to tell the courier of the deliveries
 * it records: the system shows its dialog, and the courier allows it or not.
 */
@Composable
private fun AskForNotifications(model: CourierViewModel) {
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
    val context = LocalContext.current
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) {}
    LaunchedEffect(Unit) {
        if (!model.askedForNotifications &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) {
            model.askedForNotifications = true
            ask.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
}
