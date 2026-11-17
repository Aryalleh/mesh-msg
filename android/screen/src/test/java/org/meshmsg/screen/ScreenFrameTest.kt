package org.meshmsg.screen

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ScreenFrameTest {
    private fun hex(s: String) = ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }

    @Test
    fun rebuildsTilesLikeTheFirmwareSendsThem() {
        val f = ScreenFrame()
        assertFalse(f.apply(hex("0100100008100801803412"))) // a tile before the info
        // Same bytes as firmware/test/mirror_test.cpp wire_format.
        assertTrue(f.apply(hex("0000f001401008")))
        assertEquals(240, f.width)
        assertEquals(320, f.height)
        assertTrue(f.apply(hex("0100100008100801803412")))
        val c = ScreenFrame.argb(0x1234)
        for (y in 8 until 16) for (x in 16 until 32) assertEquals(c, f.pixels[y * 240 + x])
        assertEquals(0xFF000000.toInt(), f.pixels[0])

        // A raw 2×1 tile: white, then pure red.
        assertTrue(f.apply(hex("01" + "0000" + "0000" + "02" + "01" + "00" + "ffff" + "00f8")))
        assertEquals(0xFFFFFFFF.toInt(), f.pixels[0])
        assertEquals(0xFFFF0000.toInt(), f.pixels[1])

        // Off screen, short runs, wrong sizes: refused, nothing changes.
        val v = f.version
        assertFalse(f.apply(hex("0100f00008100801803412")))
        assertFalse(f.apply(hex("010000000810080140" + "3412")))
        assertFalse(f.apply(hex("0100000000020100ffff")))
        assertEquals(v, f.version)
    }

    @Test
    fun rgb565ToArgb() {
        assertEquals(0xFF000000.toInt(), ScreenFrame.argb(0))
        assertEquals(0xFFFFFFFF.toInt(), ScreenFrame.argb(0xFFFF))
        assertEquals(0xFF00FF00.toInt(), ScreenFrame.argb(0x07E0))
        assertEquals(0xFF0000FF.toInt(), ScreenFrame.argb(0x001F))
    }
}
