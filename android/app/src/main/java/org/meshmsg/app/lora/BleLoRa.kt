package org.meshmsg.app.lora

import android.annotation.SuppressLint
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.BluetoothStatusCodes
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.Build
import android.os.ParcelUuid
import org.meshmsg.meshcore.Core
import org.meshmsg.meshcore.Stream
import org.meshmsg.meshcore.StreamOpener
import java.io.IOException
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.Semaphore
import java.util.concurrent.TimeUnit

/**
 * The Mava board over Bluetooth LE (docs/board.md §4): the board's host-link
 * GATT service, the same COBS byte stream as USB (UsbLoRa) on the board's
 * host port 1. The phone writes to RX and gets TX notifications. No pairing
 * (from 0.1.1-beta14): over Bluetooth the board takes packets and status
 * requests only. The board also joins the phones' own Bluetooth mesh
 * (BleMesh), which needs no button at all; this link adds the node's status.
 *
 * The core calls [open] again after every failure, so a board that goes out
 * of range or restarts is picked up again by itself.
 */
@SuppressLint("MissingPermission") // callers check MeshApp.bluetoothAllowed() first
class BleLoRa(private val context: Context, private var address: String?) : StreamOpener {
    private val manager = context.getSystemService(BluetoothManager::class.java)
    private var attached = false

    /** The remembered board, or null before the first connection. */
    val boardAddress: String? get() = address

    /**
     * Scans up to [timeoutMs] for a Mava board advertising the host-link
     * service; returns its address (the nearest one: strongest signal).
     */
    fun scan(timeoutMs: Long = 12_000): String? {
        val scanner = manager?.adapter?.bluetoothLeScanner ?: return null
        val found = mutableMapOf<String, Int>()
        val first = CountDownLatch(1)
        val cb = object : ScanCallback() {
            override fun onScanResult(type: Int, r: ScanResult) {
                synchronized(found) { found[r.device.address] = maxOf(found[r.device.address] ?: -999, r.rssi) }
                first.countDown()
            }
        }
        val filter = ScanFilter.Builder().setServiceUuid(ParcelUuid(SERVICE)).build()
        val settings = ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY).build()
        scanner.startScan(listOf(filter), settings, cb)
        try {
            // After the first board, listen a little longer for a nearer one.
            if (first.await(timeoutMs, TimeUnit.MILLISECONDS)) Thread.sleep(1_500)
        } finally {
            runCatching { scanner.stopScan(cb) }
        }
        return synchronized(found) { found.maxByOrNull { it.value }?.key }
    }

    /** Remembers [addr] and attaches it to the core (the core keeps it connected). */
    @Synchronized
    fun attach(core: Core, addr: String) {
        address = addr
        if (attached) return
        attached = runCatching { core.attachLoRa(LINK_NAME, this) }.isSuccess
    }

    override fun open(): Stream {
        val addr = address ?: throw IOException("no Mava board chosen")
        val adapter = manager?.adapter ?: throw IOException("no Bluetooth")
        if (!adapter.isEnabled) throw IOException("Bluetooth is off")
        val device = adapter.getRemoteDevice(addr)
        val link = Link(device)
        try {
            link.connect()
            return link
        } catch (e: Exception) {
            link.close()
            throw if (e is IOException) e else IOException(e.message ?: e.toString())
        }
    }

    private inner class Link(private val device: BluetoothDevice) : Stream, BluetoothGattCallback() {
        private var gatt: BluetoothGatt? = null
        private var rx: BluetoothGattCharacteristic? = null
        private val incoming = LinkedBlockingQueue<ByteArray>()
        @Volatile private var closed = false
        @Volatile private var mtu = 23
        private val step = Semaphore(0) // one GATT operation at a time
        @Volatile private var stepOk = false
        private val ready = CountDownLatch(1)
        @Volatile private var failure: String? = null

        fun connect() {
            gatt = device.connectGatt(context, false, this, BluetoothDevice.TRANSPORT_LE)
                ?: throw IOException("cannot connect to the board")
            if (!ready.await(20, TimeUnit.SECONDS)) throw IOException("the board did not answer over Bluetooth")
            failure?.let { throw IOException(it) }
            val g = gatt ?: throw IOException("closed")
            g.requestMtu(247)
            await("MTU", 5)
            if (!g.discoverServices()) throw IOException("service discovery failed")
            await("services", 10)
            val svc = g.getService(SERVICE) ?: throw IOException("not a Mava board (no host-link service)")
            rx = svc.getCharacteristic(RX) ?: throw IOException("board has no RX characteristic")
            val tx = svc.getCharacteristic(TX) ?: throw IOException("board has no TX characteristic")
            if (!g.setCharacteristicNotification(tx, true)) throw IOException("cannot subscribe to the board")
            val cccd = tx.getDescriptor(CCCD) ?: throw IOException("board has no CCCD")
            writeDescriptor(g, cccd, BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE)
            await("subscribe", 10)
        }

        private fun await(what: String, seconds: Long) {
            if (!step.tryAcquire(seconds, TimeUnit.SECONDS)) throw IOException("Bluetooth: $what timed out")
            if (closed) throw IOException("the board disconnected")
            if (!stepOk) throw IOException("Bluetooth: $what failed")
        }

        @Suppress("DEPRECATION")
        private fun writeDescriptor(g: BluetoothGatt, d: BluetoothGattDescriptor, v: ByteArray) {
            val ok = if (Build.VERSION.SDK_INT >= 33) {
                g.writeDescriptor(d, v) == BluetoothStatusCodes.SUCCESS
            } else {
                d.value = v
                g.writeDescriptor(d)
            }
            if (!ok) throw IOException("cannot write to the board")
        }

        // ---- GATT callbacks (binder threads) ----

        override fun onConnectionStateChange(g: BluetoothGatt, status: Int, state: Int) {
            if (state == BluetoothProfile.STATE_CONNECTED && status == BluetoothGatt.GATT_SUCCESS) {
                ready.countDown()
            } else {
                if (ready.count > 0) failure = "cannot connect to the board (status $status)"
                closed = true
                ready.countDown()
                step.release()
            }
        }

        override fun onMtuChanged(g: BluetoothGatt, m: Int, status: Int) {
            if (status == BluetoothGatt.GATT_SUCCESS) mtu = m
            stepOk = true // a refused MTU still works, in 20-byte pieces
            step.release()
        }

        override fun onServicesDiscovered(g: BluetoothGatt, status: Int) {
            stepOk = status == BluetoothGatt.GATT_SUCCESS
            step.release()
        }

        override fun onDescriptorWrite(g: BluetoothGatt, d: BluetoothGattDescriptor, status: Int) {
            stepOk = status == BluetoothGatt.GATT_SUCCESS
            step.release()
        }

        override fun onCharacteristicWrite(g: BluetoothGatt, c: BluetoothGattCharacteristic, status: Int) {
            stepOk = status == BluetoothGatt.GATT_SUCCESS
            step.release()
        }

        override fun onCharacteristicChanged(g: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray) {
            if (c.uuid == TX && value.isNotEmpty()) incoming.put(value.copyOf())
        }

        @Deprecated("Android 12 and older")
        @Suppress("DEPRECATION")
        override fun onCharacteristicChanged(g: BluetoothGatt, c: BluetoothGattCharacteristic) {
            if (Build.VERSION.SDK_INT < 33 && c.uuid == TX) c.value?.takeIf { it.isNotEmpty() }?.let { incoming.put(it.copyOf()) }
        }

        // ---- Stream (the core's threads) ----

        override fun read(): ByteArray {
            while (!closed) {
                incoming.poll(500, TimeUnit.MILLISECONDS)?.let { return it }
            }
            throw IOException("the board disconnected")
        }

        @Suppress("DEPRECATION")
        override fun write(b: ByteArray) {
            val g = gatt ?: throw IOException("closed")
            val c = rx ?: throw IOException("closed")
            val chunk = (mtu - 3).coerceIn(20, 244)
            var off = 0
            synchronized(this) {
                while (off < b.size) {
                    if (closed) throw IOException("the board disconnected")
                    val piece = b.copyOfRange(off, minOf(b.size, off + chunk))
                    val ok = if (Build.VERSION.SDK_INT >= 33) {
                        g.writeCharacteristic(c, piece, BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT) == BluetoothStatusCodes.SUCCESS
                    } else {
                        c.writeType = BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
                        c.value = piece
                        g.writeCharacteristic(c)
                    }
                    if (!ok) throw IOException("Bluetooth write refused")
                    await("write", 10)
                    off += piece.size
                }
            }
        }

        override fun close() {
            closed = true
            step.release()
            runCatching { gatt?.disconnect() }
            runCatching { gatt?.close() }
            gatt = null
        }
    }

    companion object {
        const val LINK_NAME = "ble"
        // firmware/src/ble.cpp: "mesh" "lr" in the first bytes, Bluetooth base UUID tail.
        val SERVICE: UUID = UUID.fromString("6d657368-6c72-0000-8000-00805f9b34fb")
        val RX: UUID = UUID.fromString("6d657368-6c72-0001-8000-00805f9b34fb")
        val TX: UUID = UUID.fromString("6d657368-6c72-0002-8000-00805f9b34fb")
        val CCCD: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")
    }
}
