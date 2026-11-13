package org.meshmsg.app.data

import android.media.AudioDeviceCallback
import android.media.AudioDeviceInfo
import android.media.AudioManager
import android.os.Build
import android.os.Handler
import android.os.Looper
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/** Where call audio plays. */
enum class RouteKind { Earpiece, Speaker, Wired, Bluetooth }

data class AudioRoute(val kind: RouteKind, val name: String = "")

/**
 * Call audio routing: phone earpiece, speaker, wired headset or a Bluetooth
 * headset. A Bluetooth headset is used as soon as it is there (at the start
 * of the call or when it connects during one), like other call apps; the
 * user can switch with the route button. Android 12+ uses
 * setCommunicationDevice; older phones use Bluetooth SCO.
 */
class AudioRoutes(private val am: AudioManager) {
    private val _routes = MutableStateFlow(listOf(AudioRoute(RouteKind.Earpiece)))
    private val _current = MutableStateFlow(AudioRoute(RouteKind.Earpiece))
    val routes: StateFlow<List<AudioRoute>> = _routes.asStateFlow()
    val current: StateFlow<AudioRoute> = _current.asStateFlow()
    private val main = Handler(Looper.getMainLooper())

    private val callback = object : AudioDeviceCallback() {
        override fun onAudioDevicesAdded(added: Array<out AudioDeviceInfo>) {
            refresh()
            // A headset that connects during the call takes the audio.
            val headset = added.firstOrNull { kindOf(it.type) == RouteKind.Bluetooth } ?: added.firstOrNull { kindOf(it.type) == RouteKind.Wired }
            if (headset != null) select(AudioRoute(kindOf(headset.type)!!, name(headset)))
        }

        override fun onAudioDevicesRemoved(removed: Array<out AudioDeviceInfo>) {
            refresh()
            if (_routes.value.none { it.kind == _current.value.kind }) select(best())
        }
    }

    fun start() {
        refresh()
        am.registerAudioDeviceCallback(callback, main)
        select(best())
    }

    fun stop() {
        am.unregisterAudioDeviceCallback(callback)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            am.clearCommunicationDevice()
        } else {
            @Suppress("DEPRECATION")
            run {
                if (am.isBluetoothScoOn) { am.stopBluetoothSco(); am.isBluetoothScoOn = false }
                am.isSpeakerphoneOn = false
            }
        }
    }

    /** Bluetooth headset, then wired, then earpiece. */
    private fun best(): AudioRoute = _routes.value.let { rs ->
        rs.firstOrNull { it.kind == RouteKind.Bluetooth } ?: rs.firstOrNull { it.kind == RouteKind.Wired } ?: AudioRoute(RouteKind.Earpiece)
    }

    fun select(route: AudioRoute) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            val dev = am.availableCommunicationDevices.firstOrNull { d ->
                kindOf(d.type) == route.kind && (route.name.isEmpty() || name(d) == route.name)
            } ?: am.availableCommunicationDevices.firstOrNull { kindOf(it.type) == route.kind }
            if (dev == null || !runCatching { am.setCommunicationDevice(dev) }.getOrDefault(false)) return
        } else {
            @Suppress("DEPRECATION")
            when (route.kind) {
                RouteKind.Bluetooth -> {
                    am.isSpeakerphoneOn = false
                    runCatching { am.startBluetoothSco(); am.isBluetoothScoOn = true }
                }
                RouteKind.Speaker -> {
                    if (am.isBluetoothScoOn) { am.stopBluetoothSco(); am.isBluetoothScoOn = false }
                    am.isSpeakerphoneOn = true
                }
                else -> {
                    if (am.isBluetoothScoOn) { am.stopBluetoothSco(); am.isBluetoothScoOn = false }
                    am.isSpeakerphoneOn = false
                }
            }
        }
        _current.value = route
    }

    private fun refresh() {
        val devs: List<AudioDeviceInfo> = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            am.availableCommunicationDevices
        } else {
            am.getDevices(AudioManager.GET_DEVICES_OUTPUTS).toList()
        }
        val rs = mutableListOf<AudioRoute>()
        val wired = devs.firstOrNull { kindOf(it.type) == RouteKind.Wired }
        // A wired headset replaces the earpiece (the phone can't use both).
        rs += if (wired != null) AudioRoute(RouteKind.Wired, name(wired)) else AudioRoute(RouteKind.Earpiece)
        rs += AudioRoute(RouteKind.Speaker)
        devs.filter { kindOf(it.type) == RouteKind.Bluetooth }.map { AudioRoute(RouteKind.Bluetooth, name(it)) }.distinct().forEach { rs += it }
        _routes.value = rs
    }

    companion object {
        fun kindOf(type: Int): RouteKind? = when (type) {
            AudioDeviceInfo.TYPE_BUILTIN_EARPIECE -> RouteKind.Earpiece
            AudioDeviceInfo.TYPE_BUILTIN_SPEAKER -> RouteKind.Speaker
            AudioDeviceInfo.TYPE_WIRED_HEADSET, AudioDeviceInfo.TYPE_WIRED_HEADPHONES, AudioDeviceInfo.TYPE_USB_HEADSET -> RouteKind.Wired
            AudioDeviceInfo.TYPE_BLUETOOTH_SCO -> RouteKind.Bluetooth
            else -> if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S && (type == AudioDeviceInfo.TYPE_BLE_HEADSET)) RouteKind.Bluetooth else null
        }

        private fun name(d: AudioDeviceInfo): String = d.productName?.toString().orEmpty()
    }
}
