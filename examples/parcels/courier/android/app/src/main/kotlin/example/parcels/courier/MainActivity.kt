// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Intent
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels

/**
 * The couriers' app: sign in, today's deliveries, and a delivery. Launched with
 * an api_url extra, it talks to that parcels service; opened with
 * parcels-courier://deliveries/{reference}, it shows that delivery.
 */
class MainActivity : ComponentActivity() {
    private val model: CourierViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        // Before the view model first calls the service.
        Backend.readOverride(intent)
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) deepLinkReference(intent)?.let(model::openDeepLink)
        setContent { CourierApp(model) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        Backend.readOverride(intent)
        deepLinkReference(intent)?.let(model::openDeepLink)
    }

    /** The reference in parcels-courier://deliveries/{reference}, if the intent is that link. */
    private fun deepLinkReference(intent: Intent?): String? {
        val uri = intent?.takeIf { it.action == Intent.ACTION_VIEW }?.data ?: return null
        if (uri.scheme != "parcels-courier" || uri.host != "deliveries") return null
        return uri.pathSegments.firstOrNull()?.takeIf { it.isNotBlank() }
    }
}
