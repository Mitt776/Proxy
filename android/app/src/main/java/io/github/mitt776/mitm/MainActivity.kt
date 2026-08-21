package io.github.mitt776.mitm

import android.app.Activity
import android.content.Intent
import android.graphics.Color
import android.net.Uri
import android.os.Bundle
import android.util.Log
import android.view.ViewGroup
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.webkit.WebViewAssetLoader

/**
 * Окно приложения — один WebView с тем же интерфейсом, что и на Windows.
 *
 * Логика и состояние живут в Go и переживают пересоздание Activity: ядро подняли в
 * MitmApp, а туннель держит foreground-сервис. Поэтому здесь только вёрстка окна и
 * системный диалог согласия на VPN — его показывает только Activity.
 */
class MainActivity : Activity() {

    private lateinit var webView: WebView
    private val bridge get() = MitmApp.instance.bridge

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        webView = WebView(this).apply {
            layoutParams = ViewGroup.LayoutParams(
                ViewGroup.LayoutParams.MATCH_PARENT,
                ViewGroup.LayoutParams.MATCH_PARENT,
            )
            // Тот же фон, что у токена --bg: иначе на старте мигает белым.
            setBackgroundColor(Color.parseColor("#0E0A1A"))
            // Интерфейс отдаётся из ассетов APK под https-происхождением, а не с
            // file://. Иначе Chromium отказывается грузить ES-модули (origin у
            // file:// — null, и модуль блокируется CORS), и получается пустой экран
            // без единой ошибки в логе.
            val assetLoader = WebViewAssetLoader.Builder()
                .addPathHandler("/assets/", WebViewAssetLoader.AssetsPathHandler(this@MainActivity))
                .build()
            webViewClient = object : WebViewClient() {
                override fun shouldInterceptRequest(
                    view: WebView,
                    request: WebResourceRequest,
                ): WebResourceResponse? = assetLoader.shouldInterceptRequest(request.url)
            }
            // Консоль интерфейса — в logcat. Иначе ошибка в JS выглядит как
            // просто не отрисовавшийся экран, и искать её нечем.
            webChromeClient = object : WebChromeClient() {
                override fun onConsoleMessage(message: ConsoleMessage): Boolean {
                    Log.i(TAG, "web: ${message.message()} (${message.sourceId()}:${message.lineNumber()})")
                    return true
                }
            }
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            // Интерфейс лежит в ассетах APK; читать что-то ещё с диска ему незачем.
            settings.allowFileAccess = false
            settings.allowContentAccess = false
            addJavascriptInterface(bridge.native, "MitMNative")
        }
        setContentView(webView)

        bridge.attach(webView, this)

        // Отладочная передача ссылки на ноду, чтобы не набирать её на телефоне:
        //   adb shell am start -n .../.MainActivity --es link "vless://…"
        // Ссылка доезжает до поля ввода и нигде не сохраняется.
        val link = intent?.getStringExtra("link")
        val url = buildString {
            append("https://appassets.androidplatform.net/assets/web/mobile.html")
            if (!link.isNullOrEmpty()) append("?link=").append(Uri.encode(link))
        }
        webView.loadUrl(url)
    }

    override fun onDestroy() {
        bridge.detach(this)
        super.onDestroy()
    }

    /** Показать системный диалог согласия на VPN (пришло из Go через Bridge). */
    fun requestVpnConsent(intent: Intent) {
        runOnUiThread { startActivityForResult(intent, REQUEST_VPN) }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == REQUEST_VPN) {
            bridge.onVpnConsent(resultCode == RESULT_OK)
        }
    }

    private companion object {
        const val TAG = "MitM"
        const val REQUEST_VPN = 1
    }
}
