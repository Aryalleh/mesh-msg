package org.meshmsg.boardlink

import java.io.ByteArrayOutputStream
import java.io.IOException

/**
 * The node host link (docs/firmware.md "Host link framing"), just enough to
 * send the board commands over USB: the Wi-Fi settings and the setup hotspot.
 *   COBS( kind | len(2 BE) | payload | crc16(2 BE) ) 0x00, CRC-16/CCITT-FALSE
 *   command payload: op | req | args; the node answers ACK: op | status
 * The same bytes as core/hostlink (Go) and firmware/lib/meshnode (C++).
 */
object HostLink {
    const val KIND_COMMAND = 1
    const val KIND_EVENT = 2
    const val OP_SET_WIFI = 0x09
    const val OP_HOTSPOT = 0x0A
    const val EV_ACK = 0x84
    const val ACK_OK = 0
    const val ACK_BAD_ARGS = 1
    const val ACK_UNKNOWN_OP = 2
    const val ACK_NOT_ALLOWED = 3
    const val TAG_SSID = 1
    const val TAG_PASSWORD = 2
    const val TAG_RELAY = 3

    fun crc16(b: ByteArray, off: Int = 0, len: Int = b.size): Int {
        var crc = 0xFFFF
        for (i in off until off + len) {
            crc = crc xor ((b[i].toInt() and 0xFF) shl 8)
            repeat(8) { crc = if (crc and 0x8000 != 0) (crc shl 1) xor 0x1021 else crc shl 1 }
            crc = crc and 0xFFFF
        }
        return crc
    }

    fun cobsEncode(input: ByteArray): ByteArray {
        val out = ByteArrayOutputStream(input.size + input.size / 254 + 2)
        val block = ByteArrayOutputStream()
        fun flush(code: Int) {
            out.write(code)
            block.writeTo(out)
            block.reset()
        }
        for (b in input) {
            if (b.toInt() == 0) {
                flush(block.size() + 1)
                continue
            }
            block.write(b.toInt())
            if (block.size() == 254) flush(255)
        }
        flush(block.size() + 1)
        return out.toByteArray()
    }

    fun cobsDecode(input: ByteArray): ByteArray? {
        val out = ByteArrayOutputStream(input.size)
        var i = 0
        while (i < input.size) {
            val code = input[i].toInt() and 0xFF
            if (code == 0 || i + code > input.size) return null
            out.write(input, i + 1, code - 1)
            i += code
            if (code < 0xFF && i < input.size) out.write(0)
        }
        return out.toByteArray()
    }

    /** One frame, with its trailing 0x00 delimiter. */
    fun encode(kind: Int, payload: ByteArray): ByteArray {
        val raw = ByteArray(5 + payload.size)
        raw[0] = kind.toByte()
        raw[1] = (payload.size shr 8).toByte()
        raw[2] = payload.size.toByte()
        payload.copyInto(raw, 3)
        val crc = crc16(raw, 0, 3 + payload.size)
        raw[3 + payload.size] = (crc shr 8).toByte()
        raw[4 + payload.size] = crc.toByte()
        return cobsEncode(raw) + byteArrayOf(0)
    }

    /** kind to payload, or null if the bytes between two 0x00 aren't a frame (log text, noise). */
    fun decode(encoded: ByteArray): Pair<Int, ByteArray>? {
        val raw = cobsDecode(encoded) ?: return null
        if (raw.size < 5) return null
        val len = ((raw[1].toInt() and 0xFF) shl 8) or (raw[2].toInt() and 0xFF)
        if (raw.size != 5 + len) return null
        val want = ((raw[3 + len].toInt() and 0xFF) shl 8) or (raw[4 + len].toInt() and 0xFF)
        if (crc16(raw, 0, 3 + len) != want) return null
        return (raw[0].toInt() and 0xFF) to raw.copyOfRange(3, 3 + len)
    }

    /**
     * SET_WIFI arguments; null fields stay as they are on the board. Checked
     * as the firmware checks them: SSID up to 32 bytes, password empty or
     * 8–63 bytes, relay http(s):// up to 95 bytes.
     */
    fun wifiTlv(ssid: String?, password: String?, relay: String?): ByteArray {
        val out = ByteArrayOutputStream()
        fun put(tag: Int, v: String?, max: Int, what: String) {
            if (v == null) return
            val b = v.toByteArray(Charsets.UTF_8)
            require(b.size <= max) { "$what is longer than $max bytes" }
            require(b.none { it.toInt() == 0 }) { "$what contains a NUL byte" }
            out.write(tag)
            out.write(b.size)
            out.write(b)
        }
        require(ssid != null || password != null || relay != null) { "nothing to set" }
        if (password != null) {
            val n = password.toByteArray(Charsets.UTF_8).size
            require(n == 0 || n >= 8) { "a Wi-Fi password has 8–63 characters (or none for an open network)" }
        }
        if (!relay.isNullOrEmpty()) require(relay.startsWith("https://") || relay.startsWith("http://")) { "the relay address starts with https://" }
        put(TAG_SSID, ssid, 32, "the network name")
        put(TAG_PASSWORD, password, 63, "the password")
        put(TAG_RELAY, relay, 95, "the relay address")
        return out.toByteArray()
    }
}

/**
 * Sends host link commands to the board over its USB port and waits for the
 * ACK. Anything else on the port (log text before the first frame, events,
 * packets) is skipped.
 */
class BoardLink(private val port: SerialLink) {
    private var req = 0
    private val buf = ByteArray(4096)
    private val pending = ByteArrayOutputStream()

    /** Returns the ACK status (HostLink.ACK_*). */
    fun command(op: Int, args: ByteArray, timeoutMs: Int = 4000): Int {
        req = (req % 255) + 1
        val payload = byteArrayOf(op.toByte(), req.toByte()) + args
        port.write(HostLink.encode(HostLink.KIND_COMMAND, payload))
        val deadline = System.nanoTime() + timeoutMs * 1_000_000L
        while (System.nanoTime() < deadline) {
            val n = port.read(buf, 100)
            for (i in 0 until n) {
                val b = buf[i]
                if (b.toInt() != 0) {
                    pending.write(b.toInt())
                    continue
                }
                val f = HostLink.decode(pending.toByteArray())
                pending.reset()
                if (f == null || f.first != HostLink.KIND_EVENT) continue
                val p = f.second
                if (p.size >= 4 && (p[0].toInt() and 0xFF) == HostLink.EV_ACK && (p[1].toInt() and 0xFF) == req &&
                    (p[2].toInt() and 0xFF) == op
                ) {
                    return p[3].toInt() and 0xFF
                }
            }
        }
        throw IOException("the board did not answer (is it running the Mava firmware 0.1.1-beta9 or later?)")
    }
}
