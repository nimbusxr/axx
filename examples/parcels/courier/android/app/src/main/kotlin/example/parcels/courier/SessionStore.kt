// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.content.Context

/** Keeps the courier's sign-in across launches. */
class SessionStore(context: Context) {
    private val prefs = context.getSharedPreferences("sign-in", Context.MODE_PRIVATE)

    fun load(): Session? {
        val token = prefs.getString(TOKEN, null) ?: return null
        val courier = prefs.getString(COURIER, null) ?: return null
        return Session(token, courier, prefs.getString(NAME, null) ?: courier)
    }

    fun save(session: Session) {
        prefs.edit()
            .putString(TOKEN, session.token)
            .putString(COURIER, session.courier)
            .putString(NAME, session.name)
            .apply()
    }

    fun clear() {
        prefs.edit().clear().apply()
    }

    private companion object {
        const val TOKEN = "token"
        const val COURIER = "courier"
        const val NAME = "name"
    }
}
