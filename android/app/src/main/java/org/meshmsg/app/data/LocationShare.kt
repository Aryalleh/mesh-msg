package org.meshmsg.app.data

import android.content.Context
import android.content.Intent
import android.net.Uri
import java.util.Locale

/**
 * A location sent in a chat, as Telegram does it: plain text any Mava (a
 * board, the desktop) shows, with links for Google Maps, Neshan and Balad
 * and a geo: line; the Android app draws it as a card whose Navigate button
 * lets the user pick a navigation app.
 *
 *   📍 گم شده‌ام / I'm lost
 *   32.616057, 44.024879 (±12 m)
 *   Google Maps: https://www.google.com/maps/search/?api=1&query=32.616057,44.024879
 *   Neshan: https://nshn.ir/?lat=32.616057&lng=44.024879
 *   Balad: https://balad.ir/location?latitude=32.616057&longitude=44.024879&zoom=17
 *   geo:32.616057,44.024879
 */
data class SharedLocation(val lat: Double, val lon: Double, val accuracy: Int, val title: String) {
    val coords: String get() = "%.6f, %.6f".format(Locale.ROOT, lat, lon)
    val googleMaps: String get() = "https://www.google.com/maps/search/?api=1&query=%.6f,%.6f".format(Locale.ROOT, lat, lon)
    val neshan: String get() = "https://nshn.ir/?lat=%.6f&lng=%.6f".format(Locale.ROOT, lat, lon)
    val balad: String get() = "https://balad.ir/location?latitude=%.6f&longitude=%.6f&zoom=17".format(Locale.ROOT, lat, lon)
    val geo: String get() = "geo:%.6f,%.6f".format(Locale.ROOT, lat, lon)
}

object LocationShare {
    const val PIN = "📍"
    const val TITLE_LOST = "گم شده‌ام / I'm lost"
    const val TITLE_HERE = "موقعیت من / My location"

    fun text(lat: Double, lon: Double, accuracy: Int, lost: Boolean): String {
        val l = SharedLocation(lat, lon, accuracy, if (lost) TITLE_LOST else TITLE_HERE)
        return buildString {
            append(PIN).append(' ').append(l.title).append('\n')
            append(l.coords)
            if (accuracy > 0) append(" (±").append(accuracy).append(" m)")
            append('\n')
            append("Google Maps: ").append(l.googleMaps).append('\n')
            append("Neshan: ").append(l.neshan).append('\n')
            append("Balad: ").append(l.balad).append('\n')
            append(l.geo)
        }
    }

    /** No position yet: the leaders still learn who is lost (positions follow through the trip). */
    fun lostWithoutFix(): String = "$PIN $TITLE_LOST\n(موقعیت هنوز پیدا نشده؛ به محض پیدا شدن فرستاده می‌شود / no position yet; it follows as soon as the phone has one)"

    private val geoLine = Regex("""(?m)^geo:(-?\d{1,2}(?:\.\d+)?),(-?\d{1,3}(?:\.\d+)?)\s*$""")
    private val accuracyRe = Regex("""\(±(\d+) m\)""")

    /** The location a message carries, if it is one. */
    fun parse(text: String): SharedLocation? {
        if (!text.startsWith(PIN)) return null
        val m = geoLine.find(text) ?: return null
        val lat = m.groupValues[1].toDoubleOrNull() ?: return null
        val lon = m.groupValues[2].toDoubleOrNull() ?: return null
        if (lat !in -90.0..90.0 || lon !in -180.0..180.0) return null
        val title = text.lineSequence().first().removePrefix(PIN).trim()
        val acc = accuracyRe.find(text)?.groupValues?.get(1)?.toIntOrNull() ?: 0
        return SharedLocation(lat, lon, acc, title)
    }

    /** Opens the point in a navigation app the user picks (Google Maps, Neshan, Balad, OsmAnd…). */
    fun navigate(c: Context, lat: Double, lon: Double, label: String, chooserTitle: String) {
        val q = "%.6f,%.6f".format(Locale.ROOT, lat, lon)
        val geo = Intent(Intent.ACTION_VIEW, Uri.parse("geo:$q?q=$q(" + Uri.encode(label) + ")"))
        runCatching {
            c.startActivity(Intent.createChooser(geo, chooserTitle).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }.onFailure {
            openLink(c, SharedLocation(lat, lon, 0, label).googleMaps)
        }
    }

    fun openLink(c: Context, url: String) {
        runCatching { c.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)) }
    }
}
