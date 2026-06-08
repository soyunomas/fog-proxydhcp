# FOG ProxyDHCP

[Español](README-ES.md)

`fog-proxydhcp` is a small Go ProxyDHCP service for FOG Project environments where the existing DHCP server cannot be modified.

It does **not** assign IP addresses. Your existing router, Windows DHCP, Kea, ISC DHCP or campus DHCP keeps doing that. This service only answers PXE clients with the FOG boot server and boot file information.

## Typical topology

```text
Existing DHCP/router       192.168.1.1     gives client IP addresses
FOG server                 192.168.1.50    serves TFTP/iPXE/FOG files
FOG ProxyDHCP host         192.168.1.60    runs this service
PXE clients                same VLAN/L2 broadcast domain
```

The proxy can run on the FOG server itself or on another Linux machine. The important requirement is that PXE clients can see the proxy on the same broadcast domain, unless you use DHCP relay/IP helper rules.

## How it works

PXE boot starts with DHCP. A client broadcasts a DHCP request because it needs
an IP address before it can download anything. In a normal network, the existing
DHCP server answers with the client IP address, subnet mask, gateway, DNS, and
lease information.

FOG also needs the PXE client to learn two boot-specific values:

```text
TFTP server / next-server  -> the FOG server IP
Boot file name             -> the iPXE loader, such as undionly.kpxe or ipxe.efi
```

A ProxyDHCP service provides only those PXE boot values. It does not assign an
IP address and does not replace the real DHCP server. This lets you add FOG PXE
booting on networks where the DHCP server is controlled by a router, Windows
DHCP, campus DHCP, or another service you do not want to replace.

The packet flow is:

```text
1. The PXE client broadcasts DHCPDISCOVER to UDP/67.
2. The real DHCP server offers an IP address.
3. fog-proxydhcp advertises the PXE service without sending the bootfile yet.
4. The client accepts its address from the real DHCP server.
5. PXE firmware requests boot details from the proxy on UDP/4011.
6. The proxy returns the TFTP server and the BIOS or UEFI loader.
7. The client downloads the loader from FOG TFTP.
8. iPXE opens boot.php over HTTP and displays the FOG menu.
```

The initial UDP/67 offer does not include a bootfile. Actual boot data is returned on UDP/4011 through option 66, option 67, and the BOOTP fields `siaddr`, `sname`, and `file`. Clients already identifying as iPXE can receive the HTTP URL configured in `ipxe_bootfile` directly.

BIOS and UEFI selection is automatic. The client sends DHCP option 93, also
called Client System Architecture. BIOS clients get `bootfile_bios`; UEFI
clients get `bootfile_uefi`.

Optional `[[boot_rule]]` entries can override the selected boot file for MAC
address prefixes. This is useful when a group of machines, a VM platform, or a
NIC vendor needs a different iPXE binary. Rules are checked from top to bottom;
the first matching prefix wins.

One limitation is worth knowing: ProxyDHCP is a complement, not a guaranteed
override. If the main DHCP server already sends PXE/BOOTP boot information,
especially a wrong `siaddr` / next-server value, some PXE implementations may
prefer the main DHCP server's value. In that case, remove PXE boot settings from
the main DHCP server or configure them there with the correct FOG server IP.

## Features

- TOML configuration.
- BIOS and UEFI boot file selection using DHCP option 93.
- Standard ProxyDHCP behaviour:
  - UDP/67 for DHCPDISCOVER.
  - UDP/4011 for PXE DHCPREQUEST.
- Fills both DHCP options and BOOTP fields:
  - option 66 / TFTP server name.
  - option 67 / boot file name.
  - `siaddr`.
  - `sname`.
  - `file`.
- Two-stage ProxyDHCP flow compatible with VirtualBox UEFI firmware.
- Optional MAC-prefix allow/deny rules through `[[client_rule]]`.
- Optional iPXE second-stage boot target using `ipxe_bootfile`.
- Optional lab-only TFTP server for testing without a real FOG server.
- Static Linux binary build.
- `make help` target.
- systemd unit.

## Requirements

- Linux.
- Go 1.23 or newer.
- Root privileges or equivalent capabilities to bind UDP/67 and UDP/4011.
- A working FOG server with TFTP/iPXE files available.
- PXE clients and the proxy in the same VLAN, unless you explicitly route/relay DHCP/PXE traffic.

## Installation from source

```bash
git clone https://github.com/soyunomas/fog-proxydhcp.git
cd fog-proxydhcp
make help
make build
```

The build produces:

```text
./fog-proxy
```

## Building for OpenWrt and Raspberry Pi

The `Makefile` produces static Linux binaries, so a C cross-compiler is not required. List every available target with:

```bash
make help
```

Build all common OpenWrt architectures:

```bash
make openwrt
```

Or build only the architecture used by the router:

```bash
make openwrt-amd64   # OpenWrt x86-64
make openwrt-armv7   # ARMv7
make openwrt-arm64   # ARM64 / aarch64
make openwrt-mips    # MIPS big-endian, soft-float
make openwrt-mipsel  # MIPS little-endian, soft-float
```

For Raspberry Pi:

```bash
make raspi           # Build all three variants
make raspi-armv6     # Raspberry Pi 1 and Zero with a 32-bit OS
make raspi-armv7     # Raspberry Pi 2/3/4 with a 32-bit OS
make raspi-arm64     # Raspberry Pi 3/4/5 with a 64-bit OS
```

Build results are written to `dist/bin/`. Check the device architecture with `uname -m`: `aarch64` uses `arm64`, `armv7l` uses `armv7`, and `mips`/`mipsel` must also match byte order. OpenWrt needs enough storage for the Go binary; these targets produce executables, not `.ipk` packages.

## Configuration

Open `config.toml` and choose **one scenario only**. The tested scenario is active; every alternative is fully commented.

To use another scenario:

1. Comment the complete active block.
2. Uncomment the complete scenario you want.
3. Replace the example IP addresses.
4. Never leave two scenarios active because TOML does not allow duplicate keys.

### Quick choice

| Case | Scenario | `fog_ip` value | `enable_tftp` |
|---|---|---|---|
| Proxy installed on the FOG server, including VirtualBox | 1, tested | FOG server IP | `false` |
| Proxy installed on another Linux computer | 2 | Remote FOG server IP | `false` |
| Physical clients only, without the VirtualBox rule | 3 | FOG server IP | `false` |
| Lab without a real FOG server | 4 | Proxy/lab computer IP | `true` |

> **Important rule:** `interface` always belongs to the computer running `fog-proxydhcp`. `fog_ip` always points to the server providing TFTP and FOG. When proxy and FOG are separate, they are two computers and normally have different IP addresses.

Every example uses `eno1`. Check the actual name with `ip -brief address` and change it if required.

### Scenario 1: tested, proxy on the FOG server and VirtualBox

This is the active configuration shipped in `config.toml`. It was tested with VirtualBox BIOS and UEFI. The `08:00:27` rule selects `ipxe.kpxe` for BIOS and `snponly.efi` for UEFI.

```toml
interface = "eno1"
fog_ip = "192.168.1.50"
bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"
ipxe_bootfile = "http://192.168.1.50/fog/service/ipxe/boot.php"
listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
allow_unmatched_clients = true
enable_tftp = false
tftp_root = "lab/tftproot"
listen_tftp_port = 69

[[boot_rule]]
name = "virtualbox-tested"
mac_prefix = "08:00:27"
bootfile_bios = "ipxe.kpxe"
bootfile_uefi = "snponly.efi"
```

FOG must serve `/tftpboot`. Keep the proxy TFTP helper disabled.

### Scenario 2: proxy on a different computer from FOG

Yes, this works. The proxy computer does not need FOG installed. Requirements:

- Proxy, PXE clients, and FOG must share the same VLAN/broadcast domain unless a DHCP relay/IP helper is configured.
- `eno1` is the proxy computer interface facing the clients.
- `fog_ip` is the remote FOG server IP, never the proxy IP.
- Clients must reach FOG on UDP/69 and TCP/80.
- The proxy must listen on UDP/67 and UDP/4011.

Example: proxy `192.168.1.60`, FOG `192.168.1.50`:

```toml
interface = "eno1"
fog_ip = "192.168.1.50"
bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"
ipxe_bootfile = "http://192.168.1.50/fog/service/ipxe/boot.php"
listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
allow_unmatched_clients = true
enable_tftp = false
tftp_root = "lab/tftproot"
listen_tftp_port = 69

[[boot_rule]]
name = "virtualbox-tested"
mac_prefix = "08:00:27"
bootfile_bios = "ipxe.kpxe"
bootfile_uefi = "snponly.efi"
```

Notice that proxy IP `192.168.1.60` is not assigned to `fog_ip`: the program discovers the proxy IP automatically from `eno1`.

### Scenario 3: real FOG and physical clients

This is scenario 1 or 2 without `[[boot_rule]]`:

```toml
interface = "eno1"
fog_ip = "192.168.1.50"
bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"
ipxe_bootfile = "http://192.168.1.50/fog/service/ipxe/boot.php"
listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
allow_unmatched_clients = true
enable_tftp = false
tftp_root = "lab/tftproot"
listen_tftp_port = 69
```

### Scenario 4: lab without FOG

This mode tests ProxyDHCP and the built-in TFTP helper. Do not enable it alongside a real FOG TFTP server.

```toml
interface = "eno1"
fog_ip = "192.168.1.60"
bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"
ipxe_bootfile = "fog-local.ipxe"
listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
allow_unmatched_clients = true
enable_tftp = true
tftp_root = "lab/tftproot"
listen_tftp_port = 69
```

### Field reference

| Field | Meaning |
|---|---|
| `interface` | Interface on the computer running the proxy. Examples use `eno1`. |
| `fog_ip` | Server providing TFTP/FOG. It may be a different computer. |
| `bootfile_bios` | Global BIOS loader, normally `undionly.kpxe`. |
| `bootfile_uefi` | Global UEFI loader, normally `ipxe.efi`. |
| `ipxe_bootfile` | FOG HTTP menu used by the iPXE second stage. |
| `listen_dhcp_port` | Initial ProxyDHCP advertisement port, normally UDP/67. |
| `listen_pxe_port` | Follow-up PXE selection port, normally UDP/4011. |
| `enable_pxe_port` | Keep `true` for best firmware compatibility. |
| `allow_unmatched_clients` | `true` serves everyone; `false` only serves allowed `[[client_rule]]` matches. |
| `[[client_rule]]` | Allows or denies PXE by MAC prefix. First match wins. |
| `enable_tftp` | `false` with real FOG; `true` only in a lab without external TFTP. |
| `[[boot_rule]]` | Only changes the loader for a MAC prefix; it does not allow or block clients. |

### Control which computers receive PXE

Every client is served by default:

```toml
allow_unmatched_clients = true
```

To create an allowlist, change it to `false` and add one or more `[[client_rule]]` entries:

```toml
allow_unmatched_clients = false

# Allow a group by MAC prefix.
[[client_rule]]
name = "classroom-1"
mac_prefix = "00:11:22"
allow = true

# Allow one exact computer with its complete MAC address.
[[client_rule]]
name = "teacher-computer"
mac_prefix = "AA:BB:CC:DD:EE:FF"
allow = true
```

Rules are evaluated from top to bottom and the first match wins. This supports exceptions:

```toml
allow_unmatched_clients = false

# Put the more specific rule first.
[[client_rule]]
name = "blocked-virtualbox"
mac_prefix = "08:00:27:AA:BB:CC"
allow = false

[[client_rule]]
name = "other-virtualbox"
mac_prefix = "08:00:27"
allow = true
```

An unauthorized client still receives an address from the main DHCP server, but `fog-proxydhcp` does not answer it on UDP/67 or UDP/4011. It therefore receives no FOG boot from this proxy.

> MAC filtering is operational control, not strong security: MAC addresses can be spoofed.

`[[client_rule]]` controls **who may boot through this proxy**. `[[boot_rule]]` controls **which loader an already authorized client receives**. They are separate features.

### Tested VirtualBox UEFI flow

1. UDP/67 advertises ProxyDHCP without sending the loader yet.
2. The main DHCP server assigns the client address.
3. VirtualBox requests boot details on UDP/4011.
4. The proxy returns `snponly.efi` through the `08:00:27` rule.
5. The client downloads `snponly.efi` from FOG over TFTP.
6. iPXE opens `boot.php` over HTTP.

## Test run

```bash
sudo ./fog-proxy -config ./config.toml
```

Then boot a client with PXE enabled. You should see logs similar to:

```text
sent OFFER: mac=xx:xx:xx:xx:xx:xx peer=0.0.0.0:68 port=67 bootfile=ipxe.efi arch=[9]
sent ACK: mac=xx:xx:xx:xx:xx:xx peer=192.168.1.123:4011 port=4011 bootfile=ipxe.efi arch=[9]
```

## Install system-wide

```bash
sudo make install
```

This installs:

```text
/usr/local/bin/fog-proxy
/etc/fog-proxydhcp/config.toml
```

Edit the installed config:

```bash
sudo nano /etc/fog-proxydhcp/config.toml
```

## Build a Debian package

```bash
make deb
```

The package is written to:

```text
dist/fog-proxydhcp_<version>_amd64.deb
```

Install it with:

```bash
sudo apt install ./dist/fog-proxydhcp_<version>_amd64.deb
```

It installs:

```text
/usr/local/bin/fog-proxy
/etc/fog-proxydhcp/config.toml
/etc/systemd/system/fog-proxy.service
```

The configuration file is installed as a Debian conffile, so local edits
under `/etc/fog-proxydhcp/config.toml` are preserved by package upgrades.

The service is **not** enabled or started automatically. This is intentional:
the interface and FOG server IP should be reviewed before a service listens on
UDP/67 in a live network.

After installing the package, edit the configuration:

```bash
sudo nano /etc/fog-proxydhcp/config.toml
```

Then enable and start the service:

```bash
sudo systemctl enable --now fog-proxy.service
```

To start it without enabling it at boot:

```bash
sudo systemctl start fog-proxy.service
```

## Install as a systemd service

```bash
sudo make install-service
sudo systemctl start fog-proxy.service
sudo systemctl status fog-proxy.service --no-pager
```

Follow logs:

```bash
make logs
```

Or directly:

```bash
journalctl -u fog-proxy.service -f
```

## Firewall

Allow UDP/67 and UDP/4011 on the proxy host:

```bash
sudo ufw allow 67/udp
sudo ufw allow 4011/udp
```

For firewalld:

```bash
sudo firewall-cmd --add-port=67/udp --permanent
sudo firewall-cmd --add-port=4011/udp --permanent
sudo firewall-cmd --reload
```

The clients must also reach the FOG server services, especially TFTP:

```text
UDP/69  TFTP
TCP/80  HTTP/iPXE scripts and FOG web assets, depending on setup
```

## Check port conflicts

Before starting the service:

```bash
make ports
```

Or manually:

```bash
sudo ss -ulpn | grep -E ':(67|4011)\b'
```

If another DHCP service already owns UDP/67 on the same host/interface, this proxy cannot bind that port.

## Running the proxy on a different machine than FOG

This is supported and FOG does not need to be installed on the proxy computer. Use **scenario 2** in [Configuration](#configuration). Remember:

- `interface = "eno1"` belongs to the proxy computer.
- `fog_ip` and `ipxe_bootfile` point to the remote FOG server.
- Clients must reach remote FOG on UDP/69 and TCP/80.
- Proxy, clients, and FOG must share a VLAN/broadcast domain or use DHCP relay.

## Troubleshooting

### PXE client gets an IP but does not load FOG

Check that:

- The proxy logs show an OFFER or ACK.
- `fog_ip` points to the real FOG server.
- The client can reach UDP/69 on the FOG server.
- The selected bootfile exists under the FOG TFTP root.
- If the client is already iPXE and tries to download from the router IP, set `ipxe_bootfile = "http://FOG_IP/fog/service/ipxe/boot.php"`.

### Client works in BIOS but not UEFI

Check:

```toml
bootfile_uefi = "ipxe.efi"
```

Some environments use different UEFI files, such as:

```text
snponly.efi
ipxe.efi
```

Confirm the file exists on the FOG server.

### Client waits for ProxyDHCP or shows PXE-E55

Keep:

```toml
enable_pxe_port = true
listen_pxe_port = 4011
```

And verify UDP/4011 is not blocked.

### Service fails to start

Run:

```bash
sudo systemctl status fog-proxy.service --no-pager
sudo journalctl -u fog-proxy.service -n 100 --no-pager
```

Common causes:

- Wrong interface name.
- Missing `/etc/fog-proxydhcp/config.toml`.
- Another process already owns UDP/67.
- Service not running as root.

## Make targets

Run:

```bash
make help
```

Current targets include:

| Target | Description |
|---|---|
| `help` | Show the available Make targets. |
| `deps` | Run `go mod tidy` to sync Go module dependencies. |
| `tidy` | Alias for `deps`. |
| `fmt` | Format Go source files. |
| `check` | Format the code and run the Go package checks. |
| `build` | Build the static Linux amd64 `fog-proxy` binary. |
| `deb` | Build a Debian package under `dist/`. |
| `clean` | Remove the generated binary and packaging output directories. |
| `run` | Build and run `fog-proxy` locally with `sudo` and `config.toml`. |
| `install` | Install the binary and config under `/usr/local/bin` and `/etc/fog-proxydhcp`. |
| `uninstall` | Remove the installed binary, config directory, and service unit. |
| `install-service` | Install the binary/config and enable the systemd service. |
| `uninstall-service` | Disable, stop, and remove the systemd service unit. |
| `service-start` | Start `fog-proxy.service`. |
| `service-stop` | Stop `fog-proxy.service`. |
| `service-restart` | Restart `fog-proxy.service`. |
| `service-status` | Show `fog-proxy.service` status. |
| `logs` | Follow `fog-proxy.service` journal logs. |
| `ports` | Show processes listening on UDP/67 or UDP/4011. |

## License

MIT.
