package org.meshmsg.app.ui

import android.graphics.BitmapFactory
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Info
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.meshmsg.app.R
import org.meshmsg.app.data.Contact
import org.meshmsg.app.data.Delivery
import org.meshmsg.app.data.LocalAvatars
import org.meshmsg.app.ui.theme.MeshTheme
import org.meshmsg.app.ui.theme.Space
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.DecimalStyle
import java.time.format.FormatStyle
import java.util.Locale
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/** Initial on a colour derived from the contact hash: stable, no image needed. */
@Composable
fun Avatar(contact: Contact, size: Dp = 48.dp, modifier: Modifier = Modifier, photoKey: String = contact.hash) {
    val colors = MeshTheme.extra.avatars
    val color = colors[(contact.hash.hashCode() and 0x7fffffff) % colors.size]
    val initial = contact.displayName.trim().firstOrNull()?.uppercaseChar()?.toString() ?: "?"
    val file = LocalAvatars.current[photoKey]
    val photo by produceState<ImageBitmap?>(null, file) {
        value = file?.let { f -> withContext(Dispatchers.IO) { runCatching { BitmapFactory.decodeFile(f.path)?.asImageBitmap() }.getOrNull() } }
    }
    Box(
        modifier.size(size).clip(CircleShape).background(color).clearAndSetSemantics { },
        contentAlignment = Alignment.Center,
    ) {
        val p = photo
        if (p != null) {
            Image(p, contentDescription = null, contentScale = ContentScale.Crop, modifier = Modifier.matchParentSize())
        } else {
            Text(initial, color = MeshTheme.extra.onAvatar, style = if (size >= 48.dp) MaterialTheme.typography.titleMedium else MaterialTheme.typography.labelLarge)
        }
    }
}

/** Clock / check / double check, always with a spoken label. */
@Composable
fun DeliveryMark(delivery: Delivery, tint: Color, modifier: Modifier = Modifier) {
    val (icon, label) = when (delivery) {
        Delivery.Queued -> MeshIcons.Schedule to R.string.delivery_queued
        Delivery.Sent -> Icons.Filled.Check to R.string.delivery_sent
        Delivery.Delivered -> MeshIcons.DoneAll to R.string.delivery_delivered
        Delivery.Read -> MeshIcons.DoneAll to R.string.delivery_read
    }
    // Sized in sp so it grows with the timestamp next to it at large font scales.
    val size = with(LocalDensity.current) { 16.sp.toDp() }
    Icon(icon, contentDescription = stringResource(label), tint = if (delivery == Delivery.Read) MeshTheme.extra.read else tint, modifier = modifier.size(size))
}

/** A quiet note with an icon, for "no path" and similar states. */
@Composable
fun InfoBanner(text: String, modifier: Modifier = Modifier) {
    Row(
        modifier.fillMaxWidth().background(MaterialTheme.colorScheme.surfaceContainer).padding(horizontal = Space.l, vertical = Space.m),
        horizontalArrangement = Arrangement.spacedBy(Space.m),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Icon(Icons.Filled.Info, contentDescription = null, tint = MeshTheme.extra.warning, modifier = Modifier.size(20.dp))
        Text(text, style = MaterialTheme.typography.bodyMedium, color = MaterialTheme.colorScheme.onSurface)
    }
}

@Composable
fun EmptyState(title: String, body: String, modifier: Modifier = Modifier, action: @Composable () -> Unit = {}) {
    Column(
        modifier.fillMaxWidth().padding(Space.xxl),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.spacedBy(Space.m),
    ) {
        Text(title, style = MaterialTheme.typography.titleLarge, textAlign = TextAlign.Center)
        Text(body, style = MaterialTheme.typography.bodyLarge, color = MaterialTheme.colorScheme.onSurfaceVariant, textAlign = TextAlign.Center)
        action()
    }
}

@Composable
fun SectionHeader(text: String, modifier: Modifier = Modifier) {
    Text(
        text,
        style = MaterialTheme.typography.labelLarge,
        color = MaterialTheme.colorScheme.primary,
        modifier = modifier.padding(start = Space.l, end = Space.l, top = Space.xl, bottom = Space.s),
    )
}

@Composable
fun currentLocale(): Locale = LocalConfiguration.current.locales[0]

/** Locale-aware times with the locale's own digits (Persian digits in fa). */
object Times {
    private fun fmt(f: DateTimeFormatter, locale: Locale) = f.withLocale(locale).withDecimalStyle(DecimalStyle.of(locale))

    fun clock(ms: Long, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String =
        fmt(DateTimeFormatter.ofLocalizedTime(FormatStyle.SHORT), locale).format(Instant.ofEpochMilli(ms).atZone(zone))

    fun date(ms: Long, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String =
        fmt(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM), locale).format(Instant.ofEpochMilli(ms).atZone(zone))

    fun day(ms: Long, zone: ZoneId = ZoneId.systemDefault()): LocalDate = Instant.ofEpochMilli(ms).atZone(zone).toLocalDate()

    /** Chat list: the time today, otherwise the date. */
    fun short(ms: Long, nowMs: Long, locale: Locale, zone: ZoneId = ZoneId.systemDefault()): String =
        if (day(ms, zone) == day(nowMs, zone)) clock(ms, locale, zone)
        else fmt(DateTimeFormatter.ofLocalizedDate(FormatStyle.SHORT), locale).format(Instant.ofEpochMilli(ms).atZone(zone))
}
