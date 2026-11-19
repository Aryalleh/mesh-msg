package org.meshmsg.app.data

import org.json.JSONArray
import org.json.JSONObject
import org.meshmsg.meshcore.Core
import kotlin.math.atan2
import kotlin.math.cos
import kotlin.math.sin
import kotlin.math.sqrt

/** Trip location sharing (docs/trip.md): what the core reports about one trip. */
data class TripLeader(val id: String, val name: String)

data class Trip(
    val hash: String,
    val name: String,
    /** This account leads it (holds its key, sees members). */
    val leader: Boolean,
    val leaders: List<TripLeader>,
    val every: Int,
    val ends: Long,
    val ended: Boolean,
    val relay: String,
    /** "" not asked yet, "yes", "no". */
    val consent: String,
    val sharing: Boolean,
    val sos: Boolean,
    val fastUntil: Long,
    val interval: Int,
    val nextIn: Int,
    val last: Long,
    val link: String,
    val mapKey: String,
    val members: Int,
) {
    val pending: Boolean get() = consent.isEmpty() && !ended
    val leaderNames: String get() = leaders.joinToString(", ") { it.name.ifBlank { it.id.take(8) } }
}

data class TripMember(
    val hash: String,
    val key: String,
    val name: String,
    val lat: Double,
    val lon: Double,
    val accuracy: Int,
    val battery: Int,
    val fix: Long,
    val seen: Long,
    val noFix: Boolean,
    val sos: Boolean,
    val stopped: Boolean,
    val answer: Boolean,
    val points: Int,
) {
    val hasPosition: Boolean get() = fix > 0
    val displayName: String get() = name.ifBlank { hash.take(8) }
}

data class TripRelay(val name: String, val url: String, val up: Boolean)

object TripJson {
    fun trips(s: String): List<Trip> {
        val a = JSONArray(s)
        return (0 until a.length()).map { trip(a.getJSONObject(it)) }
    }

    fun trip(o: JSONObject): Trip {
        val ls = o.optJSONArray("leaders") ?: JSONArray()
        return Trip(
            hash = o.optString("hash"), name = o.optString("name"), leader = o.optBoolean("leader"),
            leaders = (0 until ls.length()).map { ls.getJSONObject(it).let { l -> TripLeader(l.optString("id"), l.optString("name")) } },
            every = o.optInt("every"), ends = o.optLong("ends"), ended = o.optBoolean("ended"), relay = o.optString("relay"),
            consent = o.optString("consent"), sharing = o.optBoolean("sharing"), sos = o.optBoolean("sos"),
            fastUntil = o.optLong("fast_until"), interval = o.optInt("interval"), nextIn = o.optInt("next_in"),
            last = o.optLong("last"), link = o.optString("link"), mapKey = o.optString("map_key"), members = o.optInt("members"),
        )
    }

    fun members(s: String): List<TripMember> {
        val a = JSONArray(s)
        return (0 until a.length()).map {
            val o = a.getJSONObject(it)
            TripMember(
                hash = o.optString("hash"), key = o.optString("key"), name = o.optString("name"),
                lat = o.optDouble("lat"), lon = o.optDouble("lon"), accuracy = o.optInt("accuracy"), battery = o.optInt("battery", -1),
                fix = o.optLong("fix"), seen = o.optLong("seen"), noFix = o.optBoolean("no_fix"), sos = o.optBoolean("sos"),
                stopped = o.optBoolean("stopped"), answer = o.optBoolean("answer"), points = o.optJSONArray("track")?.length() ?: 0,
            )
        }
    }

    fun relays(s: String): List<TripRelay> {
        val a = JSONArray(s)
        return (0 until a.length()).map { a.getJSONObject(it).let { o -> TripRelay(o.optString("name"), o.optString("url"), o.optBoolean("up")) } }
    }
}

/** What the trips screen and the location service need; the real one is the Go core. */
interface TripsApi {
    fun trips(): List<Trip>
    fun refresh()
    fun join(link: String): Result<Trip>
    fun consent(hash: String, yes: Boolean): Result<Unit>
    fun stop(hash: String): Result<Unit>
    fun sos(hash: String, on: Boolean): Result<Unit>
    fun relays(): List<TripRelay>
    fun create(relay: String, name: String, everySec: Int, endsUnix: Long): Result<Trip>
    fun addLeader(hash: String, contact: String): Result<Trip>
    fun end(hash: String): Result<Unit>
    fun members(hash: String): List<TripMember>
    fun locate(hash: String, member: String): Result<Unit>
}

class CoreTrips(private val core: Core) : TripsApi {
    override fun trips(): List<Trip> = runCatching { TripJson.trips(core.trips()) }.getOrDefault(emptyList())
    override fun refresh() { runCatching { core.refreshTrips() } }
    override fun join(link: String): Result<Trip> = runCatching { TripJson.trip(JSONObject(core.joinTrip(link.trim()))) }
    override fun consent(hash: String, yes: Boolean): Result<Unit> = runCatching { core.tripConsent(hash, yes) }
    override fun stop(hash: String): Result<Unit> = runCatching { core.tripStop(hash) }
    override fun sos(hash: String, on: Boolean): Result<Unit> = runCatching { core.tripSOS(hash, on) }
    override fun relays(): List<TripRelay> = runCatching { TripJson.relays(core.tripRelays()) }.getOrDefault(emptyList())
    override fun create(relay: String, name: String, everySec: Int, endsUnix: Long): Result<Trip> =
        runCatching { TripJson.trip(JSONObject(core.createTrip(relay, name.trim(), everySec.toLong(), endsUnix))) }
    override fun addLeader(hash: String, contact: String): Result<Trip> = runCatching { TripJson.trip(JSONObject(core.addTripLeader(hash, contact))) }
    override fun end(hash: String): Result<Unit> = runCatching { core.endTrip(hash) }
    override fun members(hash: String): List<TripMember> = runCatching { TripJson.members(core.tripMembers(hash)) }.getOrDefault(emptyList())
    override fun locate(hash: String, member: String): Result<Unit> = runCatching { core.locateTripMember(hash, member) }

    /** Sends a report with this position if one is due (force: now); true if one went out. */
    fun report(hash: String, lat: Double, lon: Double, accuracy: Int, fixAgeSec: Int, battery: Int, hasFix: Boolean, force: Boolean): Boolean =
        runCatching { core.tripReport(hash, lat, lon, accuracy.toLong(), fixAgeSec.toLong(), battery.toLong(), hasFix, force) }.getOrDefault(false)
}

object Geo {
    /** Great-circle distance in meters. */
    fun distance(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
        val r = 6371000.0
        val p1 = Math.toRadians(lat1)
        val p2 = Math.toRadians(lat2)
        val dp = Math.toRadians(lat2 - lat1)
        val dl = Math.toRadians(lon2 - lon1)
        val a = sin(dp / 2) * sin(dp / 2) + cos(p1) * cos(p2) * sin(dl / 2) * sin(dl / 2)
        return 2 * r * atan2(sqrt(a), sqrt(1 - a))
    }

    /** Initial bearing in degrees (0 = north, clockwise). */
    fun bearing(lat1: Double, lon1: Double, lat2: Double, lon2: Double): Double {
        val p1 = Math.toRadians(lat1)
        val p2 = Math.toRadians(lat2)
        val dl = Math.toRadians(lon2 - lon1)
        val y = sin(dl) * cos(p2)
        val x = cos(p1) * sin(p2) - sin(p1) * cos(p2) * cos(dl)
        return (Math.toDegrees(atan2(y, x)) + 360) % 360
    }

    /** N, NE, E, … for a bearing. */
    fun compass(deg: Double): Int = (((deg + 22.5) % 360) / 45).toInt()
}
