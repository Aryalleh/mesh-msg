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
import androidx.core.app.Person
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
        val name = intent?.getStringExtra(EXTRA_NAME).orEmpty()
        val codec = intent?.getIntExtra(EXTRA_CODEC, CallAudio.AMR_WB) ?: CallAudio.AMR_WB
        val core = (application as MeshApp).backend.value?.core
        if (id.isEmpty() || core == null) {
            stopSelf()
            return START_NOT_STICKY
        }
        channels(this)
        val type = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0
        val ok = runCatching { ServiceCompat.startForeground(this, ID_CALL, ongoing(id, name), type) }.isSuccess
        if (!ok) { // no microphone permission, or started from the background
            runCatching { core.endCall(id) }
            stopSelf()
            return START_NOT_STICKY
        }
        if (audio == null) {
            audio = CallAudio(core, id, codec, getSystemService(AudioManager::class.java)).also {
                it.start()
                current = it
                Companion.audio.value = it
            }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        audio?.stop()
        if (current === audio) { current = null; Companion.audio.value = null }
        audio = null
        super.onDestroy()
    }

    /** The ongoing call: who, a running timer and Hang up (CallStyle, like other call apps). */
    private fun ongoing(id: String, name: String): Notification = NotificationCompat.Builder(this, CH_ONGOING)
        .setSmallIcon(R.drawable.ic_stat_mesh)
        .setStyle(NotificationCompat.CallStyle.forOngoingCall(person(name.ifEmpty { getString(R.string.call_ongoing) }), hangUp(this, id)))
        .setContentText(getString(R.string.call_ongoing))
        .setContentIntent(openApp(this))
        .setUsesChronometer(true)
        .setWhen(System.currentTimeMillis())
        .setShowWhen(true)
        .setOngoing(true)
        .setCategory(NotificationCompat.CATEGORY_CALL)
        .setVisibility(NotificationCompat.VISIBILITY_PUBLIC)
        .build()

    companion object {
        private const val EXTRA_ID = "call"
        private const val EXTRA_NAME = "name"
        /** MainActivity action: answer the ringing call [EXTRA_CALL]. */
        const val ACTION_ANSWER = "org.meshmsg.app.ANSWER"
        const val EXTRA_CALL = EXTRA_ID
        private const val EXTRA_CODEC = "codec"
        private const val CH_ONGOING = "call_ongoing"
        private const val CH_RING = "call_ring"
        private const val ID_CALL = 2
        private const val ID_RING = 3

        /** The audio of the active call, for the mute and speaker buttons. */
        @Volatile var current: CallAudio? = null
            private set

        /** The same, for the call screen's audio route button. */
        val audio = kotlinx.coroutines.flow.MutableStateFlow<CallAudio?>(null)

        fun start(c: Context, id: String, codec: Int, name: String = "") {
            runCatching {
                ContextCompat.startForegroundService(c, Intent(c, CallService::class.java).putExtra(EXTRA_ID, id).putExtra(EXTRA_CODEC, codec).putExtra(EXTRA_NAME, name))
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

        private fun person(name: String) = Person.Builder().setName(name).setImportant(true).build()

        private fun hangUp(c: Context, id: String): PendingIntent = PendingIntent.getBroadcast(
            c, 1, Intent(c, CallActionReceiver::class.java).putExtra(EXTRA_ID, id),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        /**
         * Rings for an incoming call: a CallStyle notification with Decline and
         * Answer that opens the call screen over the lock screen (full screen
         * when Android allows it, see [canFullScreen]). Answer opens the app,
         * which answers: the microphone may only start while it is in front.
         */
        fun ring(c: Context, id: String, title: String, video: Boolean = false) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
                ContextCompat.checkSelfPermission(c, android.Manifest.permission.POST_NOTIFICATIONS) != android.content.pm.PackageManager.PERMISSION_GRANTED
            ) return // the call still shows when the app is open
            channels(c)
            val n = NotificationCompat.Builder(c, CH_RING)
                .setSmallIcon(R.drawable.ic_stat_mesh)
                .setStyle(NotificationCompat.CallStyle.forIncomingCall(person(title), hangUp(c, id), answer(c, id)).setIsVideo(video))
                .setContentText(c.getString(if (video) R.string.call_incoming_video else R.string.call_incoming))
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

        private fun answer(c: Context, id: String): PendingIntent = PendingIntent.getActivity(
            c, 8, Intent(c, MainActivity::class.java).setAction(ACTION_ANSWER).putExtra(EXTRA_CALL, id)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_NEW_TASK),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )

        /** Android 14+ may refuse full-screen incoming calls until the user allows it. */
        fun canFullScreen(c: Context): Boolean =
            Build.VERSION.SDK_INT < 34 || c.getSystemService(NotificationManager::class.java).canUseFullScreenIntent()

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
