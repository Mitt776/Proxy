package io.github.mitt776.mitm

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.BitmapFactory
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
import io.github.mitt776.mobile.Mobile
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import kotlin.concurrent.thread

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

                /**
                 * Уходить с локального origin этому WebView запрещено.
                 *
                 * В нём висит мост MitMNative, а @JavascriptInterface достаётся
                 * любой странице, которая в WebView окажется, — не только нашей.
                 * То есть одна навигация на внешний адрес (ссылка в будущем
                 * интерфейсе, редирект, iframe) отдала бы чужому коду профили с
                 * учётными данными и управление туннелем целиком.
                 *
                 * Поэтому всё, что не наш ассет-хост, уходит во внешний браузер,
                 * а сам WebView остаётся на приложении. Проверять надо и хост, и
                 * схему: http://appassets.androidplatform.net — чужой адрес в
                 * интернете, совпадающий с нашим по имени.
                 */
                override fun shouldOverrideUrlLoading(
                    view: WebView,
                    request: WebResourceRequest,
                ): Boolean {
                    val url = request.url
                    if (url.scheme == "https" && url.host == ASSET_HOST) return false
                    bridge.openExternal(url)
                    return true
                }

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
        webView.loadUrl(PAGE_URL)
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
            // Клавиатура идёт снизу тем же отступом. Окно у нас во весь экран и
            // само не сжимается, поэтому без этого экранная клавиатура накрывает
            // низ модального окна — вместе с кнопкой «Сохранить», ради которой
            // пользователь и открыл форму.
            val ime = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
            val d = resources.displayMetrics.density
            fun px(v: Int) = (v / d).toInt()
            val bottom = maxOf(bars.bottom, ime)
            webView.evaluateJavascript(
                // Проверка на null не лишняя: инсеты прилетают и во время самой
                // навигации, когда старый документ уже снесён, а нового ещё нет,
                // — без неё в консоли повисает TypeError на каждом запуске.
                """
                (function(e){
                  if (!e) return;
                  e.style.setProperty('--safe-top', '${px(bars.top)}px');
                  e.style.setProperty('--safe-bottom', '${px(bottom)}px');
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

    // --- импорт профиля картинкой и камерой -----------------------------------
    //
    // Оба пути возвращают имя заведённого профиля тому вызову из JS, который их
    // начал. Ждать приходится через onActivityResult, поэтому id вызова живёт
    // здесь до возвращения пользователя.

    private var pendingImageCall = 0
    private var pendingScanCall = 0

    /** Выбрать картинку с QR из галереи. */
    fun pickQRImage(callID: Int) {
        pendingImageCall = callID
        // ACTION_GET_CONTENT, а не доступ к хранилищу: выбранный файл отдаётся
        // разово по content-URI, и никаких разрешений просить не нужно.
        val intent = Intent(Intent.ACTION_GET_CONTENT)
            .setType("image/*")
            .addCategory(Intent.CATEGORY_OPENABLE)
        runCatching { startActivityForResult(intent, REQUEST_QR_IMAGE) }
            .onFailure {
                finishCall(pendingImageCall.also { pendingImageCall = 0 }, "",
                    "[E_NO_METHOD] выбрать картинку нечем")
            }
    }

    /** Навести камеру на QR. Разрешение спрашиваем прямо перед съёмкой. */
    fun scanQR(callID: Int) {
        pendingScanCall = callID
        val granted = checkSelfPermission(Manifest.permission.CAMERA) ==
            PackageManager.PERMISSION_GRANTED
        if (granted) {
            startActivityForResult(Intent(this, QrScanActivity::class.java), REQUEST_QR_SCAN)
        } else {
            requestPermissions(arrayOf(Manifest.permission.CAMERA), REQUEST_CAMERA)
        }
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray,
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode != REQUEST_CAMERA) return
        val granted = grantResults.firstOrNull() == PackageManager.PERMISSION_GRANTED
        if (granted) {
            startActivityForResult(Intent(this, QrScanActivity::class.java), REQUEST_QR_SCAN)
        } else {
            // Отказ — не ошибка: пользователь передумал. Отвечаем пустым именем,
            // и интерфейс просто ничего не делает.
            finishCall(pendingScanCall.also { pendingScanCall = 0 }, "", null)
        }
    }

    /**
     * Читает выбранную картинку и отдаёт её в Go на распознавание.
     *
     * Уменьшаем перед этим: снимок с 50-мегапиксельной камеры телефона — это
     * ~100 МБ в памяти после декодирования, и распознавание такой картины
     * занимает секунды. Для QR хватает полутора тысяч пикселей по длинной
     * стороне.
     */
    private fun importPickedImage(uri: Uri) {
        val callID = pendingImageCall
        pendingImageCall = 0
        thread(name = "mitm-qr-image") {
            try {
                val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
                contentResolver.openInputStream(uri)?.use { BitmapFactory.decodeStream(it, null, bounds) }

                val longest = maxOf(bounds.outWidth, bounds.outHeight)
                val opts = BitmapFactory.Options().apply {
                    inSampleSize = 1
                    while (longest / inSampleSize > QR_MAX_SIDE) inSampleSize *= 2
                }
                val bitmap = contentResolver.openInputStream(uri)
                    ?.use { BitmapFactory.decodeStream(it, null, opts) }
                    ?: throw IllegalStateException("картинка не читается")

                val bytes = ByteArrayOutputStream()
                bitmap.compress(Bitmap.CompressFormat.PNG, 100, bytes)
                bitmap.recycle()

                val name = Mobile.importQRImage(bytes.toByteArray())
                finishCall(callID, name, null)
            } catch (e: Throwable) {
                finishCall(callID, "", e.message ?: "не удалось прочитать картинку")
            }
        }
    }

    /** Ответить вызову из JS: имя профиля либо ошибка. */
    private fun finishCall(callID: Int, profileName: String, error: String?) {
        if (callID == 0) return
        if (error != null) bridge.onResult(callID, "", error)
        else bridge.onResult(callID, JSONObject.quote(profileName), "")
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        when (requestCode) {
            REQUEST_VPN -> bridge.onVpnConsent(resultCode == RESULT_OK)

            REQUEST_QR_IMAGE -> {
                val uri = data?.data
                // Отмена выбора — не ошибка: отвечаем пустым именем.
                if (resultCode != RESULT_OK || uri == null) {
                    finishCall(pendingImageCall.also { pendingImageCall = 0 }, "", null)
                } else {
                    importPickedImage(uri)
                }
            }

            REQUEST_QR_SCAN -> {
                val name = data?.getStringExtra(QrScanActivity.EXTRA_PROFILE).orEmpty()
                finishCall(pendingScanCall.also { pendingScanCall = 0 }, name, null)
            }
        }
    }

    private companion object {
        const val TAG = "MitM"

        /**
         * Интерфейс отдаётся под https-происхождением через WebViewAssetLoader:
         * с file:// Chromium режет ES-модули по CORS, и экран остаётся пустым
         * без единой ошибки в журнале.
         */
        const val ASSET_HOST = "appassets.androidplatform.net"
        const val PAGE_URL = "https://$ASSET_HOST/assets/web/mobile.html"

        const val REQUEST_VPN = 1
        const val REQUEST_NOTIFY = 2
        const val REQUEST_QR_IMAGE = 3
        const val REQUEST_QR_SCAN = 4
        const val REQUEST_CAMERA = 5

        /** Длинная сторона картинки перед распознаванием QR. */
        const val QR_MAX_SIDE = 1600
    }
}
