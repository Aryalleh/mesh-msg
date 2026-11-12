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
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Modifier
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
import org.meshmsg.app.ui.ChatsScreen
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

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        handle(intent)
        setContent {
            MeshMsgTheme {
                val onboarded by vm.onboarded.collectAsStateWithLifecycle()
                if (!onboarded) Onboarding() else Main()
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
    }

    private fun handle(intent: Intent?) {
        intent ?: return
        if (intent.action == Intent.ACTION_VIEW && intent.data?.scheme in setOf("meshmsg", "meshgroup")) pendingLink.value = intent.dataString
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

    @Composable
    private fun Main() {
        val nav = rememberNavController()
        val entry by nav.currentBackStackEntryAsState()
        val route = entry?.destination?.route
        val snackbar = remember { SnackbarHostState() }
        val sendFailed = stringResource(R.string.send_failed, "%s")
        LaunchedEffect(Unit) { vm.errors.collect { snackbar.showSnackbar(sendFailed.format(it)) } }

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
                    ChatsScreen(chats, status.anyPathUp, onOpen = { nav.navigate(Routes.chat(it.contact.hash)) }, onAdd = { nav.navigate(Routes.ADD) })
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
                    SettingsScreen(state, vm)
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
                            onBack = { nav.popBackStack() },
                            onRename = { vm.rename(hash, it) },
                            onRemove = { vm.remove(hash); nav.popBackStack() },
                            onInvite = {
                                startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, contact.uri), null))
                            },
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
                    AddContactScreen(
                        myUri = myUri,
                        initialLink = remember { pendingLink.value.orEmpty().also { pendingLink.value = null } },
                        onBack = { nav.popBackStack() },
                        onCopy = {
                            clipboard.setPrimaryClip(ClipData.newPlainText("mesh-msg", myUri))
                            // Android 13+ shows its own confirmation.
                            if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) scope.launch { snackbar.showSnackbar(copied) }
                        },
                        onShare = {
                            startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, myUri), null))
                        },
                        onPaste = { clipboard.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(context)?.toString().orEmpty().trim() },
                        onAdd = { uri, name ->
                            if (uri.trim().startsWith("meshgroup:")) {
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
                        onCreateGroup = { name -> openGroup(runBlocking { vm.createGroup(name) }) },
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
        val TOP = setOf(CHATS, NETWORK, SETTINGS)
        fun chat(hash: String) = "chat/$hash"
    }

    companion object {
        const val EXTRA_CHAT = "chat"
    }
}
