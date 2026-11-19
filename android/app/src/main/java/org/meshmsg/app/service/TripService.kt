package org.meshmsg.app.service

import android.Manifest
import android.annotation.SuppressLint
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.BatteryManager
import android.os.Build
import android.os.Looper
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.meshmsg.app.MainActivity
import org.meshmsg.app.MeshApp
import org.meshmsg.app.R
import org.meshmsg.app.data.CoreTrips
import org.meshmsg.app.data.Trip

/**
 * Sends this phone's position to the leaders of the trips the user agreed to
 * share with (docs/trip.md). A foreground service of type location: it runs
 * only while sharing, with a notification that says so and a Stop button, so
 * the user always knows. The core decides when a report is due (the trip's
 * interval, every minute in SOS or after a leader asked); this service keeps
 * fresh fixes coming and hands them over.
 */
class TripService : LifecycleService(), LocationListener {
    @Volatile private var last: Location? = null
    private var listeningMs = 0L
    private val started = SystemClock.elapsedRealtime()
    private var loop: Job? = null

    override fun onCreate() {
        super.onCreate()
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CH_TRIP, getString(R.string.trip_channel_sharing), NotificationManager.IMPORTANCE_LOW).apply {
                setShowBadge(false)
            },
        )
        if (!foreground(notification(emptyList()))) {
            stopSelf()
            return
        }
        loop = lifecycleScope.launch { run() }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        super.onStartCommand(intent, flags, startId)
        if (intent?.action == ACTION_STOP) {
            lifecycleScope.launch {
                withContext(Dispatchers.IO) {
                    val api = trips() ?: return@withContext
                    api.trips().filter { it.sharing }.forEach { api.stop(it.hash) }
                }
                stopSelf()
            }
        }
        return START_NOT_STICKY
    }

    private fun trips(): CoreTrips? = (application as MeshApp).backend.value?.core?.let { CoreTrips(it) }

    private suspend fun run() {
        val nm = getSystemService(NotificationManager::class.java)
        var shown = ""
        while (true) {
            val api = trips()
            val sharing = withContext(Dispatchers.IO) { api?.trips().orEmpty().filter { it.sharing } }
            if (api != null && sharing.isEmpty()) break
            if (sharing.isNotEmpty()) {
                val fastest = sharing.minOf { it.interval }
                listen(maxOf(20_000L, fastest * 1000L / 2))
                val key = sharing.joinToString { it.hash + it.sos }
                if (key != shown) {
                    shown = key
                    nm.notify(ID_TRIP, notification(sharing))
                }
                // A first fix can take a minute outdoors; wait that long before reporting without one.
                val waitForFix = last == null && hasLocation() && SystemClock.elapsedRealtime() - started < 60_000
                if (!waitForFix) withContext(Dispatchers.IO) { for (t in sharing) if (t.nextIn <= 0) report(api!!, t) }
            }
            delay(10_000)
        }
        stopSelf()
    }

    private fun report(api: CoreTrips, t: Trip, force: Boolean = false) {
        val loc = last
        val now = SystemClock.elapsedRealtimeNanos()
        val age = if (loc != null) ((now - loc.elapsedRealtimeNanos) / 1_000_000_000L).toInt() else 0
        api.report(t.hash, loc?.latitude ?: 0.0, loc?.longitude ?: 0.0, (loc?.accuracy ?: 0f).toInt(), age.coerceAtLeast(0), battery(), loc != null, force)
    }

    private fun battery(): Int = runCatching {
        getSystemService(BatteryManager::class.java).getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)
    }.getOrDefault(-1).let { if (it in 0..100) it else -1 }

    private fun hasLocation() =
        ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED

    /** (Re)requests fixes about every ms milliseconds from GPS and the network provider. */
    @SuppressLint("MissingPermission") // checked in hasLocation()
    private fun listen(ms: Long) {
        if (ms == listeningMs || !hasLocation()) return
        val lm = getSystemService(LocationManager::class.java) ?: return
        lm.removeUpdates(this)
        listeningMs = ms
        for (p in listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER)) {
            if (runCatching { lm.isProviderEnabled(p) }.getOrDefault(false)) {
                runCatching { lm.getLastKnownLocation(p)?.let(::better) }
                runCatching { lm.requestLocationUpdates(p, ms, 0f, this, Looper.getMainLooper()) }
            }
        }
    }

    /** Keeps the newest fix, unless it is much less accurate than a recent one. */
    private fun better(l: Location) {
        val cur = last
        if (cur == null) { last = l; return }
        val newer = l.elapsedRealtimeNanos - cur.elapsedRealtimeNanos
        if (newer > 60_000_000_000L || (newer >= 0 && l.accuracy <= cur.accuracy * 2)) last = l
    }

    override fun onLocationChanged(location: Location) {
        better(location)
    }

    @Deprecated("Deprecated in Java")
    override fun onStatusChanged(provider: String?, status: Int, extras: android.os.Bundle?) {}
    override fun onProviderEnabled(provider: String) {}
    override fun onProviderDisabled(provider: String) {}

    override fun onDestroy() {
        runCatching { getSystemService(LocationManager::class.java)?.removeUpdates(this) }
        super.onDestroy()
    }

    private fun foreground(n: Notification): Boolean = runCatching {
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION else 0
        ServiceCompat.startForeground(this, ID_TRIP, n, type)
    }.isSuccess // refused without the location permission, or when started from the background

    private fun notification(sharing: List<Trip>): Notification {
        val names = sharing.joinToString(", ") { it.name }
        val sos = sharing.any { it.sos }
        val open = PendingIntent.getActivity(
            this, 7, Intent(this, MainActivity::class.java).putExtra(MainActivity.EXTRA_TRIPS, true).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val stop = PendingIntent.getService(
            this, 8, Intent(this, TripService::class.java).setAction(ACTION_STOP), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CH_TRIP)
            .setSmallIcon(R.drawable.ic_stat_mesh)
            .setContentTitle(getString(if (sos) R.string.trip_notif_sos_title else R.string.trip_notif_title))
            .setContentText(if (names.isEmpty()) getString(R.string.trip_notif_starting) else getString(R.string.trip_notif_body, names))
            .setContentIntent(open)
            .addAction(0, getString(R.string.trip_stop), stop)
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .build()
    }

    companion object {
        private const val CH_TRIP = "trip_sharing"
        private const val ID_TRIP = 3
        private const val ACTION_STOP = "org.meshmsg.app.TRIP_STOP"

        fun hasPermission(c: Context) =
            ContextCompat.checkSelfPermission(c, Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED ||
                ContextCompat.checkSelfPermission(c, Manifest.permission.ACCESS_COARSE_LOCATION) == PackageManager.PERMISSION_GRANTED

        /** Starts the service if a trip is being shared (it stops itself when none is). */
        fun sync(c: Context, sharing: Boolean) {
            if (!sharing || !hasPermission(c)) return
            runCatching { ContextCompat.startForegroundService(c, Intent(c, TripService::class.java)) }
        }

        val PERMISSIONS = arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
    }
}
