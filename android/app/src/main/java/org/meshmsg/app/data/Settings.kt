package org.meshmsg.app.data

import android.content.Context
import android.content.SharedPreferences
import androidx.core.content.edit
import org.json.JSONArray
import org.json.JSONObject

/** A relay the phone may reach. via: "direct", "tor" or a proxy URL. */
data class Relay(val name: String, val url: String, val via: String = "direct")

/** The project's default relay, used until the user edits the relay list. */
val DEFAULT_RELAYS = listOf(Relay("eu-1", "http://87.106.5.192"))

enum class BatteryMode { Active, Saver, Off }

/** User settings (not secret: keys and messages live in the Go core's files). */
class Settings(context: Context) {
    private val p: SharedPreferences = context.getSharedPreferences("settings", Context.MODE_PRIVATE)

    var onboarded: Boolean
        get() = p.getBoolean("onboarded", false)
        set(v) = p.edit { putBoolean("onboarded", v) }

    /** Optional display name sent in announces; empty = anonymous. */
    var displayName: String
        get() = p.getString("name", "") ?: ""
        set(v) = p.edit { putString("name", v.trim()) }

    /**
     * Carry other people's traffic between relays and the mesh around this phone.
     * On by default: without it, phones nearby that have no internet can send
     * through this one but never get their replies.
     */
    var gateway: Boolean
        get() = p.getBoolean("gateway", true)
        set(v) = p.edit { putBoolean("gateway", v) }

    /** Find relays automatically and share ours with nearby devices (on by default). */
    var discovery: Boolean
        get() = p.getBoolean("discovery", true)
        set(v) = p.edit { putBoolean("discovery", v) }

    var batteryMode: BatteryMode
        get() = runCatching { BatteryMode.valueOf(p.getString("battery", "") ?: "") }.getOrDefault(BatteryMode.Active)
        set(v) = p.edit { putString("battery", v.name) }

    var relays: List<Relay>
        get() {
            val json = p.getString("relays", null) ?: return DEFAULT_RELAYS
            return runCatching {
                val a = JSONArray(json)
                List(a.length()) { a.getJSONObject(it).let { o -> Relay(o.getString("name"), o.getString("url"), o.optString("via", "direct")) } }
            }.getOrDefault(emptyList())
        }
        set(v) {
            val a = JSONArray()
            v.forEach { a.put(JSONObject().put("name", it.name).put("url", it.url).put("via", it.via)) }
            p.edit { putString("relays", a.toString()) }
        }

    /** The node config the Go core is opened with (mobile/meshcore.go Open). */
    fun coreConfig(): String {
        val relays = JSONArray()
        this.relays.forEach { relays.put(JSONObject().put("name", it.name).put("url", it.url).put("via", it.via)) }
        return JSONObject()
            .put("name", displayName)
            .put("delegate", true)
            .put("relays", relays)
            .put("gateway", JSONObject().put("enabled", gateway).put("mode", "targeted").put("quota_kb_per_hour", 512))
            .put("discovery", JSONObject().put("off", !discovery))
            .toString()
    }

    fun clear() = p.edit { clear() }
}
