// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.app.Application
import android.graphics.BitmapFactory
import android.net.Uri
import android.provider.OpenableColumns
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
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

    /** The documents the courier attached to the delivery, as the app tells of each. */
    var attached by mutableStateOf<List<String>>(emptyList())
        private set
    var attaching by mutableStateOf(false)
        private set

    /** The check of the photos the courier attaches, loaded when the first comes. */
    private val photos by lazy { PhotoCheck() }

    /** What else the photos show, as ML Kit sees it, loaded when the first comes. */
    private val vision by lazy { DeliveryPhoto() }

    /** The check of the photos the courier reports damage with, loaded when the first comes. */
    private val damage by lazy { DamageCheck(getApplication()) }

    /** The model that writes the damage out from the courier's note of it, loaded when the first comes. */
    private val wording by lazy { DamageWording(getApplication()) }

    /** The recipients' notes, as the courier reads them, by the note. */
    private val notes by lazy { DeliveryNotes(getApplication()) }
    private val readNotes = mutableMapOf<String, String>()

    // The van's location, while the courier shares it.
    var sharingLocation by mutableStateOf(false)
        private set
    private val van = VanLocation(app, ::reportLocation)
    private var reporting = false

    init {
        if (session != null) {
            RoundReminderWorker.schedule(app)
            refresh()
        }
    }

    /** Shares the van's location with the parcels service, or stops; false if the app may not read it. */
    fun shareLocation(on: Boolean): Boolean {
        van.stop()
        sharingLocation = on && van.start()
        return sharingLocation == on
    }

    fun locationAllowed(): Boolean = van.allowed()

    private fun reportLocation(fix: android.location.Location) {
        val s = session ?: return
        if (reporting) return // the fix before is still on its way; the next one follows it
        reporting = true
        viewModelScope.launch {
            try {
                api().reportLocation(
                    s, fix.latitude, fix.longitude,
                    if (fix.hasSpeed()) fix.speed else null,
                    if (fix.hasBearing()) fix.bearing else null,
                )
            } catch (e: IOException) {
                // A fix that did not get through: the next one, a second later, will.
            } catch (e: JSONException) {
            } finally {
                reporting = false
            }
        }
    }

    override fun onCleared() {
        van.stop()
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
                RoundReminderWorker.schedule(getApplication())
                session = s
                deliveries = null
                val ref = pendingReference
                pendingReference = null
                when (ref) {
                    null, "" -> screen = Screen.Deliveries
                    "next" -> openNext()
                    else -> openDelivery(ref)
                }
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
        shareLocation(false)
        store.clear()
        RoundReminderWorker.cancel(getApplication())
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
    /** parcels-courier://deliveries/{reference}: a delivery, the next one ("next"), or today's ("") */
    fun openDeepLink(reference: String) {
        if (session == null) {
            pendingReference = reference
            screen = Screen.SignIn
            return
        }
        when (reference) {
            "" -> backToDeliveries()
            "next" -> openNext()
            else -> {
                openDelivery(reference)
                refresh()
            }
        }
    }

    /** The first of today's deliveries not delivered yet, as the service has them now; the list when none is left. */
    private fun openNext() {
        val s = session ?: return
        viewModelScope.launch {
            try {
                val left = api().deliveries(s)
                deliveries = left
                val next = left.firstOrNull()
                if (next != null) openDelivery(next.reference) else backToDeliveries()
            } catch (e: IOException) {
                backToDeliveries()
            } catch (e: JSONException) {
                backToDeliveries()
            }
        }
    }

    /** A recipient's delivery note, as the courier reads it: the note itself, should the model fail. */
    suspend fun noteForCourier(note: String): String {
        readNotes[note]?.let { return it }
        val read = withContext(Dispatchers.Default) {
            try {
                notes.forCourier(note)
            } catch (e: Exception) {
                note
            }
        }
        readNotes[note] = read
        return read
    }

    suspend fun saveNote(reference: String, note: String): Boolean {
        val signedIn = session ?: return false
        return try {
            api().saveNote(signedIn, reference, note)
            true
        } catch (_: Exception) {
            deliveryError = "The note could not be saved. Please try again."
            false
        }
    }

    /** No one home: the attempt, recorded with what the phone heard at the door. */
    suspend fun recordAttempt(reference: String, heard: String): Boolean {
        val signedIn = session ?: return false
        return try {
            api().recordAttempt(signedIn, reference, heard)
            true
        } catch (_: Exception) {
            deliveryError = "The attempt could not be recorded. Please try again."
            false
        }
    }

    fun openDelivery(reference: String) {
        delivered = false
        deliveryError = null
        attached = emptyList()
        screen = Screen.Delivery(reference)
        if (deliveries == null) refresh()
    }

    fun backToDeliveries() {
        // A delivery just recorded leaves the list out of date.
        val stale = deliveries == null || delivered
        delivered = false
        attached = emptyList()
        screen = Screen.Deliveries
        if (stale) refresh()
    }

    /**
     * Attaches a document the courier picked in the phone's files, like the recipient's
     * safe-place authorization, to the delivery.
     */
    fun attachDocument(reference: String, document: Uri) {
        val s = session ?: return
        if (attaching) return
        attaching = true
        deliveryError = null
        viewModelScope.launch {
            val resolver = getApplication<Application>().contentResolver
            val name = withContext(Dispatchers.IO) {
                resolver.query(document, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                    if (c.moveToFirst()) c.getString(0) else null
                }
            } ?: document.lastPathSegment ?: "document"
            try {
                val content = withContext(Dispatchers.IO) { resolver.openInputStream(document)?.use { it.readBytes() } }
                if (content == null) {
                    deliveryError = "$name cannot be read."
                    return@launch
                }
                val type = resolver.getType(document) ?: "application/octet-stream"
                var upload: ByteArray = content
                var done = "$name attached"
                val photo = if (type.startsWith("image/")) {
                    withContext(Dispatchers.Default) { BitmapFactory.decodeByteArray(content, 0, content.size) }
                } else {
                    null
                }
                // A photo of a page, like the recipient's signed note, is a document.
                val page = photo != null && withContext(Dispatchers.Default) { runCatching { vision.isDocument(photo) }.getOrDefault(false) }
                // A photo must show the parcel, where the courier leaves it.
                if (type.startsWith("image/") && !page) {
                    if (photo == null || !withContext(Dispatchers.Default) { photos.check(photo).parcel }) {
                        deliveryError = "$name shows no parcel: take a photo of the parcel where you leave it."
                        return@launch
                    }
                    // What else it shows. A photo ML Kit cannot look at goes as it is.
                    val seen = withContext(Dispatchers.Default) { runCatching { vision.look(photo) }.getOrNull() }
                    val other = seen?.reference
                    if (other != null && other != reference) {
                        deliveryError = "$name shows the label of $other: take a photo of $reference."
                        return@launch
                    }
                    if (seen?.dog == true) {
                        deliveryError = "$name shows a dog beside the parcel: leave the parcel out of its reach, and take a photo of it there."
                        return@launch
                    }
                    // The whole parcel, not cut off at the photo's edge.
                    if (withContext(Dispatchers.Default) { runCatching { vision.cutOff(photo) }.getOrDefault(false) }) {
                        deliveryError = "$name shows the parcel cut off at its edge: step back and take a photo of the whole parcel."
                        return@launch
                    }
                    // Nobody's face leaves the phone.
                    withContext(Dispatchers.Default) { runCatching { vision.blurringPeople(photo) }.getOrNull() }?.let {
                        upload = it
                        done = "$name attached: the people in it are blurred out"
                    }
                }
                api().attachDocument(s, reference, name, type, upload)
                if (screen == Screen.Delivery(reference)) attached = attached + done
            } catch (e: ApiException) {
                when (e.status) {
                    401 -> expired()
                    404 -> deliveryError = notWithYou(reference)
                    413 -> deliveryError = "$name is too large to attach."
                    else -> deliveryError = unexpected(e)
                }
            } catch (e: IOException) {
                deliveryError = UNREACHABLE
            } catch (e: JSONException) {
                deliveryError = UNREACHABLE
            } finally {
                attaching = false
            }
        }
    }

    /**
     * Writes the damage out from the courier's note of it ("corner crushed, wet"), with the
     * app's own language model: attached to the delivery as damage-description.txt.
     */
    fun describeDamage(reference: String, note: String) {
        val s = session ?: return
        if (attaching || note.isBlank()) return
        attaching = true
        deliveryError = null
        viewModelScope.launch {
            try {
                val description = try {
                    withContext(Dispatchers.Default) { wording.describe(note.trim()) }
                } catch (e: IOException) {
                    deliveryError = "The damage cannot be written out: the app's model cannot be downloaded."
                    return@launch
                } catch (e: IllegalStateException) {
                    deliveryError = "The damage cannot be written out: ${e.message}."
                    return@launch
                }
                api().attachDocument(s, reference, "damage-description.txt", "text/plain", description.toByteArray())
                if (screen == Screen.Delivery(reference)) attached = attached + "Damage: $description"
            } catch (e: ApiException) {
                when (e.status) {
                    401 -> expired()
                    404 -> deliveryError = notWithYou(reference)
                    else -> deliveryError = unexpected(e)
                }
            } catch (e: IOException) {
                deliveryError = UNREACHABLE
            } catch (e: JSONException) {
                deliveryError = UNREACHABLE
            } finally {
                attaching = false
            }
        }
    }

    /**
     * Reports the parcel damaged, with a photo of the damage: attached to the delivery as
     * damage-<the photo's name>, once the company's damage check sees damage in it.
     */
    fun reportDamage(reference: String, photo: Uri) {
        val s = session ?: return
        if (attaching) return
        attaching = true
        deliveryError = null
        viewModelScope.launch {
            val resolver = getApplication<Application>().contentResolver
            val name = withContext(Dispatchers.IO) {
                resolver.query(photo, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                    if (c.moveToFirst()) c.getString(0) else null
                }
            } ?: photo.lastPathSegment ?: "photo"
            try {
                val content = withContext(Dispatchers.IO) { resolver.openInputStream(photo)?.use { it.readBytes() } }
                val image = content?.let { withContext(Dispatchers.Default) { BitmapFactory.decodeByteArray(it, 0, it.size) } }
                if (content == null || image == null) {
                    deliveryError = "$name cannot be read."
                    return@launch
                }
                if (!withContext(Dispatchers.Default) { damage.damaged(image) }) {
                    deliveryError = "$name shows no damage: take a photo of where the parcel is damaged."
                    return@launch
                }
                val type = resolver.getType(photo) ?: "image/jpeg"
                api().attachDocument(s, reference, "damage-$name", type, content)
                if (screen == Screen.Delivery(reference)) attached = attached + "Damage reported: $name"
            } catch (e: ApiException) {
                when (e.status) {
                    401 -> expired()
                    404 -> deliveryError = notWithYou(reference)
                    413 -> deliveryError = "$name is too large to attach."
                    else -> deliveryError = unexpected(e)
                }
            } catch (e: IOException) {
                deliveryError = UNREACHABLE
            } catch (e: JSONException) {
                deliveryError = UNREACHABLE
            } finally {
                attaching = false
            }
        }
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
