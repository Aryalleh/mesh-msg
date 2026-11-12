package org.meshmsg.app.ui

import android.content.ActivityNotFoundException
import android.content.Context
import android.graphics.Bitmap
import android.media.MediaPlayer
import android.net.Uri
import android.provider.MediaStore
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.meshmsg.app.R
import org.meshmsg.app.data.Media
import org.meshmsg.app.data.MediaFiles
import org.meshmsg.app.data.MediaKind
import org.meshmsg.app.data.MediaState
import org.meshmsg.app.ui.theme.Space
import java.io.File

/** What the chat needs to show and send files; no-ops in previews and tests. */
class MediaActions(
    /** important: also through relays (reaches someone offline, a few per day). */
    val pick: (Uri, caption: String, important: Boolean) -> Unit = { _, _, _ -> },
    val sendFile: (File, mime: String, name: String, caption: String, important: Boolean) -> Unit = { _, _, _, _, _ -> },
    val fetch: (String) -> Unit = {},
    val path: suspend (String) -> String? = { null },
    val error: (String) -> Unit = {},
)

/** The camera app records a short, small video. */
class RecordVideo : ActivityResultContracts.CaptureVideo() {
    override fun createIntent(context: Context, input: Uri) = super.createIntent(context, input)
        .putExtra(MediaStore.EXTRA_DURATION_LIMIT, 60)
        .putExtra(MediaStore.EXTRA_VIDEO_QUALITY, 0)
        .putExtra(MediaStore.EXTRA_SIZE_LIMIT, 16L shl 20)
}

fun humanSize(n: Long): String = when {
    n >= 1 shl 20 -> "%.1f MB".format(n / 1048576.0)
    n >= 1 shl 10 -> "${n shr 10} KB"
    else -> "$n B"
}

@Composable
fun mediaLabel(m: Media): String = stringResource(
    when (m.kind) {
        MediaKind.Image -> R.string.media_photo
        MediaKind.Audio -> R.string.media_voice
        MediaKind.Video -> R.string.media_video
        MediaKind.Round -> R.string.media_round
        MediaKind.File -> R.string.media_file
    },
)

private fun open(ctx: Context, path: String, mime: String, onError: (String) -> Unit) {
    try {
        ctx.startActivity(MediaFiles.viewIntent(ctx, path, mime))
    } catch (_: ActivityNotFoundException) {
        onError(ctx.getString(R.string.media_no_app))
    }
}

/** A message's file inside its bubble. */
@Composable
fun MediaContent(m: Media, outgoing: Boolean, fg: Color, actions: MediaActions) {
    val ctx = LocalContext.current
    val path by produceState<String?>(null, m.id, m.state) {
        value = if (m.state == MediaState.Ready) actions.path(m.id) else null
    }
    Column(Modifier.padding(bottom = Space.xs)) {
        val p = path
        when {
            p != null && m.kind == MediaKind.Image -> {
                val bmp by produceState<Bitmap?>(null, p) { value = withContext(Dispatchers.IO) { MediaFiles.thumbnail(p) } }
                bmp?.let { b ->
                    Image(
                        b.asImageBitmap(), contentDescription = m.name,
                        contentScale = ContentScale.Fit,
                        modifier = Modifier.widthIn(max = 280.dp).heightIn(max = 320.dp).clip(RoundedCornerShape(12.dp))
                            .clickable { open(ctx, p, m.mime, actions.error) },
                    )
                }
            }
            p != null && m.kind == MediaKind.Audio -> VoicePlayer(p, fg)
            p != null && m.kind == MediaKind.Round -> RoundVideoPlayer(p)
            else -> FileRow(m, fg, p, actions)
        }
        if (outgoing && m.relays > 0 && m.uploaded < m.relays) {
            Text(stringResource(R.string.media_uploading), style = MaterialTheme.typography.labelSmall, color = fg.copy(alpha = 0.72f))
        }
    }
}

@Composable
private fun FileRow(m: Media, fg: Color, path: String?, actions: MediaActions) {
    val ctx = LocalContext.current
    Column(Modifier.widthIn(min = 200.dp)) {
        Row(
            Modifier.fillMaxWidth().heightIn(min = 48.dp)
                .clickable(enabled = path != null) { path?.let { open(ctx, it, m.mime, actions.error) } },
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(Space.s),
        ) {
            Icon(
                when (m.kind) {
                    MediaKind.Video -> MeshIcons.Videocam
                    MediaKind.Round -> MeshIcons.RoundVideo
                    MediaKind.Audio -> MeshIcons.Mic
                    else -> MeshIcons.FileIcon
                },
                contentDescription = null, tint = fg,
            )
            Column(Modifier.weight(1f)) {
                Text(m.name.ifBlank { mediaLabel(m) }, color = fg, maxLines = 1, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                Text(
                    "${mediaLabel(m)} · ${humanSize(m.size)}" + when (m.state) {
                        MediaState.Missing -> " · " + stringResource(R.string.media_missing)
                        else -> ""
                    },
                    color = fg.copy(alpha = 0.72f), style = MaterialTheme.typography.labelSmall,
                )
            }
            if ((m.kind == MediaKind.Video || m.kind == MediaKind.Round) && path != null) Icon(Icons.Filled.PlayArrow, stringResource(R.string.media_play), tint = fg)
        }
        if (m.state == MediaState.Remote && !m.important) {
            Text(stringResource(R.string.media_waits_direct), color = fg.copy(alpha = 0.72f), style = MaterialTheme.typography.labelSmall)
        }
        when (m.state) {
            MediaState.Remote -> TextButton(onClick = { actions.fetch(m.id) }) {
                Icon(MeshIcons.Download, null, Modifier.size(18.dp))
                Text(" " + stringResource(R.string.media_download, humanSize(m.size)))
            }
            MediaState.Downloading -> {
                if (m.total > 0) LinearProgressIndicator(progress = { m.progress }, modifier = Modifier.fillMaxWidth().padding(vertical = Space.xs))
                else LinearProgressIndicator(Modifier.fillMaxWidth().padding(vertical = Space.xs))
            }
            else -> {}
        }
    }
}

/** Plays a voice message in place. */
@Composable
private fun VoicePlayer(path: String, fg: Color) {
    var player by remember(path) { mutableStateOf<MediaPlayer?>(null) }
    var playing by remember(path) { mutableStateOf(false) }
    var pos by remember(path) { mutableFloatStateOf(0f) }
    val scope = rememberCoroutineScope()
    DisposableEffect(path) { onDispose { player?.release(); player = null } }
    LaunchedEffect(playing) {
        while (playing) {
            player?.let { if (it.duration > 0) pos = it.currentPosition.toFloat() / it.duration }
            delay(200)
        }
    }
    Row(Modifier.widthIn(min = 200.dp), verticalAlignment = Alignment.CenterVertically) {
        IconButton(onClick = {
            val p = player ?: runCatching {
                MediaPlayer().apply {
                    setDataSource(path)
                    prepare()
                    setOnCompletionListener { playing = false; pos = 0f; seekTo(0) }
                }
            }.getOrNull().also { player = it } ?: return@IconButton
            if (playing) { p.pause(); playing = false } else { scope.launch { p.start(); playing = true } }
        }) {
            Icon(
                if (playing) MeshIcons.Pause else Icons.Filled.PlayArrow,
                stringResource(if (playing) R.string.media_pause else R.string.media_play), tint = fg,
            )
        }
        Box(Modifier.weight(1f)) {
            LinearProgressIndicator(progress = { pos }, modifier = Modifier.fillMaxWidth(), color = fg, trackColor = fg.copy(alpha = 0.25f))
        }
    }
}
