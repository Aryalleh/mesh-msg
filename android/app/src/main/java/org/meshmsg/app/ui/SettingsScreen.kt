package org.meshmsg.app.ui

import android.content.Intent
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.PickVisualMediaRequest
import androidx.activity.result.contract.ActivityResultContracts
import android.provider.Settings
import androidx.compose.ui.platform.LocalContext
import org.meshmsg.app.service.MeshService
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
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.compose.foundation.selection.toggleable
import androidx.compose.material3.ListItem
import androidx.compose.foundation.clickable
import androidx.compose.material3.Switch
import androidx.compose.ui.semantics.Role
import org.meshmsg.app.R
import org.meshmsg.app.data.Relay
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

data class SettingsState(
    val displayName: String = "",
    val myHash: String = "",
    val relays: List<Relay> = emptyList(),
    val appVersion: String = "",
    val readReceipts: Boolean = true,
)

interface SettingsActions {
    fun setName(name: String)
    fun addRelay(relay: Relay)
    fun removeRelay(relay: Relay)
    fun wipe()

    /** Sets (image bytes) or removes (null) this account's profile picture. */
    fun setAvatar(image: ByteArray?) {}

    fun setReadReceipts(on: Boolean) {}

    /** Opens the storage screen (files on this phone). */
    fun openStorage() {}

    /** Checks for a newer release now (roadmap B7); the result shows on the chat list. */
    fun checkUpdate() {}

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
            ProfilePicture(state, actions)
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
            Text(
                stringResource(R.string.settings_mesh_fixed),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(horizontal = Space.l, vertical = Space.s),
            )
            HideRunningNotification()
            ListItem(
                headlineContent = { Text(stringResource(R.string.storage_title)) },
                supportingContent = { Text(stringResource(R.string.storage_entry_help)) },
                modifier = Modifier.clickable(onClick = actions::openStorage),
            )
            FullScreenCallsItem()
            ListItem(
                headlineContent = { Text(stringResource(R.string.read_receipts)) },
                supportingContent = { Text(stringResource(R.string.read_receipts_help)) },
                trailingContent = { Switch(checked = state.readReceipts, onCheckedChange = actions::setReadReceipts) },
                modifier = Modifier.toggleable(value = state.readReceipts, role = Role.Switch, onValueChange = actions::setReadReceipts),
            )

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

            SectionHeader(stringResource(R.string.settings_about))
            Text(
                stringResource(R.string.settings_version, state.appVersion),
                style = MaterialTheme.typography.bodyLarge,
                modifier = Modifier.padding(horizontal = Space.l),
            )
            OutlinedButton(onClick = actions::checkUpdate, modifier = Modifier.padding(horizontal = Space.l, vertical = Space.s).heightIn(min = 48.dp)) {
                Text(stringResource(R.string.update_check))
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

/** Guide to switching off the "Mava is running" notification channel. */
@Composable
private fun HideRunningNotification() {
    val context = LocalContext.current
    Column(Modifier.padding(horizontal = Space.l, vertical = Space.s)) {
        Text(stringResource(R.string.notif_hide_title), style = MaterialTheme.typography.titleSmall)
        Text(
            stringResource(R.string.notif_hide_steps),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.padding(top = Space.xs),
        )
        OutlinedButton(
            onClick = {
                val channel = Intent(Settings.ACTION_CHANNEL_NOTIFICATION_SETTINGS)
                    .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                    .putExtra(Settings.EXTRA_CHANNEL_ID, MeshService.CH_MESH)
                val app = Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS)
                    .putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
                runCatching { context.startActivity(channel) }.recoverCatching { context.startActivity(app) }
            },
            modifier = Modifier.padding(top = Space.s).heightIn(min = 48.dp),
        ) { Text(stringResource(R.string.notif_hide_open)) }
    }
}

/** This account's picture: picked from the gallery, shrunk on the phone. */
@Composable
private fun ProfilePicture(state: SettingsState, actions: SettingsActions) {
    val context = LocalContext.current
    val hasPhoto = org.meshmsg.app.data.LocalAvatars.current.containsKey("me")
    val pick = rememberLauncherForActivityResult(ActivityResultContracts.PickVisualMedia()) { uri ->
        if (uri != null) {
            runCatching { context.contentResolver.openInputStream(uri)?.use { it.readBytes() } }.getOrNull()?.let(actions::setAvatar)
        }
    }
    Row(
        Modifier.fillMaxWidth().padding(horizontal = Space.l, vertical = Space.s),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Space.l),
    ) {
        Avatar(org.meshmsg.app.data.Contact(state.myHash, state.displayName, ""), size = 72.dp, photoKey = "me")
        Column(Modifier.weight(1f)) {
            Row(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                OutlinedButton(
                    onClick = { pick.launch(PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.ImageOnly)) },
                    modifier = Modifier.heightIn(min = 48.dp),
                ) { Text(stringResource(R.string.avatar_set)) }
                if (hasPhoto) {
                    TextButton(onClick = { actions.setAvatar(null) }, modifier = Modifier.heightIn(min = 48.dp)) {
                        Text(stringResource(R.string.avatar_remove))
                    }
                }
            }
            Text(
                stringResource(R.string.avatar_help),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(top = Space.xs),
            )
        }
    }
}

/** Shown only while Android (14+) refuses full-screen incoming calls; opens the system page. */
@Composable
private fun FullScreenCallsItem() {
    val ctx = androidx.compose.ui.platform.LocalContext.current
    var allowed by remember { mutableStateOf(true) }
    androidx.lifecycle.compose.LifecycleResumeEffect(Unit) {
        allowed = org.meshmsg.app.service.CallService.canFullScreen(ctx)
        onPauseOrDispose {}
    }
    if (allowed || android.os.Build.VERSION.SDK_INT < 34) return
    ListItem(
        headlineContent = { Text(stringResource(R.string.settings_full_screen)) },
        supportingContent = { Text(stringResource(R.string.settings_full_screen_off), color = MaterialTheme.colorScheme.error) },
        modifier = Modifier.clickable {
            runCatching {
                ctx.startActivity(
                    android.content.Intent(android.provider.Settings.ACTION_MANAGE_APP_USE_FULL_SCREEN_INTENT, android.net.Uri.parse("package:" + ctx.packageName)),
                )
            }
        },
    )
}
