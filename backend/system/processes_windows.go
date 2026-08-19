package system

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"sort"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ProcessInfo — запись для пикера процессов в UI. Иконка отдаётся сразу
// data-URL'ом: фронтенд подставляет её в <img src> без отдельного запроса.
type ProcessInfo struct {
	Name string `json:"name"` // chrome.exe — то, что попадёт в правило
	Path string `json:"path"` // полный путь (пусто, если недоступен)
	PID  int    `json:"pid"`  // PID любого из экземпляров (для отладки)
	Icon string `json:"icon"` // data:image/png;base64,... либо пусто
}

// ListProcesses перечисляет запущенные процессы, схлопывая одноимённые
// (у браузера их десятки, а правило всё равно одно) и добавляя иконку exe.
func ListProcesses() ([]ProcessInfo, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil, err
	}

	byName := map[string]*ProcessInfo{}
	for {
		name := windows.UTF16ToString(entry.ExeFile[:])
		pid := int(entry.ProcessID)
		if name != "" && pid > 4 { // 0/4 — System Idle и System, правилам не нужны
			key := strings.ToLower(name)
			if p, ok := byName[key]; !ok {
				byName[key] = &ProcessInfo{Name: name, PID: pid, Path: processPath(entry.ProcessID)}
			} else if p.Path == "" {
				// Первый экземпляр мог быть недоступен по правам — пробуем ещё.
				p.Path = processPath(entry.ProcessID)
			}
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break // ERROR_NO_MORE_FILES — конец списка
		}
	}

	out := make([]ProcessInfo, 0, len(byName))
	for _, p := range byName {
		if p.Path != "" {
			p.Icon = iconDataURL(p.Path)
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// processPath возвращает полный путь к исполняемому файлу процесса.
// Для системных процессов и процессов другого пользователя вернёт пустую
// строку — это нормально, правило можно построить и по одному имени.
func processPath(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)

	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// --- иконки ---

var (
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	user32                 = windows.NewLazySystemDLL("user32.dll")
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	procExtractIconExW     = shell32.NewProc("ExtractIconExW")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procGetIconInfo        = user32.NewProc("GetIconInfo")
	procGetObject          = gdi32.NewProc("GetObjectW")
	procGetDIBits          = gdi32.NewProc("GetDIBits")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
)

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  windows.Handle
	hbmColor windows.Handle
}

type bitmap struct {
	bmType       int32
	bmWidth      int32
	bmHeight     int32
	bmWidthBytes int32
	bmPlanes     uint16
	bmBitsPixel  uint16
	bmBits       uintptr
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

// iconDataURL достаёт иконку исполняемого файла и кодирует её в data-URL с PNG.
// Любая осечка (нет иконки, нет прав, экзотический формат) — пустая строка:
// пикер просто покажет запись без картинки.
func iconDataURL(exePath string) string {
	img := extractIcon(exePath)
	if img == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func extractIcon(exePath string) image.Image {
	path, err := windows.UTF16PtrFromString(exePath)
	if err != nil {
		return nil
	}
	var large windows.Handle
	// Берём только большую иконку (обычно 32×32): маленькая мылит на HiDPI.
	ret, _, _ := procExtractIconExW.Call(uintptr(unsafe.Pointer(path)), 0,
		uintptr(unsafe.Pointer(&large)), 0, 1)
	if ret == 0 || large == 0 {
		return nil
	}
	defer procDestroyIcon.Call(uintptr(large))

	var info iconInfo
	if r, _, _ := procGetIconInfo.Call(uintptr(large), uintptr(unsafe.Pointer(&info))); r == 0 {
		return nil
	}
	if info.hbmColor != 0 {
		defer procDeleteObject.Call(uintptr(info.hbmColor))
	}
	if info.hbmMask != 0 {
		defer procDeleteObject.Call(uintptr(info.hbmMask))
	}
	if info.hbmColor == 0 {
		return nil // чёрно-белая иконка через маску — редкость, не возимся
	}
	return bitmapToImage(info.hbmColor)
}

// bitmapToImage вытаскивает пиксели HBITMAP как 32-битный BGRA и переводит их
// в image.RGBA (у GDI строки идут снизу вверх, поэтому отражаем по вертикали).
func bitmapToImage(hbm windows.Handle) image.Image {
	var bm bitmap
	if r, _, _ := procGetObject.Call(uintptr(hbm), unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return nil
	}
	if bm.bmWidth <= 0 || bm.bmHeight <= 0 || bm.bmWidth > 512 || bm.bmHeight > 512 {
		return nil
	}

	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return nil
	}
	defer procDeleteDC.Call(hdc)

	var bi bitmapInfo
	bi.header.biSize = uint32(unsafe.Sizeof(bi.header))
	bi.header.biWidth = bm.bmWidth
	bi.header.biHeight = bm.bmHeight
	bi.header.biPlanes = 1
	bi.header.biBitCount = 32
	bi.header.biCompression = 0 // BI_RGB

	pixels := make([]byte, int(bm.bmWidth)*int(bm.bmHeight)*4)
	r, _, _ := procGetDIBits.Call(hdc, uintptr(hbm), 0, uintptr(bm.bmHeight),
		uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), 0) // DIB_RGB_COLORS
	if r == 0 {
		return nil
	}

	w, h := int(bm.bmWidth), int(bm.bmHeight)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	opaque := false
	for y := 0; y < h; y++ {
		src := (h - 1 - y) * w * 4 // строки DIB идут снизу вверх
		for x := 0; x < w; x++ {
			i := src + x*4
			b, g, rr, a := pixels[i], pixels[i+1], pixels[i+2], pixels[i+3]
			if a != 0 {
				opaque = true
			}
			img.SetRGBA(x, y, color.RGBA{R: rr, G: g, B: b, A: a})
		}
	}
	// Старые 24-битные иконки приходят с нулевой альфой — иначе картинка
	// оказалась бы полностью прозрачной.
	if !opaque {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	return img
}

// ExeName возвращает имя исполняемого файла из полного пути — то, что кладётся
// в правило по имени процесса.
func ExeName(path string) string {
	return filepath.Base(path)
}
