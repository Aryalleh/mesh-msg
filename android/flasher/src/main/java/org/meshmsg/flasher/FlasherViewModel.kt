package org.meshmsg.flasher

import android.app.Application
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbManager
import android.os.Build
import androidx.core.content.ContextCompat
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import org.meshmsg.flasher.usb.UsbSerial
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

data class UiState(
    val usbHost: Boolean = true,
    val imagesVersion: String? = null,
    val device: String? = null,
    val permitted: Boolean = false,
    val busy: Boolean = false,
    val monitoring: Boolean = false,
    val phase: String = "",
    val progress: Float = 0f,
    val outcome: Outcome? = null,
    val hotspot: Pair<String, String>? = null,
    val wifiBusy: Boolean = false,
    val wifiMessage: Pair<Boolean, String>? = null, // ok?, text
    val log: List<String> = emptyList(),
)

sealed interface Outcome {
    data object Ok : Outcome
    data class Failed(val message: String) : Outcome
}

class FlasherViewModel(app: Application) : AndroidViewModel(app) {
    private val ctx: Context get() = getApplication()
    private val usb = ctx.getSystemService(UsbManager::class.java)
    private val _state = MutableStateFlow(UiState())
    val state: StateFlow<UiState> = _state
    private val images: List<BoardImage>? = loadImages()
    @Volatile private var permissionLatch = CountDownLatch(1)
    private var monitor: Job? = null

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(c: Context, intent: Intent) {
            if (intent.action == ACTION_PERMISSION) permissionLatch.countDown()
            refresh()
        }
    }

    init {
        _state.update {
            it.copy(
                usbHost = ctx.packageManager.hasSystemFeature(PackageManager.FEATURE_USB_HOST),
                imagesVersion = if (images.isNullOrEmpty()) null else readAsset("VERSION")?.toString(Charsets.UTF_8)?.trim() ?: "?",
            )
        }
        val filter = IntentFilter().apply {
            addAction(ACTION_PERMISSION)
            addAction(UsbManager.ACTION_USB_DEVICE_ATTACHED)
            addAction(UsbManager.ACTION_USB_DEVICE_DETACHED)
        }
        ContextCompat.registerReceiver(ctx, receiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
        viewModelScope.launch {
            while (isActive) {
                refresh()
                delay(1000)
            }
        }
    }

    override fun onCleared() {
        runCatching { ctx.unregisterReceiver(receiver) }
        monitor?.cancel()
    }

    private fun readAsset(name: String): ByteArray? =
        runCatching { ctx.assets.open("board/$name").use { it.readBytes() } }.getOrNull()

    private fun loadImages(): List<BoardImage>? {
        val manifest = readAsset("images.txt") ?: return null
        return BoardImages.parse(manifest.toString(Charsets.UTF_8)) { name ->
            readAsset(name) ?: throw IOException("missing $name in this APK")
        }
    }

    fun refresh() {
        val manager = usb ?: return
        val d = UsbSerial.find(manager)
        _state.update {
            it.copy(
                device = d?.let { dev -> describe(dev) },
                permitted = d != null && manager.hasPermission(d),
            )
        }
    }

    private fun describe(d: UsbDevice): String {
        val kind = UsbSerial.kindOf(d)?.label ?: "USB"
        val name = d.productName?.takeIf { it.isNotBlank() }
        return if (name != null && !kind.contains(name)) "$kind — $name" else kind
    }

    fun requestPermission() {
        val manager = usb ?: return
        val d = UsbSerial.find(manager) ?: return
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or
            (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0)
        manager.requestPermission(d, PendingIntent.getBroadcast(ctx, 0, Intent(ACTION_PERMISSION).setPackage(ctx.packageName), flags))
    }

    /** Opens the board's port, waiting for it to (re)appear and for permission. Worker thread. */
    private fun openPort(): UsbSerial {
        val manager = usb ?: throw IOException(ctx.getString(R.string.no_usb_host))
        var d: UsbDevice? = null
        for (i in 0 until 40) {
            d = UsbSerial.find(manager)
            if (d != null) break
            Thread.sleep(250)
        }
        d ?: throw IOException(ctx.getString(R.string.no_board))
        if (!manager.hasPermission(d)) {
            permissionLatch = CountDownLatch(1)
            requestPermission()
            permissionLatch.await(60, TimeUnit.SECONDS)
            if (!manager.hasPermission(d)) throw IOException("USB permission not granted")
        }
        return UsbSerial.open(manager, d)
    }

    private fun log(line: String) {
        _state.update { it.copy(log = (it.log + line).takeLast(400)) }
    }

    fun flash(mode: Mode) {
        val list = images ?: return
        if (_state.value.busy) return
        stopMonitor()
        _state.update { it.copy(busy = true, outcome = null, progress = 0f, phase = "", log = emptyList(), hotspot = null) }
        viewModelScope.launch(Dispatchers.IO) {
            monitor?.join()
            val start = System.currentTimeMillis()
            try {
                Flasher(log = ::log).run(
                    open = ::openPort,
                    images = list,
                    mode = mode,
                    phase = { p ->
                        val text = when (p) {
                            Phase.Connecting -> ctx.getString(R.string.phase_connecting)
                            is Phase.Writing -> ctx.getString(R.string.phase_writing, p.name, p.index, p.count)
                            Phase.Restarting -> ctx.getString(R.string.phase_restarting)
                        }
                        _state.update { it.copy(phase = text) }
                    },
                    progress = { done, total -> _state.update { it.copy(progress = done.toFloat() / total) } },
                )
                log("finished in ${(System.currentTimeMillis() - start) / 1000} s")
                _state.update { it.copy(outcome = Outcome.Ok, progress = 1f) }
            } catch (e: Exception) {
                log("error: ${e.message ?: e}")
                _state.update { it.copy(outcome = Outcome.Failed(e.message ?: e.toString())) }
            } finally {
                _state.update { it.copy(busy = false, phase = "") }
            }
        }
    }

    /** Shows what the board prints on USB, after restarting it so the boot lines come too. */
    fun startMonitor() {
        if (_state.value.busy || monitor?.isActive == true) return
        _state.update { it.copy(monitoring = true, log = emptyList(), hotspot = null, outcome = null) }
        monitor = viewModelScope.launch(Dispatchers.IO) {
            var port: UsbSerial? = null
            try {
                var p = openPort()
                port = p
                p.setLines(dtr = false, rts = true) // reset (EN low)
                Thread.sleep(150)
                p.setLines(dtr = false, rts = false)
                val buf = ByteArray(4096)
                val line = StringBuilder()
                while (isActive) {
                    val n = try {
                        p.read(buf, 200)
                    } catch (e: IOException) {
                        // The board's own USB drops off while the chip restarts.
                        p.close()
                        Thread.sleep(800)
                        p = openPort()
                        port = p
                        continue
                    }
                    for (ch in String(buf, 0, n, Charsets.UTF_8)) {
                        if (ch == '\n') {
                            emitBoardLine(line.toString().trimEnd('\r'))
                            line.setLength(0)
                        } else if (line.length < 400) {
                            line.append(ch)
                        }
                    }
                }
            } catch (e: Exception) {
                log("error: ${e.message ?: e}")
            } finally {
                port?.close()
                _state.update { it.copy(monitoring = false) }
            }
        }
    }

    private fun emitBoardLine(text: String) {
        if (text.isBlank()) return
        // Only text: once a host attaches, the port carries the binary host link.
        val clean = text.filter { it == '\t' || it >= ' ' }
        HOTSPOT.find(clean)?.let { m -> _state.update { it.copy(hotspot = m.groupValues[1] to m.groupValues[2]) } }
        log(clean)
    }

    /** Board Wi-Fi over the host link (firmware 0.1.1-beta9+, the board's own USB). */
    fun setWifi(ssid: String, password: String) {
        val args = try {
            HostLink.wifiTlv(ssid.trim(), password, null)
        } catch (e: IllegalArgumentException) {
            _state.update { it.copy(wifiMessage = false to (e.message ?: "")) }
            return
        }
        boardCommand(HostLink.OP_SET_WIFI, args) { ctx.getString(R.string.wifi_saved, ssid.trim()) }
    }

    fun hotspot(on: Boolean) {
        boardCommand(HostLink.OP_HOTSPOT, byteArrayOf(if (on) 1 else 0)) {
            ctx.getString(if (on) R.string.hotspot_opened else R.string.hotspot_normal)
        }
    }

    private fun boardCommand(op: Int, args: ByteArray, okText: () -> String) {
        val st = _state.value
        if (st.busy || st.wifiBusy) return
        stopMonitor()
        _state.update { it.copy(wifiBusy = true, wifiMessage = null) }
        viewModelScope.launch(Dispatchers.IO) {
            monitor?.join()
            var port: UsbSerial? = null
            val result = try {
                val p = openPort()
                port = p
                if (!p.nativeUsb) log("note: this is a USB-serial chip; the firmware's host link is on the board's own USB socket")
                when (val ack = BoardLink(p).command(op, args)) {
                    HostLink.ACK_OK -> true to okText()
                    HostLink.ACK_UNKNOWN_OP -> false to ctx.getString(R.string.ack_old_firmware)
                    HostLink.ACK_BAD_ARGS -> false to ctx.getString(R.string.ack_bad_args)
                    else -> false to "status $ack"
                }
            } catch (e: Exception) {
                false to (e.message ?: e.toString())
            } finally {
                port?.close()
            }
            log((if (result.first) "ok: " else "error: ") + result.second)
            _state.update { it.copy(wifiBusy = false, wifiMessage = result) }
        }
    }

    fun stopMonitor() {
        monitor?.cancel()
    }

    private companion object {
        const val ACTION_PERMISSION = "org.meshmsg.flasher.USB_PERMISSION"
        val HOTSPOT = Regex("hotspot (Mava-[0-9A-Fa-f]{4}), device password: (\\S+)")
    }
}
