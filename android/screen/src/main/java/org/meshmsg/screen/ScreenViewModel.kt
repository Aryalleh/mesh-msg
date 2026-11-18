package org.meshmsg.screen

import android.app.Application
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
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
import org.meshmsg.boardlink.HostLink
import org.meshmsg.boardlink.UsbSerial

data class ScreenUi(
    val device: String? = null,
    val permitted: Boolean = false,
    val connected: Boolean = false,
    val connecting: Boolean = false,
    val error: String? = null,
    val width: Int = 0,
    val height: Int = 0,
    val version: Long = 0,
    val phoneKeyboard: Boolean = true, // hide the board's touch keyboard: type with the phone's
    val picture: Boolean = true, // off: keys and keyboard only (the board's own display shows it)
    val sound: Boolean = false, // the board's speaker on the phone
    val mic: Boolean = false, // the phone's microphone to the board
    val audioError: String? = null,
    val audioIn: Long = 0, // speaker frames played
    val audioOut: Long = 0, // microphone frames sent
)

class ScreenViewModel(app: Application) : AndroidViewModel(app) {
    private val ctx: Context get() = getApplication()
    private val usb = ctx.getSystemService(UsbManager::class.java)
    private val prefs = ctx.getSharedPreferences("screen", Context.MODE_PRIVATE)
    private val _state = MutableStateFlow(
        ScreenUi(
            phoneKeyboard = prefs.getBoolean("phone_keyboard", true),
            picture = prefs.getBoolean("picture", true),
            sound = prefs.getBoolean("sound", false),
            // The microphone needs the permission again after a restart: off until asked.
        ),
    )
    @Volatile private var audio: BoardAudio? = null
    val state: StateFlow<ScreenUi> = _state
    val frame = ScreenFrame()
    @Volatile var session: ScreenSession? = null
        private set
    private var job: Job? = null
    private var autoConnect = true

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(c: Context, intent: Intent) {
            refresh()
            if (intent.action == UsbManager.ACTION_USB_DEVICE_DETACHED) disconnect()
            if (intent.action == ACTION_PERMISSION && intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED, false)) connect()
        }
    }

    init {
        val filter = IntentFilter().apply {
            addAction(ACTION_PERMISSION)
            addAction(UsbManager.ACTION_USB_DEVICE_ATTACHED)
            addAction(UsbManager.ACTION_USB_DEVICE_DETACHED)
        }
        ContextCompat.registerReceiver(ctx, receiver, filter, ContextCompat.RECEIVER_NOT_EXPORTED)
        viewModelScope.launch {
            while (isActive) {
                refresh()
                val s = _state.value
                // Plugged in and allowed: show the screen without a tap.
                if (autoConnect && s.device != null && s.permitted && !s.connected && !s.connecting && s.error == null) connect()
                delay(1000)
            }
        }
        // Redraw at up to 30 frames a second while tiles arrive.
        viewModelScope.launch {
            var seen = -1L
            while (isActive) {
                val v = frame.version
                if (v != seen) {
                    seen = v
                    _state.update { it.copy(width = frame.width, height = frame.height, version = v) }
                }
                delay(33)
            }
        }
        // Audio counters, once a second.
        viewModelScope.launch {
            while (isActive) {
                audio?.let { a -> _state.update { it.copy(audioIn = a.framesIn, audioOut = a.framesOut) } }
                delay(1000)
            }
        }
    }

    override fun onCleared() {
        runCatching { ctx.unregisterReceiver(receiver) }
        disconnect()
    }

    fun refresh() {
        val manager = usb ?: return
        val d = UsbSerial.find(manager)
        _state.update { it.copy(device = d?.let(::describe), permitted = d != null && manager.hasPermission(d)) }
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

    fun connect() {
        val manager = usb ?: return
        if (job?.isActive == true) return
        val d = UsbSerial.find(manager) ?: return
        if (!manager.hasPermission(d)) {
            requestPermission()
            return
        }
        autoConnect = true
        _state.update { it.copy(connecting = true, error = null) }
        job = viewModelScope.launch(Dispatchers.IO) {
            var port: UsbSerial? = null
            try {
                val p = UsbSerial.open(manager, d)
                port = p
                val s = ScreenSession(p, frame, picture = _state.value.picture)
                val a = BoardAudio { args -> s.micFrame(args) }
                s.onAudio = { d -> AudioLink.speaker(d)?.let(a::play) }
                s.onStart = {
                    it.option(OPT_TOUCH_KEYBOARD, !_state.value.phoneKeyboard)
                    applyAudio(it, a)
                }
                audio = a
                session = s
                _state.update { it.copy(connected = true, connecting = false) }
                // Watch for the board's answer while the session runs.
                launch {
                    for (i in 0 until 40) {
                        delay(100)
                        val st = s.started ?: continue
                        if (st != HostLink.ACK_OK) {
                            _state.update {
                                it.copy(error = ctx.getString(if (st == HostLink.ACK_UNKNOWN_OP) R.string.err_no_screen else R.string.err_refused))
                            }
                            s.close()
                        }
                        return@launch
                    }
                    if (s.started == null) _state.update { it.copy(error = ctx.getString(R.string.err_no_answer)) }
                }
                s.run()
            } catch (e: Exception) {
                _state.update { it.copy(error = e.message ?: e.toString()) }
            } finally {
                session = null
                audio?.close()
                audio = null
                port?.close()
                _state.update { it.copy(connected = false, connecting = false) }
            }
        }
    }

    fun disconnect() {
        autoConnect = false
        session?.close()
        job?.cancel()
    }

    fun setPhoneKeyboard(on: Boolean) {
        prefs.edit().putBoolean("phone_keyboard", on).apply()
        _state.update { it.copy(phoneKeyboard = on) }
        session?.option(OPT_TOUCH_KEYBOARD, !on)
    }

    fun setPicture(on: Boolean) {
        prefs.edit().putBoolean("picture", on).apply()
        _state.update { it.copy(picture = on) }
        session?.picture(on)
    }

    fun setSound(on: Boolean) {
        prefs.edit().putBoolean("sound", on).apply()
        _state.update { it.copy(sound = on) }
        reapplyAudio()
    }

    /** Call after RECORD_AUDIO was granted (the activity asks). */
    fun setMic(on: Boolean) {
        _state.update { it.copy(mic = on) }
        reapplyAudio()
    }

    private fun reapplyAudio() {
        val s = session ?: return
        val a = audio ?: return
        viewModelScope.launch(Dispatchers.IO) { applyAudio(s, a) }
    }

    // Tells the board what the phone stands in for, starts the phone's side,
    // and reports a board that does not know AUDIO.
    private fun applyAudio(s: ScreenSession, a: BoardAudio) {
        val st = _state.value
        a.speaker(st.sound)
        a.mic(st.mic)
        _state.update { it.copy(audioError = null) }
        if (!st.sound && !st.mic) {
            s.audio(speaker = false, mic = false)
            return
        }
        s.audio(st.sound, st.mic)
        viewModelScope.launch {
            repeat(30) {
                delay(100)
                val ack = s.audioAck ?: return@repeat
                if (ack != HostLink.ACK_OK) {
                    a.speaker(false)
                    a.mic(false)
                    _state.update { it.copy(audioError = ctx.getString(R.string.err_no_audio)) }
                }
                return@launch
            }
        }
    }

    fun retry() {
        _state.update { it.copy(error = null) }
        connect()
    }

    private companion object {
        const val OPT_TOUCH_KEYBOARD = 1
        const val ACTION_PERMISSION = "org.meshmsg.screen.USB_PERMISSION"
    }
}
