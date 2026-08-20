package io.github.mitt776.mitm

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.text.InputType
import android.view.Gravity
import android.widget.Button
import android.widget.EditText
import android.widget.LinearLayout
import android.widget.TextView
import io.github.mitt776.mobile.Mobile
import kotlin.concurrent.thread

/**
 * Экран этапа 0. Интерфейса здесь нет намеренно: единственная задача — доказать, что
 * связка «наш парсер → наш генератор → sing-box в процессе → VpnService» пропускает
 * настоящий трафик. Ссылка на ноду вводится руками и никуда не сохраняется — в коде и в
 * репозитории её быть не должно.
 */
class MainActivity : Activity() {

    private lateinit var linkInput: EditText
    private lateinit var status: TextView
    private var pendingConfig: String? = null

    private val stateReceiver = object : BroadcastReceiver() {
        override fun onReceive(context: Context?, intent: Intent?) {
            val state = intent?.getStringExtra(TunnelService.EXTRA_STATE).orEmpty()
            val message = intent?.getStringExtra(TunnelService.EXTRA_MESSAGE).orEmpty()
            status.text = if (message.isEmpty()) state else "$state: $message"
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        Mobile.setup(filesDir.absolutePath, cacheDir.absolutePath)

        linkInput = EditText(this).apply {
            hint = "vless://..."
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE
            setSingleLine(false)
            maxLines = 4
            // Чтобы не набирать ссылку руками при отладке:
            //   adb shell am start -n io.github.mitt776.mitm/.MainActivity --es link "vless://..."
            // Ссылка живёт только в поле ввода — ни в коде, ни в файлах её нет.
            intent?.getStringExtra("link")?.let { setText(it) }
        }
        status = TextView(this).apply {
            text = if (Mobile.isRunning()) "running" else "stopped"
            gravity = Gravity.CENTER_HORIZONTAL
        }

        val connect = Button(this).apply {
            text = "Подключить"
            setOnClickListener { connect() }
        }
        val disconnect = Button(this).apply {
            text = "Отключить"
            setOnClickListener {
                startService(Intent(this@MainActivity, TunnelService::class.java).setAction(TunnelService.ACTION_STOP))
            }
        }

        setContentView(LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 96, 48, 48)
            addView(linkInput)
            addView(connect)
            addView(disconnect)
            addView(status)
        })

        // Отладочный автозапуск, чтобы не тыкать в кнопку руками:
        //   adb shell am start -n io.github.mitt776.mitm/.MainActivity --es link "..." --ez autoconnect true
        if (intent?.getBooleanExtra("autoconnect", false) == true) {
            connect()
        }

        val filter = IntentFilter(TunnelService.BROADCAST_STATE)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(stateReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            @Suppress("UnspecifiedRegisterReceiverFlag")
            registerReceiver(stateReceiver, filter)
        }
    }

    override fun onDestroy() {
        runCatching { unregisterReceiver(stateReceiver) }
        super.onDestroy()
    }

    private fun connect() {
        val link = linkInput.text.toString().trim()
        if (link.isEmpty()) {
            status.text = "введи ссылку на ноду"
            return
        }
        status.text = "собираю конфиг…"
        thread(name = "mitm-config") {
            val config = runCatching { Mobile.spikeConfig(link) }
            runOnUiThread {
                config.onFailure { status.text = "конфиг: ${it.message}" }
                config.onSuccess { generated ->
                    pendingConfig = generated
                    // Согласие на VPN спрашивает система; при повторном запуске prepare
                    // вернёт null и диалога не будет.
                    val consent = VpnService.prepare(this)
                    if (consent != null) {
                        startActivityForResult(consent, REQUEST_VPN)
                    } else {
                        startTunnel()
                    }
                }
            }
        }
    }

    @Deprecated("Deprecated in Java")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode != REQUEST_VPN) return
        if (resultCode == RESULT_OK) {
            startTunnel()
        } else {
            status.text = "нет разрешения на VPN"
        }
    }

    private fun startTunnel() {
        val config = pendingConfig ?: return
        pendingConfig = null
        status.text = "запускаю ядро…"
        val intent = Intent(this, TunnelService::class.java)
            .setAction(TunnelService.ACTION_START)
            .putExtra(TunnelService.EXTRA_CONFIG, config)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            startForegroundService(intent)
        } else {
            startService(intent)
        }
    }

    private companion object {
        const val REQUEST_VPN = 1
    }
}
