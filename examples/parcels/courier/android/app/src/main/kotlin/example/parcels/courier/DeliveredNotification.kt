// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.drawable.Icon
import android.net.Uri
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
        // Tapped, today's deliveries; its action, the next one.
        .setContentIntent(link(context, "parcels-courier://deliveries", reference.hashCode()))
        .addAction(
            Notification.Action.Builder(
                Icon.createWithResource(context, R.drawable.ic_delivered),
                "Next delivery",
                link(context, "parcels-courier://deliveries/next", reference.hashCode() + 1),
            ).build(),
        )
        .build()
    manager.notify(reference.hashCode(), notification)
}

/** Opens the app at a link of its own, as the notification's tap or action. */
private fun link(context: Context, uri: String, request: Int): PendingIntent =
    PendingIntent.getActivity(
        context,
        request,
        Intent(Intent.ACTION_VIEW, Uri.parse(uri), context, MainActivity::class.java)
            .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT,
    )

private const val CHANNEL_ID = "deliveries"
