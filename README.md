# FOG ProxyDHCP

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
- PXE vendor option 43 helper to reduce firmware fallback discovery problems.
- Static Linux binary build.
- `make help` target.
- systemd unit.

## Requirements

- Linux.
- Go 1.22 or newer.
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

## Configuration

Edit the included configuration:

```bash
nano config.toml
```

Edit it:

```toml
interface = "eth0"

fog_ip = "192.168.1.50"

bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"

listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
```

### Important fields

| Field | Meaning |
|---|---|
| `interface` | Linux network interface where PXE clients are visible. Example: `eth0`, `ens18`, `enp3s0`. |
| `fog_ip` | Real FOG server IP. It can be different from the proxy host IP. |
| `bootfile_bios` | Boot file for legacy BIOS PXE clients. FOG default: `undionly.kpxe`. |
| `bootfile_uefi` | Boot file for UEFI PXE clients. FOG default: `ipxe.efi`. |
| `listen_dhcp_port` | Usually `67`. Receives PXE `DHCPDISCOVER`. |
| `listen_pxe_port` | Usually `4011`. Handles PXE follow-up requests. |
| `enable_pxe_port` | Keep enabled for best firmware compatibility. |

## Test run

```bash
sudo ./fog-proxy -config ./config.toml
```

Then boot a client with PXE enabled. You should see logs similar to:

```text
sent OFFER: mac=xx:xx:xx:xx:xx:xx peer=0.0.0.0:68 port=67 bootfile=ipxe.efi arch=[9]
sent ACK: mac=xx:xx:xx:xx:xx:xx peer=192.168.1.123:68 port=4011 bootfile=ipxe.efi arch=[9]
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

This is supported.

Example:

```text
FOG server:          192.168.1.50
ProxyDHCP machine:   192.168.1.60
DHCP/router:         192.168.1.1
```

Use this in `config.toml` on the proxy machine:

```toml
fog_ip = "192.168.1.50"
```

Do **not** set `fog_ip` to the proxy machine unless the proxy is also the FOG/TFTP server.

## Troubleshooting

### PXE client gets an IP but does not load FOG

Check that:

- The proxy logs show an OFFER or ACK.
- `fog_ip` points to the real FOG server.
- The client can reach UDP/69 on the FOG server.
- The selected bootfile exists under the FOG TFTP root.

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

```text
help
deps
tidy
fmt
check
build
clean
run
install
uninstall
install-service
uninstall-service
service-start
service-stop
service-restart
service-status
logs
ports
```

## License

MIT.
