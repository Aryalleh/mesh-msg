package org.meshmsg.flasher.update

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.Base64

class RemoteFirmwareTest {
    // The real release.json of v0.1.1-beta22, signed by the CI release job.
    @Test
    fun realReleaseManifestVerifies() {
        val text = javaClass.getResource("/release-beta22.json")!!.readText()
        fun field(name: String) = Regex("\"$name\"\\s*:\\s*\"([^\"]+)\"").find(text)!!.groupValues[1]
        val body = Base64.getDecoder().decode(field("manifest"))
        val sig = Base64.getDecoder().decode(field("sig"))
        assertTrue(RemoteFirmware.verifyBytes(body, sig))
        assertTrue(String(body).contains("\"version\":\"0.1.1-beta22\""))
        // One changed byte in the manifest: refused.
        val forged = body.copyOf().also { it[10] = (it[10].toInt() xor 1).toByte() }
        assertFalse(RemoteFirmware.verifyBytes(forged, sig))
        // Signed with another key: refused.
        assertFalse(RemoteFirmware.verifyBytes(body, sig, "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c"))
    }

    @Test
    fun versionOrder() {
        assertTrue(Versions.compare("v0.1.1-beta22", "v0.1.1-beta9") > 0)
        assertTrue(Versions.compare("v0.1.1", "v0.1.1-beta22") > 0)
        assertTrue(Versions.compare("v0.1.2-alpha1", "v0.1.1") > 0)
        assertTrue(Versions.compare("0.1.1-beta1", "0.1.1-alpha9") > 0)
        assertTrue(Versions.compare("v0.1.1-beta22", "0.1.1-beta22") == 0)
    }
}
