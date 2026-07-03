package core

import (
	"os"
	"path/filepath"
)

// Paths хранит разрешённые пути к бинарникам ядра, geo-базам и рабочим данным.
//
// Логика портативности:
//   - Ассеты (sing-box.exe, wintun.dll, geo-базы) ищутся сначала рядом с exe
//     (папка assets или сам каталог exe), затем в ./assets относительно рабочего
//     каталога — это покрывает режим `wails dev`, где cwd = корень проекта.
//   - Рабочие данные (config.json, профили, кэш, логи) пишутся в подпапку data/
//     рядом с exe; если каталог только для чтения — фолбэк на %LOCALAPPDATA%\Proxy.
type Paths struct {
	AssetsDir string // где лежат sing-box.exe, wintun.dll, geoip.db, geosite.db
	DataDir   string // куда пишем config.json, профили, кэш, логи

	SingBox string // полный путь к sing-box.exe
	Wintun  string // полный путь к wintun.dll
	GeoIP   string // полный путь к geoip.db
	GeoSite string // полный путь к geosite.db

	ConfigPath string // сгенерированный config.json
}

const appName = "Proxy"

// ResolvePaths вычисляет пути один раз при старте приложения.
func ResolvePaths() (*Paths, error) {
	assetsDir := resolveAssetsDir()
	dataDir, err := resolveDataDir()
	if err != nil {
		return nil, err
	}

	p := &Paths{
		AssetsDir:  assetsDir,
		DataDir:    dataDir,
		SingBox:    filepath.Join(assetsDir, "sing-box.exe"),
		Wintun:     filepath.Join(assetsDir, "wintun.dll"),
		GeoIP:      filepath.Join(assetsDir, "geoip.db"),
		GeoSite:    filepath.Join(assetsDir, "geosite.db"),
		ConfigPath: filepath.Join(dataDir, "config.json"),
	}
	return p, nil
}

// resolveAssetsDir перебирает кандидатов и возвращает первый, где есть sing-box.exe.
// Если ни один не подошёл — возвращает каталог рядом с exe как разумный дефолт.
func resolveAssetsDir() string {
	var candidates []string

	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "assets"),
			exeDir,
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "assets"),
			cwd,
		)
	}

	for _, dir := range candidates {
		if fileExists(filepath.Join(dir, "sing-box.exe")) {
			return dir
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return "assets"
}

// resolveDataDir возвращает каталог для записи данных, создавая его при необходимости.
func resolveDataDir() (string, error) {
	// 1) Портативный режим: папка data/ рядом с exe.
	if exe, err := os.Executable(); err == nil {
		portable := filepath.Join(filepath.Dir(exe), "data")
		if isWritableDir(portable) {
			return portable, nil
		}
	}
	// 2) Фолбэк: %LOCALAPPDATA%\Proxy.
	if base, err := os.UserConfigDir(); err == nil {
		fallback := filepath.Join(base, appName)
		if err := os.MkdirAll(fallback, 0o755); err == nil {
			return fallback, nil
		}
	}
	// 3) Последний шанс: временный каталог.
	tmp := filepath.Join(os.TempDir(), appName)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return "", err
	}
	return tmp, nil
}

// EnsureWintun кладёт wintun.dll в рабочий каталог ядра (DataDir). sing-box ищет
// библиотеку рядом с exe или в текущем каталоге; для альтернативного ядра вне
// assets только так TUN найдёт wintun. Для встроенного ядра это тоже безвредно.
func (p *Paths) EnsureWintun() {
	dst := filepath.Join(p.DataDir, "wintun.dll")
	if fileExists(dst) || !fileExists(p.Wintun) {
		return
	}
	if data, err := os.ReadFile(p.Wintun); err == nil {
		_ = os.WriteFile(dst, data, 0o644)
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// isWritableDir создаёт каталог (если нужно) и проверяет, что в него можно писать.
func isWritableDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, ".write_test")
	f, err := os.Create(probe)
	if err != nil {
		return false
	}
	_ = f.Close()
	_ = os.Remove(probe)
	return true
}
