package org.meshmsg.app.service

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat
import org.json.JSONObject
import org.meshmsg.app.MainActivity
import org.meshmsg.app.R

/** Leaders' alert when a member of their trip taps "I'm lost" (docs/trip.md). */
object TripAlerts {
    private const val CH = "trip_alerts"

    fun show(c: Context, info: String) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(c, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        val o = runCatching { JSONObject(info) }.getOrNull() ?: return
        val nm = c.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CH, c.getString(R.string.trip_channel_alerts), NotificationManager.IMPORTANCE_HIGH))
        val who = o.optString("who").ifBlank { o.optString("member").take(8) }
        val open = PendingIntent.getActivity(
            c, 9, Intent(c, MainActivity::class.java).putExtra(MainActivity.EXTRA_TRIPS, true).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        nm.notify(
            ("sos" + o.optString("member")).hashCode(),
            NotificationCompat.Builder(c, CH)
                .setSmallIcon(R.drawable.ic_stat_mesh)
                .setContentTitle(c.getString(R.string.trip_alert_title, who))
                .setContentText(c.getString(R.string.trip_alert_body, o.optString("name")))
                .setPriority(NotificationCompat.PRIORITY_HIGH)
                .setCategory(NotificationCompat.CATEGORY_ALARM)
                .setContentIntent(open)
                .setAutoCancel(true)
                .build(),
        )
    }
}
