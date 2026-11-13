package org.meshmsg.app

import android.Manifest
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.os.Build
import android.os.Bundle
import android.provider.Settings as SystemSettings
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.lifecycle.lifecycleScope
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.animation.EnterTransition
import androidx.compose.animation.ExitTransition
import androidx.compose.animation.core.tween
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInHorizontally
import androidx.compose.animation.slideOutHorizontally
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.LayoutDirection
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.NavHostController
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import kotlinx.coroutines.launch
import kotlinx.coroutines.runBlocking
import org.meshmsg.app.ui.AddContactScreen
import org.meshmsg.app.ui.ChatScreen
import org.meshmsg.app.data.UpdateStep
import org.meshmsg.app.ui.ChatsScreen
import org.meshmsg.app.ui.UpdateBannerState
import org.meshmsg.app.ui.MeshIcons
import org.meshmsg.app.ui.MeshViewModel
import org.meshmsg.app.ui.NetworkScreen
import org.meshmsg.app.ui.OnboardingScreen
import org.meshmsg.app.ui.SettingsScreen
import org.meshmsg.app.ui.theme.MeshMsgTheme

class MainActivity : ComponentActivity() {
    private val vm: MeshViewModel by viewModels()

    /** A meshmsg: link or a chat to open, from the launching intent. */
    private val pendingLink = mutableStateOf<String?>(null)
    private val pendingChat = mutableStateOf<String?>(null)
    private var callAfterPermission: Pair<String, Boolean>? = null
    private val askCall = registerForActivityResult(androidx.activity.result.contract.ActivityResultContracts.RequestMultiplePermissions()) { r ->
        callAfterPermission?.let { (hash, video) -> if (hasMic()) vm.startCall(hash, video && hasCamera()) }
        callAfterPermission = null
    }

    /** Starts a call (video: camera on), asking for the microphone (and camera) first if needed. */
    private fun callMic(hash: String, video: Boolean = false) {
        val need = listOfNotNull(
            Manifest.permission.RECORD_AUDIO.takeIf { !hasMic() },
            Manifest.permission.CAMERA.takeIf { video && !hasCamera() },
        )
        if (need.isEmpty()) vm.startCall(hash, video) else { callAfterPermission = hash to video; askCall.launch(need.toTypedArray()) }
    }

    private fun hasCamera() = androidx.core.content.ContextCompat.checkSelfPermission(this, Manifest.permission.CAMERA) ==
        android.content.pm.PackageManager.PERMISSION_GRANTED

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handle(intent)
        setContent {
            MeshMsgTheme {
                val onboarded by vm.onboarded.collectAsStateWithLifecycle()
                val avatars by vm.avatars.collectAsStateWithLifecycle()
                androidx.compose.runtime.CompositionLocalProvider(org.meshmsg.app.data.LocalAvatars provides avatars) {
                    if (!onboarded) Onboarding() else androidx.compose.foundation.layout.Box {
                        Main()
                        Call()
                    }
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handle(intent)
    }

    override fun onResume() {
        super.onResume()
        vm.refreshSettings()
        val app = application as MeshApp
        app.updates.checkIfStale({ app.backend.value?.core }, lifecycleScope)
    }

    private fun handle(intent: Intent?) {
        intent ?: return
        val data = intent.data
        if (intent.action == Intent.ACTION_VIEW && data != null) {
            val web = data.scheme in setOf("https", "http") && data.host == org.meshmsg.app.data.ContactLink.HOST
            if (data.scheme in setOf("meshmsg", "meshgroup") || web) pendingLink.value = org.meshmsg.app.data.ContactLink.toUri(intent.dataString.orEmpty())
        }
        intent.getStringExtra(EXTRA_CHAT)?.let { pendingChat.value = it }
    }

    @Composable
    private fun Onboarding() {
        val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
            // Whatever the answer, the app works; the Network screen offers the permission again.
            vm.finishOnboarding()
            vm.onBluetoothPermission()
        }
        OnboardingScreen(onStart = { ask.launch(startPermissions()) }, onRestore = vm::restoreIdentity)
    }

    private fun startPermissions(): Array<String> =
        MeshApp.BLE_PERMISSIONS + if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) arrayOf(Manifest.permission.POST_NOTIFICATIONS) else emptyArray()

    /** The call screen over everything, while there is a call. */
    @Composable
    private fun Call() {
        val call by vm.call.collectAsStateWithLifecycle()
        val chats by vm.chats.collectAsStateWithLifecycle()
        val st = call?.state
        LaunchedEffect(st) { showOverLock(st == "ringing" || st == "active") }
        val c = call ?: return
        val answerPerms = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
            if (hasMic()) vm.answerCall(c.id) else vm.endCall(c.id)
        }
        // Video: the camera runs only while this screen is in front.
        val backend by vm.backend.collectAsStateWithLifecycle()
        val core = backend?.core
        val video = remember(c.id, c.video && c.state == "active", core) {
            if (c.video && c.state == "active" && core != null) org.meshmsg.app.data.CallVideo(applicationContext, core, c.id) else null
        }
        DisposableEffect(video) { onDispose { video?.release() } }
        val owner = androidx.lifecycle.compose.LocalLifecycleOwner.current
        val cameraDenied = stringResource(R.string.call_camera_denied)
        val askCamera = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
            if (ok) video?.setCamera(true, owner) else vm.notice(cameraDenied)
        }
        // A video call starts with the camera on.
        LaunchedEffect(video) { if (video != null && c.videoCall && hasCamera()) video.setCamera(true, owner) }
        org.meshmsg.app.ui.CallScreen(
            c, chats.firstOrNull { it.contact.hash == c.peer }?.contact,
            org.meshmsg.app.ui.CallActions(
                answer = {
                    val need = listOfNotNull(
                        Manifest.permission.RECORD_AUDIO.takeIf { !hasMic() },
                        Manifest.permission.CAMERA.takeIf { c.videoCall && !hasCamera() },
                    )
                    if (need.isEmpty()) vm.answerCall(c.id) else answerPerms.launch(need.toTypedArray())
                },
                end = { vm.endCall(c.id) },
                mute = { vm.muteCall(c.id, it) },
                speaker = { vm.speaker(it) },
                camera = { on ->
                    when {
                        !on -> video?.setCamera(false, owner)
                        hasCamera() -> video?.setCamera(true, owner)
                        else -> askCamera.launch(Manifest.permission.CAMERA)
                    }
                },
                switchCamera = { video?.switchCamera() },
            ),
            video = video,
        )
        androidx.activity.compose.BackHandler(enabled = !c.ended) {} // the call stays until it ends
    }

    private fun hasMic() = androidx.core.content.ContextCompat.checkSelfPermission(this, Manifest.permission.RECORD_AUDIO) ==
        android.content.pm.PackageManager.PERMISSION_GRANTED

    /** Rings and calls show over the lock screen and turn it on. */
    private fun showOverLock(on: Boolean) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O_MR1) {
            setShowWhenLocked(on)
            setTurnScreenOn(on)
        }
    }

    @Composable
    private fun Main() {
        val nav = rememberNavController()
        val entry by nav.currentBackStackEntryAsState()
        val route = entry?.destination?.route
        val snackbar = remember { SnackbarHostState() }
        val sendFailed = stringResource(R.string.send_failed, "%s")
        LaunchedEffect(Unit) { vm.errors.collect { snackbar.showSnackbar(sendFailed.format(it)) } }
        LaunchedEffect(Unit) { vm.notices.collect { snackbar.showSnackbar(it) } }

        val link by pendingLink
        val chat by pendingChat
        LaunchedEffect(link) { if (link != null) nav.navigate(Routes.ADD) { launchSingleTop = true } }
        LaunchedEffect(chat) {
            chat?.let {
                pendingChat.value = null
                nav.navigate(Routes.chat(it)) { launchSingleTop = true }
            }
        }

        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            bottomBar = { if (route in Routes.TOP) BottomBar(nav, route) },
        ) { pad ->
            // The inner screens draw their own bars; only the bottom bar inset applies here.
            val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
            val dir = if (rtl) -1 else 1
            val animate = animationsEnabled()
            NavHost(
                navController = nav,
                startDestination = Routes.CHATS,
                modifier = Modifier.padding(bottom = pad.calculateBottomPadding()),
                enterTransition = { if (animate) fadeIn(tween(200)) + slideInHorizontally(tween(200)) { dir * it / 12 } else EnterTransition.None },
                exitTransition = { if (animate) fadeOut(tween(140)) else ExitTransition.None },
                popEnterTransition = { if (animate) fadeIn(tween(200)) else EnterTransition.None },
                popExitTransition = { if (animate) fadeOut(tween(140)) + slideOutHorizontally(tween(140)) { dir * it / 12 } else ExitTransition.None },
            ) {
                composable(Routes.CHATS) {
                    val chats by vm.chats.collectAsStateWithLifecycle()
                    val status by vm.status.collectAsStateWithLifecycle()
                    val updates = (application as MeshApp).updates
                    val upd by updates.update.collectAsStateWithLifecycle()
                    val step by updates.step.collectAsStateWithLifecycle()
                    val later by updates.later.collectAsStateWithLifecycle()
                    val lang = LocalConfiguration.current.locales[0].language
                    val banner = upd?.takeIf { it.available && (it.required || later != it.version) }?.let { u ->
                        UpdateBannerState(
                            version = u.version, current = u.current, notes = u.notesFor(lang), required = u.required,
                            downloading = step is UpdateStep.Downloading, error = (step as? UpdateStep.Failed)?.message,
                            onUpdate = updates::install, onLater = { updates.later(u.version) },
                        )
                    }
                    val folders by vm.folders.collectAsStateWithLifecycle()
                    ChatsScreen(
                        chats, status.anyPathUp, onOpen = { nav.navigate(Routes.chat(it.contact.hash)) }, onAdd = { nav.navigate(Routes.ADD) },
                        update = banner, folders = folders, onSaveFolders = vm::setFolders,
                    )
                }
                composable(Routes.NETWORK) {
                    val status by vm.status.collectAsStateWithLifecycle()
                    val allowed by vm.bluetoothAllowed.collectAsStateWithLifecycle()
                    val scope = rememberCoroutineScope()
                    val noNode = stringResource(R.string.usb_no_device)
                    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { vm.onBluetoothPermission() }
                    NetworkScreen(
                        status, allowed,
                        onAllowBluetooth = {
                            if (allowed) startActivity(Intent(SystemSettings.ACTION_BLUETOOTH_SETTINGS)) else ask.launch(MeshApp.BLE_PERMISSIONS)
                        },
                        onConnectUsb = { if (!vm.connectUsb()) scope.launch { snackbar.showSnackbar(noNode) } },
                    )
                }
                composable(Routes.SETTINGS) {
                    val state by vm.settingsState.collectAsStateWithLifecycle()
                    LaunchedEffect(Unit) { vm.refreshSettings() }
                    SettingsScreen(state, object : org.meshmsg.app.ui.SettingsActions by vm {
                        override fun openStorage() { nav.navigate(Routes.STORAGE) }
                    })
                }
                composable(Routes.STORAGE) {
                    val chats by vm.chats.collectAsStateWithLifecycle()
                    var files by remember { mutableStateOf<List<org.meshmsg.app.data.StoredFile>?>(null) }
                    val scope = rememberCoroutineScope()
                    val freed = LocalContext.current
                    LaunchedEffect(Unit) { files = vm.storedFiles() }
                    org.meshmsg.app.ui.StorageScreen(
                        files, chats.map { it.contact },
                        onDelete = { ids ->
                            scope.launch {
                                val b = vm.deleteFiles(ids)
                                files = vm.storedFiles()
                                if (b != null) snackbar.showSnackbar(freed.getString(R.string.storage_freed, org.meshmsg.app.ui.humanSize(b)))
                            }
                        },
                        onBack = { nav.popBackStack() },
                    )
                }
                composable(Routes.CHAT) { e ->
                    val hash = e.arguments?.getString("hash").orEmpty()
                    val chats by vm.chats.collectAsStateWithLifecycle()
                    val messages by vm.messages.collectAsStateWithLifecycle()
                    val status by vm.status.collectAsStateWithLifecycle()
                    DisposableEffect(hash) {
                        vm.openChat(hash)
                        onDispose { vm.closeChat() }
                    }
                    val contact = chats.firstOrNull { it.contact.hash == hash }?.contact
                    if (contact != null) {
                        ChatScreen(
                            contact, messages, status.anyPathUp, vm.limits,
                            onSend = { vm.send(hash, it) },
                            onCall = if (contact.isGroup) null else ({ callMic(hash) }),
                            onVideoCall = if (contact.isGroup) null else ({ callMic(hash, video = true) }),
                            media = remember(hash) {
                                org.meshmsg.app.ui.MediaActions(
                                    pick = { uri, caption, important -> vm.sendUri(hash, uri, caption, important) },
                                    sendFile = { f, mime, name, caption, important -> vm.sendFile(hash, f, mime, name, caption, important) },
                                    fetch = { vm.fetchMedia(it) },
                                    path = { vm.mediaPath(it) },
                                    error = { vm.error(it) },
                                )
                            },
                            onBack = { nav.popBackStack() },
                            onRename = { vm.rename(hash, it) },
                            onRemove = { vm.remove(hash); nav.popBackStack() },
                            onInvite = {
                                startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, org.meshmsg.app.data.ContactLink.toWeb(contact.uri)), null))
                            },
                            contacts = chats.map { it.contact }.filter { !it.isGroup },
                            onAddMembers = { vm.addMembers(hash, it) },
                            onRemoveMember = { vm.removeMember(hash, it) },
                            onLeave = { vm.leaveGroup(hash); nav.popBackStack() },
                        )
                    }
                }
                composable(Routes.ADD) {
                    val backend by vm.backend.collectAsStateWithLifecycle()
                    val context = LocalContext.current
                    val clipboard = context.getSystemService(ClipboardManager::class.java)
                    val scope = rememberCoroutineScope()
                    val copied = stringResource(R.string.link_copied)
                    val myUri = backend?.myUri.orEmpty()
                    // Creating or joining a group is a local SQLite insert: milliseconds.
                    val openGroup = { hash: String? ->
                        if (hash != null) nav.navigate(Routes.chat(hash)) { popUpTo(Routes.CHATS) }
                    }
                    val chats by vm.chats.collectAsStateWithLifecycle()
                    AddContactScreen(
                        myUri = myUri,
                        initialLink = remember { pendingLink.value.orEmpty().also { pendingLink.value = null } },
                        onBack = { nav.popBackStack() },
                        onCopy = {
                            clipboard.setPrimaryClip(ClipData.newPlainText("mesh-msg", org.meshmsg.app.data.ContactLink.toWeb(myUri)))
                            // Android 13+ shows its own confirmation.
                            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) scope.launch { snackbar.showSnackbar(copied) }
                        },
                        onShare = {
                            startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, org.meshmsg.app.data.ContactLink.toWeb(myUri)), null))
                        },
                        onPaste = { clipboard.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(context)?.toString().orEmpty().trim() },
                        onAdd = { uri, name ->
                            if (uri.trim().startsWith("meshgroup:#")) {
                                val hash = runBlocking { vm.joinGroup(uri) }
                                openGroup(hash)
                                if (hash == null) org.meshmsg.app.ui.LinkError.Invalid else null
                            } else {
                                // A local SQLite insert: milliseconds.
                                val error = runBlocking { vm.addContact(uri, name) }
                                if (error == null) nav.popBackStack()
                                error
                            }
                        },
                        regions = vm.regions,
                        contacts = chats.map { it.contact }.filter { !it.isGroup },
                        onCreateGroup = { name, members -> openGroup(runBlocking { vm.createGroup(name, members) }) },
                        onJoinGroup = { link -> openGroup(runBlocking { vm.joinGroup(link) }) },
                    )
                }
            }
        }
    }

    @Composable
    private fun animationsEnabled(): Boolean {
        val resolver = LocalContext.current.contentResolver
        return remember { SystemSettings.Global.getFloat(resolver, SystemSettings.Global.ANIMATOR_DURATION_SCALE, 1f) != 0f }
    }

    @Composable
    private fun BottomBar(nav: NavHostController, route: String?) {
        NavigationBar {
            val items = listOf(
                Triple(Routes.CHATS, MeshIcons.Chat, R.string.nav_chats),
                Triple(Routes.NETWORK, MeshIcons.Hub, R.string.nav_network),
                Triple(Routes.SETTINGS, Icons.Filled.Settings, R.string.nav_settings),
            )
            items.forEach { (r, icon, label) ->
                NavigationBarItem(
                    selected = route == r,
                    onClick = {
                        nav.navigate(r) {
                            popUpTo(nav.graph.findStartDestination().id) { saveState = true }
                            launchSingleTop = true
                            restoreState = true
                        }
                    },
                    icon = { Icon(icon, contentDescription = null) },
                    label = { Text(stringResource(label)) },
                )
            }
        }
    }

    private object Routes {
        const val CHATS = "chats"
        const val NETWORK = "network"
        const val SETTINGS = "settings"
        const val CHAT = "chat/{hash}"
        const val ADD = "add"
        const val STORAGE = "storage"
        val TOP = setOf(CHATS, NETWORK, SETTINGS)
        fun chat(hash: String) = "chat/$hash"
    }

    companion object {
        const val EXTRA_CHAT = "chat"
    }
}
