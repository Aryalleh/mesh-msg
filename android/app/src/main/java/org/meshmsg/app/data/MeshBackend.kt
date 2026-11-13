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
    /**
     * Sends the file at path (a contact or private group). important: also through relays.
     * Returns a note when it went as a normal file instead (allowance used up), else "".
     */
    fun sendMedia(hash: String, path: String, mime: String, name: String, caption: String, important: Boolean): Result<String> =
        Result.failure(UnsupportedOperationException())
    fun fetchMedia(id: String): Result<Unit> = Result.failure(UnsupportedOperationException())
    /** A downloaded file's path on this phone (decrypted into the app's storage). */
    fun mediaPath(id: String): Result<String> = Result.failure(UnsupportedOperationException())
    fun addContact(uri: String, name: String): Result<Contact>
    fun renameContact(hash: String, name: String): Result<Unit>
    fun removeContact(hash: String): Result<Unit>
    fun configureLoRa(node: String, patchJson: String): Result<Unit>

    /** Creates a private group with these contacts (hashes); each message goes to every member. */
    fun createGroup(name: String, members: List<String>): Result<Contact>
    /** Adds contacts to / removes a member from a private group we own. */
    fun addMembers(group: String, members: List<String>): Result<Contact>
    fun removeMember(group: String, member: String): Result<Contact>
    /** Joins a public chat by link (meshgroup:#tehran) or name ("global", "#tehran"). */
    fun joinGroup(link: String): Result<Contact>
    fun leaveGroup(hash: String): Result<Unit>

    /** Profile pictures on this phone (hash → file); "me" is this account. */
    val avatars: StateFlow<Map<String, AvatarFile>> get() = NoAvatars
    /** Sets (bytes: a JPEG) or removes (null) this account's picture. */
    fun setAvatar(jpeg: ByteArray?): Result<Unit> = Result.failure(UnsupportedOperationException())
    /** Asks a contact for their picture over a direct link (never relays). */
    fun requestAvatar(hash: String) {}

    /** The user's chat folders. */
    val folders: StateFlow<List<ChatFolder>> get() = NoFolders
    fun setFolders(folders: List<ChatFolder>): Result<Unit> = Result.failure(UnsupportedOperationException())

    /** The call in progress (or just ended), if any. */
    val call: StateFlow<CallInfo?> get() = NoCall
    /** Calls a contact; video: start with the camera on (a video call). */
    fun startCall(hash: String, video: Boolean = false): Result<Unit> = Result.failure(UnsupportedOperationException())
    fun answerCall(id: String): Result<Unit> = Result.failure(UnsupportedOperationException())
    fun endCall(id: String): Result<Unit> = Result.failure(UnsupportedOperationException())
    fun muteCall(id: String, muted: Boolean): Result<Unit> = Result.failure(UnsupportedOperationException())
}

private val NoCall: StateFlow<CallInfo?> = kotlinx.coroutines.flow.MutableStateFlow(null)
private val NoAvatars: StateFlow<Map<String, AvatarFile>> = kotlinx.coroutines.flow.MutableStateFlow(emptyMap())
private val NoFolders: StateFlow<List<ChatFolder>> = kotlinx.coroutines.flow.MutableStateFlow(emptyList())
