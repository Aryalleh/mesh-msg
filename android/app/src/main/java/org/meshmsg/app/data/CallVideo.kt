package org.meshmsg.app.data

import android.content.Context
import android.media.Image
import android.media.MediaCodec
import android.media.MediaCodecInfo
import android.media.MediaCodecList
import android.media.MediaFormat
import android.os.Bundle
import android.util.Log
import android.util.Size
import android.view.Surface
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.ImageProxy
import androidx.camera.core.Preview
import androidx.camera.core.resolutionselector.ResolutionSelector
import androidx.camera.core.resolutionselector.ResolutionStrategy
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.core.content.ContextCompat
import androidx.lifecycle.LifecycleOwner
import java.nio.ByteBuffer
import java.util.concurrent.Executors
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import org.meshmsg.meshcore.Core

/** What the call screen shows of the other side's video. */
data class RemoteVideo(val active: Boolean = false, val width: Int = 0, val height: Int = 0, val rotation: Int = 0)

/**
 * A call's video (docs/calls.md §7a), owned by the call screen: the camera
 * runs only while the app is in front.
 *
 * Send: CameraX frames (YUV, ~640×480) → H.264 hardware encoder → core, as
 * orientation(1) | Annex-B access unit, SPS/PPS in front of every keyframe.
 * The core asks for keyframes and sets the bitrate (CallVideoNeeds).
 * Receive: core → H.264 decoder → the remote view's Surface.
 */
class CallVideo(private val context: Context, private val core: Core, private val id: String) {
    private val _remote = MutableStateFlow(RemoteVideo())
    val remote: StateFlow<RemoteVideo> = _remote
    private val _camera = MutableStateFlow(false)
    val cameraOn: StateFlow<Boolean> = _camera
    private val _front = MutableStateFlow(true)
    val frontCamera: StateFlow<Boolean> = _front

    private val camExec = Executors.newSingleThreadExecutor()
    private var provider: ProcessCameraProvider? = null
    private var owner: LifecycleOwner? = null
    private var preview: PreviewView? = null

    // encoder state: camera thread only
    private var enc: MediaCodec? = null
    private var encSize: Size? = null
    private var csd: ByteArray? = null
    private var kbps = 0
    private var lastFrameNs = 0L

    @Volatile private var running = true
    @Volatile private var surface: Surface? = null
    private val recv = Thread(::receiveLoop, "call-video-rx").apply { isDaemon = true; start() }

    /** Shows the camera's own preview here (null: none). */
    fun setPreview(view: PreviewView?) {
        preview = view
        if (_camera.value) bind()
    }

    /** The Surface the other side's video is drawn on (null when the view is gone). */
    fun setRemoteSurface(s: Surface?) {
        surface = s
    }

    fun setCamera(on: Boolean, owner: LifecycleOwner) {
        this.owner = owner
        _camera.value = on
        if (on) bind() else unbind()
    }

    fun switchCamera() {
        _front.value = !_front.value
        if (_camera.value) bind()
    }

    fun release() {
        running = false
        unbind()
        camExec.execute { releaseEncoder() }
        camExec.shutdown()
        recv.interrupt()
    }

    // ---- camera + encoder ----

    private fun bind() {
        val o = owner ?: return
        val f = ProcessCameraProvider.getInstance(context)
        f.addListener({
            runCatching {
                val p = f.get()
                provider = p
                p.unbindAll()
                val sel = ResolutionSelector.Builder()
                    .setResolutionStrategy(ResolutionStrategy(Size(640, 480), ResolutionStrategy.FALLBACK_RULE_CLOSEST_HIGHER_THEN_LOWER))
                    .build()
                val analysis = ImageAnalysis.Builder()
                    .setResolutionSelector(sel)
                    .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                    .setOutputImageFormat(ImageAnalysis.OUTPUT_IMAGE_FORMAT_YUV_420_888)
                    .build()
                analysis.setAnalyzer(camExec, ::onFrame)
                val cam = if (_front.value) CameraSelector.DEFAULT_FRONT_CAMERA else CameraSelector.DEFAULT_BACK_CAMERA
                val pv = preview
                if (pv != null) {
                    val prev = Preview.Builder().build().also { it.setSurfaceProvider(pv.surfaceProvider) }
                    p.bindToLifecycle(o, cam, prev, analysis)
                } else {
                    p.bindToLifecycle(o, cam, analysis)
                }
            }.onFailure { Log.w(TAG, "camera: $it") }
        }, ContextCompat.getMainExecutor(context))
    }

    private fun unbind() {
        ContextCompat.getMainExecutor(context).execute { runCatching { provider?.unbindAll() } }
    }

    private fun onFrame(img: ImageProxy) {
        img.use {
            if (!running || !_camera.value) return
            val now = System.nanoTime()
            if (now - lastFrameNs < FRAME_NS) return
            lastFrameNs = now
            val size = Size(img.width, img.height)
            if (enc == null || encSize != size) startEncoder(size)
            val e = enc ?: return
            val needs = runCatching { core.callVideoNeeds(id).toInt() }.getOrDefault(0)
            if (needs and 1 == 1) e.setParameters(Bundle().apply { putInt(MediaCodec.PARAMETER_KEY_REQUEST_SYNC_FRAME, 0) })
            val want = needs shr 1
            if (want > 0 && (kbps == 0 || kotlin.math.abs(want - kbps) * 10 > kbps)) {
                kbps = want
                e.setParameters(Bundle().apply { putInt(MediaCodec.PARAMETER_KEY_VIDEO_BITRATE, want * 1000) })
            }
            runCatching {
                val i = e.dequeueInputBuffer(10_000)
                if (i >= 0) {
                    val dst = e.getInputImage(i)
                    if (dst != null) {
                        copyYuv(img, dst)
                        e.queueInputBuffer(i, 0, img.width * img.height * 3 / 2, now / 1000, 0)
                    } else {
                        e.queueInputBuffer(i, 0, 0, now / 1000, 0)
                    }
                }
                drain(e, img.imageInfo.rotationDegrees)
            }.onFailure { Log.w(TAG, "encode: $it"); releaseEncoder() }
        }
    }

    private fun startEncoder(size: Size) {
        releaseEncoder()
        val f = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, size.width, size.height).apply {
            setInteger(MediaFormat.KEY_COLOR_FORMAT, MediaCodecInfo.CodecCapabilities.COLOR_FormatYUV420Flexible)
            setInteger(MediaFormat.KEY_BIT_RATE, (if (kbps > 0) kbps else START_KBPS) * 1000)
            setInteger(MediaFormat.KEY_FRAME_RATE, FPS)
            setInteger(MediaFormat.KEY_I_FRAME_INTERVAL, 2)
            setInteger(MediaFormat.KEY_BITRATE_MODE, MediaCodecInfo.EncoderCapabilities.BITRATE_MODE_CBR)
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.R) setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
        }
        enc = runCatching {
            MediaCodec.createEncoderByType(MediaFormat.MIMETYPE_VIDEO_AVC).apply {
                configure(f, null, null, MediaCodec.CONFIGURE_FLAG_ENCODE)
                start()
            }
        }.onFailure { Log.w(TAG, "encoder: $it") }.getOrNull()
        encSize = size
        csd = null
    }

    private fun releaseEncoder() {
        runCatching { enc?.stop() }
        runCatching { enc?.release() }
        enc = null
        encSize = null
    }

    private fun drain(e: MediaCodec, rotation: Int) {
        val info = MediaCodec.BufferInfo()
        while (true) {
            val o = e.dequeueOutputBuffer(info, 0)
            if (o < 0) return
            val buf = e.getOutputBuffer(o)
            if (buf != null && info.size > 0) {
                val data = ByteArray(info.size)
                buf.position(info.offset)
                buf.get(data)
                if (info.flags and MediaCodec.BUFFER_FLAG_CODEC_CONFIG != 0) {
                    csd = data
                } else {
                    val key = info.flags and MediaCodec.BUFFER_FLAG_KEY_FRAME != 0
                    val c = csd
                    val out = ByteArray(1 + (if (key && c != null) c.size else 0) + data.size)
                    out[0] = (rotation / 90).toByte()
                    var p = 1
                    if (key && c != null) { System.arraycopy(c, 0, out, 1, c.size); p += c.size }
                    System.arraycopy(data, 0, out, p, data.size)
                    runCatching { core.sendCallVideo(id, out, key) }
                }
            }
            e.releaseOutputBuffer(o, false)
        }
    }

    // ---- receive + decoder ----

    private fun receiveLoop() {
        var dec: MediaCodec? = null
        var decSurface: Surface? = null
        val info = MediaCodec.BufferInfo()
        var lastShown = 0L
        try {
            while (running) {
                val b = runCatching { core.pullCallVideo(id, 200L) }.getOrNull()
                val now = System.currentTimeMillis()
                if (b == null || b.size < 7) {
                    if (_remote.value.active && now - lastShown > 1_500) _remote.value = _remote.value.copy(active = false)
                    continue
                }
                val key = b[0].toInt() and 1 == 1
                val rotation = (b[5].toInt() and 3) * 90
                val au = b.copyOfRange(6, b.size)
                val s = surface
                if (dec != null && decSurface !== s) { // the view changed: start over at the next keyframe
                    runCatching { dec.stop(); dec.release() }
                    dec = null
                }
                if (s == null || !s.isValid) continue
                if (dec == null) {
                    if (!key) continue
                    dec = startDecoder(au, s) ?: continue
                    decSurface = s
                }
                val d = dec ?: continue
                runCatching {
                    val i = d.dequeueInputBuffer(20_000)
                    if (i >= 0) {
                        d.getInputBuffer(i)?.apply { clear(); put(au) }
                        d.queueInputBuffer(i, 0, au.size, now * 1000, 0)
                    }
                    while (true) {
                        val o = d.dequeueOutputBuffer(info, 0)
                        if (o == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                            val f = d.outputFormat
                            val w = if (f.containsKey("crop-right")) f.getInteger("crop-right") - f.getInteger("crop-left") + 1 else f.getInteger(MediaFormat.KEY_WIDTH)
                            val h = if (f.containsKey("crop-bottom")) f.getInteger("crop-bottom") - f.getInteger("crop-top") + 1 else f.getInteger(MediaFormat.KEY_HEIGHT)
                            _remote.value = _remote.value.copy(width = w, height = h)
                            continue
                        }
                        if (o < 0) break
                        d.releaseOutputBuffer(o, true)
                        lastShown = now
                        val r = _remote.value
                        if (!r.active || r.rotation != rotation) _remote.value = r.copy(active = true, rotation = rotation)
                    }
                }.onFailure {
                    Log.w(TAG, "decode: $it")
                    runCatching { dec?.release() }
                    dec = null
                }
            }
        } finally {
            runCatching { dec?.stop() }
            runCatching { dec?.release() }
        }
    }

    private fun startDecoder(keyframe: ByteArray, s: Surface): MediaCodec? {
        val nals = H264.nals(keyframe)
        val sps = nals.firstOrNull { H264.type(it) == 7 } ?: return null
        val pps = nals.firstOrNull { H264.type(it) == 8 } ?: return null
        val f = MediaFormat.createVideoFormat(MediaFormat.MIMETYPE_VIDEO_AVC, 1280, 720).apply {
            setByteBuffer("csd-0", ByteBuffer.wrap(H264.START + sps))
            setByteBuffer("csd-1", ByteBuffer.wrap(H264.START + pps))
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.R) setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
        }
        return runCatching {
            MediaCodec.createDecoderByType(MediaFormat.MIMETYPE_VIDEO_AVC).apply {
                configure(f, s, null, 0)
                start()
            }
        }.onFailure { Log.w(TAG, "decoder: $it") }.getOrNull()
    }

    companion object {
        private const val TAG = "CallVideo"
        private const val FPS = 20
        private const val FRAME_NS = 1_000_000_000L / FPS - 5_000_000L
        private const val START_KBPS = 500
        const val H264_CODEC = 17

        /** Video codecs this phone can both encode and decode ("" = none: voice only). */
        fun codecs(): String {
            val list = MediaCodecList(MediaCodecList.REGULAR_CODECS).codecInfos
            val enc = list.any { it.isEncoder && MediaFormat.MIMETYPE_VIDEO_AVC in it.supportedTypes }
            val dec = list.any { !it.isEncoder && MediaFormat.MIMETYPE_VIDEO_AVC in it.supportedTypes }
            return if (enc && dec) "$H264_CODEC" else ""
        }

        /** Copies a camera frame into the encoder's input image (any plane layout). */
        fun copyYuv(src: ImageProxy, dst: Image) {
            val w = minOf(src.width, dst.width)
            val h = minOf(src.height, dst.height)
            for (p in 0 until 3) {
                val sp = src.planes[p]
                val dp = dst.planes[p]
                val pw = if (p == 0) w else w / 2
                val ph = if (p == 0) h else h / 2
                copyPlane(sp.buffer, sp.rowStride, sp.pixelStride, dp.buffer, dp.rowStride, dp.pixelStride, pw, ph)
            }
        }

        fun copyPlane(s: ByteBuffer, sRow: Int, sPix: Int, d: ByteBuffer, dRow: Int, dPix: Int, w: Int, h: Int) {
            if (sPix == 1 && dPix == 1) {
                val row = ByteArray(w)
                for (y in 0 until h) {
                    s.position(y * sRow); s.get(row, 0, minOf(w, s.remaining()))
                    d.position(y * dRow); d.put(row, 0, minOf(w, d.remaining()))
                }
                return
            }
            for (y in 0 until h) {
                val so = y * sRow
                val dO = y * dRow
                for (x in 0 until w) {
                    val si = so + x * sPix
                    val di = dO + x * dPix
                    if (si < s.limit() && di < d.limit()) d.put(di, s.get(si))
                }
            }
        }
    }
}

/** H.264 Annex-B helpers. */
object H264 {
    val START = byteArrayOf(0, 0, 0, 1)

    fun type(nal: ByteArray): Int = if (nal.isEmpty()) -1 else nal[0].toInt() and 0x1f

    /** Splits an Annex-B stream into NAL units (without start codes). */
    fun nals(b: ByteArray): List<ByteArray> {
        val starts = ArrayList<IntArray>() // [start of code, start of NAL]
        var i = 0
        while (i + 2 < b.size) {
            if (b[i].toInt() == 0 && b[i + 1].toInt() == 0) {
                if (b[i + 2].toInt() == 1) { starts.add(intArrayOf(i, i + 3)); i += 3; continue }
                if (i + 3 < b.size && b[i + 2].toInt() == 0 && b[i + 3].toInt() == 1) { starts.add(intArrayOf(i, i + 4)); i += 4; continue }
            }
            i++
        }
        return starts.mapIndexed { k, s -> b.copyOfRange(s[1], if (k + 1 < starts.size) starts[k + 1][0] else b.size) }
    }
}
