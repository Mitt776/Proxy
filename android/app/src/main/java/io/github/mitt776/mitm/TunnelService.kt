package io.github.mitt776.mitm

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkCapabilities.NET_CAPABILITY_NOT_METERED
import android.net.NetworkRequest
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import android.os.Process
import android.util.Log
import io.github.mitt776.mobile.Mobile
import io.github.mitt776.mobile.Platform
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetAddress
import java.net.InetSocketAddress
import kotlin.concurrent.thread

/**
 * Туннель. Здесь живёт вся Android-специфика ядра: VpnService выдаёт файловый дескриптор
 * TUN, снимает наши собственные сокеты с маршрутизации через туннель (protect) и держит
 * процесс живым foreground-уведомлением.
 *
 * Само ядро — библиотека внутри этого же процесса (см. mobile/core_android.go): на Android
 * запустить sing-box отдельным процессом, как на Windows, нельзя — /dev/net/tun без рута
 * недоступен.
 */
class TunnelService : VpnService(), Platform {

    companion object {
        const val ACTION_START = "io.github.mitt776.mitm.START"
        const val ACTION_STOP = "io.github.mitt776.mitm.STOP"
        const val EXTRA_CONFIG = "config"

        private const val TAG = "MitM"
        private const val CHANNEL_ID = "tunnel"
        private const val NOTIFICATION_ID = 1

        /**
         * Строки уведомления. Оно живёт вне WebView, поэтому словарями фронтенда
         * (frontend/src/lib/i18n) его не перевести — держим маленькую таблицу
         * здесь, а язык спрашиваем у Go: пользователь мог выбрать его вручную, и
         * с языком системы он не обязан совпадать.
         */
        private val TEXT = mapOf(
            "ru" to mapOf(
                "running" to "Подключено",
                "starting" to "Подключение…",
                "error" to "Ошибка подключения",
                "disconnect" to "Отключить",
                "mb" to "МБ/с", "kb" to "КБ/с", "b" to "Б/с",
            ),
            "en" to mapOf(
                "running" to "Connected",
                "starting" to "Connecting…",
                "error" to "Connection failed",
                "disconnect" to "Disconnect",
                "mb" to "MB/s", "kb" to "KB/s", "b" to "B/s",
            ),
        )

        /** Состояние ядра, от которого пляшет текст уведомления. */
        @Volatile
        private var notificationState: String = "starting"

        /** Скорость в уведомлении; обе нули — строку про скорость не показываем. */
        @Volatile
        private var downSpeed: Long = 0

        @Volatile
        private var upSpeed: Long = 0

        /** Обновить уведомление при смене состояния ядра (зовётся из Bridge). */
        fun updateNotification(context: Context, state: String) {
            when (state) {
                "running", "starting", "error" -> notificationState = state
                else -> return // остановились — уведомление снимет сам сервис
            }
            if (state != "running") {
                downSpeed = 0
                upSpeed = 0
            }
            notify(context)
        }

        /** Обновить скорость в уведомлении. */
        fun updateSpeed(context: Context, down: Long, up: Long) {
            if (down == downSpeed && up == upSpeed) return
            downSpeed = down
            upSpeed = up
            notify(context)
        }

        private fun notify(context: Context) {
            val manager =
                context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            // Уведомления нет, пока сервис не в foreground; тогда обновлять нечего.
            runCatching { manager.notify(NOTIFICATION_ID, buildNotification(context)) }
        }

        fun buildNotification(context: Context): Notification {
            // Язык, имя активного профиля и момент подключения — из Go. Вызов
            // локальный, к Clash API не ходит, поэтому годится и раз в секунду.
            val info = runCatching { JSONObject(Mobile.notificationInfo()) }.getOrNull()
            val words = TEXT[info?.optString("lang").orEmpty()] ?: TEXT["ru"]!!
            val profile = info?.optString("profile").orEmpty()
            val since = info?.optLong("since") ?: 0L

            val status = words[notificationState] ?: notificationState
            val text = buildString {
                if (profile.isNotEmpty()) append(profile).append(" · ")
                append(status)
                if (downSpeed != 0L || upSpeed != 0L) {
                    append(" · ↓ ").append(speed(downSpeed, words))
                    append(" ↑ ").append(speed(upSpeed, words))
                }
            }
            return buildNotification(context, text, words, since)
        }

        private fun buildNotification(
            context: Context,
            text: String,
            words: Map<String, String>,
            since: Long,
        ): Notification {
            val manager =
                context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                manager.createNotificationChannel(
                    NotificationChannel(CHANNEL_ID, "MitM", NotificationManager.IMPORTANCE_LOW)
                )
            }

            val openApp = PendingIntent.getActivity(
                context,
                0,
                Intent(context, MainActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE
            )
            val disconnect = PendingIntent.getService(
                context,
                1,
                Intent(context, TunnelService::class.java).setAction(ACTION_STOP),
                PendingIntent.FLAG_IMMUTABLE
            )

            @Suppress("DEPRECATION")
            val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                Notification.Builder(context, CHANNEL_ID)
            } else {
                Notification.Builder(context)
            }

            builder
                .setContentTitle("MitM")
                .setContentText(text)
                .setSmallIcon(R.drawable.ic_notification)
                .setContentIntent(openApp)
                .setOngoing(true)
                .addAction(
                    Notification.Action.Builder(null, words["disconnect"], disconnect).build()
                )

            // Время подключения считает сама система: свой счётчик в уведомлении
            // пришлось бы перерисовывать каждую секунду даже при нулевом трафике.
            if (since > 0) {
                builder.setWhen(since).setUsesChronometer(true).setShowWhen(true)
            } else {
                builder.setShowWhen(false)
            }
            return builder.build()
        }

        private fun speed(bytes: Long, words: Map<String, String>): String = when {
            bytes >= 1024 * 1024 -> "%.1f %s".format(bytes / 1024.0 / 1024.0, words["mb"])
            bytes >= 1024 -> "%.0f %s".format(bytes / 1024.0, words["kb"])
            else -> "%d %s".format(bytes, words["b"])
        }
    }

    private var tunDescriptor: ParcelFileDescriptor? = null
    private var networkCallback: ConnectivityManager.NetworkCallback? = null

    /** Не-VPN сети в порядке появления; последняя — текущая. */
    private val underlying = LinkedHashSet<Network>()

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Always-on VPN системы стартует сервис с пустым интентом; мы такой режим не
        // поддерживаем и просто корректно уходим, а не падаем.
        val action = intent?.action ?: run {
            stopSelf()
            return START_NOT_STICKY
        }

        when (action) {
            ACTION_START -> {
                val config = intent.getStringExtra(EXTRA_CONFIG).orEmpty()
                startForegroundNotification()
                thread(name = "mitm-core-start") { startCore(config) }
            }

            ACTION_STOP -> thread(name = "mitm-core-stop") { stopCore() }
        }
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        stopCore()
        super.onDestroy()
    }

    /** Пользователь смахнул приложение из недавних или отозвал разрешение на VPN. */
    override fun onRevoke() {
        stopCore()
        super.onRevoke()
    }

    private fun startCore(config: String) {
        try {
            // Слушать сеть начинаем до старта ядра: ему нужен внешний интерфейс уже
            // на первом соединении.
            registerNetworkCallback()
            Mobile.serviceStart(config, this)
        } catch (error: Throwable) {
            Log.e(TAG, "start core", error)
            stopCore()
        }
    }

    private fun stopCore() {
        try {
            Mobile.serviceStop()
        } catch (error: Throwable) {
            Log.e(TAG, "stop core", error)
        }
        unregisterNetworkCallback()
        // Дескриптор закрываем строго после остановки ядра: ядру он ещё нужен, чтобы
        // дочитать очередь.
        tunDescriptor?.close()
        tunDescriptor = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
    }

    // --- Platform: то, что ядро просит у Android -----------------------------

    /**
     * Ядро описало туннель — строим VpnService.Builder и отдаём дескриптор.
     * Вызывается из горутины Go, то есть не с главного потока.
     */
    override fun openTun(optionsJSON: String): Int {
        val options = JSONObject(optionsJSON)
        val builder = Builder()

        builder.setMtu(options.optInt("mtu", 9000))
        builder.setSession("MitM")

        for (address in options.strings("inet4Address") + options.strings("inet6Address")) {
            val (ip, prefix) = address.splitPrefix()
            builder.addAddress(ip, prefix)
        }

        val routes4 = options.strings("inet4RouteAddress")
        val routes6 = options.strings("inet6RouteAddress")
        if (routes4.isEmpty() && routes6.isEmpty()) {
            // autoRoute без явных маршрутов = забрать весь трафик.
            builder.addRoute("0.0.0.0", 0)
            if (options.strings("inet6Address").isNotEmpty()) {
                builder.addRoute("::", 0)
            }
        } else {
            for (route in routes4 + routes6) {
                val (ip, prefix) = route.splitPrefix()
                builder.addRoute(ip, prefix)
            }
        }

        for (server in options.strings("dnsServers")) {
            builder.addDnsServer(server)
        }

        // Приложения «мимо VPN». Себя исключаем всегда: иначе трафик к ноде пойдёт через
        // собственный туннель.
        builder.addDisallowedApplication(packageName)
        for (packageName in options.strings("excludePackage")) {
            runCatching { builder.addDisallowedApplication(packageName) }
        }
        for (packageName in options.strings("includePackage")) {
            runCatching { builder.addAllowedApplication(packageName) }
        }

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            builder.setMetered(false)
        }

        val descriptor = builder.establish() ?: throw IllegalStateException("VPN permission revoked")
        tunDescriptor = descriptor
        return descriptor.fd
    }

    /** Сокет ядра к ноде обязан идти мимо туннеля, иначе получится петля. */
    override fun protectFd(fd: Int) {
        if (!protect(fd)) {
            throw IllegalStateException("protect($fd) failed")
        }
    }

    /**
     * Список сетевых интерфейсов. Из Go его не получить: с Android 11 SELinux запрещает
     * приложению netlink-сокет, net.Interfaces() падает, и ядро отвечает на всё
     * «no available network interface». У java.net путь через ioctl — он разрешён.
     */
    override fun interfaces(): String {
        val manager = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        // Активной считаем не-VPN сеть, а не manager.activeNetwork: при поднятом туннеле
        // активной системе видится наша же tun0, и тип с DNS достались бы ей.
        val activeNetwork = synchronized(underlying) { underlying.lastOrNull() }
        val activeProperties = activeNetwork?.let { manager.getLinkProperties(it) }
        val activeCapabilities = activeNetwork?.let { manager.getNetworkCapabilities(it) }

        val result = JSONArray()
        for (item in java.net.NetworkInterface.getNetworkInterfaces()) {
            val addresses = JSONArray()
            for (address in item.interfaceAddresses) {
                val host = address.address.hostAddress ?: continue
                // hostAddress у IPv6 умеет приписывать зону (fe80::1%wlan0) — ядру она
                // не нужна и ParsePrefix на ней спотыкается.
                addresses.put(host.substringBefore('%') + "/" + address.networkPrefixLength)
            }

            val isActive = activeProperties?.interfaceName == item.name
            val dnsServers = JSONArray()
            if (isActive) {
                activeProperties?.dnsServers?.forEach { dnsServers.put(it.hostAddress) }
            }

            result.put(
                JSONObject()
                    .put("index", item.index)
                    .put("mtu", runCatching { item.mtu }.getOrDefault(1500))
                    .put("name", item.name)
                    .put("addresses", addresses)
                    .put("up", runCatching { item.isUp }.getOrDefault(false))
                    .put("loopback", runCatching { item.isLoopback }.getOrDefault(false))
                    .put("pointToPoint", runCatching { item.isPointToPoint }.getOrDefault(false))
                    .put("multicast", runCatching { item.supportsMulticast() }.getOrDefault(false))
                    .put("type", if (isActive) activeCapabilities.interfaceType() else "other")
                    .put("metered", isActive && activeCapabilities?.hasCapability(NET_CAPABILITY_NOT_METERED) == false)
                    .put("dnsServers", dnsServers)
            )
        }
        return result.toString()
    }

    override fun writeLog(level: Int, message: String) {
        Log.i(TAG, message)
    }

    /**
     * Какое приложение открыло соединение. Ядро на Android спрашивает это всегда —
     * не ответив, оно полезет в netlink, который Android запрещает, и журнал
     * забьётся ошибками на каждом соединении.
     */
    override fun findConnectionOwner(
        ipProtocol: Int,
        sourceAddress: String,
        sourcePort: Int,
        destinationAddress: String,
        destinationPort: Int,
    ): String {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.Q) return "{}"
        val manager = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val uid = runCatching {
            manager.getConnectionOwnerUid(
                ipProtocol,
                InetSocketAddress(InetAddress.getByName(sourceAddress), sourcePort),
                InetSocketAddress(InetAddress.getByName(destinationAddress), destinationPort),
            )
        }.getOrDefault(Process.INVALID_UID)
        if (uid == Process.INVALID_UID) return "{}"

        val name = runCatching { packageManager.getPackagesForUid(uid)?.firstOrNull() }.getOrNull()
        return JSONObject()
            .put("uid", uid)
            .put("package", name.orEmpty())
            .toString()
    }

    // --- смена сети ----------------------------------------------------------

    /**
     * Следим за сетью, по которой ядро пойдёт к ноде.
     *
     * Именно за **не-VPN**: как только туннель поднят, системный «активный» интерфейс — это
     * наш же tun0, и если сообщить его ядру, оно решит, что выходить наружу не через что.
     * Симптом ровно такой: «no available network interface» на каждом соединении при живом
     * туннеле. Поэтому не registerDefaultNetworkCallback, а запрос с NET_CAPABILITY_NOT_VPN.
     */
    private fun registerNetworkCallback() {
        if (networkCallback != null) return
        val manager = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                synchronized(underlying) {
                    underlying.remove(network)
                    underlying.add(network)
                }
                pushBestNetwork(manager)
            }

            override fun onLost(network: Network) {
                synchronized(underlying) { underlying.remove(network) }
                pushBestNetwork(manager)
            }

            override fun onLinkPropertiesChanged(network: Network, properties: android.net.LinkProperties) {
                pushBestNetwork(manager)
            }
        }
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        manager.registerNetworkCallback(request, callback)
        networkCallback = callback
    }

    /**
     * Свежайшая появившаяся не-VPN сеть и есть текущая: Android сначала поднимает новую,
     * и только потом гасит старую.
     */
    private fun pushBestNetwork(manager: ConnectivityManager) {
        val network = synchronized(underlying) { underlying.lastOrNull() }
        if (network == null) {
            Mobile.updateDefaultInterface("", -1)
            return
        }
        // Система должна знать, на чём стоит туннель, — иначе индикатор сети и учёт
        // трафика врут, а при смене Wi-Fi/LTE VPN не перепривязывается.
        runCatching { setUnderlyingNetworks(arrayOf(network)) }

        val name = manager.getLinkProperties(network)?.interfaceName ?: return
        val index = runCatching { java.net.NetworkInterface.getByName(name)?.index ?: -1 }
            .getOrDefault(-1)
        Mobile.updateDefaultInterface(name, index)
    }

    private fun unregisterNetworkCallback() {
        val callback = networkCallback ?: return
        val manager = getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        runCatching { manager.unregisterNetworkCallback(callback) }
        networkCallback = null
    }

    // --- уведомление ---------------------------------------------------------

    private fun startForegroundNotification() {
        startForeground(NOTIFICATION_ID, buildNotification(this))
    }
}

private fun NetworkCapabilities?.interfaceType(): String = when {
    this == null -> "other"
    hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
    hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
    hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "ethernet"
    else -> "other"
}

private fun JSONObject.strings(key: String): List<String> {
    val array: JSONArray = optJSONArray(key) ?: return emptyList()
    return (0 until array.length()).map { array.getString(it) }
}

/** "172.19.0.1/30" -> ("172.19.0.1", 30) */
private fun String.splitPrefix(): Pair<String, Int> {
    val index = lastIndexOf('/')
    if (index < 0) return this to 32
    return substring(0, index) to substring(index + 1).toInt()
}
