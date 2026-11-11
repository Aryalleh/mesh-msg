package org.meshmsg.app.service

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.pm.ServiceInfo
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleService
import androidx.lifecycle.lifecycleScope
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.filterNotNull
import kotlinx.coroutines.flow.map
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.launch
import org.meshmsg.app.MainActivity
import org.meshmsg.app.MeshApp
import org.meshmsg.app.R

/**
 * Keeps the mesh running in the background (docs/android.md §7): a foreground
 * service of type connectedDevice with a quiet, persistent notification that
 * says in words whether a path exists. Incoming messages get their own
 * notification — with no message text, so nothing sensitive shows on a lock
 * screen.
 */
class MeshService : LifecycleService() {
    @OptIn(ExperimentalCoroutinesApi::class)
    override fun onCreate() {
        super.onCreate()
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CH_MESH, getString(R.string.notif_channel_mesh), NotificationManager.IMPORTANCE_LOW))
        nm.createNotificationChannel(NotificationChannel(CH_MSG, getString(R.string.notif_channel_messages), NotificationManager.IMPORTANCE_DEFAULT))
        if (!foreground(statusNotification(false))) {
            stopSelf()
            return
        }
        val app = application as MeshApp
        lifecycleScope.launch {
            app.backend.filterNotNull().collectLatest { b ->
                launch {
                    b.status.map { it.anyPathUp }.distinctUntilChanged().collect { up -> nm.notify(ID_MESH, statusNotification(up)) }
                }
                b.incoming.receiveAsFlow().collect { m ->
                    if (canNotify()) nm.notify(m.peer.hashCode(), messageNotification(m.peer))
                }
            }
        }
    }

    private fun foreground(n: Notification): Boolean = runCatching {
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) ServiceInfo.FOREGROUND_SERVICE_TYPE_CONNECTED_DEVICE else 0
        ServiceCompat.startForeground(this, ID_MESH, n, type)
    }.isSuccess // refused without the Bluetooth permission on Android 14+

    private fun canNotify() = Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU ||
        ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED

    private fun open(chat: String? = null): PendingIntent = PendingIntent.getActivity(
        this, chat?.hashCode() ?: 0,
        Intent(this, MainActivity::class.java).putExtra(MainActivity.EXTRA_CHAT, chat).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )

    private fun statusNotification(up: Boolean): Notification = NotificationCompat.Builder(this, CH_MESH)
        .setSmallIcon(R.drawable.ic_stat_mesh)
        .setContentTitle(getString(R.string.notif_mesh_title))
        .setContentText(getString(if (up) R.string.notif_mesh_up else R.string.notif_mesh_down))
        .setContentIntent(open())
        .setOngoing(true)
        .setOnlyAlertOnce(true)
        .setVisibility(NotificationCompat.VISIBILITY_SECRET)
        .build()

    private fun messageNotification(peer: String): Notification = NotificationCompat.Builder(this, CH_MSG)
        .setSmallIcon(R.drawable.ic_stat_mesh)
        .setContentTitle(getString(R.string.notif_new_message))
        .setContentIntent(open(peer))
        .setAutoCancel(true)
        .setVisibility(NotificationCompat.VISIBILITY_PRIVATE)
        .build()

    companion object {
        private const val CH_MESH = "mesh"
        private const val CH_MSG = "messages"
        private const val ID_MESH = 1

        fun start(c: Context) {
            runCatching { ContextCompat.startForegroundService(c, Intent(c, MeshService::class.java)) }
        }

        fun stop(c: Context) {
            c.stopService(Intent(c, MeshService::class.java))
        }
    }
}
