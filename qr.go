package main

import (
	"encoding/base64"
	"image"
	_ "image/jpeg" // регистрируем декодеры для image.Decode
	_ "image/png"
	"os"
	"strings"

	"Proxy/backend/profile"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	qrgen "github.com/skip2/go-qrcode"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProfileQR возвращает QR-код профиля как data-URL (PNG в base64) — для переноса
// ноды на телефон. Для подписки кодируем её URL, иначе — первую ссылку профиля.
func (a *App) ProfileQR(id string) (string, error) {
	if a.store == nil {
		return "", codedErr(ErrNotReady, "хранилище не готово")
	}
	p := a.store.Get(id)
	if p == nil {
		return "", codedErr(ErrProfileNotFound, "профиль не найден")
	}

	text := qrPayload(p)
	if text == "" {
		return "", codedErr(ErrQRNothing, "нечего кодировать в QR")
	}

	png, err := qrgen.Encode(text, qrgen.Medium, 320)
	if err != nil {
		return "", codedErrf(ErrQRGenerate, "не удалось сгенерировать QR: %w", err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png), nil
}

// qrPayload выбирает, что кодировать: URL подписки или первую ссылку.
func qrPayload(p *profile.Profile) string {
	if p.Kind == "subscription" && strings.TrimSpace(p.SubURL) != "" {
		return strings.TrimSpace(p.SubURL)
	}
	for _, line := range strings.Split(p.Raw, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s
		}
	}
	return ""
}

// ImportQRImage открывает диалог выбора картинки, распознаёт в ней QR-код и
// создаёт профиль из считанной ссылки.
func (a *App) ImportQRImage() (*profile.Profile, error) {
	if a.store == nil {
		return nil, codedErr(ErrNotReady, "хранилище не готово")
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

	text, err := decodeQRFile(path)
	if err != nil {
		return nil, err
	}
	return a.store.AddManual("", text)
}

// decodeQRFile читает картинку и извлекает текст QR-кода.
func decodeQRFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "", codedErrf(ErrQRImage, "не удалось прочитать картинку: %w", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", codedErrf(ErrQRNotFound, "QR-код на картинке не найден: %w", err)
	}
	return res.GetText(), nil
}
