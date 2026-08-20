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
import android.util.Log
import io.github.mitt776.mobile.Mobile
import io.github.mitt776.mobile.Platform
import org.json.JSONArray
import org.json.JSONObject
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

        const val BROADCAST_STATE = "io.github.mitt776.mitm.STATE"
        const val EXTRA_STATE = "state"
        const val EXTRA_MESSAGE = "message"

        private const val TAG = "MitM"
        private const val CHANNEL_ID = "tunnel"
        private const val NOTIFICATION_ID = 1
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
            Mobile.setup(filesDir.absolutePath, cacheDir.absolutePath)
            registerNetworkCallback()
            Mobile.start(config, this)
            broadcast("running", "")
        } catch (error: Throwable) {
            Log.e(TAG, "start core", error)
            broadcast("error", error.message ?: error.toString())
            stopCore()
        }
    }

    private fun stopCore() {
        try {
            Mobile.stop()
        } catch (error: Throwable) {
            Log.e(TAG, "stop core", error)
        }
        unregisterNetworkCallback()
        // Дескриптор закрываем строго после остановки ядра: ядру он ещё нужен, чтобы
        // дочитать очередь.
        tunDescriptor?.close()
        tunDescriptor = null
        broadcast("stopped", "")
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
        val manager = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            manager.createNotificationChannel(
                NotificationChannel(CHANNEL_ID, "MitM", NotificationManager.IMPORTANCE_LOW)
            )
        }

        val openApp = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
        )

        @Suppress("DEPRECATION")
        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(this, CHANNEL_ID)
        } else {
            Notification.Builder(this)
        }

        val notification = builder
            .setContentTitle("MitM")
            .setContentText("Подключено")
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setContentIntent(openApp)
            .setOngoing(true)
            .build()

        startForeground(NOTIFICATION_ID, notification)
    }

    private fun broadcast(state: String, message: String) {
        sendBroadcast(
            Intent(BROADCAST_STATE)
                .setPackage(packageName)
                .putExtra(EXTRA_STATE, state)
                .putExtra(EXTRA_MESSAGE, message)
        )
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
