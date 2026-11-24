package org.meshmsg.app.hamyar

import android.Manifest
import android.content.pm.PackageManager
import android.nfc.NfcAdapter
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AccountBox
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Warning
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.content.ContextCompat
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.meshmsg.app.ui.theme.HamyarColors as Hy

private fun now() = System.currentTimeMillis() / 1000

private fun ago(t: Long): String {
    val m = ((now() - t) / 60).coerceAtLeast(0)
    return when {
        m < 1 -> "همین حالا"
        m < 60 -> "${fa(m)} دقیقه پیش"
        m < 1440 -> "${fa(m / 60)} ساعت پیش"
        else -> "${fa(m / 1440)} روز پیش"
    }
}

private fun cardError(e: String?) = when (e) {
    "expired" -> "کد قدیمی است (ساعت گوشی دانشجو را بررسی کنید)"
    "card_off" -> "این کارت غیرفعال شده است"
    "bad_token", "no_card" -> "کارت نامعتبر است"
    "other_university" -> "کارت دانشگاه دیگری است"
    "bad_code" -> "این کد، کارت همیار نیست"
    "not_checker" -> "شما دسترسی بررسی کارت ندارید"
    "nfc_not_hamyar" -> "این کارت، کارت همیار نیست"
    else -> e ?: "خطا"
}

// ------------------------------------------------------------------ my card

@Composable
fun StudentCardScreen(api: HamyarApi, nav: HamyarNav) {
    val scope = rememberCoroutineScope()
    val ctx = LocalContext.current
    var card by remember { mutableStateOf(api.cachedCard) }
    var error by remember { mutableStateOf<String?>(null) }
    var showQr by rememberSaveable { mutableStateOf(false) }
    var askRekey by remember { mutableStateOf(false) }
    var tick by remember { mutableLongStateOf(now()) }
    LaunchedEffect(Unit) {
        runCatching { api.card() }.onSuccess { card = it; error = null }.onFailure { error = errCode(it) }
    }
    LaunchedEffect(Unit) { while (true) { delay(1000); tick = now() } }
    val nfc = remember { NfcAdapter.getDefaultAdapter(ctx) }
    val c = card
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        HyHeader("کارت دانشجویی", c?.university)
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            ErrorLine(error)
            if (error == "module_off") return@Column
            if (c == null) {
                if (error == null) Text("در حال دریافت کارت…", color = Hy.ink600)
                return@Column
            }
            // the card itself
            Column(
                Modifier.fillMaxWidth().clip(RoundedCornerShape(28.dp))
                    .background(Brush.linearGradient(listOf(Color(0xFF1F3B2A), Hy.brand700, Color(0xFF0F7A66)))).padding(20.dp),
                verticalArrangement = Arrangement.spacedBy(16.dp),
            ) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    Box(Modifier.size(48.dp).background(Hy.amber400, RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                        Icon(Icons.Filled.Home, null, tint = Hy.ink900)
                    }
                    Column(Modifier.weight(1f)) {
                        Text(c.university, color = Color.White, fontWeight = FontWeight.ExtraBold, fontSize = 16.sp)
                        Text("کارت مجازی همیار", color = Color.White.copy(alpha = .7f), fontSize = 12.sp)
                    }
                    if (c.status == "active") Badge("معتبر", Color(0xFFA7F3D0), Color.White.copy(alpha = .12f))
                    else Badge("غیرفعال", Color(0xFFFECDD3), Color.White.copy(alpha = .12f))
                }
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                    Column(Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        Text(c.name, color = Color.White, fontWeight = FontWeight.ExtraBold, fontSize = 22.sp)
                        Text(listOfNotNull(c.field, c.degree).joinToString("، ").ifEmpty { "دانشجو" }, color = Color.White.copy(alpha = .75f), fontSize = 13.sp)
                        c.studentCode?.let { Text(it.chunked(3).joinToString(" "), color = Hy.amber300, fontFamily = FontFamily.Monospace, fontSize = 18.sp, fontWeight = FontWeight.Bold) }
                    }
                    RemoteImage(c.photoUrl, Modifier.size(76.dp, 92.dp).clip(RoundedCornerShape(16.dp)).background(Color.White.copy(alpha = .15f))) {
                        Icon(Icons.Filled.Person, null, tint = Color.White, modifier = Modifier.size(36.dp).align(Alignment.Center))
                    }
                }
                c.dormitory?.let {
                    Text("خوابگاه: $it", color = Color.White.copy(alpha = .8f), fontSize = 12.sp,
                        modifier = Modifier.fillMaxWidth().background(Color.Black.copy(alpha = .18f), RoundedCornerShape(16.dp)).padding(horizontal = 14.dp, vertical = 10.dp))
                }
            }
            if (c.status != "active") {
                Note("کارت شما غیرفعال است. برای فعال شدن با دفتر دانشگاه تماس بگیرید.", Hy.rose700, Hy.rose50)
            } else {
                // NFC
                HyCard {
                    Column(Modifier.fillMaxWidth(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(8.dp)) {
                        Box(Modifier.size(96.dp).border(6.dp, Hy.brand100, CircleShape).padding(6.dp).background(Hy.brand700, CircleShape), contentAlignment = Alignment.Center) {
                            Text(")))", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Black)
                        }
                        when {
                            nfc == null -> {
                                Text("این گوشی NFC ندارد", fontWeight = FontWeight.ExtraBold, fontSize = 16.sp)
                                Text("از کد QR استفاده کنید.", fontSize = 13.sp, color = Hy.ink600)
                            }
                            !nfc.isEnabled -> {
                                Text("NFC خاموش است", fontWeight = FontWeight.ExtraBold, fontSize = 16.sp)
                                Text("برای کارت‌خوان‌ها NFC را از تنظیمات گوشی روشن کنید، یا کد QR را نشان دهید.", fontSize = 13.sp, color = Hy.ink600)
                                HyPill("تنظیمات NFC") { runCatching { ctx.startActivity(android.content.Intent(android.provider.Settings.ACTION_NFC_SETTINGS)) } }
                            }
                            else -> {
                                Text("گوشی را به کارت‌خوان نزدیک کنید", fontWeight = FontWeight.ExtraBold, fontSize = 16.sp)
                                Text("در ورودی خوابگاه، کتابخانه و سلف. قفل گوشی باید باز باشد.", fontSize = 13.sp, color = Hy.ink600)
                            }
                        }
                    }
                }
                // QR
                HyCard {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(Icons.Filled.AccountBox, Hy.ink900, Hy.sand100, 52.dp)
                        Column(Modifier.weight(1f)) {
                            Text("کد QR", fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                            Text("برای کارت‌خوان‌های بدون NFC. هر ${fa(CardCodes.WINDOW_SEC)} ثانیه عوض می‌شود.", fontSize = 12.sp, color = Hy.ink600, lineHeight = 20.sp)
                        }
                        HyPill(if (showQr) "بستن" else "نمایش") { showQr = !showQr }
                    }
                    if (showQr) {
                        val code = remember(tick / CardCodes.WINDOW_SEC, c) { CardCodes.current(c) }
                        if (code != null) {
                            val img = remember(code) { qrBitmap(code) }
                            Image(img, "کد QR کارت", Modifier.fillMaxWidth().padding(horizontal = 24.dp).height(260.dp))
                            Text("${fa(CardCodes.secondsLeft(tick))} ثانیه تا کد بعدی", fontSize = 12.sp, color = Hy.ink600, modifier = Modifier.align(Alignment.CenterHorizontally))
                        } else Text("کلید کارت روی این گوشی نیست؛ یک بار با اینترنت باز کنید.", color = Hy.rose600, fontSize = 12.sp)
                    }
                }
            }
            if (c.isChecker) {
                HyCard(onClick = nav.openCheck) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(Icons.Filled.Search, Hy.violet600, Hy.violet50)
                        Column(Modifier.weight(1f)) {
                            Text("بررسی کارت دانشجوها", fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                            Text("با NFC یا دوربین؛ هر بررسی برای دانشجو ثبت می‌شود.", fontSize = 12.sp, color = Hy.ink600)
                        }
                    }
                }
            }
            HyCard {
                Text("آخرین استفاده‌ها", fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                if (c.uses.isEmpty()) Text("هنوز کارت شما خوانده نشده.", fontSize = 13.sp, color = Hy.ink400)
                c.uses.forEach { u ->
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(if (u.result == "ok") Icons.Filled.CheckCircle else Icons.Filled.Warning, if (u.result == "ok") Hy.violet600 else Hy.rose600, if (u.result == "ok") Hy.violet50 else Hy.rose50, 40.dp)
                        Column(Modifier.weight(1f)) {
                            Text(u.place ?: "بررسی کارت", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                            Text("${ago(u.at)} · ${u.via.uppercase()}${if (u.result != "ok") " · " + cardError(u.result) else ""}", fontSize = 12.sp, color = Hy.ink600)
                        }
                    }
                }
            }
            TextButton(onClick = { askRekey = true }, modifier = Modifier.fillMaxWidth()) {
                Text("گوشی قبلی‌ام گم شده (کارت روی آن باطل شود)", color = Hy.ink600, fontSize = 13.sp)
            }
        }
    }
    if (askRekey) {
        AlertDialog(
            onDismissRequest = { askRekey = false },
            title = { Text("کلید تازه برای کارت") },
            text = { Text("کارت روی هر گوشی دیگری که با حساب شما وارد شده فوراً باطل می‌شود و فقط این گوشی کارت معتبر دارد.") },
            confirmButton = {
                TextButton(onClick = {
                    askRekey = false
                    scope.launch { runCatching { api.card(rekey = true) }.onSuccess { card = it }.onFailure { error = errCode(it) } }
                }) { Text("باطل کن") }
            },
            dismissButton = { TextButton(onClick = { askRekey = false }) { Text("انصراف") } },
        )
    }
}

// ------------------------------------------------------------------ checking cards (admins with card_check)

@Composable
fun CardCheckScreen(api: HamyarApi, onBack: () -> Unit) {
    val ctx = LocalContext.current
    val scope = rememberCoroutineScope()
    var place by rememberSaveable { mutableStateOf(api.store.getString("check_place", "") ?: "") }
    var result by remember { mutableStateOf<CheckedStudent?>(null) }
    var busy by remember { mutableStateOf(false) }
    var camera by rememberSaveable { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    val camPerm = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { camera = it }

    fun check(code: String?, via: String) {
        if (busy) return
        if (code == null) { result = CheckedStudent(false, "nfc_not_hamyar", null, null, null, null, null); return }
        busy = true; error = null
        api.store.edit().putString("check_place", place).apply()
        scope.launch {
            runCatching { api.checkCard(code, via, place.trim().ifEmpty { null }) }.onSuccess { result = it }.onFailure { error = errCode(it) }
            busy = false
        }
    }
    val nfcOn = NfcCardReader { check(it, "nfc") }
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        HyHeader("بررسی کارت دانشجویی", "کارت را به این گوشی نزدیک کنید یا کد QR آن را بخوانید.", onBack)
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            OutlinedTextField(place, { place = it.take(80) }, label = { Text("محل (مثلاً «ورودی خوابگاه ب»)") }, singleLine = true, modifier = Modifier.fillMaxWidth())
            val r = result
            if (r != null) {
                val ok = r.ok
                Column(
                    Modifier.fillMaxWidth().clip(CardShape).background(if (ok) Hy.emerald50 else Hy.rose50).border(2.dp, if (ok) Hy.emerald700 else Hy.rose600, CardShape).padding(16.dp),
                    verticalArrangement = Arrangement.spacedBy(10.dp),
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                        if (ok) RemoteImage(r.photoUrl, Modifier.size(84.dp, 100.dp).clip(RoundedCornerShape(16.dp)).background(Hy.sand200)) {
                            Icon(Icons.Filled.Person, null, tint = Hy.ink400, modifier = Modifier.size(40.dp).align(Alignment.Center))
                        }
                        Column(Modifier.weight(1f)) {
                            Text(if (ok) "✓ معتبر" else "✗ ${cardError(r.error)}", color = if (ok) Hy.emerald700 else Hy.rose700, fontWeight = FontWeight.Black, fontSize = 20.sp)
                            if (ok) {
                                Text(r.name.orEmpty(), fontWeight = FontWeight.ExtraBold, fontSize = 18.sp, color = Hy.ink900)
                                r.studentCode?.let { Text(fa(it), fontSize = 14.sp, color = Hy.ink600) }
                                Text(listOfNotNull(r.field, r.dormitory?.let { "خوابگاه $it" }).joinToString(" · "), fontSize = 12.sp, color = Hy.ink600)
                            }
                        }
                    }
                    if (ok) Text("عکس را با چهرهٔ دانشجو مقایسه کنید.", fontSize = 12.sp, color = Hy.emerald700)
                }
            }
            ErrorLine(error)
            HyCard {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    IconTile(Icons.Filled.Lock, if (nfcOn) Hy.brand700 else Hy.ink400, if (nfcOn) Hy.brand50 else Hy.sand100)
                    Text(
                        if (nfcOn) "NFC آماده است: گوشی دانشجو را پشت این گوشی بگیرید." else "NFC این گوشی خاموش است یا ندارد؛ از دوربین استفاده کنید.",
                        fontSize = 13.sp, color = Hy.ink900, modifier = Modifier.weight(1f),
                    )
                }
            }
            if (camera) {
                QrScanner(Modifier.fillMaxWidth().height(300.dp).clip(CardShape)) { text ->
                    if (CardCodes.looksLikeCode(text)) check(text.trim(), "qr") else result = CheckedStudent(false, "bad_code", null, null, null, null, null)
                }
                HyPill("بستن دوربین", bg = Hy.sand200, fg = Hy.ink900) { camera = false }
            } else {
                HyButton("خواندن کد QR با دوربین", Modifier.fillMaxWidth(), busy = busy) {
                    if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.CAMERA) == PackageManager.PERMISSION_GRANTED) camera = true
                    else camPerm.launch(Manifest.permission.CAMERA)
                }
            }
        }
    }
}

// ------------------------------------------------------------------ my classes (attendance from presence)

@Composable
fun ClassesScreen(api: HamyarApi, onBack: () -> Unit) {
    var data by remember { mutableStateOf<Classes?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) { runCatching { api.classes() }.onSuccess { data = it; error = null }.onFailure { error = errCode(it) } }
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        HyHeader("کلاس‌های من", "حضور سر کلاس از نودهای ماوای کلاس", onBack)
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            ErrorLine(error)
            val d = data ?: return@Column
            if (!d.presenceOn) {
                Note("«حضور در دانشگاه» خاموش است، پس حضور سر کلاس شما ثبت نمی‌شود و «نامعلوم» می‌ماند (نه غایب). از برگهٔ حضور روشنش کنید.", Hy.amber700, Hy.amber50)
            }
            if (d.classes.isEmpty()) HyCard { Text("کلاسی برای شما تعریف نشده است.", color = Hy.ink400, fontSize = 13.sp) }
            d.classes.forEach { k ->
                HyCard {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(Icons.Filled.DateRange, Hy.brand700, Hy.brand50)
                        Column(Modifier.weight(1f)) {
                            Text(k.title, fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                            Text(listOfNotNull(k.teacher, k.place).joinToString(" · "), fontSize = 12.sp, color = Hy.ink600)
                        }
                    }
                    Text(fa(k.slots.joinToString("، ")), fontSize = 12.sp, color = Hy.ink600)
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        listOf("present" to "حاضر", "partial" to "بخشی", "absent" to "غایب", "unknown" to "نامعلوم").forEach { (s, label) ->
                            Column(Modifier.weight(1f).background(statusBg(s), RoundedCornerShape(14.dp)).padding(8.dp), horizontalAlignment = Alignment.CenterHorizontally) {
                                Text(fa(k.counts[s] ?: 0), fontWeight = FontWeight.Black, fontSize = 18.sp, color = statusFg(s))
                                Text(label, fontSize = 11.sp, color = Hy.ink600)
                            }
                        }
                    }
                    k.history.take(8).forEach { m ->
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(fa(m.day), fontSize = 12.sp, color = Hy.ink600, modifier = Modifier.weight(1f))
                            Text(
                                if (m.live) "در حال برگزاری" else "${statusLabel(m.status)}${if (m.minutes > 0) " · ${fa(m.minutes)} دقیقه" else ""}",
                                fontSize = 12.sp, fontWeight = FontWeight.Bold, color = if (m.live) Hy.sky600 else statusFg(m.status),
                            )
                        }
                    }
                }
            }
            Text("حاضر یعنی دست‌کم نیمی از زمان کلاس، گوشی شما کنار نود کلاس دیده شده. مکان دقیق ثبت نمی‌شود.", fontSize = 12.sp, color = Hy.ink400, lineHeight = 20.sp)
        }
    }
}

private fun statusLabel(s: String) = when (s) { "present" -> "حاضر"; "partial" -> "بخشی از کلاس"; "absent" -> "غایب"; else -> "نامعلوم" }
private fun statusFg(s: String) = when (s) { "present" -> Hy.emerald700; "partial" -> Hy.amber700; "absent" -> Hy.rose700; else -> Hy.ink600 }
private fun statusBg(s: String) = when (s) { "present" -> Hy.emerald50; "partial" -> Hy.amber50; "absent" -> Hy.rose50; else -> Hy.sand100 }
