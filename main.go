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
	Interface      string `toml:"interface"`
	FogIP          string `toml:"fog_ip"`
	BootfileBIOS   string `toml:"bootfile_bios"`
	BootfileUEFI   string `toml:"bootfile_uefi"`
	ListenDHCPPort int    `toml:"listen_dhcp_port"`
	ListenPXEPort  int    `toml:"listen_pxe_port"`
	EnablePXEPort  bool   `toml:"enable_pxe_port"`
	ProxyIP        net.IP `toml:"-"`
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

	return nil
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
		bootFile := selectBootFile(req, cfg)

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
		reply.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionVendorSpecificInformation, pxeVendorOption43()))

		if _, err := conn.WriteTo(reply.ToBytes(), peer); err != nil {
			log.Printf("send failed: mac=%s peer=%s port=%d err=%v", req.ClientHWAddr, peer, listenPort, err)
			return
		}

		log.Printf("sent %s: mac=%s peer=%s port=%d bootfile=%s arch=%v",
			replyType, req.ClientHWAddr, peer, listenPort, bootFile, req.ClientArch())
	}
}

func isPXEClient(req *dhcpv4.DHCPv4) bool {
	classID := req.ClassIdentifier()
	return strings.HasPrefix(classID, "PXEClient")
}

func selectBootFile(req *dhcpv4.DHCPv4, cfg *Config) string {
	// IANA DHCP option 93 architecture values commonly seen with PXE:
	// 0  = Intel x86PC BIOS
	// 6  = EFI IA32
	// 7  = EFI BC
	// 9  = EFI x86-64
	// 11 = EFI ARM64
	for _, arch := range req.ClientArch() {
		switch uint16(arch) {
		case 6, 7, 9, 11:
			return cfg.BootfileUEFI
		}
	}
	return cfg.BootfileBIOS
}

func pxeVendorOption43() []byte {
	return []byte{
		// Sub-option 6: PXE Discovery Control.
		// 8 asks the client not to do extra broadcast discovery.
		6, 1, 8,

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
