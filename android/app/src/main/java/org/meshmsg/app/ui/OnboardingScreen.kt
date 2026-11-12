package org.meshmsg.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material3.Button
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.meshmsg.app.R
import org.meshmsg.app.ui.theme.Space

@Composable
fun OnboardingScreen(
    onStart: () -> Unit,
    modifier: Modifier = Modifier,
    onRestore: suspend (backup: String, passphrase: String) -> RestoreError? = { _, _ -> RestoreError.Other },
) {
    var restoring by rememberSaveable { mutableStateOf(false) }
    Surface(modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(Modifier.safeDrawingPadding().padding(Space.xl)) {
            Column(
                Modifier.weight(1f).verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(Space.l),
            ) {
                Spacer(Modifier.size(Space.xl))
                Icon(MeshIcons.Hub, contentDescription = null, tint = MaterialTheme.colorScheme.primary, modifier = Modifier.size(48.dp))
                Text(stringResource(R.string.onb_title), style = MaterialTheme.typography.headlineMedium)
                Text(stringResource(R.string.onb_body), style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant)
                Spacer(Modifier.size(Space.s))
                Point(Icons.Filled.Lock, stringResource(R.string.onb_point_identity))
                Point(MeshIcons.Hub, stringResource(R.string.onb_point_mesh))
                Point(MeshIcons.Bluetooth, stringResource(R.string.onb_point_perm))
            }
            Button(onClick = onStart, modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp)) {
                Text(stringResource(R.string.onb_start))
            }
            TextButton(onClick = { restoring = true }, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text(stringResource(R.string.onb_restore))
            }
        }
    }
    if (restoring) {
        RestoreDialog(restore = onRestore, onRestored = { restoring = false; onStart() }, onDismiss = { restoring = false })
    }
}

@Composable
private fun Point(icon: ImageVector, text: String) {
    Row(horizontalArrangement = Arrangement.spacedBy(Space.l), verticalAlignment = Alignment.Top) {
        Icon(icon, contentDescription = null, tint = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 2.dp).size(24.dp))
        Text(text, style = MaterialTheme.typography.bodyLarge)
    }
}
