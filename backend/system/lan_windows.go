package system

import (
	"net"
	"sort"
)

// tunSubnet — адресация нашего же tun-инбаунда (см. config.buildInbounds).
// Дублируется строкой, а не импортируется: config зависит от rules, и тянуть его
// в system ради одной константы незачем. Разъехаться этим двум местам мешает
// TestTunSubnetMatchesConfig.
var _, tunSubnet, _ = net.ParseCIDR("172.19.0.0/30")

// LANAddresses возвращает IPv4-адреса машины в локальных сетях — то, что нужно
// вписать в настройки прокси на телефоне или планшете, когда прокси открыт
// наружу (settings.AllowLAN).
//
// Живёт в system и только под Windows: на Android net.Interfaces() запрещён
// SELinux (см. mobile/platform_android.go), и общий код на него полагаться не может.
func LANAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if ip := ipnet.IP.To4(); isLANAddress(ip) {
				out = append(out, ip.String())
			}
		}
	}
	sort.Strings(out)
	return out
}

// isLANAddress отбирает адреса, которые имеет смысл показать пользователю.
//
// Отбор нарочно узкий: только частные диапазоны (10/8, 172.16/12, 192.168/16) и
// APIPA (169.254/16 — туда попадает адрес раздачи, пока ICS не выдал свой).
// Публичный адрес интерфейса в список попасть не должен: показать его рядом с
// «пропишите этот адрес» значило бы предложить выставить прокси без пароля в
// интернет. Адрес самого туннеля тоже отсеивается — он существует только внутри
// этой машины, а пользователю нужен тот, по которому до неё дозвонятся соседи.
func isLANAddress(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if !ip.IsPrivate() && !ip.IsLinkLocalUnicast() {
		return false
	}
	return !tunSubnet.Contains(ip)
}
