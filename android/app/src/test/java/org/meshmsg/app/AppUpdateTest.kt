package org.meshmsg.app

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.meshmsg.app.data.AppUpdate
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

/** Parsing of Core.checkUpdate's JSON (update.Status in core/update). */
@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class AppUpdateTest {
    private val status = """{"current":"0.1.1-beta1","channel":"beta","checked":"2026-11-13T10:00:00Z","source":"http://87.106.5.192",
      "latest":{"version":"0.1.1-beta2","channel":"beta","date":"2026-11-14T10:00:00Z","min_supported":"0.1.1-beta1",
        "notes":{"en":"Fixes.","fa":"اصلاحات."},"page":"https://github.com/Aryalleh/mesh-msg/releases/tag/v0.1.1-beta2",
        "files":[{"name":"meshmsg-0.1.1-beta2-arm64-v8a.apk","url":"https://x/a.apk","sha256":"${"ab".repeat(32)}","size":123},
                 {"name":"meshmsg-0.1.1-beta2-universal.apk","url":"https://x/u.apk","sha256":"${"cd".repeat(32)}","size":456}]},
      "available":true,"required":false}"""

    @Test fun parsesStatus() {
        val u = AppUpdate.parse(status)!!
        assertEquals("0.1.1-beta2", u.version)
        assertEquals("0.1.1-beta1", u.current)
        assertTrue(u.available)
        assertFalse(u.required)
        assertEquals("اصلاحات.", u.notesFor("fa"))
        assertEquals("Fixes.", u.notesFor("de"))
        assertEquals("https://x/a.apk", u.apkFor(listOf("arm64-v8a", "armeabi-v7a"))!!.url)
        assertEquals(123L, u.apkFor(listOf("arm64-v8a"))!!.size)
        assertEquals("https://x/u.apk", u.apkFor(listOf("x86_64"))!!.url)
    }

    @Test fun noRelease() {
        assertNull(AppUpdate.parse("""{"current":"0.1.1-beta1","available":false,"required":false,"error":"offline"}"""))
        assertNull(AppUpdate.parse("not json"))
    }
}
