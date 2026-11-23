package org.meshmsg.app

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.meshmsg.app.hamyar.PresenceCodec

/** Same values as spec/test-vectors/presence.json (made by the Hamyar server) and the firmware's presence_test.cpp. */
class PresenceCodecTest {
    private val key = PresenceCodec.unhex("0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")!!

    @Test fun tokensMatchTheServer() {
        assertEquals("1d50077d00f91f27", PresenceCodec.hex(PresenceCodec.beaconToken(key, 15000000)))
        assertEquals("604335470604075b", PresenceCodec.hex(PresenceCodec.phoneToken(key, 15000000)))
        assertEquals("b67fc2856512297e", PresenceCodec.hex(PresenceCodec.tripToken(key, 15000000)))
        assertEquals(15000000L, PresenceCodec.window(1_800_000_000L))
    }

    /** trip_server in the vectors: the member key as the Hamyar API hands it out, and its token. */
    @Test fun tripTokenMatchesTheServer() {
        val k = PresenceCodec.unhex("8188d5c1ece945a9dc567ade7b79fef6d2c6fe1c4732ba03c9b9612464cf4689")!!
        assertEquals("6cf5de1f2bcd5089", PresenceCodec.hex(PresenceCodec.tripToken(k, 15000000)))
        val a = PresenceCodec.parse(PresenceCodec.adv('T', 7, PresenceCodec.tripToken(k, 15000000)))!!
        assertEquals('T', a.kind)
        assertEquals(7L, a.id)
    }

    @Test fun advertRoundTrip() {
        val t = PresenceCodec.phoneToken(key, 7)
        val a = PresenceCodec.adv('P', 0x01020304L, t)
        assertEquals(16, a.size)
        val p = PresenceCodec.parse(a)!!
        assertEquals('P', p.kind)
        assertEquals(0x01020304L, p.id)
        assertArrayEquals(t, p.token)
        assertNull(PresenceCodec.parse(a.copyOf(15)))
        assertNull(PresenceCodec.parse(a.copyOf().also { it[1] = 'X'.code.toByte() }))
        assertEquals(0xFFFFFFFFL, PresenceCodec.parse(PresenceCodec.adv('V', 0xFFFFFFFFL, t))!!.id)
    }

    @Test fun hex() {
        assertNull(PresenceCodec.unhex("abc"))
        assertNull(PresenceCodec.unhex("zz"))
        assertEquals("00ff", PresenceCodec.hex(PresenceCodec.unhex("00FF")!!))
    }
}
