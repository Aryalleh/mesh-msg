package org.meshmsg.app.data

import androidx.compose.runtime.Immutable
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant

// UI models, parsed from the JSON the Go core returns (the same JSON meshd
// serves). All immutable, so Compose can skip unchanged rows.

@Immutable
data class Contact(val hash: String, val name: String, val uri: String) {
    /** Name to show: the saved name, or a short form of the hash. */
    val displayName: String get() = name.ifBlank { hash.take(8) }
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
) {
    /** When the user saw it happen on this phone. */
    val timeMs: Long get() = if (outgoing) sentMs else receivedMs
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

object Json {
    private fun time(s: String): Long = runCatching { Instant.parse(s).toEpochMilli() }.getOrDefault(0L)

    fun contact(o: JSONObject) = Contact(o.optString("hash"), o.optString("name"), o.optString("uri"))

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
        )
    }

    fun messages(json: String, linkUp: Boolean): List<Message> {
        val a = JSONArray(json)
        return List(a.length()) { message(a.getJSONObject(it), linkUp) }
    }

    fun chats(json: String, linkUp: Boolean): List<Chat> {
        val a = JSONArray(json)
        return List(a.length()) {
            val o = a.getJSONObject(it)
            Chat(contact(o.getJSONObject("contact")), o.optJSONObject("last")?.let { m -> message(m, linkUp) }, o.optInt("unread"))
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
