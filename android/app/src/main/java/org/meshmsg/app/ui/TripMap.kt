package org.meshmsg.app.ui

import android.content.Context
import android.graphics.BitmapFactory
import android.util.LruCache
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.FilledTonalIconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.drawText
import androidx.compose.ui.text.rememberTextMeasurer
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.floor
import kotlin.math.ln
import kotlin.math.max
import kotlin.math.min
import kotlin.math.pow
import kotlin.math.sin
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.meshmsg.app.BuildConfig
import org.meshmsg.app.data.TripMember

/**
 * OpenStreetMap tiles for the trip map, kept on disk (cacheDir/tiles) so a
 * map seen once with internet still shows without it. The map works without
 * tiles too: members, tracks and distances are drawn on a plain grid.
 */
private object TileCache {
    private val mem = LruCache<String, ImageBitmap>(96)
    private val loading = ConcurrentHashMap.newKeySet<String>()
    private val failed = ConcurrentHashMap<String, Long>()
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    fun get(ctx: Context, z: Int, x: Int, y: Int, loaded: () -> Unit): ImageBitmap? {
        val key = "$z/$x/$y"
        mem.get(key)?.let { return it }
        if ((failed[key] ?: 0L) > System.currentTimeMillis() || !loading.add(key)) return null
        scope.launch {
            try {
                val f = File(ctx.cacheDir, "tiles/$key.png")
                if (!f.exists() || System.currentTimeMillis() - f.lastModified() > 30L * 86400_000) {
                    val c = URL("https://tile.openstreetmap.org/$key.png").openConnection() as HttpURLConnection
                    c.connectTimeout = 10_000
                    c.readTimeout = 15_000
                    c.setRequestProperty("User-Agent", "Mava/${BuildConfig.VERSION_NAME} (mesh messenger, trip map)")
                    if (c.responseCode == 200) {
                        f.parentFile?.mkdirs()
                        c.inputStream.use { inp -> f.outputStream().use { inp.copyTo(it) } }
                    }
                    c.disconnect()
                }
                val bmp = if (f.exists()) BitmapFactory.decodeFile(f.path) else null
                if (bmp != null) {
                    mem.put(key, bmp.asImageBitmap())
                    loaded()
                } else {
                    failed[key] = System.currentTimeMillis() + 60_000
                }
            } catch (e: Exception) {
                failed[key] = System.currentTimeMillis() + 60_000
            } finally {
                loading.remove(key)
            }
        }
        return null
    }
}

// Web Mercator, as unit square coordinates (0…1).
private fun mx(lon: Double) = (lon + 180.0) / 360.0
private fun my(lat: Double): Double {
    val s = sin(lat * PI / 180.0)
    return 0.5 - ln((1 + s) / (1 - s)) / (4 * PI)
}

/**
 * The members of a trip on a map (leaders): last positions (red when lost,
 * grey when old), their tracks, the accuracy circle and this phone ("me").
 * Drag to move, pinch to zoom, tap a member to select it.
 */
@Composable
fun TripMapView(
    members: List<TripMember>,
    me: Pair<Double, Double>?,
    staleAfterSec: Long,
    selected: String?,
    onSelect: (TripMember?) -> Unit,
    modifier: Modifier = Modifier,
) {
    val ctx = LocalContext.current
    val density = LocalDensity.current.density
    val measurer = rememberTextMeasurer()
    var tick by remember { mutableIntStateOf(0) } // a tile arrived
    var zoom by remember { mutableStateOf(14.0) }
    var cx by remember { mutableStateOf(0.5) }
    var cy by remember { mutableStateOf(0.5) }
    var box by remember { mutableStateOf(IntSize.Zero) }
    var fitted by remember { mutableStateOf(false) }
    val placed = members.filter { it.hasPosition }

    fun fit() {
        val pts = placed.map { mx(it.lon) to my(it.lat) } + listOfNotNull(me?.let { mx(it.second) to my(it.first) })
        if (pts.isEmpty() || box.width == 0) return
        val minX = pts.minOf { it.first }; val maxX = pts.maxOf { it.first }
        val minY = pts.minOf { it.second }; val maxY = pts.maxOf { it.second }
        cx = (minX + maxX) / 2; cy = (minY + maxY) / 2
        val tile = 256.0 * density
        val spanX = max(maxX - minX, 1e-9) * tile; val spanY = max(maxY - minY, 1e-9) * tile
        val z = minOf(ln((box.width * 0.7) / spanX) / ln(2.0), ln((box.height * 0.7) / spanY) / ln(2.0))
        zoom = z.coerceIn(3.0, 17.0)
        fitted = true
    }
    LaunchedEffect(box, placed.size, me != null) { if (!fitted) fit() }

    Box(modifier.background(Color(0xFFE9E5DC))) {
        Canvas(
            Modifier.fillMaxSize()
                .onSizeChanged { box = it }
                .pointerInput(Unit) {
                    detectTransformGestures { _, pan, z, _ ->
                        val world = 256.0 * density * 2.0.pow(zoom)
                        cx -= pan.x / world
                        cy -= pan.y / world
                        zoom = (zoom + ln(z.toDouble()) / ln(2.0)).coerceIn(2.0, 19.0)
                    }
                }
                .pointerInput(members) {
                    detectTapGestures { at ->
                        val world = 256.0 * density * 2.0.pow(zoom)
                        fun screen(m: TripMember) = Offset(
                            ((mx(m.lon) - cx) * world + box.width / 2.0).toFloat(),
                            ((my(m.lat) - cy) * world + box.height / 2.0).toFloat(),
                        )
                        val hit = placed.minByOrNull { (screen(it) - at).getDistance() }
                        onSelect(hit?.takeIf { (screen(it) - at).getDistance() < 40 * density })
                    }
                },
        ) {
            if (tick < 0) return@Canvas // redraw when a tile arrives
            val w = this.size.width; val h = this.size.height
            val tz = floor(zoom).toInt().coerceIn(2, 19)
            val n = 1 shl tz
            val world = 256.0 * density * 2.0.pow(zoom)
            val tilePx = (world / n).toFloat()
            val left = cx * world - w / 2; val top = cy * world - h / 2
            // Tiles (or a grid where none is loaded).
            val x0 = floor(left / tilePx).toInt(); val x1 = floor((left + w) / tilePx).toInt()
            val y0 = max(0, floor(top / tilePx).toInt()); val y1 = min(n - 1, floor((top + h) / tilePx).toInt())
            for (ty in y0..y1) for (tx in x0..x1) {
                val dx = (tx * tilePx - left).toFloat(); val dy = (ty * tilePx - top).toFloat()
                val img = TileCache.get(ctx, tz, ((tx % n) + n) % n, ty) { tick++ }
                if (img != null) {
                    drawImage(img, dstOffset = IntOffset(dx.toInt(), dy.toInt()), dstSize = IntSize(tilePx.toInt() + 1, tilePx.toInt() + 1))
                } else {
                    drawRect(Color(0xFFD9D4C7), Offset(dx, dy), Size(tilePx, tilePx), style = Stroke(1f))
                }
            }
            fun pos(lat: Double, lon: Double) = Offset(((mx(lon) * world) - left).toFloat(), ((my(lat) * world) - top).toFloat())
            val now = System.currentTimeMillis() / 1000
            // Tracks.
            for (m in placed) {
                if (m.track.size < 2) continue
                val path = Path()
                m.track.forEachIndexed { i, p -> val o = pos(p.lat, p.lon); if (i == 0) path.moveTo(o.x, o.y) else path.lineTo(o.x, o.y) }
                drawPath(path, if (m.sos) Color(0xCCDC2626) else Color(0xAA7C3AED), style = Stroke(3f * density))
            }
            // Members.
            for (m in placed) {
                val o = pos(m.lat, m.lon)
                val metersPerPx = 40075016.686 * cos(m.lat * PI / 180) / world
                val r = (m.accuracy / metersPerPx).toFloat()
                if (r > 6) drawCircle(Color(0x337C3AED), r, o)
                val old = now - m.seen > staleAfterSec
                val color = when { m.sos -> Color(0xFFDC2626); old || m.stopped -> Color(0xFF6B7280); else -> Color(0xFF7C3AED) }
                drawCircle(Color.White, 9f * density, o)
                drawCircle(color, 7f * density, o)
                if (m.hash == selected) drawCircle(Color.Black, 12f * density, o, style = Stroke(2f * density))
                val label = (if (m.sos) "⚠ " else "") + m.displayName
                val tl = measurer.measure(label, TextStyle(fontSize = 12.sp, color = Color.White))
                val lo = Offset(o.x - tl.size.width / 2f, o.y - 14 * density - tl.size.height)
                drawRect(color, lo - Offset(4 * density, 1 * density), Size(tl.size.width + 8 * density, tl.size.height + 2 * density))
                drawText(tl, topLeft = lo)
            }
            // This phone.
            me?.let { (lat, lon) ->
                val o = pos(lat, lon)
                drawCircle(Color.White, 8f * density, o)
                drawCircle(Color(0xFF0EA5E9), 6f * density, o)
            }
        }
        Column(Modifier.align(Alignment.TopEnd).padding(8.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            FilledTonalIconButton(onClick = { zoom = (zoom + 1).coerceAtMost(19.0) }, modifier = Modifier.size(40.dp)) { Text("+") }
            FilledTonalIconButton(onClick = { zoom = (zoom - 1).coerceAtLeast(2.0) }, modifier = Modifier.size(40.dp)) { Text("−") }
            FilledTonalIconButton(onClick = { fit() }, modifier = Modifier.size(40.dp)) { Text("⤢") }
        }
        Text(
            "© OpenStreetMap",
            style = MaterialTheme.typography.labelSmall,
            color = Color.Black,
            modifier = Modifier.align(Alignment.BottomEnd).background(Color(0xBBFFFFFF)).padding(horizontal = 4.dp),
        )
    }
}
