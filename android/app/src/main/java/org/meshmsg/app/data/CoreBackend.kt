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
import org.meshmsg.meshcore.Meshcore

/**
 * The Go core behind [MeshBackend]. Core calls hit SQLite, so they run on an
 * IO thread; the flows are refreshed when the core reports an event, and the
 * status also every few seconds (link details change without events).
 */
class CoreBackend(dataDir: String, configJson: String) : MeshBackend, Listener {
    val core: Core = Meshcore.open(dataDir, configJson)

    override val myUri: String = core.uri()
    override val myHash: String = core.hash()

    private val _chats = MutableStateFlow<List<Chat>>(emptyList())
    private val _status = MutableStateFlow(NetStatus())
    private val _messages = MutableStateFlow<List<Message>>(emptyList())
    override val chats: StateFlow<List<Chat>> = _chats
    override val status: StateFlow<NetStatus> = _status
    override val messages: StateFlow<List<Message>> = _messages

    /** Incoming messages, for notifications: (contact hash, text). */
    val incoming = Channel<Message>(Channel.BUFFERED)

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val refresh = Channel<Unit>(Channel.CONFLATED)
    @Volatile private var open: String? = null

    init {
        core.setListener(this)
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

    private fun reload() {
        val st = runCatching { Json.status(core.status()) }.getOrDefault(_status.value)
        _status.value = st
        runCatching { _chats.value = Json.chats(core.chats(), st.anyPathUp) }
        open?.let { h -> runCatching { _messages.value = Json.messages(core.messages(h, 500), st.anyPathUp) } }
    }

    // Called on a Go thread.
    override fun onEvent(json: String) {
        runCatching {
            val o = JSONObject(json)
            if (o.optString("type") == "message") {
                o.optJSONObject("message")?.let { m ->
                    val msg = Json.message(m, true)
                    if (msg.peer == open) runCatching { core.markRead(msg.peer) } else incoming.trySend(msg)
                }
            }
        }
        refresh.trySend(Unit)
    }

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

    override fun addContact(uri: String, name: String): Result<Contact> =
        call { Json.contact(JSONObject(core.addContact(uri.trim(), name.trim()))) }

    override fun renameContact(hash: String, name: String): Result<Unit> = call { core.renameContact(hash, name.trim()) }
    override fun removeContact(hash: String): Result<Unit> = call { core.removeContact(hash) }
    override fun configureLoRa(node: String, patchJson: String): Result<Unit> = call { core.loRaConfigure(node, patchJson) }
    override fun createGroup(name: String): Result<Contact> = call { Json.group(JSONObject(core.createGroup(name.trim()))) }
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
