package io.github.mitt776.mitm

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.drawable.Drawable
import android.net.Uri
import android.net.VpnService
import android.os.Build
import android.util.Base64
import android.util.Log
import android.webkit.JavascriptInterface
import android.webkit.WebView
import io.github.mitt776.mobile.Controller
import io.github.mitt776.mobile.EventSink
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.lang.ref.WeakReference
import kotlin.concurrent.thread

/**
 * Мост между WebView и Go.
 *
 * На Windows этим занимается Wails: он биндит методы App прямо в JS. Здесь биндинга
 * нет, поэтому вызов едет тройкой (id, имя метода, аргументы JSON), а ответ
 * возвращается в JS по тому же id.
 *
 * Мост асинхронный намеренно: синхронный @JavascriptInterface заблокировал бы поток
 * WebView на обновлении подписки или тесте задержки — это секунды с замершим
 * интерфейсом.
 */
class Bridge(private val context: Context) : EventSink, Controller {

    private var webView: WeakReference<WebView> = WeakReference(null)
    private var activity: WeakReference<MainActivity> = WeakReference(null)

    /** Конфиг, с которым Go просил поднять туннель; ждёт согласия на VPN. */
    @Volatile
    private var pendingConfig: String? = null

    /** Состояние ядра — нужно окну при холодном старте. */
    @Volatile
    var state: String = "stopped"
        private set

    fun attach(view: WebView, host: MainActivity) {
        webView = WeakReference(view)
        activity = WeakReference(host)
    }

    fun detach(host: MainActivity) {
        if (activity.get() === host) {
            activity = WeakReference(null)
            webView = WeakReference(null)
        }
    }

    // --- то, что видит JS ---

    inner class Native {
        @JavascriptInterface
        fun call(id: Int, method: String, argsJSON: String) {
            // Часть методов до Go-диспетчера не доезжает: они открывают системный
            // экран (галерею, камеру) или лезут в PackageManager, а ответ отдают
            // только когда пользователь оттуда вернулся. Протокол тот же — ответ
            // приходит в __mitmResolve по тому же id.
            if (handleLocally(id, method, argsJSON)) return
            io.github.mitt776.mobile.Mobile.call(id, method, argsJSON)
        }

        @JavascriptInterface
        fun clipboardGet(): String {
            val manager = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            return manager.primaryClip?.takeIf { it.itemCount > 0 }
                ?.getItemAt(0)?.coerceToText(context)?.toString().orEmpty()
        }

        @JavascriptInterface
        fun clipboardSet(text: String) {
            val manager = context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
            manager.setPrimaryClip(ClipData.newPlainText("MitM", text))
        }
    }

    val native = Native()

    /**
     * Методы, которые выполняет сама Kotlin.
     *
     * Возвращает true, если вызов забран: тогда до Go он не доедет, а ответ
     * придёт позже — из onActivityResult или из фонового потока.
     */
    private fun handleLocally(id: Int, method: String, argsJSON: String): Boolean {
        when (method) {
            "OpenURL" -> {
                val url = firstString(argsJSON)
                openURL(id, url)
            }

            "PickQRImage" -> withActivity(id) { it.pickQRImage(id) }
            "ScanQR" -> withActivity(id) { it.scanQR(id) }

            // Список приложений собирается в фоне: у сотни пакетов надо достать
            // имя и растеризовать иконку, а @JavascriptInterface выполняется на
            // потоке WebView — интерфейс замер бы на секунду-другую.
            "ListApps" -> thread(name = "mitm-list-apps") {
                runCatching { listApps() }
                    .onSuccess { onResult(id, it, "") }
                    .onFailure { onResult(id, "", it.message ?: "не удалось получить список приложений") }
            }

            else -> return false
        }
        return true
    }

    /** Действие требует открытого окна: без Activity системный экран не показать. */
    private fun withActivity(id: Int, action: (MainActivity) -> Unit) {
        val host = activity.get()
        if (host == null) {
            onResult(id, "", "[E_NOT_READY] откройте приложение")
            return
        }
        host.runOnUiThread { action(host) }
    }

    private fun openURL(id: Int, url: String) {
        if (url.isEmpty()) {
            onResult(id, "null", "")
            return
        }
        val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url))
            .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val err = runCatching { context.startActivity(intent) }.exceptionOrNull()
        // Браузера может не быть вовсе — на «голой» прошивке это реальность.
        onResult(id, if (err == null) "null" else "", err?.let { "[E_NO_METHOD] открыть ссылку нечем" } ?: "")
    }

    /**
     * Установленные приложения: пакет, читаемое имя, иконка и признак системного.
     *
     * Иконку отдаём data-URL'ом — ровно как это делает выбор процесса на Windows
     * (backend/system/processes_windows.go). Размер 96 px: на экране она 32 px,
     * но при плотности x3 меньшая заметно мылит.
     */
    private fun listApps(): String {
        val pm = context.packageManager
        val self = context.packageName

        // getInstalledPackages(GET_PERMISSIONS) вместо getInstalledApplications плюс
        // checkPermission на каждый пакет: разрешения приезжают тем же ответом, а
        // не двумя сотнями отдельных обращений к системе.
        val packages = pm.getInstalledPackages(PackageManager.GET_PERMISSIONS)

        val rows = ArrayList<JSONObject>(packages.size)
        for (pkg in packages) {
            val info = pkg.applicationInfo ?: continue
            // Себя в списке не показываем: собственный трафик и так идёт мимо
            // туннеля, исключать его повторно нечего.
            if (info.packageName == self) continue
            // Приложение без доступа в интернет исключать не из чего.
            if (pkg.requestedPermissions?.contains(android.Manifest.permission.INTERNET) != true) continue

            val item = JSONObject()
            item.put("package", info.packageName)
            item.put("label", runCatching { pm.getApplicationLabel(info).toString() }
                .getOrDefault(info.packageName))
            item.put("system", (info.flags and ApplicationInfo.FLAG_SYSTEM) != 0)
            item.put("icon", runCatching { iconDataURL(pm.getApplicationIcon(info)) }.getOrDefault(""))
            rows.add(item)
        }

        // По алфавиту: без сортировки система отдаёт пакеты в порядке установки,
        // и найти нужное приложение глазами в таком списке невозможно.
        rows.sortBy { it.optString("label").lowercase() }
        return JSONArray(rows).toString()
    }

    /**
     * Иконка приложения как data-URL.
     *
     * 48 точек — компромисс: на экране иконка занимает 32, но растеризация
     * адаптивных иконок (два слоя плюс маска) стоит дорого, а их тут под две
     * сотни. При 96 сбор списка занимал полтора десятка секунд.
     */
    private fun iconDataURL(drawable: Drawable, size: Int = 48): String {
        val bitmap = Bitmap.createBitmap(size, size, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        drawable.setBounds(0, 0, size, size)
        drawable.draw(canvas)
        val bytes = ByteArrayOutputStream()
        bitmap.compress(Bitmap.CompressFormat.PNG, 100, bytes)
        bitmap.recycle()
        return "data:image/png;base64," +
            Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP)
    }

    /** Первый строковый аргумент вызова («» — аргументов нет). */
    private fun firstString(argsJSON: String): String =
        runCatching { JSONArray(argsJSON).optString(0) }.getOrDefault("")

    // --- EventSink: из Go в WebView ---

    override fun onEvent(name: String, payloadJSON: String) {
        evaluate("window.__mitmEvent && window.__mitmEvent(${quote(name)}, ${quote(payloadJSON)})")
    }

    override fun onResult(id: Int, resultJSON: String, errText: String) {
        evaluate(
            "window.__mitmResolve && window.__mitmResolve($id, ${quote(resultJSON)}, ${quote(errText)})"
        )
    }

    override fun onState(state: String, reason: String) {
        this.state = state
        TunnelService.updateNotification(context, state)
    }

    override fun onSpeed(downSpeed: Long, upSpeed: Long) {
        TunnelService.updateSpeed(context, downSpeed, upSpeed)
    }

    // --- Controller: из Go в систему ---

    /**
     * Поднять туннель. Разрешение на VPN спрашивает система и только у Activity,
     * поэтому без открытого окна поднять соединение нельзя — говорим об этом
     * прямым текстом, а не молчим.
     */
    override fun startTunnel(configJSON: String) {
        pendingConfig = configJSON
        val consent = VpnService.prepare(context)
        if (consent == null) {
            launchService(configJSON)
            return
        }
        val host = activity.get()
            ?: throw IllegalStateException("[E_VPN_CONSENT] нужно разрешение на VPN — откройте приложение")
        host.requestVpnConsent(consent)
    }

    override fun stopTunnel() {
        pendingConfig = null
        context.startService(
            Intent(context, TunnelService::class.java).setAction(TunnelService.ACTION_STOP)
        )
    }

    /** Пользователь ответил на системный диалог согласия. */
    fun onVpnConsent(granted: Boolean) {
        val config = pendingConfig
        pendingConfig = null
        if (!granted || config == null) {
            io.github.mitt776.mobile.Mobile.serviceStop()
            return
        }
        launchService(config)
    }

    private fun launchService(configJSON: String) {
        val intent = Intent(context, TunnelService::class.java)
            .setAction(TunnelService.ACTION_START)
            .putExtra(TunnelService.EXTRA_CONFIG, configJSON)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            context.startForegroundService(intent)
        } else {
            context.startService(intent)
        }
    }

    // --- вспомогательное ---

    private fun evaluate(script: String) {
        val view = webView.get() ?: return
        view.post {
            runCatching { view.evaluateJavascript(script, null) }
                .onFailure { Log.e(TAG, "evaluate", it) }
        }
    }

    /** Экранирование строки в JS-литерал: делаем это через JSON, а не руками. */
    private fun quote(value: String): String = JSONObject.quote(value)

    private companion object {
        const val TAG = "MitM"
    }
}
