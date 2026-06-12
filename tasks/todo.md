# TODO: Lista blanca de MACs por fichero e inline

## Objetivo
Permitir dar servicio solo a un conjunto de MACs concretas mediante:
- `allowed_macs` (lista inline en config.toml)
- `allowed_macs_file` (fichero externo, una MAC por línea, "#" comentarios)

## Semántica de `clientAllowed` (orden)
1. `[[client_rule]]` por prefijo (primera coincidencia gana) — comportamiento actual intacto.
2. `allowed_macs` + `allowed_macs_file` (MAC exacta, 12 hex) -> ALLOW.
3. `allow_unmatched_clients` (default).

## Decisiones
- Match por MAC exacta vía `map[string]bool` (normalizada a 12 hex).
- Ruta del fichero relativa al directorio del config.toml.
- Formato fichero: 1 MAC/línea, líneas vacías y `#` ignoradas.
- MAC inválida (inline o fichero) -> aborta arranque con contexto (nº línea para fichero).
- Se carga al arrancar (sin recarga en caliente).

## Tareas
- [x] Leer código actual y confirmar diseño.
- [x] main.go: campos `AllowedMACs`, `AllowedMACsFile` en Config; mapa interno `allowedMACs`.
- [x] main.go: cargar/normalizar inline + fichero en loadConfig; validar.
- [x] main.go: nueva rama en clientAllowed (source "allowed_macs" / "allowed_macs_file").
- [x] main.go: log de arranque con allowed_macs=N.
- [x] config.toml: documentar las dos claves nuevas.
- [x] allowed-macs.txt.example: fichero de ejemplo.
- [x] main_test.go: tests (parseo fichero, inline, exacta allow/deny, precedencia, inválida).
- [x] README.md / README-ES.md: documentar.
- [x] go test -race ./... + go vet.

## Resultados
- Implementado `allowed_macs` (inline) y `allowed_macs_file` (fichero externo, ruta
  relativa al config.toml). MAC exacta, normalizada a 12 hex, lookup O(1).
- Orden: client_rule (prefijo) > allowed_macs/allowed_macs_file (exacta) > allow_unmatched_clients.
- inline tiene prioridad de "source" sobre fichero si la MAC está en ambos.
- Validación en arranque: MAC inválida aborta (fichero indica nº de línea); fichero ausente aborta.
- gofmt limpio, go vet sin warnings, go test -race -count=1 todo PASS.

# TODO: flag --debug con opciones DHCP decodificadas

## Objetivo
Añadir un modo verboso que, por cada paquete relevante, muestre las opciones
DHCP/PXE decodificadas para diagnosticar arranques PXE en campo. Sin migrar a
slog (eso es otra mejora aparte); solo activar/desactivar detalle con un flag.

## Alcance (qué SÍ y qué NO)
- SÍ: flag --debug (bool), traza rx/decisión/tx por paquete cuando está activo.
- SÍ: usar dhcpv4.Summary() para el volcado completo + extracto de opciones clave.
- NO: migración a log/slog, JSON logs, PCAP, métricas (son ítems separados).
- NO: cambiar la lógica del flujo ProxyDHCP; solo observabilidad.

## Decisiones de diseño
- Flag: `--debug` en main(); también respetar env FOG_PROXY_DEBUG=1 (opcional).
- Propagación: pasar `debug bool` a makeProxyHandler (firma) y al tftpServer.
  Evitar estado global mutable (AGENTS.md #42); inyectar por parámetro.
- Helper local mínimo: `logDebug(format, args...)` guardado por el bool, o
  simplemente `if debug { log.Printf(...) }`. Sin dependencias nuevas.
- Nivel de detalle por paquete (solo si debug):
  1. RX: "debug rx: port=%d peer=%s\n%s" con req.Summary().
  2. Extracto legible de opciones clave (además del Summary, para grep rápido):
     - msgtype (53), client MAC (chaddr)
     - vendor class id (60)  -> req.ClassIdentifier()
     - user class (77)       -> req.UserClass()  (detecta iPXE)
     - client arch (93)      -> req.ClientArch()
     - param request list (55)
     - client machine id/UUID (97) -> OptionClientMachineIdentifier
  3. DECISIÓN: isPXEClient, isIPXEClient, clientAllowed -> allowed+source,
     selectBootFile -> file+source, rama elegida (servicio-only vs target).
  4. TX: "debug tx: replytype=%s port=%s\n%s" con reply.Summary() (incluye
     opción 43 que construimos, siaddr, sname, file).
- Las líneas "ignored ..." actuales se mantienen; en debug añadir el porqué ya
  está hecho, pero se puede volcar también el Summary del paquete ignorado.
- Coste cero cuando debug=false (Summary() solo se llama dentro del if).

## Cambios por fichero
- main.go:
  - main(): `debug := flag.Bool("debug", false, "...")`; pasarlo a startServer
    -> makeProxyHandler y a startTFTPServer/Serve.
  - startServer(cfg, port) -> startServer(cfg, port, debug) (y makeProxyHandler).
  - makeProxyHandler: añadir trazas rx/decisión/tx guardadas por debug.
  - (TFTP) handleRequest: en debug, loguear RRQ filename/mode/options y blksize.
  - Log de arranque: añadir `debug=%t`.
- README.md / README-ES.md: documentar el flag en la tabla y un ejemplo de uso
  (`sudo ./fog-proxy --debug`), avisando de que es verboso.
- Makefile: target opcional `run-debug` (sudo ./$(APP) -config $(CONFIG) --debug).
- config.toml: NO añadir clave; es un flag de invocación, no de config.

## Tests (AGENTS.md #66, #71)
- TestDebugLogsDecodedOptions: capturar salida con log.SetOutput(&buf), construir
  un DISCOVER PXE (con opciones 60/93/77/55), invocar el handler con debug=true,
  y assert de que el buffer contiene "debug rx", "debug tx" y, p.ej., la arch.
- TestNoDebugStaysQuiet: mismo handler con debug=false -> el buffer NO contiene
  "debug rx"/"debug tx" (solo el log "sent ...").
- Restaurar log.SetOutput(os.Stderr) con defer.

## Verificación
- gofmt -l, go vet ./..., go build ./..., go test -race -count=1 ./...
- Prueba manual: `sudo ./fog-proxy --debug` y confirmar volcado de un cliente real.

## Riesgos / notas
- Summary() es multilínea y muy verboso: dejar claro en README que --debug es
  para diagnóstico puntual, no para dejarlo fijo en producción (ruido en journald).
- No loguear secretos (no aplica aquí; sin tokens). AGENTS.md #76.

## Estado de implementación
- [x] Confirmar que el flag no existe en el código, binarios ni historial actual.
- [x] Implementar `--debug` e inyectarlo en DHCP y TFTP.
- [x] Añadir pruebas de logs activos e inactivos.
- [x] Añadir `make run-debug`.
- [x] Documentar ejecución manual y activación bajo systemd en ambos README.
- [x] Ejecutar `gofmt`, `go test -race`, `go vet`, build y comprobación del diff.

## Resultados
- `--debug` aparece en `fog-proxy -help`.
- Añadidas trazas RX/opciones/decisión/TX para DHCP/PXE y RRQ para TFTP.
- `make help` muestra `run-debug`.
- `go test -race ./... -count=1`: PASS fuera del sandbox; la primera
  ejecución confinada no podía consultar `lo` ni abrir sockets UDP.
- `go vet ./...`: PASS.
- Build estático de verificación en `/tmp/fog-proxy-debug`: PASS.
- `gofmt -l` y `git diff --check`: sin salida.

# TODO: Diagrama de funcionamiento en el README español

## Objetivo
Incluir `images/funcionamiento1.png` en la explicación por fases de
`README-ES.md`.

## Tareas
- [x] Revisar la imagen y localizar la sección adecuada del README.
- [x] Añadir la imagen con texto introductorio y texto alternativo en español.
- [x] Verificar la ruta relativa, el formato Markdown y el diff final.

## Resultados
- Imagen añadida tras la lista de fases de la sección `Cómo funciona`.
- Ruta relativa comprobada: `images/funcionamiento1.png`.
- `git diff --check -- README-ES.md tasks/todo.md` sin errores.
