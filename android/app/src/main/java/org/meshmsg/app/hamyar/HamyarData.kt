package org.meshmsg.app.hamyar

import androidx.core.content.edit
import org.json.JSONArray
import org.json.JSONObject

// The Hamyar app's screens: what they read from the Hamyar API (worker src/routes/app-api.ts). Everything is
// parsed by hand from org.json so the app needs no extra library.

private fun JSONObject.str(k: String): String? = if (isNull(k)) null else optString(k).ifEmpty { null }
private fun JSONObject.lng(k: String): Long? = if (isNull(k) || !has(k)) null else optLong(k)
private fun JSONArray?.objects(): List<JSONObject> = if (this == null) emptyList() else List(length()) { getJSONObject(it) }

/** GET /v1/me. [modules] null = every module is on. */
data class HamyarMe(
    val userId: Long, val name: String, val firstName: String, val studentCode: String?, val phone: String?,
    val university: String, val slug: String, val modules: List<String>?, val photoUrl: String?, val mavaId: String?,
) {
    fun has(m: String) = modules == null || m in modules
}

data class Registration(val memberId: Long, val status: String, val price: Long?, val paidAt: String?, val servantStatus: String?)

data class HomeTrip(
    val id: String, val name: String, val description: String?, val date: String?, val location: String?, val gender: String?,
    val capacity: Int?, val deadline: String?, val coverUrl: String?, val price: Long?, val isFull: Boolean,
    val installments: Int, val servant: Boolean, val registration: Registration?,
)

data class HomeSurvey(val id: Long, val name: String, val questions: Int, val minutes: Int, val done: Boolean)

data class HomeData(val trips: List<HomeTrip>, val surveys: List<HomeSurvey>)

data class MyTrip(
    val id: String, val name: String, val date: String?, val location: String?, val memberId: Long, val status: String,
    val price: Long?, val paidAt: String?, val mavaLink: String?,
)

data class Installment(val number: Int, val amount: Long?, val status: String, val paidAt: String?)
data class PaymentRow(val memberId: Long, val tripName: String, val price: Long?, val status: String, val payPath: String?, val installments: List<Installment>)

/** GET /v1/mava: the university's relay, the linked account and the private-history settings. */
data class MavaInfo(
    val relay: String?, val account: String?, val historyOn: Boolean, val keepDays: Int?, val backupMaxMb: Int,
    val backupSize: Long?, val backupAt: String?,
)

data class CardUse(val at: Long, val place: String?, val via: String, val result: String)

/** GET /v1/card. [key] makes the 30-second codes on the phone; null while the card is off. */
data class StudentCard(
    val name: String, val studentCode: String?, val field: String?, val degree: String?, val dormitory: String?,
    val university: String, val slug: String, val status: String, val photoUrl: String?, val key: ByteArray?, val epoch: Int,
    val uses: List<CardUse>, val isChecker: Boolean, val userId: Long,
)

/** POST /v1/card/check. */
data class CheckedStudent(val ok: Boolean, val error: String?, val name: String?, val studentCode: String?, val field: String?, val dormitory: String?, val photoUrl: String?)

data class ClassMeeting(val day: String, val start: Long, val end: Long, val minutes: Int, val status: String, val live: Boolean)
data class ClassInfo(
    val id: Long, val title: String, val teacher: String?, val place: String?, val slots: List<String>,
    val next: ClassMeeting?, val history: List<ClassMeeting>, val counts: Map<String, Int>,
)
data class Classes(val presenceOn: Boolean, val classes: List<ClassInfo>)

suspend fun HamyarApi.me(): HamyarMe {
    val r = call("GET", "/v1/me")
    store.edit { putString("me", r.toString()) }
    return parseMe(r)
}

/** The last /v1/me, so the header shows at once (and offline). */
val HamyarApi.cachedMe: HamyarMe? get() = store.getString("me", null)?.let { runCatching { parseMe(JSONObject(it)) }.getOrNull() }

private fun parseMe(r: JSONObject): HamyarMe {
    val u = r.getJSONObject("user")
    val uni = r.optJSONObject("university") ?: JSONObject()
    val mods = uni.optJSONArray("modules")?.let { a -> List(a.length()) { a.getString(it) } }
    return HamyarMe(
        userId = u.optLong("user_id"), name = u.optString("name"), firstName = u.optString("first_name"),
        studentCode = u.str("student_code"), phone = u.str("phone_number"), university = uni.optString("name"),
        slug = uni.optString("slug"), modules = mods, photoUrl = u.str("photo_url"), mavaId = u.str("mava_id"),
    )
}

suspend fun HamyarApi.home(): HomeData {
    val r = call("GET", "/v1/home")
    store.edit { putString("home", r.toString()) }
    return parseHome(r)
}

val HamyarApi.cachedHome: HomeData? get() = store.getString("home", null)?.let { runCatching { parseHome(JSONObject(it)) }.getOrNull() }

private fun parseHome(r: JSONObject) = HomeData(
    trips = r.optJSONArray("trips").objects().map { t ->
        HomeTrip(
            id = t.optString("trip_id"), name = t.optString("trip_name"), description = t.str("trip_description"),
            date = t.str("trip_date"), location = t.str("location"), gender = t.str("gender"),
            capacity = if (t.isNull("capacity")) null else t.optInt("capacity"), deadline = t.str("registration_deadline"),
            coverUrl = t.str("cover_url"), price = t.lng("final_price"), isFull = t.optBoolean("is_full"),
            installments = t.optInt("installment_count", 0), servant = t.optInt("allow_servant", 0) == 1,
            registration = t.optJSONObject("registration")?.let { m ->
                Registration(m.optLong("id"), m.optString("status"), m.lng("trip_price"), m.str("paid_at"), m.str("servant_status"))
            },
        )
    },
    surveys = r.optJSONArray("surveys").objects().map { s ->
        HomeSurvey(s.optLong("id"), s.optString("survey_name"), s.optInt("question_count"), s.optInt("survey_time"), s.optBoolean("participated"))
    },
)

suspend fun HamyarApi.myTrips(): List<MyTrip> = call("GET", "/v1/trips/mine").optJSONArray("trips").objects().map { t ->
    MyTrip(
        id = t.optString("trip_id"), name = t.optString("trip_name"), date = t.str("trip_date"), location = t.str("location"),
        memberId = t.optLong("member_id"), status = t.optString("status"), price = t.lng("trip_price"), paidAt = t.str("paid_at"),
        mavaLink = t.optJSONObject("mava_trip")?.str("link"),
    )
}

suspend fun HamyarApi.payments(): List<PaymentRow> = call("GET", "/v1/payments").optJSONArray("registrations").objects().map { r ->
    PaymentRow(
        memberId = r.optLong("id"), tripName = r.optString("trip_name"), price = r.lng("trip_price"), status = r.optString("status"),
        payPath = r.str("pay_path"),
        installments = r.optJSONArray("installments").objects().map { i -> Installment(i.optInt("installment_number"), i.lng("amount"), i.optString("status"), i.str("paid_at")) },
    )
}

/** A one-time link (2 minutes) that opens [path] on the university's site already logged in. */
suspend fun HamyarApi.webLink(path: String): String =
    call("GET", "/v1/web-link?path=" + java.net.URLEncoder.encode(path, "UTF-8")).getString("url")

// ------------------------------------------------------------------ Mava account and private history

suspend fun HamyarApi.mava(): MavaInfo {
    val r = call("GET", "/v1/mava")
    val h = r.optJSONObject("history") ?: JSONObject()
    val b = r.optJSONObject("backup")
    return MavaInfo(
        relay = r.str("relay"), account = r.str("account"), historyOn = h.optBoolean("enabled"),
        keepDays = if (h.isNull("relay_keep_days")) null else h.optInt("relay_keep_days"), backupMaxMb = h.optInt("backup_max_mb"),
        backupSize = b?.lng("size"), backupAt = b?.str("updated_at"),
    )
}

/**
 * Links this phone's Mava account to the Hamyar login (POST /v1/mava/account). [signed] are the headers
 * Core.signHamyar("POST", "/v1/mava/account") made with the account's key, which never leaves the phone.
 */
suspend fun HamyarApi.linkMava(signed: Map<String, String>): String = call("POST", "/v1/mava/account", JSONObject(), headers = signed).getString("account")

/** The account last linked from this phone, so it is linked once, not on every start. */
var HamyarApi.linkedMava: String?
    get() = store.getString("mava_linked", null)
    set(v) = store.edit { putString("mava_linked", v) }

suspend fun HamyarApi.uploadBackup(file: java.io.File, sha256: String) = bytes("PUT", "/v1/mava/backup", upload = file, headers = mapOf("x-backup-sha256" to sha256))
suspend fun HamyarApi.downloadBackup(to: java.io.File) = bytes("GET", "/v1/mava/backup/file", download = to)

// ------------------------------------------------------------------ student card

suspend fun HamyarApi.card(rekey: Boolean = false): StudentCard {
    val r = if (rekey) call("POST", "/v1/card", JSONObject().put("rekey", true)) else call("GET", "/v1/card")
    store.edit { putString("card", r.toString()) }
    return parseCard(r)
}

/** The last card, so it shows (and its codes work) without the network. */
val HamyarApi.cachedCard: StudentCard? get() = store.getString("card", null)?.let { runCatching { parseCard(JSONObject(it)) }.getOrNull() }

private fun parseCard(r: JSONObject): StudentCard {
    val c = r.getJSONObject("card")
    return StudentCard(
        name = c.optString("name"), studentCode = c.str("student_code"), field = c.str("field_of_study"), degree = c.str("degree"),
        dormitory = c.str("dormitory"), university = c.optString("university"), slug = c.optString("slug"), status = c.optString("status", "active"),
        photoUrl = c.str("photo_url"), key = r.str("key")?.let(PresenceCodec::unhex), epoch = r.optInt("epoch", 1),
        uses = r.optJSONArray("uses").objects().map { CardUse(it.optLong("at"), it.str("place"), it.optString("via"), it.optString("result")) },
        isChecker = r.optBoolean("is_checker"), userId = c.optLong("user_id", 0),
    )
}

suspend fun HamyarApi.checkCard(code: String, via: String, place: String?): CheckedStudent {
    val r = try {
        call("POST", "/v1/card/check", JSONObject().put("code", code).put("via", via).put("place", place ?: ""))
    } catch (e: HamyarError) {
        if (e.http == 400) return CheckedStudent(false, e.code, null, null, null, null, null) else throw e
    }
    val s = r.optJSONObject("student")
    return CheckedStudent(r.optBoolean("ok"), r.str("error"), s?.optString("name"), s?.str("student_code"), s?.str("field_of_study"), s?.str("dormitory"), s?.str("photo_url"))
}

// ------------------------------------------------------------------ classes

suspend fun HamyarApi.classes(): Classes {
    val r = call("GET", "/v1/classes")
    fun meeting(o: JSONObject) = ClassMeeting(o.optString("day"), o.optLong("start"), o.optLong("end"), o.optInt("minutes"), o.optString("status", "unknown"), o.optBoolean("live"))
    return Classes(
        presenceOn = r.optBoolean("presence_on"),
        classes = r.optJSONArray("classes").objects().map { c ->
            val cnt = c.optJSONObject("counts")
            ClassInfo(
                id = c.optLong("id"), title = c.optString("title"), teacher = c.str("teacher"), place = c.str("place"),
                slots = c.optJSONArray("slots").objects().map { "${it.optString("day_name")} ${it.optString("start")}–${it.optString("end")}" },
                next = c.optJSONObject("next")?.let(::meeting), history = c.optJSONArray("history").objects().map(::meeting),
                counts = cnt?.keys()?.asSequence()?.associateWith { cnt.optInt(it) } ?: emptyMap(),
            )
        },
    )
}
