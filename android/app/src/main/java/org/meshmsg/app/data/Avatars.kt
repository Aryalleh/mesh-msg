package org.meshmsg.app.data

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.staticCompositionLocalOf
import java.io.ByteArrayOutputStream
import java.io.File
import org.json.JSONArray
import org.json.JSONObject

/** Profile pictures on this phone: chat hash ("me" for this account) → file + version. */
@Immutable
data class AvatarFile(val path: String, val version: Long)

val LocalAvatars = staticCompositionLocalOf<Map<String, AvatarFile>> { emptyMap() }

/** A user-made chat folder (built-in views are filtered in the UI). */
@Immutable
data class ChatFolder(val name: String, val chats: List<String>)

object Folders {
    fun parse(json: String): List<ChatFolder> = runCatching {
        val a = JSONArray(json)
        List(a.length()) { i ->
            val o = a.getJSONObject(i)
            val c = o.optJSONArray("chats") ?: JSONArray()
            ChatFolder(o.optString("name"), List(c.length()) { c.getString(it) })
        }
    }.getOrDefault(emptyList())

    fun json(fs: List<ChatFolder>): String = JSONArray(
        fs.map { f -> JSONObject().put("name", f.name).put("chats", JSONArray(f.chats)) },
    ).toString()
}

object AvatarImage {
    const val MAX_BYTES = 60_000

    fun file(path: String): AvatarFile? =
        File(path).takeIf { path.isNotEmpty() && it.isFile }?.let { AvatarFile(path, it.lastModified()) }

    /** Center-crops and shrinks a picture to a square JPEG of at most [MAX_BYTES]. */
    fun shrink(bytes: ByteArray): ByteArray {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, bounds)
        require(bounds.outWidth > 0) { "not an image" }
        var sample = 1
        while (minOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= 384) sample *= 2
        val src = BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })
            ?: error("not an image")
        val side = minOf(src.width, src.height)
        val square = Bitmap.createBitmap(src, (src.width - side) / 2, (src.height - side) / 2, side, side)
        for (px in intArrayOf(384, 256, 192, 128)) {
            val scaled = Bitmap.createScaledBitmap(square, minOf(px, side), minOf(px, side), true)
            for (q in intArrayOf(85, 70, 55)) {
                val out = ByteArrayOutputStream()
                scaled.compress(Bitmap.CompressFormat.JPEG, q, out)
                if (out.size() <= MAX_BYTES) return out.toByteArray()
            }
        }
        error("picture too large")
    }
}
