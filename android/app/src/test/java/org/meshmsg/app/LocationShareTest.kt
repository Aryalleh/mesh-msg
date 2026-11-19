package org.meshmsg.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.meshmsg.app.data.LocationShare

class LocationShareTest {
    @Test
    fun locationMessageRoundTrip() {
        val text = LocationShare.text(32.6160567, 44.0248793, 12, lost = true)
        assertTrue(text.contains("https://nshn.ir/?lat=32.616057&lng=44.024879"))
        assertTrue(text.contains("https://balad.ir/location?latitude=32.616057&longitude=44.024879&zoom=17"))
        assertTrue(text.contains("https://www.google.com/maps/search/?api=1&query=32.616057,44.024879"))
        val l = LocationShare.parse(text)
        assertNotNull(l)
        assertEquals(32.616057, l!!.lat, 1e-9)
        assertEquals(44.024879, l.lon, 1e-9)
        assertEquals(12, l.accuracy)
        assertEquals(LocationShare.TITLE_LOST, l.title)
        // Southern and western coordinates, no accuracy.
        val s = LocationShare.parse(LocationShare.text(-33.8688, -151.2093, 0, lost = false))
        assertEquals(-151.2093, s!!.lon, 1e-9)
        assertEquals(0, s.accuracy)
    }

    @Test
    fun ordinaryTextIsNotALocation() {
        assertNull(LocationShare.parse("hello"))
        assertNull(LocationShare.parse("📍 somewhere nice"))
        assertNull(LocationShare.parse("📍 x\ngeo:91.0,10.0"))
        assertNull(LocationShare.parse(LocationShare.lostWithoutFix()))
    }
}
