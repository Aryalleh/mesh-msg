package org.meshmsg.flasher

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Assert.fail
import org.junit.Test
import org.meshmsg.flasher.esp.SerialLink
import java.io.File

class HostLinkTest {
    private fun hex(s: String) = ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }

    /** The frames of spec/test-vectors/vectors.json, shared with Go and the firmware. */
    @Test
    fun matchesTheSharedVectors() {
        val json = File("../../spec/test-vectors/vectors.json").readText()
        val section = json.substring(json.indexOf("\"hostlink\""))
        val re = Regex("""\{\s*"kind":\s*(\d+),\s*"payload":\s*"([0-9a-f]*)",\s*"encoded":\s*"([0-9a-f]+)"\s*\}""")
        val frames = re.findAll(section).toList()
        assertTrue("no frames found", frames.isNotEmpty())
        for (m in frames) {
            val kind = m.groupValues[1].toInt()
            val payload = hex(m.groupValues[2])
            val encoded = hex(m.groupValues[3])
            assertArrayEquals(encoded, HostLink.encode(kind, payload))
            val back = HostLink.decode(encoded.copyOf(encoded.size - 1))!!
            assertEquals(kind, back.first)
            assertArrayEquals(payload, back.second)
        }
    }

    @Test
    fun wifiArgumentsAsTheFirmwareReadsThem() {
        // Same bytes as firmware/test/node_test.cpp wifi_commands and core/node's test.
        assertArrayEquals("\u0001\u0004home\u0002\u0008secret12".toByteArray(), HostLink.wifiTlv("home", "secret12", null))
        assertArrayEquals(byteArrayOf(1, 0), HostLink.wifiTlv("", null, null))
        for (bad in listOf<() -> Unit>(
            { HostLink.wifiTlv(null, null, null) },
            { HostLink.wifiTlv("x", "short", null) },
            { HostLink.wifiTlv("x".repeat(33), null, null) },
            { HostLink.wifiTlv(null, null, "ftp://x") },
        )) {
            try {
                bad()
                fail("accepted")
            } catch (e: IllegalArgumentException) {
                // expected
            }
        }
    }

    /** A board that prints log text, then answers each command with an ACK. */
    private class FakeBoard(val status: Int) : SerialLink {
        override val nativeUsb = true
        val out = ArrayDeque<Byte>().apply { "boot: Wi-Fi\r\nhotspot Mava-1A2B\r\n".toByteArray().forEach { addLast(it) } }
        var got: Pair<Int, ByteArray>? = null
        override fun write(data: ByteArray) {
            val f = HostLink.decode(data.copyOf(data.size - 1))!!
            got = f
            val p = f.second
            // An unrelated event first, then the ACK.
            HostLink.encode(HostLink.KIND_EVENT, byteArrayOf(0x85.toByte(), 0, 'h'.code.toByte())).forEach { out.addLast(it) }
            HostLink.encode(HostLink.KIND_EVENT, byteArrayOf(HostLink.EV_ACK.toByte(), p[1], p[0], status.toByte()))
                .forEach { out.addLast(it) }
        }
        override fun read(buf: ByteArray, timeoutMs: Int): Int {
            var n = 0
            while (n < buf.size && out.isNotEmpty()) buf[n++] = out.removeFirst()
            return n
        }
        override fun setLines(dtr: Boolean, rts: Boolean) {}
        override fun setBaud(baud: Int) {}
    }

    @Test
    fun boardLinkSendsAndReadsTheAck() {
        val board = FakeBoard(HostLink.ACK_OK)
        val args = HostLink.wifiTlv("home", "secret12", null)
        assertEquals(HostLink.ACK_OK, BoardLink(board).command(HostLink.OP_SET_WIFI, args))
        val (kind, p) = board.got!!
        assertEquals(HostLink.KIND_COMMAND, kind)
        assertEquals(HostLink.OP_SET_WIFI, p[0].toInt())
        assertArrayEquals(args, p.copyOfRange(2, p.size))
        assertEquals(HostLink.ACK_UNKNOWN_OP, BoardLink(FakeBoard(HostLink.ACK_UNKNOWN_OP)).command(HostLink.OP_HOTSPOT, byteArrayOf(1)))
    }
}
