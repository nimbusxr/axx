// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Intent
import android.net.Uri
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/** Where the parcels service is: the build's default, or a launch intent's api_url extra. */
object Backend {
    @Volatile
    var url: String = BuildConfig.API_URL
        private set

    /** Takes the intent's api_url extra, if it has one, for the rest of the process. */
    fun readOverride(intent: Intent?) {
        intent?.getStringExtra("api_url")?.trim()?.takeIf { it.isNotEmpty() }?.let { url = it.trimEnd('/') }
    }
}

/** A courier's sign-in, as the service answers it and the app keeps it. */
data class Session(val token: String, val courier: String, val name: String)

data class Recipient(val name: String, val street: String, val city: String, val postcode: String, val country: String)

/** A parcel out for delivery with the courier. */
data class Delivery(val reference: String, val serviceLevel: String, val recipient: Recipient) {
    val address: String get() = "${recipient.street}, ${recipient.postcode} ${recipient.city}"
}

/** The service answered with a status other than 2xx. */
class ApiException(val status: Int) : IOException("the parcels service answered $status")

/** The couriers' endpoints of the parcels service. */
class CourierApi(private val baseUrl: String) {

    suspend fun signIn(courier: String, pin: String): Session {
        val body = JSONObject().put("courier", courier).put("pin", pin)
        val out = call("POST", "/api/couriers/sign-in", null, body)
        return Session(out.getString("token"), out.getString("courier"), out.getString("name"))
    }

    suspend fun deliveries(session: Session): List<Delivery> {
        val out = call("GET", "/api/couriers/${enc(session.courier)}/deliveries", session.token, null)
        val list = out.getJSONArray("deliveries")
        return List(list.length()) { i ->
            val d = list.getJSONObject(i)
            val r = d.getJSONObject("recipient")
            Delivery(
                reference = d.getString("reference"),
                serviceLevel = d.optString("serviceLevel"),
                recipient = Recipient(
                    name = r.optString("name"),
                    street = r.optString("street"),
                    city = r.optString("city"),
                    postcode = r.optString("postcode"),
                    country = r.optString("country"),
                ),
            )
        }
    }

    suspend fun markDelivered(session: Session, reference: String, signedBy: String) {
        val body = JSONObject().put("signedBy", signedBy)
        call("POST", "/api/couriers/${enc(session.courier)}/deliveries/${enc(reference)}", session.token, body)
    }

    /** Where the van is, its speed in metres a second and its course in degrees, when the GPS knows them. */
    suspend fun reportLocation(session: Session, latitude: Double, longitude: Double, speed: Float?, course: Float?) {
        val body = JSONObject().put("latitude", latitude).put("longitude", longitude)
        if (speed != null) body.put("speed", speed.toDouble())
        if (course != null) body.put("course", course.toDouble())
        call("POST", "/api/couriers/${enc(session.courier)}/location", session.token, body)
    }

    /** A document for the delivery, like the recipient's safe-place authorization: its content, under its file's name. */
    suspend fun attachDocument(session: Session, reference: String, name: String, contentType: String, content: ByteArray) {
        call("PUT", "/api/couriers/${enc(session.courier)}/deliveries/${enc(reference)}/documents/${enc(name)}", session.token, content, contentType)
    }

    private suspend fun call(method: String, path: String, token: String?, body: JSONObject?): JSONObject =
        call(method, path, token, body?.toString()?.toByteArray(), "application/json")

    private suspend fun call(method: String, path: String, token: String?, content: ByteArray?, contentType: String): JSONObject =
        withContext(Dispatchers.IO) {
            val conn = URL(baseUrl + path).openConnection() as HttpURLConnection
            try {
                conn.requestMethod = method
                conn.connectTimeout = 10_000
                conn.readTimeout = 15_000
                conn.setRequestProperty("Accept", "application/json")
                if (token != null) conn.setRequestProperty("Authorization", "Bearer $token")
                if (content != null) {
                    conn.doOutput = true
                    conn.setRequestProperty("Content-Type", contentType)
                    conn.outputStream.use { it.write(content) }
                }
                val status = conn.responseCode
                if (status !in 200..299) throw ApiException(status)
                if (status == HttpURLConnection.HTTP_NO_CONTENT) JSONObject() else JSONObject(conn.inputStream.bufferedReader().use { it.readText() })
            } finally {
                conn.disconnect()
            }
        }

    private fun enc(segment: String): String = Uri.encode(segment)
}
