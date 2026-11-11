package org.meshmsg.app.data

import kotlinx.coroutines.flow.StateFlow

/**
 * What the UI needs from the mesh. The real one wraps the Go core
 * ([CoreBackend]); tests and previews use a fake, so every screen renders
 * without a phone.
 */
interface MeshBackend {
    val myUri: String
    val myHash: String
    val chats: StateFlow<List<Chat>>
    val status: StateFlow<NetStatus>

    /** Messages of the chat opened with [openChat], oldest first. */
    val messages: StateFlow<List<Message>>

    fun openChat(hash: String)
    fun closeChat()
    fun send(hash: String, text: String): Result<Unit>
    fun addContact(uri: String, name: String): Result<Contact>
    fun renameContact(hash: String, name: String): Result<Unit>
    fun removeContact(hash: String): Result<Unit>
    fun configureLoRa(node: String, patchJson: String): Result<Unit>
}
