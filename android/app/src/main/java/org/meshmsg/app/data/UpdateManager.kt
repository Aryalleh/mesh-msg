package org.meshmsg.app.data

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
import androidx.core.content.FileProvider
import androidx.core.content.edit
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.meshmsg.app.MainActivity
import org.meshmsg.app.R
import org.meshmsg.meshcore.Core
import org.meshmsg.meshcore.Meshcore
import java.io.File
import java.util.Locale
import kotlin.random.Random

/** What the update banner shows. */
sealed interface UpdateStep {
    data object Idle : UpdateStep
    data class Downloading(val version: String) : UpdateStep
    data class Failed(val message: String) : UpdateStep
}

/**
 * Update check (roadmap B7, decisions D20): asks the relays, then GitHub, at
 * start and about once a day for the newest signed release; shows a banner
 * and one notification per new version. "Update" downloads the APK, checks it
 * against the signed SHA-256 and hands it to Android's installer. The request
 * carries no identity or contact data.
 */
class UpdateManager(private val context: Context, private val scope: CoroutineScope) {
    private val prefs = context.getSharedPreferences("update", Context.MODE_PRIVATE)
    private val _update = MutableStateFlow<AppUpdate?>(null)
    private val _step = MutableStateFlow<UpdateStep>(UpdateStep.Idle)
    private val _later = MutableStateFlow(prefs.getString(KEY_LATER, null))
    private val checking = Mutex()

    /** The newest release found (null until a check succeeds). */
    val update: StateFlow<AppUpdate?> = _update
    val step: StateFlow<UpdateStep> = _step

    /** The version the user put off with "Later"; its banner stays hidden. */
    val later: StateFlow<String?> = _later

    val currentVersion: String = runCatching {
        context.packageManager.getPackageInfo(context.packageName, 0).versionName
    }.getOrNull() ?: "0.0.0"

    /** Starts the daily check loop for [core]; the loop ends with [loopScope]. */
    fun run(core: () -> Core?, loopScope: CoroutineScope) {
        loopScope.launch(Dispatchers.IO) {
            delay(Random.nextLong(10_000, 40_000))
            while (true) {
                val ok = check(core)
                delay(if (ok) Random.nextLong(20 * HOUR, 28 * HOUR) else Random.nextLong(HOUR, 2 * HOUR))
            }
        }
    }

    /** Checks now; returns whether a release manifest was found. */
    suspend fun check(core: () -> Core?): Boolean = withContext(Dispatchers.IO) {
        checking.withLock {
            val c = core() ?: return@withLock false
            val u = AppUpdate.parse(runCatching { c.checkUpdate(currentVersion) }.getOrDefault("")) ?: return@withLock false
            _update.value = u
            if (u.available && prefs.getString(KEY_NOTIFIED, null) != u.version) {
                notify(u)
                prefs.edit { putString(KEY_NOTIFIED, u.version) }
            }
            true
        }
    }

    /** Shows a put-off version's banner again (after a manual check). */
    fun clearLater() {
        prefs.edit { remove(KEY_LATER) }
        _later.value = null
    }

    fun later(version: String) {
        prefs.edit { putString(KEY_LATER, version) }
        _later.value = version
    }

    /** Downloads the APK for this phone, verifies it, and opens the installer. */
    fun install() {
        val u = _update.value ?: return
        val f = u.apkFor(Build.SUPPORTED_ABIS.toList()) ?: run {
            _step.value = UpdateStep.Failed(context.getString(R.string.update_no_apk))
            return
        }
        if (_step.value is UpdateStep.Downloading) return
        _step.value = UpdateStep.Downloading(u.version)
        scope.launch(Dispatchers.IO) {
            val dir = File(context.cacheDir, "updates").apply { deleteRecursively(); mkdirs() }
            val apk = File(dir, f.name)
            val err = runCatching { Meshcore.downloadUpdate(f.url, f.sha256, f.size, apk.absolutePath) }.exceptionOrNull()
            if (err != null) {
                _step.value = UpdateStep.Failed(err.message ?: err.toString())
                return@launch
            }
            _step.value = UpdateStep.Idle
            val uri = FileProvider.getUriForFile(context, context.packageName + ".files", apk)
            val intent = Intent(Intent.ACTION_VIEW)
                .setDataAndType(uri, "application/vnd.android.package-archive")
                .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_ACTIVITY_NEW_TASK)
            withContext(Dispatchers.Main) { runCatching { context.startActivity(intent) } }
        }
    }

    private fun notify(u: AppUpdate) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(context, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(CHANNEL, context.getString(R.string.update_channel), NotificationManager.IMPORTANCE_DEFAULT))
        val open = PendingIntent.getActivity(
            context, 1, Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val n = NotificationCompat.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_mesh)
            .setContentTitle(context.getString(if (u.required) R.string.update_required_title else R.string.update_available_title, u.version))
            .setContentText(u.notesFor(Locale.getDefault().language).lineSequence().firstOrNull().orEmpty())
            .setStyle(NotificationCompat.BigTextStyle().bigText(u.notesFor(Locale.getDefault().language)))
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        nm.notify(ID, n)
    }

    private companion object {
        const val CHANNEL = "updates"
        const val ID = 4
        const val KEY_NOTIFIED = "notified"
        const val KEY_LATER = "later"
        const val HOUR = 3_600_000L
    }
}
