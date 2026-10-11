// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Looper

/**
 * The van's location, from the phone's GPS, every second while the courier shares it: where it
 * is, how fast it goes and which way.
 */
class VanLocation(private val context: Context, private val onFix: (Location) -> Unit) : LocationListener {
    private val manager = context.getSystemService(LocationManager::class.java)

    fun allowed(): Boolean =
        context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED

    /** Starts listening to the GPS; false when the courier has not allowed the app their location. */
    @SuppressLint("MissingPermission")
    fun start(): Boolean {
        if (!allowed()) return false
        manager.requestLocationUpdates(LocationManager.GPS_PROVIDER, 1000L, 0f, this, Looper.getMainLooper())
        return true
    }

    fun stop() {
        manager.removeUpdates(this)
    }

    override fun onLocationChanged(location: Location) = onFix(location)
}
