package org.meshmsg.app.ui

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringArrayResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import java.text.DateFormat
import java.util.Date
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.meshmsg.app.R
import org.meshmsg.app.data.Contact
import org.meshmsg.app.data.Geo
import org.meshmsg.app.data.Trip
import org.meshmsg.app.data.TripMember
import org.meshmsg.app.data.TripRelay
import org.meshmsg.app.data.TripsApi
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

/** What the trips screen asks of the activity. */
class TripsActions(
    /** Asks for the location permission; done(true) once granted. */
    val requestLocation: (done: (Boolean) -> Unit) -> Unit,
    /** Sharing may have started or stopped: start the location service. */
    val sharingChanged: () -> Unit,
    /** Navigates to a point: the user picks the app (Google Maps, Neshan, Balad…). */
    val openMap: (lat: Double, lon: Double, label: String) -> Unit,
    /** "I'm lost": sends this phone's location to the trip's leaders and opens that chat. */
    val lost: (Trip) -> Unit = {},
    val share: (text: String) -> Unit,
    val copy: (text: String) -> Unit,
    /** This phone's last known position, if location is allowed. */
    val myLocation: () -> Pair<Double, Double>?,
)

/**
 * Trip location sharing (docs/trip.md). A member sees what a trip asks and
 * decides; while sharing, "I'm lost" and Stop are always one tap away.
 * Leaders see members' last positions, ask one for their position, add
 * leaders, show the trip's QR code and the key for the relay's web map.
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TripsScreen(
    api: TripsApi?,
    changes: Flow<String>,
    contacts: List<Contact>,
    actions: TripsActions,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    initialLink: String = "",
) {
    var trips by remember { mutableStateOf<List<Trip>>(emptyList()) }
    var members by remember { mutableStateOf<Map<String, List<TripMember>>>(emptyMap()) }
    var tick by remember { mutableStateOf(0) }
    val scope = rememberCoroutineScope()
    val snackbar = remember { SnackbarHostState() }
    var creating by rememberSaveable { mutableStateOf(false) }
    val needLocation = stringResource(R.string.trip_need_location)

    suspend fun reload() {
        val a = api ?: return
        val (t, m) = withContext(Dispatchers.IO) {
            val t = a.trips()
            t to t.filter { it.leader }.associate { it.hash to a.members(it.hash) }
        }
        trips = t
        members = m
    }

    fun act(block: suspend () -> Result<*>) {
        scope.launch {
            val r = withContext(Dispatchers.IO) { block() }
            r.exceptionOrNull()?.let { snackbar.showSnackbar(it.message ?: it.toString()) }
            reload()
            actions.sharingChanged()
        }
    }

    LaunchedEffect(api) {
        if (api == null) return@LaunchedEffect
        reload()
        withContext(Dispatchers.IO) { api.refresh() } // a trip on our relays?
        reload()
    }
    LaunchedEffect(api, changes) { changes.collect { reload() } }
    LaunchedEffect(api) {
        while (true) {
            delay(15_000)
            tick++
            reload()
        }
    }
    LaunchedEffect(api, initialLink) {
        if (api != null && initialLink.isNotBlank()) act { api.join(initialLink) }
    }

    Scaffold(
        modifier = modifier,
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.trips_title)) },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) }
                },
            )
        },
    ) { pad ->
        Column(Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).padding(bottom = Space.xl)) {
            Text(
                stringResource(R.string.trips_intro),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = Space.l, vertical = Space.s),
            )
            if (api == null) return@Column
            val now = remember(tick, trips) { System.currentTimeMillis() / 1000 }
            trips.forEach { t ->
                TripCard(
                    t, members[t.hash].orEmpty(), contacts, now, actions,
                    onShare = {
                        actions.requestLocation { ok ->
                            if (ok) act { api.consent(t.hash, true) }
                            else scope.launch { snackbar.showSnackbar(needLocation) }
                        }
                    },
                    onDecline = { act { api.consent(t.hash, false) } },
                    onStop = { act { api.stop(t.hash) } },
                    onSos = { on ->
                        act { api.sos(t.hash, on) }
                        if (on) actions.lost(t)
                    },
                    onForget = { act { api.end(t.hash) } },
                    onLocate = { m -> act { api.locate(t.hash, m.hash) } },
                    onAddLeader = { c -> act { api.addLeader(t.hash, c.hash) } },
                )
            }
            SectionHeader(stringResource(R.string.trip_join_title))
            JoinTrip { link -> act { api.join(link) } }
            SectionHeader(stringResource(R.string.trip_lead_title))
            Text(
                stringResource(R.string.trip_lead_help),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = Space.l),
            )
            OutlinedButton(onClick = { creating = true }, modifier = Modifier.padding(Space.l).heightIn(min = 48.dp)) {
                Text(stringResource(R.string.trip_create))
            }
        }
    }
    if (creating && api != null) {
        var relays by remember { mutableStateOf<List<TripRelay>>(emptyList()) }
        LaunchedEffect(Unit) { relays = withContext(Dispatchers.IO) { api.relays() } }
        CreateTripDialog(relays, onDismiss = { creating = false }) { relay, name, every, days ->
            creating = false
            act { api.create(relay, name, every, System.currentTimeMillis() / 1000 + days * 86400L) }
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun TripCard(
    t: Trip,
    members: List<TripMember>,
    contacts: List<Contact>,
    now: Long,
    actions: TripsActions,
    onShare: () -> Unit,
    onDecline: () -> Unit,
    onStop: () -> Unit,
    onSos: (Boolean) -> Unit,
    onForget: () -> Unit,
    onLocate: (TripMember) -> Unit,
    onAddLeader: (Contact) -> Unit,
) {
    var forgetting by remember { mutableStateOf(false) }
    var showQr by remember { mutableStateOf(false) }
    var showKey by remember { mutableStateOf(false) }
    var addingLeader by remember { mutableStateOf(false) }
    var showMap by remember { mutableStateOf(false) }
    val sosColors = ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.error, contentColor = MaterialTheme.colorScheme.onError)
    Card(
        Modifier.fillMaxWidth().padding(horizontal = Space.l, vertical = Space.s),
        colors = CardDefaults.cardColors(
            containerColor = if (t.sos) MaterialTheme.colorScheme.errorContainer else MaterialTheme.colorScheme.surfaceContainer,
        ),
    ) {
        Column(Modifier.padding(Space.l), verticalArrangement = Arrangement.spacedBy(Space.s)) {
            Text(t.name, style = MaterialTheme.typography.titleMedium)
            Text(
                stringResource(R.string.trip_until_leaders, dateTime(t.ends), t.leaderNames),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            when {
                t.ended -> Text(stringResource(R.string.trip_ended))
                t.pending -> {
                    // What the member agrees to, in plain words, before anything is sent.
                    Text(stringResource(R.string.trip_consent_body, t.leaderNames, t.every / 60, dateTime(t.ends)))
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                        Button(onClick = onShare, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_share)) }
                        TextButton(onClick = onDecline, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_decline)) }
                    }
                }
                t.sharing -> {
                    Text(
                        if (t.last > 0) stringResource(R.string.trip_sharing_last, (t.interval / 60).coerceAtLeast(1), ago(now - t.last))
                        else stringResource(R.string.trip_sharing_first),
                    )
                    if (t.fastUntil > now) Text(stringResource(R.string.trip_leader_asked), color = MeshTheme.extra.warning)
                    if (t.sos) Text(stringResource(R.string.trip_sos_on), color = MaterialTheme.colorScheme.error, fontWeight = FontWeight.SemiBold)
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s), verticalArrangement = Arrangement.spacedBy(Space.s)) {
                        if (t.sos) {
                            OutlinedButton(onClick = { onSos(false) }, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_sos_off)) }
                        } else {
                            Button(onClick = { onSos(true) }, colors = sosColors, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_sos)) }
                        }
                        OutlinedButton(onClick = onStop, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_stop)) }
                    }
                }
                else -> {
                    Text(stringResource(R.string.trip_not_sharing))
                    FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                        Button(onClick = onShare, modifier = Modifier.heightIn(min = 48.dp)) { Text(stringResource(R.string.trip_share)) }
                    }
                }
            }
            if (t.leader) {
                HorizontalDivider()
                Text(stringResource(R.string.trip_members, members.size), style = MaterialTheme.typography.titleSmall)
                if (members.isEmpty()) {
                    Text(stringResource(R.string.trip_no_members), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                val me = remember(now) { actions.myLocation() }
                members.forEach { m -> MemberRow(m, me, now, actions, onLocate) }
                FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s), verticalArrangement = Arrangement.spacedBy(Space.s)) {
                    Button(onClick = { showMap = true }) { Text(stringResource(R.string.trip_map)) }
                    OutlinedButton(onClick = { showQr = true }) { Text(stringResource(R.string.trip_show_qr)) }
                    OutlinedButton(onClick = { addingLeader = true }) { Text(stringResource(R.string.trip_add_leader)) }
                    OutlinedButton(onClick = { showKey = true }) { Text(stringResource(R.string.trip_map_key)) }
                }
            }
            TextButton(onClick = { forgetting = true }) {
                Text(stringResource(if (t.leader) R.string.trip_end else R.string.trip_forget), color = MaterialTheme.colorScheme.error)
            }
        }
    }
    if (showMap) TripMapDialog(t, members, actions, onLocate, onClose = { showMap = false })
    if (forgetting) {
        AlertDialog(
            onDismissRequest = { forgetting = false },
            title = { Text(stringResource(if (t.leader) R.string.trip_end else R.string.trip_forget)) },
            text = { Text(stringResource(if (t.leader) R.string.trip_end_body else R.string.trip_forget_body)) },
            confirmButton = { TextButton(onClick = { forgetting = false; onForget() }) { Text(stringResource(R.string.ok)) } },
            dismissButton = { TextButton(onClick = { forgetting = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
    if (showQr) {
        AlertDialog(
            onDismissRequest = { showQr = false },
            title = { Text(t.name) },
            text = {
                Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(Space.s)) {
                    Text(stringResource(R.string.trip_qr_help))
                    QrCode(t.link, stringResource(R.string.trip_show_qr))
                }
            },
            confirmButton = { TextButton(onClick = { actions.share(t.link) }) { Text(stringResource(R.string.trip_share_link)) } },
            dismissButton = { TextButton(onClick = { showQr = false }) { Text(stringResource(R.string.close)) } },
        )
    }
    if (showKey) {
        AlertDialog(
            onDismissRequest = { showKey = false },
            title = { Text(stringResource(R.string.trip_map_key)) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(Space.s)) {
                    Text(stringResource(R.string.trip_map_key_help, t.relay.trimEnd('/') + "/admin/trip"))
                    SelectionContainer { Text(t.mapKey, style = MeshTheme.mono) }
                }
            },
            confirmButton = { TextButton(onClick = { actions.copy(t.mapKey) }) { Text(stringResource(R.string.trip_copy)) } },
            dismissButton = { TextButton(onClick = { showKey = false }) { Text(stringResource(R.string.close)) } },
        )
    }
    if (addingLeader) {
        val candidates = contacts.filter { c -> !c.isGroup && t.leaders.none { it.id == c.hash } }
        AlertDialog(
            onDismissRequest = { addingLeader = false },
            title = { Text(stringResource(R.string.trip_add_leader)) },
            text = {
                Column(Modifier.verticalScroll(rememberScrollState())) {
                    Text(stringResource(R.string.trip_add_leader_help), style = MaterialTheme.typography.bodySmall)
                    if (candidates.isEmpty()) Text(stringResource(R.string.trip_add_leader_none))
                    candidates.forEach { c ->
                        Text(
                            c.displayName,
                            Modifier.fillMaxWidth().clickable { addingLeader = false; onAddLeader(c) }.padding(vertical = Space.m),
                        )
                    }
                }
            },
            confirmButton = {},
            dismissButton = { TextButton(onClick = { addingLeader = false }) { Text(stringResource(R.string.cancel)) } },
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun MemberRow(m: TripMember, me: Pair<Double, Double>?, now: Long, actions: TripsActions, onLocate: (TripMember) -> Unit) {
    val compass = stringArrayResource(R.array.trip_compass)
    Column(Modifier.fillMaxWidth().padding(vertical = Space.xs)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.s)) {
            Text(m.displayName, style = MaterialTheme.typography.bodyLarge, fontWeight = FontWeight.SemiBold)
            if (m.sos) Text(stringResource(R.string.trip_badge_sos), color = MaterialTheme.colorScheme.error, fontWeight = FontWeight.Bold)
            if (m.stopped) Text(stringResource(R.string.trip_badge_stopped), color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        val parts = mutableListOf(stringResource(R.string.trip_seen, ago(now - m.seen)))
        if (m.battery >= 0) parts += stringResource(R.string.trip_battery, m.battery)
        Text(parts.joinToString(" · "), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        if (m.hasPosition) {
            val where = buildString {
                if (me != null) {
                    val d = Geo.distance(me.first, me.second, m.lat, m.lon)
                    append(if (d < 1000) "${d.toInt()} m " else String.format(java.util.Locale.getDefault(), "%.1f km ", d / 1000))
                    append(compass[Geo.compass(Geo.bearing(me.first, me.second, m.lat, m.lon))])
                    append(" · ")
                }
                append("±${m.accuracy} m · ")
                append(ago(now - m.fix))
            }
            Text(where, style = MaterialTheme.typography.bodySmall)
        } else {
            Text(stringResource(R.string.trip_no_position), style = MaterialTheme.typography.bodySmall)
        }
        FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
            if (m.hasPosition) TextButton(onClick = { actions.openMap(m.lat, m.lon, m.displayName) }) { Text(stringResource(R.string.trip_navigate)) }
            if (!m.stopped) TextButton(onClick = { onLocate(m) }) { Text(stringResource(R.string.trip_locate)) }
        }
    }
}

@Composable
private fun JoinTrip(onJoin: (String) -> Unit) {
    var link by rememberSaveable { mutableStateOf("") }
    Column(Modifier.padding(horizontal = Space.l)) {
        Text(stringResource(R.string.trip_join_help), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        OutlinedTextField(
            value = link, onValueChange = { link = it.trim() },
            label = { Text(stringResource(R.string.trip_link)) }, singleLine = true,
            modifier = Modifier.fillMaxWidth().padding(vertical = Space.s),
        )
        Button(onClick = { onJoin(link); link = "" }, enabled = link.startsWith("meshtrip:"), modifier = Modifier.heightIn(min = 48.dp)) {
            Text(stringResource(R.string.trip_join))
        }
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun CreateTripDialog(relays: List<TripRelay>, onDismiss: () -> Unit, onCreate: (relay: String, name: String, everySec: Int, days: Int) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    var relay by rememberSaveable { mutableStateOf("") }
    var every by rememberSaveable { mutableStateOf(300) }
    var days by rememberSaveable { mutableStateOf(7) }
    if (relay.isEmpty() && relays.isNotEmpty()) relay = (relays.firstOrNull { it.up } ?: relays.first()).name
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.trip_create)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Space.s)) {
                OutlinedTextField(value = name, onValueChange = { name = it.take(64) }, label = { Text(stringResource(R.string.trip_name)) }, singleLine = true)
                Text(stringResource(R.string.trip_relay), style = MaterialTheme.typography.labelLarge)
                if (relays.isEmpty()) Text(stringResource(R.string.trip_no_relays))
                relays.forEach { r ->
                    Row(
                        Modifier.fillMaxWidth().selectable(selected = relay == r.name, role = Role.RadioButton, onClick = { relay = r.name }),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        RadioButton(selected = relay == r.name, onClick = null)
                        Column(Modifier.padding(start = Space.s)) {
                            Text(r.name)
                            Text(r.url, style = MeshTheme.mono, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
                Text(stringResource(R.string.trip_every), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    listOf(120, 300, 600, 900).forEach { s ->
                        FilterChip(selected = every == s, onClick = { every = s }, label = { Text(stringResource(R.string.trip_minutes, s / 60)) })
                    }
                }
                Text(stringResource(R.string.trip_days), style = MaterialTheme.typography.labelLarge)
                FlowRow(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    listOf(1, 3, 7, 14, 30).forEach { d ->
                        FilterChip(selected = days == d, onClick = { days = d }, label = { Text(stringResource(R.string.trip_n_days, d)) })
                    }
                }
                Spacer(Modifier.height(Space.xs))
                Text(stringResource(R.string.trip_create_help), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        },
        confirmButton = {
            TextButton(onClick = { onCreate(relay, name, every, days) }, enabled = name.isNotBlank() && relay.isNotEmpty()) {
                Text(stringResource(R.string.trip_create_ok))
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

private fun dateTime(unix: Long): String = DateFormat.getDateTimeInstance(DateFormat.MEDIUM, DateFormat.SHORT).format(Date(unix * 1000))

@Composable
private fun ago(seconds: Long): String {
    val s = seconds.coerceAtLeast(0)
    return when {
        s < 90 -> stringResource(R.string.trip_ago_s, s.toInt())
        s < 5400 -> stringResource(R.string.trip_ago_min, (s / 60).toInt())
        s < 172800 -> stringResource(R.string.trip_ago_h, (s / 3600).toInt())
        else -> stringResource(R.string.trip_ago_d, (s / 86400).toInt())
    }
}

/** The trip's members on a map, full screen (leaders). */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun TripMapDialog(t: Trip, members: List<TripMember>, actions: TripsActions, onLocate: (TripMember) -> Unit, onClose: () -> Unit) {
    var selected by remember { mutableStateOf<String?>(null) }
    val me = remember(members) { actions.myLocation() }
    val now = System.currentTimeMillis() / 1000
    androidx.compose.ui.window.Dialog(onDismissRequest = onClose, properties = androidx.compose.ui.window.DialogProperties(usePlatformDefaultWidth = false)) {
        androidx.compose.material3.Surface(Modifier.fillMaxSize()) {
            Column(Modifier.fillMaxSize()) {
                TopAppBar(
                    title = { Text(t.name) },
                    navigationIcon = { IconButton(onClick = onClose) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) } },
                )
                TripMapView(
                    members, me, staleAfterSec = 3L * t.every + 120, selected = selected,
                    onSelect = { selected = it?.hash },
                    modifier = Modifier.weight(1f).fillMaxWidth(),
                )
                val m = members.firstOrNull { it.hash == selected }
                if (m != null) {
                    Column(Modifier.padding(Space.l), verticalArrangement = Arrangement.spacedBy(Space.xs)) {
                        MemberRow(m, me, now, actions, onLocate)
                    }
                } else {
                    Text(
                        stringResource(R.string.trip_map_help, members.count { it.hasPosition }),
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.padding(Space.l),
                    )
                }
            }
        }
    }
}
