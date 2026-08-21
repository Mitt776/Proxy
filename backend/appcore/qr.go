package appcore

import (
	"bytes"
	"encoding/base64"
	"image"
	_ "image/jpeg" // регистрируем декодеры для image.Decode
	_ "image/png"
	"strings"

	"Proxy/backend/profile"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	qrgen "github.com/skip2/go-qrcode"
)

// ProfileQR возвращает QR-код профиля как data-URL (PNG в base64) — для переноса
// ноды на телефон. Для подписки кодируем её URL, иначе — первую ссылку профиля.
func (c *Core) ProfileQR(id string) (string, error) {
	if c.profiles == nil {
		return "", CodedErr(ErrNotReady, "хранилище не готово")
	}
	p := c.profiles.Get(id)
	if p == nil {
		return "", CodedErr(ErrProfileNotFound, "профиль не найден")
	}

	text := qrPayload(p)
	if text == "" {
		return "", CodedErr(ErrQRNothing, "нечего кодировать в QR")
	}

	png, err := qrgen.Encode(text, qrgen.Medium, 320)
	if err != nil {
		return "", CodedErrf(ErrQRGenerate, "не удалось сгенерировать QR: %w", err)
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

// DecodeQR извлекает текст QR-кода из картинки. Платформа сама решает, откуда её
// взять: файловый диалог на Windows, галерея на Android.
func DecodeQR(data []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", CodedErrf(ErrQRImage, "не удалось прочитать картинку: %w", err)
	}
	return decodeImage(img)
}

// DecodeQRGray читает QR прямо из плоскости яркости — так кадр с камеры приходит
// от CameraX (YUV_420_888, плоскость Y).
//
// Смысл в том, чтобы не гонять каждый кадр через JPEG: кодирование в Kotlin и
// обратное декодирование здесь — это десятки миллисекунд и мусор в куче на
// каждый из нескольких кадров в секунду, а распознавателю нужна ровно яркость,
// цвет он всё равно отбрасывает.
//
// stride — длина строки в байтах; у камеры она обычно больше ширины кадра, и
// без учёта выравнивания картинка «съезжает» по диагонали.
func DecodeQRGray(data []byte, width, height, stride int) (string, error) {
	if width <= 0 || height <= 0 || stride < width || len(data) < stride*(height-1)+width {
		return "", CodedErr(ErrQRImage, "кадр камеры пришёл повреждённым")
	}
	img := &image.Gray{
		Pix:    data,
		Stride: stride,
		Rect:   image.Rect(0, 0, width, height),
	}
	return decodeImage(img)
}

func decodeImage(img image.Image) (string, error) {
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", CodedErrf(ErrQRNotFound, "QR-код на картинке не найден: %w", err)
	}
	return res.GetText(), nil
}
