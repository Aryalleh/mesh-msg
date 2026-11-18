package org.meshmsg.flasher.screen

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AudioLinkTest {
    private fun hex(s: String) = ByteArray(s.length / 2) { s.substring(2 * it, 2 * it + 2).toInt(16).toByte() }

    @Test
    fun matchesTheFirmware() {
        // Same bytes as firmware/test/audio_test.cpp link(): 16000 Hz, 160 samples per frame (10 ms).
        assertEquals(16000 to 160, AudioLink.format(hex("003e8000a0")))
        assertEquals(AudioLink.RATE to AudioLink.FRAME, AudioLink.format(hex("003e8000a0")))
        assertNull(AudioLink.format(hex("013e8000a0")))
        assertArrayEquals(hex("0103"), AudioLink.start(speaker = true, mic = true))
        assertArrayEquals(hex("0101"), AudioLink.start(speaker = true, mic = false))
        assertArrayEquals(hex("0102"), AudioLink.start(speaker = false, mic = true))
        assertArrayEquals(hex("00"), AudioLink.start(speaker = false, mic = false))
    }

    @Test
    fun framesBothWays() {
        val pcm = hex("40a2" + "ff7f") // -24000, 32767 little-endian
        val mic = AudioLink.mic(0x1234, pcm)
        assertArrayEquals(hex("021234" + "40a2ff7f"), mic)
        assertArrayEquals(pcm, AudioLink.speaker(hex("011234" + "40a2ff7f")))
        assertNull(AudioLink.speaker(hex("021234" + "40a2ff7f"))) // a mic frame is not for the speaker
        assertNull(AudioLink.speaker(hex("01123440"))) // half a sample
    }
}
