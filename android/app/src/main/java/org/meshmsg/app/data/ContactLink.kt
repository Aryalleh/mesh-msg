package org.meshmsg.app.data

/**
 * Contact links as web links (core: identity.Contact.Link): messengers make
 * https://eu-1.mfadem.ir/add#<key>?n=<name> clickable, and Android opens it in
 * the app (App Link). The contact is in the fragment, never sent to the server.
 */
object ContactLink {
    const val HOST = "eu-1.mfadem.ir"
    const val BASE = "https://$HOST/add#"

    /** meshmsg:… → https link; anything else (e.g. meshgroup:…) unchanged. */
    fun toWeb(uri: String): String = if (uri.startsWith("meshmsg:")) BASE + uri.removePrefix("meshmsg:") else uri

    /** An http(s) …/add#… link → meshmsg:…; anything else unchanged. */
    fun toUri(link: String): String {
        val s = link.trim()
        if (!s.startsWith("https://") && !s.startsWith("http://")) return s
        val page = s.substringBefore('#', missingDelimiterValue = "")
        if (page.isEmpty() || !page.trimEnd('/').endsWith("/add")) return s
        return "meshmsg:" + s.substringAfter('#')
    }
}
