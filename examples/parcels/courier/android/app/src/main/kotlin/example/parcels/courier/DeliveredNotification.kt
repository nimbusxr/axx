// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build

/**
 * Tells the courier a delivery was recorded, in the "Deliveries" channel.
 * Nothing is posted when the app may not post notifications.
 */
fun notifyDelivered(context: Context, reference: String, signedBy: String) {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
        context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
    ) {
        return
    }
    val manager = context.getSystemService(NotificationManager::class.java)
    manager.createNotificationChannel(
        NotificationChannel(CHANNEL_ID, "Deliveries", NotificationManager.IMPORTANCE_DEFAULT),
    )
    val notification = Notification.Builder(context, CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_delivered)
        .setContentTitle("$reference delivered")
        .setContentText("Signed by $signedBy")
        .setAutoCancel(true)
        .build()
    manager.notify(reference.hashCode(), notification)
}

private const val CHANNEL_ID = "deliveries"
