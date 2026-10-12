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
 * parcels-courier://deliveries/{reference}, it shows that delivery, with
 * parcels-courier://deliveries/next the next one, and with
 * parcels-courier://deliveries today's deliveries.
 */
class MainActivity : ComponentActivity() {
    private val model: CourierViewModel by viewModels()

    /**
     * Where the intent links, opened once the app is in front: Android 15 fails a call to the
     * service an app makes before then, as a tapped notification brings it back.
     */
    private var link: String? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        // Before the view model first calls the service.
        Backend.readOverride(intent)
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        if (savedInstanceState == null) link = deepLinkReference(intent)
        setContent { CourierApp(model) }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        Backend.readOverride(intent)
        link = deepLinkReference(intent)
    }

    override fun onResume() {
        super.onResume()
        link?.let {
            link = null
            model.openDeepLink(it)
        }
    }

    /**
     * What parcels-courier://deliveries/{reference} links to, if the intent is that link: a
     * reference, "next", or "" for today's deliveries.
     */
    private fun deepLinkReference(intent: Intent?): String? {
        val uri = intent?.takeIf { it.action == Intent.ACTION_VIEW }?.data ?: return null
        if (uri.scheme != "parcels-courier" || uri.host != "deliveries") return null
        return uri.pathSegments.firstOrNull()?.trim().orEmpty()
    }
}
