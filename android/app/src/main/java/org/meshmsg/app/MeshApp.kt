package org.meshmsg.app

import android.Manifest
import android.app.Application
import android.bluetooth.BluetoothAdapter
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.meshmsg.app.ble.BleMesh
import org.meshmsg.app.data.BatteryMode
import org.meshmsg.app.data.CoreBackend
import org.meshmsg.app.data.NetBinder
import org.meshmsg.app.data.Settings
import org.meshmsg.app.lora.UsbLoRa
import org.meshmsg.app.service.MeshService
import java.io.File

/**
 * Owns the mesh: the Go core ([CoreBackend]) and the radios attached to it.
 * The UI observes [backend]; [MeshService] only keeps the process alive.
 */
class MeshApp : Application() {
    lateinit var settings: Settings
        private set

    private val _backend = MutableStateFlow<CoreBackend?>(null)
    val backend: StateFlow<CoreBackend?> = _backend

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val lock = Mutex()
    private var ble: BleMesh? = null
    private var usb: UsbLoRa? = null
    private var restartJob: Job? = null

    private val meshDir: File get() = File(filesDir, "mesh")
    val meshDirPath: String get() = meshDir.absolutePath

    override fun onCreate() {
        super.onCreate()
        settings = Settings(this)
        registerReceiver(
            object : BroadcastReceiver() {
                override fun onReceive(c: Context, i: Intent) {
                    // Bluetooth was switched on or off: rebuild the BLE transport.
                    scope.launch { lock.withLock { restartBle() } }
                }
            },
            IntentFilter(BluetoothAdapter.ACTION_STATE_CHANGED),
        )
        if (settings.onboarded) start()
    }

    /** Opens the core (creating the identity on first run) and the radios. */
    fun start() {
        scope.launch {
            lock.withLock {
                if (_backend.value == null) {
                    _backend.value = CoreBackend(meshDir.absolutePath, settings.coreConfig(), NetBinder(this@MeshApp)).also(::watchCalls)
                    restartBle()
                    usb = UsbLoRa(this@MeshApp).also { it.attachIfPresent(_backend.value!!.core) }
                }
            }
            if (settings.batteryMode != BatteryMode.Off) MeshService.start(this@MeshApp)
        }
    }

    /**
     * Rings for incoming calls and runs [org.meshmsg.app.service.CallService]
     * (the audio) while a call is active.
     */
    private fun watchCalls(b: CoreBackend) {
        scope.launch {
            var ringing: String? = null
            var active: String? = null
            b.call.collect { c ->
                if (c != null && c.incoming && c.state == "ringing") {
                    if (ringing != c.id) {
                        ringing = c.id
                        val name = b.chats.value.firstOrNull { it.contact.hash == c.peer }?.contact?.displayName ?: c.peer.take(8)
                        org.meshmsg.app.service.CallService.ring(this@MeshApp, name)
                    }
                } else if (ringing != null) {
                    ringing = null
                    org.meshmsg.app.service.CallService.stopRinging(this@MeshApp)
                }
                if (c != null && c.state == "active" && active != c.id) {
                    active = c.id
                    org.meshmsg.app.service.CallService.start(this@MeshApp, c.id, c.codec)
                } else if (active != null && (c == null || c.ended || c.id != active)) {
                    active = null
                    org.meshmsg.app.service.CallService.stop(this@MeshApp)
                }
            }
        }
    }

    /** Applies changed settings (relays, gateway, name, battery): reopens the core. */
    fun restartSoon() {
        restartJob?.cancel()
        restartJob = scope.launch {
            delay(1_500) // the user may still be typing
            lock.withLock {
                stopLocked()
                _backend.value = CoreBackend(meshDir.absolutePath, settings.coreConfig(), NetBinder(this@MeshApp)).also(::watchCalls)
                restartBle()
                usb = UsbLoRa(this@MeshApp).also { it.attachIfPresent(_backend.value!!.core) }
            }
            if (settings.batteryMode != BatteryMode.Off) MeshService.start(this@MeshApp) else MeshService.stop(this@MeshApp)
        }
    }

    private fun stopLocked() {
        ble?.stop()
        ble = null
        usb = null
        _backend.value?.stop()
        _backend.value = null
    }

    fun bluetoothAllowed(): Boolean = BLE_PERMISSIONS.all {
        ContextCompat.checkSelfPermission(this, it) == PackageManager.PERMISSION_GRANTED
    }

    /** Called when the Bluetooth permission was just granted. */
    fun onBluetoothPermission() {
        scope.launch { lock.withLock { restartBle() } }
    }

    private fun restartBle() {
        ble?.stop()
        ble = null
        val core = _backend.value?.core ?: return
        if (!bluetoothAllowed()) return
        ble = BleMesh(this, core, saver = settings.batteryMode == BatteryMode.Saver).takeIf { it.start() }
    }

    /** Looks for a LoRa node on USB; returns false if there is none. */
    fun connectUsb(): Boolean {
        val core = _backend.value?.core ?: return false
        return (usb ?: UsbLoRa(this).also { usb = it }).attachOrRequest(core)
    }

    /** Panic wipe: identity, contacts, messages, stored packets, settings. */
    fun wipe(done: () -> Unit) {
        scope.launch {
            lock.withLock {
                ble?.stop()
                ble = null
                usb = null
                val b = _backend.value
                _backend.value = null
                runCatching { b?.wipe() }
                meshDir.deleteRecursively()
                settings.clear()
            }
            MeshService.stop(this@MeshApp)
            launch(Dispatchers.Main) { done() }
        }
    }

    companion object {
        val BLE_PERMISSIONS: Array<String> = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            arrayOf(Manifest.permission.BLUETOOTH_SCAN, Manifest.permission.BLUETOOTH_CONNECT, Manifest.permission.BLUETOOTH_ADVERTISE)
        } else {
            arrayOf(Manifest.permission.ACCESS_FINE_LOCATION)
        }
    }
}
