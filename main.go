package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/server4"
	"github.com/pelletier/go-toml/v2"
)

const (
	defaultConfigFile = "config.toml"

	portDHCP = 67
	portPXE  = 4011
	portTFTP = 69

	tftpBlockSize        = 512
	tftpMaxBlockSize     = 1468
	tftpTransferAttempts = 5
	tftpTransferTimeout  = 2 * time.Second
)

type Config struct {
	Interface             string       `toml:"interface"`
	FogIP                 string       `toml:"fog_ip"`
	BootfileBIOS          string       `toml:"bootfile_bios"`
	BootfileUEFI          string       `toml:"bootfile_uefi"`
	ListenDHCPPort        int          `toml:"listen_dhcp_port"`
	ListenPXEPort         int          `toml:"listen_pxe_port"`
	EnablePXEPort         bool         `toml:"enable_pxe_port"`
	EnableTFTP            bool         `toml:"enable_tftp"`
	ListenTFTPPort        int          `toml:"listen_tftp_port"`
	TFTPRoot              string       `toml:"tftp_root"`
	IPXEBootfile          string       `toml:"ipxe_bootfile"`
	AllowUnmatchedClients bool         `toml:"allow_unmatched_clients"`
	ClientRules           []ClientRule `toml:"client_rule"`
	BootRules             []BootRule   `toml:"boot_rule"`
	ProxyIP               net.IP       `toml:"-"`
}

type ClientRule struct {
	Name                string `toml:"name"`
	MACPrefix           string `toml:"mac_prefix"`
	Allow               bool   `toml:"allow"`
	normalizedMACPrefix string
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
		ListenDHCPPort:        portDHCP,
		ListenPXEPort:         portPXE,
		ListenTFTPPort:        portTFTP,
		EnablePXEPort:         true,
		AllowUnmatchedClients: true,
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
	if cfg.ListenTFTPPort < 1 || cfg.ListenTFTPPort > 65535 {
		return fmt.Errorf("listen_tftp_port out of range: %d", cfg.ListenTFTPPort)
	}
	cfg.TFTPRoot = strings.TrimSpace(cfg.TFTPRoot)
	cfg.IPXEBootfile = strings.TrimSpace(cfg.IPXEBootfile)
	if cfg.EnableTFTP {
		if cfg.TFTPRoot == "" {
			return errors.New("tftp_root cannot be empty when enable_tftp is true")
		}
		info, err := os.Stat(cfg.TFTPRoot)
		if err != nil {
			return fmt.Errorf("cannot read tftp_root %q: %w", cfg.TFTPRoot, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("tftp_root must be a directory: %q", cfg.TFTPRoot)
		}
	}

	for i := range cfg.ClientRules {
		rule := &cfg.ClientRules[i]
		rule.Name = strings.TrimSpace(rule.Name)
		rule.MACPrefix = strings.TrimSpace(rule.MACPrefix)

		if rule.MACPrefix == "" {
			return fmt.Errorf("client_rule[%d] mac_prefix cannot be empty", i)
		}

		normalized, err := normalizeMACPrefix(rule.MACPrefix)
		if err != nil {
			return fmt.Errorf("client_rule[%d] invalid mac_prefix %q: %w", i, rule.MACPrefix, err)
		}
		rule.normalizedMACPrefix = normalized
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
			// UDP/67 only advertises ProxyDHCP. A DHCPREQUEST selects the real
			// DHCP server; a zero-address ACK here can race the real lease ACK.
			if msgType != dhcpv4.MessageTypeDiscover {
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

		allowed, source := clientAllowed(req.ClientHWAddr, cfg)
		if !allowed {
			log.Printf("ignored unauthorized PXE client: mac=%s peer=%s port=%d source=%s", req.ClientHWAddr, peer, listenPort, source)
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

		reply.UpdateOption(dhcpv4.OptMessageType(replyType))
		reply.UpdateOption(dhcpv4.OptServerIdentifier(cfg.ProxyIP))
		reply.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient"))

		if listenPort == cfg.ListenDHCPPort && !isIPXEClient(req) {
			// The initial proxy offer only announces the PXE service. Firmware gets
			// its lease from the real DHCP server, then requests boot details on 4011.
			reply.ServerIPAddr = cfg.ProxyIP
			reply.ServerHostName = ""
			reply.BootFileName = ""
			copyPXEClientIdentifier(reply, req)
		} else {
			// UDP/4011 and already-running iPXE clients receive the actual target.
			reply.ServerIPAddr = fogIP
			reply.ServerHostName = cfg.FogIP
			reply.BootFileName = bootFile
			reply.UpdateOption(dhcpv4.OptTFTPServerName(cfg.FogIP))
			reply.UpdateOption(dhcpv4.OptBootFileName(bootFile))
			copyPXEClientOptions(reply, req)
		}

		if _, err := conn.WriteTo(reply.ToBytes(), peer); err != nil {
			log.Printf("send failed: mac=%s peer=%s port=%d err=%v", req.ClientHWAddr, peer, listenPort, err)
			return
		}

		log.Printf("sent %s: mac=%s peer=%s port=%d bootfile=%s source=%s arch=%v",
			replyType, req.ClientHWAddr, peer, listenPort, bootFile, selection.Source, req.ClientArch())
	}
}

func copyPXEClientIdentifier(reply, req *dhcpv4.DHCPv4) {
	if value := req.Options.Get(dhcpv4.OptionClientMachineIdentifier); len(value) > 0 {
		reply.UpdateOption(dhcpv4.OptGeneric(dhcpv4.OptionClientMachineIdentifier, append([]byte(nil), value...)))
	}
}

func copyPXEClientOptions(reply, req *dhcpv4.DHCPv4) {
	// RFC 4578 requires these options in PXE client and server packets.
	for _, code := range []dhcpv4.OptionCode{
		dhcpv4.OptionClientSystemArchitectureType,
		dhcpv4.OptionClientNetworkInterfaceIdentifier,
		dhcpv4.OptionClientMachineIdentifier,
	} {
		if value := req.Options.Get(code); len(value) > 0 {
			reply.UpdateOption(dhcpv4.OptGeneric(code, append([]byte(nil), value...)))
		}
	}
}

func isPXEClient(req *dhcpv4.DHCPv4) bool {
	classID := req.ClassIdentifier()
	return strings.HasPrefix(classID, "PXEClient") || isIPXEClient(req)
}

func clientAllowed(mac net.HardwareAddr, cfg *Config) (bool, string) {
	normalizedMAC, err := normalizeMACPrefix(mac.String())
	if err == nil {
		for i := range cfg.ClientRules {
			rule := &cfg.ClientRules[i]
			if strings.HasPrefix(normalizedMAC, rule.normalizedMACPrefix) {
				name := rule.Name
				if name == "" {
					name = rule.MACPrefix
				}
				return rule.Allow, "client_rule:" + name
			}
		}
	}

	return cfg.AllowUnmatchedClients, "allow_unmatched_clients"
}

func selectBootFile(req *dhcpv4.DHCPv4, cfg *Config) bootSelection {
	if cfg.IPXEBootfile != "" && isIPXEClient(req) {
		return bootSelection{File: ipxeBootFile(cfg), Source: "ipxe"}
	}

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

func ipxeBootFile(cfg *Config) string {
	if strings.Contains(cfg.IPXEBootfile, "://") {
		return cfg.IPXEBootfile
	}
	return "tftp://" + cfg.FogIP + "/" + strings.TrimLeft(cfg.IPXEBootfile, "/")
}

func isIPXEClient(req *dhcpv4.DHCPv4) bool {
	for _, userClass := range req.UserClass() {
		if strings.EqualFold(strings.TrimSpace(userClass), "iPXE") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(req.ClassIdentifier()), "ipxe")
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
		// Type 0, layer 2, description length 3, label "FOG".
		9, 6, 0, 0, 3, 'F', 'O', 'G',

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

type tftpServer struct {
	conn *net.UDPConn
	root string
}

type tftpRequest struct {
	filename string
	mode     string
	options  map[string]string
}

func startTFTPServer(root string, port int) (*tftpServer, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		return nil, err
	}
	return &tftpServer{conn: conn, root: root}, nil
}

func (s *tftpServer) Serve() error {
	buf := make([]byte, 2048)
	for {
		n, peer, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		packet := append([]byte(nil), buf[:n]...)
		go s.handleRequest(peer, packet)
	}
}

func (s *tftpServer) handleRequest(peer *net.UDPAddr, packet []byte) {
	req, err := parseTFTPRequest(packet)
	if err != nil {
		sendTFTPError(s.conn, peer, 4, err.Error())
		return
	}
	if req.mode != "octet" && req.mode != "netascii" {
		sendTFTPError(s.conn, peer, 4, "unsupported transfer mode")
		return
	}

	fullPath, err := safeTFTPPath(s.root, req.filename)
	if err != nil {
		sendTFTPError(s.conn, peer, 2, err.Error())
		return
	}

	file, err := os.Open(fullPath)
	if err != nil {
		sendTFTPError(s.conn, peer, 1, "file not found")
		log.Printf("tftp not found: peer=%s file=%q", peer, req.filename)
		return
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil || stat.IsDir() {
		sendTFTPError(s.conn, peer, 1, "file not found")
		return
	}

	blockSize, acceptedOptions := negotiateTFTPOptions(req.options, stat.Size())
	if err := sendTFTPFile(peer, req.filename, file, blockSize, acceptedOptions); err != nil {
		log.Printf("tftp transfer failed: peer=%s file=%q err=%v", peer, req.filename, err)
		return
	}
	log.Printf("tftp sent: peer=%s file=%q size=%d blksize=%d", peer, req.filename, stat.Size(), blockSize)
}

func parseTFTPRequest(packet []byte) (*tftpRequest, error) {
	if len(packet) < 4 {
		return nil, errors.New("short tftp packet")
	}
	if binary.BigEndian.Uint16(packet[:2]) != 1 {
		return nil, errors.New("only RRQ is supported")
	}

	parts := bytes.Split(packet[2:], []byte{0})
	if len(parts) < 3 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return nil, errors.New("malformed RRQ")
	}

	req := &tftpRequest{
		filename: string(parts[0]),
		mode:     strings.ToLower(string(parts[1])),
		options:  map[string]string{},
	}

	for i := 2; i+1 < len(parts); i += 2 {
		if len(parts[i]) == 0 {
			break
		}
		key := strings.ToLower(string(parts[i]))
		req.options[key] = string(parts[i+1])
	}

	return req, nil
}

func safeTFTPPath(root, filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("empty filename")
	}

	cleanName := path.Clean("/" + strings.ReplaceAll(filename, "\\", "/"))
	if cleanName == "/" {
		return "", errors.New("empty filename")
	}
	relativeName := strings.TrimPrefix(cleanName, "/")

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	fullPath, err := filepath.Abs(filepath.Join(rootAbs, filepath.FromSlash(relativeName)))
	if err != nil {
		return "", err
	}

	if fullPath != rootAbs && !strings.HasPrefix(fullPath, rootAbs+string(os.PathSeparator)) {
		return "", errors.New("filename escapes tftp_root")
	}
	return fullPath, nil
}

func negotiateTFTPOptions(options map[string]string, fileSize int64) (int, map[string]string) {
	blockSize := tftpBlockSize
	accepted := map[string]string{}

	if requested, ok := options["blksize"]; ok {
		if value, err := strconv.Atoi(requested); err == nil {
			if value < 8 {
				value = 8
			}
			if value > tftpMaxBlockSize {
				value = tftpMaxBlockSize
			}
			blockSize = value
			accepted["blksize"] = strconv.Itoa(blockSize)
		}
	}

	if _, ok := options["tsize"]; ok {
		accepted["tsize"] = strconv.FormatInt(fileSize, 10)
	}

	return blockSize, accepted
}

func sendTFTPFile(peer *net.UDPAddr, filename string, file *os.File, blockSize int, options map[string]string) error {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return err
	}
	defer conn.Close()

	if len(options) > 0 {
		oack := makeTFTPOACK(options)
		if err := sendAndWaitForAck(conn, peer, oack, 0); err != nil {
			return fmt.Errorf("option ack failed: %w", err)
		}
	}

	buf := make([]byte, blockSize)
	block := uint16(1)
	for {
		n, readErr := file.Read(buf)
		if readErr != nil && readErr != io.EOF {
			return readErr
		}

		data := makeTFTPData(block, buf[:n])
		if err := sendAndWaitForAck(conn, peer, data, block); err != nil {
			return fmt.Errorf("block %d failed: %w", block, err)
		}

		if n < blockSize {
			return nil
		}
		block++
	}
}

func sendAndWaitForAck(conn *net.UDPConn, peer *net.UDPAddr, packet []byte, block uint16) error {
	var lastErr error
	for attempt := 0; attempt < tftpTransferAttempts; attempt++ {
		if _, err := conn.WriteToUDP(packet, peer); err != nil {
			return err
		}
		if err := waitForTFTPAck(conn, peer, block); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

func waitForTFTPAck(conn *net.UDPConn, peer *net.UDPAddr, block uint16) error {
	if err := conn.SetReadDeadline(time.Now().Add(tftpTransferTimeout)); err != nil {
		return err
	}

	buf := make([]byte, 2048)
	for {
		n, addr, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if !addr.IP.Equal(peer.IP) || addr.Port != peer.Port {
			continue
		}
		if n < 4 {
			continue
		}

		opcode := binary.BigEndian.Uint16(buf[:2])
		switch opcode {
		case 4:
			if binary.BigEndian.Uint16(buf[2:4]) == block {
				return nil
			}
		case 5:
			return errors.New("client sent tftp error")
		}
	}
}

func makeTFTPData(block uint16, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(packet[0:2], 3)
	binary.BigEndian.PutUint16(packet[2:4], block)
	copy(packet[4:], payload)
	return packet
}

func makeTFTPOACK(options map[string]string) []byte {
	packet := []byte{0, 6}
	for _, key := range []string{"blksize", "tsize"} {
		value, ok := options[key]
		if !ok {
			continue
		}
		packet = append(packet, []byte(key)...)
		packet = append(packet, 0)
		packet = append(packet, []byte(value)...)
		packet = append(packet, 0)
	}
	return packet
}

func sendTFTPError(conn *net.UDPConn, peer *net.UDPAddr, code uint16, message string) {
	packet := make([]byte, 4, 4+len(message)+1)
	binary.BigEndian.PutUint16(packet[0:2], 5)
	binary.BigEndian.PutUint16(packet[2:4], code)
	packet = append(packet, []byte(message)...)
	packet = append(packet, 0)
	_, _ = conn.WriteToUDP(packet, peer)
}

func main() {
	configFile := flag.String("config", defaultConfigFile, "Path to TOML configuration file")
	flag.Parse()

	cfg, err := loadConfig(*configFile)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	log.Printf("starting FOG ProxyDHCP: interface=%s proxy_ip=%s fog_ip=%s dhcp_port=%d pxe_port=%d tftp=%t allow_unmatched=%t client_rules=%d",
		cfg.Interface, cfg.ProxyIP, cfg.FogIP, cfg.ListenDHCPPort, cfg.ListenPXEPort, cfg.EnableTFTP, cfg.AllowUnmatchedClients, len(cfg.ClientRules))

	dhcpServer, err := startServer(cfg, cfg.ListenDHCPPort)
	if err != nil {
		log.Fatalf("cannot listen on UDP/%d interface=%s: %v", cfg.ListenDHCPPort, cfg.Interface, err)
	}

	errCh := make(chan error, 3)

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

	if cfg.EnableTFTP {
		tftpServer, err := startTFTPServer(cfg.TFTPRoot, cfg.ListenTFTPPort)
		if err != nil {
			log.Fatalf("cannot listen on UDP/%d for TFTP: %v", cfg.ListenTFTPPort, err)
		}

		go func() {
			log.Printf("serving TFTP on UDP/%d root=%s", cfg.ListenTFTPPort, cfg.TFTPRoot)
			errCh <- tftpServer.Serve()
		}()
	}

	if err := <-errCh; err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}
