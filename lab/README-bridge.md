# Laboratorio PXE en LAN puente (router como DHCP)

Este modo reproduce tu escenario:

```text
Router de casa        -> DHCP real, reparte la IP de la VM
Host Linux            -> fog-proxydhcp + TFTP emulado
VirtualBox en puente  -> cliente PXE
```

No necesitas FOG. Si la VM arranca con una ROM PXE clasica, el host sirve
`undionly.kpxe` por TFTP y despues entrega `fog-local.ipxe` a iPXE. Si
VirtualBox arranca ya con iPXE incorporado, puede recibir una URL TFTP absoluta
al script directamente. Ese script se queda en la shell de iPXE, para que
VirtualBox no termine cayendo al mensaje fatal de BIOS sin disco.

## 1. Preparar la config

Copia la plantilla y rellena la interfaz y la IP real del host en tu LAN:

```bash
cp lab/config.bridge.example.toml lab/config.bridge.toml
nano lab/config.bridge.toml
```

Valores que debes cambiar:

```toml
interface = "eno1"        # ejemplo; usa tu interfaz LAN real
fog_ip = "192.168.1.50"   # IP LAN del host que ejecuta fog-proxydhcp
```

El DHCP del router debe seguir activo. No pongas la VM en NAT: usa adaptador
puente y la misma red donde esta el host.

## 2. Compilar y arrancar

```bash
make build
sudo ./fog-proxy --config lab/config.bridge.toml
```

Deberias ver algo parecido a:

```text
starting FOG ProxyDHCP: interface=eno1 proxy_ip=192.168.1.50 fog_ip=192.168.1.50 ...
listening on eno1 UDP/67
listening on eno1 UDP/4011
serving TFTP on UDP/69 root=lab/tftproot
```

## 3. Arrancar la VM

En VirtualBox:

- Adaptador 1: `Adaptador puente`.
- Tipo de NIC: Intel PRO/1000 MT Desktop suele funcionar bien en BIOS.
- Firmware BIOS para usar `undionly.kpxe`.
- Orden de arranque: red primero.

Resultado esperado:

1. La VM obtiene IP del router.
2. `fog-proxydhcp` registra un `sent OFFER`.
3. Si VirtualBox ya usa iPXE, el OFFER puede ser `bootfile=tftp://IP_DEL_HOST/fog-local.ipxe source=ipxe`.
4. Si la ROM es PXE clasica, primero veras `bootfile=undionly.kpxe` y luego `bootfile=tftp://IP_DEL_HOST/fog-local.ipxe source=ipxe`.
5. El TFTP embebido registra `tftp sent`.
6. La VM muestra el texto de exito y queda en `iPXE>`, sin error fatal.

## Diagnostico rapido

Si no aparece nada en logs, revisa que la VM este realmente en puente y en la
misma LAN. Si llega el OFFER pero no hay TFTP, verifica firewall local para
UDP/69. Para observar el trafico:

```bash
sudo tcpdump -ni INTERFAZ -e -vvv -s0 '(udp port 67 or 68 or 69 or 4011)'
```

Sustituye `INTERFAZ` por el valor de `interface`.

## UEFI

La plantilla anuncia `ipxe.efi` a clientes UEFI, pero este repo no lo incluye.
Para probar UEFI, copia un `ipxe.efi` valido dentro de `lab/tftproot` o cambia
la VM a BIOS.
