package org.meshmsg.app

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.meshmsg.app.ble.BleProto

class BleProtoTest {
    private fun b(vararg v: Int) = ByteArray(v.size) { v[it].toByte() }

    @Test fun helloRoundTrip() {
        val s = b(0x01, 0xff, 0x80, 0x00)
        assertArrayEquals(s, BleProto.parseHello(BleProto.hello(s)))
        assertNull(BleProto.parseHello(b(0x4d, 1, 2, 3)))        // short
        assertNull(BleProto.parseHello(b(0x4d, 1, 2, 3, 4, 5)))  // long
        assertNull(BleProto.parseHello(b(0x00, 1, 2, 3, 4)))     // wrong magic
        assertEquals("01ff8000", BleProto.peerId(s))
    }

    @Test fun compareIsUnsigned() {
        assertTrue(BleProto.compare(b(0x80, 0, 0, 0), b(0x7f, 0xff, 0xff, 0xff)) > 0)
        assertTrue(BleProto.compare(b(0, 0, 0, 1), b(0, 0, 0, 2)) < 0)
        assertEquals(0, BleProto.compare(b(1, 2, 3, 4), b(1, 2, 3, 4)))
    }

    /** Of any two phones exactly one connects, so a pair never gets two links. */
    @Test fun exactlyOneSideConnects() {
        val rnd = java.util.Random(7)
        repeat(2000) {
            val x = ByteArray(4).also(rnd::nextBytes)
            val y = ByteArray(4).also(rnd::nextBytes)
            if (!x.contentEquals(y)) {
                assertTrue(BleProto.shouldConnect(x, y, false) != BleProto.shouldConnect(y, x, false))
            }
        }
        val s = b(1, 2, 3, 4)
        assertFalse(BleProto.shouldConnect(s, s, false))           // our own advert
        assertFalse(BleProto.shouldConnect(s, b(0, 0), true))      // not a mesh-msg advert
        // A stealth phone is invisible, so it must be the one that connects.
        assertTrue(BleProto.shouldConnect(b(0, 0, 0, 1), b(0xff, 0xff, 0xff, 0xff), true))
    }

    @Test fun splitKeepsBytesAndFitsMtu() {
        val chunk = ByteArray(BleProto.CORE_CHUNK) { it.toByte() }
        assertEquals(1, BleProto.split(chunk, BleProto.WANTED_MTU).size)
        for (mtu in listOf(23, 24, 64, 100, 183, 185, 517)) {
            val parts = BleProto.split(chunk, mtu)
            assertTrue(parts.all { it.isNotEmpty() && it.size <= mtu - 3 })
            assertArrayEquals(chunk, parts.reduce { a, c -> a + c })
        }
        assertEquals(20, BleProto.payload(5))    // never below the BLE minimum
        assertEquals(512, BleProto.payload(9999)) // never above the attribute limit
    }

    /** The advert must fit in 31 bytes: flags 3 + 128-bit UUID 18 + manufacturer data 4+session. */
    @Test fun advertFits() {
        assertTrue(3 + 18 + 4 + BleProto.SESSION_BYTES <= 31)
    }
}
