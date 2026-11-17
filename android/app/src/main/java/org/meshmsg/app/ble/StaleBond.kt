package org.meshmsg.app.ble

import android.annotation.SuppressLint
import android.bluetooth.BluetoothDevice
import android.util.Log

/**
 * Mesh peers and Mava boards never pair. A phone that paired with a board in
 * the pairing-code builds (0.1.1-beta10 to beta13) still keeps that bond, and
 * on every connection it tries to encrypt with keys the board no longer has:
 * the link drops at once and the Bluetooth mesh never forms. Forget such a
 * bond before connecting. Android 13 and later refuse this for apps (the
 * system keeps it to itself); there the board's new address (beta20 firmware)
 * avoids the old bond, and the user can tap Forget in the Bluetooth settings.
 */
object StaleBond {
    @SuppressLint("MissingPermission", "DiscouragedPrivateApi", "PrivateApi")
    fun forget(device: BluetoothDevice) {
        val bonded = runCatching { device.bondState == BluetoothDevice.BOND_BONDED }.getOrDefault(false)
        if (!bonded) return
        runCatching { device.javaClass.getMethod("removeBond").invoke(device) }
            .onFailure { Log.i("StaleBond", "cannot forget an old pairing: ${it.message}") }
    }
}
