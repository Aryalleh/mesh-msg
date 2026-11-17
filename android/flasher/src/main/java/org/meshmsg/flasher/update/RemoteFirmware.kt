package org.meshmsg.flasher.update

import android.util.Base64
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.security.MessageDigest
import java.util.zip.ZipInputStream

/**
 * The newest board firmware from GitHub, so the flasher needs no new APK for
 * every release. Each release publishes mava-board-images-<version>.zip and a
 * release.json signed with the project's release key (core/update/update.go:
 * {"manifest": base64(json), "sig": base64(Ed25519(SignContext || json))}).
 * Only a manifest that verifies is used, and the zip must match its SHA-256
 * and size. The images land in [dir]/<version>/.
 */
class RemoteFirmware(private val dir: File) {
    data class Release(val version: String, val zipName: String, val url: String, val sha256: String, val size: Long)

    /** The newest signed release that carries board images, or an error. */
    fun latest(): Release {
        val rels = JSONArray(get(RELEASES, 4 shl 20).toString(Charsets.UTF_8))
        val list = (0 until rels.length()).map { rels.getJSONObject(it) }
            .filter { !it.optBoolean("draft") }
            .sortedWith { a, b -> Versions.compare(b.optString("tag_name"), a.optString("tag_name")) }
        var last: Exception? = null
        for (r in list.take(5)) {
            val assets = r.optJSONArray("assets") ?: continue
            val manifestUrl = (0 until assets.length()).map { assets.getJSONObject(it) }
                .firstOrNull { it.optString("name") == "release.json" }?.optString("browser_download_url") ?: continue
            try {
                val m = verify(get(manifestUrl, 1 shl 20))
                val version = m.getString("version")
                val files = m.getJSONArray("files")
                for (i in 0 until files.length()) {
                    val f = files.getJSONObject(i)
                    if (f.getString("name") == "mava-board-images-$version.zip") {
                        return Release(version, f.getString("name"), f.getString("url"), f.getString("sha256").lowercase(), f.optLong("size"))
                    }
                }
                last = IOException("release $version has no board images (made before 0.1.1-beta23)")
            } catch (e: Exception) {
                last = e
            }
        }
        throw last ?: IOException("no signed release found on GitHub")
    }

    /** The images of [r] on this phone, downloading and unpacking them if needed. */
    fun fetch(r: Release, progress: (Float) -> Unit = {}): File {
        val out = File(dir, r.version)
        if (File(out, "images.txt").isFile) return out
        dir.mkdirs()
        val zip = File(dir, r.zipName + ".part")
        download(r, zip, progress)
        val tmp = File(dir, r.version + ".tmp").apply { deleteRecursively(); mkdirs() }
        ZipInputStream(zip.inputStream().buffered()).use { z ->
            while (true) {
                val e = z.nextEntry ?: break
                val name = e.name
                if (e.isDirectory || name.contains('/') || name.contains('\\') || name.startsWith(".")) continue
                File(tmp, name).outputStream().use { z.copyTo(it) }
            }
        }
        zip.delete()
        if (!File(tmp, "images.txt").isFile) throw IOException("${r.zipName}: no images.txt")
        out.deleteRecursively()
        if (!tmp.renameTo(out)) throw IOException("cannot store the firmware")
        // Keep only this version.
        dir.listFiles()?.filter { it.name != r.version }?.forEach { it.deleteRecursively() }
        return out
    }

    private fun download(r: Release, to: File, progress: (Float) -> Unit) {
        val c = open(r.url)
        try {
            val sha = MessageDigest.getInstance("SHA-256")
            var n = 0L
            c.inputStream.use { inp ->
                to.outputStream().use { out ->
                    val buf = ByteArray(64 * 1024)
                    while (true) {
                        val k = inp.read(buf)
                        if (k < 0) break
                        n += k
                        if (r.size > 0 && n > r.size) throw IOException("${r.zipName}: larger than the signed size")
                        sha.update(buf, 0, k)
                        out.write(buf, 0, k)
                        if (r.size > 0) progress(n.toFloat() / r.size)
                    }
                }
            }
            val got = sha.digest().joinToString("") { "%02x".format(it) }
            if (got != r.sha256 || (r.size > 0 && n != r.size)) {
                to.delete()
                throw IOException("${r.zipName}: does not match the signed release (SHA-256)")
            }
        } finally {
            c.disconnect()
        }
    }

    companion object {
        const val RELEASES = "https://api.github.com/repos/Aryalleh/mesh-msg/releases?per_page=10"
        // core/update/update.go ReleaseKeyHex / SignContext.
        private const val RELEASE_KEY = "4a89de4b477138723c6f8a2084c4f52dd843471b3869d465e9a9e402d7395626"
        private const val SIGN_CONTEXT = "meshmsg-release-manifest-v1\n"

        /** Checks release.json and returns its manifest. */
        fun verify(envelope: ByteArray): JSONObject {
            val e = JSONObject(envelope.toString(Charsets.UTF_8))
            val body = Base64.decode(e.getString("manifest"), Base64.DEFAULT)
            val sig = Base64.decode(e.getString("sig"), Base64.DEFAULT)
            if (!verifyBytes(body, sig)) throw IOException("release.json: the signature does not verify")
            return JSONObject(body.toString(Charsets.UTF_8))
        }

        fun verifyBytes(body: ByteArray, sig: ByteArray, keyHex: String = RELEASE_KEY): Boolean {
            val key = keyHex.chunked(2).map { it.toInt(16).toByte() }.toByteArray()
            return Ed25519.verify(key, SIGN_CONTEXT.toByteArray() + body, sig)
        }

        private fun open(u: String): HttpURLConnection {
            val c = URL(u).openConnection() as HttpURLConnection
            c.connectTimeout = 20_000
            c.readTimeout = 60_000
            c.instanceFollowRedirects = true
            c.setRequestProperty("User-Agent", "mava-flasher")
            if (c.responseCode != 200) {
                val code = c.responseCode
                c.disconnect()
                throw IOException("${URL(u).host}: HTTP $code")
            }
            return c
        }

        private fun get(u: String, limit: Int): ByteArray {
            val c = open(u)
            try {
                val b = c.inputStream.use { it.readNBytesCompat(limit + 1) }
                if (b.size > limit) throw IOException("${URL(u).host}: response too large")
                return b
            } finally {
                c.disconnect()
            }
        }

        private fun java.io.InputStream.readNBytesCompat(max: Int): ByteArray {
            val out = java.io.ByteArrayOutputStream()
            val buf = ByteArray(16 * 1024)
            while (out.size() < max) {
                val k = read(buf, 0, minOf(buf.size, max - out.size()))
                if (k < 0) break
                out.write(buf, 0, k)
            }
            return out.toByteArray()
        }
    }
}

/** Version tags like v0.1.1-beta22: newer compares greater. */
object Versions {
    private fun parts(v: String): Pair<List<Int>, String?> {
        val s = v.removePrefix("v")
        val main = s.substringBefore('-')
        val pre = if ('-' in s) s.substringAfter('-') else null
        return main.split('.').map { it.toIntOrNull() ?: -1 } to pre
    }

    fun compare(a: String, b: String): Int {
        val (am, ap) = parts(a)
        val (bm, bp) = parts(b)
        for (i in 0 until maxOf(am.size, bm.size)) {
            val d = (am.getOrNull(i) ?: 0).compareTo(bm.getOrNull(i) ?: 0)
            if (d != 0) return d
        }
        if (ap == null || bp == null) return if (ap == bp) 0 else if (ap == null) 1 else -1
        // alpha3 < beta2 < beta10 < rc1: name first, then the number.
        val an = ap.takeWhile { !it.isDigit() }
        val bn = bp.takeWhile { !it.isDigit() }
        if (an != bn) return an.compareTo(bn)
        return (ap.drop(an.length).toIntOrNull() ?: 0).compareTo(bp.drop(bn.length).toIntOrNull() ?: 0)
    }
}
