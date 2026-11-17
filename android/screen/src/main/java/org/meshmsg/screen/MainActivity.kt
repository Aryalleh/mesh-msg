package org.meshmsg.screen

import android.content.Intent
import android.graphics.Bitmap
import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.RowScope
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.aspectRatio
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.CutCornerShape
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.FilterQuality
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.compose.collectAsStateWithLifecycle

// Mava design tokens (docs/design.md).
private val Bg = Color(0xFF0B0A14)
private val S1 = Color(0xFF15122A)
private val S2 = Color(0xFF1E1A3A)
private val Border = Color(0xFF3B2A6B)
private val Fg = Color(0xFFECE8FF)
private val Muted = Color(0xFFA9A2CF)
private val Primary = Color(0xFF7C3AED)
private val PrimaryHi = Color(0xFFA78BFA)
private val Accent = Color(0xFFD946EF)
private val Ok = Color(0xFF22D3EE)
private val Err = Color(0xFFFB7185)
private val CallGreen = Color(0xFF16A34A)
private val EndRed = Color(0xFFDC2626)
private val Chamfer = CutCornerShape(8.dp)

private val Colors = darkColorScheme(
    background = Bg, surface = S1, surfaceVariant = S2, onBackground = Fg, onSurface = Fg,
    onSurfaceVariant = Muted, primary = Primary, onPrimary = Color.White, secondary = PrimaryHi,
    tertiary = Accent, outline = Border, error = Err,
)

class MainActivity : ComponentActivity() {
    private val vm: ScreenViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MaterialTheme(colorScheme = Colors) {
                val s by vm.state.collectAsStateWithLifecycle()
                LaunchedEffect(s.connected) {
                    if (s.connected) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                    else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                }
                ScreenApp(s, vm)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        vm.refresh()
    }
}

@Composable
private fun ScreenApp(s: ScreenUi, vm: ScreenViewModel) {
    Surface(Modifier.fillMaxSize(), color = Bg) {
        Column(
            Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(horizontal = 16.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    "MAVA", fontSize = 22.sp, fontWeight = FontWeight.Black, letterSpacing = 4.sp,
                    style = MaterialTheme.typography.headlineSmall.copy(brush = Brush.linearGradient(listOf(PrimaryHi, Accent))),
                )
                Spacer(Modifier.width(10.dp))
                Text(stringResource(R.string.title), color = Fg, fontSize = 16.sp, modifier = Modifier.weight(1f))
                Spacer(Modifier.size(10.dp).background(if (s.connected) Ok else Muted, CircleShape))
            }
            Status(s, vm)
            BoardScreen(s, vm)
            if (s.connected) {
                Keys(vm)
                TypeBox(vm)
            }
            Text(stringResource(R.string.help), color = Muted, fontSize = 12.sp, lineHeight = 18.sp)
        }
    }
}

@Composable
private fun Status(s: ScreenUi, vm: ScreenViewModel) {
    val text = when {
        s.error != null -> s.error
        s.connected && s.width == 0 -> stringResource(R.string.waiting)
        s.connected -> stringResource(R.string.connected, s.device ?: "USB")
        s.connecting -> stringResource(R.string.connecting)
        s.device == null -> stringResource(R.string.no_board)
        !s.permitted -> stringResource(R.string.board_found, s.device)
        else -> stringResource(R.string.board_found, s.device)
    }
    Text(text, color = if (s.error != null) Err else if (s.connected) Ok else Muted, fontSize = 13.sp)
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        when {
            s.device != null && !s.permitted ->
                SmallButton(stringResource(R.string.allow_usb), Modifier) { vm.requestPermission() }
            s.error != null -> SmallButton(stringResource(R.string.retry), Modifier) { vm.retry() }
            s.connected -> SmallButton(stringResource(R.string.disconnect), Modifier) { vm.disconnect() }
            s.device != null && !s.connecting -> SmallButton(stringResource(R.string.connect), Modifier) { vm.connect() }
        }
    }
}

/** The board's screen, scaled to fit, pixels kept sharp; touches go back to the board. */
@Composable
private fun BoardScreen(s: ScreenUi, vm: ScreenViewModel) {
    val w = if (s.width > 0) s.width else 240
    val h = if (s.height > 0) s.height else 320
    val bitmap = remember(w, h) { Bitmap.createBitmap(w, h, Bitmap.Config.ARGB_8888) }
    val buf = remember(w, h) { IntArray(w * h) }
    Box(
        Modifier.fillMaxWidth().padding(horizontal = 24.dp).aspectRatio(w.toFloat() / h)
            .border(BorderStroke(1.dp, if (s.connected) Accent else Border), Chamfer)
            .padding(3.dp)
            .pointerInput(w, h) {
                fun map(o: Offset): IntOffset = IntOffset(
                    (o.x / size.width * w).toInt().coerceIn(0, w - 1),
                    (o.y / size.height * h).toInt().coerceIn(0, h - 1),
                )
                awaitEachGesture {
                    val down = awaitFirstDown()
                    var last = map(down.position)
                    var lastSent = System.currentTimeMillis()
                    vm.session?.touch(last.x, last.y, true)
                    while (true) {
                        val ev = awaitPointerEvent()
                        val c = ev.changes.firstOrNull { it.id == down.id } ?: break
                        val p = map(c.position)
                        if (!c.pressed) {
                            vm.session?.touch(p.x, p.y, false)
                            break
                        }
                        val now = System.currentTimeMillis()
                        if (p != last && now - lastSent >= 30) {
                            vm.session?.touch(p.x, p.y, true)
                            last = p
                            lastSent = now
                        }
                        c.consume()
                    }
                }
            },
    ) {
        Canvas(Modifier.fillMaxSize().background(Color.Black)) {
            if (s.version >= 0 && s.width > 0) { // reading s.version redraws on every change
                vm.frame.copyTo(buf)
                bitmap.setPixels(buf, 0, w, 0, 0, w, h)
                drawImage(
                    bitmap.asImageBitmap(), dstSize = IntSize(size.width.toInt(), size.height.toInt()),
                    filterQuality = FilterQuality.None,
                )
            }
        }
    }
}

@Composable
private fun Keys(vm: ScreenViewModel) {
    val k: (Key) -> Unit = { vm.session?.key(it) }
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            KeyButton("📞", CallGreen) { k(Key.Call) }
            KeyButton(stringResource(R.string.key_menu)) { k(Key.Menu) }
            KeyButton("▲") { k(Key.Up) }
            KeyButton(stringResource(R.string.key_back)) { k(Key.Back) }
            KeyButton("⏻", EndRed) { k(Key.End) }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            KeyButton("◀") { k(Key.Left) }
            KeyButton(stringResource(R.string.key_ok), Primary) { k(Key.Ok) }
            KeyButton("▶") { k(Key.Right) }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
            KeyButton("⌫") { k(Key.Backspace) }
            KeyButton("▼") { k(Key.Down) }
            KeyButton("⏎") { k(Key.Enter) }
            KeyButton("#?") { k(Key.Symbols) }
        }
    }
}

@Composable
private fun RowScope.KeyButton(label: String, color: Color? = null, onClick: () -> Unit) {
    if (color != null) {
        Button(
            onClick = onClick, shape = Chamfer, modifier = Modifier.weight(1f).heightIn(min = 46.dp),
            colors = ButtonDefaults.buttonColors(containerColor = color),
        ) { Text(label, fontSize = 15.sp, maxLines = 1) }
    } else {
        OutlinedButton(
            onClick = onClick, shape = Chamfer, modifier = Modifier.weight(1f).heightIn(min = 46.dp),
            border = BorderStroke(1.dp, Border),
        ) { Text(label, color = Fg, fontSize = 15.sp, maxLines = 1) }
    }
}

@Composable
private fun SmallButton(label: String, modifier: Modifier, onClick: () -> Unit) {
    OutlinedButton(onClick = onClick, shape = Chamfer, border = BorderStroke(1.dp, PrimaryHi), modifier = modifier) {
        Text(label, color = PrimaryHi, fontSize = 13.sp)
    }
}

/** Typing with the phone's keyboard (any language it has): sent as keys to the board. */
@Composable
private fun TypeBox(vm: ScreenViewModel) {
    var text by rememberSaveable { mutableStateOf("") }
    val send = {
        if (text.isNotEmpty()) {
            vm.session?.type(text)
            text = ""
        }
    }
    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedTextField(
            value = text, onValueChange = { text = it }, singleLine = true, modifier = Modifier.weight(1f),
            label = { Text(stringResource(R.string.type_hint)) }, shape = Chamfer,
            keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
            keyboardActions = KeyboardActions(onSend = { send() }),
            colors = OutlinedTextFieldDefaults.colors(
                focusedBorderColor = Accent, unfocusedBorderColor = Border, focusedLabelColor = PrimaryHi,
                unfocusedLabelColor = Muted, cursorColor = Accent, focusedTextColor = Fg, unfocusedTextColor = Fg,
            ),
        )
        Button(onClick = send, shape = Chamfer, colors = ButtonDefaults.buttonColors(containerColor = Primary)) {
            Text(stringResource(R.string.type_send))
        }
    }
}
