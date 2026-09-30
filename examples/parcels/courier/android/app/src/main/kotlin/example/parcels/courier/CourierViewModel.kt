// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.app.Application
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import org.json.JSONException
import java.io.IOException

/** The app's screens. */
sealed interface Screen {
    data object SignIn : Screen
    data object Deliveries : Screen
    data class Delivery(val reference: String) : Screen
}

/** What the app shows, and what the courier does in it. */
class CourierViewModel(app: Application) : AndroidViewModel(app) {
    private val store = SessionStore(app)

    var session by mutableStateOf(store.load())
        private set
    var screen by mutableStateOf<Screen>(if (session == null) Screen.SignIn else Screen.Deliveries)
        private set

    // Sign in.
    var signingIn by mutableStateOf(false)
        private set
    var signInError by mutableStateOf<String?>(null)
        private set

    /** Whether the app asked for leave to post notifications while it runs. */
    var askedForNotifications = false

    /** A delivery a deep link asked for before the courier signed in. */
    private var pendingReference: String? = null

    // Today's deliveries: null until they are loaded.
    var deliveries by mutableStateOf<List<Delivery>?>(null)
        private set
    var refreshing by mutableStateOf(false)
        private set
    var deliveriesError by mutableStateOf<String?>(null)
        private set
    private var refreshJob: Job? = null
    private var refreshGeneration = 0

    // A delivery.
    var posting by mutableStateOf(false)
        private set
    var delivered by mutableStateOf(false)
        private set
    var deliveryError by mutableStateOf<String?>(null)
        private set

    init {
        if (session != null) refresh()
    }

    private fun api() = CourierApi(Backend.url)

    fun signIn(courier: String, pin: String) {
        if (signingIn) return
        signingIn = true
        signInError = null
        viewModelScope.launch {
            try {
                val s = api().signIn(courier, pin)
                store.save(s)
                session = s
                deliveries = null
                val ref = pendingReference
                pendingReference = null
                if (ref != null) openDelivery(ref) else screen = Screen.Deliveries
                refresh()
            } catch (e: ApiException) {
                signInError = if (e.status == 401) WRONG_SIGN_IN else unexpected(e)
            } catch (e: IOException) {
                signInError = UNREACHABLE
            } catch (e: JSONException) {
                signInError = UNREACHABLE
            } finally {
                signingIn = false
            }
        }
    }

    fun signOut() {
        store.clear()
        refreshJob?.cancel()
        refreshing = false
        session = null
        deliveries = null
        deliveriesError = null
        signInError = null
        pendingReference = null
        screen = Screen.SignIn
    }

    /** The sign-in expired: the courier signs in again. */
    private fun expired() {
        signOut()
    }

    fun refresh() {
        val s = session ?: return
        val generation = ++refreshGeneration
        refreshJob?.cancel()
        refreshing = true
        refreshJob = viewModelScope.launch {
            try {
                deliveries = api().deliveries(s)
                deliveriesError = null
            } catch (e: ApiException) {
                if (e.status == 401) expired() else deliveriesError = unexpected(e)
            } catch (e: IOException) {
                deliveriesError = UNREACHABLE
            } catch (e: JSONException) {
                deliveriesError = UNREACHABLE
            } finally {
                if (generation == refreshGeneration) refreshing = false
            }
        }
    }

    /** parcels-courier://deliveries/{reference}: after signing in, if the courier has not. */
    fun openDeepLink(reference: String) {
        if (session == null) {
            pendingReference = reference
            screen = Screen.SignIn
            return
        }
        openDelivery(reference)
        refresh()
    }

    fun openDelivery(reference: String) {
        delivered = false
        deliveryError = null
        screen = Screen.Delivery(reference)
        if (deliveries == null) refresh()
    }

    fun backToDeliveries() {
        // A delivery just recorded leaves the list out of date.
        val stale = deliveries == null || delivered
        delivered = false
        screen = Screen.Deliveries
        if (stale) refresh()
    }

    fun markDelivered(reference: String, signedBy: String) {
        val s = session ?: return
        if (posting) return
        posting = true
        deliveryError = null
        viewModelScope.launch {
            try {
                api().markDelivered(s, reference, signedBy)
                // "Delivered" shows until the courier goes back to the list, which
                // reloads then; a courier who went back already gets it reloaded now.
                if (screen == Screen.Delivery(reference)) delivered = true else refresh()
                notifyDelivered(getApplication(), reference, signedBy)
            } catch (e: ApiException) {
                when (e.status) {
                    401 -> expired()
                    404 -> deliveryError = notWithYou(reference)
                    409 -> deliveryError = "$reference is not out for delivery."
                    else -> deliveryError = unexpected(e)
                }
            } catch (e: IOException) {
                deliveryError = UNREACHABLE
            } catch (e: JSONException) {
                deliveryError = UNREACHABLE
            } finally {
                posting = false
            }
        }
    }

    companion object {
        const val WRONG_SIGN_IN = "The courier ID or the PIN is wrong."
        const val UNREACHABLE = "The parcels service cannot be reached."

        fun notWithYou(reference: String) = "$reference is not out for delivery with you."

        private fun unexpected(e: ApiException) = "The parcels service answered ${e.status}."
    }
}
