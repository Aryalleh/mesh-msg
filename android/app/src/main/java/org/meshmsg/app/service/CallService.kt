package org.meshmsg.app.service

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.AudioAttributes
import android.media.AudioManager
import android.media.RingtoneManager
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import org.meshmsg.app.MainActivity
import org.meshmsg.app.MeshApp
import org.meshmsg.app.R
import org.meshmsg.app.data.CallAudio

/**
 * Runs while a call is active: a foreground service of type microphone (so
 * the call goes on with the screen off or another app open) that owns the
 * call's audio ([CallAudio]). Started by [MeshApp] when a call becomes
 * active, stopped when it ends.
 */
class CallService : Service() {
    private var audio: CallAudio? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        val id = intent?.getStringExtra(EXTRA_ID).orEmpty()
        val codec = intent?.getIntExtra(EXTRA_CODEC, CallAudio.AMR_WB) ?: CallAudio.AMR_WB
        val core = (application as MeshApp).backend.value?.core
        if (id.isEmpty() || core == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        channels(this)
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0
        val ok = runCatching { ServiceCompat.startForeground(this, ID_CALL, ongoing(id), type) }.isSuccess
        if (!ok) { // no microphone permission, or started from the background
            runCatching { core.endCall(id) }
            stopSelf()
            return START_NOT_STICKY
        }
        if (audio == null) {
            audio = CallAudio(core, id, codec, getSystemService(AudioManager::class.java)).also {
                it.start()
                current = it
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        audio?.stop()
        if (current === audio) current = null
        audio = null
        super.onDestroy()
    }

    private fun ongoing(id: String): Notification = NotificationCompat.Builder(this, CH_ONGOING)
        .setSmallIcon(R.drawable.ic_stat_mesh)
        .setContentTitle(getString(R.string.call_ongoing))
        .setContentIntent(openApp(this))
        .addAction(0, getString(R.string.call_hang_up), PendingIntent.getBroadcast(
            this, 1, Intent(this, CallActionReceiver::class.java).putExtra(EXTRA_ID, id),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        ))
        .setOngoing(true)
        .setCategory(NotificationCompat.CATEGORY_CALL)
        .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
        .build()

    companion object {
        private const val EXTRA_ID = "call"
        private const val EXTRA_CODEC = "codec"
        private const val CH_ONGOING = "call_ongoing"
        private const val CH_RING = "call_ring"
        private const val ID_CALL = 2
        private const val ID_RING = 3

        /** The audio of the active call, for the mute and speaker buttons. */
        @Volatile var current: CallAudio? = null
            private set

        fun start(c: Context, id: String, codec: Int) {
            runCatching {
                ContextCompat.startForegroundService(c, Intent(c, CallService::class.java).putExtra(EXTRA_ID, id).putExtra(EXTRA_CODEC, codec))
            }
        }

        fun stop(c: Context) {
            c.stopService(Intent(c, CallService::class.java))
        }

        private fun channels(c: Context) {
            val nm = c.getSystemService(NotificationManager::class.java)
            nm.createNotificationChannel(NotificationChannel(CH_ONGOING, c.getString(R.string.call_channel_ongoing), NotificationManager.IMPORTANCE_LOW))
            nm.createNotificationChannel(
                NotificationChannel(CH_RING, c.getString(R.string.call_channel_ring), NotificationManager.IMPORTANCE_HIGH).apply {
                    setSound(
                        RingtoneManager.getDefaultUri(RingtoneManager.TYPE_RINGTONE),
                        AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_NOTIFICATION_RINGTONE).build(),
                    )
                    enableVibration(true)
                },
            )
        }

        private fun openApp(c: Context): PendingIntent = PendingIntent.getActivity(
            c, 7, Intent(c, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_NEW_TASK),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        /**
         * Rings for an incoming call: a high-priority notification that opens
         * the call screen over the lock screen. Answering happens in the app
         * (the microphone may only start while the app is in front).
         */
        fun ring(c: Context, title: String) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                ContextCompat.checkSelfPermission(c, android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED
            ) return // the call still shows when the app is open
            channels(c)
            val n = NotificationCompat.Builder(c, CH_RING)
                .setSmallIcon(R.drawable.ic_stat_mesh)
                .setContentTitle(title)
                .setContentText(c.getString(R.string.call_incoming))
                .setContentIntent(openApp(c))
                .setFullScreenIntent(openApp(c), true)
                .setCategory(NotificationCompat.CATEGORY_CALL)
                .setPriority(NotificationCompat.PRIORITY_MAX)
                .setOngoing(true)
                .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
                .build()
            n.flags = n.flags or Notification.FLAG_INSISTENT
            runCatching { c.getSystemService(NotificationManager::class.java).notify(ID_RING, n) }
        }

        fun stopRinging(c: Context) {
            c.getSystemService(NotificationManager::class.java).cancel(ID_RING)
        }
    }
}

/** The notification's hang-up button. */
class CallActionReceiver : android.content.BroadcastReceiver() {
    override fun onReceive(c: Context, i: Intent) {
        val id = i.getStringExtra("call") ?: return
        (c.applicationContext as MeshApp).backend.value?.endCall(id)
    }
}
