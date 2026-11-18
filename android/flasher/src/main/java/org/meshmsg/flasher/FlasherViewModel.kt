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
import org.meshmsg.boardlink.BoardLink
import org.meshmsg.boardlink.HostLink
import org.meshmsg.boardlink.UsbSerial
import org.meshmsg.flasher.update.RemoteFirmware
import org.meshmsg.flasher.update.Versions
import java.io.IOException
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

data class UiState(
    val usbHost: Boolean = true,
    val imagesVersion: String? = null,
    val imagesFrom: String = "", // "github" (downloaded) or "apk" (bundled)
    val fwChecking: Boolean = false,
    val fwProgress: Float = 0f,
    val fwMessage: Pair<Boolean, String>? = null, // ok?, text
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
    val radioMessage: Pair<Boolean, String>? = null, // ok?, text
    val log: List<String> = emptyList(),
    /** The images carry a second firmware without the phone simulators (variants.txt). */
    val hasPlain: Boolean = false,
    /** Write that one: faster, for a board with its own display and keys. */
    val plain: Boolean = false,
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
    private val prefs = ctx.getSharedPreferences("flasher", Context.MODE_PRIVATE)
    // Where the current images come from: a file by name, null if missing.
    @Volatile private var source: (String) -> ByteArray? = ::readAsset
    @Volatile private var images: List<BoardImage>? = loadImages()
    private val remote = RemoteFirmware(java.io.File(ctx.filesDir, "board"))
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
                imagesFrom = "apk",
                hasPlain = readAsset(PLAIN) != null,
                plain = prefs.getBoolean("plain", false),
            )
        }
        useDownloaded()
        checkFirmware()
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

    /** A firmware downloaded earlier and newer than the APK's: use it (works offline). */
    private fun useDownloaded() {
        val dir = java.io.File(ctx.filesDir, "board")
        val newest = dir.listFiles()?.filter { java.io.File(it, "images.txt").isFile }
            ?.maxWithOrNull { a, b -> Versions.compare(a.name, b.name) } ?: return
        val cur = _state.value.imagesVersion
        if (cur != null && Versions.compare(newest.name, cur) <= 0) return
        loadFrom(newest)?.let { list ->
            images = list
            source = fileSource(newest)
            _state.update { it.copy(imagesVersion = newest.name, imagesFrom = "github", hasPlain = java.io.File(newest, PLAIN).isFile) }
        }
    }

    private fun fileSource(dir: java.io.File): (String) -> ByteArray? =
        { name -> java.io.File(dir, name).takeIf { it.isFile }?.readBytes() }

    /** With a firmware without the phone simulators or not (if the images carry one). */
    fun setPlain(on: Boolean) {
        prefs.edit().putBoolean("plain", on).apply()
        _state.update { it.copy(plain = on) }
    }

    private fun loadFrom(dir: java.io.File): List<BoardImage>? = runCatching {
        BoardImages.parse(java.io.File(dir, "images.txt").readText()) { name ->
            val f = java.io.File(dir, name)
            if (!f.isFile) throw IOException("missing $name")
            f.readBytes()
        }
    }.getOrNull()

    /**
     * Looks for a newer firmware on GitHub (signed release only) and downloads
     * it: a new firmware needs no new flasher APK.
     */
    fun checkFirmware() {
        if (_state.value.fwChecking) return
        _state.update { it.copy(fwChecking = true, fwProgress = 0f, fwMessage = null) }
        viewModelScope.launch(Dispatchers.IO) {
            try {
                val r = remote.latest()
                val cur = _state.value.imagesVersion
                if (cur != null && Versions.compare(r.version, cur) <= 0) {
                    _state.update { it.copy(fwMessage = true to ctx.getString(R.string.fw_newest, cur)) }
                    return@launch
                }
                val dir = remote.fetch(r) { p -> _state.update { it.copy(fwProgress = p) } }
                val list = loadFrom(dir) ?: throw IOException("downloaded firmware is incomplete")
                if (!_state.value.busy) {
                    images = list
                    source = fileSource(dir)
                }
                _state.update {
                    it.copy(
                        imagesVersion = r.version, imagesFrom = "github", hasPlain = java.io.File(dir, PLAIN).isFile,
                        fwMessage = true to ctx.getString(R.string.fw_downloaded, r.version),
                    )
                }
            } catch (e: Exception) {
                _state.update { it.copy(fwMessage = false to ctx.getString(R.string.fw_failed, e.message ?: e.toString())) }
            } finally {
                _state.update { it.copy(fwChecking = false) }
            }
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
        val all = images ?: return
        // The firmware without the simulators in place of firmware.bin, when chosen.
        val src = source
        val list = if (_state.value.plain && _state.value.hasPlain) {
            all.map { im ->
                if (im.part != BoardImage.Part.App) im
                else BoardImage(im.offset, PLAIN, im.part) { src(PLAIN) ?: throw IOException("missing $PLAIN") }
            }
        } else all
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

    /**
     * The LoRa module setting (firmware 0.1.1-beta29+): without a module the
     * board skips the radio (no 10–20 s probe at boot that holds Bluetooth up).
     */
    fun setRadio(fitted: Boolean) {
        boardCommand(HostLink.OP_SET_CONFIG, HostLink.radioTlv(fitted), radio = true) {
            ctx.getString(if (fitted) R.string.radio_saved_on else R.string.radio_saved_off)
        }
    }

    fun hotspot(on: Boolean) {
        boardCommand(HostLink.OP_HOTSPOT, byteArrayOf(if (on) 1 else 0)) {
            ctx.getString(if (on) R.string.hotspot_opened else R.string.hotspot_normal)
        }
    }

    private fun boardCommand(op: Int, args: ByteArray, radio: Boolean = false, okText: () -> String) {
        val st = _state.value
        if (st.busy || st.wifiBusy) return
        stopMonitor()
        _state.update { if (radio) it.copy(wifiBusy = true, radioMessage = null) else it.copy(wifiBusy = true, wifiMessage = null) }
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
            _state.update { if (radio) it.copy(wifiBusy = false, radioMessage = result) else it.copy(wifiBusy = false, wifiMessage = result) }
        }
    }

    fun stopMonitor() {
        monitor?.cancel()
    }

    private companion object {
        /** The firmware without the phone simulators (scripts/build-flasher.sh). */
        const val PLAIN = "firmware-plain.bin"
        const val ACTION_PERMISSION = "org.meshmsg.flasher.USB_PERMISSION"
        val HOTSPOT = Regex("hotspot (Mava-[0-9A-Fa-f]{4}), device password: (\\S+)")
    }
}
