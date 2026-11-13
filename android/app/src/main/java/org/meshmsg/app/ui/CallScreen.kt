package org.meshmsg.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.systemBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Call
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.FilledTonalIconToggleButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.delay
import org.meshmsg.app.R
import org.meshmsg.app.data.CallInfo
import org.meshmsg.app.data.Contact
import org.meshmsg.app.ui.theme.Space

/** What the call screen can do. */
class CallActions(
    val answer: () -> Unit = {},
    val end: () -> Unit = {},
    val mute: (Boolean) -> Unit = {},
    val speaker: (Boolean) -> Unit = {},
)

/**
 * The call, over everything else: who, what is happening, the code to
 * compare, and the buttons for this state.
 */
@Composable
fun CallScreen(call: CallInfo, contact: Contact?, actions: CallActions, modifier: Modifier = Modifier) {
    var muted by remember(call.id) { mutableStateOf(false) }
    var speaker by remember(call.id) { mutableStateOf(false) }
    var now by remember { mutableLongStateOf(System.currentTimeMillis()) }
    LaunchedEffect(call.state) {
        while (call.state == "active") {
            now = System.currentTimeMillis()
            delay(1_000)
        }
    }
    val name = contact?.displayName ?: call.peer.take(8)
    val status = when (call.state) {
        "outgoing" -> stringResource(R.string.call_calling)
        "ringing" -> stringResource(R.string.call_incoming)
        "connecting" -> stringResource(R.string.call_connecting)
        "active" -> duration(((now - call.started) / 1000).coerceAtLeast(0))
        else -> endReason(call)
    }
    Surface(modifier.fillMaxSize(), color = MaterialTheme.colorScheme.surface) {
        Column(
            Modifier.fillMaxSize().systemBarsPadding().padding(Space.l),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(Modifier.height(48.dp))
            if (contact != null) Avatar(contact, size = 96.dp)
            Spacer(Modifier.height(Space.l))
            Text(name, style = MaterialTheme.typography.headlineMedium, textAlign = TextAlign.Center)
            Text(status, style = MaterialTheme.typography.titleMedium, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (call.state == "active" && call.code.isNotEmpty()) {
                Spacer(Modifier.height(Space.l))
                Text(stringResource(R.string.call_code_hint), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
                Text(call.code, style = MaterialTheme.typography.displaySmall)
            }
            if (call.relayed) {
                Spacer(Modifier.height(Space.m))
                Text(stringResource(R.string.call_relayed), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
            }
            if (call.peerMuted) {
                Spacer(Modifier.height(Space.m))
                Text(stringResource(R.string.call_peer_muted, name), style = MaterialTheme.typography.bodyMedium)
            }
            Spacer(Modifier.weight(1f))
            when {
                call.ended -> {}
                call.incoming && call.state == "ringing" -> Row(Modifier.padding(bottom = 48.dp), horizontalArrangement = Arrangement.spacedBy(96.dp)) {
                    RoundButton(MeshIcons.CallEnd, stringResource(R.string.call_decline), MaterialTheme.colorScheme.error, actions.end)
                    RoundButton(Icons.Filled.Call, stringResource(R.string.call_answer), Color(0xFF2E7D32), actions.answer)
                }
                else -> Column(horizontalAlignment = Alignment.CenterHorizontally) {
                    if (call.state == "active") {
                        Row(horizontalArrangement = Arrangement.spacedBy(Space.xl)) {
                            FilledTonalIconToggleButton(checked = muted, onCheckedChange = { muted = it; actions.mute(it) }, modifier = Modifier.size(64.dp)) {
                                Icon(if (muted) MeshIcons.MicOff else MeshIcons.Mic, stringResource(R.string.call_mute))
                            }
                            FilledTonalIconToggleButton(checked = speaker, onCheckedChange = { speaker = it; actions.speaker(it) }, modifier = Modifier.size(64.dp)) {
                                Icon(MeshIcons.VolumeUp, stringResource(R.string.call_speaker))
                            }
                        }
                        Spacer(Modifier.height(Space.xl))
                    }
                    Box(Modifier.padding(bottom = 48.dp)) {
                        RoundButton(MeshIcons.CallEnd, stringResource(R.string.call_hang_up), MaterialTheme.colorScheme.error, actions.end)
                    }
                }
            }
        }
    }
}

@Composable
private fun RoundButton(icon: androidx.compose.ui.graphics.vector.ImageVector, label: String, color: Color, onClick: () -> Unit) {
    FilledIconButton(
        onClick = onClick,
        modifier = Modifier.size(72.dp).semantics { contentDescription = label },
        shape = CircleShape,
        colors = IconButtonDefaults.filledIconButtonColors(containerColor = color, contentColor = Color.White),
    ) { Icon(icon, null, Modifier.size(32.dp)) }
}

private fun duration(s: Long): String = "%d:%02d".format(s / 60, s % 60)

@Composable
private fun endReason(c: CallInfo): String = when (c.reason) {
    "declined" -> stringResource(R.string.call_end_declined)
    "busy" -> stringResource(R.string.call_end_busy)
    "no direct path" -> stringResource(R.string.call_end_no_path)
    "missed", "no answer" -> stringResource(R.string.call_end_no_answer)
    "connection lost" -> stringResource(R.string.call_end_lost)
    else -> stringResource(R.string.call_ended)
}
