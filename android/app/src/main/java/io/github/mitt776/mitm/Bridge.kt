package io.github.mitt776.mitm

import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.util.Log
import android.webkit.JavascriptInterface
import android.webkit.WebView
import io.github.mitt776.mobile.Controller
import io.github.mitt776.mobile.EventSink
import org.json.JSONObject
import java.lang.ref.WeakReference

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
