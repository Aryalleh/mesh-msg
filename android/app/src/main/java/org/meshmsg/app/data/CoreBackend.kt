package org.meshmsg.app.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.launch
import org.json.JSONObject
import org.meshmsg.meshcore.Core
import org.meshmsg.meshcore.Listener
import org.meshmsg.meshcore.NetworkBinder
import org.meshmsg.meshcore.Meshcore

/**
 * The Go core behind [MeshBackend]. Core calls hit SQLite, so they run on an
 * IO thread; the flows are refreshed when the core reports an event, and the
 * status also every few seconds (link details change without events).
 */
class CoreBackend(dataDir: String, configJson: String, binder: NetworkBinder? = null) : MeshBackend, Listener {
    val core: Core = Meshcore.open(dataDir, configJson)

    override val myUri: String = core.uri()
    override val myHash: String = core.hash()

    private val _chats = MutableStateFlow<List<Chat>>(emptyList())
    private val _status = MutableStateFlow(NetStatus())
    private val _messages = MutableStateFlow<List<Message>>(emptyList())
    override val chats: StateFlow<List<Chat>> = _chats
    override val status: StateFlow<NetStatus> = _status
    override val messages: StateFlow<List<Message>> = _messages

    private val _call = MutableStateFlow<CallInfo?>(null)
    override val call: StateFlow<CallInfo?> = _call

    /** Incoming messages, for notifications: (contact hash, text). */
    val incoming = Channel<Message>(Channel.BUFFERED)

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val refresh = Channel<Unit>(Channel.CONFLATED)
    @Volatile private var open: String? = null

    init {
        core.setListener(this)
        if (binder != null) core.setNetworkBinder(binder)
        core.start()
        scope.launch {
            for (u in refresh) reload()
        }
        scope.launch {
            while (true) {
                refresh.trySend(Unit)
                delay(5_000)
            }
        }
    }

    private val _avatars = MutableStateFlow<Map<String, AvatarFile>>(emptyMap())
    override val avatars: StateFlow<Map<String, AvatarFile>> = _avatars
    private val _folders = MutableStateFlow<List<ChatFolder>>(emptyList())
    override val folders: StateFlow<List<ChatFolder>> = _folders

    private fun reload() {
        val st = runCatching { Json.status(core.status()) }.getOrDefault(_status.value)
        _status.value = st
        runCatching { _chats.value = Json.chats(core.chats(), st.anyPathUp) }
        runCatching {
            val m = HashMap<String, AvatarFile>()
            AvatarImage.file(core.avatarPath("me"))?.let { m["me"] = it }
            for (c in _chats.value) if (!c.contact.isGroup) AvatarImage.file(core.avatarPath(c.contact.hash))?.let { m[c.contact.hash] = it }
            if (m != _avatars.value) _avatars.value = m
        }
        runCatching { Folders.parse(core.folders()).let { if (it != _folders.value) _folders.value = it } }
        open?.let { h -> runCatching { _messages.value = Json.messages(core.messages(h, 500), st.anyPathUp) } }
    }

    // Called on a Go thread.
    override fun onEvent(json: String) {
        runCatching {
            val o = JSONObject(json)
            if (o.optString("type") == "call") {
                o.optJSONObject("call")?.let { onCall(Json.call(it)) }
                return
            }
            if (o.optString("type") == "message") {
                o.optJSONObject("message")?.let { m ->
                    val msg = Json.message(m, true)
                    if (msg.peer == open) runCatching { core.markRead(msg.peer) } else incoming.trySend(msg)
                }
            }
        }
        refresh.trySend(Unit)
    }

    private fun onCall(c: CallInfo) {
        val cur = _call.value
        // A missed call or a busy reject of another call doesn't replace the live one.
        if (cur != null && !cur.ended && cur.id != c.id) return
        _call.value = c
        if (c.ended) {
            scope.launch {
                delay(2_500)
                if (_call.value?.id == c.id) _call.value = null
            }
        }
    }

    // Video is always offered when the phone has an H.264 codec, so the camera
    // can be turned on during a voice call too.
    override fun startCall(hash: String, video: Boolean): Result<Unit> = runCatching {
        onCall(Json.call(JSONObject(core.startVideoCall(hash, CallAudio.codecs(), CallVideo.codecs(), video))))
    }
    override fun answerCall(id: String): Result<Unit> = runCatching { core.answerCallVideo(id, CallAudio.codecs(), CallVideo.codecs()) }
    override fun endCall(id: String): Result<Unit> = runCatching { core.endCall(id) }
    override fun muteCall(id: String, muted: Boolean): Result<Unit> = runCatching { core.muteCall(id, muted) }

    override fun setAvatar(jpeg: ByteArray?): Result<Unit> = call { core.setAvatar(jpeg ?: ByteArray(0)) }
    override var readReceipts: Boolean
        get() = runCatching { core.readReceipts() }.getOrDefault(true)
        set(v) { runCatching { core.setReadReceipts(v) } }
    override fun requestAvatar(hash: String) { scope.launch { runCatching { core.requestAvatar(hash) } } }
    override fun setFolders(folders: List<ChatFolder>): Result<Unit> = call { core.setFolders(Folders.json(folders)) }

    override fun openChat(hash: String) {
        open = hash
        _messages.value = emptyList()
        scope.launch {
            runCatching { core.markRead(hash) }
            refresh.trySend(Unit)
        }
    }

    override fun closeChat() {
        open = null
        _messages.value = emptyList()
    }

    private fun <T> call(block: () -> T): Result<T> = runCatching(block).also { refresh.trySend(Unit) }

    override fun send(hash: String, text: String): Result<Unit> = call { core.send(hash, text); Unit }
    override fun sendMedia(hash: String, path: String, mime: String, name: String, caption: String, important: Boolean): Result<String> =
        call {
            val m = org.json.JSONObject(core.sendMedia(hash, path, mime, name, caption, important))
            m.optJSONObject("media")?.optString("note").orEmpty()
        }
    override fun fetchMedia(id: String): Result<Unit> = call { core.fetchMedia(id) }
    override fun mediaPath(id: String): Result<String> = call { core.mediaPath(id) }

    override fun addContact(uri: String, name: String): Result<Contact> =
        call { Json.contact(JSONObject(core.addContact(uri.trim(), name.trim()))) }

    override fun renameContact(hash: String, name: String): Result<Unit> = call { core.renameContact(hash, name.trim()) }
    override fun removeContact(hash: String): Result<Unit> = call { core.removeContact(hash) }
    override fun configureLoRa(node: String, patchJson: String): Result<Unit> = call { core.loRaConfigure(node, patchJson) }
    override fun createGroup(name: String, members: List<String>): Result<Contact> =
        call { Json.group(JSONObject(core.createGroup(name.trim(), org.json.JSONArray(members).toString()))) }
    override fun addMembers(group: String, members: List<String>): Result<Contact> =
        call { Json.group(JSONObject(core.addMembers(group, org.json.JSONArray(members).toString()))) }
    override fun removeMember(group: String, member: String): Result<Contact> = call { Json.group(JSONObject(core.removeMember(group, member))) }
    override fun joinGroup(link: String): Result<Contact> = call { Json.group(JSONObject(core.joinGroup(link.trim()))) }
    override fun leaveGroup(hash: String): Result<Unit> = call { core.leaveGroup(hash) }

    fun stop() {
        core.setListener(null)
        core.stop()
    }

    /** Panic wipe: erases identity, contacts, messages and stored packets. */
    fun wipe() {
        core.setListener(null)
        core.wipe()
    }
}
