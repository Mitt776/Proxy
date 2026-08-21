package appcore

import (
	"hash/crc32"
	"strings"
	"testing"

	"Proxy/backend/rules"
)

// Адрес выпуска приезжает в ответе GitHub, а уходит в систему как ACTION_VIEW
// (Android) или BrowserOpenURL (Windows) — то есть содержимое чужого ответа
// решает, что откроется по нажатию пользователя. Подменить ответ api.github.com
// непросто, но проверка стоит три строки, а промах открывает произвольную ссылку
// от нашего имени.
func TestReleasePageURL(t *testing.T) {
	const fallback = "https://github.com/Mitt776/Proxy/releases"

	keep := []string{
		"https://github.com/Mitt776/Proxy/releases/tag/v2.1.0",
		"https://GitHub.com/Mitt776/Proxy/releases/tag/v2.1.0",
	}
	for _, u := range keep {
		if got := releasePageURL(u); got != u {
			t.Errorf("releasePageURL(%q) = %q, ожидался тот же адрес", u, got)
		}
	}

	replace := []string{
		"http://github.com/Mitt776/Proxy/releases/tag/v1", // без TLS
		"https://evil.example/releases",                   // чужой хост
		"https://github.com.evil.example/releases",        // хост лишь начинается на github.com
		"javascript:alert(1)",                             // не веб-ссылка вовсе
		"intent://scan/#Intent;scheme=zxing;end",          //
		"",                                                //
		"://",                                             // не разбирается
	}
	for _, u := range replace {
		if got := releasePageURL(u); got != fallback {
			t.Errorf("releasePageURL(%q) = %q, ожидался фолбэк", u, got)
		}
	}
}

// Набор правил решает, что пойдёт мимо туннеля: подменив список по дороге,
// посредник отправляет «напрямую» ровно те домены, ради которых туннель и
// поднимали. Внешне при этом не меняется ничего.
func TestRequireSecureSetURL(t *testing.T) {
	// Локальному .srs качать нечего — его адрес не проверяется.
	local := rules.RuleSet{Tag: "geoip-ru", Type: rules.SetLocal}
	if err := requireSecureSetURL(local); err != nil {
		t.Errorf("локальный набор отвергнут: %v", err)
	}

	good := rules.RuleSet{Tag: "ads", Type: rules.SetRemote, URL: "https://example.com/list.srs"}
	if err := requireSecureSetURL(good); err != nil {
		t.Errorf("https-набор отвергнут: %v", err)
	}

	for _, u := range []string{"http://example.com/list.srs", "ftp://example.com/list.srs", "example.com", ""} {
		rs := rules.RuleSet{Tag: "ads", Type: rules.SetRemote, URL: u}
		err := requireSecureSetURL(rs)
		if err == nil {
			t.Errorf("набор с адресом %q принят, ожидался отказ", u)
			continue
		}
		if !strings.Contains(err.Error(), ErrInsecureURL) {
			t.Errorf("ошибка для %q без кода %s: %v", u, ErrInsecureURL, err)
		}
	}
}

// Картинку с QR пользователь берёт из галереи, то есть она может приехать
// откуда угодно. Заголовок PNG объявляет размеры до распаковки — на этом и
// ловим бомбу, иначе пара килобайт файла превращается в гигабайты памяти и
// уносит процесс вместе с туннелем.
func TestDecodeQRRejectsHugeImage(t *testing.T) {
	_, err := DecodeQR(hugePNGHeader())
	if err == nil {
		t.Fatal("картинка 30000×30000 принята, ожидался отказ")
	}
	if !strings.Contains(err.Error(), ErrQRImage) {
		t.Errorf("ошибка без кода %s: %v", ErrQRImage, err)
	}
	// Отказать мог и сам декодер, не дойдя до проверки размеров, — тогда тест
	// ничего бы не доказывал. Сверяем, что сработал именно предел.
	if !strings.Contains(err.Error(), "слишком большая") {
		t.Errorf("отказ не от проверки размеров: %v", err)
	}
}

// hugePNGHeader собирает PNG, который объявляет 30000×30000 и на этом
// заканчивается: до пиксельных данных дело дойти не должно — размеры отвергаются
// раньше. Ровно так выглядит бомба: файл на полсотни байт, распаковка на 3,6 ГБ.
//
// CRC считаем настоящий: декодер PNG в Go его сверяет, и на битой контрольной
// сумме заголовок отвалился бы сам — тест проходил бы, ничего не проверяя.
func hugePNGHeader() []byte {
	const side = 30000

	body := []byte{'I', 'H', 'D', 'R'}
	for i := 0; i < 2; i++ {
		body = append(body, byte(side>>24&0xff), byte(side>>16&0xff), byte(side>>8&0xff), byte(side&0xff))
	}
	body = append(body, 8, 6, 0, 0, 0) // 8 бит на канал, RGBA, без интерлейса

	out := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	out = append(out, 0, 0, 0, 13) // длина данных IHDR
	out = append(out, body...)

	sum := crc32.ChecksumIEEE(body)
	return append(out, byte(sum>>24), byte(sum>>16), byte(sum>>8), byte(sum))
}
