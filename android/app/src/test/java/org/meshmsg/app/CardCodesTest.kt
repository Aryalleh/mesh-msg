package org.meshmsg.app

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import org.meshmsg.app.hamyar.CardCodes
import org.meshmsg.app.hamyar.PresenceCodec
import org.meshmsg.app.hamyar.StudentCard

/** The card codes must match the Hamyar server's (worker src/lib/student-card.ts, vector made there). */
class CardCodesTest {
    private val key = PresenceCodec.unhex("2b6d828d3a39d918aebf582ed13ef8a15d5f13ab010674dda4cbc1c141d992d9")!!
    private val card = StudentCard(
        name = "S", studentCode = "4001", field = null, degree = null, dormitory = null, university = "U", slug = "iribu",
        status = "active", photoUrl = null, key = key, epoch = 1, uses = emptyList(), isChecker = false, userId = 42,
    )

    @Test fun serverVector() {
        assertEquals("HYC1.iribu.42.60000000.bb470962b343f40bc150", CardCodes.code(key, "iribu", 42, 60_000_000))
        assertEquals("HYC1.iribu.42.60000000.bb470962b343f40bc150", CardCodes.current(card, 60_000_000L * 30 + 29))
        assertEquals(1L, CardCodes.secondsLeft(60_000_000L * 30 + 29))
    }

    @Test fun apdu() {
        val r = CardCodes.answer(CardCodes.selectApdu(), card, 60_000_000L * 30)
        assertEquals("HYC1.iribu.42.60000000.bb470962b343f40bc150", CardCodes.parseAnswer(r))
        assertArrayEquals(CardCodes.NOT_FOUND, CardCodes.answer(byteArrayOf(0, 0xA4.toByte(), 4, 0, 2, 1, 2), card))
        assertArrayEquals(CardCodes.NOT_FOUND, CardCodes.answer(CardCodes.selectApdu(), null))
        assertArrayEquals(CardCodes.NOT_FOUND, CardCodes.answer(CardCodes.selectApdu(), card.copy(status = "off")))
        assertNull(CardCodes.parseAnswer(byteArrayOf(0x6A, 0x82.toByte())))
        assertNull(CardCodes.parseAnswer("hello".toByteArray() + CardCodes.OK))
    }
}
