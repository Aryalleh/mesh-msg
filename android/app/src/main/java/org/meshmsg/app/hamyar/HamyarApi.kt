package org.meshmsg.app.hamyar

import android.content.Context
import androidx.core.content.edit
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL

/** One Hamyar account the phone number has (a university or a club). */
data class HamyarAccount(val slug: String, val title: String, val name: String)

/** The app's presence settings from the user's university (GET /v1/presence). */
data class PresenceInfo(
    val optIn: Boolean,
    /** The dorm whose rule counts this student there (null: not a resident of a dorm with nodes). */
    val requiredDorm: String?,
    val counted: Boolean,
    val windowSec: Long,
    /** Makes this phone's BLE token; null while the student isn't counted anywhere. */
    val key: ByteArray?,
    /** "uni", "dorm" or "outside". */
    val status: String,
    val place: String?,
)

class HamyarError(val code: String, val http: Int) : Exception(code)

/** A trip of mine whose group ("are we all together?") is on (GET /v1/groups). [key] is set once I joined. */
data class TripGroup(val tripId: String, val tripName: String, val gid: Long, val joined: Boolean, val key: ByteArray?)

/** One member's state in a trip group: "together", "far", "separated" or "unknown". */
data class GroupMember(val userId: Long, val name: String, val state: String, val lastTogether: Long?, val phone: String?)

/** GET /v1/trips/<id>/group: my state, the leaders, and (leaders only) every joined member. */
data class GroupView(
    val tripName: String,
    val active: Boolean,
    val joined: Boolean,
    val sepMin: Int,
    val myState: String?,
    val isLeader: Boolean,
    val leaders: List<String>,
    /** together, far, separated, unknown, joined, registered. */
    val counts: Map<String, Int>,
    val members: List<GroupMember>,
)

/**
 * Client for the Hamyar app API (api.hamyared.ir, Hamyar worker src/routes/api-hub.ts): phone + SMS code login, then a
 * bearer token (hy1.…, 30 days) that the API routes to the user's own university. Only the token, the university and
 * the user's name are kept on the phone.
 */
class HamyarApi(context: Context, private val base: String = BASE) {
    private val p = context.getSharedPreferences("hamyar", Context.MODE_PRIVATE)

    val token: String? get() = p.getString("token", null)?.takeIf { p.getLong("expires", 0) > now() }
    val university: String get() = p.getString("uni_title", "") ?: ""
    val userName: String get() = p.getString("user_name", "") ?: ""
    val loggedIn: Boolean get() = token != null

    /** The student went through the presence setup screen (shown once). */
    var presenceSetupDone: Boolean
        get() = p.getBoolean("presence_setup", false)
        set(v) = p.edit { putBoolean("presence_setup", v) }

    /** Last presence settings, so the radio can start without the network. */
    var cachedPresence: PresenceInfo?
        get() = p.getString("presence", null)?.let { runCatching { presence(JSONObject(it)) }.getOrNull() }
        private set(v) = p.edit { if (v == null) remove("presence") else putString("presence", json(v).toString()) }

    suspend fun sendCode(phone: String) { call("POST", "/v1/auth/otp", JSONObject().put("phone", phone), auth = false) }

    /** Checks the code: one account logs in now; several come back for the user to pick (then [select]). */
    suspend fun verify(phone: String, code: String): Pair<List<HamyarAccount>, String?> {
        val r = call("POST", "/v1/auth/verify", JSONObject().put("phone", phone).put("code", code), auth = false)
        val a = r.optJSONArray("accounts")
        val list = List(a?.length() ?: 0) { a!!.getJSONObject(it).let { o -> HamyarAccount(o.getString("slug"), o.optString("title"), o.optString("name")) } }
        if (r.has("token")) keep(r)
        return list to r.optString("proof").ifEmpty { null }
    }

    suspend fun select(proof: String, slug: String) = keep(call("POST", "/v1/auth/select", JSONObject().put("proof", proof).put("slug", slug), auth = false))

    fun logout() = p.edit { clear() }

    suspend fun presence(): PresenceInfo = presence(call("GET", "/v1/presence")).also { cachedPresence = it }

    suspend fun setOptIn(on: Boolean): PresenceInfo {
        call("POST", "/v1/presence", JSONObject().put("opt_in", on))
        return presence()
    }

    /** The phone heard nodes: [{n, w, t, r}]. Answers the place it was counted in, if any. */
    suspend fun seen(list: List<Sighting>): String? {
        val a = org.json.JSONArray()
        list.forEach { a.put(JSONObject().put("n", it.node).put("w", it.window).put("t", it.token).put("r", it.rssi)) }
        return call("POST", "/v1/presence/seen", JSONObject().put("seen", a)).optString("place").ifEmpty { null }
    }

    // ------------------------------------------------------------ trip groups

    /** Last list of my trip groups, so the radio can advertise without the network. */
    var cachedGroups: List<TripGroup>
        get() = p.getString("groups", null)?.let { runCatching { groups(org.json.JSONArray(it)) }.getOrNull() } ?: emptyList()
        private set(v) = p.edit { putString("groups", org.json.JSONArray().apply { v.forEach { put(json(it)) } }.toString()) }

    suspend fun groups(): List<TripGroup> = groups(call("GET", "/v1/groups").optJSONArray("groups") ?: org.json.JSONArray()).also { cachedGroups = it }

    suspend fun group(tripId: String): GroupView = groupView(call("GET", "/v1/trips/${enc(tripId)}/group"))

    suspend fun setJoined(tripId: String, join: Boolean) {
        call("POST", "/v1/trips/${enc(tripId)}/group", JSONObject().put("join", join))
        if (!join) dropHeard(tripId)
        runCatching { groups() }
    }

    /**
     * Group tokens heard, queued per trip until sent (phones on a trip are often offline). The server takes windows up
     * to 12 h old; older ones are dropped here, and at most [MAX_QUEUE] are kept per trip.
     */
    fun queueHeard(tripId: String, list: Collection<Heard>) {
        if (list.isEmpty()) return
        synchronized(queueLock) {
            val q = heardQueue()
            val a = q.optJSONArray(tripId) ?: org.json.JSONArray()
            list.forEach { a.put(JSONObject().put("w", it.window).put("t", it.token)) }
            q.put(tripId, trimQueue(a))
            p.edit { putString("group_heard", q.toString()) }
        }
    }

    /** Sends every queued trip's sightings; keeps them on network errors, drops them when the group is gone. */
    suspend fun flushHeard() {
        val q = synchronized(queueLock) { heardQueue() }
        for (tripId in q.keys().asSequence().toList()) {
            val a = trimQueue(q.getJSONArray(tripId))
            if (a.length() == 0) { dropHeard(tripId); continue }
            try {
                call("POST", "/v1/trips/${enc(tripId)}/group/heard", JSONObject().put("heard", a))
                dropHeard(tripId, a.length())
            } catch (e: HamyarError) {
                if (e.http == 403 || e.http == 404) dropHeard(tripId) else throw e
            }
        }
    }

    /** Removes the first [n] queued sightings of a trip (all when null): what was sent, while new ones may have arrived. */
    private fun dropHeard(tripId: String, n: Int? = null) = synchronized(queueLock) {
        val q = heardQueue()
        val a = q.optJSONArray(tripId) ?: return@synchronized
        if (n == null || n >= a.length()) q.remove(tripId)
        else q.put(tripId, org.json.JSONArray().apply { for (i in n until a.length()) put(a.get(i)) })
        p.edit { putString("group_heard", q.toString()) }
    }

    private fun heardQueue() = p.getString("group_heard", null)?.let { runCatching { JSONObject(it) }.getOrNull() } ?: JSONObject()

    private fun trimQueue(a: org.json.JSONArray): org.json.JSONArray {
        val oldest = PresenceCodec.window(now() - KEEP_HEARD_SEC)
        val keep = (0 until a.length()).map { a.getJSONObject(it) }.filter { it.optLong("w") >= oldest }
        return org.json.JSONArray().apply { keep.takeLast(MAX_QUEUE).forEach { put(it) } }
    }

    /** Refreshes the token when it has under a week left. */
    suspend fun refreshIfOld() {
        if (token != null && p.getLong("expires", 0) - now() < 7 * 86400) runCatching { keep(call("POST", "/v1/auth/refresh")) }
    }

    private fun keep(r: JSONObject) = p.edit {
        putString("token", r.getString("token"))
        putLong("expires", r.getLong("expires_at"))
        r.optJSONObject("university")?.let { putString("uni_slug", it.optString("slug")); putString("uni_title", it.optString("title")) }
        r.optJSONObject("user")?.let { putString("user_name", it.optString("name")) }
    }

    private suspend fun call(method: String, path: String, body: JSONObject? = null, auth: Boolean = true): JSONObject = withContext(Dispatchers.IO) {
        val c = URL(base + path).openConnection() as HttpURLConnection
        try {
            c.requestMethod = method
            c.connectTimeout = 15_000
            c.readTimeout = 20_000
            c.setRequestProperty("Accept", "application/json")
            if (auth) c.setRequestProperty("Authorization", "Bearer " + (token ?: throw HamyarError("unauthorized", 401)))
            if (body != null) {
                c.doOutput = true
                c.setRequestProperty("Content-Type", "application/json")
                c.outputStream.use { it.write(body.toString().toByteArray()) }
            }
            val code = c.responseCode
            val text = (if (code < 400) c.inputStream else c.errorStream)?.bufferedReader()?.use { it.readText() }.orEmpty()
            val json = runCatching { JSONObject(text) }.getOrElse { JSONObject() }
            if (code >= 400) {
                if (code == 401 && auth) logout()
                throw HamyarError(json.optString("error").ifEmpty { "http_$code" }, code)
            }
            json
        } finally {
            c.disconnect()
        }
    }

    data class Sighting(val node: Long, val window: Long, val token: String, val rssi: Int)

    /** A trip group token heard in a window. */
    data class Heard(val window: Long, val token: String)

    companion object {
        const val BASE = "https://api.hamyared.ir"
        /** The server accepts group sightings up to 12 h old (worker src/lib/trip-group.ts GROUP_KEEP_SEC). */
        const val KEEP_HEARD_SEC = 12 * 3600L
        const val MAX_QUEUE = 2000
        private val queueLock = Any()
        private fun now() = System.currentTimeMillis() / 1000
        private fun enc(s: String) = java.net.URLEncoder.encode(s, "UTF-8").replace("+", "%20")

        fun groups(a: org.json.JSONArray) = List(a.length()) { a.getJSONObject(it) }.map { o ->
            TripGroup(
                tripId = o.getString("trip_id"), tripName = o.optString("trip_name"), gid = o.getLong("gid"),
                joined = o.optBoolean("joined"), key = if (o.isNull("key")) null else PresenceCodec.unhex(o.optString("key")),
            )
        }

        fun json(g: TripGroup): JSONObject = JSONObject().put("trip_id", g.tripId).put("trip_name", g.tripName).put("gid", g.gid)
            .put("joined", g.joined).put("key", g.key?.let(PresenceCodec::hex) ?: JSONObject.NULL)

        fun groupView(o: JSONObject): GroupView {
            val c = o.optJSONObject("counts")
            val l = o.optJSONArray("leaders")
            val m = o.optJSONArray("members")
            return GroupView(
                tripName = o.optJSONObject("trip")?.optString("trip_name").orEmpty(),
                active = o.optBoolean("active"), joined = o.optBoolean("joined"), sepMin = o.optInt("sep_min", 10),
                myState = o.optJSONObject("me")?.optString("state")?.ifEmpty { null },
                isLeader = o.optBoolean("is_leader"),
                leaders = List(l?.length() ?: 0) { l!!.getJSONObject(it).optString("name") },
                counts = c?.keys()?.asSequence()?.associateWith { c.optInt(it) } ?: emptyMap(),
                members = List(m?.length() ?: 0) { m!!.getJSONObject(it) }.map {
                    GroupMember(
                        userId = it.optLong("user_id"), name = it.optString("name").trim(), state = it.optString("state", "unknown"),
                        lastTogether = if (it.isNull("last_together")) null else it.optLong("last_together"),
                        phone = if (it.isNull("phone_number")) null else it.optString("phone_number").ifEmpty { null },
                    )
                },
            )
        }

        fun presence(o: JSONObject) = PresenceInfo(
            optIn = o.optBoolean("opt_in"),
            requiredDorm = o.optString("required_dorm").takeIf { o.has("required_dorm") && !o.isNull("required_dorm") && it.isNotEmpty() },
            counted = o.optBoolean("counted"),
            windowSec = o.optLong("window_sec", PresenceCodec.WINDOW_SEC),
            key = if (o.isNull("key")) null else PresenceCodec.unhex(o.optString("key")),
            status = o.optString("status", "outside"),
            place = if (o.isNull("place")) null else o.optString("place").ifEmpty { null },
        )

        fun json(i: PresenceInfo): JSONObject = JSONObject()
            .put("opt_in", i.optIn).put("required_dorm", i.requiredDorm ?: JSONObject.NULL).put("counted", i.counted)
            .put("window_sec", i.windowSec).put("key", i.key?.let(PresenceCodec::hex) ?: JSONObject.NULL)
            .put("status", i.status).put("place", i.place ?: JSONObject.NULL)
    }
}
