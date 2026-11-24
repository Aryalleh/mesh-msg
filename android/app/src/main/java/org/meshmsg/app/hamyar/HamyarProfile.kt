package org.meshmsg.app.hamyar

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.AccountBox
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Email
import androidx.compose.material.icons.filled.Favorite
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Lock
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.meshmsg.app.ui.theme.HamyarColors as Hy

/** The Mava side of the Hamyar app, done by the shell (it owns the core). */
class MavaHooks(
    /** This phone's Mava account (hash), or null while the core is starting. */
    val myAccount: () -> String?,
    /** Links the account now (signs with the account key); returns the linked account. */
    val link: suspend () -> Result<String>,
    /** Writes the sealed history backup to the file (Core.exportHistory). */
    val exportHistory: suspend (String, java.io.File) -> Result<Unit>,
    /** Installs a downloaded history backup before the core first opens (Meshcore.restoreHistory). */
    val restoreHistory: suspend (java.io.File, String) -> Result<Unit>,
    val cacheDir: java.io.File,
)

// ------------------------------------------------------------------ profile

@Composable
fun HamyarProfile(api: HamyarApi, nav: HamyarNav, onBack: () -> Unit) {
    var me by remember { mutableStateOf(api.cachedMe) }
    LaunchedEffect(Unit) { runCatching { api.me() }.onSuccess { me = it } }
    val m = me
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        HyHeader("پروفایل", m?.university, onBack) {
            Row(Modifier.padding(horizontal = 12.dp), verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(14.dp)) {
                Avatar(m?.photoUrl, m?.firstName ?: "", 64.dp)
                Column {
                    Text(m?.name ?: "", color = Color.White, fontWeight = FontWeight.ExtraBold, fontSize = 18.sp)
                    Text(listOfNotNull(m?.studentCode?.let(::fa), m?.phone?.let(::fa)).joinToString(" · "), color = Color.White.copy(alpha = .7f), fontSize = 13.sp)
                }
            }
        }
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            HyCard(padding = 6.dp) {
                Row_(Icons.Filled.Person, "ویرایش پروفایل و مدارک", Hy.brand700, Hy.brand50) { nav.web("/profile") }
                Row_(Icons.Filled.AccountBox, "پرداخت‌ها و کارت‌های بانکی", Hy.sky600, Hy.sky50) { nav.web("/payments") }
                if (m == null || m.has("surveys")) Row_(Icons.Filled.Star, "نظرسنجی‌ها", Hy.violet600, Hy.violet50) { nav.web("/surveys") }
                if (m == null || m.has("presence")) Row_(Icons.Filled.DateRange, "کلاس‌های من و حضور", Hy.brand700, Hy.brand50, nav.openClasses)
                if (m == null || m.has("clinic")) Row_(Icons.Filled.Favorite, "درمانگاه (نوبت‌دهی)", Hy.rose600, Hy.rose50) { nav.web("/clinic") }
                Row_(Icons.Filled.Share, "کانون‌ها و انجمن‌ها", Hy.amber700, Hy.amber50) { nav.web("/clubs") }
                if (m == null || m.has("tickets")) {
                    Row_(Icons.Filled.Email, "پشتیبانی", Hy.ink900, Hy.sand100) { nav.web("/tickets") }
                    Row_(Icons.Filled.Info, "پرسش‌های متداول", Hy.ink900, Hy.sand100) { nav.web("/faq") }
                }
            }
            SectionTitle("پیام‌رسان ماوا")
            HyCard(padding = 6.dp) {
                Row_(Icons.Filled.Lock, "حساب ماوا و تاریخچهٔ گفتگو", Hy.sky600, Hy.sky50, nav.openMavaAccount)
                Row_(Icons.Filled.Refresh, "شبکه (بلوتوث، برد ماوا)", Hy.brand700, Hy.brand50, nav.openNetwork)
                Row_(Icons.Filled.Settings, "تنظیمات ماوا", Hy.ink900, Hy.sand100, nav.openMavaSettings)
            }
            TextButton(onClick = nav.logout, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text("خروج از همیار", color = Hy.rose600, fontWeight = FontWeight.Bold)
            }
            Text("بخش‌هایی که هنوز در اپ نیستند در سایت دانشگاه باز می‌شوند (بدون ورود دوباره).", fontSize = 12.sp, color = Hy.ink400, lineHeight = 20.sp)
        }
    }
}

@Composable
private fun Row_(icon: ImageVector, label: String, fg: Color, bg: Color, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().heightIn(min = 56.dp).clickable(role = Role.Button, onClick = onClick).padding(horizontal = 10.dp, vertical = 6.dp),
        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        IconTile(icon, fg, bg, 40.dp)
        Text(label, fontWeight = FontWeight.Bold, fontSize = 14.sp, color = Hy.ink900, modifier = Modifier.weight(1f))
        Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, null, tint = Hy.ink400)
    }
}

// ------------------------------------------------------------------ Mava account and private history

private const val MIN_PASS = 8

@Composable
fun MavaAccountScreen(api: HamyarApi, hooks: MavaHooks, onBack: () -> Unit) {
    val scope = rememberCoroutineScope()
    var info by remember { mutableStateOf<MavaInfo?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var pass by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var done by remember { mutableStateOf<String?>(null) }
    suspend fun load() { runCatching { api.mava() }.onSuccess { info = it; error = null }.onFailure { error = errCode(it) } }
    LaunchedEffect(Unit) { load() }
    val i = info
    val mine = hooks.myAccount()
    val linked = i?.account != null && i.account == mine
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        HyHeader("پیام‌رسان ماوا", "پیام‌رسان بدون اینترنت دانشگاه، داخل همیار", onBack)
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(14.dp)) {
            ErrorLine(error)
            HyCard {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    IconTile(Icons.Filled.Lock, Hy.sky600, Hy.sky50, 52.dp)
                    Column(Modifier.weight(1f)) {
                        Text(if (linked) "حساب ماوا متصل است" else "حساب ماوا هنوز متصل نیست", fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                        Text(mine?.let { "mava:${it.take(6)}…${it.takeLast(4)}" } ?: "در حال راه‌اندازی…", fontSize = 12.sp, color = Hy.ink400)
                    }
                    if (linked) Badge("● فعال", Hy.emerald700, Hy.emerald50)
                }
                i?.relay?.let {
                    Row(Modifier.fillMaxWidth().background(Hy.sand50, RoundedCornerShape(16.dp)).padding(12.dp)) {
                        Text("سرور دانشگاه", fontSize = 12.sp, color = Hy.ink600, modifier = Modifier.weight(1f))
                        Text(it.removePrefix("https://"), fontSize = 13.sp, fontWeight = FontWeight.Bold)
                    }
                }
                if (!linked && mine != null) {
                    HyButton("اتصال حساب", Modifier.fillMaxWidth(), busy = busy) {
                        busy = true
                        scope.launch { hooks.link().onFailure { error = errCode(it) }; load(); busy = false }
                    }
                }
                Text("کلید حساب ماوا فقط روی همین گوشی ساخته و نگه داشته می‌شود؛ همیار فقط شناسهٔ آن را می‌داند.", fontSize = 12.sp, color = Hy.ink600, lineHeight = 20.sp)
            }
            SectionTitle("تاریخچهٔ خصوصی گفتگو")
            HyCard {
                if (i != null && !i.historyOn) {
                    Text("دانشگاه شما نگه‌داری تاریخچه را روشن نکرده است.", fontSize = 13.sp, color = Hy.ink600)
                } else if (i != null) {
                    Text(
                        "دانشگاه پیام‌های رمزنگاری‌شده را ${fa(i.keepDays ?: 0)} روز نگه می‌دارد. پشتیبان با عبارت عبور خودت رمز می‌شود؛ همیار و دانشگاه نمی‌توانند آن را باز کنند.",
                        fontSize = 13.sp, color = Hy.ink600, lineHeight = 22.sp,
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        Stat(i.backupSize?.let { "%.1f MB".format(java.util.Locale.US, it / 1_048_576.0) } ?: "—", "حجم", Modifier.weight(1f))
                        Stat("${fa(i.backupMaxMb)} MB", "سقف", Modifier.weight(1f))
                        Stat(i.backupAt?.take(10)?.let(::fa) ?: "ندارد", "آخرین", Modifier.weight(1f))
                    }
                    OutlinedTextField(
                        pass, { pass = it }, label = { Text("عبارت عبور پشتیبان (دست‌کم ${fa(MIN_PASS)} حرف)") }, singleLine = true,
                        visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                        modifier = Modifier.fillMaxWidth(),
                    )
                    HyButton("پشتیبان‌گیری الان", Modifier.fillMaxWidth(), enabled = pass.length >= MIN_PASS && mine != null, busy = busy) {
                        busy = true; done = null; error = null
                        scope.launch {
                            val r = runCatching {
                                val f = java.io.File(hooks.cacheDir, "mava-history.bin")
                                hooks.exportHistory(pass, f).getOrThrow()
                                try {
                                    if (f.length() > i.backupMaxMb * 1_048_576L) throw HamyarError("too_large", 413)
                                    api.uploadBackup(f, withContext(Dispatchers.IO) { sha256(f) })
                                } finally { f.delete() }
                            }
                            r.onSuccess { done = "پشتیبان ذخیره شد. عبارت عبور را جایی امن نگه دارید؛ بدون آن باز نمی‌شود."; pass = "" }
                                .onFailure { error = if (it is HamyarError) it.code else (it.message ?: "network") }
                            load(); busy = false
                        }
                    }
                    done?.let { Note(it, Hy.emerald700, Hy.emerald50) }
                    Text("بازگرداندن: روی گوشی تازه، بعد از ورود به همیار پرسیده می‌شود.", fontSize = 12.sp, color = Hy.ink400)
                }
            }
        }
    }
}

@Composable
private fun Stat(value: String, label: String, modifier: Modifier) {
    Column(modifier.background(Hy.sand50, RoundedCornerShape(16.dp)).padding(10.dp), horizontalAlignment = Alignment.CenterHorizontally) {
        Text(value, fontWeight = FontWeight.ExtraBold, fontSize = 14.sp, color = Hy.ink900)
        Text(label, fontSize = 11.sp, color = Hy.ink600)
    }
}

private fun sha256(f: java.io.File): String {
    val md = java.security.MessageDigest.getInstance("SHA-256")
    f.inputStream().use { s -> val b = ByteArray(64 * 1024); while (true) { val n = s.read(b); if (n < 0) break; md.update(b, 0, n) } }
    return md.digest().joinToString("") { "%02x".format(it) }
}

// ------------------------------------------------------------------ first start: login, then restore or start fresh

/**
 * Shown until the Mava core first opens: Hamyar login, then (when the university keeps history and this user has a
 * backup) the offer to restore the chats with the backup's passphrase. [done] starts the core (a new account, or the
 * restored one).
 */
@Composable
fun HamyarStart(api: HamyarApi, hooks: MavaHooks, onQuit: () -> Unit, done: () -> Unit) {
    var loggedIn by remember { mutableStateOf(api.loggedIn) }
    if (!loggedIn) {
        Box(Modifier.fillMaxSize().background(Hy.sand100)) { HamyarLogin(api, onQuit) { loggedIn = true } }
        return
    }
    val scope = rememberCoroutineScope()
    var info by remember { mutableStateOf<MavaInfo?>(null) }
    var checked by remember { mutableStateOf(false) }
    var pass by remember { mutableStateOf("") }
    var busy by remember { mutableStateOf(false) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        info = runCatching { api.mava() }.getOrNull()
        checked = true
        if (info?.backupSize == null) done()
    }
    Column(Modifier.fillMaxSize().background(Hy.sand100).verticalScroll(rememberScrollState())) {
        Column(
            Modifier.fillMaxWidth().background(Hy.brand900, RoundedCornerShape(bottomStart = 28.dp, bottomEnd = 28.dp)).statusBarsPadding().padding(24.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Text("بازگرداندن گفتگوها", color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.ExtraBold)
            Text("برای حساب شما یک پشتیبان تاریخچهٔ ماوا هست. با عبارت عبور آن، گفتگوها و حساب ماوای قبلی‌تان برمی‌گردد.", color = Color.White.copy(alpha = .75f), fontSize = 14.sp, lineHeight = 24.sp)
        }
        if (!checked || info?.backupSize == null) return@Column
        Column(Modifier.padding(20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            HyCard {
                Text("پشتیبان ${info?.backupAt?.take(10)?.let(::fa) ?: ""}", fontWeight = FontWeight.Bold)
                OutlinedTextField(
                    pass, { pass = it }, label = { Text("عبارت عبور پشتیبان") }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password),
                    modifier = Modifier.fillMaxWidth(),
                )
                error?.let { Text(it, color = Hy.rose600, fontSize = 13.sp) }
                HyButton("بازگرداندن", Modifier.fillMaxWidth(), enabled = pass.length >= MIN_PASS, busy = busy) {
                    busy = true; error = null
                    scope.launch {
                        val f = java.io.File(hooks.cacheDir, "mava-restore.bin")
                        val r = runCatching {
                            api.downloadBackup(f)
                            hooks.restoreHistory(f, pass).getOrThrow()
                        }
                        f.delete()
                        busy = false
                        r.onSuccess { done() }.onFailure {
                            error = when {
                                it.message.orEmpty().startsWith("passphrase:") -> "عبارت عبور درست نیست."
                                it.message.orEmpty().startsWith("format:") -> "فایل پشتیبان خراب است."
                                else -> "بازگرداندن نشد: ${(it as? HamyarError)?.code ?: it.message}"
                            }
                        }
                    }
                }
            }
            TextButton(onClick = done, modifier = Modifier.fillMaxWidth().heightIn(min = 48.dp)) {
                Text("شروع با حساب تازه (پشتیبان دست نمی‌خورد)", color = Hy.ink600)
            }
        }
    }
}
