// SPDX-License-Identifier: Apache-2.0

package example.parcels.courier

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.work.Constraints
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.NetworkType
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import org.json.JSONException
import java.io.IOException
import java.util.concurrent.TimeUnit

/**
 * The round reminder: background work Android runs now and then, an hour apart at the soonest,
 * while the courier is signed in and the phone is online. It loads today's deliveries from the
 * parcels service and tells the courier how many are left, and which is next.
 */
class RoundReminderWorker(context: Context, params: WorkerParameters) : CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val session = SessionStore(applicationContext).load() ?: return Result.success()
        val left = try {
            CourierApi(Backend.url).deliveries(session)
        } catch (e: IOException) {
            return Result.retry()
        } catch (e: JSONException) {
            return Result.retry()
        }
        val next = left.firstOrNull() ?: return Result.success()
        notifyRoundLeft(applicationContext, left.size, next)
        return Result.success()
    }

    companion object {
        /** The work's unique name. */
        const val NAME = "round-reminder"

        /** Schedules the reminder: hourly, the first an hour from now, when the phone is online. */
        fun schedule(context: Context) {
            val request = PeriodicWorkRequestBuilder<RoundReminderWorker>(1, TimeUnit.HOURS)
                .setInitialDelay(1, TimeUnit.HOURS)
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .build()
            WorkManager.getInstance(context).enqueueUniquePeriodicWork(NAME, ExistingPeriodicWorkPolicy.KEEP, request)
        }

        /** Stops the reminders: the courier signed out. */
        fun cancel(context: Context) {
            WorkManager.getInstance(context).cancelUniqueWork(NAME)
        }
    }
}

/**
 * Tells the courier how many deliveries are left today, and which is next, in the "Round"
 * channel. Nothing is posted when the app may not post notifications.
 */
private fun notifyRoundLeft(context: Context, left: Int, next: Delivery) {
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
        context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
    ) {
        return
    }
    val manager = context.getSystemService(NotificationManager::class.java)
    manager.createNotificationChannel(
        NotificationChannel(ROUND_CHANNEL_ID, "Round", NotificationManager.IMPORTANCE_DEFAULT),
    )
    val notification = Notification.Builder(context, ROUND_CHANNEL_ID)
        .setSmallIcon(R.drawable.ic_delivered)
        .setContentTitle(if (left == 1) "1 delivery left today" else "$left deliveries left today")
        .setContentText("Next: ${next.reference}, ${next.address}")
        .setAutoCancel(true)
        .build()
    manager.notify(ROUND_NOTIFICATION_ID, notification)
}

private const val ROUND_CHANNEL_ID = "round"
private const val ROUND_NOTIFICATION_ID = 1
