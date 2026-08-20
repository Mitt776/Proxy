# Подготовка зависимостей Android-сборки.
#
# Форк ядра Leadaxe/sing-box-lx держит свои патчи wireguard-go, sing-tun и gvisor
# гит-сабмодулями. В zip Go-модуля сабмодули не попадают, поэтому один из них приходится
# клонировать руками: без него не собирается protocol/masque, который тянет
# transport/wireguard даже с выключенным тегом with_wireguard.
#
# sing-tun и gvisor берём штатные — форк закрепляет ровно те ревизии, что лежат в его
# go.mod, а его правки касаются поведения (перепривязка при рестарте VPN, guard в gvisor),
# а не API, и на компиляцию не влияют.

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$vendorDir = Join-Path $root "third_party"
$wireguardDir = Join-Path $vendorDir "wireguard-go"

# Коммит закреплён намеренно: ветка lx-awg2-v005 движется, а сборка должна быть
# воспроизводимой.
$wireguardRepo = "https://github.com/Leadaxe/wireguard-go-awg2-lx"
$wireguardCommit = "93b5a4d984f6eb0b3fa151f3ae16b4d2a9396563"

if (-not (Test-Path $vendorDir)) {
    New-Item -ItemType Directory -Path $vendorDir | Out-Null
}

if (-not (Test-Path (Join-Path $wireguardDir ".git"))) {
    Write-Host "Клонирую wireguard-go..."
    git clone --no-checkout $wireguardRepo $wireguardDir
}

Push-Location $wireguardDir
try {
    git fetch --depth 1 origin $wireguardCommit
    git checkout --detach $wireguardCommit
    Write-Host "wireguard-go: $wireguardCommit"
} finally {
    Pop-Location
}

# gomobile нужен для bind; ставится в %USERPROFILE%\go\bin.
if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
    Write-Host "Ставлю gomobile..."
    go install golang.org/x/mobile/cmd/gomobile@latest
    go install golang.org/x/mobile/cmd/gobind@latest
}

Write-Host "Готово."
