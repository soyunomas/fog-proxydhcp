# Orquestación del Flujo de Trabajo

**1. Modo Plan por Defecto**
* Entra en modo planificación para CUALQUIER tarea no trivial (3+ pasos o decisiones arquitectónicas).
* Si algo se tuerce, DETENTE y vuelve a planificar inmediatamente — no sigas avanzando sin control.
* Usa el modo planificación también para pasos de verificación, no solo para construir.
* Escribe especificaciones detalladas desde el principio para reducir ambigüedad.

**2. Estrategia de Subagentes**
* Delega investigación, exploración y análisis en paralelo a subagentes para mantener limpio el contexto principal.
* Una tarea por subagente para mantener el enfoque.

**3. Bucle de Auto-Mejora**
* Después de CUALQUIER corrección del usuario: actualiza `tasks/lessons.md`.
* Escribe reglas para evitar repetir errores. Itera sin piedad. Revisa al inicio de cada sesión.

**4. Verificación Antes de Dar por Terminado**
* Nunca marques una tarea como completada sin demostrar que funciona.
* Ejecuta pruebas, revisa logs y demuestra la corrección. Pregúntate: “¿Un ingeniero senior aprobaría esto?”

**5. Exigir Elegancia (con equilibrio)**
* Para cambios no triviales: “¿Hay una forma más elegante?”. Si se siente improvisada, implementa la solución elegante.
* Omite esto para cambios simples y obvios — no sobreingenierizar.

**6. Corrección Autónoma de Bugs**
* Ante un bug: arréglalo directamente. Cero necesidad de que el usuario cambie de contexto.
* Revisa logs, tests fallidos y soluciona fallos de CI sin guía paso a paso.

**7. Gestión de Tareas**
* Planifica Primero en `tasks/todo.md`. Marca tareas, explica cambios, documenta resultados.

---

# 80 Reglas de Desarrollo Eficiente en Go

> Reglas que aplican desarrolladores Go expertos y que esta aplicación (servicio de red,
> SQLite, concurrencia, sockets raw, systemd) **debe cumplir**.

## A. Errores y control de flujo
1. Devuelve `error` como último valor de retorno; nunca uses `panic` para flujo normal.
2. Envuelve errores con contexto usando `fmt.Errorf("...: %w", err)` para preservar la cadena.
3. Comprueba errores con `errors.Is` y extrae tipos con `errors.As`; no compares strings de error.
4. Define errores centinela exportados (`var ErrNotFound = errors.New(...)`) para casos esperados.
5. No ignores errores con `_` salvo justificación explícita (ej. `defer f.Close()` documentado).
6. Falla rápido: valida en los límites del sistema (entrada de usuario, red) y confía dentro.
7. `panic` solo para invariantes imposibles; usa `recover` únicamente en límites de goroutine/handler.
8. Un error de inicialización crítica (DB, NIC) debe abortar el arranque, no degradar en silencio.
9. No registres y devuelvas el mismo error a la vez (doble logging); decide quién lo maneja.
10. Añade contexto útil al error (IP, MAC, interfaz) sin filtrar secretos.

## B. Concurrencia
11. Propaga `context.Context` como primer parámetro en operaciones cancelables/bloqueantes.
12. Todo bucle de larga duración (worker, sniffer) escucha `ctx.Done()` para apagado limpio.
13. No guardes un `Context` en structs; pásalo explícitamente por parámetro.
14. Cada goroutine debe tener una salida garantizada; evita fugas de goroutines.
15. Usa `sync.WaitGroup` o `errgroup.Group` para esperar el cierre de goroutines.
16. Protege estado compartido con mutex o canales; ejecuta tests con `-race` siempre.
17. Prefiere "compartir memoria comunicando" (canales) cuando modele un flujo, no un estado.
18. Cierra canales solo desde el emisor, nunca desde el receptor.
19. Usa `sync.Once` para inicialización única (carga de OUI, config singleton).
20. No uses `time.Sleep` para sincronizar goroutines; usa canales, `WaitGroup` o tickers.
21. Aplica timeouts/`context.WithTimeout` a operaciones de red e I/O.
22. Limita la concurrencia con semáforos (canal buffer o `golang.org/x/sync/semaphore`).
23. El apagado por SIGTERM/SIGINT cancela el context raíz y espera el drenaje con timeout.

## C. Base de datos (SQLite)
24. Abre SQLite en modo WAL y configura `busy_timeout` para evitar `SQLITE_BUSY`.
25. Con un solo proceso multi-goroutine, serializa escrituras (un pool/conn de escritura).
26. Usa siempre consultas parametrizadas; nunca concatenes SQL (anti inyección).
27. Cierra `*sql.Rows` y comprueba `rows.Err()` tras iterar.
28. Pasa `context` a `QueryContext`/`ExecContext` para respetar cancelaciones.
29. Usa transacciones para operaciones multi-paso; `defer tx.Rollback()` y `tx.Commit()` al final.
30. Releer config cada ciclo (equivalente al `session.remove()` Python) para ver cambios en vivo.
31. Define índices para columnas de búsqueda/orden (mac_address único, last_seen).
32. Versiona el esquema con migraciones (`goose`); nunca alteres tablas a mano en producción.
33. Mapea NULL con `sql.NullString`/punteros o usa `sqlc` para tipado seguro.

## D. Estructura, API y módulos
34. Organiza por dominio en `internal/` (db, scanner, api, auth, config), no por capa genérica.
35. Mantén `cmd/<binario>/main.go` mínimo: parseo de flags y wiring; lógica en paquetes.
36. Exporta lo mínimo; minúsculas por defecto para mantener la superficie pública pequeña.
37. Acepta interfaces, devuelve structs concretos.
38. Define interfaces en el paquete consumidor, no en el productor.
39. Mantén interfaces pequeñas (1–3 métodos); evita interfaces "god".
40. Evita dependencias circulares; extrae tipos compartidos a un paquete neutro.
41. No uses `init()` con efectos secundarios ocultos; prefiere constructores explícitos.
42. Inyecta dependencias por parámetros/constructores; evita estado global mutable.
43. Nombres idiomáticos: sin stutter (`scanner.New`, no `scanner.NewScanner`).

## E. HTTP / servicio web
44. Configura `http.Server` con `ReadTimeout`, `WriteTimeout`, `IdleTimeout`.
45. Implementa `Shutdown(ctx)` para cierre gradual del servidor HTTP.
46. Usa middleware para auth, CSRF, recovery y logging; no repitas lógica por handler.
47. Valida y acota entrada (paginación: límites de `per_page`; whitelist de `sort_by`).
48. Devuelve códigos HTTP correctos y cuerpos JSON consistentes (`{error: ...}`).
49. Embebe assets/plantillas con `embed.FS` para un binario autocontenido.
50. No expongas detalles internos de error al cliente; loguea el detalle, responde genérico.

## F. Rendimiento y memoria
51. Perfila antes de optimizar (`pprof`, benchmarks `testing.B`); no adivines.
52. Preasigna slices/maps con capacidad conocida (`make([]T, 0, n)`).
53. Pasa structs grandes por puntero; los pequeños por valor.
54. Reutiliza buffers con `sync.Pool` en rutas calientes (parseo de paquetes).
55. Evita asignaciones en bucles internos; mira `-benchmem` para asignaciones por op.
56. Usa `strings.Builder` para concatenación repetida, no `+` en bucle.
57. Prefiere `[]byte` sobre `string` cuando manipules paquetes de red para evitar copias.
58. No abuses de reflexión ni de `interface{}`/`any` en rutas de alto rendimiento.

## G. Red y sockets raw (específico de esta app)
59. Construye paquetes DHCP/ARP con librerías tipadas (`insomniacslk/dhcp`, `gopacket`), no a mano.
60. Cierra handles de captura y sockets raw con `defer`; gestiona reinicios del sniffer.
61. Documenta y requiere capacidades mínimas (`cap_net_raw`, `cap_net_admin`), no root pleno.
62. Aplica timeouts y backoff en operaciones de red; nunca bloquees indefinidamente.
63. Respeta `dry_run`: ninguna ruta de envío real debe ejecutarse cuando esté activo.
64. Maneja interfaces caídas/cambiadas: detecta y reinicia captura sin tumbar el servicio.
65. Filtra captura a nivel BPF (udp port 67/68) para no procesar tráfico irrelevante.

## H. Testing
66. Tests de tabla (`table-driven`) con subtests `t.Run` y nombres descriptivos.
67. Ejecuta siempre con `-race` en CI; trata data races como fallo bloqueante.
68. Usa `t.TempDir()` para SQLite de test; no toques la DB real.
69. Inyecta dependencias de red/tiempo tras interfaces para poder mockear.
70. Usa `t.Helper()` en helpers de aserción; marca paralelizables con `t.Parallel()`.
71. Cubre rutas de error, no solo el camino feliz (releases fallidos, NIC ausente).
72. Añade benchmarks para parseo de paquetes y queries críticas.

## I. Calidad, build y operación
73. Formatea con `gofmt`/`goimports` y pasa `go vet` y `golangci-lint` sin warnings.
74. Compila estático con `CGO_ENABLED=0` para un binario portable (mantén todo pure-Go).
75. Usa logging estructurado con `log/slog`; salida a journald bajo systemd.
76. No registres secretos (SECRET_KEY, hashes, tokens) en logs.
77. Lee config de flags/env (`os.Getenv`); valida al arranque y falla con mensaje claro.
78. Inyecta versión/commit con `-ldflags -X` para trazabilidad en producción.
79. Maneja SIGTERM en el unit systemd (`Restart=on-failure`) y libera recursos al salir.
80. Mantén `go.mod` ordenado (`go mod tidy`); fija versiones y revisa vulnerabilidades (`govulncheck`).

---
