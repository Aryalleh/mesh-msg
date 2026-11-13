package org.meshmsg.app

import org.junit.Assert.assertEquals
import org.junit.Test
import org.meshmsg.app.data.ContactLink

class ContactLinkTest {
    private val uri = "meshmsg:" + "A".repeat(86) + "?n=Sara+Ahmadi"

    @Test
    fun roundTrip() {
        val web = ContactLink.toWeb(uri)
        assertEquals("https://eu-1.mfadem.ir/add#" + "A".repeat(86) + "?n=Sara+Ahmadi", web)
        assertEquals(uri, ContactLink.toUri(web))
        assertEquals(uri, ContactLink.toUri("  $web\n"))
        assertEquals(uri, ContactLink.toUri(web.replace("/add#", "/add/#")))
    }

    @Test
    fun othersUnchanged() {
        assertEquals("meshgroup:#tehran", ContactLink.toWeb("meshgroup:#tehran"))
        assertEquals(uri, ContactLink.toUri(uri))
        assertEquals("https://eu-1.mfadem.ir/other#x", ContactLink.toUri("https://eu-1.mfadem.ir/other#x"))
        assertEquals("https://eu-1.mfadem.ir/add", ContactLink.toUri("https://eu-1.mfadem.ir/add"))
    }
}
