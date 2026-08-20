//go:build android

package mobile

// Открытие TUN. Дескриптор создаёт Kotlin через VpnService.Builder.establish(), сюда он
// приезжает числом; параметры туннеля ядро описывает в tun.Options, а мы переводим их в
// JSON — gomobile умеет возить между Go и Kotlin только простые типы, структуры пришлось бы
// биндить поштучно.

import (
	"encoding/json"
	"net/netip"
	"unsafe"

	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	E "github.com/sagernet/sing/common/exceptions"
	"golang.org/x/sys/unix"
)

// tunSpec — то, что видит Kotlin. Имена полей в camelCase, как принято в Kotlin/JS.
type tunSpec struct {
	MTU                      uint32   `json:"mtu"`
	AutoRoute                bool     `json:"autoRoute"`
	StrictRoute              bool     `json:"strictRoute"`
	Inet4Address             []string `json:"inet4Address"`
	Inet6Address             []string `json:"inet6Address"`
	Inet4RouteAddress        []string `json:"inet4RouteAddress"`
	Inet6RouteAddress        []string `json:"inet6RouteAddress"`
	Inet4RouteExcludeAddress []string `json:"inet4RouteExcludeAddress"`
	Inet6RouteExcludeAddress []string `json:"inet6RouteExcludeAddress"`
	DNSServers               []string `json:"dnsServers"`
	IncludePackage           []string `json:"includePackage"`
	ExcludePackage           []string `json:"excludePackage"`
	HTTPProxyEnabled         bool     `json:"httpProxyEnabled"`
	HTTPProxyServer          string   `json:"httpProxyServer"`
	HTTPProxyPort            int      `json:"httpProxyPort"`
}

func (p *platformImpl) OpenInterface(options *tun.Options, platformOptions option.TunPlatformOptions) (tun.Tun, error) {
	// Правила по UID/пользователю Android разруливает VpnService, внутрь ядра они не едут.
	if len(options.IncludeUID) > 0 || len(options.ExcludeUID) > 0 {
		return nil, E.New("platform: unsupported uid options")
	}
	if len(options.IncludeAndroidUser) > 0 {
		return nil, E.New("platform: unsupported android_user option")
	}

	spec := tunSpec{
		MTU:                      options.MTU,
		AutoRoute:                options.AutoRoute,
		StrictRoute:              options.StrictRoute,
		Inet4Address:             prefixStrings(options.Inet4Address),
		Inet6Address:             prefixStrings(options.Inet6Address),
		Inet4RouteAddress:        prefixStrings(options.Inet4RouteAddress),
		Inet6RouteAddress:        prefixStrings(options.Inet6RouteAddress),
		Inet4RouteExcludeAddress: prefixStrings(options.Inet4RouteExcludeAddress),
		Inet6RouteExcludeAddress: prefixStrings(options.Inet6RouteExcludeAddress),
		DNSServers:               addrStrings(options.DNSAddress),
		IncludePackage:           options.IncludePackage,
		ExcludePackage:           options.ExcludePackage,
	}
	if platformOptions.HTTPProxy != nil && platformOptions.HTTPProxy.Enabled {
		spec.HTTPProxyEnabled = true
		spec.HTTPProxyServer = platformOptions.HTTPProxy.Server
		spec.HTTPProxyPort = int(platformOptions.HTTPProxy.ServerPort)
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, E.Cause(err, "encode tun options")
	}

	tunFd, err := p.kt.OpenTun(string(specJSON))
	if err != nil {
		return nil, E.Cause(err, "open tun")
	}

	// Имя интерфейса Android назначает сам (обычно tun0) — спрашиваем у дескриптора.
	options.Name, err = tunnelName(tunFd)
	if err != nil {
		return nil, E.Cause(err, "query tun name")
	}
	options.InterfaceMonitor.RegisterMyInterface(options.Name)

	// Дублируем дескриптор: оригиналом владеет ParcelFileDescriptor на стороне Kotlin,
	// и закрывать его будет он же — иначе получим двойное закрытие при остановке.
	dupFd, err := unix.FcntlInt(uintptr(tunFd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, E.Cause(err, "dup tun file descriptor")
	}
	options.FileDescriptor = dupFd

	p.mu.Lock()
	p.myTunName = options.Name
	p.myTunAddress = tunAddresses(options)
	p.mu.Unlock()

	return tun.New(*options)
}

const ifReqSize = unix.IFNAMSIZ + 64

func tunnelName(fd int32) (string, error) {
	var ifr [ifReqSize]byte
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.TUNGETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		return "", errno
	}
	return unix.ByteSliceToString(ifr[:]), nil
}

func tunAddresses(options *tun.Options) []netip.Addr {
	addresses := make([]netip.Addr, 0, len(options.Inet4Address)+len(options.Inet6Address))
	for _, prefix := range options.Inet4Address {
		addresses = append(addresses, prefix.Addr())
	}
	for _, prefix := range options.Inet6Address {
		addresses = append(addresses, prefix.Addr())
	}
	return addresses
}

func prefixStrings(prefixes []netip.Prefix) []string {
	result := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		result = append(result, prefix.String())
	}
	return result
}

func addrStrings(addresses []netip.Addr) []string {
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		result = append(result, address.String())
	}
	return result
}
