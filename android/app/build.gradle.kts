plugins {
    // Kotlin встроен в AGP 9 — отдельный плагин kotlin-android больше не применяется.
    id("com.android.application")
}

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
        // versionCode = major*10000 + minor*100 + patch, см. план порта.
        versionCode = 20100
        versionName = "2.1.0"

        ndk {
            // Релиз — только arm64: 32-битных телефонов практически не осталось, а каждая
            // ABI добавляет к APK ещё одно ядро целиком.
            abiFilters += listOf("arm64-v8a")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
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

tasks.matching { it.name.startsWith("merge") && it.name.endsWith("Assets") }
    .configureEach { dependsOn(copyRuleSets) }

dependencies {
    // mitm.aar — продукт gomobile bind, в git не хранится (см. .gitignore).
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))

    // WebViewAssetLoader: интерфейс из ассетов APK отдаётся под https-происхождением.
    // С file:// Chromium не грузит ES-модули (origin null режется CORS), и экран
    // остаётся пустым без единой ошибки в логе.
    implementation("androidx.webkit:webkit:1.14.0")
}
