//go:build !android

package config

// tunDNSAddress пуст везде, кроме Android: поле `dns_address` понимает только
// форк sing-box-lx, а штатное ядро отвергает конфиг целиком («unknown field»),
// то есть подключение перестало бы работать вовсе. На Windows это и не нужно —
// DNS там забирает себе auto_route/strict_route самого ядра.
const tunDNSAddress = ""
