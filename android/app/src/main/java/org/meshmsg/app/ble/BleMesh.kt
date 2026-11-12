package org.meshmsg.app.ble

import android.annotation.SuppressLint
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothGattServer
import android.bluetooth.BluetoothGattServerCallback
import android.bluetooth.BluetoothGattService
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.BluetoothStatusCodes
import android.bluetooth.le.AdvertiseCallback
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertiseSettings
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.ParcelUuid
import android.util.Log
import androidx.core.os.HandlerCompat
import org.meshmsg.meshcore.Core
import org.meshmsg.meshcore.Transport
import java.security.SecureRandom

/**
 * Tier 0: the BLE mesh with nearby phones (protocol in [BleProto]). It only
 * moves opaque chunks between GATT and the Go core; the core decides what to
 * send to whom.
 *
 * All state lives on one handler thread: Android delivers GATT callbacks on
 * binder threads and the core calls [send] from Go threads, so everything is
 * posted to [handler] and nothing here needs a lock.
 *
 * Permissions are checked by the caller ([org.meshmsg.app.MeshApp]).
 */
@SuppressLint("MissingPermission")
class BleMesh(
    private val context: Context,
    private val core: Core,
    private val saver: Boolean,
) : Transport {
    private val thread = HandlerThread("ble-mesh").apply { start() }
    private val handler = Handler(thread.looper)
    private val session = ByteArray(BleProto.SESSION_BYTES).also { SecureRandom().nextBytes(it) }

    private val manager = context.getSystemService(BluetoothManager::class.java)
    private val adapter = manager?.adapter
    private var server: BluetoothGattServer? = null
    private var txChar: BluetoothGattCharacteristic? = null
    private var running = false
    private var scanning = false

    private abstract inner class Peer(val id: String) {
        val queue = ArrayDeque<ByteArray>()
        var queued = 0
        var busy = false
        var mtu = BleProto.DEFAULT_MTU
        var up = false
        abstract fun write(b: ByteArray): Boolean
        abstract fun close()
    }

    private val peers = HashMap<String, Peer>() // by session ID
    private val serverPeers = HashMap<String, Peer>() // by device address
    private val serverMtu = HashMap<String, Int>()
    private val connecting = HashSet<String>() // session IDs with a connection attempt running

    // ---- lifecycle ----

    /** Returns false if Bluetooth is missing or off (nothing is started). */
    fun start(): Boolean {
        val a = adapter ?: return false
        if (!a.isEnabled || !context.packageManager.hasSystemFeature("android.hardware.bluetooth_le")) return false
        runCatching { core.addTransport(BleProto.LINK_NAME, BleProto.CORE_CHUNK.toLong(), this) }.onFailure { return false }
        handler.post {
            running = true
            openServer()
            advertise()
            scan(true)
        }
        return true
    }

    fun stop() {
        handler.post {
            running = false
            scan(false)
            runCatching { adapter?.bluetoothLeAdvertiser?.stopAdvertising(advertiseCallback) }
            peers.values.toList().forEach { drop(it) }
            runCatching { server?.close() }
            server = null
            runCatching { core.removeTransport(BleProto.LINK_NAME) }
            thread.quitSafely()
        }
    }

    // ---- discovery ----

    private val advertiseCallback = object : AdvertiseCallback() {
        override fun onStartFailure(errorCode: Int) {
            Log.w(TAG, "advertising failed: $errorCode")
        }
    }

    private fun advertise() {
        val settings = AdvertiseSettings.Builder()
            .setAdvertiseMode(if (saver) AdvertiseSettings.ADVERTISE_MODE_LOW_POWER else AdvertiseSettings.ADVERTISE_MODE_BALANCED)
            .setTxPowerLevel(AdvertiseSettings.ADVERTISE_TX_POWER_MEDIUM)
            .setConnectable(true)
            .build()
        // 3 (flags) + 18 (128-bit UUID) + 8 (manufacturer data) = 29 of 31 bytes; no device name.
        val data = AdvertiseData.Builder()
            .setIncludeDeviceName(false)
            .addServiceUuid(ParcelUuid(BleProto.SERVICE))
            .addManufacturerData(BleProto.MANUFACTURER_ID, session)
            .build()
        runCatching { adapter?.bluetoothLeAdvertiser?.startAdvertising(settings, data, advertiseCallback) }
    }

    private val scanCallback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) {
            val theirs = result.scanRecord?.getManufacturerSpecificData(BleProto.MANUFACTURER_ID) ?: return
            val device = result.device
            handler.post { found(device, theirs) }
        }

        override fun onScanFailed(errorCode: Int) {
            Log.w(TAG, "scan failed: $errorCode")
        }
    }

    /** Saver mode scans [SAVER_SCAN_MS] out of every [SAVER_PERIOD_MS]. */
    private fun scan(on: Boolean) {
        handler.removeCallbacksAndMessages(SCAN_TOKEN)
        val scanner = adapter?.bluetoothLeScanner ?: return
        if (on && running) {
            val filter = ScanFilter.Builder().setServiceUuid(ParcelUuid(BleProto.SERVICE)).build()
            val settings = ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_POWER).build()
            if (!scanning) scanning = runCatching { scanner.startScan(listOf(filter), settings, scanCallback) }.isSuccess
            if (saver) HandlerCompat.postDelayed(handler, { scan(false) }, SCAN_TOKEN, SAVER_SCAN_MS)
        } else {
            if (scanning) runCatching { scanner.stopScan(scanCallback) }
            scanning = false
            if (saver && running) HandlerCompat.postDelayed(handler, { scan(true) }, SCAN_TOKEN, SAVER_PERIOD_MS - SAVER_SCAN_MS)
        }
    }

    private fun found(device: BluetoothDevice, theirs: ByteArray) {
        if (!running || !BleProto.shouldConnect(session, theirs)) return
        val id = BleProto.peerId(theirs)
        if (id in peers || id in connecting || peers.size + connecting.size >= BleProto.MAX_PEERS) return
        connecting += id
        val cb = Client(id)
        val gatt = runCatching { device.connectGatt(context, false, cb, BluetoothDevice.TRANSPORT_LE) }.getOrNull()
        if (gatt == null) connecting -= id
        // Give up on attempts that never complete (peer walked away).
        handler.postDelayed({ if (id in connecting) cb.fail(gatt) }, CONNECT_TIMEOUT_MS)
    }

    // ---- shared peer handling ----

    private fun peerUp(p: Peer) {
        peers[p.id] = p
        p.up = true
        runCatching { core.peerUp(BleProto.LINK_NAME, p.id) }
    }

    private fun drop(p: Peer) {
        if (peers.remove(p.id) === p && p.up) runCatching { core.peerDown(BleProto.LINK_NAME, p.id) }
        p.up = false
        serverPeers.values.remove(p)
        p.queue.clear()
        p.close()
    }

    private fun received(p: Peer, value: ByteArray) {
        // An error means the chunk stream is corrupt: reconnect from scratch.
        runCatching { core.receive(BleProto.LINK_NAME, p.id, value) }.onFailure { drop(p) }
    }

    private fun pump(p: Peer) {
        if (p.busy) return
        val next = p.queue.removeFirstOrNull() ?: return
        p.queued -= next.size
        p.busy = true
        if (!p.write(next)) drop(p)
    }

    /** Called by the Go core (any thread). */
    override fun send(peer: String, chunk: ByteArray) {
        val copy = chunk.copyOf()
        handler.post {
            val p = peers[peer] ?: return@post
            for (part in BleProto.split(copy, p.mtu)) {
                p.queue.addLast(part)
                p.queued += part.size
            }
            if (p.queued > BleProto.MAX_QUEUED_BYTES) drop(p) else pump(p)
        }
    }

    // ---- GATT client: we connected to them ----

    private inner class Client(val id: String) : BluetoothGattCallback() {
        private var peer: Peer? = null
        private var rx: BluetoothGattCharacteristic? = null
        private var helloSent = false

        fun fail(gatt: BluetoothGatt?) {
            connecting -= id
            peer?.let { drop(it) } ?: runCatching { gatt?.close() }
        }

        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            handler.post {
                if (status == BluetoothGatt.GATT_SUCCESS && newState == BluetoothProfile.STATE_CONNECTED && running) {
                    if (!gatt.requestMtu(BleProto.WANTED_MTU)) gatt.discoverServices()
                } else {
                    fail(gatt)
                }
            }
        }

        override fun onMtuChanged(gatt: BluetoothGatt, mtu: Int, status: Int) {
            handler.post {
                if (status == BluetoothGatt.GATT_SUCCESS) this.mtu = mtu
                gatt.discoverServices()
            }
        }

        private var mtu = BleProto.DEFAULT_MTU

        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            handler.post {
                val service = gatt.getService(BleProto.SERVICE)
                val rxC = service?.getCharacteristic(BleProto.RX)
                val txC = service?.getCharacteristic(BleProto.TX)
                val cccd = txC?.getDescriptor(BleProto.CCCD)
                if (status != BluetoothGatt.GATT_SUCCESS || rxC == null || txC == null || cccd == null) return@post fail(gatt)
                rx = rxC
                gatt.setCharacteristicNotification(txC, true)
                val ok = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    gatt.writeDescriptor(cccd, BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE) == BluetoothStatusCodes.SUCCESS
                } else {
                    @Suppress("DEPRECATION")
                    cccd.value = BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
                    @Suppress("DEPRECATION")
                    gatt.writeDescriptor(cccd)
                }
                if (!ok) fail(gatt)
            }
        }

        override fun onDescriptorWrite(gatt: BluetoothGatt, descriptor: BluetoothGattDescriptor, status: Int) {
            handler.post {
                if (status != BluetoothGatt.GATT_SUCCESS) return@post fail(gatt)
                helloSent = true
                if (!write(gatt, BleProto.hello(session))) fail(gatt)
            }
        }

        override fun onCharacteristicWrite(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic, status: Int) {
            handler.post {
                if (status != BluetoothGatt.GATT_SUCCESS) return@post fail(gatt)
                val p = peer
                if (p == null && helloSent) {
                    // The hello was accepted: the link is up.
                    connecting -= id
                    if (id in peers) return@post runCatching { gatt.close() }.let { }
                    val np = object : Peer(id) {
                        override fun write(b: ByteArray) = this@Client.write(gatt, b)
                        override fun close() {
                            runCatching { gatt.disconnect() }
                            runCatching { gatt.close() }
                        }
                    }
                    np.mtu = mtu
                    peer = np
                    peerUp(np)
                } else if (p != null) {
                    p.busy = false
                    pump(p)
                }
            }
        }

        private fun write(gatt: BluetoothGatt, b: ByteArray): Boolean {
            val c = rx ?: return false
            return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                gatt.writeCharacteristic(c, b, BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT) == BluetoothStatusCodes.SUCCESS
            } else {
                @Suppress("DEPRECATION")
                c.value = b
                c.writeType = BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
                @Suppress("DEPRECATION")
                gatt.writeCharacteristic(c)
            }
        }

        // Android 13+
        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic, value: ByteArray) {
            val copy = value.copyOf()
            handler.post { peer?.let { received(it, copy) } }
        }

        @Deprecated("Android 12 and older")
        @Suppress("DEPRECATION")
        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) return
            val copy = characteristic.value?.copyOf() ?: return
            handler.post { peer?.let { received(it, copy) } }
        }
    }

    // ---- GATT server: they connected to us ----

    private fun openServer() {
        val s = runCatching { manager?.openGattServer(context, serverCallback) }.getOrNull() ?: return
        val service = BluetoothGattService(BleProto.SERVICE, BluetoothGattService.SERVICE_TYPE_PRIMARY)
        val rx = BluetoothGattCharacteristic(BleProto.RX, BluetoothGattCharacteristic.PROPERTY_WRITE, BluetoothGattCharacteristic.PERMISSION_WRITE)
        val tx = BluetoothGattCharacteristic(BleProto.TX, BluetoothGattCharacteristic.PROPERTY_NOTIFY, BluetoothGattCharacteristic.PERMISSION_READ)
        tx.addDescriptor(BluetoothGattDescriptor(BleProto.CCCD, BluetoothGattDescriptor.PERMISSION_READ or BluetoothGattDescriptor.PERMISSION_WRITE))
        service.addCharacteristic(rx)
        service.addCharacteristic(tx)
        s.addService(service)
        txChar = tx
        server = s
    }

    private val serverCallback = object : BluetoothGattServerCallback() {
        // Also fires for connections we made as a client; only disconnects of
        // peers that said hello matter here.
        override fun onConnectionStateChange(device: BluetoothDevice, status: Int, newState: Int) {
            if (newState != BluetoothProfile.STATE_DISCONNECTED) return
            handler.post {
                serverMtu.remove(device.address)
                serverPeers[device.address]?.let { drop(it) }
            }
        }

        override fun onMtuChanged(device: BluetoothDevice, mtu: Int) {
            handler.post {
                serverMtu[device.address] = mtu
                serverPeers[device.address]?.mtu = mtu
            }
        }

        override fun onDescriptorWriteRequest(
            device: BluetoothDevice, requestId: Int, descriptor: BluetoothGattDescriptor,
            preparedWrite: Boolean, responseNeeded: Boolean, offset: Int, value: ByteArray?,
        ) {
            handler.post {
                if (responseNeeded) runCatching { server?.sendResponse(device, requestId, BluetoothGatt.GATT_SUCCESS, 0, null) }
            }
        }

        override fun onCharacteristicWriteRequest(
            device: BluetoothDevice, requestId: Int, characteristic: BluetoothGattCharacteristic,
            preparedWrite: Boolean, responseNeeded: Boolean, offset: Int, value: ByteArray?,
        ) {
            val copy = value?.copyOf() ?: ByteArray(0)
            handler.post {
                var status = BluetoothGatt.GATT_SUCCESS
                if (characteristic.uuid != BleProto.RX || preparedWrite || offset != 0) {
                    status = BluetoothGatt.GATT_REQUEST_NOT_SUPPORTED
                } else {
                    val known = serverPeers[device.address]
                    if (known != null) {
                        received(known, copy)
                    } else if (!hello(device, copy)) {
                        status = BluetoothGatt.GATT_WRITE_NOT_PERMITTED
                    }
                }
                if (responseNeeded) runCatching { server?.sendResponse(device, requestId, status, 0, null) }
            }
        }

        override fun onNotificationSent(device: BluetoothDevice, status: Int) {
            handler.post {
                val p = serverPeers[device.address] ?: return@post
                if (status != BluetoothGatt.GATT_SUCCESS) return@post drop(p)
                p.busy = false
                pump(p)
            }
        }
    }

    /** The first write from a new client: its session ID. */
    private fun hello(device: BluetoothDevice, value: ByteArray): Boolean {
        val theirs = BleProto.parseHello(value) ?: return false
        val id = BleProto.peerId(theirs)
        if (!running || id in peers || peers.size >= BleProto.MAX_PEERS) return false
        val p = object : Peer(id) {
            override fun write(b: ByteArray): Boolean {
                val s = server ?: return false
                val c = txChar ?: return false
                return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                    s.notifyCharacteristicChanged(device, c, false, b) == BluetoothStatusCodes.SUCCESS
                } else {
                    @Suppress("DEPRECATION")
                    c.value = b
                    @Suppress("DEPRECATION")
                    s.notifyCharacteristicChanged(device, c, false)
                }
            }

            override fun close() {
                runCatching { server?.cancelConnection(device) }
            }
        }
        p.mtu = serverMtu[device.address] ?: BleProto.DEFAULT_MTU
        serverPeers[device.address] = p
        peerUp(p)
        return true
    }

    private companion object {
        const val TAG = "BleMesh"
        const val CONNECT_TIMEOUT_MS = 20_000L
        const val SAVER_SCAN_MS = 10_000L
        const val SAVER_PERIOD_MS = 180_000L
        val SCAN_TOKEN = Any()
    }
}
