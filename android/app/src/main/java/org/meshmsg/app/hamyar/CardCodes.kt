package org.meshmsg.app.hamyar

import javax.crypto.Mac
import javax.crypto.spec.SecretKeySpec

/**
 * The virtual student card's codes (Hamyar worker src/lib/student-card.ts, docs/student-card.md in the Hamyar repo):
 * "HYC1.<slug>.<user id>.<w>.<HMAC-SHA256(card key, "card|<w>")[:10] hex>", w = floor(unix / 30). Made on the phone,
 * so the card works offline; shown as a QR code and answered over NFC (HCE, AID F0484D59415231).
 */
object CardCodes {
    const val WINDOW_SEC = 30L
    const val AID_HEX = "F0484D59415231"
    val AID: ByteArray = PresenceCodec.unhex(AID_HEX.lowercase())!!
    val OK = byteArrayOf(0x90.toByte(), 0x00)
    val NOT_FOUND = byteArrayOf(0x6A, 0x82.toByte())
    private val CODE = Regex("""^HYC1\.[a-z0-9-]{1,40}\.\d{1,12}\.\d{1,12}\.[0-9a-f]{20}$""")

    fun window(unixSec: Long) = Math.floorDiv(unixSec, WINDOW_SEC)

    fun token(key: ByteArray, w: Long): String {
        val mac = Mac.getInstance("HmacSHA256").apply { init(SecretKeySpec(key, "HmacSHA256")) }
        return PresenceCodec.hex(mac.doFinal("card|$w".toByteArray()).copyOf(10))
    }

    fun code(key: ByteArray, slug: String, userId: Long, w: Long) = "HYC1.$slug.$userId.$w.${token(key, w)}"

    /** This moment's code for the card, or null (card off, or an old card without its user id). */
    fun current(card: StudentCard, nowSec: Long = System.currentTimeMillis() / 1000): String? {
        val key = card.key ?: return null
        if (card.status != "active" || card.userId <= 0 || card.slug.isEmpty()) return null
        return code(key, card.slug, card.userId, window(nowSec))
    }

    /** Seconds until the code changes. */
    fun secondsLeft(nowSec: Long = System.currentTimeMillis() / 1000) = WINDOW_SEC - Math.floorMod(nowSec, WINDOW_SEC)

    fun looksLikeCode(s: String) = CODE.matches(s.trim())

    /** ISO 7816 SELECT by AID: 00 A4 04 00 Lc AID (Le). */
    fun selectApdu(): ByteArray = byteArrayOf(0x00, 0xA4.toByte(), 0x04, 0x00, AID.size.toByte()) + AID + byteArrayOf(0x00)

    fun isSelectOfOurAid(apdu: ByteArray): Boolean {
        if (apdu.size < 5 + AID.size || apdu[0] != 0x00.toByte() || apdu[1] != 0xA4.toByte() || apdu[2] != 0x04.toByte()) return false
        val lc = apdu[4].toInt() and 0xff
        return lc == AID.size && apdu.copyOfRange(5, 5 + lc).contentEquals(AID)
    }

    /** The card's answer: the code's ASCII bytes + 90 00, or "not found" when there is no card on this phone. */
    fun answer(apdu: ByteArray, card: StudentCard?, nowSec: Long = System.currentTimeMillis() / 1000): ByteArray {
        if (!isSelectOfOurAid(apdu)) return NOT_FOUND
        val code = card?.let { current(it, nowSec) } ?: return NOT_FOUND
        return code.toByteArray(Charsets.US_ASCII) + OK
    }

    /** Reads a tapped card's answer: the code, or null when it isn't a Hamyar card. */
    fun parseAnswer(r: ByteArray?): String? {
        if (r == null || r.size < 3 || !r.copyOfRange(r.size - 2, r.size).contentEquals(OK)) return null
        return String(r, 0, r.size - 2, Charsets.US_ASCII).takeIf(::looksLikeCode)
    }
}
