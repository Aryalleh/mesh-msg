package org.meshmsg.flasher

import android.content.Intent
import android.os.Bundle
import android.view.WindowManager
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.foundation.BorderStroke
import androidx.compose.foundation.background
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
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.CutCornerShape
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.OutlinedTextFieldDefaults
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
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

private val Colors = darkColorScheme(
    background = Bg, surface = S1, surfaceVariant = S2, onBackground = Fg, onSurface = Fg,
    onSurfaceVariant = Muted, primary = Primary, onPrimary = Color.White, secondary = PrimaryHi,
    tertiary = Accent, outline = Border, error = Err,
)
private val Chamfer = CutCornerShape(10.dp)

class MainActivity : ComponentActivity() {
    private val vm: FlasherViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MaterialTheme(colorScheme = Colors) {
                val s by vm.state.collectAsStateWithLifecycle()
                LaunchedEffect(s.busy || s.monitoring) {
                    // The screen must stay on while writing: a sleeping phone can cut USB.
                    if (s.busy || s.monitoring) window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                    else window.clearFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
                }
                FlasherScreen(s, vm)
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        vm.refresh()
    }

    override fun onResume() {
        super.onResume()
        vm.refresh()
    }
}

@Composable
private fun FlasherScreen(s: UiState, vm: FlasherViewModel) {
    var confirm by remember { mutableStateOf(false) }
    Surface(Modifier.fillMaxSize(), color = Bg) {
        Column(
            Modifier.safeDrawingPadding().verticalScroll(rememberScrollState()).padding(16.dp),
            verticalArrangement = Arrangement.spacedBy(14.dp),
        ) {
            Header(s)
            Panel {
                val ready = s.device != null
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Spacer(Modifier.size(10.dp).background(if (ready) Ok else Muted, CircleShape))
                    Spacer(Modifier.width(10.dp))
                    Text(
                        when {
                            !s.usbHost -> stringResource(R.string.no_usb_host)
                            ready -> stringResource(R.string.board_found, s.device!!)
                            else -> stringResource(R.string.no_board)
                        },
                        color = if (ready) Fg else Muted,
                    )
                }
                if (ready && !s.permitted && !s.busy) {
                    OutlinedButton(onClick = vm::requestPermission, shape = Chamfer, border = BorderStroke(1.dp, PrimaryHi)) {
                        Text(stringResource(R.string.allow_usb), color = PrimaryHi)
                    }
                }
            }

            Panel {
                Text(stringResource(R.string.fw_title), color = PrimaryHi, fontWeight = FontWeight.Bold)
                Text(
                    s.imagesVersion?.let {
                        stringResource(if (s.imagesFrom == "github") R.string.fw_from_github else R.string.fw_from_apk, it)
                    } ?: stringResource(R.string.no_images),
                    color = if (s.imagesVersion == null) Err else Fg, fontSize = 14.sp,
                )
                if (s.fwChecking) {
                    LinearProgressIndicator(
                        progress = { s.fwProgress }, modifier = Modifier.fillMaxWidth(), color = Accent, trackColor = S2,
                    )
                }
                s.fwMessage?.let { (ok, text) -> Text(text, color = if (ok) Ok else Err, fontSize = 13.sp) }
                OutlinedButton(
                    onClick = vm::checkFirmware, enabled = !s.fwChecking && !s.busy, shape = Chamfer,
                    border = BorderStroke(1.dp, Border),
                ) { Text(stringResource(R.string.fw_check), color = if (!s.fwChecking && !s.busy) Fg else Muted) }
            }

            if (s.imagesVersion != null) {
                val enabled = s.device != null && !s.busy && !s.monitoring
                if (s.hasPlain) {
                    Panel {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(stringResource(R.string.sims_title), color = PrimaryHi, fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f))
                            Switch(checked = !s.plain, onCheckedChange = { vm.setPlain(!it) }, enabled = !s.busy)
                        }
                        Text(stringResource(if (s.plain) R.string.sims_off else R.string.sims_on), color = Muted, fontSize = 13.sp)
                    }
                }
                Panel {
                    Button(
                        onClick = { confirm = true }, enabled = enabled, shape = Chamfer,
                        modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp),
                        colors = ButtonDefaults.buttonColors(containerColor = Primary),
                    ) { Text(stringResource(R.string.install_all), fontWeight = FontWeight.Bold) }
                    Text(stringResource(R.string.install_all_desc), color = Muted, fontSize = 13.sp)
                    Spacer(Modifier.size(4.dp))
                    OutlinedButton(
                        onClick = { vm.flash(Mode.FirmwareOnly) }, enabled = enabled, shape = Chamfer,
                        modifier = Modifier.fillMaxWidth().heightIn(min = 52.dp), border = BorderStroke(1.dp, Border),
                    ) { Text(stringResource(R.string.firmware_only), color = if (enabled) Fg else Muted) }
                    Text(stringResource(R.string.firmware_only_desc), color = Muted, fontSize = 13.sp)
                }
            }

            if (s.busy || s.outcome != null) {
                Panel {
                    if (s.busy) {
                        Text(s.phase, color = Fg)
                        LinearProgressIndicator(
                            progress = { s.progress }, modifier = Modifier.fillMaxWidth(),
                            color = Accent, trackColor = S2,
                        )
                        Text("${(s.progress * 100).toInt()} %", color = Muted, fontSize = 13.sp)
                    }
                    when (val o = s.outcome) {
                        Outcome.Ok -> Text(stringResource(R.string.done), color = Ok)
                        is Outcome.Failed -> Text(stringResource(R.string.failed, o.message), color = Err)
                        null -> {}
                    }
                }
            }

            WifiPanel(s, vm)

            RadioPanel(s, vm)

            Panel {
                Text(stringResource(R.string.board_log), color = PrimaryHi, fontWeight = FontWeight.Bold)
                Text(stringResource(R.string.board_log_desc), color = Muted, fontSize = 13.sp)
                s.hotspot?.let { (name, pass) ->
                    SelectionContainer {
                        Text(stringResource(R.string.hotspot_found, name, pass), color = Ok, fontFamily = FontFamily.Monospace)
                    }
                }
                if (s.monitoring) {
                    OutlinedButton(onClick = vm::stopMonitor, shape = Chamfer, border = BorderStroke(1.dp, Border)) {
                        Text(stringResource(R.string.stop), color = Fg)
                    }
                } else {
                    OutlinedButton(
                        onClick = vm::startMonitor, enabled = s.device != null && !s.busy, shape = Chamfer,
                        border = BorderStroke(1.dp, Border),
                    ) { Text(stringResource(R.string.board_log), color = if (s.device != null && !s.busy) Fg else Muted) }
                }
            }

            if (s.log.isNotEmpty()) {
                Panel {
                    Text(stringResource(R.string.log_title), color = PrimaryHi, fontWeight = FontWeight.Bold)
                    SelectionContainer {
                        Text(
                            s.log.takeLast(120).joinToString("\n"), color = Muted, fontSize = 11.sp,
                            fontFamily = FontFamily.Monospace, lineHeight = 15.sp,
                        )
                    }
                }
            }

            Panel {
                Text(stringResource(R.string.help_title), color = PrimaryHi, fontWeight = FontWeight.Bold)
                Text(stringResource(R.string.help_text), color = Muted, fontSize = 13.sp, lineHeight = 20.sp)
            }
        }
    }

    if (confirm) {
        AlertDialog(
            onDismissRequest = { confirm = false },
            title = { Text(stringResource(R.string.confirm_title)) },
            text = { Text(stringResource(R.string.confirm_text)) },
            confirmButton = {
                TextButton(onClick = { confirm = false; vm.flash(Mode.Everything) }) {
                    Text(stringResource(R.string.confirm_ok), color = Accent)
                }
            },
            dismissButton = { TextButton(onClick = { confirm = false }) { Text(stringResource(R.string.cancel)) } },
            containerColor = S1,
        )
    }
}

@Composable
private fun WifiPanel(s: UiState, vm: FlasherViewModel) {
    var ssid by rememberSaveable { mutableStateOf("") }
    var pass by rememberSaveable { mutableStateOf("") }
    var show by remember { mutableStateOf(false) }
    val enabled = s.device != null && !s.busy && !s.wifiBusy
    val fieldColors = OutlinedTextFieldDefaults.colors(
        focusedBorderColor = Accent, unfocusedBorderColor = Border, focusedLabelColor = PrimaryHi,
        unfocusedLabelColor = Muted, cursorColor = Accent, focusedTextColor = Fg, unfocusedTextColor = Fg,
    )
    Panel {
        Text(stringResource(R.string.wifi_title), color = PrimaryHi, fontWeight = FontWeight.Bold)
        Text(stringResource(R.string.wifi_desc), color = Muted, fontSize = 13.sp)
        OutlinedTextField(
            value = ssid, onValueChange = { ssid = it }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text(stringResource(R.string.wifi_ssid)) }, colors = fieldColors, shape = Chamfer,
        )
        OutlinedTextField(
            value = pass, onValueChange = { pass = it }, singleLine = true, modifier = Modifier.fillMaxWidth(),
            label = { Text(stringResource(R.string.wifi_password)) }, colors = fieldColors, shape = Chamfer,
            visualTransformation = if (show) VisualTransformation.None else PasswordVisualTransformation(),
            keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
            trailingIcon = {
                TextButton(onClick = { show = !show }) {
                    Text(stringResource(if (show) R.string.hide else R.string.show), color = Muted, fontSize = 12.sp)
                }
            },
        )
        Button(
            onClick = { vm.setWifi(ssid, pass) }, enabled = enabled && ssid.isNotBlank(), shape = Chamfer,
            modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp), colors = ButtonDefaults.buttonColors(containerColor = Primary),
        ) { Text(stringResource(R.string.wifi_save)) }
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = { vm.hotspot(true) }, enabled = enabled, shape = Chamfer, border = BorderStroke(1.dp, Border),
                modifier = Modifier.weight(1f),
            ) { Text(stringResource(R.string.hotspot_on), color = if (enabled) Fg else Muted, fontSize = 13.sp) }
            OutlinedButton(
                onClick = { vm.hotspot(false) }, enabled = enabled, shape = Chamfer, border = BorderStroke(1.dp, Border),
                modifier = Modifier.weight(1f),
            ) { Text(stringResource(R.string.hotspot_off), color = if (enabled) Fg else Muted, fontSize = 13.sp) }
        }
        s.wifiMessage?.let { (ok, text) -> Text(text, color = if (ok) Ok else Err, fontSize = 13.sp) }
    }
}

/** The LoRa module setting: a board without the SX1262 skips the radio. */
@Composable
private fun RadioPanel(s: UiState, vm: FlasherViewModel) {
    val enabled = s.device != null && !s.busy && !s.wifiBusy
    Panel {
        Text(stringResource(R.string.radio_title), color = PrimaryHi, fontWeight = FontWeight.Bold)
        Text(stringResource(R.string.radio_desc), color = Muted, fontSize = 13.sp)
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            OutlinedButton(
                onClick = { vm.setRadio(false) }, enabled = enabled, shape = Chamfer, border = BorderStroke(1.dp, Border),
                modifier = Modifier.weight(1f),
            ) { Text(stringResource(R.string.radio_off), color = if (enabled) Fg else Muted, fontSize = 13.sp) }
            OutlinedButton(
                onClick = { vm.setRadio(true) }, enabled = enabled, shape = Chamfer, border = BorderStroke(1.dp, Border),
                modifier = Modifier.weight(1f),
            ) { Text(stringResource(R.string.radio_on), color = if (enabled) Fg else Muted, fontSize = 13.sp) }
        }
        s.radioMessage?.let { (ok, text) -> Text(text, color = if (ok) Ok else Err, fontSize = 13.sp) }
    }
}

@Composable
private fun Header(s: UiState) {
    Column {
        Text(
            "MAVA", fontSize = 30.sp, fontWeight = FontWeight.Black, letterSpacing = 6.sp,
            style = MaterialTheme.typography.headlineLarge.copy(brush = Brush.linearGradient(listOf(PrimaryHi, Accent))),
        )
        Text(stringResource(R.string.title), color = Fg, fontSize = 18.sp)
        s.imagesVersion?.let { Text(stringResource(R.string.images_version, it), color = Muted, fontSize = 13.sp) }
    }
}

@Composable
private fun Panel(content: @Composable () -> Unit) {
    Surface(color = S1, shape = Chamfer, border = BorderStroke(1.dp, Border), modifier = Modifier.fillMaxWidth()) {
        Column(Modifier.padding(14.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) { content() }
    }
}
