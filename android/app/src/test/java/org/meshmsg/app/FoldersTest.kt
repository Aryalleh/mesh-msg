package org.meshmsg.app

import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith
import org.meshmsg.app.data.ChatFolder
import org.meshmsg.app.data.Folders
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(sdk = [34])
class FoldersTest {
    @Test
    fun roundTrip() {
        val fs = listOf(ChatFolder("Work", listOf("ab".repeat(16))), ChatFolder("خانواده", emptyList()))
        assertEquals(fs, Folders.parse(Folders.json(fs)))
    }

    @Test
    fun coreFormat() {
        // As meshd/the Go core writes it; a missing chats list is empty.
        assertEquals(listOf(ChatFolder("A", listOf("x")), ChatFolder("B", emptyList())), Folders.parse("""[{"name":"A","chats":["x"]},{"name":"B"}]"""))
        assertEquals(emptyList<ChatFolder>(), Folders.parse("null"))
        assertEquals(emptyList<ChatFolder>(), Folders.parse(""))
    }
}
