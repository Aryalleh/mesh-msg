package org.meshmsg.app

import java.nio.ByteBuffer
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import org.meshmsg.app.data.CallVideo
import org.meshmsg.app.data.H264

class CallVideoTest {
    private fun b(vararg v: Int) = ByteArray(v.size) { v[it].toByte() }

    @Test
    fun splitsAnnexB() {
        // SPS (4-byte start), PPS (3-byte start), IDR slice.
        val au = b(0, 0, 0, 1, 0x67, 1, 2, 0, 0, 1, 0x68, 3, 0, 0, 0, 1, 0x65, 9, 9, 9)
        val n = H264.nals(au)
        assertEquals(3, n.size)
        assertEquals(listOf(7, 8, 5), n.map { H264.type(it) })
        assertArrayEquals(b(0x67, 1, 2), n[0])
        assertArrayEquals(b(0x68, 3), n[1])
        assertArrayEquals(b(0x65, 9, 9, 9), n[2])
        assertEquals(0, H264.nals(b(1, 2, 3)).size)
    }

    @Test
    fun copiesPlanesAcrossLayouts() {
        // 4×2 interleaved chroma (pixel stride 2, row stride 10) into planar (stride 1, row 4).
        val w = 4
        val h = 2
        val src = ByteBuffer.allocate(20)
        for (y in 0 until h) for (x in 0 until w) src.put(y * 10 + x * 2, (y * w + x + 1).toByte())
        val dst = ByteBuffer.allocate(8)
        CallVideo.copyPlane(src, 10, 2, dst, 4, 1, w, h)
        assertArrayEquals(b(1, 2, 3, 4, 5, 6, 7, 8), dst.array())
        // Row padding on both sides, pixel stride 1.
        val s2 = ByteBuffer.wrap(b(1, 2, 3, 4, 0, 0, 5, 6, 7, 8, 0, 0))
        val d2 = ByteBuffer.allocate(16)
        CallVideo.copyPlane(s2, 6, 1, d2, 8, 1, w, h)
        assertArrayEquals(b(1, 2, 3, 4, 0, 0, 0, 0, 5, 6, 7, 8, 0, 0, 0, 0), d2.array())
    }
}
