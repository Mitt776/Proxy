package main

// Импорт профиля из картинки с QR-кодом. Генерация QR и его распознавание живут в
// backend/appcore — они переносимы; здесь только файловый диалог Windows.

import (
	"os"

	"Proxy/backend/appcore"
	"Proxy/backend/profile"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ImportQRImage открывает диалог выбора картинки, распознаёт в ней QR-код и
// создаёт профиль из считанной ссылки.
func (a *App) ImportQRImage() (*profile.Profile, error) {
	if a.core == nil {
		return nil, codedErr(appcore.ErrNotReady, "хранилище не готово")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выберите картинку с QR-кодом",
		Filters: []runtime.FileFilter{
			{DisplayName: "Изображения", Pattern: "*.png;*.jpg;*.jpeg"},
		},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil // отменено
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text, err := appcore.DecodeQR(data)
	if err != nil {
		return nil, err
	}
	return a.core.AddManualProfile("", text)
}
