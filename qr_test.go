package main

import (
	"bytes"
	"image"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	qrgen "github.com/skip2/go-qrcode"
)

// TestQRRoundtrip проверяет, что связка кодер (go-qrcode) + декодер (gozxing)
// работает: сгенерированный QR читается обратно с тем же текстом.
func TestQRRoundtrip(t *testing.T) {
	link := "vless://0060c67b-dea5-4037-bca2-67ac3bf4aab9@example.com:8443?type=xhttp&security=reality#node"

	png, err := qrgen.Encode(link, qrgen.Medium, 320)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatalf("bitmap: %v", err)
	}
	res, err := qrcode.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		t.Fatalf("decode qr: %v", err)
	}
	if res.GetText() != link {
		t.Fatalf("QR-текст = %q, ожидался %q", res.GetText(), link)
	}
	t.Log("✅ QR кодируется и читается обратно корректно")
}
