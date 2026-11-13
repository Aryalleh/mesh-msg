package org.meshmsg.app.data

import org.json.JSONObject

/** One download of a release (from the signed manifest). */
data class UpdateFile(val name: String, val url: String, val sha256: String, val size: Long)

/**
 * Result of the update check (roadmap B7): the Go core verified the release
 * manifest's signature before this is built from it.
 */
data class AppUpdate(
    val current: String,
    val version: String,
    val notes: Map<String, String>,
    val page: String,
    val available: Boolean,
    val required: Boolean,
    val files: List<UpdateFile>,
) {
    /** Release notes in [lang] ("fa", "en"), falling back to English. */
    fun notesFor(lang: String): String = notes[lang] ?: notes["en"] ?: notes.values.firstOrNull().orEmpty()

    /** The APK for the first ABI this phone supports, else the universal one. */
    fun apkFor(abis: List<String>): UpdateFile? =
        abis.firstNotNullOfOrNull { abi -> files.firstOrNull { it.name.endsWith("-$abi.apk") } }
            ?: files.firstOrNull { it.name.endsWith("-universal.apk") }

    companion object {
        /** Parses Core.checkUpdate's JSON; null when no release was found. */
        fun parse(json: String): AppUpdate? = runCatching {
            val o = JSONObject(json)
            val m = o.optJSONObject("latest") ?: return null
            val notes = m.optJSONObject("notes")?.let { n -> n.keys().asSequence().associateWith { n.getString(it) } }.orEmpty()
            val fs = m.optJSONArray("files")
            val files = if (fs == null) emptyList() else List(fs.length()) {
                fs.getJSONObject(it).let { f -> UpdateFile(f.getString("name"), f.getString("url"), f.getString("sha256"), f.optLong("size")) }
            }
            AppUpdate(
                current = o.optString("current"), version = m.getString("version"), notes = notes,
                page = m.optString("page"), available = o.optBoolean("available"), required = o.optBoolean("required"), files = files,
            )
        }.getOrNull()
    }
}
