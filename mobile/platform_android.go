//go:build android

package mobile

// Реализация adapter.PlatformInterface — того интерфейса, через который sing-box
// разговаривает с операционной системой. На Windows этой прослойки нет вовсе: там ядро
// живёт отдельным процессом и само открывает /dev/net/tun. На Android TUN выдаёт система
// через VpnService, поэтому ядро линкуется в наш процесс, а всё платформенное приезжает
// сюда из Kotlin.
//
// Сознательно не берём libbox.CommandServer: он тащит gRPC-демона с unix-сокетом ради
// общения с UI, которого у нас нет — интерфейс живёт в этом же процессе.

import (
	"encoding/json"
	"net"
	"net/netip"
	"sync"

	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
)

var _ adapter.PlatformInterface = (*platformImpl)(nil)

type platformImpl struct {
	kt Platform // сторона Kotlin: VpnService и всё, что требует Android API

	networkManager adapter.NetworkManager

	mu               sync.Mutex
	defaultInterface *control.Interface
	monitor          *defaultInterfaceMonitor
	myTunName        string
	myTunAddress     []netip.Addr

	// Kotlin узнаёт про сеть от ConnectivityManager и может сообщить о ней раньше, чем
	// ядро создаст монитор. Держим последнее значение, чтобы применить его после старта:
	// иначе первая же сессия уходит в «no available network interface» и ждёт следующей
	// смены сети, которой может не быть.
	pendingName  string
	pendingIndex int32
	hasPending   bool
}

// applyPending скармливает ядру сеть, о которой Kotlin сообщил до создания монитора.
func (p *platformImpl) applyPending() {
	p.mu.Lock()
	monitor := p.monitor
	name, index, has := p.pendingName, p.pendingIndex, p.hasPending
	p.mu.Unlock()
	if monitor == nil || !has {
		return
	}
	monitor.update(name, index)
}

func (p *platformImpl) Initialize(networkManager adapter.NetworkManager) error {
	p.networkManager = networkManager
	return nil
}

// --- перехват сокетов -------------------------------------------------------
// Каждый исходящий сокет ядра обязан пройти через VpnService.protect(), иначе он уйдёт
// обратно в наш же TUN и получится петля.

func (p *platformImpl) UsePlatformAutoDetectInterfaceControl() bool { return true }

func (p *platformImpl) AutoDetectInterfaceControl(fd int) error {
	return p.kt.ProtectFd(int32(fd))
}

// --- TUN --------------------------------------------------------------------

func (p *platformImpl) UsePlatformInterface() bool { return true }

func (p *platformImpl) ProcessPlatformOptions(options option.TunPlatformOptions) error { return nil }

// --- монитор основного интерфейса -------------------------------------------
// Какой интерфейс сейчас смотрит наружу, знает только Android (ConnectivityManager),
// поэтому монитор пассивный: Kotlin зовёт UpdateDefaultInterface при смене сети.

func (p *platformImpl) UsePlatformDefaultInterfaceMonitor() bool { return true }

func (p *platformImpl) CreateDefaultInterfaceMonitor(logger logger.Logger) tun.DefaultInterfaceMonitor {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.monitor = &defaultInterfaceMonitor{platform: p, logger: logger}
	return p.monitor
}

func (p *platformImpl) UsePlatformNetworkInterfaces() bool { return true }

// networkInterface — то, что присылает Kotlin. Флаги едут булевыми полями, а не сырой
// битовой маской: у Java своя нумерация, и совпадение с syscall.IFF_* было бы случайным.
type networkInterface struct {
	Index        int      `json:"index"`
	MTU          int      `json:"mtu"`
	Name         string   `json:"name"`
	Addresses    []string `json:"addresses"`
	Up           bool     `json:"up"`
	Loopback     bool     `json:"loopback"`
	PointToPoint bool     `json:"pointToPoint"`
	Multicast    bool     `json:"multicast"`
	Type         string   `json:"type"`
	Metered      bool     `json:"metered"`
	DNSServers   []string `json:"dnsServers"`
}

func (p *platformImpl) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	raw, err := p.kt.Interfaces()
	if err != nil {
		return nil, E.Cause(err, "query interfaces")
	}
	var decoded []networkInterface
	if err = json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, E.Cause(err, "decode interfaces")
	}

	interfaces := make([]adapter.NetworkInterface, 0, len(decoded))
	for _, item := range decoded {
		prefixes := make([]netip.Prefix, 0, len(item.Addresses))
		for _, address := range item.Addresses {
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				continue
			}
			prefixes = append(prefixes, prefix)
		}

		var flags net.Flags
		if item.Up {
			flags |= net.FlagUp | net.FlagRunning
		}
		if item.Loopback {
			flags |= net.FlagLoopback
		}
		if item.PointToPoint {
			flags |= net.FlagPointToPoint
		}
		if item.Multicast {
			flags |= net.FlagMulticast
		}

		interfaces = append(interfaces, adapter.NetworkInterface{
			Interface: control.Interface{
				Index:     item.Index,
				MTU:       item.MTU,
				Name:      item.Name,
				Addresses: prefixes,
				Flags:     flags,
			},
			Type:       interfaceType(item.Type),
			DNSServers: item.DNSServers,
			Expensive:  item.Metered,
		})
	}
	return interfaces, nil
}

func interfaceType(name string) C.InterfaceType {
	switch name {
	case "wifi":
		return C.InterfaceTypeWIFI
	case "cellular":
		return C.InterfaceTypeCellular
	case "ethernet":
		return C.InterfaceTypeEthernet
	default:
		return C.InterfaceTypeOther
	}
}

func (p *platformImpl) MyInterfaceAddress() []netip.Addr {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.myTunAddress
}

// --- всё остальное: на Android не применимо ---------------------------------

func (p *platformImpl) UnderNetworkExtension() bool              { return false }
func (p *platformImpl) NetworkExtensionIncludeAllNetworks() bool { return false }
func (p *platformImpl) ClearDNSCache()                           {}
func (p *platformImpl) RequestPermissionForWIFIState() error     { return nil }
func (p *platformImpl) ReadWIFIState() adapter.WIFIState         { return adapter.WIFIState{} }
func (p *platformImpl) UsePlatformWIFIMonitor() bool             { return false }

// UsePlatformConnectionOwnerFinder — обязательно true. Ядро на Android включает
// поиск процесса всегда, когда есть платформенный слой; ответив false, мы отправили
// бы его в netlink, который Android запрещает.
func (p *platformImpl) UsePlatformConnectionOwnerFinder() bool { return true }
func (p *platformImpl) UsePlatformNotification() bool          { return false }
func (p *platformImpl) UsePlatformNeighborResolver() bool      { return false }
func (p *platformImpl) UsePlatformShell() bool                 { return false }
func (p *platformImpl) UsePlatformBridge() bool                { return false }
func (p *platformImpl) TailscaleHostname() string              { return "" }

// connectionOwner — ответ Kotlin: uid и имя пакета, если его удалось определить.
type connectionOwner struct {
	UID     int32  `json:"uid"`
	Package string `json:"package"`
}

func (p *platformImpl) FindConnectionOwner(request *adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	raw, err := p.kt.FindConnectionOwner(
		request.IpProtocol,
		request.SourceAddress, request.SourcePort,
		request.DestinationAddress, request.DestinationPort,
	)
	if err != nil {
		return nil, err
	}
	var decoded connectionOwner
	if err = json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, E.Cause(err, "decode connection owner")
	}
	owner := &adapter.ConnectionOwner{UserId: decoded.UID}
	if decoded.Package != "" {
		// Имя пакета — то же, что показывает Android в списке приложений; ядро
		// отдаёт его в Clash API как процесс.
		owner.ProcessPath = decoded.Package
		owner.AndroidPackageNames = []string{decoded.Package}
	}
	return owner, nil
}

func (p *platformImpl) SendNotification(notification *adapter.Notification) error { return nil }

func (p *platformImpl) StartNeighborMonitor(listener adapter.NeighborUpdateListener) error {
	return E.New("neighbor monitor is not supported on android")
}

func (p *platformImpl) CloseNeighborMonitor(listener adapter.NeighborUpdateListener) error {
	return nil
}

func (p *platformImpl) CheckPlatformShell() error { return E.New("shell is not supported on android") }

func (p *platformImpl) OpenShellSession(user *adapter.PlatformUser, command string, env []string, term string, rows int32, cols int32) (adapter.ShellSession, error) {
	return nil, E.New("shell is not supported on android")
}

func (p *platformImpl) LookupUser(username string) (*adapter.PlatformUser, error) {
	return nil, E.New("user lookup is not supported on android")
}

func (p *platformImpl) LookupSFTPServer() (string, error) {
	return "", E.New("sftp is not supported on android")
}

func (p *platformImpl) ReadSystemSSHHostKey() ([]byte, error) {
	return nil, E.New("ssh host key is not supported on android")
}

func (p *platformImpl) CreateBridge(options adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, E.New("bridge is not supported on android")
}

// --- монитор -----------------------------------------------------------------

var _ tun.DefaultInterfaceMonitor = (*defaultInterfaceMonitor)(nil)

type defaultInterfaceMonitor struct {
	platform     *platformImpl
	logger       logger.Logger
	callbacks    list.List[tun.DefaultInterfaceUpdateCallback]
	myInterfaces []string
	initialized  bool
}

func (m *defaultInterfaceMonitor) Start() error { return nil }
func (m *defaultInterfaceMonitor) Close() error { return nil }

func (m *defaultInterfaceMonitor) DefaultInterface() *control.Interface {
	m.platform.mu.Lock()
	defer m.platform.mu.Unlock()
	return m.platform.defaultInterface
}

func (m *defaultInterfaceMonitor) OverrideAndroidVPN() bool { return false }
func (m *defaultInterfaceMonitor) AndroidVPNEnabled() bool  { return false }

func (m *defaultInterfaceMonitor) RegisterCallback(callback tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	m.platform.mu.Lock()
	defer m.platform.mu.Unlock()
	return m.callbacks.PushBack(callback)
}

func (m *defaultInterfaceMonitor) UnregisterCallback(element *list.Element[tun.DefaultInterfaceUpdateCallback]) {
	m.platform.mu.Lock()
	defer m.platform.mu.Unlock()
	m.callbacks.Remove(element)
}

func (m *defaultInterfaceMonitor) RegisterMyInterface(interfaceName string) {
	m.platform.mu.Lock()
	defer m.platform.mu.Unlock()
	m.myInterfaces = append(m.myInterfaces, interfaceName)
}

func (m *defaultInterfaceMonitor) MyInterfaces() []string {
	m.platform.mu.Lock()
	defer m.platform.mu.Unlock()
	return m.myInterfaces
}

// update зовётся из Kotlin при смене сети; index == -1 означает «сети нет».
func (m *defaultInterfaceMonitor) update(name string, index int32) {
	if m.platform.networkManager != nil {
		if err := m.platform.networkManager.UpdateInterfaces(); err != nil {
			m.logger.Error(E.Cause(err, "update interfaces"))
		}
	}

	m.platform.mu.Lock()
	if index == -1 {
		m.platform.defaultInterface = nil
		m.initialized = true
		callbacks := m.callbacks.Array()
		m.platform.mu.Unlock()
		for _, callback := range callbacks {
			callback(nil, 0)
		}
		return
	}

	oldInterface := m.platform.defaultInterface
	newInterface, err := m.platform.networkManager.InterfaceFinder().ByIndex(int(index))
	if err != nil {
		m.platform.mu.Unlock()
		m.logger.Error(E.Cause(err, "find updated interface: ", name))
		return
	}
	m.platform.defaultInterface = newInterface
	if m.initialized && oldInterface != nil && oldInterface.Name == newInterface.Name && oldInterface.Index == newInterface.Index {
		m.platform.mu.Unlock()
		return
	}
	m.initialized = true
	callbacks := m.callbacks.Array()
	m.platform.mu.Unlock()
	for _, callback := range callbacks {
		callback(newInterface, 0)
	}
}
