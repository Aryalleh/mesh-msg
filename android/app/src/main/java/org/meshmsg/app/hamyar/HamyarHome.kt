package org.meshmsg.app.hamyar

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.filled.AccountBox
import androidx.compose.material.icons.filled.CheckCircle
import androidx.compose.material.icons.filled.DateRange
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.LocationOn
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.filled.Star
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.meshmsg.app.ui.theme.HamyarColors as Hy

/** What the Hamyar screens ask the app shell to do (MainActivity). */
class HamyarNav(
    /** Opens a page of the university's site, logged in (one-time link), in the browser. */
    val web: (String) -> Unit,
    val openProfile: () -> Unit,
    val openGroup: (String) -> Unit,
    val openClasses: () -> Unit,
    val openCheck: () -> Unit,
    val openTrips: () -> Unit,
    /** A Mava trip card link (meshtrip:…): opens Mava's trips screen with it. */
    val openMavaTrip: (String) -> Unit,
    val openMavaSettings: () -> Unit,
    val openNetwork: () -> Unit,
    val openMavaAccount: () -> Unit,
    val logout: () -> Unit,
)

// ------------------------------------------------------------------ home (the website's trips home)

@Composable
fun HamyarHome(api: HamyarApi, nav: HamyarNav) {
    var me by remember { mutableStateOf(api.cachedMe) }
    var data by remember { mutableStateOf(api.cachedHome) }
    var error by remember { mutableStateOf<String?>(null) }
    var query by rememberSaveable { mutableStateOf("") }
    var filter by rememberSaveable { mutableStateOf("all") }
    LaunchedEffect(Unit) {
        runCatching { api.me() }.onSuccess { me = it }.onFailure { error = errCode(it) }
        runCatching { api.home() }.onSuccess { data = it; error = null }.onFailure { error = errCode(it) }
    }
    val trips = data?.trips.orEmpty()
    val next = trips.firstOrNull { val r = it.registration; r != null && r.status == "pending" && (r.price ?: it.price ?: 0) > 0 }
    val shown = trips.filter { t ->
        (query.isBlank() || listOfNotNull(t.name, t.location).any { it.contains(query.trim(), ignoreCase = true) }) &&
            when (filter) {
                "open" -> t.registration == null && !t.isFull
                "mine" -> t.registration != null
                "inst" -> t.installments > 1
                else -> true
            }
    }
    LazyColumn(Modifier.fillMaxSize().background(Hy.sand100), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
        item {
            HomeHeader(me, next, nav)
        }
        item { ErrorBox(error) }
        if (trips.isNotEmpty()) {
            item {
                Column(Modifier.padding(start = 20.dp, end = 20.dp, top = 20.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
                    Row(
                        Modifier.fillMaxWidth().heightIn(min = 48.dp).background(Color.White, RoundedCornerShape(16.dp))
                            .border(1.dp, Hy.sand200, RoundedCornerShape(16.dp)).padding(horizontal = 14.dp),
                        verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp),
                    ) {
                        Icon(Icons.Filled.Search, null, tint = Hy.ink400)
                        Box(Modifier.weight(1f)) {
                            if (query.isEmpty()) Text("جستجوی اردو یا مقصد…", color = Hy.ink400, fontSize = 14.sp)
                            BasicTextField(query, { query = it }, singleLine = true, textStyle = MaterialTheme.typography.bodyMedium.copy(color = Hy.ink900), modifier = Modifier.fillMaxWidth())
                        }
                    }
                    Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        listOf(
                            "all" to "همه ${fa(trips.size)}", "open" to "قابل ثبت‌نام",
                            "mine" to "ثبت‌نام‌های من ${fa(trips.count { it.registration != null })}", "inst" to "اقساطی",
                        ).forEach { (k, label) -> Chip(label, filter == k) { filter = k } }
                    }
                }
            }
            items(shown, key = { it.id }) { t -> TripCard(t, nav) }
            if (shown.isEmpty()) item {
                HyCard(Modifier.padding(20.dp)) { Text("اردویی با این فیلتر پیدا نشد.", color = Hy.ink400, modifier = Modifier.align(Alignment.CenterHorizontally)) }
            }
        }
        val surveys = data?.surveys.orEmpty()
        if (surveys.isNotEmpty()) {
            item { Box(Modifier.padding(horizontal = 20.dp)) { SectionTitle("نظرسنجی‌های باز") } }
            items(surveys, key = { "s" + it.id }) { s ->
                HyCard(Modifier.padding(horizontal = 20.dp, vertical = 6.dp), padding = 12.dp) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(if (s.done) Icons.Filled.CheckCircle else Icons.Filled.Star, if (s.done) Hy.emerald700 else Hy.violet600, if (s.done) Hy.emerald50 else Hy.violet50, 48.dp)
                        Column(Modifier.weight(1f)) {
                            Text(s.name, fontWeight = FontWeight.ExtraBold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
                            Text("${fa(s.questions)} سؤال – ${fa(s.minutes)} دقیقه", fontSize = 11.sp, color = Hy.ink400)
                        }
                        if (s.done) Badge("شرکت کرده‌اید", Hy.emerald700, Hy.emerald50)
                        else HyPill("شروع", bg = Hy.ink900, fg = Color.White) { nav.web("/survey/${s.id}") }
                    }
                }
            }
        }
        if (data != null && trips.isEmpty() && surveys.isEmpty()) item {
            Column(Modifier.fillMaxWidth().padding(top = 40.dp, start = 20.dp, end = 20.dp), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.spacedBy(6.dp)) {
                IconTile(Icons.Filled.DateRange, Hy.ink400, Color.White, 80.dp)
                Text("در حال حاضر اردو یا نظرسنجی فعالی نیست.", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                Text("به محض باز شدن ثبت‌نام، در بله خبرتان می‌کنیم.", fontSize = 12.sp, color = Hy.ink400)
            }
        }
    }
}

@Composable
private fun HomeHeader(me: HamyarMe?, next: HomeTrip?, nav: HamyarNav) {
    Column(
        Modifier.fillMaxWidth().background(Brush.verticalGradient(listOf(Hy.brand900, Color(0xFF0D3B33))), RoundedCornerShape(bottomStart = 28.dp, bottomEnd = 28.dp))
            .statusBarsPadding().padding(start = 20.dp, end = 20.dp, top = 16.dp, bottom = 24.dp),
        verticalArrangement = Arrangement.spacedBy(20.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
            Box(Modifier.size(44.dp).background(Color.White.copy(alpha = .1f), RoundedCornerShape(16.dp)).border(1.dp, Color.White.copy(alpha = .15f), RoundedCornerShape(16.dp)), contentAlignment = Alignment.Center) {
                Icon(Icons.Filled.Home, null, tint = Hy.amber300)
            }
            Column(Modifier.weight(1f)) {
                Text("همیار · ${me?.slug ?: ""}.hamyared.ir", color = Color.White.copy(alpha = .6f), fontSize = 11.sp, maxLines = 1)
                Text(me?.university ?: "همیار دانشجو", color = Color.White, fontWeight = FontWeight.Bold, fontSize = 14.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            Box(Modifier.clip(RoundedCornerShape(50)).clickable(role = Role.Button, onClick = nav.openProfile)) {
                Avatar(me?.photoUrl, me?.firstName ?: "", 44.dp)
            }
        }
        Column {
            Text("سلام، ${me?.name ?: ""} 👋", color = Color.White.copy(alpha = .7f), fontSize = 14.sp)
            Text("کجا می‌خوای بری این ترم؟", color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.ExtraBold)
        }
        if (next != null) {
            val r = next.registration!!
            Row(
                Modifier.fillMaxWidth().background(Color.White, RoundedCornerShape(16.dp)).padding(12.dp),
                verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp),
            ) {
                IconTile(Icons.Filled.DateRange, Hy.amber700, Hy.amber50)
                Column(Modifier.weight(1f)) {
                    Text("در انتظار پرداخت · ${next.name}", fontSize = 11.sp, color = Hy.ink400, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text("${money(r.price ?: next.price)} ریال", fontSize = 14.sp, fontWeight = FontWeight.Bold, color = Hy.ink900)
                }
                HyButton("پرداخت") { nav.web("/pay/${r.memberId}") }
            }
        }
    }
}

@Composable
private fun Chip(label: String, on: Boolean, click: () -> Unit) {
    Box(
        Modifier.heightIn(min = 40.dp).clip(RoundedCornerShape(50)).background(if (on) Hy.ink900 else Color.White)
            .border(1.dp, if (on) Hy.ink900 else Hy.sand200, RoundedCornerShape(50)).clickable(role = Role.Tab, onClick = click).padding(horizontal = 16.dp, vertical = 9.dp),
        contentAlignment = Alignment.Center,
    ) { Text(label, color = if (on) Color.White else Hy.ink600, fontSize = 12.sp, fontWeight = FontWeight.Bold) }
}

private fun genderLabel(g: String?): String? {
    val v = g?.trim()?.lowercase() ?: return null
    return when (v) {
        "male", "m", "آقا", "برادر", "برادران", "مرد" -> "♂ برادران"
        "female", "f", "خانم", "خواهر", "خواهران", "زن" -> "♀ خواهران"
        else -> null
    }
}

@Composable
private fun TripCard(t: HomeTrip, nav: HamyarNav) {
    val r = t.registration
    val price = r?.price ?: t.price
    val paid = r != null && (r.status == "paid" || price == 0L)
    Column(
        Modifier.padding(horizontal = 20.dp, vertical = 8.dp).fillMaxWidth().clip(CardShape).background(Color.White).border(1.dp, Hy.sand200, CardShape),
    ) {
        Box(Modifier.fillMaxWidth().height(if (t.coverUrl != null) 160.dp else 112.dp)) {
            RemoteImage(t.coverUrl, Modifier.fillMaxSize().background(Brush.linearGradient(listOf(Hy.brand700, Hy.brand900))))
            Box(Modifier.fillMaxSize().background(Brush.verticalGradient(listOf(Color.Black.copy(alpha = .1f), Color.Transparent, Color.Black.copy(alpha = .6f)))))
            Row(Modifier.padding(12.dp).align(Alignment.TopStart), horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                when {
                    paid -> Badge("✓ پرداخت‌شده", Hy.emerald700, Color.White.copy(alpha = .9f))
                    r != null -> Badge("ثبت‌نام شده · پرداخت‌نشده", Hy.rose700, Color.White.copy(alpha = .9f))
                    t.isFull -> Badge("تکمیل ظرفیت", Hy.ink600, Color.White.copy(alpha = .9f))
                    else -> Badge("● ثبت‌نام باز", Hy.brand700, Color.White.copy(alpha = .9f))
                }
            }
            genderLabel(t.gender)?.let { Box(Modifier.padding(12.dp).align(Alignment.TopEnd)) { Badge(it, Color.White, Color.Black.copy(alpha = .4f)) } }
            Column(Modifier.align(Alignment.BottomStart).padding(horizontal = 16.dp, vertical = 12.dp)) {
                Text(t.name, color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.ExtraBold)
                t.date?.let { Text(fa(it), color = Color.White.copy(alpha = .8f), fontSize = 12.sp) }
            }
        }
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
            t.description?.let { Text(it, fontSize = 12.sp, color = Hy.ink600, lineHeight = 22.sp, maxLines = 3, overflow = TextOverflow.Ellipsis) }
            Row(verticalAlignment = Alignment.CenterVertically) {
                Icon(Icons.Filled.LocationOn, null, tint = Hy.ink400, modifier = Modifier.size(16.dp))
                Text(t.location ?: "محل برگزاری مشخص نشده", fontSize = 12.sp, color = Hy.ink600, modifier = Modifier.weight(1f).padding(start = 4.dp))
                t.capacity?.let { Text("ظرفیت ${fa(it)} نفر", fontSize = 12.sp, color = Hy.brand700, fontWeight = FontWeight.Bold) }
            }
            t.deadline?.let { Text("مهلت ثبت‌نام: ${fa(it)}", fontSize = 11.sp, color = Hy.ink400) }
            if (price != null) {
                Column(Modifier.fillMaxWidth().background(Hy.sand50, RoundedCornerShape(16.dp)).border(1.dp, Hy.sand200, RoundedCornerShape(16.dp)).padding(12.dp)) {
                    Text(if (r != null) "مبلغ ثبت‌نام شما" else "قیمت برای شما", fontSize = 11.sp, color = Hy.ink400)
                    Text("${money(price)} ریال", fontSize = 20.sp, fontWeight = FontWeight.ExtraBold, color = Hy.ink900)
                }
            }
            if (t.installments > 1 || t.servant) Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                if (t.installments > 1) Badge("${fa(t.installments)} قسط", Hy.sky600, Hy.sky50)
                if (t.servant) Badge("امکان خادمی", Hy.violet600, Hy.violet50)
            }
            when {
                r?.servantStatus == "pending" -> Note("درخواست خادمی شما در انتظار تأیید مدیر است.", Hy.sky900, Hy.sky50)
                r?.servantStatus == "approved" -> Note("خادمی شما تأیید شد.", Hy.violet800, Hy.violet50)
                r?.servantStatus == "rejected" -> Note("درخواست خادمی پذیرفته نشد؛ ثبت‌نام با قیمت عادی ادامه دارد.", Hy.amber700, Hy.amber50)
            }
            when {
                paid -> Row(
                    Modifier.fillMaxWidth().background(Hy.emerald50, RoundedCornerShape(16.dp)).padding(horizontal = 16.dp, vertical = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Text("✓ صندلی شما قطعی است", fontSize = 12.sp, color = Hy.emerald700, modifier = Modifier.weight(1f))
                    Text("فاکتور", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = Hy.emerald700, modifier = Modifier.clickable { nav.web("/pay/${r!!.memberId}") }.padding(4.dp))
                }
                r != null -> HyButton("پرداخت آنلاین", Modifier.fillMaxWidth()) { nav.web("/pay/${r.memberId}") }
                t.isFull -> Box(Modifier.fillMaxWidth().background(Hy.sand100, RoundedCornerShape(16.dp)).padding(12.dp), contentAlignment = Alignment.Center) {
                    Text("ظرفیت تکمیل شده است", fontSize = 12.sp, fontWeight = FontWeight.Bold, color = Hy.ink600)
                }
                // registration asks for missing profile fields and documents: the website does it
                else -> HyButton("ثبت‌نام", Modifier.fillMaxWidth()) { nav.web("/") }
            }
        }
    }
}

@Composable
fun Note(text: String, fg: Color, bg: Color) {
    Text(text, color = fg, fontSize = 12.sp, lineHeight = 22.sp, modifier = Modifier.fillMaxWidth().background(bg, RoundedCornerShape(16.dp)).padding(12.dp))
}

@Composable
private fun ErrorBox(error: String?) {
    if (error == null || error == "unauthorized") return
    Box(Modifier.padding(start = 20.dp, end = 20.dp, top = 16.dp)) { ErrorLine(error) }
}

// ------------------------------------------------------------------ my trips (registrations, payments, trip groups)

@Composable
fun HamyarTrips(api: HamyarApi, nav: HamyarNav) {
    var trips by remember { mutableStateOf<List<MyTrip>?>(null) }
    var pays by remember { mutableStateOf<List<PaymentRow>>(emptyList()) }
    var groups by remember { mutableStateOf(api.cachedGroups) }
    var error by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        runCatching { api.myTrips() }.onSuccess { trips = it; error = null }.onFailure { error = errCode(it) }
        runCatching { api.payments() }.onSuccess { pays = it }
        runCatching { api.groups() }.onSuccess { groups = it }
    }
    LazyColumn(Modifier.fillMaxSize().background(Hy.sand100), contentPadding = androidx.compose.foundation.layout.PaddingValues(bottom = 24.dp)) {
        item { HyHeader("سفرهای من", "ثبت‌نام‌ها، پرداخت‌ها و گروه «همه با هم هستیم؟»") }
        item { ErrorBox(error) }
        if (groups.isNotEmpty()) {
            item { Box(Modifier.padding(horizontal = 20.dp)) { SectionTitle("گروه سفر (با هم هستیم؟)") } }
            items(groups, key = { "g" + it.tripId }) { g ->
                HyCard(Modifier.padding(horizontal = 20.dp, vertical = 6.dp), padding = 12.dp, onClick = { nav.openGroup(g.tripId) }) {
                    Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                        IconTile(Icons.Filled.Person, Hy.amber700, Hy.amber50)
                        Column(Modifier.weight(1f)) {
                            Text(g.tripName, fontWeight = FontWeight.Bold, fontSize = 14.sp)
                            Text(if (g.joined) "عضو گروه هستید" else "هنوز به گروه نپیوسته‌اید", fontSize = 12.sp, color = if (g.joined) Hy.brand600 else Hy.ink600)
                        }
                        Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, null, tint = Hy.ink400)
                    }
                }
            }
        }
        item { Box(Modifier.padding(horizontal = 20.dp)) { SectionTitle("ثبت‌نام‌های من", "همهٔ اردوها", nav.openTrips) } }
        val list = trips
        if (list != null && list.isEmpty()) item {
            HyCard(Modifier.padding(horizontal = 20.dp, vertical = 6.dp)) { Text("هنوز در اردویی ثبت‌نام نکرده‌اید.", color = Hy.ink400, fontSize = 13.sp) }
        }
        items(list.orEmpty(), key = { "t" + it.memberId }) { t ->
            val pay = pays.firstOrNull { it.memberId == t.memberId }
            HyCard(Modifier.padding(horizontal = 20.dp, vertical = 6.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    IconTile(Icons.Filled.DateRange, Hy.brand700, Hy.brand50)
                    Column(Modifier.weight(1f)) {
                        Text(t.name, fontWeight = FontWeight.ExtraBold, fontSize = 15.sp)
                        Text(listOfNotNull(t.date?.let(::fa), t.location).joinToString(" · "), fontSize = 12.sp, color = Hy.ink600)
                    }
                    when (t.status) {
                        "paid" -> Badge("پرداخت‌شده", Hy.emerald700, Hy.emerald50)
                        "pending" -> Badge("پرداخت‌نشده", Hy.rose700, Hy.rose50)
                        else -> Badge(t.status, Hy.ink600, Hy.sand100)
                    }
                }
                Text("مبلغ: ${money(t.price)} ریال", fontSize = 13.sp, color = Hy.ink900)
                pay?.installments?.takeIf { it.isNotEmpty() }?.let { inst ->
                    Column(verticalArrangement = Arrangement.spacedBy(4.dp)) {
                        inst.forEach { i ->
                            Row {
                                Text("قسط ${fa(i.number)}", fontSize = 12.sp, color = Hy.ink600, modifier = Modifier.weight(1f))
                                Text("${money(i.amount)} ریال · ${if (i.status == "paid") "پرداخت شد" else "مانده"}", fontSize = 12.sp, color = if (i.status == "paid") Hy.emerald700 else Hy.amber700)
                            }
                        }
                    }
                }
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (t.status == "pending") HyButton("پرداخت", Modifier.weight(1f)) { nav.web("/pay/${t.memberId}") }
                    else HyPill("فاکتور") { nav.web("/pay/${t.memberId}") }
                    t.mavaLink?.let { link -> HyPill("کارت سفر ماوا", bg = Hy.sky50, fg = Hy.sky600) { nav.openMavaTrip(link) } }
                    HyPill("پشتیبانی", bg = Hy.sand100, fg = Hy.ink600) { nav.web("/tickets") }
                }
            }
        }
        item {
            HyCard(Modifier.padding(horizontal = 20.dp, vertical = 12.dp), onClick = { nav.web("/payments") }) {
                Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                    IconTile(Icons.Filled.AccountBox, Hy.sky600, Hy.sky50)
                    Column(Modifier.weight(1f)) {
                        Text("پرداخت‌ها و کارت‌های بانکی", fontWeight = FontWeight.Bold, fontSize = 14.sp)
                        Text("کارت‌به‌کارت، رسیدها، بازگشت وجه (در سایت)", fontSize = 12.sp, color = Hy.ink600)
                    }
                    Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, null, tint = Hy.ink400)
                }
            }
        }
        if (list == null && error == null) item { Spacer(Modifier.height(40.dp)) }
        if (error == "module_off") item {
            Box(Modifier.padding(20.dp)) { Note("اردوها برای دانشگاه شما فعال نیست.", Hy.amber700, Hy.amber50) }
        }
    }
}
