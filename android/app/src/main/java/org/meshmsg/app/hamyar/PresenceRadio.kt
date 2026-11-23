package org.meshmsg.app.hamyar

import android.Manifest
import android.annotation.SuppressLint
import android.bluetooth.BluetoothManager
import android.bluetooth.le.AdvertiseCallback
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertiseSettings
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.util.Log
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/** What the presence radio is doing, for the presence screen. */
data class RadioState(
    val running: Boolean = false,
    /** Why it isn't running: "login", "setup", "not_counted", "bluetooth", "permission". */
    val idle: String? = "login",
    /** Unix seconds a node was last heard, and the place the server last counted this phone in. */
    val lastHeard: Long = 0,
    val lastPlace: String? = null,
    val lastSent: Long = 0,
    /** The trip group this phone advertises in (trip id), and when another member was last heard. */
    val groupTrip: String? = null,
    val lastMemberHeard: Long = 0,
)

/**
 * The phone side of Hamyar presence and trip groups (docs/presence.md in mesh-msg and Hamyar), run by MeshService so it
 * keeps going in the background:
 *  - campus presence: advertises "MP" + this student's rotating phone token, so the nodes around hear it, and scans for
 *    "MV" node beacons; once per 2-minute window tells the university which nodes it heard;
 *  - trip group: advertises "MT" + gid + this member's rotating trip token, scans for other members' "MT" adverts of
 *    the same group, queues what it heard (also offline, up to 12 h) and sends it when the network is there.
 * It only runs while the student is logged in to Hamyar and went through the setup screen, and only advertises what
 * the server gave a key for (counted for presence, joined a group). No location or other devices leave the phone.
 */
object PresenceRadio {
    private const val TAG = "PresenceRadio"
    private const val REFRESH_SEC = 30 * 60L

    private val _state = MutableStateFlow(RadioState())
    val state: StateFlow<RadioState> = _state

    private var job: Job? = null
    private var window = -1L
    private var ticks = 0L
    private var lastRefresh = 0L
    /** Payloads for this window: "MP" (presence) and/or "MT" (trip group). */
    private var payloads: List<ByteArray> = emptyList()
    /** node id -> best sighting in the current window. */
    private val heard = HashMap<Long, HamyarApi.Sighting>()
    /** Group tokens heard since the last hand-over, per gid, each with the window it was heard in. */
    private val members = HashMap<Long, HashSet<HamyarApi.Heard>>()
    /** gid -> trip id of the group scanned for. */
    @Volatile private var groupTrips: Map<Long, String> = emptyMap()
    private val lock = Any()

    fun start(context: Context, scope: CoroutineScope) {
        if (job?.isActive == true) return
        val ctx = context.applicationContext
        val api = HamyarApi(ctx)
        job = scope.launch {
            while (isActive) {
                runCatching { tick(ctx, api) }.onFailure { Log.w(TAG, "tick", it) }
                delay(15_000)
            }
        }
    }

    fun stop(context: Context) {
        job?.cancel()
        job = null
        radioOff(context.applicationContext)
    }

    /** Ask for fresh settings on the next tick (after a change on the presence or trip screen). */
    fun poke() { lastRefresh = 0 }

    private fun now() = System.currentTimeMillis() / 1000

    private suspend fun tick(ctx: Context, api: HamyarApi) {
        ticks++
        val why = when {
            !api.loggedIn -> "login"
            !api.presenceSetupDone -> "setup"
            !permitted(ctx) -> "permission"
            adapterOff(ctx) -> "bluetooth"
            else -> null
        }
        if (why == null && now() - lastRefresh > REFRESH_SEC) {
            api.refreshIfOld()
            val p = runCatching { api.presence() }
            val g = runCatching { api.groups() }
            if (p.isSuccess || g.isSuccess) lastRefresh = now()
        }
        val presenceKey = api.cachedPresence?.key
        // one trip at a time: a student is on one trip at once; the first joined group wins
        val group = api.cachedGroups.firstOrNull { it.joined && it.key != null }
        if (why != null || (presenceKey == null && group == null)) {
            radioOff(ctx)
            _state.value = _state.value.copy(running = false, idle = why ?: "not_counted", groupTrip = null)
            return
        }
        val trips = group?.let { mapOf(it.gid to it.tripId) } ?: emptyMap()
        groupTrips = trips
        val w = PresenceCodec.window(now())
        if (w != window) {
            // a new window: hand over what was heard in the last one, then rotate the tokens
            val nodes = synchronized(lock) { heard.values.toList().also { heard.clear() } }
            val tokens = synchronized(lock) { members.toMap().also { members.clear() } }
            if (presenceKey != null && nodes.isNotEmpty()) {
                runCatching { api.seen(nodes.sortedByDescending { it.rssi }.take(20)) }
                    .onSuccess { place -> _state.value = _state.value.copy(lastSent = now(), lastPlace = place ?: _state.value.lastPlace) }
                    .onFailure { Log.i(TAG, "seen: ${it.message}") }
            }
            for ((gid, set) in tokens) trips[gid]?.let { api.queueHeard(it, set) }
            runCatching { api.flushHeard() }.onFailure { Log.i(TAG, "group sightings kept: ${it.message}") }
            payloads = listOfNotNull(
                presenceKey?.let { PresenceCodec.adv('P', 0, PresenceCodec.phoneToken(it, w)) },
                group?.let { PresenceCodec.adv('T', it.gid, PresenceCodec.tripToken(it.key!!, w)) },
            )
            window = w
        }
        advertise(ctx)
        scan(ctx, buildString { if (presenceKey != null) append('V'); if (group != null) append('T') })
        _state.value = _state.value.copy(running = true, idle = null, groupTrip = group?.tripId)
    }

    private fun permitted(ctx: Context): Boolean {
        val need = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
            listOf(Manifest.permission.BLUETOOTH_SCAN, Manifest.permission.BLUETOOTH_ADVERTISE)
        } else {
            listOf(Manifest.permission.ACCESS_FINE_LOCATION)
        }
        return need.all { ContextCompat.checkSelfPermission(ctx, it) == PackageManager.PERMISSION_GRANTED }
    }

    private fun adapter(ctx: Context) = ctx.getSystemService(BluetoothManager::class.java)?.adapter
    private fun adapterOff(ctx: Context) = adapter(ctx)?.isEnabled != true

    /** One advertising set. Phones without multiple advertising use only slot 0 and alternate the payloads per tick. */
    private class Slot : AdvertiseCallback() {
        var on = false
        var payload: ByteArray? = null
        override fun onStartFailure(errorCode: Int) {
            Log.w(TAG, "advertise failed: $errorCode")
            on = false
            payload = null // try again next tick
        }
    }

    private val slots = arrayOf(Slot(), Slot())

    @SuppressLint("MissingPermission") // checked in permitted()
    private fun advertise(ctx: Context) {
        val a = adapter(ctx) ?: return
        val adv = a.bluetoothLeAdvertiser ?: return
        val want: List<ByteArray?> = when {
            payloads.size <= 1 -> listOf(payloads.firstOrNull(), null)
            a.isMultipleAdvertisementSupported -> payloads
            else -> listOf(payloads[(ticks % payloads.size).toInt()], null)
        }
        for (i in slots.indices) {
            val s = slots[i]
            val p = want.getOrNull(i)
            if (s.on && p != null && p.contentEquals(s.payload)) continue
            if (s.on) runCatching { adv.stopAdvertising(s) }
            s.on = false
            s.payload = null
            if (p == null) continue
            val settings = AdvertiseSettings.Builder()
                .setAdvertiseMode(AdvertiseSettings.ADVERTISE_MODE_LOW_POWER)
                .setTxPowerLevel(AdvertiseSettings.ADVERTISE_TX_POWER_MEDIUM)
                .setConnectable(false)
                .build()
            val data = AdvertiseData.Builder()
                .setIncludeDeviceName(false)
                .setIncludeTxPowerLevel(false)
                .addManufacturerData(PresenceCodec.MANUFACTURER, p)
                .build()
            s.on = runCatching { adv.startAdvertising(settings, data, s) }.isSuccess
            if (s.on) s.payload = p
        }
    }

    /** The advert kinds the running scan filters on ("" = not scanning). */
    private var scanKinds = ""

    private val scanCallback = object : ScanCallback() {
        override fun onScanResult(callbackType: Int, result: ScanResult) = found(result)
        override fun onBatchScanResults(results: MutableList<ScanResult>) = results.forEach(::found)
        override fun onScanFailed(errorCode: Int) {
            Log.w(TAG, "scan failed: $errorCode")
            scanKinds = ""
        }
    }

    private fun found(r: ScanResult) {
        val a = PresenceCodec.parse(r.scanRecord?.getManufacturerSpecificData(PresenceCodec.MANUFACTURER)) ?: return
        val w = PresenceCodec.window(now())
        when (a.kind) {
            'V' -> {
                val s = HamyarApi.Sighting(a.id, w, PresenceCodec.hex(a.token), r.rssi)
                synchronized(lock) {
                    val old = heard[a.id]
                    if (old == null || s.rssi > old.rssi) heard[a.id] = s
                }
                _state.value = _state.value.copy(lastHeard = now())
            }
            'T' -> {
                if (a.id !in groupTrips) return
                synchronized(lock) {
                    val set = members.getOrPut(a.id) { HashSet() }
                    if (set.size < 400) set.add(HamyarApi.Heard(w, PresenceCodec.hex(a.token)))
                }
                _state.value = _state.value.copy(lastMemberHeard = now())
            }
        }
    }

    @SuppressLint("MissingPermission") // checked in permitted()
    private fun scan(ctx: Context, kinds: String) {
        if (kinds == scanKinds) return
        val scanner = adapter(ctx)?.bluetoothLeScanner ?: return
        if (scanKinds.isNotEmpty()) runCatching { scanner.stopScan(scanCallback) }
        scanKinds = ""
        if (kinds.isEmpty()) return
        // a filter is required for scanning with the screen off; filters are OR-ed
        val filters = kinds.map { k ->
            ScanFilter.Builder()
                .setManufacturerData(PresenceCodec.MANUFACTURER, byteArrayOf('M'.code.toByte(), k.code.toByte()), byteArrayOf(-1, -1))
                .build()
        }
        val settings = ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_POWER).build()
        if (runCatching { scanner.startScan(filters, settings, scanCallback) }.isSuccess) scanKinds = kinds
    }

    @SuppressLint("MissingPermission")
    private fun radioOff(ctx: Context) {
        scan(ctx, "")
        val adv = adapter(ctx)?.bluetoothLeAdvertiser
        for (s in slots) {
            if (s.on) runCatching { adv?.stopAdvertising(s) }
            s.on = false
            s.payload = null
        }
        window = -1
        payloads = emptyList()
        groupTrips = emptyMap()
        synchronized(lock) { heard.clear(); members.clear() }
    }
}
