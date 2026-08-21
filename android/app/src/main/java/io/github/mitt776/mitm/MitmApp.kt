package io.github.mitt776.mitm

import android.app.Application
import android.util.Log
import io.github.mitt776.mobile.Mobile
import java.io.File
import java.util.Locale

/**
 * Точка сборки приложения. Go-ядро поднимается один раз на процесс — раньше, чем
 * появится окно: сервис туннеля может пережить Activity, и состояние должно
 * храниться не в ней.
 */
class MitmApp : Application() {

    lateinit var bridge: Bridge
        private set

    override fun onCreate() {
        super.onCreate()
        instance = this

        val assetsDir = File(filesDir, "assets")
        unpackAssets(assetsDir)

        bridge = Bridge(this)
        try {
            Mobile.start(
                filesDir.absolutePath,
                assetsDir.absolutePath,
                systemLang(),
                bridge,
                bridge,
            )
        } catch (error: Throwable) {
            Log.e(TAG, "start core", error)
        }
    }

    /**
     * Наборы правил (.srs) лежат в APK и нужны ядру файлами на диске. Копируем при
     * первом запуске и после обновления: versionCode меняется — значит файлы могли
     * поменяться вместе с ним.
     */
    private fun unpackAssets(target: File) {
        val stamp = File(target, ".version")
        val version = packageManager.getPackageInfo(packageName, 0).longVersionCode.toString()
        if (stamp.takeIf { it.isFile }?.readText() == version) return

        target.mkdirs()
        val names = assets.list("rulesets").orEmpty()
        for (name in names) {
            runCatching {
                assets.open("rulesets/$name").use { input ->
                    File(target, name).outputStream().use(input::copyTo)
                }
            }.onFailure { Log.e(TAG, "unpack $name", it) }
        }
        stamp.writeText(version)
    }

    /** Язык системы; используется, пока пользователь не выбрал язык сам. */
    private fun systemLang(): String =
        if (Locale.getDefault().language.lowercase() == "ru") "ru" else "en"

    companion object {
        private const val TAG = "MitM"

        lateinit var instance: MitmApp
            private set
    }
}
