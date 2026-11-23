package org.meshmsg.app.hamyar

import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * Hamyar presence tokens and adverts (docs/presence.md v1), byte-compatible with the Hamyar server and the board
 * firmware (firmware/lib/meshpresence); pinned to spec/test-vectors/presence.json.
 *
 *  token  = first 8 bytes of HMAC-SHA256(key, "beacon|<w>", "phone|<w>" or "trip|<w>"), w = unix seconds / 120
 *  advert = manufacturer 0xFFFF, payload "M" | "V" (node), "P" (phone) or "T" (trip group member) | version 1 | flags 0
 *           | id u32 BE (node id, 0, or the trip group id) | token
 */
object PresenceCodec {
    const val WINDOW_SEC = 120L
    const val MANUFACTURER = 0xFFFF
    const val TOKEN_LEN = 8
    const val ADV_LEN = 16
    private const val VERSION: Byte = 1

    fun window(unixSec: Long): Long = unixSec / WINDOW_SEC

    private fun token(key: ByteArray, what: String, w: Long): ByteArray {
        val mac = Mac.getInstance("HmacSHA256")
        mac.init(SecretKeySpec(key, "HmacSHA256"))
        return mac.doFinal("$what|$w".toByteArray(Charsets.US_ASCII)).copyOf(TOKEN_LEN)
    }

    fun beaconToken(key: ByteArray, w: Long) = token(key, "beacon", w)
    fun phoneToken(key: ByteArray, w: Long) = token(key, "phone", w)
    fun tripToken(key: ByteArray, w: Long) = token(key, "trip", w)

    fun adv(kind: Char, id: Long, token: ByteArray): ByteArray {
        require(token.size == TOKEN_LEN)
        val b = ByteArray(ADV_LEN)
        b[0] = 'M'.code.toByte(); b[1] = kind.code.toByte(); b[2] = VERSION; b[3] = 0
        b[4] = (id ushr 24).toByte(); b[5] = (id ushr 16).toByte(); b[6] = (id ushr 8).toByte(); b[7] = id.toByte()
        token.copyInto(b, 8)
        return b
    }

    data class Adv(val kind: Char, val id: Long, val token: ByteArray)

    fun parse(p: ByteArray?): Adv? {
        if (p == null || p.size < ADV_LEN || p[0] != 'M'.code.toByte() || p[2] != VERSION) return null
        val kind = p[1].toInt().toChar()
        if (kind != 'V' && kind != 'P' && kind != 'T') return null
        val id = ((p[4].toLong() and 0xff) shl 24) or ((p[5].toLong() and 0xff) shl 16) or ((p[6].toLong() and 0xff) shl 8) or (p[7].toLong() and 0xff)
        return Adv(kind, id, p.copyOfRange(8, 16))
    }

    fun hex(b: ByteArray): String = b.joinToString("") { "%02x".format(it.toInt() and 0xff) }

    fun unhex(s: String): ByteArray? {
        if (s.length % 2 != 0 || !s.all { it in '0'..'9' || it in 'a'..'f' || it in 'A'..'F' }) return null
        return ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }
    }
}
