package org.meshmsg.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.pluralStringResource
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.data.LoRaNode
import org.meshmsg.app.data.NetStatus
import org.meshmsg.app.data.PathInfo
import org.meshmsg.app.data.PathState
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun NetworkScreen(
    status: NetStatus,
    bluetoothAllowed: Boolean,
    onAllowBluetooth: () -> Unit,
    onConnectUsb: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Scaffold(modifier = modifier, topBar = { TopAppBar(title = { Text(stringResource(R.string.nav_network)) }) }) { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).verticalScroll(rememberScrollState()).padding(Space.l),
            verticalArrangement = Arrangement.spacedBy(Space.m),
        ) {
            Summary(status.anyPathUp)

            val nearby = status.paths.filter { it.kind == PathInfo.Kind.Nearby }
            val lora = status.paths.filter { it.kind == PathInfo.Kind.LoRa }
            val relays = status.paths.filter { it.kind == PathInfo.Kind.Relay }

            // Nearby phones
            if (!bluetoothAllowed || nearby.isEmpty()) {
                PathCard(MeshIcons.Bluetooth, stringResource(R.string.path_nearby), PathState.Off, stringResource(R.string.nearby_off_body)) {
                    FilledTonalButton(onClick = onAllowBluetooth, modifier = Modifier.heightIn(min = 48.dp)) {
                        Text(stringResource(R.string.allow_bluetooth))
                    }
                }
            } else {
                nearby.forEach {
                    // The core reports "2 peers"; say it in the user's language.
                    val count = it.detail.takeWhile(Char::isDigit).toIntOrNull() ?: 0
                    val detail = if (it.state == PathState.Up && count > 0) pluralStringResource(R.plurals.nearby_phones, count, count)
                    else stringResource(R.string.nearby_searching)
                    PathCard(MeshIcons.Bluetooth, stringResource(R.string.path_nearby), it.state, detail)
                }
            }

            // LoRa
            if (lora.isEmpty()) {
                PathCard(MeshIcons.Antenna, stringResource(R.string.path_lora), PathState.Off, stringResource(R.string.lora_none_body)) {
                    FilledTonalButton(onClick = onConnectUsb, modifier = Modifier.heightIn(min = 48.dp)) {
                        Text(stringResource(R.string.connect_usb_node))
                    }
                }
            } else {
                lora.forEach { p ->
                    val node = status.lora.firstOrNull { it.name == p.name }
                    val waiting = if (node?.connected == true) "" else stringResource(R.string.lora_waiting)
                    PathCard(MeshIcons.Antenna, stringResource(R.string.path_lora), p.state, waiting) {
                        if (node != null && node.connected) LoRaDetails(node)
                    }
                }
            }

            // Relays
            if (relays.isEmpty()) {
                PathCard(MeshIcons.Globe, stringResource(R.string.path_relay), PathState.Off, stringResource(R.string.relay_none_body))
            } else {
                relays.forEach {
                    val detail = stringResource(if (it.state == PathState.Up) R.string.relay_up else R.string.relay_waiting)
                    PathCard(MeshIcons.Globe, "${stringResource(R.string.path_relay)} · ${it.name}", it.state, detail)
                }
            }

            Card {
                Text(stringResource(R.string.stats_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.semantics { heading() })
                val locale = currentLocale()
                Stat(stringResource(R.string.stat_stored), pluralStringResource(R.plurals.stat_stored_value, status.stored, status.stored))
                Stat(stringResource(R.string.stat_known), "%d".format(locale, status.knownNodes))
                Stat(stringResource(R.string.stat_forwarded), "%d".format(locale, status.forwarded))
                Stat(stringResource(R.string.stat_gateway), stringResource(if (status.gateway) R.string.on else R.string.off))
            }
        }
    }
}

@Composable
private fun Summary(up: Boolean) {
    Column(verticalArrangement = Arrangement.spacedBy(Space.xs), modifier = Modifier.padding(bottom = Space.s)) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.s)) {
            StateDot(if (up) PathState.Up else PathState.Waiting)
            Text(
                stringResource(if (up) R.string.network_summary_up else R.string.network_summary_down),
                style = MaterialTheme.typography.titleLarge,
                modifier = Modifier.semantics { heading() },
            )
        }
        if (!up) {
            Text(stringResource(R.string.network_summary_down_body), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun stateColor(state: PathState): Color = when (state) {
    PathState.Up -> MeshTheme.extra.success
    PathState.Waiting -> MeshTheme.extra.warning
    PathState.Off -> MaterialTheme.colorScheme.outline
}

@Composable
private fun StateDot(state: PathState) {
    Box(Modifier.size(10.dp).background(stateColor(state), CircleShape))
}

@Composable
private fun Card(content: @Composable () -> Unit) {
    Column(
        Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer, RoundedCornerShape(16.dp)).padding(Space.l),
        verticalArrangement = Arrangement.spacedBy(Space.s),
    ) { content() }
}

/** One path: icon + name, a dot *and* the state in words, detail, optional action. */
@Composable
private fun PathCard(icon: ImageVector, title: String, state: PathState, detail: String, extra: @Composable () -> Unit = {}) {
    Card {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Space.m)) {
            Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.size(24.dp))
            Text(title, style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            StateDot(state)
            Text(
                stringResource(
                    when (state) {
                        PathState.Up -> R.string.path_state_up
                        PathState.Waiting -> R.string.path_state_waiting
                        PathState.Off -> R.string.path_state_off
                    },
                ),
                style = MaterialTheme.typography.labelLarge,
                color = MaterialTheme.colorScheme.onSurface,
            )
        }
        if (detail.isNotBlank()) {
            Text(detail, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        extra()
    }
}

@Composable
private fun LoRaDetails(n: LoRaNode) {
    val locale = currentLocale()
    Stat(stringResource(R.string.lora_role), n.role)
    Stat(stringResource(R.string.lora_region), "%s · %.3f MHz".format(locale, n.region, n.freqHz / 1e6))
    Stat(stringResource(R.string.lora_preset), "%s · %d dBm".format(locale, n.preset, n.txDbm))
    Stat(stringResource(R.string.lora_neighbours), "%d".format(locale, n.neighbours))
    if (n.airBudgetMs > 0) {
        Stat(stringResource(R.string.lora_airtime), stringResource(R.string.percent, (n.airUsedMs * 100 / n.airBudgetMs).toInt()))
    }
    if (n.batteryMv > 0) Stat(stringResource(R.string.lora_battery), stringResource(R.string.millivolts, n.batteryMv))
    Text(stringResource(R.string.lora_region_warning), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
}

@Composable
private fun Stat(label: String, value: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.spacedBy(Space.l)) {
        Text(label, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.weight(1f))
        Text(value, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
    }
}
