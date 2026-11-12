package org.meshmsg.app.ui

import androidx.compose.ui.semantics.Role

import androidx.compose.foundation.selection.toggleable

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.horizontalScroll
import androidx.compose.material3.SuggestionChip
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.Button
import androidx.compose.material3.ExperimentalMaterial3Api
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
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.error
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.common.BitMatrix
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import org.meshmsg.app.R
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

/** Why a pasted link was rejected; shown under the field. */
enum class LinkError { Invalid, Own }

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AddContactScreen(
    myUri: String,
    onBack: () -> Unit,
    onCopy: () -> Unit,
    onShare: () -> Unit,
    onPaste: () -> String,
    onAdd: (uri: String, name: String) -> LinkError?,
    modifier: Modifier = Modifier,
    initialLink: String = "",
    initialError: LinkError? = null,
    regions: List<String> = emptyList(),
    contacts: List<org.meshmsg.app.data.Contact> = emptyList(),
    onCreateGroup: (name: String, members: List<String>) -> Unit = { _, _ -> },
    onJoinGroup: (link: String) -> Unit = {},
) {
    var groupName by rememberSaveable { mutableStateOf("") }
    var picked by rememberSaveable { mutableStateOf(listOf<String>()) }
    var region by rememberSaveable { mutableStateOf("") }
    var link by rememberSaveable { mutableStateOf(initialLink) }
    var name by rememberSaveable { mutableStateOf("") }
    var error by rememberSaveable { mutableStateOf(initialError) }

    Scaffold(
        modifier = modifier,
        topBar = {
            TopAppBar(
                title = { Text(stringResource(R.string.new_chat)) },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, stringResource(R.string.back)) }
                },
            )
        },
    ) { pad ->
        Column(
            Modifier.fillMaxSize().padding(pad).imePadding().verticalScroll(rememberScrollState()).padding(Space.l),
            verticalArrangement = Arrangement.spacedBy(Space.l),
        ) {
            // ---- their link: the one primary action of this screen ----
            Text(stringResource(R.string.their_link_title), style = MaterialTheme.typography.titleMedium)
            val errorText = when (error) {
                LinkError.Invalid -> stringResource(R.string.link_invalid)
                LinkError.Own -> stringResource(R.string.link_is_yours)
                null -> null
            }
            OutlinedTextField(
                value = link,
                onValueChange = { link = it; error = null },
                label = { Text(stringResource(R.string.their_link_label)) },
                supportingText = { Text(errorText ?: stringResource(R.string.their_link_helper)) },
                isError = error != null,
                trailingIcon = {
                    TextButton(onClick = { link = onPaste(); error = null }) { Text(stringResource(R.string.paste)) }
                },
                maxLines = 3,
                textStyle = MeshTheme.mono,
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri, imeAction = ImeAction.Next, autoCorrectEnabled = false),
                modifier = Modifier.fillMaxWidth().semantics { if (errorText != null) error(errorText) },
            )
            OutlinedTextField(
                value = name,
                onValueChange = { name = it.take(40) },
                label = { Text(stringResource(R.string.name_optional_label)) },
                supportingText = { Text(stringResource(R.string.name_optional_helper)) },
                singleLine = true,
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Done),
                modifier = Modifier.fillMaxWidth(),
            )
            Button(
                onClick = { error = onAdd(link, name) },
                enabled = link.isNotBlank(),
                modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp),
            ) { Text(stringResource(R.string.add_contact)) }

            // ---- groups and public chats ----
            Column(
                Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer, RoundedCornerShape(16.dp)).padding(Space.l),
                verticalArrangement = Arrangement.spacedBy(Space.m),
            ) {
                Text(stringResource(R.string.groups_title), style = MaterialTheme.typography.titleMedium)
                Text(
                    stringResource(R.string.groups_body),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedTextField(
                    value = groupName,
                    onValueChange = { groupName = it.take(64) },
                    label = { Text(stringResource(R.string.group_name_label)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Text(stringResource(R.string.group_pick_members), style = MaterialTheme.typography.titleSmall)
                if (contacts.isEmpty()) {
                    Text(stringResource(R.string.group_no_contacts), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                contacts.forEach { c ->
                    val on = c.hash in picked
                    androidx.compose.foundation.layout.Row(
                        Modifier.fillMaxWidth().heightIn(min = 48.dp)
                            .toggleable(value = on, role = Role.Checkbox) { picked = if (on) picked - c.hash else picked + c.hash },
                        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(Space.m),
                    ) {
                        androidx.compose.material3.Checkbox(checked = on, onCheckedChange = null)
                        Text(c.displayName, style = MaterialTheme.typography.bodyLarge)
                    }
                }
                Button(
                    onClick = { onCreateGroup(groupName, picked) },
                    enabled = groupName.isNotBlank(),
                    modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp),
                ) { Text(stringResource(R.string.group_create)) }

                Text(stringResource(R.string.public_chats_title), style = MaterialTheme.typography.titleSmall)
                Text(
                    stringResource(R.string.group_public_warning),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                OutlinedButton(onClick = { onJoinGroup("global") }, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                    Text(stringResource(R.string.join_global))
                }
                OutlinedTextField(
                    value = region,
                    onValueChange = { region = it.take(40) },
                    label = { Text(stringResource(R.string.region_label)) },
                    supportingText = { Text(stringResource(R.string.region_helper)) },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                if (regions.isNotEmpty()) {
                    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                        regions.take(16).forEach { r ->
                            SuggestionChip(onClick = { onJoinGroup("#$r") }, label = { Text("#$r") })
                        }
                    }
                }
                OutlinedButton(
                    onClick = { onJoinGroup("#" + region.trim().removePrefix("#")) },
                    enabled = region.isNotBlank(),
                    modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp),
                ) { Text(stringResource(R.string.join_region)) }
            }

            // ---- my link ----
            Column(
                Modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer, RoundedCornerShape(16.dp)).padding(Space.l),
                verticalArrangement = Arrangement.spacedBy(Space.m),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(stringResource(R.string.my_link_title), style = MaterialTheme.typography.titleMedium, modifier = Modifier.fillMaxWidth())
                Text(
                    stringResource(R.string.my_link_body),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.fillMaxWidth(),
                )
                QrCode(myUri, stringResource(R.string.qr_description))
                Text(myUri, style = MeshTheme.mono, color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 2, overflow = TextOverflow.Ellipsis)
                Row(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                    OutlinedButton(onClick = onCopy, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) {
                        Icon(MeshIcons.Copy, contentDescription = null, modifier = Modifier.size(18.dp))
                        Text(stringResource(R.string.copy_link), Modifier.padding(start = Space.s))
                    }
                    OutlinedButton(onClick = onShare, modifier = Modifier.weight(1f).heightIn(min = 48.dp)) {
                        Icon(Icons.Filled.Share, contentDescription = null, modifier = Modifier.size(18.dp))
                        Text(stringResource(R.string.share_link), Modifier.padding(start = Space.s))
                    }
                }
            }
        }
    }
}

/**
 * A QR code is always dark modules on a white card with a quiet zone, in both
 * themes: scanners need the contrast.
 */
@Composable
fun QrCode(text: String, description: String, modifier: Modifier = Modifier) {
    val matrix: BitMatrix? = remember(text) {
        runCatching {
            QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, 0, 0, mapOf(EncodeHintType.MARGIN to 2, EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.M))
        }.getOrNull()
    }
    Canvas(modifier.size(220.dp).background(Color.White, RoundedCornerShape(12.dp)).semantics { contentDescription = description }) {
        val m = matrix ?: return@Canvas
        val cell = size.minDimension / m.width
        for (y in 0 until m.height) for (x in 0 until m.width) {
            if (m.get(x, y)) drawRect(Color.Black, topLeft = Offset(x * cell, y * cell), size = Size(cell + 0.5f, cell + 0.5f))
        }
    }
}
