import java.util.Properties

plugins {
    // Kotlin встроен в AGP 9 — отдельный плагин kotlin-android больше не применяется.
    id("com.android.application")
}

// Ключ подписи и пароли живут в android/local.properties (файл в .gitignore), сам
// keystore — вне репозитория вовсе (%USERPROFILE%\.android-keys\mitm.jks).
// Если ключа нет — конфигурация подписи просто не заводится: у стороннего сборщика
// release выйдет неподписанным, а не свалится на первой же строке.
val keyProps = Properties().apply {
    val file = rootProject.file("local.properties")
    if (file.exists()) file.inputStream().use { load(it) }
}
val keyStoreFile = keyProps.getProperty("mitm.storeFile")?.let { file(it) }?.takeIf { it.exists() }

android {
    namespace = "io.github.mitt776.mitm"
    compileSdk = 36
    // Без явной версии AGP не находит strip из NDK и кладёт libgojni.so с отладочными
    // символами — это сотня лишних мегабайт в APK.
    ndkVersion = "29.0.14206865"

    defaultConfig {
        applicationId = "io.github.mitt776.mitm"
        // Android 7.0. Ниже не опускаемся: VpnService там уже стабилен, а доля устройств
        // мизерна.
        minSdk = 24
        targetSdk = 36
        // Версия бампится в трёх местах сразу (app.go, wails.json и здесь) — см. CLAUDE.md.
        // Здесь руками задаётся только строка: versionCode считается из неё по правилу
        // major*10000 + minor*100 + patch (2.1.0 → 20100). Android сравнивает версии
        // исключительно по этому числу, и не возросшее означает «обновления нет» —
        // разъехаться с versionName ему нельзя.
        versionName = "2.1.1"
        versionCode = versionName!!.split(".").map(String::toInt)
            .let { (major, minor, patch) -> major * 10000 + minor * 100 + patch }

        ndk {
            // Релиз — только arm64: 32-битных телефонов практически не осталось, а каждая
            // ABI добавляет к APK ещё одно ядро целиком.
            abiFilters += listOf("arm64-v8a")
        }
    }

    if (keyStoreFile != null) {
        signingConfigs {
            create("release") {
                storeFile = keyStoreFile
                storePassword = keyProps.getProperty("mitm.storePassword")
                keyAlias = keyProps.getProperty("mitm.keyAlias")
                keyPassword = keyProps.getProperty("mitm.keyPassword")
                // Подпись только схемой v2: она появилась ровно в Android 7.0, ниже
                // которого мы не опускаемся, а старую v1 (JAR) AGP при minSdk 24 всё
                // равно игнорирует, сколько её ни включай.
                enableV2Signing = true
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release")
        }
        debug {
            // Эмулятор x86_64 — единственная причина второй ABI.
            ndk {
                abiFilters.clear()
                abiFilters += listOf("arm64-v8a", "x86_64")
            }
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

// Гео-наборы .srs ядру нужны файлами на диске. В git они не хранятся (как и на
// Windows — лежат в архиве релиза), поэтому забираем их из корневых assets перед
// упаковкой, а не держим вторую копию в дереве Android.
val copyRuleSets = tasks.register<Copy>("copyRuleSets") {
    from(rootProject.file("../assets")) { include("*.srs") }
    into(layout.projectDirectory.dir("src/main/assets/rulesets"))
}

// GPLv3 требует отдать пользователю сам текст лицензии вместе с программой, а не
// ссылку на него: ядро здесь линкуется в APK. Кладём файл в ассеты — раздел
// «О программе» показывает его тем же WebViewAssetLoader, каким отдаётся интерфейс.
val copyLicense = tasks.register<Copy>("copyLicense") {
    from(rootProject.file("LICENSE"))
    into(layout.projectDirectory.dir("src/main/assets"))
    rename { "LICENSE.txt" }
}

// Каталог ассетов читает не только упаковщик: на release туда же заглядывает lint
// (generateReleaseLintVitalReportModel), и без объявленной зависимости Gradle 9 валит
// сборку на «implicit dependency» — на debug этого не видно, там lint-vital не
// запускается вовсе. Поэтому копирование цепляется ко всему, что ассеты читает.
tasks.matching { it.name.contains("assets", true) || it.name.contains("lint", true) }
    .configureEach { dependsOn(copyRuleSets, copyLicense) }

dependencies {
    // mitm.aar — продукт gomobile bind, в git не хранится (см. .gitignore).
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))

    // WebViewAssetLoader: интерфейс из ассетов APK отдаётся под https-происхождением.
    // С file:// Chromium не грузит ES-модули (origin null режется CORS), и экран
    // остаётся пустым без единой ошибки в логе.
    implementation("androidx.webkit:webkit:1.14.0")

    // WindowInsets единым API от Android 7 до 16: отступы под статусбар и полоску
    // жеста интерфейс получает переменными CSS (см. MainActivity.applyInsets), а
    // платформенный WindowInsets менял форму трижды за эти версии.
    implementation("androidx.core:core:1.15.0")

    // CameraX — только ради сканера QR. Сам распознаватель наш (Go, backend/appcore),
    // поэтому ML Kit и его зависимость от сервисов Google в APK не тянутся.
    // camera-lifecycle требует LifecycleOwner, отсюда androidx.activity: обычная
    // android.app.Activity им не является.
    val camerax = "1.4.2"
    implementation("androidx.camera:camera-camera2:$camerax")
    implementation("androidx.camera:camera-lifecycle:$camerax")
    implementation("androidx.camera:camera-view:$camerax")
    implementation("androidx.activity:activity:1.9.3")
}
