# FOG ProxyDHCP

[English](README.md)

`fog-proxydhcp` es un pequeño servicio ProxyDHCP escrito en Go para entornos de FOG Project donde no se puede modificar el servidor DHCP existente.

No **asigna** direcciones IP. El router existente, Windows DHCP, Kea, ISC DHCP o el DHCP del campus siguen haciendo eso. Este servicio solo responde a clientes PXE con la información del servidor de arranque FOG y del archivo de arranque.

## Topología típica

```text
DHCP/router existente      192.168.1.1     asigna direcciones IP a clientes
Servidor FOG               192.168.1.50    sirve archivos TFTP/iPXE/FOG
Host FOG ProxyDHCP         192.168.1.60    ejecuta este servicio
Clientes PXE               misma VLAN/dominio de broadcast L2
```

El proxy puede ejecutarse en el propio servidor FOG o en otra máquina Linux. El requisito importante es que los clientes PXE puedan ver el proxy en el mismo dominio de broadcast, salvo que uses reglas de DHCP relay/IP helper.

## Cómo funciona

El arranque PXE empieza con DHCP. Un cliente emite una solicitud DHCP por broadcast porque necesita una dirección IP antes de poder descargar nada. En una red normal, el servidor DHCP existente responde con la dirección IP del cliente, máscara de subred, puerta de enlace, DNS e información de concesión.

FOG también necesita que el cliente PXE conozca dos valores específicos de arranque:

```text
Servidor TFTP / next-server  -> la IP del servidor FOG
Nombre del archivo de arranque -> el cargador iPXE, como undionly.kpxe o ipxe.efi
```

Un servicio ProxyDHCP proporciona solo esos valores de arranque PXE. No asigna una dirección IP y no reemplaza al servidor DHCP real. Esto permite añadir arranque PXE con FOG en redes donde el servidor DHCP lo controla un router, Windows DHCP, DHCP de campus u otro servicio que no quieres reemplazar.

El flujo de paquetes es:

```text
1. El cliente PXE envía DHCPDISCOVER por broadcast a UDP/67.
2. El DHCP real ofrece una dirección IP.
3. fog-proxydhcp anuncia que existe un servicio PXE, sin enviar aún el bootfile.
4. El cliente acepta la IP del DHCP real.
5. El firmware PXE solicita los datos de arranque al proxy por UDP/4011.
6. El proxy responde con el servidor TFTP y el cargador BIOS o UEFI.
7. El cliente descarga el cargador desde el TFTP de FOG.
8. iPXE abre boot.php por HTTP y muestra el menú FOG.
```

En la oferta inicial de UDP/67 no se envía el bootfile. Los datos reales de arranque se entregan en UDP/4011 mediante option 66, option 67 y los campos BOOTP `siaddr`, `sname` y `file`. Los clientes que ya se identifican como iPXE pueden recibir directamente la URL HTTP configurada en `ipxe_bootfile`.

La selección BIOS y UEFI es automática. El cliente envía la opción DHCP 93, también llamada Client System Architecture. Los clientes BIOS reciben `bootfile_bios`; los clientes UEFI reciben `bootfile_uefi`.

Las entradas opcionales `[[boot_rule]]` pueden sobrescribir el archivo de arranque seleccionado para prefijos de direcciones MAC. Esto es útil cuando un grupo de máquinas, una plataforma de virtualización o un fabricante de NIC necesita un binario iPXE distinto. Las reglas se comprueban de arriba abajo; gana el primer prefijo que coincida.

Conviene conocer una limitación: ProxyDHCP es un complemento, no una sobrescritura garantizada. Si el servidor DHCP principal ya envía información de arranque PXE/BOOTP, especialmente un valor `siaddr` / next-server incorrecto, algunas implementaciones PXE pueden preferir el valor del servidor DHCP principal. En ese caso, elimina los ajustes de arranque PXE del servidor DHCP principal o configúralos allí con la IP correcta del servidor FOG.

## Características

- Configuración TOML.
- Selección de archivo de arranque BIOS y UEFI mediante la opción DHCP 93.
- Comportamiento ProxyDHCP estándar:
  - UDP/67 para DHCPDISCOVER.
  - UDP/4011 para PXE DHCPREQUEST.
- Rellena tanto opciones DHCP como campos BOOTP:
  - option 66 / nombre del servidor TFTP.
  - option 67 / nombre del archivo de arranque.
  - `siaddr`.
  - `sname`.
  - `file`.
- Flujo ProxyDHCP en dos fases compatible con firmware UEFI de VirtualBox.
- Lista blanca/negra opcional por prefijo MAC mediante `[[client_rule]]`.
- Destino opcional de segunda fase iPXE mediante `ipxe_bootfile`.
- Servidor TFTP opcional solo para laboratorio, útil para probar sin FOG real.
- Compilación de binario Linux estático.
- Objetivo `make help`.
- Unidad systemd.

## Requisitos

- Linux.
- Go 1.23 o posterior.
- Privilegios root o capacidades equivalentes para enlazar UDP/67 y UDP/4011.
- Un servidor FOG funcional con archivos TFTP/iPXE disponibles.
- Clientes PXE y proxy en la misma VLAN, salvo que enrutes o retransmitas explícitamente el tráfico DHCP/PXE.

## Instalación desde código fuente

```bash
git clone https://github.com/soyunomas/fog-proxydhcp.git
cd fog-proxydhcp
make help
make build
```

La compilación produce:

```text
./fog-proxy
```

## Compilación para OpenWrt y Raspberry Pi

El `Makefile` genera binarios Linux estáticos, por lo que no necesita instalar un compilador C cruzado. Consulta todos los objetivos disponibles con:

```bash
make help
```

Para compilar todas las arquitecturas habituales de OpenWrt:

```bash
make openwrt
```

También puedes compilar solo la arquitectura de tu router:

```bash
make openwrt-amd64   # OpenWrt x86-64
make openwrt-armv7   # ARMv7
make openwrt-arm64   # ARM64 / aarch64
make openwrt-mips    # MIPS big-endian, soft-float
make openwrt-mipsel  # MIPS little-endian, soft-float
```

Para Raspberry Pi:

```bash
make raspi           # Compila las tres variantes
make raspi-armv6     # Raspberry Pi 1 y Zero con sistema de 32 bits
make raspi-armv7     # Raspberry Pi 2/3/4 con sistema de 32 bits
make raspi-arm64     # Raspberry Pi 3/4/5 con sistema de 64 bits
```

Los resultados se guardan en `dist/bin/`. Comprueba la arquitectura del dispositivo con `uname -m`: `aarch64` usa `arm64`, `armv7l` usa `armv7`, y `mips`/`mipsel` deben coincidir también en el orden de bytes. OpenWrt necesita espacio suficiente para el binario Go; estos objetivos generan ejecutables, no paquetes `.ipk`.

## Configuración

Abre `config.toml` y elige **un solo escenario**. El archivo trae el escenario probado activo y los demás completamente comentados.

Para usar otro escenario:

1. Comenta el bloque activo completo.
2. Descomenta el bloque elegido completo.
3. Cambia las direcciones IP de ejemplo.
4. No dejes dos escenarios activos a la vez porque TOML no permite claves duplicadas.

### Elección rápida

| Caso | Escenario | Valor de `fog_ip` | `enable_tftp` |
|---|---|---|---|
| Proxy instalado en el propio servidor FOG, incluido VirtualBox | 1, probado | IP del servidor FOG | `false` |
| Proxy instalado en otro ordenador Linux | 2 | IP del servidor FOG remoto | `false` |
| Solo equipos físicos, sin regla VirtualBox | 3 | IP del servidor FOG | `false` |
| Laboratorio sin FOG real | 4 | IP del ordenador proxy/laboratorio | `true` |

> **Regla importante:** `interface` siempre pertenece al ordenador que ejecuta `fog-proxydhcp`. `fog_ip` siempre apunta al servidor que ofrece TFTP y FOG. Si proxy y FOG están separados, son dos ordenadores y normalmente dos IP distintas.

Los ejemplos usan siempre `eno1`. Comprueba el nombre real con `ip -brief address` y cámbialo si tu sistema usa otro.

### Escenario 1: probado, proxy en el servidor FOG y VirtualBox

Esta es la configuración principal incluida. Fue probada con VirtualBox en BIOS y UEFI. La regla `08:00:27` selecciona `ipxe.kpxe` para BIOS y `snponly.efi` para UEFI.

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

FOG debe servir `/tftpboot`. El TFTP auxiliar del proxy permanece desactivado.

### Escenario 2: proxy en otro ordenador distinto de FOG

Sí, funciona. El ordenador proxy no necesita tener FOG instalado. Debe cumplir estas condiciones:

- Proxy, clientes PXE y servidor FOG deben estar en la misma VLAN o dominio de broadcast, salvo que exista DHCP relay/IP helper.
- `eno1` es la interfaz del ordenador proxy conectada a los clientes.
- `fog_ip` es la IP del servidor FOG remoto, no la IP del proxy.
- Los clientes deben alcanzar FOG por UDP/69 y TCP/80.
- El proxy debe poder escuchar UDP/67 y UDP/4011.

Ejemplo: proxy `192.168.1.60`, FOG `192.168.1.50`:

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

Observa que la IP `192.168.1.60` del proxy no aparece en `fog_ip`: el programa obtiene automáticamente la IP del proxy desde `eno1`.

### Escenario 3: FOG real y equipos físicos

Es igual al escenario 1 o 2, pero sin `[[boot_rule]]`. Usa los cargadores globales de FOG:

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

### Escenario 4: laboratorio sin FOG

Este modo prueba ProxyDHCP y el TFTP auxiliar. No lo uses junto al TFTP de un FOG real.

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

### Significado de los campos

| Campo | Significado |
|---|---|
| `interface` | Interfaz del ordenador que ejecuta el proxy. Los ejemplos usan `eno1`. |
| `fog_ip` | Servidor que ofrece TFTP/FOG. Puede estar en otro ordenador. |
| `bootfile_bios` | Cargador BIOS general, normalmente `undionly.kpxe`. |
| `bootfile_uefi` | Cargador UEFI general, normalmente `ipxe.efi`. |
| `ipxe_bootfile` | Menú HTTP de FOG para la segunda fase iPXE. |
| `listen_dhcp_port` | Puerto de anuncio ProxyDHCP inicial, normalmente UDP/67. |
| `listen_pxe_port` | Puerto de selección PXE posterior, normalmente UDP/4011. |
| `enable_pxe_port` | Debe permanecer en `true` para máxima compatibilidad. |
| `allow_unmatched_clients` | `true` atiende a todos; `false` solo atiende coincidencias permitidas en `[[client_rule]]`. |
| `[[client_rule]]` | Autoriza o deniega PXE por prefijo MAC. La primera coincidencia gana. |
| `enable_tftp` | `false` con FOG real; `true` solo en laboratorio sin TFTP externo. |
| `[[boot_rule]]` | Solo cambia el cargador para un prefijo MAC; no autoriza ni bloquea clientes. |

### Controlar qué equipos reciben PXE

Por defecto se atiende a todos:

```toml
allow_unmatched_clients = true
```

Para crear una lista blanca, cambia el valor a `false` y añade uno o varios `[[client_rule]]`:

```toml
allow_unmatched_clients = false

# Permite todo un lote por prefijo MAC.
[[client_rule]]
name = "aula-1"
mac_prefix = "00:11:22"
allow = true

# Permite un equipo exacto usando la MAC completa.
[[client_rule]]
name = "equipo-profesor"
mac_prefix = "AA:BB:CC:DD:EE:FF"
allow = true
```

Las reglas se evalúan de arriba abajo y gana la primera coincidencia. Esto permite excepciones:

```toml
allow_unmatched_clients = false

# Esta regla más específica debe ir primero.
[[client_rule]]
name = "virtualbox-bloqueada"
mac_prefix = "08:00:27:AA:BB:CC"
allow = false

[[client_rule]]
name = "resto-virtualbox"
mac_prefix = "08:00:27"
allow = true
```

Un cliente no autorizado sigue obteniendo su dirección IP del DHCP principal, pero `fog-proxydhcp` no le responde ni en UDP/67 ni en UDP/4011. Por tanto, no recibe el arranque FOG desde este proxy.

> El filtrado por MAC es control operativo, no seguridad fuerte: una dirección MAC puede falsificarse.

`[[client_rule]]` controla **quién puede arrancar por este proxy**. `[[boot_rule]]` controla **qué cargador recibe un cliente ya autorizado**. Son funciones distintas.

### Flujo probado con VirtualBox UEFI

1. UDP/67 anuncia que existe ProxyDHCP, sin enviar todavía el cargador.
2. El DHCP principal asigna la IP al cliente.
3. VirtualBox consulta UDP/4011.
4. El proxy responde con `snponly.efi` por la regla `08:00:27`.
5. El cliente descarga `snponly.efi` por TFTP desde FOG.
6. iPXE abre `boot.php` por HTTP.

## Ejecución de prueba

```bash
sudo ./fog-proxy -config ./config.toml
```

Después arranca un cliente con PXE activado. Deberías ver logs similares a:

```text
sent OFFER: mac=xx:xx:xx:xx:xx:xx peer=0.0.0.0:68 port=67 bootfile=ipxe.efi arch=[9]
sent ACK: mac=xx:xx:xx:xx:xx:xx peer=192.168.1.123:4011 port=4011 bootfile=ipxe.efi arch=[9]
```

## Instalación en el sistema

```bash
sudo make install
```

Esto instala:

```text
/usr/local/bin/fog-proxy
/etc/fog-proxydhcp/config.toml
```

Edita la configuración instalada:

```bash
sudo nano /etc/fog-proxydhcp/config.toml
```

## Construir un paquete Debian

```bash
make deb
```

El paquete se escribe en:

```text
dist/fog-proxydhcp_<version>_amd64.deb
```

Instálalo con:

```bash
sudo apt install ./dist/fog-proxydhcp_<version>_amd64.deb
```

Instala:

```text
/usr/local/bin/fog-proxy
/etc/fog-proxydhcp/config.toml
/etc/systemd/system/fog-proxy.service
```

El archivo de configuración se instala como conffile de Debian, por lo que las ediciones locales bajo `/etc/fog-proxydhcp/config.toml` se conservan durante actualizaciones del paquete.

El servicio **no** se habilita ni se inicia automáticamente. Es intencionado: la interfaz y la IP del servidor FOG deben revisarse antes de que un servicio escuche en UDP/67 en una red en producción.

Tras instalar el paquete, edita la configuración:

```bash
sudo nano /etc/fog-proxydhcp/config.toml
```

Después habilita e inicia el servicio:

```bash
sudo systemctl enable --now fog-proxy.service
```

Para iniciarlo sin habilitarlo en el arranque:

```bash
sudo systemctl start fog-proxy.service
```

## Instalar como servicio systemd

```bash
sudo make install-service
sudo systemctl start fog-proxy.service
sudo systemctl status fog-proxy.service --no-pager
```

Seguir logs:

```bash
make logs
```

O directamente:

```bash
journalctl -u fog-proxy.service -f
```

## Firewall

Permite UDP/67 y UDP/4011 en el host proxy:

```bash
sudo ufw allow 67/udp
sudo ufw allow 4011/udp
```

Para firewalld:

```bash
sudo firewall-cmd --add-port=67/udp --permanent
sudo firewall-cmd --add-port=4011/udp --permanent
sudo firewall-cmd --reload
```

Los clientes también deben poder alcanzar los servicios del servidor FOG, especialmente TFTP:

```text
UDP/69  TFTP
TCP/80  scripts HTTP/iPXE y recursos web de FOG, según la instalación
```

## Comprobar conflictos de puertos

Antes de iniciar el servicio:

```bash
make ports
```

O manualmente:

```bash
sudo ss -ulpn | grep -E ':(67|4011)\b'
```

Si otro servicio DHCP ya ocupa UDP/67 en el mismo host/interfaz, este proxy no podrá enlazar ese puerto.

## Ejecutar el proxy en una máquina distinta a FOG

Está soportado y no requiere instalar FOG en la máquina proxy. Usa el **escenario 2** de la sección [Configuración](#configuración). Recuerda:

- `interface = "eno1"` pertenece a la máquina proxy.
- `fog_ip` e `ipxe_bootfile` apuntan al servidor FOG remoto.
- Los clientes deben llegar al FOG remoto por UDP/69 y TCP/80.
- Proxy, clientes y FOG deben compartir VLAN/dominio de broadcast o usar DHCP relay.

## Solución de problemas

### El cliente PXE obtiene IP pero no carga FOG

Comprueba que:

- Los logs del proxy muestran un OFFER o ACK.
- `fog_ip` apunta al servidor FOG real.
- El cliente puede alcanzar UDP/69 en el servidor FOG.
- El bootfile seleccionado existe bajo la raíz TFTP de FOG.
- Si el cliente ya es iPXE e intenta descargar desde la IP del router, define `ipxe_bootfile = "http://IP_FOG/fog/service/ipxe/boot.php"`.

### El cliente funciona en BIOS pero no en UEFI

Comprueba:

```toml
bootfile_uefi = "ipxe.efi"
```

Algunos entornos usan archivos UEFI distintos, como:

```text
snponly.efi
ipxe.efi
```

Confirma que el archivo existe en el servidor FOG.

### El cliente espera ProxyDHCP o muestra PXE-E55

Mantén:

```toml
enable_pxe_port = true
listen_pxe_port = 4011
```

Y verifica que UDP/4011 no esté bloqueado.

### El servicio no arranca

Ejecuta:

```bash
sudo systemctl status fog-proxy.service --no-pager
sudo journalctl -u fog-proxy.service -n 100 --no-pager
```

Causas comunes:

- Nombre de interfaz incorrecto.
- Falta `/etc/fog-proxydhcp/config.toml`.
- Otro proceso ya ocupa UDP/67.
- El servicio no se está ejecutando como root.

## Objetivos Make

Ejecuta:

```bash
make help
```

Los objetivos actuales incluyen:

| Objetivo | Descripción |
|---|---|
| `help` | Muestra los objetivos Make disponibles. |
| `deps` | Ejecuta `go mod tidy` para sincronizar dependencias del módulo Go. |
| `tidy` | Alias de `deps`. |
| `fmt` | Formatea los archivos fuente Go. |
| `check` | Formatea el código y ejecuta las comprobaciones del paquete Go. |
| `build` | Construye el binario Linux amd64 estático `fog-proxy`. |
| `deb` | Construye un paquete Debian bajo `dist/`. |
| `clean` | Elimina el binario generado y los directorios de salida de empaquetado. |
| `run` | Construye y ejecuta `fog-proxy` localmente con `sudo` y `config.toml`. |
| `install` | Instala el binario y la configuración bajo `/usr/local/bin` y `/etc/fog-proxydhcp`. |
| `uninstall` | Elimina el binario instalado, el directorio de configuración y la unidad de servicio. |
| `install-service` | Instala el binario/configuración y habilita el servicio systemd. |
| `uninstall-service` | Deshabilita, detiene y elimina la unidad systemd. |
| `service-start` | Inicia `fog-proxy.service`. |
| `service-stop` | Detiene `fog-proxy.service`. |
| `service-restart` | Reinicia `fog-proxy.service`. |
| `service-status` | Muestra el estado de `fog-proxy.service`. |
| `logs` | Sigue los logs del journal de `fog-proxy.service`. |
| `ports` | Muestra procesos escuchando en UDP/67 o UDP/4011. |

## Licencia

MIT.
