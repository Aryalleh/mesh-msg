package org.meshmsg.app.ui

import android.graphics.SurfaceTexture
import android.net.ConnectivityManager
import android.view.Surface
import android.view.TextureView
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.requiredSize
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.runtime.collectAsState
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.flow.MutableStateFlow
import org.meshmsg.app.data.CallVideo
import org.meshmsg.app.data.RemoteVideo
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
    /** Turns the camera on or off (asks for the permission first if needed). */
    val camera: (Boolean) -> Unit = {},
    val switchCamera: () -> Unit = {},
)

/**
 * The call, over everything else: who, what is happening, the code to
 * compare, and the buttons for this state.
 */
@Composable
fun CallScreen(call: CallInfo, contact: Contact?, actions: CallActions, modifier: Modifier = Modifier, video: CallVideo? = null) {
    val remote by (video?.remote ?: remember { MutableStateFlow(RemoteVideo()) }).collectAsState()
    val cameraOn by (video?.cameraOn ?: remember { MutableStateFlow(false) }).collectAsState()
    val showRemote = video != null && call.state == "active" && remote.active
    Box(modifier.fillMaxSize().background(Color.Black)) {
        if (video != null && call.state == "active") {
            // Kept in the layout (invisible until frames come) so its Surface is ready.
            RemoteVideoView(video, remote, Modifier.fillMaxSize().alpha(if (showRemote) 1f else 0f))
        }
        CallControls(call, contact, actions, video, remote, cameraOn, showRemote)
        if (video != null && cameraOn && call.state == "active") {
            SelfView(video, Modifier.align(Alignment.TopEnd).systemBarsPadding().padding(Space.l).size(width = 108.dp, height = 144.dp))
        }
    }
}

@Composable
private fun CallControls(call: CallInfo, contact: Contact?, actions: CallActions, video: CallVideo?, remote: RemoteVideo, cameraOn: Boolean, showRemote: Boolean) {
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
    // Over video: no background, light text.
    Surface(Modifier.fillMaxSize(), color = if (showRemote) Color.Transparent else MaterialTheme.colorScheme.surface, contentColor = if (showRemote) Color.White else MaterialTheme.colorScheme.onSurface) {
        Column(
            Modifier.fillMaxSize().systemBarsPadding().padding(Space.l),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Spacer(Modifier.height(if (showRemote) 8.dp else 48.dp))
            if (contact != null && !showRemote) Avatar(contact, size = 96.dp)
            Spacer(Modifier.height(Space.l))
            Text(name, style = MaterialTheme.typography.headlineMedium, textAlign = TextAlign.Center)
            Text(
                if (call.state == "ringing" && call.videoCall) stringResource(R.string.call_incoming_video) else status,
                style = MaterialTheme.typography.titleMedium,
                color = if (showRemote) Color.White else MaterialTheme.colorScheme.onSurfaceVariant,
            )
            if (call.state == "active" && call.code.isNotEmpty() && !showRemote) {
                Spacer(Modifier.height(Space.l))
                Text(stringResource(R.string.call_code_hint), style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
                Text(call.code, style = MaterialTheme.typography.displaySmall)
            }
            // How the audio goes: peer to peer or through a relay (owner asked to always show it).
            if (!call.ended && (call.pathClass > 0 || call.relayed)) {
                Spacer(Modifier.height(Space.m))
                val relayed = call.relayed || call.pathClass >= 6
                Surface(
                    shape = MaterialTheme.shapes.small,
                    color = if (relayed) MaterialTheme.colorScheme.tertiaryContainer else MaterialTheme.colorScheme.secondaryContainer,
                ) {
                    Text(
                        stringResource(
                            when {
                                relayed -> R.string.call_path_relay
                                call.pathClass == 1 -> R.string.call_path_local
                                else -> R.string.call_path_p2p
                            },
                        ),
                        style = MaterialTheme.typography.labelLarge,
                        modifier = Modifier.padding(horizontal = Space.m, vertical = Space.xs),
                    )
                }
                if (relayed) {
                    Text(stringResource(R.string.call_relayed), style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
                }
            }
            if (video != null && call.state == "active" && (cameraOn || remote.active) && metered()) {
                Spacer(Modifier.height(Space.s))
                Text(stringResource(R.string.call_video_data), style = MaterialTheme.typography.bodySmall, textAlign = TextAlign.Center)
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
                    RoundButton(if (call.videoCall) MeshIcons.Videocam else Icons.Filled.Call, stringResource(R.string.call_answer), Color(0xFF2E7D32), actions.answer)
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
                            if (call.video) {
                                FilledTonalIconToggleButton(checked = cameraOn, onCheckedChange = { actions.camera(it) }, modifier = Modifier.size(64.dp)) {
                                    Icon(if (cameraOn) MeshIcons.Videocam else MeshIcons.VideocamOff, stringResource(R.string.call_camera))
                                }
                            }
                            if (call.video && cameraOn) {
                                FilledTonalIconButton(onClick = actions.switchCamera, modifier = Modifier.size(64.dp)) {
                                    Icon(MeshIcons.SwitchCamera, stringResource(R.string.call_switch_camera))
                                }
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

/** The other side's video, rotated as sent and fitted into the screen. */
@Composable
private fun RemoteVideoView(video: CallVideo, remote: RemoteVideo, modifier: Modifier) {
    BoxWithConstraints(modifier, contentAlignment = Alignment.Center) {
        val turned = remote.rotation % 180 != 0
        val vw = (if (turned) remote.height else remote.width).coerceAtLeast(1)
        val vh = (if (turned) remote.width else remote.height).coerceAtLeast(1)
        // Fill the screen (crop the edges), like video calling apps.
        val scale = maxOf(maxWidth.value / vw, maxHeight.value / vh)
        val dw = (vw * scale).dp
        val dh = (vh * scale).dp
        AndroidView(
            factory = { ctx ->
                TextureView(ctx).apply {
                    surfaceTextureListener = object : TextureView.SurfaceTextureListener {
                        override fun onSurfaceTextureAvailable(st: SurfaceTexture, w: Int, h: Int) { video.setRemoteSurface(Surface(st)) }
                        override fun onSurfaceTextureSizeChanged(st: SurfaceTexture, w: Int, h: Int) {}
                        override fun onSurfaceTextureDestroyed(st: SurfaceTexture): Boolean { video.setRemoteSurface(null); return true }
                        override fun onSurfaceTextureUpdated(st: SurfaceTexture) {}
                    }
                }
            },
            modifier = Modifier.requiredSize(if (turned) dh else dw, if (turned) dw else dh).graphicsLayer { rotationZ = remote.rotation.toFloat() },
        )
    }
}

/** This phone's camera, small in the corner. */
@Composable
private fun SelfView(video: CallVideo, modifier: Modifier) {
    Box(modifier.clip(MaterialTheme.shapes.medium).background(Color.DarkGray)) {
        AndroidView(
            factory = { ctx -> PreviewView(ctx).apply { scaleType = PreviewView.ScaleType.FILL_CENTER }.also { video.setPreview(it) } },
            onRelease = { video.setPreview(null) },
            modifier = Modifier.fillMaxSize(),
        )
    }
}

@Composable
private fun metered(): Boolean {
    val ctx = LocalContext.current
    return remember { runCatching { ctx.getSystemService(ConnectivityManager::class.java).isActiveNetworkMetered }.getOrDefault(false) }
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
