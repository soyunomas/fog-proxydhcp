package main

import (
	"net"
	"testing"

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

func TestPXEVendorOption43IncludesBootServerList(t *testing.T) {
	got := pxeVendorOption43(net.IPv4(192, 168, 24, 2))
	want := []byte{
		6, 1, 7,
		8, 7, 0, 0, 1, 192, 168, 24, 2,
		9, 6, 0, 0, 2, 'F', 'O', 'G',
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
