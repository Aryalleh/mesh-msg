package org.meshmsg.app.hamyar

import android.app.Activity
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.nfc.NfcAdapter
import android.nfc.tech.IsoDep
import android.util.LruCache
import androidx.camera.core.CameraSelector
import androidx.camera.core.ImageAnalysis
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.view.PreviewView
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import androidx.lifecycle.compose.LocalLifecycleOwner
import com.google.zxing.BarcodeFormat
import com.google.zxing.BinaryBitmap
import com.google.zxing.DecodeHintType
import com.google.zxing.EncodeHintType
import com.google.zxing.PlanarYUVLuminanceSource
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import com.google.zxing.qrcode.QRCodeWriter
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.meshmsg.app.ui.theme.HamyarColors as Hy

// ------------------------------------------------------------------ text helpers

private const val FA_DIGITS = "۰۱۲۳۴۵۶۷۸۹"

/** Latin digits to Persian ones (the website shows Persian digits everywhere). */
fun fa(s: Any?): String = s?.toString().orEmpty().map { if (it in '0'..'9') FA_DIGITS[it - '0'] else it }.joinToString("")

/** 2450000 -> "۲٬۴۵۰٬۰۰۰". */
fun money(n: Long?): String = if (n == null) "—" else fa("%,d".format(java.util.Locale.US, n)).replace(',', '٬')

// ------------------------------------------------------------------ building blocks (the website's look)

val CardShape = RoundedCornerShape(24.dp)

/** The dark green header with rounded bottom corners, like the website's pages. */
@Composable
fun HyHeader(title: String, sub: String? = null, onBack: (() -> Unit)? = null, actions: @Composable RowScope.() -> Unit = {}, extra: @Composable ColumnScope.() -> Unit = {}) {
    Column(
        Modifier.fillMaxWidth().background(Hy.brand900, RoundedCornerShape(bottomStart = 28.dp, bottomEnd = 28.dp))
            .statusBarsPadding().padding(start = 12.dp, end = 12.dp, top = 8.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            if (onBack != null) {
                IconButton(onClick = onBack, modifier = Modifier.size(48.dp)) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, "بازگشت", tint = Color.White)
                }
            } else Spacer(Modifier.size(8.dp))
            Column(Modifier.weight(1f).padding(horizontal = 4.dp)) {
                Text(title, color = Color.White, fontSize = 20.sp, fontWeight = FontWeight.ExtraBold)
                if (sub != null) Text(sub, color = Color.White.copy(alpha = .7f), fontSize = 13.sp, lineHeight = 22.sp)
            }
            actions()
        }
        extra()
    }
}

@Composable
fun HyCard(modifier: Modifier = Modifier, padding: Dp = 16.dp, onClick: (() -> Unit)? = null, content: @Composable ColumnScope.() -> Unit) {
    Column(
        modifier.fillMaxWidth().clip(CardShape).background(Color.White).border(1.dp, Hy.sand200, CardShape)
            .then(if (onClick != null) Modifier.clickable(role = Role.Button, onClick = onClick) else Modifier).padding(padding),
        verticalArrangement = Arrangement.spacedBy(10.dp), content = content,
    )
}

@Composable
fun HyButton(text: String, modifier: Modifier = Modifier, enabled: Boolean = true, busy: Boolean = false, color: Color = Hy.brand600, onClick: () -> Unit) {
    Button(
        onClick = onClick, enabled = enabled && !busy, shape = RoundedCornerShape(16.dp),
        colors = ButtonDefaults.buttonColors(containerColor = color, contentColor = Color.White),
        modifier = modifier.heightIn(min = 48.dp),
    ) {
        if (busy) CircularProgressIndicator(Modifier.size(20.dp), color = Color.White, strokeWidth = 2.dp)
        else Text(text, fontWeight = FontWeight.Bold, fontSize = 14.sp)
    }
}

/** A light pill button (secondary actions). */
@Composable
fun HyPill(text: String, modifier: Modifier = Modifier, bg: Color = Hy.brand50, fg: Color = Hy.brand700, onClick: () -> Unit) {
    Box(
        modifier.heightIn(min = 40.dp).clip(RoundedCornerShape(14.dp)).background(bg).clickable(role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 8.dp),
        contentAlignment = Alignment.Center,
    ) { Text(text, color = fg, fontWeight = FontWeight.Bold, fontSize = 13.sp) }
}

/** A coloured rounded square with an icon, as on the website's lists. */
@Composable
fun IconTile(icon: ImageVector, tint: Color, bg: Color, size: Dp = 44.dp) {
    Box(Modifier.size(size).background(bg, RoundedCornerShape(14.dp)), contentAlignment = Alignment.Center) {
        Icon(icon, null, tint = tint, modifier = Modifier.size(size / 2))
    }
}

@Composable
fun Badge(text: String, fg: Color, bg: Color) {
    Text(text, color = fg, fontSize = 11.sp, fontWeight = FontWeight.Bold, modifier = Modifier.background(bg, RoundedCornerShape(50)).padding(horizontal = 10.dp, vertical = 3.dp))
}

@Composable
fun SectionTitle(text: String, action: String? = null, onAction: (() -> Unit)? = null) {
    Row(Modifier.fillMaxWidth().padding(top = 8.dp), verticalAlignment = Alignment.CenterVertically) {
        Text(text, fontSize = 16.sp, fontWeight = FontWeight.ExtraBold, color = Hy.ink900, modifier = Modifier.weight(1f))
        if (action != null && onAction != null) {
            Text(action, color = Hy.brand700, fontSize = 13.sp, fontWeight = FontWeight.Bold, modifier = Modifier.clickable(onClick = onAction).padding(8.dp))
        }
    }
}

@Composable
fun ErrorLine(code: String?) {
    if (code == null || code == "unauthorized") return
    Text(
        when (code) {
            "network" -> "اینترنت در دسترس نیست؛ آخرین اطلاعات ذخیره‌شده نمایش داده می‌شود."
            "module_off" -> "این بخش برای دانشگاه شما فعال نیست."
            else -> "خطا: $code"
        },
        color = Hy.rose600, fontSize = 13.sp,
    )
}

fun errCode(t: Throwable) = (t as? HamyarError)?.code ?: "network"

// ------------------------------------------------------------------ images from the network (no image library)

private val imageCache = LruCache<String, Bitmap>(24)

/** A picture from a URL, kept in memory; [fallback] while loading or when it fails. */
@Composable
fun RemoteImage(url: String?, modifier: Modifier, fallback: @Composable BoxScope.() -> Unit = {}) {
    var bmp by remember(url) { mutableStateOf(url?.let { imageCache.get(it) }) }
    LaunchedEffect(url) {
        if (url == null || bmp != null) return@LaunchedEffect
        bmp = withContext(Dispatchers.IO) {
            runCatching {
                val c = java.net.URL(url).openConnection() as java.net.HttpURLConnection
                c.connectTimeout = 15_000; c.readTimeout = 20_000
                try {
                    val bytes = c.inputStream.use { it.readBytes() }
                    val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, o)
                    var s = 1
                    while (o.outWidth / (s * 2) >= 720) s *= 2
                    BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = s })
                } finally { c.disconnect() }
            }.getOrNull()?.also { imageCache.put(url, it) }
        }
    }
    Box(modifier) {
        val b = bmp
        if (b != null) Image(b.asImageBitmap(), null, Modifier.fillMaxSize(), contentScale = ContentScale.Crop) else fallback()
    }
}

/** A round profile photo, or the first letter of the name. */
@Composable
fun Avatar(url: String?, name: String, size: Dp, ring: Color = Color.White.copy(alpha = .25f)) {
    RemoteImage(url, Modifier.size(size).clip(CircleShape).border(2.dp, ring, CircleShape).background(Hy.brand700)) {
        Text(name.trim().take(1), color = Color.White, fontWeight = FontWeight.Bold, fontSize = (size.value / 2.4f).sp, modifier = Modifier.align(Alignment.Center))
    }
}

// ------------------------------------------------------------------ QR codes

fun qrBitmap(text: String, px: Int = 512): ImageBitmap {
    val m = QRCodeWriter().encode(text, BarcodeFormat.QR_CODE, px, px, mapOf(EncodeHintType.MARGIN to 1))
    val pixels = IntArray(m.width * m.height) { i -> if (m.get(i % m.width, i / m.width)) 0xFF14211F.toInt() else 0xFFFFFFFF.toInt() }
    return Bitmap.createBitmap(pixels, m.width, m.height, Bitmap.Config.ARGB_8888).asImageBitmap()
}

/** The back camera, reading QR codes; [onCode] gets each new text (the camera permission must be granted). */
@Composable
fun QrScanner(modifier: Modifier, onCode: (String) -> Unit) {
    val ctx = LocalContext.current
    val owner = LocalLifecycleOwner.current
    val emit by rememberUpdatedState(onCode)
    val view = remember { PreviewView(ctx).apply { scaleType = PreviewView.ScaleType.FILL_CENTER } }
    DisposableEffect(owner) {
        val future = ProcessCameraProvider.getInstance(ctx)
        val exec = java.util.concurrent.Executors.newSingleThreadExecutor()
        var last = ""
        var lastAt = 0L
        future.addListener({
            val provider = future.get()
            val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
            val reader = QRCodeReader()
            val analysis = ImageAnalysis.Builder().setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
            analysis.setAnalyzer(exec) { img ->
                try {
                    val plane = img.planes[0]
                    val buf = plane.buffer
                    val data = ByteArray(buf.remaining()).also { buf.get(it) }
                    val src = PlanarYUVLuminanceSource(data, plane.rowStride, img.height, 0, 0, img.width, img.height, false)
                    val text = runCatching {
                        reader.decode(BinaryBitmap(HybridBinarizer(src)), mapOf(DecodeHintType.TRY_HARDER to true)).text
                    }.getOrNull()
                    reader.reset()
                    val now = System.currentTimeMillis()
                    if (text != null && (text != last || now - lastAt > 3000)) {
                        last = text; lastAt = now
                        ContextCompat.getMainExecutor(ctx).execute { emit(text) }
                    }
                } finally { img.close() }
            }
            runCatching {
                provider.unbindAll()
                provider.bindToLifecycle(owner, CameraSelector.DEFAULT_BACK_CAMERA, preview, analysis)
            }
        }, ContextCompat.getMainExecutor(ctx))
        onDispose {
            runCatching { future.get().unbindAll() }
            exec.shutdown()
        }
    }
    AndroidView({ view }, modifier)
}

// ------------------------------------------------------------------ NFC: reading a card (checkers)

/**
 * While shown, this phone reads Hamyar cards held against it (NFC reader mode); [onCode] gets the code, or null for a
 * card that isn't a Hamyar card. Returns false when the phone has no NFC or it is off.
 */
@Composable
fun NfcCardReader(onCode: (String?) -> Unit): Boolean {
    val activity = LocalContext.current as? Activity ?: return false
    val adapter = remember { NfcAdapter.getDefaultAdapter(activity) }
    val emit by rememberUpdatedState(onCode)
    if (adapter == null || !adapter.isEnabled) return false
    DisposableEffect(adapter) {
        adapter.enableReaderMode(
            activity,
            { tag ->
                val code = runCatching {
                    IsoDep.get(tag)?.use { dep ->
                        dep.connect()
                        dep.timeout = 3000
                        CardCodes.parseAnswer(dep.transceive(CardCodes.selectApdu()))
                    }
                }.getOrNull()
                activity.runOnUiThread { emit(code) }
            },
            NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_B or NfcAdapter.FLAG_READER_SKIP_NDEF_CHECK,
            null,
        )
        onDispose { runCatching { adapter.disableReaderMode(activity) } }
    }
    return true
}
