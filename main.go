package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/pelletier/go-toml/v2"
)

const (
	defaultConfigFile = "config.toml"

	portDHCP = 67
	portPXE  = 4011
)

type Config struct {
	Interface      string     `toml:"interface"`
	FogIP          string     `toml:"fog_ip"`
	BootfileBIOS   string     `toml:"bootfile_bios"`
	BootfileUEFI   string     `toml:"bootfile_uefi"`
	ListenDHCPPort int        `toml:"listen_dhcp_port"`
	ListenPXEPort  int        `toml:"listen_pxe_port"`
	EnablePXEPort  bool       `toml:"enable_pxe_port"`
	BootRules      []BootRule `toml:"boot_rule"`
	ProxyIP        net.IP     `toml:"-"`
}

type BootRule struct {
	Name                string `toml:"name"`
	MACPrefix           string `toml:"mac_prefix"`
	BootfileBIOS        string `toml:"bootfile_bios"`
	BootfileUEFI        string `toml:"bootfile_uefi"`
	normalizedMACPrefix string
}

type bootSelection struct {
	File   string
	Source string
}

func loadConfig(filename string) (*Config, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cfg := Config{
		ListenDHCPPort: portDHCP,
		ListenPXEPort:  portPXE,
		EnablePXEPort:  true,
	}

	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if err := validateConfig(&cfg); err != nil {
		return nil, err
	}

	proxyIP, err := interfaceIPv4(cfg.Interface)
	if err != nil {
		return nil, err
	}
	cfg.ProxyIP = proxyIP

	return &cfg, nil
}

func validateConfig(cfg *Config) error {
	if strings.TrimSpace(cfg.Interface) == "" {
		return errors.New("interface cannot be empty")
	}

	fogIP := net.ParseIP(cfg.FogIP)
	if fogIP == nil || fogIP.To4() == nil {
		return fmt.Errorf("fog_ip must be a valid IPv4 address: %q", cfg.FogIP)
	}

	if strings.TrimSpace(cfg.BootfileBIOS) == "" {
		return errors.New("bootfile_bios cannot be empty")
	}
	if strings.TrimSpace(cfg.BootfileUEFI) == "" {
		return errors.New("bootfile_uefi cannot be empty")
	}

	if cfg.ListenDHCPPort < 1 || cfg.ListenDHCPPort > 65535 {
		return fmt.Errorf("listen_dhcp_port out of range: %d", cfg.ListenDHCPPort)
	}
	if cfg.ListenPXEPort < 1 || cfg.ListenPXEPort > 65535 {
		return fmt.Errorf("listen_pxe_port out of range: %d", cfg.ListenPXEPort)
	}

	for i := range cfg.BootRules {
		rule := &cfg.BootRules[i]
		rule.Name = strings.TrimSpace(rule.Name)
		rule.MACPrefix = strings.TrimSpace(rule.MACPrefix)
		rule.BootfileBIOS = strings.TrimSpace(rule.BootfileBIOS)
		rule.BootfileUEFI = strings.TrimSpace(rule.BootfileUEFI)

		if rule.MACPrefix == "" {
			return fmt.Errorf("boot_rule[%d] mac_prefix cannot be empty", i)
		}
		if rule.BootfileBIOS == "" && rule.BootfileUEFI == "" {
			return fmt.Errorf("boot_rule[%d] must set bootfile_bios, bootfile_uefi, or both", i)
		}

		normalized, err := normalizeMACPrefix(rule.MACPrefix)
		if err != nil {
			return fmt.Errorf("boot_rule[%d] invalid mac_prefix %q: %w", i, rule.MACPrefix, err)
		}
		rule.normalizedMACPrefix = normalized
	}

	return nil
}

func normalizeMACPrefix(value string) (string, error) {
	var builder strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r == ':' || r == '-' || r == '.':
			continue
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r >= 'a' && r <= 'f':
			builder.WriteRune(r)
		default:
			return "", fmt.Errorf("unexpected character %q", r)
		}
	}

	normalized := builder.String()
	if len(normalized) == 0 {
		return "", errors.New("empty prefix")
	}
	if len(normalized)%2 != 0 {
		return "", errors.New("prefix must contain full MAC octets")
	}
	if len(normalized) > 12 {
		return "", errors.New("prefix is longer than a MAC address")
	}

	return normalized, nil
}

func interfaceIPv4(name string) (net.IP, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, fmt.Errorf("cannot find interface %s: %w", name, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("cannot read addresses for interface %s: %w", name, err)
	}

	for _, addr := range addrs {
		var ip net.IP
		switch value := addr.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}

		if ip4 := ip.To4(); ip4 != nil {
			return ip4, nil
		}
	}

	return nil, fmt.Errorf("interface %s has no IPv4 address", name)
}

func makeProxyHandler(cfg *Config, listenPort int) server4.Handler {
	return func(conn net.PacketConn, peer net.Addr, req *dhcpv4.DHCPv4) {
		msgType := req.MessageType()

		switch listenPort {
		case cfg.ListenDHCPPort:
			if msgType != dhcpv4.MessageTypeDiscover && msgType != dhcpv4.MessageTypeRequest {
				if isPXEClient(req) {
					log.Printf("ignored %s: mac=%s peer=%s port=%d", msgType, req.ClientHWAddr, peer, listenPort)
				}
				return
			}
		case cfg.ListenPXEPort:
			if msgType != dhcpv4.MessageTypeRequest {
				if isPXEClient(req) {
					log.Printf("ignored %s: mac=%s peer=%s port=%d", msgType, req.ClientHWAddr, peer, listenPort)
				}
				return
			}
		default:
			if msgType != dhcpv4.MessageTypeDiscover && msgType != dhcpv4.MessageTypeRequest {
				if isPXEClient(req) {
					log.Printf("ignored %s: mac=%s peer=%s port=%d", msgType, req.ClientHWAddr, peer, listenPort)
				}
				return
			}
		}

		if !isPXEClient(req) {
			return
		}

		replyType := dhcpv4.MessageTypeOffer
		if msgType == dhcpv4.MessageTypeRequest {
			replyType = dhcpv4.MessageTypeAck
		}

		reply, err := dhcpv4.NewReplyFromRequest(req)
		if err != nil {
			log.Printf("cannot create reply for %s: %v", req.ClientHWAddr, err)
			return
		}

		fogIP := net.ParseIP(cfg.FogIP).To4()
		selection := selectBootFile(req, cfg)
		bootFile := selection.File

		// ProxyDHCP must not allocate an address. The real DHCP server does that.
		reply.YourIPAddr = net.IPv4zero

		// BOOTP fields used by several PXE firmwares.
		reply.ServerIPAddr = fogIP
		reply.ServerHostName = cfg.FogIP
		reply.BootFileName = bootFile

		// DHCP options used by most PXE/iPXE clients.
		reply.UpdateOption(dhcpv4.OptMessageType(replyType))
		reply.UpdateOption(dhcpv4.OptServerIdentifier(cfg.ProxyIP))
		reply.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient"))
		reply.UpdateOption(dhcpv4.OptTFTPServerName(cfg.FogIP))
		reply.UpdateOption(dhcpv4.OptBootFileName(bootFile))
		reply.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionVendorSpecificInformation, pxeVendorOption43(fogIP)))

		if _, err := conn.WriteTo(reply.ToBytes(), peer); err != nil {
			log.Printf("send failed: mac=%s peer=%s port=%d err=%v", req.ClientHWAddr, peer, listenPort, err)
			return
		}

		log.Printf("sent %s: mac=%s peer=%s port=%d bootfile=%s source=%s arch=%v",
			replyType, req.ClientHWAddr, peer, listenPort, bootFile, selection.Source, req.ClientArch())
	}
}

func isPXEClient(req *dhcpv4.DHCPv4) bool {
	classID := req.ClassIdentifier()
	return strings.HasPrefix(classID, "PXEClient")
}

func selectBootFile(req *dhcpv4.DHCPv4, cfg *Config) bootSelection {
	rule := matchingBootRule(req.ClientHWAddr, cfg.BootRules)
	source := "default"

	// IANA DHCP option 93 architecture values commonly seen with PXE:
	// 0  = Intel x86PC BIOS
	// 6  = EFI IA32
	// 7  = EFI BC
	// 9  = EFI x86-64
	// 11 = EFI ARM64
	for _, arch := range req.ClientArch() {
		switch uint16(arch) {
		case 6, 7, 9, 11:
			if rule != nil && rule.BootfileUEFI != "" {
				return bootSelection{File: rule.BootfileUEFI, Source: ruleSource(rule)}
			}
			return bootSelection{File: cfg.BootfileUEFI, Source: source}
		}
	}

	if rule != nil && rule.BootfileBIOS != "" {
		return bootSelection{File: rule.BootfileBIOS, Source: ruleSource(rule)}
	}
	return bootSelection{File: cfg.BootfileBIOS, Source: source}
}

func matchingBootRule(mac net.HardwareAddr, rules []BootRule) *BootRule {
	normalizedMAC, err := normalizeMACPrefix(mac.String())
	if err != nil {
		return nil
	}

	for i := range rules {
		if strings.HasPrefix(normalizedMAC, rules[i].normalizedMACPrefix) {
			return &rules[i]
		}
	}

	return nil
}

func ruleSource(rule *BootRule) string {
	if rule.Name != "" {
		return "boot_rule:" + rule.Name
	}
	return "boot_rule:" + rule.MACPrefix
}

func pxeVendorOption43(serverIP net.IP) []byte {
	serverIP = serverIP.To4()
	if serverIP == nil {
		return []byte{255}
	}

	return []byte{
		// Sub-option 6: PXE Discovery Control.
		// 7 asks the client to use the PXE_BOOT_SERVERS list and avoid
		// multicast/broadcast discovery.
		6, 1, 7,

		// Sub-option 8: PXE Boot Servers.
		// Type 0, one server, followed by the FOG/PXE server IPv4 address.
		8, 7, 0, 0, 1, serverIP[0], serverIP[1], serverIP[2], serverIP[3],

		// Sub-option 9: PXE Boot Menu.
		// Type 0, layer 2, label "FOG".
		9, 6, 0, 0, 2, 'F', 'O', 'G',

		// Sub-option 10: PXE Menu Prompt.
		// Zero timeout, prompt "FOG".
		10, 4, 0, 'F', 'O', 'G',

		// End.
		255,
	}
}

func startServer(cfg *Config, port int) (*server4.Server, error) {
	addr := &net.UDPAddr{
		IP:   net.IPv4zero,
		Port: port,
	}

	return server4.NewServer(cfg.Interface, addr, makeProxyHandler(cfg, port))
}

func main() {
	configFile := flag.String("config", defaultConfigFile, "Path to TOML configuration file")
	flag.Parse()

	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("starting FOG ProxyDHCP: interface=%s proxy_ip=%s fog_ip=%s dhcp_port=%d pxe_port=%d",
		cfg.Interface, cfg.ProxyIP, cfg.FogIP, cfg.ListenDHCPPort, cfg.ListenPXEPort)

	dhcpServer, err := startServer(cfg, cfg.ListenDHCPPort)
	if err != nil {
		log.Fatalf("cannot listen on UDP/%d interface=%s: %v", cfg.ListenDHCPPort, cfg.Interface, err)
	}

	errCh := make(chan error, 2)

	go func() {
		log.Printf("listening on %s UDP/%d", cfg.Interface, cfg.ListenDHCPPort)
		errCh <- dhcpServer.Serve()
	}()

	if cfg.EnablePXEPort {
		pxeServer, err := startServer(cfg, cfg.ListenPXEPort)
		if err != nil {
			log.Fatalf("cannot listen on UDP/%d interface=%s: %v", cfg.ListenPXEPort, cfg.Interface, err)
		}

		go func() {
			log.Printf("listening on %s UDP/%d", cfg.Interface, cfg.ListenPXEPort)
			errCh <- pxeServer.Serve()
		}()
	}

	if err := <-errCh; err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
