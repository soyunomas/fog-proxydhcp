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
1. El cliente PXE envía un DHCPDISCOVER por broadcast en UDP/67.
2. El servidor DHCP real responde con la configuración IP del cliente.
3. fog-proxydhcp también responde con información de arranque PXE.
4. Algunos firmwares PXE envían una solicitud PXE DHCPREQUEST posterior a UDP/4011.
5. El cliente descarga el archivo de arranque seleccionado desde el servidor TFTP de FOG.
```

Este servicio rellena tanto opciones DHCP comunes como campos BOOTP:

```text
option 66  nombre del servidor TFTP
option 67  nombre del archivo de arranque
option 43  datos de proveedor PXE: control de descubrimiento, lista de servidores de arranque, menú, prompt
siaddr     dirección next-server
sname      nombre del servidor
file       nombre del archivo de arranque
```

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
- Ayudante para la opción PXE vendor 43, para reducir problemas de descubrimiento alternativo en firmwares:
  - subopción 6 / control de descubrimiento PXE.
  - subopción 8 / lista de servidores de arranque PXE.
  - subopción 9 / menú de arranque PXE.
  - subopción 10 / prompt del menú PXE.
- Compilación de binario Linux estático.
- Objetivo `make help`.
- Unidad systemd.

## Requisitos

- Linux.
- Go 1.22 o posterior.
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

## Configuración

Edita la configuración incluida:

```bash
nano config.toml
```

Modifícala:

```toml
interface = "eth0"

fog_ip = "192.168.1.50"

bootfile_bios = "undionly.kpxe"
bootfile_uefi = "ipxe.efi"

# Sobrescrituras opcionales por prefijo MAC.
#
# [[boot_rule]]
# name = "virtualbox"
# mac_prefix = "08:00:27"
# bootfile_bios = "ipxe.kpxe"
# bootfile_uefi = "snponly.efi"

listen_dhcp_port = 67
listen_pxe_port = 4011
enable_pxe_port = true
```

### Campos importantes

| Campo | Significado |
|---|---|
| `interface` | Interfaz de red Linux donde los clientes PXE son visibles. Ejemplos: `eth0`, `ens18`, `enp3s0`. |
| `fog_ip` | IP real del servidor FOG. Puede ser distinta de la IP del host proxy. |
| `bootfile_bios` | Archivo de arranque para clientes PXE BIOS legacy. Valor predeterminado de FOG: `undionly.kpxe`. |
| `bootfile_uefi` | Archivo de arranque para clientes PXE UEFI. Valor predeterminado de FOG: `ipxe.efi`. |
| `[[boot_rule]]` | Reglas opcionales de sobrescritura de bootfile por prefijo MAC. |
| `boot_rule.name` | Etiqueta opcional usada en logs cuando la regla coincide. |
| `boot_rule.mac_prefix` | Prefijo MAC a comparar, como `08:00:27`, `08-00-27` o `080027`. |
| `boot_rule.bootfile_bios` | Archivo de arranque BIOS para clientes coincidentes. Omítelo para usar el archivo BIOS global. |
| `boot_rule.bootfile_uefi` | Archivo de arranque UEFI para clientes coincidentes. Omítelo para usar el archivo UEFI global. |
| `listen_dhcp_port` | Normalmente `67`. Recibe `DHCPDISCOVER` PXE. |
| `listen_pxe_port` | Normalmente `4011`. Gestiona solicitudes PXE posteriores. |
| `enable_pxe_port` | Déjalo activado para la mejor compatibilidad con firmwares. |

### Reglas de arranque

Las reglas de arranque permiten servir un binario iPXE distinto a un subconjunto de máquinas.

```toml
[[boot_rule]]
name = "virtualbox"
mac_prefix = "08:00:27"
bootfile_bios = "ipxe.kpxe"
bootfile_uefi = "snponly.efi"

[[boot_rule]]
name = "uefi-snp-clients"
mac_prefix = "52:54:00"
bootfile_uefi = "snponly.efi"
```

Las reglas se evalúan en el orden del archivo. Usa primero los prefijos más específicos. Una regla puede definir solo `bootfile_bios` o solo `bootfile_uefi`; el valor ausente cae al `bootfile_bios` o `bootfile_uefi` global.

Cuando una regla coincide, el log del servicio incluye el origen:

```text
sent OFFER: mac=08:00:27:aa:bb:cc peer=255.255.255.255:68 port=67 bootfile=ipxe.kpxe source=boot_rule:virtualbox arch=[Intel x86PC]
```

## Ejecución de prueba

```bash
sudo ./fog-proxy -config ./config.toml
```

Después arranca un cliente con PXE activado. Deberías ver logs similares a:

```text
sent OFFER: mac=xx:xx:xx:xx:xx:xx peer=0.0.0.0:68 port=67 bootfile=ipxe.efi arch=[9]
sent ACK: mac=xx:xx:xx:xx:xx:xx peer=192.168.1.123:68 port=4011 bootfile=ipxe.efi arch=[9]
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

Está soportado.

Ejemplo:

```text
Servidor FOG:          192.168.1.50
Máquina ProxyDHCP:     192.168.1.60
DHCP/router:           192.168.1.1
```

Usa esto en `config.toml` en la máquina proxy:

```toml
fog_ip = "192.168.1.50"
```

No pongas `fog_ip` con la IP de la máquina proxy salvo que el proxy sea también el servidor FOG/TFTP.

## Solución de problemas

### El cliente PXE obtiene IP pero no carga FOG

Comprueba que:

- Los logs del proxy muestran un OFFER o ACK.
- `fog_ip` apunta al servidor FOG real.
- El cliente puede alcanzar UDP/69 en el servidor FOG.
- El bootfile seleccionado existe bajo la raíz TFTP de FOG.

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
