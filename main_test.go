package main

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/iana"
)

func TestNormalizeMACPrefix(t *testing.T) {
	tests := map[string]string{
		"08:00:27":          "080027",
		"08-00-27":          "080027",
		"080027":            "080027",
		"52:54:00:aa:bb:cc": "525400aabbcc",
	}

	for input, want := range tests {
		got, err := normalizeMACPrefix(input)
		if err != nil {
			t.Fatalf("normalizeMACPrefix(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeMACPrefix(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeMACPrefixRejectsInvalidInput(t *testing.T) {
	tests := []string{"", "08:00:2", "08:00:zz", "08:00:27:aa:bb:cc:dd"}

	for _, input := range tests {
		if _, err := normalizeMACPrefix(input); err == nil {
			t.Fatalf("normalizeMACPrefix(%q) returned nil error", input)
		}
	}
}

func TestSelectBootFileUsesFirstMatchingMACPrefixRule(t *testing.T) {
	cfg := Config{
		BootfileBIOS: "undionly.kpxe",
		BootfileUEFI: "ipxe.efi",
		BootRules: []BootRule{
			{
				Name:                "virtualbox",
				MACPrefix:           "08:00:27",
				BootfileBIOS:        "ipxe.kpxe",
				BootfileUEFI:        "snponly.efi",
				normalizedMACPrefix: "080027",
			},
			{
				Name:                "less-specific",
				MACPrefix:           "08",
				BootfileBIOS:        "undionly.kkpxe",
				normalizedMACPrefix: "08",
			},
		},
	}

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x08, 0x00, 0x27, 0xaa, 0xbb, 0xcc},
	}

	selection := selectBootFile(req, &cfg)
	if selection.File != "ipxe.kpxe" {
		t.Fatalf("selection.File = %q, want %q", selection.File, "ipxe.kpxe")
	}
	if selection.Source != "boot_rule:virtualbox" {
		t.Fatalf("selection.Source = %q, want %q", selection.Source, "boot_rule:virtualbox")
	}
}

func TestSelectBootFileFallsBackForMissingRuleArchitecture(t *testing.T) {
	cfg := Config{
		BootfileBIOS: "undionly.kpxe",
		BootfileUEFI: "ipxe.efi",
		BootRules: []BootRule{
			{
				Name:                "uefi-only",
				MACPrefix:           "52:54:00",
				BootfileUEFI:        "snponly.efi",
				normalizedMACPrefix: "525400",
			},
		},
	}

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x52, 0x54, 0x00, 0xaa, 0xbb, 0xcc},
	}

	selection := selectBootFile(req, &cfg)
	if selection.File != "undionly.kpxe" {
		t.Fatalf("selection.File = %q, want default BIOS bootfile", selection.File)
	}
	if selection.Source != "default" {
		t.Fatalf("selection.Source = %q, want default", selection.Source)
	}
}

func TestSelectBootFileUsesRuleUEFIOverride(t *testing.T) {
	cfg := Config{
		BootfileBIOS: "undionly.kpxe",
		BootfileUEFI: "ipxe.efi",
		BootRules: []BootRule{
			{
				Name:                "uefi-only",
				MACPrefix:           "52:54:00",
				BootfileUEFI:        "snponly.efi",
				normalizedMACPrefix: "525400",
			},
		},
	}

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x52, 0x54, 0x00, 0xaa, 0xbb, 0xcc},
	}
	req.UpdateOption(dhcpv4.OptClientArch(iana.EFI_X86_64))

	selection := selectBootFile(req, &cfg)
	if selection.File != "snponly.efi" {
		t.Fatalf("selection.File = %q, want UEFI rule bootfile", selection.File)
	}
	if selection.Source != "boot_rule:uefi-only" {
		t.Fatalf("selection.Source = %q, want rule source", selection.Source)
	}
}

func TestSelectBootFileUsesIPXEBootfileForIPXEClient(t *testing.T) {
	cfg := Config{
		BootfileBIOS: "undionly.kpxe",
		BootfileUEFI: "ipxe.efi",
		FogIP:        "192.168.56.10",
		IPXEBootfile: "fog-local.ipxe",
	}

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x08, 0x00, 0x27, 0xaa, 0xbb, 0xcc},
	}
	req.UpdateOption(dhcpv4.OptUserClass("iPXE"))

	selection := selectBootFile(req, &cfg)
	if selection.File != "tftp://192.168.56.10/fog-local.ipxe" {
		t.Fatalf("selection.File = %q, want absolute iPXE TFTP URI", selection.File)
	}
	if selection.Source != "ipxe" {
		t.Fatalf("selection.Source = %q, want ipxe", selection.Source)
	}
}

func TestSelectBootFilePreservesAbsoluteIPXEURI(t *testing.T) {
	cfg := Config{
		BootfileBIOS: "undionly.kpxe",
		BootfileUEFI: "ipxe.efi",
		FogIP:        "192.168.56.10",
		IPXEBootfile: "http://192.168.56.10/fog/service/ipxe/boot.php",
	}

	req := &dhcpv4.DHCPv4{
		ClientHWAddr: net.HardwareAddr{0x08, 0x00, 0x27, 0xaa, 0xbb, 0xcc},
	}
	req.UpdateOption(dhcpv4.OptUserClass("iPXE"))

	selection := selectBootFile(req, &cfg)
	if selection.File != "http://192.168.56.10/fog/service/ipxe/boot.php" {
		t.Fatalf("selection.File = %q, want unchanged absolute URI", selection.File)
	}
	if selection.Source != "ipxe" {
		t.Fatalf("selection.Source = %q, want ipxe", selection.Source)
	}
}

func TestIPXEBootFileBuildsTFTPURIForRelativeLabScript(t *testing.T) {
	cfg := Config{
		FogIP:        "192.168.56.10",
		IPXEBootfile: "fog-local.ipxe",
	}

	got := ipxeBootFile(&cfg)
	want := "tftp://192.168.56.10/fog-local.ipxe"
	if got != want {
		t.Fatalf("ipxeBootFile = %q, want %q", got, want)
	}
}

func TestPXEVendorOption43IncludesBootServerList(t *testing.T) {
	got := pxeVendorOption43(net.IPv4(192, 168, 24, 2))
	want := []byte{
		6, 1, 7,
		8, 7, 0, 0, 1, 192, 168, 24, 2,
		9, 6, 0, 0, 3, 'F', 'O', 'G',
		10, 4, 0, 'F', 'O', 'G',
		255,
	}

	if len(got) != len(want) {
		t.Fatalf("len(pxeVendorOption43) = %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pxeVendorOption43 byte %d = %d, want %d: %v", i, got[i], want[i], got)
		}
	}
}

// capturingConn is a minimal net.PacketConn that records what the handler sends.
type capturingConn struct {
	lastPayload []byte
	lastPeer    net.Addr
}

func (c *capturingConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.lastPayload = append([]byte(nil), p...)
	c.lastPeer = addr
	return len(p), nil
}
func (c *capturingConn) ReadFrom(p []byte) (int, net.Addr, error) { return 0, nil, nil }
func (c *capturingConn) Close() error                             { return nil }
func (c *capturingConn) LocalAddr() net.Addr                      { return nil }
func (c *capturingConn) SetDeadline(time.Time) error              { return nil }
func (c *capturingConn) SetReadDeadline(time.Time) error          { return nil }
func (c *capturingConn) SetWriteDeadline(time.Time) error         { return nil }

// TestHandlerProducesValidProxyOffer drives makeProxyHandler with a synthetic
// PXE DHCPDISCOVER and asserts the OFFER is a well-formed ProxyDHCP reply:
// no IP allocation, FOG IP in BOOTP/option 66, the selected bootfile, and the
// PXE boot-server (option 43) pointing at the proxy IP.
func TestHandlerProducesValidProxyOffer(t *testing.T) {
	cfg := &Config{
		Interface:      "lo",
		FogIP:          "192.168.56.10",
		BootfileBIOS:   "undionly.kpxe",
		BootfileUEFI:   "ipxe.efi",
		ListenDHCPPort: portDHCP,
		ListenPXEPort:  portPXE,
		ProxyIP:        net.IPv4(192, 168, 56, 1).To4(),
	}

	discover, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New: %v", err)
	}
	discover.OpCode = dhcpv4.OpcodeBootRequest
	discover.ClientHWAddr = net.HardwareAddr{0x08, 0x00, 0x27, 0x11, 0x22, 0x33}
	discover.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover))
	discover.UpdateOption(dhcpv4.OptClassIdentifier("PXEClient:Arch:00000:UNDI:002001"))

	conn := &capturingConn{}
	handler := makeProxyHandler(cfg, cfg.ListenDHCPPort)
	peer := &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	handler(conn, peer, discover)

	if conn.lastPayload == nil {
		t.Fatal("handler did not send any reply to a PXE discover")
	}

	reply, err := dhcpv4.FromBytes(conn.lastPayload)
	if err != nil {
		t.Fatalf("reply does not parse as DHCPv4: %v", err)
	}

	if reply.MessageType() != dhcpv4.MessageTypeOffer {
		t.Fatalf("reply message type = %v, want OFFER", reply.MessageType())
	}
	if !reply.YourIPAddr.Equal(net.IPv4zero) {
		t.Fatalf("ProxyDHCP must not allocate an address, got YourIPAddr=%v", reply.YourIPAddr)
	}
	if !reply.ServerIPAddr.Equal(net.IPv4(192, 168, 56, 10)) {
		t.Fatalf("siaddr (ServerIPAddr) = %v, want FOG IP 192.168.56.10", reply.ServerIPAddr)
	}
	if reply.BootFileName != "undionly.kpxe" {
		t.Fatalf("BootFileName = %q, want undionly.kpxe", reply.BootFileName)
	}
	if got := reply.ClassIdentifier(); got != "PXEClient" {
		t.Fatalf("option 60 = %q, want PXEClient", got)
	}
	if got := reply.TFTPServerName(); got != "192.168.56.10" {
		t.Fatalf("option 66 (TFTP server name) = %q, want FOG IP", got)
	}

	// Option 43 sub-option 8 must advertise the proxy IP (the host answering 4011).
	vendor := reply.Options.Get(dhcpv4.OptionVendorSpecificInformation)
	wantVendor := []byte{
		6, 1, 7,
		8, 7, 0, 0, 1, 192, 168, 56, 1,
		9, 6, 0, 0, 3, 'F', 'O', 'G',
		10, 4, 0, 'F', 'O', 'G',
		255,
	}
	if !bytes.Equal(vendor, wantVendor) {
		t.Fatalf("option 43 = %v, want %v", vendor, wantVendor)
	}
}

func TestHandlerRepliesToIPXEUserClassWithScript(t *testing.T) {
	cfg := &Config{
		Interface:      "lo",
		FogIP:          "192.168.56.10",
		BootfileBIOS:   "undionly.kpxe",
		BootfileUEFI:   "ipxe.efi",
		IPXEBootfile:   "fog-local.ipxe",
		ListenDHCPPort: portDHCP,
		ListenPXEPort:  portPXE,
		ProxyIP:        net.IPv4(192, 168, 56, 1).To4(),
	}

	discover, err := dhcpv4.New()
	if err != nil {
		t.Fatalf("dhcpv4.New: %v", err)
	}
	discover.OpCode = dhcpv4.OpcodeBootRequest
	discover.ClientHWAddr = net.HardwareAddr{0x08, 0x00, 0x27, 0x11, 0x22, 0x33}
	discover.UpdateOption(dhcpv4.OptMessageType(dhcpv4.MessageTypeDiscover))
	discover.UpdateOption(dhcpv4.OptUserClass("iPXE"))

	conn := &capturingConn{}
	handler := makeProxyHandler(cfg, cfg.ListenDHCPPort)
	handler(conn, &net.UDPAddr{IP: net.IPv4bcast, Port: 68}, discover)

	if conn.lastPayload == nil {
		t.Fatal("handler did not reply to an iPXE user-class request")
	}

	reply, err := dhcpv4.FromBytes(conn.lastPayload)
	if err != nil {
		t.Fatalf("reply does not parse as DHCPv4: %v", err)
	}
	if reply.BootFileName != "tftp://192.168.56.10/fog-local.ipxe" {
		t.Fatalf("BootFileName = %q, want absolute iPXE TFTP URI", reply.BootFileName)
	}
	if got := reply.BootFileNameOption(); got != "tftp://192.168.56.10/fog-local.ipxe" {
		t.Fatalf("option 67 = %q, want absolute iPXE TFTP URI", got)
	}
	if vendor := reply.Options.Get(dhcpv4.OptionVendorSpecificInformation); vendor != nil {
		t.Fatalf("iPXE clients should use option 67 directly, got option 43 = %v", vendor)
	}
}

func TestTFTPServerServesFileWithOptions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(root+"/fog-local.ipxe", []byte("abc"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	server, err := startTFTPServer(root, 0)
	if err != nil {
		t.Fatalf("startTFTPServer: %v", err)
	}
	defer server.conn.Close()
	go func() {
		_ = server.Serve()
	}()

	client, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP client: %v", err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}

	serverPort := server.conn.LocalAddr().(*net.UDPAddr).Port
	serverAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: serverPort}
	rrq := makeTFTPRRQ("fog-local.ipxe", "octet", map[string]string{
		"blksize": "16",
		"tsize":   "0",
	})
	if _, err := client.WriteToUDP(rrq, serverAddr); err != nil {
		t.Fatalf("send RRQ: %v", err)
	}

	buf := make([]byte, 2048)
	n, transferAddr, err := client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read OACK: %v", err)
	}
	wantOACK := []byte{
		0, 6,
		'b', 'l', 'k', 's', 'i', 'z', 'e', 0, '1', '6', 0,
		't', 's', 'i', 'z', 'e', 0, '3', 0,
	}
	if !bytes.Equal(buf[:n], wantOACK) {
		t.Fatalf("OACK = %v, want %v", buf[:n], wantOACK)
	}

	if _, err := client.WriteToUDP(makeTFTPACK(0), transferAddr); err != nil {
		t.Fatalf("send OACK ACK: %v", err)
	}
	n, _, err = client.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("read DATA: %v", err)
	}
	if got := binary.BigEndian.Uint16(buf[:2]); got != 3 {
		t.Fatalf("DATA opcode = %d, want 3", got)
	}
	if got := binary.BigEndian.Uint16(buf[2:4]); got != 1 {
		t.Fatalf("DATA block = %d, want 1", got)
	}
	if !bytes.Equal(buf[4:n], []byte("abc")) {
		t.Fatalf("DATA payload = %q, want abc", buf[4:n])
	}
	if _, err := client.WriteToUDP(makeTFTPACK(1), transferAddr); err != nil {
		t.Fatalf("send DATA ACK: %v", err)
	}
}

func makeTFTPRRQ(filename, mode string, options map[string]string) []byte {
	packet := []byte{0, 1}
	packet = append(packet, []byte(filename)...)
	packet = append(packet, 0)
	packet = append(packet, []byte(mode)...)
	packet = append(packet, 0)
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

func makeTFTPACK(block uint16) []byte {
	packet := make([]byte, 4)
	binary.BigEndian.PutUint16(packet[0:2], 4)
	binary.BigEndian.PutUint16(packet[2:4], block)
	return packet
}
