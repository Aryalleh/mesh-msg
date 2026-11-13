package org.meshmsg.app.data

import androidx.compose.runtime.Immutable
import org.json.JSONArray
import org.json.JSONObject

/** One file kept on this phone (core StoredFile). */
@Immutable
data class StoredFile(
    val id: String,
    val chat: String,
    val kind: String,
    val name: String,
    val bytes: Long,
    val createdMs: Long,
    val outgoing: Boolean,
)

/** What the storage screen deletes: chats (empty: all), kinds (empty: all), a time range (0: open). */
@Immutable
data class StoragePick(
    val chats: Set<String>? = null,
    val kinds: Set<String> = emptySet(),
    val fromMs: Long = 0,
    val toMs: Long = 0,
) {
    fun matches(f: StoredFile): Boolean =
        (chats == null || f.chat in chats) &&
            (kinds.isEmpty() || f.kind in kinds) &&
            (fromMs == 0L || f.createdMs >= fromMs) &&
            (toMs == 0L || f.createdMs <= toMs)
}

object StorageJson {
    fun files(s: String): List<StoredFile> {
        if (s.isBlank() || s == "null") return emptyList()
        val a = JSONArray(s)
        return (0 until a.length()).map { i ->
            val o = a.getJSONObject(i)
            StoredFile(
                id = o.optString("id"), chat = o.optString("chat"), kind = o.optString("kind"), name = o.optString("name"),
                bytes = o.optLong("bytes"), createdMs = Json.time(o.optString("created")), outgoing = o.optBoolean("outgoing"),
            )
        }
    }

    fun freed(s: String): Long = JSONObject(s).optLong("bytes")
}
