package org.meshmsg.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.selection.selectable
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.data.BatteryMode
import org.meshmsg.app.data.Relay
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

data class SettingsState(
    val displayName: String = "",
    val myHash: String = "",
    val stealth: Boolean = false,
    val gateway: Boolean = false,
    val battery: BatteryMode = BatteryMode.Active,
    val relays: List<Relay> = emptyList(),
)

interface SettingsActions {
    fun setName(name: String)
    fun setStealth(on: Boolean)
    fun setGateway(on: Boolean)
    fun setBattery(mode: BatteryMode)
    fun addRelay(relay: Relay)
    fun removeRelay(relay: Relay)
    fun wipe()

    /** The account key sealed with [passphrase] (meshkey1:…). */
    suspend fun exportIdentity(passphrase: String): Result<String>
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(state: SettingsState, actions: SettingsActions, modifier: Modifier = Modifier) {
    var name by rememberSaveable(state.displayName) { mutableStateOf(state.displayName) }
    var addingRelay by rememberSaveable { mutableStateOf(false) }
    var wiping by rememberSaveable { mutableStateOf(false) }
    var backingUp by rememberSaveable { mutableStateOf(false) }

    Scaffold(modifier = modifier, topBar = { TopAppBar(title = { Text(stringResource(R.string.nav_settings)) }) }) { pad ->
        Column(Modifier.fillMaxSize().padding(pad).imePadding().verticalScroll(rememberScrollState())) {
            SectionHeader(stringResource(R.string.settings_identity))
            OutlinedTextField(
                value = name,
                onValueChange = { name = it.take(32); actions.setName(name) },
                label = { Text(stringResource(R.string.settings_name_label)) },
                supportingText = { Text(stringResource(R.string.settings_name_helper)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                modifier = Modifier.fillMaxWidth().padding(horizontal = Space.l),
            )
            Column(Modifier.padding(horizontal = Space.l, vertical = Space.s)) {
                Text(stringResource(R.string.settings_your_id), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Text(state.myHash, style = MeshTheme.mono)
            }
            Text(
                stringResource(R.string.backup_settings_body),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = Space.l),
            )
            OutlinedButton(
                onClick = { backingUp = true },
                enabled = state.myHash.isNotEmpty(),
                modifier = Modifier.padding(horizontal = Space.l, vertical = Space.s).heightIn(min = 48.dp),
            ) { Text(stringResource(R.string.backup_title)) }

            SectionHeader(stringResource(R.string.settings_mesh))
            SwitchRow(stringResource(R.string.settings_stealth), stringResource(R.string.settings_stealth_body), state.stealth, actions::setStealth)
            SwitchRow(stringResource(R.string.settings_gateway), stringResource(R.string.settings_gateway_body), state.gateway, actions::setGateway)

            SectionHeader(stringResource(R.string.settings_battery))
            BatteryMode.entries.forEach { mode ->
                val label = stringResource(
                    when (mode) {
                        BatteryMode.Active -> R.string.battery_active
                        BatteryMode.Saver -> R.string.battery_saver
                        BatteryMode.Off -> R.string.battery_off
                    },
                )
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 56.dp)
                        .selectable(selected = state.battery == mode, role = Role.RadioButton, onClick = { actions.setBattery(mode) })
                        .padding(horizontal = Space.l),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Space.m),
                ) {
                    RadioButton(selected = state.battery == mode, onClick = null)
                    Text(label, style = MaterialTheme.typography.bodyLarge)
                }
            }

            SectionHeader(stringResource(R.string.settings_relays))
            if (state.relays.isEmpty()) {
                Text(
                    stringResource(R.string.settings_relays_empty),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(horizontal = Space.l),
                )
            }
            state.relays.forEach { relay ->
                Row(
                    Modifier.fillMaxWidth().heightIn(min = 56.dp).padding(start = Space.l, end = Space.xs),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Column(Modifier.weight(1f)) {
                        Text(relay.name, style = MaterialTheme.typography.bodyLarge)
                        Text(relay.url, style = MeshTheme.mono, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    IconButton(onClick = { actions.removeRelay(relay) }) {
                        Icon(Icons.Filled.Delete, stringResource(R.string.relay_remove, relay.name), tint = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
            }
            OutlinedButton(onClick = { addingRelay = true }, modifier = Modifier.padding(Space.l).heightIn(min = 48.dp)) {
                Icon(Icons.Filled.Add, contentDescription = null)
                Text(stringResource(R.string.relay_add), Modifier.padding(start = Space.s))
            }

            // Destructive: separated, error colour, typed confirmation.
            HorizontalDivider(Modifier.padding(top = Space.l))
            SectionHeader(stringResource(R.string.settings_danger))
            Text(
                stringResource(R.string.wipe_body),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = Space.l),
            )
            OutlinedButton(
                onClick = { wiping = true },
                colors = ButtonDefaults.outlinedButtonColors(contentColor = MaterialTheme.colorScheme.error),
                modifier = Modifier.padding(Space.l).heightIn(min = 48.dp),
            ) { Text(stringResource(R.string.wipe)) }
            Spacer(Modifier.height(Space.xl))
        }
    }

    if (addingRelay) RelayDialog(onDismiss = { addingRelay = false }, onAdd = { addingRelay = false; actions.addRelay(it) })
    if (backingUp) BackupDialog(export = actions::exportIdentity, onDismiss = { backingUp = false })
    if (wiping) WipeDialog(onDismiss = { wiping = false }, onConfirm = { wiping = false; actions.wipe() })
}

@Composable
private fun SwitchRow(title: String, body: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp)
            .toggleable(value = checked, role = Role.Switch, onValueChange = onChange)
            .padding(horizontal = Space.l, vertical = Space.m),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Space.l),
    ) {
        Column(Modifier.weight(1f)) {
            Text(title, style = MaterialTheme.typography.bodyLarge)
            Text(body, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Switch(checked = checked, onCheckedChange = null)
    }
}

@Composable
private fun RelayDialog(onDismiss: () -> Unit, onAdd: (Relay) -> Unit) {
    var name by rememberSaveable { mutableStateOf("") }
    var url by rememberSaveable { mutableStateOf("") }
    var checked by rememberSaveable { mutableStateOf(false) }
    val urlOk = url.startsWith("https://") || url.startsWith("http://")
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.relay_add)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Space.s)) {
                OutlinedTextField(name, { name = it.take(24) }, label = { Text(stringResource(R.string.relay_name_label)) }, singleLine = true)
                OutlinedTextField(
                    url, { url = it.trim(); checked = false },
                    label = { Text(stringResource(R.string.relay_url_label)) },
                    isError = checked && !urlOk,
                    supportingText = { if (checked && !urlOk) Text(stringResource(R.string.relay_url_invalid)) },
                    singleLine = true,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, autoCorrectEnabled = false),
                )
            }
        },
        confirmButton = {
            TextButton(
                onClick = { checked = true; if (urlOk) onAdd(Relay(name.trim().ifEmpty { "relay" }, url)) },
                enabled = url.isNotBlank(),
            ) { Text(stringResource(R.string.save)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}

@Composable
private fun WipeDialog(onDismiss: () -> Unit, onConfirm: () -> Unit) {
    val word = stringResource(R.string.wipe_confirm_word)
    var typed by rememberSaveable { mutableStateOf("") }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(stringResource(R.string.wipe_confirm_title)) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(Space.m)) {
                Text(stringResource(R.string.wipe_confirm_body))
                OutlinedTextField(typed, { typed = it }, label = { Text(stringResource(R.string.wipe_confirm_label, word)) }, singleLine = true)
            }
        },
        confirmButton = {
            TextButton(
                onClick = onConfirm,
                enabled = typed.trim() == word,
                colors = ButtonDefaults.textButtonColors(contentColor = MaterialTheme.colorScheme.error),
            ) { Text(stringResource(R.string.wipe)) }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text(stringResource(R.string.cancel)) } },
    )
}
