package io.github.mitt776.mitm

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.util.Log
import android.view.ViewGroup
import android.webkit.ConsoleMessage
import android.webkit.WebChromeClient
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.webkit.WebViewAssetLoader

/**
 * Окно приложения — один WebView с тем же интерфейсом, что и на Windows.
 *
 * Логика и состояние живут в Go и переживают пересоздание Activity: ядро подняли в
 * MitmApp, а туннель держит foreground-сервис. Поэтому здесь только вёрстка окна,
 * системный диалог согласия на VPN (его показывает только Activity) и аппаратная
 * кнопка «назад».
 */
class MainActivity : Activity() {

    private lateinit var webView: WebView
    private val bridge get() = MitmApp.instance.bridge

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        // Интерфейс рисуется во весь экран, включая области под статусбаром и
        // полоской жеста. На Android 15+ система это навязывает сама, поэтому
        // включаем везде — иначе раскладка на разных версиях разъезжается.
        WindowCompat.setDecorFitsSystemWindows(window, false)

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

                // Переменные с отступами живут в DOM, а DOM только что заменили
                // загрузкой страницы — просим систему раздать инсеты заново.
                override fun onPageFinished(view: WebView, url: String) {
                    ViewCompat.requestApplyInsets(view)
                }
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
        applyInsets()

        bridge.attach(webView, this)
        askForNotifications()
        webView.loadUrl(startUrl())
    }

    /**
     * Разрешение на уведомления. С Android 13 оно спрашивается в рантайме, и без
     * него канал сервиса получает importance=NONE: туннель работает, а уведомления
     * с состоянием, скоростью и кнопкой «Отключить» пользователь не видит вовсе —
     * единственный способ отключиться остаётся через само приложение.
     *
     * Отказ не мешает работе, поэтому результат не обрабатываем и повторно не
     * пристаём: система сама показывает запрос только один раз.
     */
    private fun askForNotifications() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return
        val granted = checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) ==
            PackageManager.PERMISSION_GRANTED
        if (!granted) {
            requestPermissions(arrayOf(Manifest.permission.POST_NOTIFICATIONS), REQUEST_NOTIFY)
        }
    }

    /**
     * Отступы под системные панели отдаются интерфейсу переменными CSS.
     *
     * Только на env(safe-area-inset-*) полагаться нельзя: в WebView эти значения
     * описывают вырез экрана, а не статусбар с полоской жеста, — на телефоне без
     * «чёлки» они равны нулю, и шапка уезжает под часы. Значения из
     * WindowInsets точные, а env() остаётся запасным вариантом в самом CSS.
     */
    private fun applyInsets() {
        ViewCompat.setOnApplyWindowInsetsListener(webView) { _, insets ->
            val bars = insets.getInsets(
                WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()
            )
            val d = resources.displayMetrics.density
            fun px(v: Int) = (v / d).toInt()
            webView.evaluateJavascript(
                // Проверка на null не лишняя: инсеты прилетают и во время самой
                // навигации, когда старый документ уже снесён, а нового ещё нет,
                // — без неё в консоли повисает TypeError на каждом запуске.
                """
                (function(e){
                  if (!e) return;
                  e.style.setProperty('--safe-top', '${px(bars.top)}px');
                  e.style.setProperty('--safe-bottom', '${px(bars.bottom)}px');
                  e.style.setProperty('--safe-left', '${px(bars.left)}px');
                  e.style.setProperty('--safe-right', '${px(bars.right)}px');
                })(document.documentElement)
                """.trimIndent(),
                null,
            )
            insets
        }
        // Первую раздачу инсетов система могла провести до того, как слушатель
        // повесили, — просим её повторить.
        ViewCompat.requestApplyInsets(webView)
    }

    private fun startUrl(): String {
        val url = "https://appassets.androidplatform.net/assets/web/mobile.html"
        // Отладочная передача ссылки на ноду, чтобы не набирать её на телефоне:
        //   adb shell am start -n .../.MainActivity --es link "vless://…"
        // Только в отладочной сборке: в релизе такого входа быть не должно.
        // Ссылка доезжает до интерфейса и нигде не сохраняется.
        if (!BuildConfig.DEBUG) return url
        val link = intent?.getStringExtra("link")
        return if (link.isNullOrEmpty()) url else "$url?link=${Uri.encode(link)}"
    }

    override fun onDestroy() {
        bridge.detach(this)
        super.onDestroy()
    }

    /**
     * Кнопка «назад» сперва предлагается интерфейсу: он закрывает меню разделов
     * или возвращается на «Подключение». Не взял — сворачиваем приложение, а не
     * закрываем: туннель живёт дальше в сервисе, и убивать окно незачем.
     *
     * Ответ читается из evaluateJavascript, поэтому __mitmBack обязан быть
     * синхронным — промис сюда не доедет.
     */
    @Deprecated("Deprecated in Java")
    override fun onBackPressed() {
        webView.evaluateJavascript("window.__mitmBack ? __mitmBack() : false") { result ->
            if (result != "true") moveTaskToBack(true)
        }
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
        const val REQUEST_NOTIFY = 2
    }
}
