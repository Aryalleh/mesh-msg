package org.meshmsg.app.data

import androidx.compose.runtime.Immutable
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant

// UI models, parsed from the JSON the Go core returns (the same JSON meshd
// serves). All immutable, so Compose can skip unchanged rows.

/** Kinds of group chat, as the core names them. */
enum class GroupKind(val id: String) {
    Private("private"), Regional("regional"), Global("global");

    val public: Boolean get() = this != Private

    companion object {
        fun of(id: String) = entries.firstOrNull { it.id == id } ?: Private
    }
}

/** A member of a private group. */
@Immutable
data class GroupMember(val hash: String, val name: String, val owner: Boolean = false, val you: Boolean = false) {
    val displayName: String get() = name.ifBlank { hash.take(8) }
}

/**
 * A chat partner: a person, or a group ([group] set; then [name] is the
 * group's title such as "#tehran" and [uri] a public chat's link). Private
 * groups carry their [members]; [isOwner] may add and remove them.
 */
@Immutable
data class Contact(
    val hash: String,
    val name: String,
    val uri: String,
    val group: GroupKind? = null,
    val members: List<GroupMember> = emptyList(),
    val isOwner: Boolean = false,
    val removed: Boolean = false,
) {
    /** Name to show: the saved name, or a short form of the hash. */
    val displayName: String get() = name.ifBlank { hash.take(8) }
    val isGroup: Boolean get() = group != null
}

/** How far an outgoing message got. Never shown by colour alone. */
enum class Delivery { Queued, Sent, Delivered }

@Immutable
data class Message(
    val id: String,
    val peer: String,
    val outgoing: Boolean,
    val text: String,
    val sentMs: Long,
    val receivedMs: Long,
    val delivery: Delivery,
    /** Group messages: who wrote it (their chosen or saved name, "you" for us). */
    val group: Boolean = false,
    val fromName: String = "",
    /** Outgoing private-group messages: members it went to, and how many confirmed. */
    val recipients: Int = 0,
    val deliveredTo: Int = 0,
    /** A photo, voice or video message, or file. */
    val media: Media? = null,
) {
    /** When the user saw it happen on this phone. */
    val timeMs: Long get() = if (outgoing) sentMs else receivedMs
}

enum class MediaKind { Image, Audio, Video, Round, File }

/** ready: on this phone; remote: tap to download; downloading; missing: gone everywhere. */
enum class MediaState { Ready, Remote, Downloading, Missing }

@Immutable
data class Media(
    val id: String,
    val mime: String,
    val name: String,
    val size: Long,
    val state: MediaState,
    val have: Long = 0,
    val total: Long = 0,
    /** Outgoing: relays it should go to, and how many have it. */
    val relays: Int = 0,
    val uploaded: Int = 0,
    /** Also goes through relays (reaches someone offline); normal files go only over direct paths. */
    val important: Boolean = false,
) {
    val kind: MediaKind get() = when (mime.substringBefore('/')) {
        "image" -> MediaKind.Image
        "audio" -> MediaKind.Audio
        "video" -> if (name.startsWith("round-")) MediaKind.Round else MediaKind.Video
        else -> MediaKind.File
    }
    val progress: Float get() = if (total > 0) (have.toFloat() / total).coerceIn(0f, 1f) else 0f
}

@Immutable
data class Chat(val contact: Contact, val last: Message?, val unread: Int)

enum class PathState { Up, Waiting, Off }

/** One way messages can travel right now (design: "path chips"). */
@Immutable
data class PathInfo(val kind: Kind, val name: String, val state: PathState, val detail: String) {
    enum class Kind { Nearby, LoRa, Relay }
}

@Immutable
data class LoRaNode(
    val name: String,
    val connected: Boolean,
    val error: String,
    val role: String,
    val region: String,
    val preset: String,
    val freqHz: Long,
    val txDbm: Int,
    val neighbours: Int,
    val batteryMv: Int,
    val airUsedMs: Long,
    val airBudgetMs: Long,
)

@Immutable
data class NetStatus(
    val hash: String = "",
    val stored: Int = 0,
    val knownNodes: Int = 0,
    val gateway: Boolean = false,
    val delivered: Long = 0,
    val forwarded: Long = 0,
    val paths: List<PathInfo> = emptyList(),
    val lora: List<LoRaNode> = emptyList(),
) {
    /** True if at least one path could carry a message right now. */
    val anyPathUp: Boolean get() = paths.any { it.state == PathState.Up }
}

// IDs as in core/hostlink (checked against spec/test-vectors on the Go side).
val ROLES = listOf("client", "repeater", "gateway")
val REGIONS = listOf("EU_868", "EU_868_1PCT", "EU_433", "US_915", "AU_915", "IN_865", "AS_923")
val PRESETS = listOf("FAST", "BALANCED", "LONG")

/** A call, as the core reports it (docs/calls.md). */
data class CallInfo(
    val id: String,
    val peer: String,
    val incoming: Boolean,
    /** outgoing | ringing | connecting | active | ended */
    val state: String,
    val codec: Int = 0,
    /** Six digits both people can compare by voice. */
    val code: String = "",
    val relayed: Boolean = false,
    val reason: String = "",
    val missed: Boolean = false,
    /** When it became active (ms), 0 before. */
    val started: Long = 0,
    val peerMuted: Boolean = false,
) {
    val ended: Boolean get() = state == "ended"
}

object Json {
    fun call(o: JSONObject) = CallInfo(
        id = o.optString("id"),
        peer = o.optString("peer"),
        incoming = o.optBoolean("incoming"),
        state = o.optString("state"),
        codec = o.optInt("codec"),
        code = o.optString("code"),
        relayed = o.optBoolean("relayed"),
        reason = o.optString("reason"),
        missed = o.optBoolean("missed"),
        started = o.optLong("started"),
        peerMuted = o.optBoolean("peer_muted"),
    )

    private fun time(s: String): Long = runCatching { Instant.parse(s).toEpochMilli() }.getOrDefault(0L)

    fun contact(o: JSONObject) = Contact(o.optString("hash"), o.optString("name"), o.optString("uri"))

    /** A group (core node.Group JSON) as a chat partner. */
    fun group(o: JSONObject): Contact {
        val ms = o.optJSONArray("members") ?: JSONArray()
        val members = List(ms.length()) {
            val m = ms.getJSONObject(it)
            GroupMember(m.optString("hash"), m.optString("name"), m.optBoolean("owner"), m.optBoolean("you"))
        }
        return Contact(
            o.optString("hash"), o.optString("title"), o.optString("uri"), GroupKind.of(o.optString("kind")),
            members, o.optBoolean("is_owner"), o.optBoolean("removed"),
        )
    }

    /**
     * [linkUp]: whether any path existed when the list was read. The core
     * hands a message to every link at once, so an outgoing message that is
     * not yet delivered counts as Sent only if there was somewhere to send it.
     */
    fun message(o: JSONObject, linkUp: Boolean): Message {
        val outgoing = o.optBoolean("outgoing")
        val delivery = when {
            o.optBoolean("delivered") -> Delivery.Delivered
            linkUp -> Delivery.Sent
            else -> Delivery.Queued
        }
        return Message(
            id = o.optString("id"), peer = o.optString("peer"), outgoing = outgoing, text = o.optString("text"),
            sentMs = time(o.optString("sent")), receivedMs = time(o.optString("received")),
            delivery = if (outgoing) delivery else Delivery.Delivered,
            group = o.optBoolean("group"),
            fromName = o.optString("from_name").ifBlank { o.optString("from").take(8) },
            recipients = o.optInt("recipients"),
            deliveredTo = o.optInt("delivered_to"),
            media = o.optJSONObject("media")?.let { media(it) },
        )
    }

    fun media(o: JSONObject): Media = Media(
        id = o.optString("id"), mime = o.optString("mime"), name = o.optString("name"), size = o.optLong("size"),
        state = when (o.optString("state")) {
            "ready" -> MediaState.Ready
            "downloading" -> MediaState.Downloading
            "missing" -> MediaState.Missing
            else -> MediaState.Remote
        },
        have = o.optLong("have"), total = o.optLong("total"),
        relays = o.optInt("relays"), uploaded = o.optInt("uploaded"),
        important = o.optBoolean("important"),
    )

    fun messages(json: String, linkUp: Boolean): List<Message> {
        val a = JSONArray(json)
        return List(a.length()) { message(a.getJSONObject(it), linkUp) }
    }

    fun chats(json: String, linkUp: Boolean): List<Chat> {
        val a = JSONArray(json)
        return List(a.length()) {
            val o = a.getJSONObject(it)
            val who = o.optJSONObject("group")?.let(::group) ?: contact(o.getJSONObject("contact"))
            Chat(who, o.optJSONObject("last")?.let { m -> message(m, linkUp) }, o.optInt("unread"))
        }
    }

    fun status(json: String): NetStatus {
        val o = JSONObject(json)
        val links = o.optJSONObject("links") ?: JSONObject()
        val loraNames = HashSet<String>()
        val lora = o.optJSONArray("lora")?.let { a ->
            List(a.length()) { i ->
                val n = a.getJSONObject(i)
                val st = n.optJSONObject("status")
                loraNames += n.optString("name")
                LoRaNode(
                    name = n.optString("name"), connected = n.optBoolean("connected"), error = n.optString("error"),
                    role = ROLES.getOrElse(st?.optInt("role", -1) ?: -1) { "" },
                    region = REGIONS.getOrElse(st?.optInt("region", -1) ?: -1) { "" },
                    preset = PRESETS.getOrElse(st?.optInt("preset", -1) ?: -1) { "" },
                    freqHz = st?.optLong("freq_hz") ?: 0, txDbm = st?.optInt("tx_dbm") ?: 0,
                    neighbours = st?.optInt("neighbours") ?: 0, batteryMv = st?.optInt("battery_mv") ?: 0,
                    airUsedMs = st?.optLong("air_used_ms") ?: 0, airBudgetMs = st?.optLong("air_budget_ms") ?: 0,
                )
            }
        } ?: emptyList()

        val paths = ArrayList<PathInfo>()
        for (name in links.keys().asSequence().sorted()) {
            val detail = links.optString(name)
            val kind = when {
                name == "core" -> continue
                name in loraNames || name.startsWith("lora") || name.startsWith("usb") -> PathInfo.Kind.LoRa
                name == "ble" || name == "lan" -> PathInfo.Kind.Nearby
                else -> PathInfo.Kind.Relay
            }
            paths += PathInfo(kind, name, pathState(kind, detail), detail)
        }
        val r = o.optJSONObject("router")
        return NetStatus(
            hash = o.optString("hash"), stored = o.optInt("stored"), knownNodes = o.optInt("routes"),
            gateway = o.optBoolean("gateway"), delivered = r?.optLong("Delivered") ?: 0, forwarded = r?.optLong("Forwarded") ?: 0,
            paths = paths, lora = lora,
        )
    }

    /** Maps the core's free-text link status onto three states. */
    fun pathState(kind: PathInfo.Kind, detail: String): PathState {
        val d = detail.lowercase()
        return when {
            d == "off" -> PathState.Off
            kind == PathInfo.Kind.Nearby ->
                if (d.startsWith("no peers") || d.startsWith("0 peers") || d.startsWith("listening")) PathState.Waiting else PathState.Up
            // A LoRa link describes the node ("client, EU_868 …") once it answers.
            kind == PathInfo.Kind.LoRa ->
                if (d.startsWith("disconnected") || d.startsWith("connecting") || d.startsWith("error")) PathState.Waiting else PathState.Up
            d.startsWith("connected") || d.startsWith("syncing") -> PathState.Up
            else -> PathState.Waiting
        }
    }
}
