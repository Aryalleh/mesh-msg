package org.meshmsg.app.lora

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.hardware.usb.UsbConstants
import android.hardware.usb.UsbDevice
import android.hardware.usb.UsbDeviceConnection
import android.hardware.usb.UsbEndpoint
import android.hardware.usb.UsbInterface
import android.hardware.usb.UsbManager
import android.os.Build
import androidx.core.content.ContextCompat
import org.meshmsg.meshcore.Core
import org.meshmsg.meshcore.Stream
import org.meshmsg.meshcore.StreamOpener
import java.io.IOException

/**
 * Tier 1: a LoRa node on a USB-OTG cable (docs/android.md §5). Speaks USB
 * CDC-ACM directly — the serial class of boards with native USB (ESP32-S3:
 * T-Beam Supreme; nRF52840: RAK4631, T-Echo) — and hands the byte stream to
 * the core, which runs the same host link meshd uses.
 *
 * Boards behind a USB-UART bridge chip (Heltec V3: CP2102) need that chip's
 * vendor protocol, which is not implemented yet.
 */
class UsbLoRa(private val context: Context) : StreamOpener {
    private val usb = context.getSystemService(UsbManager::class.java)
    private var attached = false

    private class Port(val device: UsbDevice, val comm: UsbInterface?, val data: UsbInterface, val input: UsbEndpoint, val output: UsbEndpoint)

    private fun find(): Port? {
        for (device in usb?.deviceList?.values ?: return null) {
            var comm: UsbInterface? = null
            for (i in 0 until device.interfaceCount) {
                val itf = device.getInterface(i)
                if (itf.interfaceClass == UsbConstants.USB_CLASS_COMM) comm = itf
                if (itf.interfaceClass != UsbConstants.USB_CLASS_CDC_DATA) continue
                var input: UsbEndpoint? = null
                var output: UsbEndpoint? = null
                for (e in 0 until itf.endpointCount) {
                    val ep = itf.getEndpoint(e)
                    if (ep.type != UsbConstants.USB_ENDPOINT_XFER_BULK) continue
                    if (ep.direction == UsbConstants.USB_DIR_IN) input = ep else output = ep
                }
                if (input != null && output != null) return Port(device, comm, itf, input, output)
            }
        }
        return null
    }

    /** Attaches silently if a node is plugged in and already permitted. */
    fun attachIfPresent(core: Core) {
        val port = find() ?: return
        if (usb?.hasPermission(port.device) == true) attach(core)
    }

    /**
     * Attaches the node, asking for USB permission first if needed.
     * Returns false if no node is plugged in.
     */
    fun attachOrRequest(core: Core): Boolean {
        val port = find() ?: return false
        val manager = usb ?: return false
        if (manager.hasPermission(port.device)) {
            attach(core)
            return true
        }
        val receiver = object : BroadcastReceiver() {
            override fun onReceive(c: Context, intent: Intent) {
                context.unregisterReceiver(this)
                if (intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED, false)) attach(core)
            }
        }
        ContextCompat.registerReceiver(context, receiver, IntentFilter(ACTION_PERMISSION), ContextCompat.RECEIVER_NOT_EXPORTED)
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or
            (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0)
        manager.requestPermission(
            port.device,
            PendingIntent.getBroadcast(context, 0, Intent(ACTION_PERMISSION).setPackage(context.packageName), flags),
        )
        return true
    }

    @Synchronized
    private fun attach(core: Core) {
        if (attached) return
        // The core reopens the stream after failures (cable pulled and plugged back).
        attached = runCatching { core.attachLoRa(LINK_NAME, this) }.isSuccess
    }

    /** Called by the Go core, again after every failure. */
    override fun open(): Stream {
        val port = find() ?: throw IOException("no LoRa node on USB")
        val manager = usb ?: throw IOException("no USB host support")
        if (!manager.hasPermission(port.device)) throw IOException("USB permission not granted")
        val conn = manager.openDevice(port.device) ?: throw IOException("cannot open USB device")
        port.comm?.let { conn.claimInterface(it, true) }
        if (!conn.claimInterface(port.data, true)) {
            conn.close()
            throw IOException("cannot claim the USB serial interface")
        }
        val commId = port.comm?.id ?: 0
        // SET_LINE_CODING: 115200 baud, 1 stop bit, no parity, 8 data bits.
        val coding = byteArrayOf(0x00, 0xC2.toByte(), 0x01, 0x00, 0, 0, 8)
        conn.controlTransfer(0x21, 0x20, 0, commId, coding, coding.size, 1000)
        // SET_CONTROL_LINE_STATE: DTR | RTS — the node sees a host attach.
        conn.controlTransfer(0x21, 0x22, 0x03, commId, null, 0, 1000)
        return UsbStream(manager, port, conn)
    }

    private class UsbStream(private val manager: UsbManager, private val port: Port, private val conn: UsbDeviceConnection) : Stream {
        @Volatile private var closed = false
        private val buf = ByteArray(4096)

        override fun read(): ByteArray {
            while (!closed) {
                val n = conn.bulkTransfer(port.input, buf, buf.size, 500)
                if (n > 0) return buf.copyOf(n)
                // A timeout and an unplugged cable both return -1.
                if (n < 0 && port.device.deviceName !in manager.deviceList) throw IOException("USB node unplugged")
            }
            throw IOException("closed")
        }

        override fun write(b: ByteArray) {
            var off = 0
            while (off < b.size) {
                if (closed) throw IOException("closed")
                val n = conn.bulkTransfer(port.output, b, off, b.size - off, 2000)
                if (n <= 0) throw IOException("USB write failed")
                off += n
            }
        }

        override fun close() {
            if (closed) return
            closed = true
            // Drop DTR so the node knows the host left.
            runCatching { conn.controlTransfer(0x21, 0x22, 0x00, port.comm?.id ?: 0, null, 0, 500) }
            runCatching { conn.releaseInterface(port.data) }
            runCatching { conn.close() }
        }
    }

    private companion object {
        const val ACTION_PERMISSION = "org.meshmsg.app.USB_PERMISSION"
        const val LINK_NAME = "usb"
    }
}
