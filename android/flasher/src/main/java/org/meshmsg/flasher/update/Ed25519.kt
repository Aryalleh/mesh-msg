package org.meshmsg.flasher.update

import java.math.BigInteger
import java.security.MessageDigest

/**
 * Ed25519 signature verification (RFC 8032 §5.1.7), for the signed release
 * manifest. Android has Ed25519 only from API 33 and this app runs from 26.
 * It checks one signature per firmware check, so plain BigInteger arithmetic
 * is fast enough. Verification only: no secret is ever handled here.
 */
object Ed25519 {
    private val TWO = BigInteger.valueOf(2) // BigInteger.TWO is API 33
    private val P = BigInteger.ONE.shiftLeft(255) - BigInteger.valueOf(19)
    private val L = BigInteger.ONE.shiftLeft(252) + BigInteger("27742317777372353535851937790883648493")
    private val D = BigInteger.valueOf(-121665).mod(P) * BigInteger.valueOf(121666).modInverse(P) % P
    private val SQRT_M1 = TWO.modPow((P - BigInteger.ONE).shiftRight(2), P)
    private val B: Point = run {
        val y = BigInteger.valueOf(4) * BigInteger.valueOf(5).modInverse(P) % P
        Point(recoverX(y, false)!!, y)
    }

    /** Extended coordinates (X, Y, Z, T), x = X/Z, y = Y/Z, xy = T/Z. */
    private class Point(val x: BigInteger, val y: BigInteger, val z: BigInteger = BigInteger.ONE, val t: BigInteger = x * y % P)

    private fun add(a: Point, b: Point): Point {
        val aa = (a.y - a.x) * (b.y - b.x) % P
        val bb = (a.y + a.x) * (b.y + b.x) % P
        val c = TWO * a.t * b.t * D % P
        val dd = TWO * a.z * b.z % P
        val e = bb - aa
        val f = dd - c
        val g = dd + c
        val h = bb + aa
        return Point(e * f % P, g * h % P, f * g % P, e * h % P)
    }

    private fun mul(s: BigInteger, p: Point): Point {
        var q = Point(BigInteger.ZERO, BigInteger.ONE, BigInteger.ONE, BigInteger.ZERO)
        var r = p
        var k = s
        while (k.signum() > 0) {
            if (k.testBit(0)) q = add(q, r)
            r = add(r, r)
            k = k.shiftRight(1)
        }
        return q
    }

    private fun equal(a: Point, b: Point): Boolean =
        (a.x * b.z - b.x * a.z).mod(P).signum() == 0 && (a.y * b.z - b.y * a.z).mod(P).signum() == 0

    private fun recoverX(y: BigInteger, sign: Boolean): BigInteger? {
        if (y >= P) return null
        val x2 = (y * y - BigInteger.ONE) * (D * y * y + BigInteger.ONE).modInverse(P) % P
        if (x2.mod(P).signum() == 0) return if (sign) null else BigInteger.ZERO
        var x = x2.mod(P).modPow((P + BigInteger.valueOf(3)).shiftRight(3), P)
        if ((x * x - x2).mod(P).signum() != 0) x = x * SQRT_M1 % P
        if ((x * x - x2).mod(P).signum() != 0) return null
        if (x.testBit(0) != sign) x = P - x
        return x
    }

    private fun le(b: ByteArray): BigInteger = BigInteger(1, b.reversedArray())

    private fun decode(b: ByteArray): Point? {
        if (b.size != 32) return null
        val sign = (b[31].toInt() and 0x80) != 0
        val c = b.copyOf()
        c[31] = (c[31].toInt() and 0x7f).toByte()
        val y = le(c)
        val x = recoverX(y, sign) ?: return null
        return Point(x, y)
    }

    fun verify(publicKey: ByteArray, message: ByteArray, signature: ByteArray): Boolean {
        if (publicKey.size != 32 || signature.size != 64) return false
        val a = decode(publicKey) ?: return false
        val rBytes = signature.copyOfRange(0, 32)
        val r = decode(rBytes) ?: return false
        val s = le(signature.copyOfRange(32, 64))
        if (s >= L) return false
        val sha = MessageDigest.getInstance("SHA-512")
        sha.update(rBytes)
        sha.update(publicKey)
        sha.update(message)
        val h = le(sha.digest()).mod(L)
        return equal(mul(s, B), add(r, mul(h, a)))
    }
}
