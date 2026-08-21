# Сборка Android-части: gomobile bind + Gradle.
#
# Скрипт, а не строчка в README: команда bind несёт теги протоколов и ldflags с
# версией ядра, и разъехавшись, они дают либо неподдерживаемый транспорт, либо
# «ядро unknown» в разделе «О программе».
#
#   .\scripts\android-build.ps1            # отладочный APK (arm64 + x86_64)
#   .\scripts\android-build.ps1 -Release   # релизный (только arm64)
#   .\scripts\android-build.ps1 -SkipBind  # только Gradle, если Go не менялся

param(
    [switch]$Release,
    [switch]$SkipBind
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$mobileDir = Join-Path $root "mobile"
$androidDir = Join-Path $root "android"
$frontendDir = Join-Path $root "frontend"

# Теги ядра. with_xhttp — ради форка, остальное нужно нашим протоколам:
# gvisor для TUN, quic для hysteria2/tuic, utls для отпечатков, clash_api для
# списка нод, задержек и режимов.
$tags = "with_gvisor,with_quic,with_utls,with_clash_api,with_xhttp"

if (-not $SkipBind) {
    # Версию ядра тянем из replace-директивы: иначе она живёт в двух местах и
    # в «О программе» показывается не то, что реально собрано.
    $replace = Select-String -Path (Join-Path $mobileDir "go.mod") `
        -Pattern 'replace github.com/sagernet/sing-box => \S+ (\S+)'
    if (-not $replace) { throw "не нашёл версию ядра в mobile/go.mod" }
    $coreVersion = $replace.Matches[0].Groups[1].Value.TrimStart('v')
    Write-Host "ядро: $coreVersion"

    if (-not $env:ANDROID_NDK_HOME) {
        $ndkRoot = Join-Path $env:LOCALAPPDATA "Android\Sdk\ndk"
        $ndk = Get-ChildItem $ndkRoot -Directory | Sort-Object Name -Descending | Select-Object -First 1
        if (-not $ndk) { throw "NDK не найден в $ndkRoot" }
        $env:ANDROID_NDK_HOME = $ndk.FullName
    }
    if (-not $env:ANDROID_HOME) {
        $env:ANDROID_HOME = Join-Path $env:LOCALAPPDATA "Android\Sdk"
    }

    # Отладка ставится и на эмулятор, поэтому там обе ABI; в релизе только arm64 —
    # каждая ABI добавляет к APK ещё одно ядро целиком (~46 МБ).
    $targets = if ($Release) { "android/arm64" } else { "android/arm64,android/amd64" }

    Write-Host "gomobile bind ($targets)..."
    Push-Location $mobileDir
    try {
        $env:GOFLAGS = "-mod=mod"
        gomobile bind `
            "-target=$targets" `
            -androidapi 24 `
            -javapkg io.github.mitt776 `
            -tags $tags `
            -ldflags "-X github.com/sagernet/sing-box/constant.Version=$coreVersion" `
            -o (Join-Path $androidDir "app\libs\mitm.aar") .
        if ($LASTEXITCODE -ne 0) { throw "gomobile bind завершился с кодом $LASTEXITCODE" }
    } finally {
        Pop-Location
    }
}

Write-Host "сборка интерфейса..."
Push-Location $frontendDir
try {
    npm run build:mobile
    if ($LASTEXITCODE -ne 0) { throw "сборка фронтенда завершилась с кодом $LASTEXITCODE" }
} finally {
    Pop-Location
}

Write-Host "gradle..."
Push-Location $androidDir
try {
    $task = if ($Release) { "assembleRelease" } else { "assembleDebug" }
    & (Join-Path $androidDir "gradlew.bat") $task
    if ($LASTEXITCODE -ne 0) { throw "gradle завершился с кодом $LASTEXITCODE" }
} finally {
    Pop-Location
}

Write-Host "готово."
