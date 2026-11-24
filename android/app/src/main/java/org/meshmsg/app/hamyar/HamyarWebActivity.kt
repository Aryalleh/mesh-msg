package org.meshmsg.app.hamyar

import android.app.Activity
import android.app.DownloadManager
import android.content.Context
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.view.Gravity
import android.view.View
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.webkit.CookieManager
import android.webkit.DownloadListener
import android.webkit.ValueCallback
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.TextView
import androidx.activity.ComponentActivity
import androidx.activity.OnBackPressedCallback
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.getSystemService

/**
 * The Hamyar website inside the app (hybrid parity): the web flows that are not native — trip registration, payment,
 * documents/KYC, surveys, tickets, clinic — run here, logged in. The caller passes a one-time web-link URL
 * (api.<base>/v1/web-link, MainActivity.openHamyarWeb) that sets the web session cookie; after that the WebView stays
 * logged in via the cookie. Supports file upload (documents), downloads (receipts), and hands non-http links (the bank
 * gateway's app, tel:, mailto:) to the system.
 */
class HamyarWebActivity : ComponentActivity() {
    private lateinit var web: WebView
    private var fileCb: ValueCallback<Array<Uri>>? = null

    private val filePicker = registerForActivityResult(ActivityResultContracts.StartActivityForResult()) { r ->
        val cb = fileCb; fileCb = null
        cb?.onReceiveValue(if (r.resultCode == Activity.RESULT_OK) WebChromeClient.FileChooserParams.parseResult(r.resultCode, r.data) else null)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val url = intent.getStringExtra(EXTRA_URL)
        if (url.isNullOrBlank()) { finish(); return }

        val root = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setBackgroundColor(Color.WHITE) }
        root.addView(topBar())
        val progress = ProgressBar(this, null, android.R.attr.progressBarStyleHorizontal).apply {
            max = 100; visibility = View.GONE
        }
        root.addView(progress, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT))

        web = WebView(this)
        root.addView(web, LinearLayout.LayoutParams(MATCH_PARENT, 0, 1f))
        setContentView(root)

        @Suppress("SetJavaScriptEnabled")
        web.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            databaseEnabled = true
            loadWithOverviewMode = true
            useWideViewPort = true
            mediaPlaybackRequiresUserGesture = false
            setSupportMultipleWindows(false)
            allowFileAccess = false
            allowContentAccess = false
        }
        CookieManager.getInstance().apply {
            setAcceptCookie(true)
            setAcceptThirdPartyCookies(web, true)
        }

        web.webViewClient = object : WebViewClient() {
            override fun shouldOverrideUrlLoading(view: WebView, req: WebResourceRequest): Boolean {
                val u = req.url
                if (u.scheme == "http" || u.scheme == "https") return false // keep in the WebView
                // bank app, tel:, mailto:, intent:, meshtrip:… -> let the system handle it
                return runCatching { startActivity(Intent(Intent.ACTION_VIEW, u).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)); true }.getOrDefault(true)
            }
        }
        web.webChromeClient = object : WebChromeClient() {
            override fun onProgressChanged(view: WebView, p: Int) {
                progress.progress = p
                progress.visibility = if (p in 1..99) View.VISIBLE else View.GONE
            }
            override fun onReceivedTitle(view: WebView, t: String?) {
                if (!t.isNullOrBlank() && !t.startsWith("http")) titleView.text = t
            }
            override fun onShowFileChooser(view: WebView, cb: ValueCallback<Array<Uri>>, params: FileChooserParams): Boolean {
                fileCb?.onReceiveValue(null)
                fileCb = cb
                return runCatching { filePicker.launch(params.createIntent()); true }.getOrElse { fileCb = null; false }
            }
        }
        web.setDownloadListener(DownloadListener { dl, _, disposition, mime, _ ->
            runCatching {
                val req = DownloadManager.Request(Uri.parse(dl)).apply {
                    setMimeType(mime)
                    addRequestHeader("Cookie", CookieManager.getInstance().getCookie(dl))
                    setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE_NOTIFY_COMPLETED)
                    val name = android.webkit.URLUtil.guessFileName(dl, disposition, mime)
                    setDestinationInExternalPublicDir(android.os.Environment.DIRECTORY_DOWNLOADS, name)
                }
                getSystemService<DownloadManager>()?.enqueue(req)
            }
        })

        onBackPressedDispatcher.addCallback(this, object : OnBackPressedCallback(true) {
            override fun handleOnBackPressed() { if (web.canGoBack()) web.goBack() else finish() }
        })

        if (savedInstanceState != null) web.restoreState(savedInstanceState) else web.loadUrl(url)
    }

    private lateinit var titleView: TextView

    private fun topBar(): View {
        val bar = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setBackgroundColor(Color.parseColor("#0F4C3A")) // brand green
            val pad = (12 * resources.displayMetrics.density).toInt()
            setPadding(pad, pad, pad, pad)
        }
        val close = TextView(this).apply {
            text = "✕"; setTextColor(Color.WHITE); textSize = 20f
            setOnClickListener { finish() }
            val p = (8 * resources.displayMetrics.density).toInt(); setPadding(p, 0, p, 0)
        }
        titleView = TextView(this).apply {
            text = "همیار"; setTextColor(Color.WHITE); textSize = 16f
            maxLines = 1; ellipsize = android.text.TextUtils.TruncateAt.END
        }
        bar.addView(close)
        bar.addView(titleView, LinearLayout.LayoutParams(0, WRAP_CONTENT, 1f).apply {
            marginStart = (8 * resources.displayMetrics.density).toInt()
        })
        bar.layoutParams = FrameLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT)
        return bar
    }

    override fun onSaveInstanceState(outState: Bundle) {
        super.onSaveInstanceState(outState)
        if (::web.isInitialized) web.saveState(outState)
    }

    override fun onPause() {
        super.onPause()
        CookieManager.getInstance().flush()
    }

    override fun onDestroy() {
        if (::web.isInitialized) { web.stopLoading(); web.destroy() }
        super.onDestroy()
    }

    companion object {
        private const val EXTRA_URL = "url"
        fun open(ctx: Context, url: String) {
            ctx.startActivity(Intent(ctx, HamyarWebActivity::class.java).putExtra(EXTRA_URL, url))
        }
    }
}
