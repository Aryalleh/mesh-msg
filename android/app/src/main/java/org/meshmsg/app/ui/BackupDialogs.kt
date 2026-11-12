package org.meshmsg.app.ui

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Intent
import android.os.PersistableBundle
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Share
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import org.meshmsg.app.R
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space

/** Shortest passphrase the core accepts (identity.MinPassphraseLength). */
const val MIN_PASSPHRASE = 8

/** Why a restore failed; shown in the dialog. */
enum class RestoreError { Passphrase, Format, Other }

private fun String.chars() = codePointCount(0, length)

@Composable
private fun PassField(value: String, onChange: (String) -> Unit, label: String, supporting: String?, isError: Boolean = false) {
    OutlinedTextField(
        value = value,
        onValueChange = onChange,
        label = { Text(label) },
        supportingText = if (supporting != null) { { Text(supporting) } } else null,
        isError = isError,
        singleLine = true,
        visualTransformation = PasswordVisualTransformation(),
        keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password, autoCorrectEnabled = false),
        modifier = Modifier.fillMaxWidth(),
    )
}

/**
 * Seals the account key with a passphrase, then shows the backup as a QR code
 * and text to copy or share. Secrets are kept in plain `remember`, never in
 * saved instance state.
 */
@Composable
fun BackupDialog(export: suspend (String) -> Result<String>, onDismiss: () -> Unit) {
    var pass by remember { mutableStateOf("") }
    var again by remember { mutableStateOf("") }
    var backup by remember { mutableStateOf<String?>(null) }
    var working by remember { mutableStateOf(false) }
    var failed by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current
    val done = backup
    val mismatch = again.isNotEmpty() && again != pass

    AlertDialog(
        onDismissRequest = { if (!working) onDismiss() },
        title = { Text(stringResource(if (done == null) R.string.backup_title else R.string.backup_done_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Space.m)) {
                if (done == null) {
                    Text(stringResource(R.string.backup_body))
                    PassField(pass, { pass = it; failed = false }, stringResource(R.string.backup_pass_label), stringResource(R.string.backup_pass_helper, MIN_PASSPHRASE))
                    PassField(
                        again, { again = it }, stringResource(R.string.backup_pass_again),
                        if (mismatch) stringResource(R.string.backup_pass_mismatch) else null, isError = mismatch,
                    )
                    if (failed) Text(stringResource(R.string.backup_failed), color = MaterialTheme.colorScheme.error)
                    if (working) LinearProgressIndicator(Modifier.fillMaxWidth())
                } else {
                    Text(stringResource(R.string.backup_done_body))
                    QrCode(done, stringResource(R.string.backup_qr_description))
                    SelectionContainer { Text(done, style = MeshTheme.mono) }
                    Row(horizontalArrangement = Arrangement.spacedBy(Space.s)) {
                        OutlinedButton(
                            onClick = {
                                val clip = ClipData.newPlainText("mesh-msg backup", done)
                                // Keeps the backup out of clipboard previews (Android 13+).
                                clip.description.extras = PersistableBundle().apply { putBoolean("android.content.extra.IS_SENSITIVE", true) }
                                context.getSystemService(ClipboardManager::class.java).setPrimaryClip(clip)
                            },
                            modifier = Modifier.weight(1f).heightIn(min = 48.dp),
                        ) {
                            Icon(MeshIcons.Copy, contentDescription = null, modifier = Modifier.size(18.dp))
                            Text(stringResource(R.string.backup_copy), Modifier.padding(start = Space.s))
                        }
                        OutlinedButton(
                            onClick = {
                                val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_TEXT, done)
                                context.startActivity(Intent.createChooser(send, null))
                            },
                            modifier = Modifier.weight(1f).heightIn(min = 48.dp),
                        ) {
                            Icon(Icons.Filled.Share, contentDescription = null, modifier = Modifier.size(18.dp))
                            Text(stringResource(R.string.share_link), Modifier.padding(start = Space.s))
                        }
                    }
                }
            }
        },
        confirmButton = {
            if (done == null) {
                TextButton(
                    onClick = {
                        working = true
                        scope.launch {
                            val r = export(pass)
                            working = false
                            r.onSuccess { backup = it; pass = ""; again = "" }.onFailure { failed = true }
                        }
                    },
                    enabled = !working && pass.chars() >= MIN_PASSPHRASE && pass == again,
                ) { Text(stringResource(R.string.backup_create)) }
            } else {
                TextButton(onClick = onDismiss) { Text(stringResource(R.string.backup_close)) }
            }
        },
        dismissButton = {
            if (done == null) TextButton(onClick = onDismiss, enabled = !working) { Text(stringResource(R.string.cancel)) }
        },
    )
}

/** Restores an account from a backup made on another phone or computer. */
@Composable
fun RestoreDialog(restore: suspend (backup: String, passphrase: String) -> RestoreError?, onRestored: () -> Unit, onDismiss: () -> Unit) {
    var text by remember { mutableStateOf("") }
    var pass by remember { mutableStateOf("") }
    var error by remember { mutableStateOf<RestoreError?>(null) }
    var working by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val context = LocalContext.current

    AlertDialog(
        onDismissRequest = { if (!working) onDismiss() },
        title = { Text(stringResource(R.string.restore_title)) },
        text = {
            Column(Modifier.verticalScroll(rememberScrollState()), verticalArrangement = Arrangement.spacedBy(Space.m)) {
                Text(stringResource(R.string.restore_body))
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it; error = null },
                    label = { Text(stringResource(R.string.restore_backup_label)) },
                    isError = error == RestoreError.Format,
                    supportingText = { if (error == RestoreError.Format) Text(stringResource(R.string.restore_err_format)) },
                    trailingIcon = {
                        TextButton(onClick = {
                            val clip = context.getSystemService(ClipboardManager::class.java).primaryClip
                            text = clip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(context)?.toString().orEmpty().trim()
                            error = null
                        }) { Text(stringResource(R.string.paste)) }
                    },
                    maxLines = 4,
                    textStyle = MeshTheme.mono,
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Ascii, autoCorrectEnabled = false),
                    modifier = Modifier.fillMaxWidth(),
                )
                PassField(
                    pass, { pass = it; error = null }, stringResource(R.string.backup_pass_label),
                    when (error) {
                        RestoreError.Passphrase -> stringResource(R.string.restore_err_passphrase)
                        RestoreError.Other -> stringResource(R.string.restore_err_other)
                        else -> null
                    },
                    isError = error == RestoreError.Passphrase || error == RestoreError.Other,
                )
                if (working) LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    working = true
                    scope.launch {
                        val e = restore(text, pass)
                        working = false
                        if (e == null) onRestored() else error = e
                    }
                },
                enabled = !working && text.isNotBlank() && pass.isNotEmpty(),
            ) { Text(stringResource(R.string.restore_confirm)) }
        },
        dismissButton = { TextButton(onClick = onDismiss, enabled = !working) { Text(stringResource(R.string.cancel)) } },
    )
}
