package org.meshmsg.app.ui

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Matrix
import android.graphics.SurfaceTexture
import android.media.MediaMetadataRetriever
import android.media.MediaPlayer
import android.view.Surface
import android.view.TextureView
import androidx.camera.core.CameraSelector
import androidx.camera.core.MirrorMode
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.video.FallbackStrategy
import androidx.camera.video.FileOutputOptions
import androidx.camera.video.Quality
import androidx.camera.video.QualitySelector
import androidx.camera.video.Recorder
import androidx.camera.video.Recording
import androidx.camera.video.VideoCapture
import androidx.camera.video.VideoRecordEvent
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.PlayArrow
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.FilledIconButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.IconButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.produceState
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.compose.ui.window.Dialog
import androidx.compose.ui.window.DialogProperties
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import org.meshmsg.app.R
import org.meshmsg.app.data.MediaFiles
import java.io.File
import kotlin.coroutines.resume

/** Round video messages: the name marks them (core: media.RoundPrefix). */
object RoundVideo {
    const val MAX_MS = 60_000L
    const val MIME = "video/mp4"
    val permissions = arrayOf(Manifest.permission.CAMERA, Manifest.permission.RECORD_AUDIO)

    fun granted(ctx: Context) = permissions.all { ContextCompat.checkSelfPermission(ctx, it) == PackageManager.PERMISSION_GRANTED }
}

private suspend fun cameraProvider(ctx: Context): ProcessCameraProvider = suspendCancellableCoroutine { c ->
    val f = ProcessCameraProvider.getInstance(ctx)
    f.addListener({ runCatching { f.get() }.onSuccess { c.resume(it) } }, ContextCompat.getMainExecutor(ctx))
}

/**
 * Full-screen recorder: front camera in a circle, tap the red button to
 * record, tap send to stop and send. onDone gets the file, or null if cancelled.
 */
@Composable
fun RoundVideoRecorder(onDone: (File?) -> Unit, onError: (String) -> Unit) {
    val ctx = LocalContext.current
    val owner = LocalLifecycleOwner.current
    val preview = remember {
        PreviewView(ctx).apply {
            // A TextureView, so the circle clip applies to the picture.
            implementationMode = PreviewView.ImplementationMode.COMPATIBLE
            scaleType = PreviewView.ScaleType.FILL_CENTER
        }
    }
    var front by remember { mutableStateOf(true) }
    var capture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var recording by remember { mutableStateOf<Recording?>(null) }
    var started by remember { mutableLongStateOf(0L) }
    var elapsed by remember { mutableLongStateOf(0L) }
    var send by remember { mutableStateOf(false) }
    var provider by remember { mutableStateOf<ProcessCameraProvider?>(null) }
    val file = remember { MediaFiles.newOutgoing(ctx, "round", "mp4") }

    LaunchedEffect(front) {
        val p = runCatching { cameraProvider(ctx) }.getOrElse { onError(it.message ?: "camera"); onDone(null); return@LaunchedEffect }
        provider = p
        val pv = Preview.Builder().build().also { it.setSurfaceProvider(preview.surfaceProvider) }
        val recorder = Recorder.Builder()
            .setQualitySelector(QualitySelector.from(Quality.SD, FallbackStrategy.lowerQualityOrHigherThan(Quality.SD)))
            .setTargetVideoEncodingBitRate(450_000) // ~3.5 MB a minute with sound
            .build()
        val vc = VideoCapture.Builder(recorder).setMirrorMode(MirrorMode.MIRROR_MODE_ON_FRONT_ONLY).build()
        val hasFront = runCatching { p.hasCamera(CameraSelector.DEFAULT_FRONT_CAMERA) }.getOrDefault(false)
        val selector = if (front && hasFront) CameraSelector.DEFAULT_FRONT_CAMERA else CameraSelector.DEFAULT_BACK_CAMERA
        runCatching {
            p.unbindAll()
            p.bindToLifecycle(owner, selector, pv, vc)
            capture = vc
        }.onFailure { onError(it.message ?: "camera"); onDone(null) }
    }
    DisposableEffect(Unit) {
        onDispose {
            recording?.stop()
            provider?.unbindAll()
        }
    }
    LaunchedEffect(recording) {
        while (recording != null) {
            elapsed = System.currentTimeMillis() - started
            delay(100)
        }
    }

    @SuppressLint("MissingPermission") // checked in RoundVideo.granted before this screen opens
    fun start() {
        val vc = capture ?: return
        val opts = FileOutputOptions.Builder(file).setDurationLimitMillis(RoundVideo.MAX_MS).build()
        var pending = vc.output.prepareRecording(ctx, opts)
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) {
            pending = pending.withAudioEnabled()
        }
        started = System.currentTimeMillis()
        elapsed = 0
        recording = pending.start(ContextCompat.getMainExecutor(ctx)) { ev ->
            if (ev is VideoRecordEvent.Finalize) {
                recording = null
                val ok = !ev.hasError() || ev.error == VideoRecordEvent.Finalize.ERROR_DURATION_LIMIT_REACHED
                when {
                    // The limit stops it too: send what was recorded.
                    ok && (send || ev.error == VideoRecordEvent.Finalize.ERROR_DURATION_LIMIT_REACHED) && file.length() > 0 -> onDone(file)
                    else -> { file.delete(); if (!ok) onError(ev.cause?.message ?: "recording failed"); onDone(null) }
                }
            }
        }
    }

    Dialog(onDismissRequest = { recording?.stop() ?: onDone(null) }, properties = DialogProperties(usePlatformDefaultWidth = false)) {
        Box(Modifier.fillMaxSize().background(Color.Black.copy(alpha = 0.88f)), contentAlignment = Alignment.Center) {
            Column(horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(24.dp)) {
                Box(contentAlignment = Alignment.Center) {
                    AndroidView({ preview }, Modifier.size(280.dp).clip(CircleShape))
                    if (recording != null) {
                        CircularProgressIndicator(
                            progress = { (elapsed.toFloat() / RoundVideo.MAX_MS).coerceIn(0f, 1f) },
                            modifier = Modifier.size(296.dp), strokeWidth = 4.dp, color = MaterialTheme.colorScheme.error,
                            trackColor = Color.White.copy(alpha = 0.2f),
                        )
                    }
                }
                Text(
                    if (recording != null) "● %d:%02d".format(elapsed / 60000, elapsed / 1000 % 60) else stringResource(R.string.round_hint),
                    color = if (recording != null) MaterialTheme.colorScheme.error else Color.White,
                )
                Row(horizontalArrangement = Arrangement.spacedBy(28.dp), verticalAlignment = Alignment.CenterVertically) {
                    IconButton(onClick = {
                        send = false
                        recording?.stop() ?: run { file.delete(); onDone(null) }
                    }) { Icon(Icons.Filled.Close, stringResource(R.string.voice_cancel), tint = Color.White) }
                    FilledIconButton(
                        onClick = { if (recording == null) start() else { send = true; recording?.stop() } },
                        enabled = capture != null,
                        modifier = Modifier.size(72.dp),
                        colors = IconButtonDefaults.filledIconButtonColors(containerColor = MaterialTheme.colorScheme.error),
                    ) {
                        if (recording == null) Icon(MeshIcons.Videocam, stringResource(R.string.round_record), Modifier.size(32.dp))
                        else Icon(Icons.AutoMirrored.Filled.Send, stringResource(R.string.round_send), Modifier.size(32.dp))
                    }
                    IconButton(onClick = { front = !front }, enabled = recording == null) {
                        Icon(MeshIcons.SwitchCamera, stringResource(R.string.round_switch), tint = Color.White)
                    }
                }
            }
        }
    }
}

/** A round video message in the chat: a circle that plays in place when tapped. */
@Composable
fun RoundVideoPlayer(path: String) {
    var playing by remember(path) { mutableStateOf(false) }
    var pos by remember(path) { mutableFloatStateOf(0f) }
    var player by remember(path) { mutableStateOf<MediaPlayer?>(null) }
    val thumb by produceState<Bitmap?>(null, path) {
        value = withContext(Dispatchers.IO) {
            runCatching {
                MediaMetadataRetriever().run {
                    try { setDataSource(path); getFrameAtTime(0) } finally { release() }
                }
            }.getOrNull()
        }
    }
    DisposableEffect(path) { onDispose { player?.release(); player = null } }
    LaunchedEffect(playing) {
        while (playing) {
            player?.let { if (it.duration > 0) pos = it.currentPosition.toFloat() / it.duration }
            delay(100)
        }
    }
    val size = 220.dp
    Box(
        Modifier.size(size + 8.dp).clickable {
            val p = player
            when {
                p == null -> playing = true
                p.isPlaying -> { p.pause(); playing = false }
                else -> { p.start(); playing = true }
            }
        },
        contentAlignment = Alignment.Center,
    ) {
        if (player != null || playing) {
            AndroidView(
                factory = { c ->
                    TextureView(c).apply {
                        surfaceTextureListener = object : TextureView.SurfaceTextureListener {
                            override fun onSurfaceTextureAvailable(st: SurfaceTexture, w: Int, h: Int) {
                                val mp = MediaPlayer()
                                runCatching {
                                    mp.setDataSource(path)
                                    mp.setSurface(Surface(st))
                                    mp.setOnVideoSizeChangedListener { _, vw, vh -> cropToFill(this@apply, vw, vh) }
                                    mp.setOnCompletionListener { playing = false; pos = 0f; it.seekTo(0) }
                                    mp.prepare()
                                    mp.start()
                                    player = mp
                                }.onFailure { mp.release(); playing = false }
                            }
                            override fun onSurfaceTextureSizeChanged(st: SurfaceTexture, w: Int, h: Int) {}
                            override fun onSurfaceTextureDestroyed(st: SurfaceTexture): Boolean {
                                player?.release(); player = null; playing = false
                                return true
                            }
                            override fun onSurfaceTextureUpdated(st: SurfaceTexture) {}
                        }
                    }
                },
                modifier = Modifier.size(size).clip(CircleShape),
            )
        } else {
            thumb?.let {
                Image(it.asImageBitmap(), null, Modifier.size(size).clip(CircleShape), contentScale = ContentScale.Crop)
            } ?: Box(Modifier.size(size).clip(CircleShape).background(Color.Black))
        }
        if (!playing) {
            Box(Modifier.size(48.dp).background(Color.Black.copy(alpha = 0.45f), CircleShape), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.PlayArrow, stringResource(R.string.media_play), tint = Color.White)
            }
        }
        CircularProgressIndicator(
            progress = { pos }, modifier = Modifier.size(size + 8.dp).padding(1.dp), strokeWidth = 3.dp,
            trackColor = Color.Transparent,
        )
    }
}

/** Scales the video so it fills the square view (center crop) instead of stretching. */
private fun cropToFill(v: TextureView, vw: Int, vh: Int) {
    if (vw <= 0 || vh <= 0) return
    val sx = if (vw > vh) vw.toFloat() / vh else 1f
    val sy = if (vh > vw) vh.toFloat() / vw else 1f
    v.setTransform(Matrix().apply { setScale(sx, sy, v.width / 2f, v.height / 2f) })
}
