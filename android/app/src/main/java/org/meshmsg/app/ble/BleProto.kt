package org.meshmsg.app.ble

import java.util.UUID

/**
 * The phone-to-phone BLE protocol (docs/android.md §4), the parts that are
 * pure logic and unit-tested.
 *
 * Every phone advertises [SERVICE] plus 4 random bytes (a session ID, new on
 * every start, unrelated to the user's identity) and scans for the same. Of
 * two phones that see each other, the one with the larger session ID connects
 * as GATT client, so a pair never ends up with two connections.
 *
 *   client → server: writes (with response) to [RX]
 *   server → client: notifications on [TX]
 *
 * The first write is a hello carrying the client's session ID; after it the
 * stream in both directions is the chunked packet stream of mobile/meshcore.go.
 */
object BleProto {
    // "meshmsg" in ASCII in the first bytes, then the Bluetooth base UUID tail.
    val SERVICE: UUID = UUID.fromString("6d657368-6d73-6700-8000-00805f9b34fb")
    val RX: UUID = UUID.fromString("6d657368-6d73-6701-8000-00805f9b34fb")
    val TX: UUID = UUID.fromString("6d657368-6d73-6702-8000-00805f9b34fb")
    val CCCD: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")

    /** Bluetooth SIG "no company" test ID: the advert carries no vendor identity. */
    const val MANUFACTURER_ID = 0xFFFF
    const val SESSION_BYTES = 4
    const val HELLO_MAGIC: Byte = 0x4D
    const val LINK_NAME = "ble"

    /** Chunk size the Go core frames for; smaller MTUs are split further here. */
    const val CORE_CHUNK = 180
    const val WANTED_MTU = 247
    const val DEFAULT_MTU = 23
    const val MAX_PEERS = 4

    /** Bytes waiting for one peer before it is considered stuck and dropped. */
    const val MAX_QUEUED_BYTES = 2 * 1024 * 1024

    fun peerId(session: ByteArray): String = session.joinToString("") { "%02x".format(it) }

    fun hello(session: ByteArray): ByteArray = byteArrayOf(HELLO_MAGIC) + session

    /** The session ID in a hello, or null if it is not one. */
    fun parseHello(b: ByteArray): ByteArray? =
        if (b.size == 1 + SESSION_BYTES && b[0] == HELLO_MAGIC) b.copyOfRange(1, b.size) else null

    /** Unsigned, big-endian comparison of two session IDs. */
    fun compare(a: ByteArray, b: ByteArray): Int {
        for (i in 0 until minOf(a.size, b.size)) {
            val d = (a[i].toInt() and 0xff) - (b[i].toInt() and 0xff)
            if (d != 0) return d
        }
        return a.size - b.size
    }

    /** Whether this phone is the one that connects (see the class comment). */
    fun shouldConnect(mine: ByteArray, theirs: ByteArray): Boolean =
        theirs.size == SESSION_BYTES && compare(mine, theirs) > 0

    /** Payload bytes per write / notification for an ATT MTU. */
    fun payload(mtu: Int): Int = (mtu - 3).coerceIn(DEFAULT_MTU - 3, 512)

    /** Splits a chunk from the core to fit the peer's MTU, keeping order. */
    fun split(chunk: ByteArray, mtu: Int): List<ByteArray> {
        val size = payload(mtu)
        if (chunk.size <= size) return listOf(chunk)
        return (chunk.indices step size).map { chunk.copyOfRange(it, minOf(it + size, chunk.size)) }
    }
}
