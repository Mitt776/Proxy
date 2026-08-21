package appcore

import (
	"testing"

	qrgen "github.com/skip2/go-qrcode"
)

// TestQRRoundtrip проверяет, что связка кодер (go-qrcode) + декодер (gozxing)
// работает: сгенерированный QR читается обратно с тем же текстом. Декодируем через
// DecodeQR — тот же путь, которым идут импорт из файла на Windows и снимок камеры
// на Android.
func TestQRRoundtrip(t *testing.T) {
	link := "vless://0060c67b-dea5-4037-bca2-67ac3bf4aab9@example.com:8443?type=xhttp&security=reality#node"

	png, err := qrgen.Encode(link, qrgen.Medium, 320)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	text, err := DecodeQR(png)
	if err != nil {
		t.Fatalf("decode qr: %v", err)
	}
	if text != link {
		t.Fatalf("QR-текст = %q, ожидался %q", text, link)
	}
	t.Log("✅ QR кодируется и читается обратно корректно")
}

// TestDecodeQRRejectsGarbage — на вход может прийти что угодно (не та картинка из
// галереи, случайный кадр с камеры). Ошибка должна быть с кодом, чтобы интерфейс
// показал понятный текст, а не сырое сообщение библиотеки.
func TestDecodeQRRejectsGarbage(t *testing.T) {
	if _, err := DecodeQR([]byte("не картинка")); err == nil {
		t.Fatal("ожидалась ошибка на мусорном вводе")
	} else if got := err.Error(); got[:len("[E_QR_IMAGE]")] != "[E_QR_IMAGE]" {
		t.Fatalf("ошибка без кода E_QR_IMAGE: %s", got)
	}
}
