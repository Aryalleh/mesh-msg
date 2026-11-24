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
import androidx.compose.foundation.layout.consumeWindowInsets
import androidx.compose.foundation.layout.statusBars
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Settings
import org.meshmsg.app.hamyar.card
import org.meshmsg.app.hamyar.mava
import org.meshmsg.app.hamyar.cachedMe
import org.meshmsg.app.hamyar.me
import org.meshmsg.app.hamyar.linkedMava
import org.meshmsg.app.hamyar.linkMava
import org.meshmsg.app.hamyar.webLink
import androidx.compose.material.icons.filled.AccountBox
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
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

    // ---- the Hamyar build (BuildConfig.HAMYAR): Hamyar's login, tabs and screens around Mava ----
    private val hamyar by lazy { org.meshmsg.app.hamyar.HamyarApi(applicationContext) }
    private val hamyarLoggedIn = mutableStateOf(false)

    /** The Hamyar app is Persian only, like the website. */
    override fun attachBaseContext(base: android.content.Context) {
        super.attachBaseContext(if (BuildConfig.HAMYAR) persian(base) else base)
    }

    /** Opens a page of the university's site inside the app (in-app WebView), already logged in (one-time link). */
    private fun openHamyarWeb(path: String) {
        lifecycleScope.launch {
            runCatching { hamyar.webLink(path) }
                .onSuccess { url ->
                    runCatching { org.meshmsg.app.hamyar.HamyarWebActivity.open(this@MainActivity, url) }
                        .onFailure { runCatching { startActivity(Intent(Intent.ACTION_VIEW, android.net.Uri.parse(url))) } }
                }
                .onFailure { vm.notice(if ((it as? org.meshmsg.app.hamyar.HamyarError)?.code == "unauthorized") "دوباره وارد همیار شوید." else "اینترنت در دسترس نیست.") }
        }
    }

    /** Links this phone's Mava account to the Hamyar login, signed with the account key (it never leaves the phone). */
    private suspend fun linkMava(): Result<String> = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
        runCatching {
            val b = vm.backend.value ?: error("Mava is starting")
            val js = org.json.JSONObject(b.core.signHamyar("POST", "/v1/mava/account"))
            val headers = js.keys().asSequence().associateWith { js.getString(it) }
            hamyar.linkMava(headers).also { hamyar.linkedMava = b.myHash }
        }
    }

    private val mavaHooks by lazy {
        org.meshmsg.app.hamyar.MavaHooks(
            myAccount = { vm.backend.value?.myHash },
            link = { linkMava() },
            exportHistory = { pass, f ->
                kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
                    runCatching { (vm.backend.value ?: error("Mava is starting")).core.exportHistory(pass, f.absolutePath) }
                }
            },
            restoreHistory = { f, pass ->
                kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
                    runCatching { org.meshmsg.meshcore.Meshcore.restoreHistory((application as MeshApp).meshDirPath, f.absolutePath, pass, true); Unit }
                }
            },
            cacheDir = cacheDir,
        )
    }

    private fun hamyarLogout() {
        hamyar.logout()
        hamyarLoggedIn.value = false
        org.meshmsg.app.hamyar.PresenceRadio.poke()
    }

    /** A meshmsg: link or a chat to open, from the launching intent. */
    private val pendingLink = mutableStateOf<String?>(null)
    private val pendingChat = mutableStateOf<String?>(null)
    /** A call to answer: the Answer button of the ringing notification. */
    private val pendingAnswer = mutableStateOf<String?>(null)
    /** A meshtrip: link to show, or the trips screen to open (notifications). */
    private val pendingTripLink = mutableStateOf<String?>(null)
    private val pendingTrips = mutableStateOf(false)
    private var afterLocation: ((Boolean) -> Unit)? = null
    private val askLocation = registerForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        afterLocation?.invoke(org.meshmsg.app.service.TripService.hasPermission(this))
        afterLocation = null
    }

    /** Asks for the location permission (trip sharing) if needed, then calls done. */
    private fun withLocation(done: (Boolean) -> Unit) {
        if (org.meshmsg.app.service.TripService.hasPermission(this)) done(true)
        else { afterLocation = done; askLocation.launch(org.meshmsg.app.service.TripService.PERMISSIONS) }
    }

    @android.annotation.SuppressLint("MissingPermission") // checked first
    private fun lastFix(): android.location.Location? {
        if (!org.meshmsg.app.service.TripService.hasPermission(this)) return null
        val lm = getSystemService(android.location.LocationManager::class.java) ?: return null
        return listOf(android.location.LocationManager.GPS_PROVIDER, android.location.LocationManager.NETWORK_PROVIDER)
            .mapNotNull { p -> runCatching { lm.getLastKnownLocation(p) }.getOrNull() }
            .maxByOrNull { it.elapsedRealtimeNanos }
    }

    private fun lastLocation(): Pair<Double, Double>? = lastFix()?.let { it.latitude to it.longitude }

    /** A fresh position if the phone can get one soon (else the last known one, or null). */
    @android.annotation.SuppressLint("MissingPermission") // checked first
    private fun currentFix(done: (android.location.Location?) -> Unit) {
        val last = lastFix()
        val lm = getSystemService(android.location.LocationManager::class.java)
        val fresh = last != null && android.os.SystemClock.elapsedRealtimeNanos() - last.elapsedRealtimeNanos < 120_000_000_000L
        if (fresh || lm == null || Build.VERSION.SDK_INT < Build.VERSION_CODES.R || !org.meshmsg.app.service.TripService.hasPermission(this)) {
            done(last)
            return
        }
        val provider = if (runCatching { lm.isProviderEnabled(android.location.LocationManager.GPS_PROVIDER) }.getOrDefault(false))
            android.location.LocationManager.GPS_PROVIDER else android.location.LocationManager.NETWORK_PROVIDER
        val cancel = android.os.CancellationSignal()
        var answered = false
        val answer = { l: android.location.Location? -> if (!answered) { answered = true; done(l ?: last) } }
        runCatching { lm.getCurrentLocation(provider, cancel, mainExecutor) { answer(it) } }.onFailure { answer(null) }
        window.decorView.postDelayed({ cancel.cancel(); answer(null) }, 20_000)
    }

    /** Sends this phone's location in a chat (the Location item of the attach menu). */
    private fun shareLocation(hash: String) {
        withLocation { ok ->
            if (!ok) { vm.notice(getString(R.string.trip_need_location)); return@withLocation }
            vm.notice(getString(R.string.location_finding))
            currentFix { l ->
                if (l == null) vm.notice(getString(R.string.location_none))
                else vm.send(hash, org.meshmsg.app.data.LocationShare.text(l.latitude, l.longitude, l.accuracy.toInt(), lost = false))
            }
        }
    }

    /** "I'm lost": the location goes to the trip's leaders as a message, and their chat opens. */
    private fun lostToLeaders(t: org.meshmsg.app.data.Trip) {
        val b = vm.backend.value ?: return
        withLocation { _ ->
            currentFix { l ->
                val text = if (l != null) org.meshmsg.app.data.LocationShare.text(l.latitude, l.longitude, l.accuracy.toInt(), lost = true)
                else org.meshmsg.app.data.LocationShare.lostWithoutFix()
                lifecycleScope.launch(kotlinx.coroutines.Dispatchers.IO) {
                    val r = b.trips.messageLeaders(t.hash, text)
                    kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.Main) {
                        r.onSuccess { first -> pendingChat.value = first }
                            .onFailure { vm.notice(getString(R.string.trip_lost_no_leaders, it.message ?: "")) }
                    }
                }
            }
        }
    }

    private fun tripActions() = org.meshmsg.app.ui.TripsActions(
        requestLocation = ::withLocation,
        sharingChanged = { syncTripService() },
        openMap = { lat, lon, label ->
            org.meshmsg.app.data.LocationShare.navigate(this, lat, lon, label, getString(R.string.location_navigate_with))
        },
        lost = ::lostToLeaders,
        share = { text -> startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, text), null)) },
        copy = { text -> getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("mava-trip", text)) },
        myLocation = ::lastLocation,
    )

    /** Keeps the trip location service running while a trip is shared. */
    private fun syncTripService() {
        val b = vm.backend.value ?: return
        lifecycleScope.launch(kotlinx.coroutines.Dispatchers.IO) {
            org.meshmsg.app.service.TripService.sync(this@MainActivity, b.trips.trips().any { it.sharing })
        }
    }

    /** The call screen is hidden behind the ongoing-call bar (voice calls). */
    private val callMinimized = mutableStateOf(false)
    /** Android 14+ refused full-screen incoming calls and the user hasn't been asked yet. */
    private val askFullScreen = mutableStateOf(false)
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
        hamyarLoggedIn.value = hamyar.loggedIn
        setContent {
            MeshMsgTheme {
                val onboarded by vm.onboarded.collectAsStateWithLifecycle()
                val avatars by vm.avatars.collectAsStateWithLifecycle()
                androidx.compose.runtime.CompositionLocalProvider(org.meshmsg.app.data.LocalAvatars provides avatars) {
                    val hamyarIn by hamyarLoggedIn
                    if (BuildConfig.HAMYAR && !onboarded) {
                        HamyarOnboarding()
                    } else if (BuildConfig.HAMYAR && !hamyarIn) {
                        org.meshmsg.app.hamyar.HamyarLogin(hamyar, onBack = { finish() }) { hamyarLoggedIn.value = true; org.meshmsg.app.hamyar.PresenceRadio.poke() }
                    } else if (!onboarded) Onboarding() else androidx.compose.foundation.layout.Box {
                        androidx.compose.foundation.layout.Column {
                            val bar = CallBar()
                            // The bar takes the status bar's space: the app below doesn't pad for it again.
                            val below = if (bar) Modifier.consumeWindowInsets(androidx.compose.foundation.layout.WindowInsets.statusBars) else Modifier
                            androidx.compose.foundation.layout.Box(Modifier.weight(1f).then(below)) { Main() }
                        }
                        Call()
                        FullScreenPrompt()
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
        val prefs = getSharedPreferences("ui", MODE_PRIVATE)
        askFullScreen.value = !org.meshmsg.app.service.CallService.canFullScreen(this) && !prefs.getBoolean("asked_full_screen", false)
        val app = application as MeshApp
        app.updates.checkIfStale({ app.backend.value?.core }, lifecycleScope)
        syncTripService() // e.g. after the phone restarted while sharing
    }

    private fun handle(intent: Intent?) {
        intent ?: return
        val data = intent.data
        if (intent.action == Intent.ACTION_VIEW && data != null) {
            val web = data.scheme in setOf("https", "http") && data.host == org.meshmsg.app.data.ContactLink.HOST
            if (data.scheme in setOf("meshmsg", "meshgroup") || web) pendingLink.value = org.meshmsg.app.data.ContactLink.toUri(intent.dataString.orEmpty())
            if (data.scheme == "meshtrip") pendingTripLink.value = intent.dataString
        }
        intent.getStringExtra(EXTRA_CHAT)?.let { pendingChat.value = it }
        if (intent.getBooleanExtra(EXTRA_TRIPS, false)) pendingTrips.value = true
        if (intent.action == org.meshmsg.app.service.CallService.ACTION_ANSWER) {
            pendingAnswer.value = intent.getStringExtra(org.meshmsg.app.service.CallService.EXTRA_CALL)
            callMinimized.value = false
        }
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

    /** The Hamyar build's first start: Hamyar login, the offer to restore Mava chats, then the usual permissions. */
    @Composable
    private fun HamyarOnboarding() {
        val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
            vm.onBluetoothPermission()
        }
        org.meshmsg.app.hamyar.HamyarStart(hamyar, mavaHooks, onQuit = { finish() }) {
            hamyarLoggedIn.value = true
            vm.finishOnboarding()
            ask.launch(startPermissions())
        }
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
        LaunchedEffect(c.id) { callMinimized.value = false }
        val audio by org.meshmsg.app.service.CallService.audio.collectAsStateWithLifecycle()
        val answerPerms = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
            if (hasMic()) vm.answerCall(c.id) else vm.endCall(c.id)
        }
        // Video: the camera runs only while this screen is in front.
        val backend by vm.backend.collectAsStateWithLifecycle()
        val core = backend?.core
        val video = remember(c.id, c.video && c.state == "active", c.videoCodec, core) {
            if (c.video && c.state == "active" && core != null) org.meshmsg.app.data.CallVideo(applicationContext, core, c.id, c.videoCodec) else null
        }
        DisposableEffect(video) { onDispose { video?.release() } }
        val owner = androidx.lifecycle.compose.LocalLifecycleOwner.current
        val cameraDenied = stringResource(R.string.call_camera_denied)
        val askCamera = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
            if (ok) video?.setCamera(true, owner) else vm.notice(cameraDenied)
        }
        // A video call starts with the camera on.
        LaunchedEffect(video) { if (video != null && c.videoCall && hasCamera()) video.setCamera(true, owner) }
        val answer = {
            val need = listOfNotNull(
                Manifest.permission.RECORD_AUDIO.takeIf { !hasMic() },
                Manifest.permission.CAMERA.takeIf { c.videoCall && !hasCamera() },
            )
            if (need.isEmpty()) vm.answerCall(c.id) else answerPerms.launch(need.toTypedArray())
        }
        // Answer tapped on the ringing notification.
        val toAnswer by pendingAnswer
        LaunchedEffect(toAnswer, c.id, c.state) {
            if (toAnswer == c.id && c.incoming && c.state == "ringing") answer()
            if (toAnswer != null) pendingAnswer.value = null
        }
        // Voice calls can hide behind the ongoing-call bar (video needs its screen).
        val canMinimize = !c.video && c.state == "active"
        if (callMinimized.value && canMinimize) return
        org.meshmsg.app.ui.CallScreen(
            c, chats.firstOrNull { it.contact.hash == c.peer }?.contact,
            org.meshmsg.app.ui.CallActions(
                answer = answer,
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
                route = { audio?.routes?.select(it) },
                minimize = if (canMinimize) ({ callMinimized.value = true }) else null,
            ),
            video = video,
            audio = audio?.routes,
        )
        // Back hides a voice call behind the bar; otherwise the call stays until it ends.
        androidx.activity.compose.BackHandler(enabled = !c.ended) { if (canMinimize) callMinimized.value = true }
    }

    /** The ongoing-call bar while the call screen is minimized. */
    @Composable
    private fun CallBar(): Boolean {
        val call by vm.call.collectAsStateWithLifecycle()
        val chats by vm.chats.collectAsStateWithLifecycle()
        val c = call ?: return false
        if (!callMinimized.value || c.ended || c.video || c.state != "active") return false
        val name = chats.firstOrNull { it.contact.hash == c.peer }?.contact?.displayName ?: c.peer.take(8)
        org.meshmsg.app.ui.OngoingCallBar(c, name, onOpen = { callMinimized.value = false })
        return true
    }

    /** Asks once (Android 14+) to allow incoming calls to show full screen. */
    @Composable
    private fun FullScreenPrompt() {
        if (!askFullScreen.value) return
        val done = {
            askFullScreen.value = false
            getSharedPreferences("ui", MODE_PRIVATE).edit().putBoolean("asked_full_screen", true).apply()
        }
        androidx.compose.material3.AlertDialog(
            onDismissRequest = done,
            title = { Text(stringResource(R.string.call_full_screen_title)) },
            text = { Text(stringResource(R.string.call_full_screen_body)) },
            confirmButton = {
                androidx.compose.material3.TextButton(onClick = { done(); openFullScreenSettings() }) { Text(stringResource(R.string.call_full_screen_allow)) }
            },
            dismissButton = { androidx.compose.material3.TextButton(onClick = done) { Text(stringResource(R.string.not_now)) } },
        )
    }

    /** The system page where the user allows full-screen incoming calls (Android 14+). */
    fun openFullScreenSettings() {
        if (Build.VERSION.SDK_INT >= 34) {
            runCatching {
                startActivity(Intent(SystemSettings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT, android.net.Uri.parse("package:$packageName")))
            }
        }
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
        val tripLink by pendingTripLink
        val openTrips by pendingTrips
        LaunchedEffect(tripLink, openTrips) {
            if (tripLink != null || openTrips) {
                pendingTrips.value = false
                nav.navigate(Routes.TRIPS) { launchSingleTop = true }
            }
        }
        LaunchedEffect(chat) {
            chat?.let {
                pendingChat.value = null
                nav.navigate(Routes.chat(it)) { launchSingleTop = true }
            }
        }

        if (BuildConfig.HAMYAR) HamyarSync()
        Scaffold(
            snackbarHost = { SnackbarHost(snackbar) },
            bottomBar = { if (route in Routes.TOP || (BuildConfig.HAMYAR && route in Routes.H_TOP)) BottomBar(nav, route) },
        ) { pad ->
            // The inner screens draw their own bars; only the bottom bar inset applies here.
            val rtl = LocalLayoutDirection.current == LayoutDirection.Rtl
            val dir = if (rtl) -1 else 1
            val animate = animationsEnabled()
            NavHost(
                navController = nav,
                startDestination = if (BuildConfig.HAMYAR) Routes.H_HOME else Routes.CHATS,
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
                        tripBanner = tripBanner()?.let { it to { nav.navigate(Routes.TRIPS) { launchSingleTop = true } } },
                    )
                }
                composable(Routes.NETWORK) {
                    val status by vm.status.collectAsStateWithLifecycle()
                    val allowed by vm.bluetoothAllowed.collectAsStateWithLifecycle()
                    val scope = rememberCoroutineScope()
                    val noNode = stringResource(R.string.usb_no_device)
                    val noBoard = stringResource(R.string.ble_no_board)
                    val boardFound = stringResource(R.string.ble_board_found)
                    val searching = stringResource(R.string.ble_searching)
                    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) { vm.onBluetoothPermission() }
                    NetworkScreen(
                        status, allowed,
                        lastBleDrop = vm.bleLastDrop,
                        onAllowBluetooth = {
                            if (allowed) startActivity(Intent(SystemSettings.ACTION_BLUETOOTH_SETTINGS)) else ask.launch(MeshApp.BLE_PERMISSIONS)
                        },
                        onConnectUsb = { if (!vm.connectUsb()) scope.launch { snackbar.showSnackbar(noNode) } },
                        onConnectBle = {
                            if (!allowed) {
                                ask.launch(MeshApp.BLE_PERMISSIONS)
                            } else {
                                scope.launch { snackbar.showSnackbar(searching) }
                                vm.connectBoardBle { ok -> scope.launch { snackbar.showSnackbar(if (ok) boardFound else noBoard) } }
                            }
                        },
                    )
                }
                composable(Routes.SETTINGS) {
                    val state by vm.settingsState.collectAsStateWithLifecycle()
                    LaunchedEffect(Unit) { vm.refreshSettings() }
                    SettingsScreen(state, object : org.meshmsg.app.ui.SettingsActions by vm {
                        override fun openStorage() { nav.navigate(Routes.STORAGE) }
                        override fun openTrips() { nav.navigate(Routes.TRIPS) }
                        override fun openHamyar() { nav.navigate(Routes.HAMYAR) }
                    })
                }
                composable(Routes.HAMYAR) {
                    if (BuildConfig.HAMYAR) {
                        LaunchedEffect(Unit) { nav.navigate(Routes.H_PRESENCE) { popUpTo(Routes.HAMYAR) { inclusive = true } } }
                    } else org.meshmsg.app.hamyar.HamyarScreen(onBack = { nav.popBackStack() })
                }
                if (BuildConfig.HAMYAR) hamyarRoutes(nav)
                composable(Routes.TRIPS) {
                    val backend by vm.backend.collectAsStateWithLifecycle()
                    val chats by vm.chats.collectAsStateWithLifecycle()
                    val b = backend
                    org.meshmsg.app.ui.TripsScreen(
                        api = b?.trips,
                        changes = b?.tripChanges ?: kotlinx.coroutines.flow.emptyFlow(),
                        contacts = chats.map { it.contact },
                        actions = remember { tripActions() },
                        onBack = { nav.popBackStack() },
                        initialLink = remember { pendingTripLink.value.orEmpty().also { pendingTripLink.value = null } },
                    )
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
                                    shareLocation = { shareLocation(hash) },
                                )
                            },
                            onBack = { nav.popBackStack() },
                            onRename = { vm.rename(hash, it) },
                            onRemove = { vm.remove(hash); nav.popBackStack() },
                            onMerge = { into -> vm.merge(hash, into) },
                            onMerged = { into -> nav.navigate(Routes.chat(into)) { popUpTo(Routes.CHATS) } },
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

    /** The chats screen's trip line: a lost member, an offer to decide, or sharing in progress. */
    @Composable
    private fun tripBanner(): String? {
        val backend by vm.backend.collectAsStateWithLifecycle()
        val b = backend ?: return null
        var state by remember(b) { mutableStateOf<Triple<String?, Boolean, String?>>(Triple(null, false, null)) }
        LaunchedEffect(b) {
            suspend fun load() {
                state = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) {
                    val ts = b.trips.trips()
                    val lost = ts.firstOrNull { t -> t.leader && b.trips.members(t.hash).any { it.sos } }?.name
                    Triple(lost, ts.any { it.pending }, ts.filter { it.sharing }.joinToString(", ") { it.name }.ifEmpty { null })
                }
            }
            load()
            b.tripChanges.collect { load() }
        }
        val (lost, pending, sharing) = state
        return when {
            lost != null -> stringResource(R.string.trip_banner_sos, lost)
            pending -> stringResource(R.string.trip_banner_pending)
            sharing != null -> stringResource(R.string.trip_banner_sharing, sharing)
            else -> null
        }
    }

    @Composable
    private fun animationsEnabled(): Boolean {
        val resolver = LocalContext.current.contentResolver
        return remember { SystemSettings.Global.getFloat(resolver, SystemSettings.Global.ANIMATOR_DURATION_SCALE, 1f) != 0f }
    }

    @Composable
    private fun BottomBar(nav: NavHostController, route: String?) {
        NavigationBar(
            containerColor = if (BuildConfig.HAMYAR) androidx.compose.ui.graphics.Color.White else androidx.compose.material3.NavigationBarDefaults.containerColor,
        ) {
            val items = if (BuildConfig.HAMYAR) hamyarTabs() else listOf(
                Triple(Routes.CHATS, MeshIcons.Chat, stringResource(R.string.nav_chats)),
                Triple(Routes.NETWORK, MeshIcons.Hub, stringResource(R.string.nav_network)),
                Triple(Routes.SETTINGS, Icons.Filled.Settings, stringResource(R.string.nav_settings)),
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
                    label = { Text(label) },
                    colors = if (BuildConfig.HAMYAR) androidx.compose.material3.NavigationBarItemDefaults.colors(
                        selectedIconColor = org.meshmsg.app.ui.theme.HamyarColors.brand700,
                        selectedTextColor = org.meshmsg.app.ui.theme.HamyarColors.brand700,
                        indicatorColor = org.meshmsg.app.ui.theme.HamyarColors.brand50,
                        unselectedIconColor = org.meshmsg.app.ui.theme.HamyarColors.ink400,
                        unselectedTextColor = org.meshmsg.app.ui.theme.HamyarColors.ink400,
                    ) else androidx.compose.material3.NavigationBarItemDefaults.colors(),
                )
            }
        }
    }

    // ------------------------------------------------------------------ the Hamyar build

    private fun persian(base: android.content.Context): android.content.Context {
        val c = android.content.res.Configuration(base.resources.configuration)
        c.setLocale(java.util.Locale.forLanguageTag("fa"))
        return base.createConfigurationContext(c)
    }

    @Composable
    private fun hamyarTabs(): List<Triple<String, androidx.compose.ui.graphics.vector.ImageVector, String>> {
        val me = remember { hamyar.cachedMe }
        return listOfNotNull(
            Triple(Routes.H_HOME, Icons.Filled.Home, "خانه"),
            Triple(Routes.H_TRIPS, Icons.Filled.DateRange, "سفرها"),
            Triple(Routes.CHATS, MeshIcons.Chat, "ماوا"),
            Triple(Routes.H_PRESENCE, Icons.Filled.LocationOn, "حضور").takeIf { me?.has("presence") != false },
            Triple(Routes.H_CARD, Icons.Filled.AccountBox, "کارت").takeIf { me?.has("card") != false },
        )
    }

    private fun hamyarNav(nav: NavHostController) = org.meshmsg.app.hamyar.HamyarNav(
        web = ::openHamyarWeb,
        openProfile = { nav.navigate(Routes.H_PROFILE) { launchSingleTop = true } },
        openGroup = { nav.navigate(Routes.group(it)) },
        openClasses = { nav.navigate(Routes.H_CLASSES) { launchSingleTop = true } },
        openCheck = { nav.navigate(Routes.H_CHECK) { launchSingleTop = true } },
        openTrips = { nav.navigate(Routes.H_HOME) { popUpTo(Routes.H_HOME); launchSingleTop = true } },
        openMavaTrip = { link -> pendingTripLink.value = link; pendingTrips.value = true },
        openMavaSettings = { nav.navigate(Routes.SETTINGS) { launchSingleTop = true } },
        openNetwork = { nav.navigate(Routes.NETWORK) { launchSingleTop = true } },
        openMavaAccount = { nav.navigate(Routes.H_MAVA) { launchSingleTop = true } },
        logout = ::hamyarLogout,
    )

    private fun androidx.navigation.NavGraphBuilder.hamyarRoutes(nav: NavHostController) {
        val hn = hamyarNav(nav)
        val back: () -> Unit = { nav.popBackStack() }
        composable(Routes.H_HOME) { org.meshmsg.app.hamyar.HamyarHome(hamyar, hn) }
        composable(Routes.H_TRIPS) { org.meshmsg.app.hamyar.HamyarTrips(hamyar, hn) }
        composable(Routes.H_PRESENCE) { org.meshmsg.app.hamyar.PresenceTab(hamyar, hn.openGroup, hn.openClasses, hn.logout) }
        composable(Routes.H_CARD) { org.meshmsg.app.hamyar.StudentCardScreen(hamyar, hn) }
        composable(Routes.H_PROFILE) { org.meshmsg.app.hamyar.HamyarProfile(hamyar, hn, back) }
        composable(Routes.H_CLASSES) { org.meshmsg.app.hamyar.ClassesScreen(hamyar, back) }
        composable(Routes.H_CHECK) { org.meshmsg.app.hamyar.CardCheckScreen(hamyar, back) }
        composable(Routes.H_MAVA) { org.meshmsg.app.hamyar.MavaAccountScreen(hamyar, mavaHooks, back) }
        composable(Routes.H_GROUP) { e ->
            org.meshmsg.app.hamyar.TripGroupScreen(hamyar, android.net.Uri.decode(e.arguments?.getString("trip").orEmpty()), back)
        }
    }

    /**
     * Once Mava runs: adds the university's relay, names the Mava account after the student, and links it to the
     * Hamyar login (Hamyar users get a Mava account automatically; the key is made and kept on this phone).
     */
    @Composable
    private fun HamyarSync() {
        val backend by vm.backend.collectAsStateWithLifecycle()
        val loggedIn by hamyarLoggedIn
        LaunchedEffect(backend, loggedIn) {
            val b = backend ?: return@LaunchedEffect
            if (!loggedIn) return@LaunchedEffect
            val me = runCatching { hamyar.me() }.getOrNull() ?: hamyar.cachedMe
            val settings = (application as MeshApp).settings
            if (me != null && settings.displayName.isBlank() && me.name.isNotBlank()) vm.setName(me.name)
            if (me != null && !me.has("mava")) return@LaunchedEffect
            val info = runCatching { hamyar.mava() }.getOrNull() ?: return@LaunchedEffect
            val relay = info.relay
            if (relay != null && settings.relays.none { it.url.trimEnd('/') == relay.trimEnd('/') }) {
                vm.addRelay(org.meshmsg.app.data.Relay((me?.slug ?: "uni") + "-mava", relay)) // restarts the core; this runs again
                return@LaunchedEffect
            }
            if (info.account != b.myHash) linkMava()
        }
        // the card's key is kept fresh for NFC (it answers from the cached card)
        LaunchedEffect(loggedIn) { if (loggedIn && hamyar.cachedMe?.has("card") != false) runCatching { hamyar.card() } }
    }

    private object Routes {
        const val CHATS = "chats"
        const val NETWORK = "network"
        const val SETTINGS = "settings"
        const val CHAT = "chat/{hash}"
        const val ADD = "add"
        const val STORAGE = "storage"
        const val TRIPS = "trips"
        const val HAMYAR = "hamyar"
        val TOP = setOf(CHATS, NETWORK, SETTINGS)
        const val H_HOME = "h_home"
        const val H_TRIPS = "h_trips"
        const val H_PRESENCE = "h_presence"
        const val H_CARD = "h_card"
        const val H_PROFILE = "h_profile"
        const val H_GROUP = "h_group/{trip}"
        const val H_CLASSES = "h_classes"
        const val H_CHECK = "h_check"
        const val H_MAVA = "h_mava"
        /** The Hamyar build's tabs: home, trips, Mava (the chats), presence, card. */
        val H_TOP = setOf(H_HOME, H_TRIPS, CHATS, H_PRESENCE, H_CARD)
        fun group(trip: String) = "h_group/" + android.net.Uri.encode(trip)
        fun chat(hash: String) = "chat/$hash"
    }

    companion object {
        const val EXTRA_CHAT = "chat"
        /** Opens the trips screen (trip notifications). */
        const val EXTRA_TRIPS = "trips"
    }
}
