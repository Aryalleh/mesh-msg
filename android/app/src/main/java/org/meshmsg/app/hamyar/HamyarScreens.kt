package org.meshmsg.app.hamyar

import android.Manifest
import android.os.Build
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Person
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.foundation.selection.toggleable
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.meshmsg.app.R

/** Hamyar's palette (the UX Pilot designs: brand green, sand, ink). */
private object HC {
    val brand900 = Color(0xFF0A2E28)
    val brand700 = Color(0xFF0B5A4D)
    val brand600 = Color(0xFF0E6B5C)
    val brand50 = Color(0xFFE8F3F0)
    val sand100 = Color(0xFFF5F2EC)
    val sand200 = Color(0xFFEAE4D8)
    val ink900 = Color(0xFF14211F)
    val ink600 = Color(0xFF4B5A57)
    val violet50 = Color(0xFFF5F3FF)
    val violet600 = Color(0xFF7C3AED)
    val violet800 = Color(0xFF5B21B6)
    val sky50 = Color(0xFFF0F9FF)
    val sky900 = Color(0xFF0C4A6E)
    val amber = Color(0xFFFBBF24)
    val rose = Color(0xFFE11D48)
}

private val Card = RoundedCornerShape(24.dp)

/**
 * The Hamyar entry: login when needed, then the presence setup screen once, then "my presence".
 * [onBack] leaves Hamyar (back to Mava's settings).
 */
@Composable
fun HamyarScreen(onBack: () -> Unit) {
    val ctx = LocalContext.current
    val api = remember { HamyarApi(ctx) }
    var loggedIn by remember { mutableStateOf(api.loggedIn) }
    var setupDone by remember { mutableStateOf(api.presenceSetupDone) }
    var trip by rememberSaveable { mutableStateOf<String?>(null) }
    BackHandler(enabled = trip != null) { trip = null }
    Box(Modifier.fillMaxSize().background(HC.sand100)) {
        when {
            !loggedIn -> HamyarLogin(api, onBack) { loggedIn = true; PresenceRadio.poke() }
            !setupDone -> PresenceSetup(api, onLater = onBack) { api.presenceSetupDone = true; setupDone = true; PresenceRadio.poke() }
            trip != null -> TripGroupScreen(api, trip!!) { trip = null }
            else -> MyPresence(api, onBack, openTrip = { trip = it }) { api.logout(); loggedIn = false; setupDone = false; PresenceRadio.poke() }
        }
    }
}

/** The Hamyar app's presence tab: the setup screen once, then "my presence" (no back button: it is a tab). */
@Composable
fun PresenceTab(api: HamyarApi, openTrip: (String) -> Unit, openClasses: () -> Unit, logout: () -> Unit) {
    var setupDone by remember { mutableStateOf(api.presenceSetupDone) }
    Box(Modifier.fillMaxSize().background(HC.sand100)) {
        if (!setupDone) {
            PresenceSetup(api, onLater = { api.presenceSetupDone = true; setupDone = true }) {
                api.presenceSetupDone = true; setupDone = true; PresenceRadio.poke()
            }
        } else MyPresence(api, onBack = null, openTrip = openTrip, openClasses = openClasses, logout = logout)
    }
}

// ------------------------------------------------------------------ login

@Composable
internal fun HamyarLogin(api: HamyarApi, onBack: () -> Unit, done: () -> Unit) {
    val scope = rememberCoroutineScope()
    var phone by rememberSaveable { mutableStateOf("") }
    var code by rememberSaveable { mutableStateOf("") }
    var sent by rememberSaveable { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    var choices by remember { mutableStateOf<Pair<List<HamyarAccount>, String>?>(null) }
    val errText = error?.let { errorText(it) }

    fun run(block: suspend () -> Unit) {
        busy = true; error = null
        scope.launch {
            runCatching { block() }.onFailure { error = (it as? HamyarError)?.code ?: "network" }
            busy = false
        }
    }

    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Header(stringResource(R.string.hamyar_login_title), stringResource(R.string.hamyar_login_sub), onBack)
        Column(Modifier.padding(20.dp).offset(y = (-24).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            WhiteCard {
                val list = choices
                if (list != null) {
                    Text(stringResource(R.string.hamyar_pick_account), fontWeight = FontWeight.Black, color = HC.ink900)
                    list.first.forEach { a ->
                        Row(
                            Modifier.fillMaxWidth().heightIn(min = 56.dp).background(HC.sand100, RoundedCornerShape(16.dp))
                                .toggleable(value = false, role = Role.Button, onValueChange = { run { api.select(list.second, a.slug); done() } })
                                .padding(horizontal = 16.dp, vertical = 8.dp),
                            verticalAlignment = Alignment.CenterVertically,
                        ) {
                            Column(Modifier.weight(1f)) {
                                Text(a.title, fontWeight = FontWeight.Bold, color = HC.ink900)
                                if (a.name.isNotEmpty()) Text(a.name, fontSize = 12.sp, color = HC.ink600)
                            }
                        }
                    }
                } else {
                    OutlinedTextField(
                        value = phone, onValueChange = { phone = it.filter(Char::isDigit).take(11); sent = false },
                        label = { Text(stringResource(R.string.hamyar_phone)) }, singleLine = true,
                        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Phone), modifier = Modifier.fillMaxWidth(),
                    )
                    if (sent) {
                        OutlinedTextField(
                            value = code, onValueChange = { code = it.filter(Char::isDigit).take(6) },
                            label = { Text(stringResource(R.string.hamyar_code)) }, singleLine = true,
                            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.NumberPassword), modifier = Modifier.fillMaxWidth(),
                        )
                    }
                    if (errText != null) Text(errText, color = HC.rose, fontSize = 13.sp)
                    BrandButton(
                        text = stringResource(if (sent) R.string.hamyar_enter else R.string.hamyar_send_code),
                        enabled = !busy && phone.length >= 10 && (!sent || code.length >= 4), busy = busy,
                    ) {
                        if (!sent) run { api.sendCode(phone); sent = true }
                        else run {
                            val (accounts, proof) = api.verify(phone, code)
                            when {
                                api.loggedIn -> done()
                                accounts.isEmpty() -> error = "no_account"
                                proof != null -> choices = accounts to proof
                            }
                        }
                    }
                }
            }
            Text(stringResource(R.string.hamyar_login_note), fontSize = 12.sp, color = HC.ink600, lineHeight = 20.sp, modifier = Modifier.padding(horizontal = 8.dp))
        }
    }
}

@Composable
private fun errorText(code: String): String = stringResource(
    when (code) {
        "bad_phone" -> R.string.hamyar_err_phone
        "wrong_code" -> R.string.hamyar_err_code
        "code_expired" -> R.string.hamyar_err_expired
        "too_many_requests" -> R.string.hamyar_err_limit
        "no_account" -> R.string.hamyar_err_no_account
        "network" -> R.string.hamyar_err_network
        else -> R.string.hamyar_err_other
    },
)

// ------------------------------------------------------------------ setup (shown once)

@Composable
private fun PresenceSetup(api: HamyarApi, onLater: () -> Unit, done: () -> Unit) {
    val scope = rememberCoroutineScope()
    var info by remember { mutableStateOf(api.cachedPresence) }
    var campus by rememberSaveable { mutableStateOf(false) }
    LaunchedEffect(Unit) { runCatching { api.presence() }.onSuccess { info = it; campus = it.optIn } }
    val ask = rememberLauncherForActivityResult(ActivityResultContracts.RequestMultiplePermissions()) {
        scope.launch {
            runCatching { if (campus != (info?.optIn ?: false)) api.setOptIn(campus) }
            done()
        }
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Column(
            Modifier.fillMaxWidth().background(HC.brand900, RoundedCornerShape(bottomStart = 32.dp, bottomEnd = 32.dp))
                .statusBarsPadding().padding(start = 24.dp, end = 24.dp, top = 32.dp, bottom = 44.dp),
            horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            Box(Modifier.size(56.dp).background(HC.amber, RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.LocationOn, null, tint = HC.ink900)
            }
            Text(stringResource(R.string.presence_setup_title), color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.Black)
            Text(stringResource(R.string.presence_setup_body), color = Color.White.copy(alpha = .75f), fontSize = 14.sp, lineHeight = 24.sp)
        }
        Column(Modifier.padding(horizontal = 20.dp).offset(y = (-20).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            WhiteCard {
                Text(stringResource(R.string.presence_setup_why), fontWeight = FontWeight.Black, color = HC.ink900)
                Bullet(Icons.Filled.Home, HC.violet600, HC.violet50, R.string.presence_why_dorm, R.string.presence_why_dorm_body)
                Bullet(Icons.Filled.Person, HC.brand700, HC.brand50, R.string.presence_why_class, R.string.presence_why_class_body)
                Bullet(Icons.Filled.LocationOn, Color(0xFFD97706), Color(0xFFFFFBEB), R.string.presence_why_trip, R.string.presence_why_trip_body)
            }
            WhiteCard {
                val dorm = info?.requiredDorm
                if (dorm != null) {
                    Row(
                        Modifier.fillMaxWidth().background(HC.violet50, RoundedCornerShape(16.dp)).padding(12.dp),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Icon(Icons.Filled.Lock, null, tint = HC.violet600)
                        Column(Modifier.weight(1f)) {
                            Text(stringResource(R.string.presence_in_my_dorm, dorm), fontWeight = FontWeight.Bold, color = HC.ink900)
                            Text(stringResource(R.string.presence_dorm_required), fontSize = 12.sp, color = HC.violet800)
                        }
                        Icon(Icons.Filled.CheckCircle, null, tint = HC.violet600)
                    }
                }
                CampusSwitch(campus) { campus = it }
            }
            Column(
                Modifier.fillMaxWidth().background(HC.sky50, Card).padding(16.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp),
            ) {
                Text(stringResource(R.string.presence_perms_title), fontWeight = FontWeight.Bold, fontSize = 13.sp, color = HC.sky900)
                Text(stringResource(R.string.presence_perms_body), fontSize = 13.sp, color = HC.sky900, lineHeight = 22.sp)
            }
            BrandButton(stringResource(R.string.presence_continue)) {
                ask.launch(
                    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                        arrayOf(Manifest.permission.BLUETOOTH_SCAN, Manifest.permission.BLUETOOTH_ADVERTISE, Manifest.permission.BLUETOOTH_CONNECT)
                    } else {
                        arrayOf(Manifest.permission.ACCESS_FINE_LOCATION)
                    },
                )
            }
            TextButton(onClick = onLater, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text(stringResource(R.string.presence_later), color = HC.ink600, fontWeight = FontWeight.Bold)
            }
        }
    }
}

// ------------------------------------------------------------------ my presence

@Composable
private fun MyPresence(api: HamyarApi, onBack: (() -> Unit)?, openTrip: (String) -> Unit, openClasses: (() -> Unit)? = null, logout: () -> Unit) {
    val scope = rememberCoroutineScope()
    var info by remember { mutableStateOf(api.cachedPresence) }
    var groups by remember { mutableStateOf(api.cachedGroups) }
    var error by remember { mutableStateOf<String?>(null) }
    val radio by PresenceRadio.state.collectAsState()
    LaunchedEffect(Unit) {
        while (true) {
            runCatching { api.presence() }.onSuccess { info = it; error = null }.onFailure { error = (it as? HamyarError)?.code ?: "network" }
            runCatching { api.groups() }.onSuccess { groups = it }
            if (!api.loggedIn) { logout(); return@LaunchedEffect }
            delay(60_000)
        }
    }
    val i = info
    val status = i?.status ?: "outside"
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Column(
            Modifier.fillMaxWidth().background(HC.brand900, RoundedCornerShape(bottomStart = 32.dp, bottomEnd = 32.dp))
                .statusBarsPadding().padding(start = 20.dp, end = 20.dp, top = 12.dp, bottom = 48.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                if (onBack != null) IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White) }
                else Spacer(Modifier.size(48.dp))
                Column(Modifier.weight(1f), horizontalAlignment = Alignment.CenterHorizontally) {
                    Text(stringResource(R.string.presence_title), color = Color.White, fontWeight = FontWeight.Black, fontSize = 16.sp)
                    Text(api.university, color = Color.White.copy(alpha = .6f), fontSize = 12.sp)
                }
                Spacer(Modifier.size(48.dp))
            }
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                Box(
                    Modifier.size(56.dp).background(if (status == "outside") Color.White.copy(alpha = .12f) else HC.amber, RoundedCornerShape(18.dp)),
                    contentAlignment = Alignment.Center,
                ) { Icon(if (status == "dorm") Icons.Filled.Home else Icons.Filled.LocationOn, null, tint = if (status == "outside") Color.White else HC.ink900) }
                Column {
                    Text(stringResource(statusLabel(status)), color = Color.White, fontSize = 24.sp, fontWeight = FontWeight.Black)
                    Text(i?.place ?: stringResource(R.string.presence_no_place), color = Color.White.copy(alpha = .75f), fontSize = 13.sp)
                    if (radio.lastHeard > 0) {
                        Text(stringResource(R.string.presence_heard_ago, minutesAgo(radio.lastHeard)), color = Color(0xFF6EE7B7), fontSize = 12.sp)
                    }
                }
            }
            Row(
                Modifier.fillMaxWidth().background(Color.White.copy(alpha = .08f), RoundedCornerShape(16.dp)).padding(4.dp),
                horizontalArrangement = Arrangement.spacedBy(4.dp),
            ) {
                listOf("uni", "dorm", "outside").forEach { z ->
                    val on = z == status
                    Box(
                        Modifier.weight(1f).heightIn(min = 40.dp).background(if (on) Color.White else Color.Transparent, RoundedCornerShape(12.dp)),
                        contentAlignment = Alignment.Center,
                    ) { Text(stringResource(statusLabel(z)), fontSize = 12.sp, fontWeight = FontWeight.Bold, color = if (on) HC.brand900 else Color.White.copy(alpha = .7f)) }
                }
            }
        }
        Column(Modifier.padding(horizontal = 20.dp).offset(y = (-24).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (error != null && error != "unauthorized") {
                Text(errorText(error!!), color = HC.rose, fontSize = 13.sp, modifier = Modifier.padding(top = 32.dp))
            }
            val dorm = i?.requiredDorm
            if (dorm != null) {
                Row(
                    Modifier.fillMaxWidth().background(HC.violet50, Card).border(1.dp, Color(0xFFDDD6FE), Card).padding(16.dp),
                    horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Icon(Icons.Filled.Lock, null, tint = HC.violet600)
                    Column {
                        Text(stringResource(R.string.presence_dorm_rule, dorm), fontWeight = FontWeight.Bold, color = HC.ink900)
                        Text(stringResource(R.string.presence_dorm_rule_body), fontSize = 12.sp, color = HC.violet800, lineHeight = 20.sp)
                    }
                }
            }
            WhiteCard {
                CampusSwitch(i?.optIn ?: false) { on ->
                    scope.launch {
                        runCatching { api.setOptIn(on) }.onSuccess { info = it; PresenceRadio.poke() }.onFailure { error = (it as? HamyarError)?.code ?: "network" }
                    }
                }
            }
            if (openClasses != null) {
                Row(
                    Modifier.fillMaxWidth().background(Color.White, Card).border(1.dp, HC.sand200, Card).clickable(onClick = openClasses).padding(16.dp),
                    verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                ) {
                    Box(Modifier.size(40.dp).background(HC.brand50, RoundedCornerShape(14.dp)), contentAlignment = Alignment.Center) { Icon(Icons.Filled.Person, null, tint = HC.brand700) }
                    Column(Modifier.weight(1f)) {
                        Text("کلاس‌های من", fontWeight = FontWeight.Bold, color = HC.ink900)
                        Text("حضور سر کلاس، از نود کلاس", fontSize = 12.sp, color = HC.ink600)
                    }
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = HC.ink600)
                }
            }
            if (groups.isNotEmpty()) {
                WhiteCard {
                    Text(stringResource(R.string.trip_groups_title), fontWeight = FontWeight.Black, color = HC.ink900)
                    groups.forEach { g ->
                        Row(
                            Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable { openTrip(g.tripId) },
                            verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                        ) {
                            Box(Modifier.size(40.dp).background(Color(0xFFFFFBEB), RoundedCornerShape(14.dp)), contentAlignment = Alignment.Center) {
                                Icon(Icons.Filled.Person, null, tint = Color(0xFFD97706))
                            }
                            Column(Modifier.weight(1f)) {
                                Text(g.tripName, fontWeight = FontWeight.Bold, color = HC.ink900)
                                Text(
                                    stringResource(if (g.joined) R.string.trip_group_joined else R.string.trip_group_not_joined),
                                    fontSize = 12.sp, color = if (g.joined) HC.brand600 else HC.ink600,
                                )
                            }
                            Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, null, tint = HC.ink600)
                        }
                    }
                }
            }
            WhiteCard {
                Text(stringResource(R.string.presence_shared_title), fontWeight = FontWeight.Black, color = HC.ink900)
                Text(stringResource(R.string.presence_shared_body), fontSize = 13.sp, color = HC.ink600, lineHeight = 22.sp)
            }
            Row(
                Modifier.fillMaxWidth().background(Color.White, Card).border(1.dp, HC.sand200, Card).padding(16.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                Icon(Icons.Filled.Info, null, tint = if (radio.running) HC.brand600 else HC.rose)
                Text(stringResource(radioLabel(radio)), fontSize = 13.sp, color = HC.ink900, lineHeight = 20.sp, modifier = Modifier.weight(1f))
            }
            TextButton(onClick = logout, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text(stringResource(R.string.hamyar_logout, api.userName), color = HC.ink600)
            }
        }
    }
}

// ------------------------------------------------------------------ trip group

/** One trip's group: my state and the join switch; leaders also see every member, separated first. */
@Composable
internal fun TripGroupScreen(api: HamyarApi, tripId: String, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var view by remember { mutableStateOf<GroupView?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var busy by remember { mutableStateOf(false) }
    val radio by PresenceRadio.state.collectAsState()
    suspend fun load() {
        runCatching { api.group(tripId) }.onSuccess { view = it; error = null }.onFailure { error = (it as? HamyarError)?.code ?: "network" }
    }
    LaunchedEffect(tripId) {
        while (true) { load(); delay(60_000) }
    }
    val v = view
    val state = v?.myState ?: "unknown"
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
        Header(v?.tripName ?: "", stringResource(R.string.trip_groups_body), onBack)
        Column(Modifier.padding(horizontal = 20.dp).offset(y = (-24).dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            if (error != null) Text(errorText(error!!), color = HC.rose, fontSize = 13.sp, modifier = Modifier.padding(top = 32.dp))
            if (v != null && !v.active) {
                WhiteCard { Text(stringResource(R.string.trip_group_off), color = HC.ink600) }
            }
            if (v != null && v.active) {
                WhiteCard {
                    if (v.joined) {
                        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                            Box(Modifier.size(48.dp).background(stateColor(state), RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                                Icon(Icons.Filled.Person, null, tint = Color.White)
                            }
                            Column {
                                Text(stringResource(stateLabel(state)), fontSize = 20.sp, fontWeight = FontWeight.Black, color = HC.ink900)
                                if (radio.groupTrip == tripId && radio.lastMemberHeard > 0) {
                                    Text(stringResource(R.string.trip_group_heard_ago, minutesAgo(radio.lastMemberHeard)), fontSize = 12.sp, color = HC.brand600)
                                }
                            }
                        }
                    }
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 56.dp).toggleable(value = v.joined, enabled = !busy, role = Role.Switch) { on ->
                            busy = true
                            scope.launch {
                                runCatching { api.setJoined(tripId, on) }.onFailure { error = (it as? HamyarError)?.code ?: "network" }
                                PresenceRadio.poke()
                                load()
                                busy = false
                            }
                        },
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(stringResource(R.string.trip_group_join), fontWeight = FontWeight.Bold, color = HC.ink900)
                            Text(stringResource(R.string.trip_group_join_body), fontSize = 12.sp, color = HC.ink600, lineHeight = 20.sp)
                        }
                        Switch(checked = v.joined, onCheckedChange = null, colors = SwitchDefaults.colors(checkedTrackColor = HC.brand600))
                    }
                    if (v.leaders.isNotEmpty()) {
                        Text(stringResource(R.string.trip_group_leaders, v.leaders.joinToString("، ")), fontSize = 13.sp, color = HC.ink600)
                    }
                    Text(stringResource(R.string.trip_group_rule, v.sepMin), fontSize = 12.sp, color = HC.ink600)
                }
                if (v.isLeader) {
                    WhiteCard {
                        Text(
                            stringResource(R.string.trip_group_members, v.counts["joined"] ?: 0, v.counts["registered"] ?: 0),
                            fontWeight = FontWeight.Black, color = HC.ink900,
                        )
                        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                            listOf("separated", "far", "together").forEach { k ->
                                Column(
                                    Modifier.weight(1f).background(stateColor(k).copy(alpha = .12f), RoundedCornerShape(14.dp)).padding(10.dp),
                                    horizontalAlignment = Alignment.CenterHorizontally,
                                ) {
                                    Text("${v.counts[k] ?: 0}", fontSize = 20.sp, fontWeight = FontWeight.Black, color = stateColor(k))
                                    Text(stringResource(stateLabel(k)), fontSize = 11.sp, color = HC.ink600)
                                }
                            }
                        }
                        v.members.forEach { m ->
                            Row(
                                Modifier.fillMaxWidth().heightIn(min = 48.dp),
                                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp),
                            ) {
                                Box(Modifier.size(10.dp).background(stateColor(m.state), RoundedCornerShape(5.dp)))
                                Column(Modifier.weight(1f)) {
                                    Text(m.name, fontWeight = FontWeight.Bold, fontSize = 14.sp, color = HC.ink900)
                                    val sub = if (m.state != "together" && m.lastTogether != null) {
                                        stringResource(R.string.trip_group_last_together, minutesAgo(m.lastTogether))
                                    } else stringResource(stateLabel(m.state))
                                    Text(sub, fontSize = 12.sp, color = HC.ink600)
                                }
                                val phone = m.phone
                                if (phone != null && m.state != "together") {
                                    TextButton(onClick = {
                                        runCatching { ctx.startActivity(Intent(Intent.ACTION_DIAL, Uri.parse("tel:$phone"))) }
                                    }) { Text(stringResource(R.string.trip_group_call), color = HC.brand600, fontWeight = FontWeight.Bold) }
                                }
                            }
                        }
                    }
                }
            }
        }
    }
}

private fun stateLabel(s: String) = when (s) {
    "together" -> R.string.trip_state_together
    "far" -> R.string.trip_state_far
    "separated" -> R.string.trip_state_separated
    else -> R.string.trip_state_unknown
}

private fun stateColor(s: String) = when (s) {
    "together" -> HC.brand600
    "far" -> Color(0xFFD97706)
    "separated" -> HC.rose
    else -> HC.ink600
}

private fun statusLabel(s: String) = when (s) {
    "uni" -> R.string.presence_in_uni
    "dorm" -> R.string.presence_in_dorm
    else -> R.string.presence_outside
}

private fun radioLabel(r: RadioState) = when (r.idle) {
    null -> R.string.presence_radio_on
    "bluetooth" -> R.string.presence_radio_bt_off
    "permission" -> R.string.presence_radio_permission
    "not_counted" -> R.string.presence_radio_not_counted
    else -> R.string.presence_radio_off
}

private fun minutesAgo(t: Long) = ((System.currentTimeMillis() / 1000 - t) / 60).coerceAtLeast(0).toInt()

// ------------------------------------------------------------------ pieces

@Composable
private fun Header(title: String, sub: String, onBack: () -> Unit) {
    Column(
        Modifier.fillMaxWidth().background(HC.brand900, RoundedCornerShape(bottomStart = 32.dp, bottomEnd = 32.dp))
            .statusBarsPadding().padding(start = 12.dp, end = 20.dp, top = 12.dp, bottom = 48.dp),
    ) {
        IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back), tint = Color.White) }
        Text(title, color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.Black, modifier = Modifier.padding(horizontal = 8.dp))
        Text(sub, color = Color.White.copy(alpha = .7f), fontSize = 13.sp, lineHeight = 22.sp, modifier = Modifier.padding(horizontal = 8.dp))
    }
}

@Composable
private fun WhiteCard(content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxWidth().background(Color.White, Card).border(1.dp, HC.sand200, Card).padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) { content() }
}

@Composable
private fun Bullet(icon: ImageVector, tint: Color, bg: Color, title: Int, body: Int) {
    Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
        Box(Modifier.size(40.dp).background(bg, RoundedCornerShape(14.dp)), contentAlignment = Alignment.Center) { Icon(icon, null, tint = tint) }
        Column(Modifier.weight(1f)) {
            Text(stringResource(title), fontWeight = FontWeight.Bold, fontSize = 14.sp, color = HC.ink900)
            Text(stringResource(body), fontSize = 12.sp, color = HC.ink600, lineHeight = 20.sp)
        }
    }
}

@Composable
private fun CampusSwitch(on: Boolean, change: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp).toggleable(value = on, role = Role.Switch, onValueChange = change),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(Modifier.weight(1f)) {
            Text(stringResource(R.string.presence_campus), fontWeight = FontWeight.Bold, color = HC.ink900)
            Text(stringResource(R.string.presence_campus_body), fontSize = 12.sp, color = HC.ink600)
        }
        Switch(checked = on, onCheckedChange = null, colors = SwitchDefaults.colors(checkedTrackColor = HC.brand600))
    }
}

@Composable
private fun BrandButton(text: String, enabled: Boolean = true, busy: Boolean = false, onClick: () -> Unit) {
    Button(
        onClick = onClick, enabled = enabled, shape = RoundedCornerShape(16.dp),
        colors = ButtonDefaults.buttonColors(containerColor = HC.brand600, contentColor = Color.White),
        modifier = Modifier.fillMaxWidth().heightIn(min = 56.dp),
    ) {
        if (busy) CircularProgressIndicator(Modifier.size(20.dp), color = Color.White, strokeWidth = 2.dp)
        else Text(text, fontWeight = FontWeight.Bold, fontSize = 16.sp)
    }
}
