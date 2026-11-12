package org.meshmsg.app.ui

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.ExperimentalCoroutinesApi
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.flatMapLatest
import kotlinx.coroutines.flow.flowOf
import kotlinx.coroutines.flow.receiveAsFlow
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.meshmsg.app.MeshApp
import org.meshmsg.app.data.BatteryMode
import org.meshmsg.app.data.Chat
import org.meshmsg.app.data.CoreBackend
import org.meshmsg.app.data.Message
import org.meshmsg.app.data.NetStatus
import org.meshmsg.app.data.Relay
import org.meshmsg.meshcore.Meshcore

@OptIn(ExperimentalCoroutinesApi::class)
class MeshViewModel(application: Application) : AndroidViewModel(application), SettingsActions {
    private val app = application as MeshApp
    private val settings = app.settings
    val backend: StateFlow<CoreBackend?> = app.backend

    private fun <T> from(initial: T, pick: (CoreBackend) -> StateFlow<T>): StateFlow<T> =
        backend.flatMapLatest { it?.let(pick) ?: flowOf(initial) }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5_000), initial)

    val chats: StateFlow<List<Chat>> = from(emptyList()) { it.chats }
    val status: StateFlow<NetStatus> = from(NetStatus()) { it.status }
    val messages: StateFlow<List<Message>> = from(emptyList()) { it.messages }

    private val _settingsState = MutableStateFlow(readSettings())
    val settingsState: StateFlow<SettingsState> = _settingsState

    val onboarded = MutableStateFlow(settings.onboarded)
    val bluetoothAllowed = MutableStateFlow(app.bluetoothAllowed())

    /** One-off errors for a snackbar (the core's own words, e.g. "text too long"). */
    private val _errors = Channel<String>(Channel.BUFFERED)
    val errors = _errors.receiveAsFlow()

    val limits = TextLimits(
        onePacket = Meshcore.onePacketBytes().toInt(),
        max = Meshcore.maxTextBytes().toInt(),
        bytes = { Meshcore.textBytes(it).toInt() },
    )

    private fun readSettings() = SettingsState(
        displayName = settings.displayName, myHash = app.backend.value?.myHash.orEmpty(),
        gateway = settings.gateway, discovery = settings.discovery, battery = settings.batteryMode, relays = settings.relays,
    )

    private fun changed() {
        _settingsState.value = readSettings()
        app.restartSoon()
    }

    fun refreshSettings() {
        _settingsState.value = readSettings()
        bluetoothAllowed.value = app.bluetoothAllowed()
    }

    fun finishOnboarding() {
        settings.onboarded = true
        onboarded.value = true
        app.start()
    }

    fun onBluetoothPermission() {
        bluetoothAllowed.value = app.bluetoothAllowed()
        if (bluetoothAllowed.value) app.onBluetoothPermission()
    }

    fun connectUsb(): Boolean = app.connectUsb()

    private fun io(block: suspend (CoreBackend) -> Result<*>) {
        val b = backend.value ?: return
        viewModelScope.launch(Dispatchers.IO) {
            block(b).exceptionOrNull()?.let { _errors.trySend(it.message ?: it.toString()) }
        }
    }

    fun openChat(hash: String) = backend.value?.openChat(hash)
    fun closeChat() = backend.value?.closeChat()
    fun send(hash: String, text: String) = io { it.send(hash, text) }

    /** Sends what a picker returned (photos are shrunk first). */
    fun sendUri(hash: String, uri: android.net.Uri, caption: String) = io { b ->
        runCatching { org.meshmsg.app.data.MediaFiles.copyIn(app, uri) }.mapCatching { p ->
            try { b.sendMedia(hash, p.file.path, p.mime, p.name, caption).getOrThrow() } finally { p.file.delete() }
        }
    }

    /** Sends a recorded voice message or video, then deletes the copy (the core keeps its own). */
    fun sendFile(hash: String, file: java.io.File, mime: String, name: String, caption: String) = io { b ->
        if (file.length() > org.meshmsg.app.data.MediaFiles.MAX_BYTES) {
            file.delete()
            Result.failure<Unit>(IllegalArgumentException("File larger than 64 MB"))
        } else {
            b.sendMedia(hash, file.path, mime, name, caption).also { file.delete() }
        }
    }

    fun fetchMedia(id: String) = io { it.fetchMedia(id) }

    /** A downloaded file's path, or null while it isn't on the phone. */
    suspend fun mediaPath(id: String): String? = withContext(Dispatchers.IO) { backend.value?.mediaPath(id)?.getOrNull() }

    fun error(msg: String) { _errors.trySend(msg) }
    fun rename(hash: String, name: String) = io { it.renameContact(hash, name) }
    fun remove(hash: String) = io { it.removeContact(hash) }
    fun leaveGroup(hash: String) = io { it.leaveGroup(hash) }
    fun addMembers(group: String, members: List<String>) = io { it.addMembers(group, members) }
    fun removeMember(group: String, member: String) = io { it.removeMember(group, member) }

    /** Suggested regional chats (from the core). */
    val regions: List<String> by lazy {
        runCatching { org.json.JSONArray(Meshcore.regions()).let { a -> List(a.length()) { a.getString(it) } } }.getOrDefault(emptyList())
    }

    /** Creates or joins a group; returns its hash, or null (the error goes to the snackbar). */
    suspend fun createGroup(name: String, members: List<String>): String? = groupCall { it.createGroup(name, members) }
    suspend fun joinGroup(link: String): String? = groupCall { it.joinGroup(link) }

    private suspend fun groupCall(f: (CoreBackend) -> Result<org.meshmsg.app.data.Contact>): String? {
        val b = backend.value ?: return null
        return withContext(Dispatchers.IO) {
            f(b).onFailure { _errors.trySend(it.message ?: it.toString()) }.getOrNull()?.hash
        }
    }

    /** Returns null on success, or why the link was refused. */
    suspend fun addContact(uri: String, name: String): LinkError? {
        val b = backend.value ?: return LinkError.Invalid
        val link = uri.trim()
        if (link == b.myUri || link.substringBefore('?') == b.myUri.substringBefore('?')) return LinkError.Own
        return withContext(Dispatchers.IO) { if (b.addContact(link, name).isSuccess) null else LinkError.Invalid }
    }

    // ---- SettingsActions ----
    override fun setName(name: String) { settings.displayName = name; changed() }
    override fun setGateway(on: Boolean) { settings.gateway = on; changed() }
    override fun setDiscovery(on: Boolean) { settings.discovery = on; changed() }
    override fun setBattery(mode: BatteryMode) { settings.batteryMode = mode; changed() }
    override fun addRelay(relay: Relay) { settings.relays = settings.relays.filter { it.name != relay.name } + relay; changed() }
    override fun removeRelay(relay: Relay) { settings.relays = settings.relays - relay; changed() }
    override suspend fun exportIdentity(passphrase: String): Result<String> {
        val b = backend.value ?: return Result.failure(IllegalStateException("core not running"))
        return withContext(Dispatchers.IO) { runCatching { b.core.exportIdentity(passphrase) } }
    }

    /** Installs a backed-up identity before the core first opens (onboarding). */
    suspend fun restoreIdentity(backup: String, passphrase: String): RestoreError? = withContext(Dispatchers.IO) {
        try {
            Meshcore.restoreIdentity(app.meshDirPath, backup, passphrase, true)
            null
        } catch (e: Exception) {
            val m = e.message.orEmpty()
            when {
                m.startsWith("passphrase:") -> RestoreError.Passphrase
                m.startsWith("format:") -> RestoreError.Format
                else -> RestoreError.Other
            }
        }
    }

    override fun wipe() {
        app.wipe {
            onboarded.value = false
            _settingsState.value = SettingsState()
        }
    }
}
