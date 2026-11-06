# Validación de Tavito Guard 0.1.0

Fecha: 2026-11-05 (America/Mexico_City). Entorno de construcción: Linux amd64, Go 1.27.3.

## Resultado técnico

- Suite de pruebas: correcta. Incluye autenticación persistente, bloqueo de intentos, permisos, escritura transaccional y estado dañado.
- Suite con detector de carreras: correcta. El recorrido inicial completo terminó en 99.577 s sin carreras reportadas. Las pruebas adicionales de respaldos, filtros, cambio de contraseña y selección de procesos pasaron también con -race.
- go vet: correcto.
- govulncheck v1.9.0, ejecutado con Go 1.27.3: No vulnerabilities found.
- Binarios Linux amd64 y arm64: compilados con CGO_ENABLED=0. readelf no muestra segmentos INTERP ni DYNAMIC.
- Binario amd64 ejecutado mediante chroot en Alpine 3.24.4: Tavito Guard 0.1.0.
- Archivo oficial Alpine utilizado: alpine-minirootfs-3.24.4-x86_64.tar.gz; SHA256 cdaa38cb8de3537967ee5b6c9b6cd00cc962da2b018e6eea48696bb274923012.
- PNG de recursos: generado con datos reales del entorno de pruebas, abierto e inspeccionado visualmente. Sin solapamientos ni texto cortado en el caso observado. Incluido como ejemplo, no como medición del servidor de destino.

El primer intento del analizador de vulnerabilidades se compiló con una versión de Go distinta y no pudo cargar los paquetes. Se repitió con GOTOOLCHAIN=go1.27.3 y la versión fijada v1.9.0. La evidencia válida es el resultado del segundo intento.

## Recorrido de controles

La interacción se verificó enviando callbacks a una API de Telegram simulada y revisando los mensajes, teclados y archivos generados. No se afirma que se hayan pulsado botones en un cliente de Telegram real.

| Control | Evidencia |
|---|---|
| Iniciar sesión | Solicita usuario, luego contraseña; crea sesión solo tras validación y solicita borrar el mensaje sensible |
| Salir y /exit | Revocan estado persistente; callbacks posteriores solicitan login |
| Recursos / Actualizar | Recopila datos Linux, envía un PNG decodificable y un resumen |
| Historial | Genera PNG y explica el estado con menos de dos muestras |
| Discos | Renderiza uso, disponibilidad, inodos y botones de exploración |
| Procesos | Lista datos reales; selección obsoleta rechazada; PID propio protegido |
| Detener | La función de terminación fija la identidad con pidfd y detiene solo un proceso de prueba creado para ello |
| Red | Lista interfaces; IP pública y comprobación TCP probadas con sustitutos controlados de red |
| Velocidad | Transferencias HTTP simuladas de 16 MiB y 4 MiB; cancelación comprobada |
| Registros | Consulta, filtro, limpieza del filtro y descarga; lectura acotada y redacción de credenciales verificadas |
| Docker | API Unix simulada: lista, consumo, logs y start/stop/restart con confirmación |
| Respaldos | Agregar carpeta, confirmar creación, listar, confirmar envío, quitar carpeta y cambiar programación |
| Alertas | Activación y cambio de umbrales por callbacks; lógica de revisión inspeccionada |
| Accesos | Consulta, bitácora, descarga y cambio de contraseña que revoca la sesión |
| Mantenimiento | Busca actualización o informa la clave que falta; reinicio probado con sustituto que registra una sola ejecución |
| Confirmar | Rechaza otro usuario, vencimiento y repetición |
| Volver / Menú / Cancelar | Rutas presentes y callbacks reconocidos; navegación con una o dos opciones por fila |

## Revisión de diseño y contenido

El alcance verificable aquí es el código del bot, las respuestas de API simuladas y los PNG. Telegram controla la apariencia y la accesibilidad de sus controles nativos. No hay HTML, CSS, ventanas ni temas personalizados del cliente.

| Regla | Estado y evidencia |
|---|---|
| R-01 | PASS: fondo neutro; acento para consumo; advertencias en función de porcentajes reales |
| R-02 | PASS: texto propio sin raya larga; mensajes concretos |
| R-03 | PASS en PNG y payloads: una columna, texto limitado y máximo dos botones por fila; cliente real pendiente |
| R-04 | PASS: botones de texto; sin iconos decorativos |
| R-05 | PASS: composición construida alrededor de recursos y tareas, sin secciones de plantilla |
| R-06 | PASS: Go Sans incorporada por legibilidad y despliegue sin fuentes externas |
| R-07 | PASS: fondo sólido, sin patrones decorativos |
| R-08 | PASS: botones con acciones nombradas, sin flechas decorativas |
| R-09 | PASS: sin insignias de marketing |
| R-10 | PASS: sin transparencias ni glassmorphism |
| R-11 | PASS: geometría simple de gráficos; el cliente controla los botones nativos |
| R-12 | PASS: sin sombras decorativas |
| R-13 | PASS: sin brillos |
| R-14 | PASS: cada métrica ocupa el espacio que requiere su contenido |
| R-15 | PASS: acciones nombradas: actualizar, detener, respaldar, confirmar y cancelar |
| R-16 | PASS: mensajes describen operaciones y límites concretos |
| R-17 | PASS: métricas del sistema o fixtures solo en pruebas; tarjeta de ejemplo rotulada como entorno de prueba en esta documentación |
| R-18 | PASS: no hay testimonios ni identidades ficticias de clientes |
| R-19 | PASS: PNG estático y estados de recopilación explícitos |
| R-20 | PASS: motivos de composición y voz registrados en DESIGN.md |
| R-21 | PASS: tarjeta de tema claro definido; Telegram conserva el tema del usuario |
| R-22 | PASS: sin ilustraciones decorativas |
| R-23 | PASS: nombre indicado por el propietario; tarjeta solicitada; no se inventó logotipo |
| R-24 | PASS: botones enrutados a pantallas presentes; módulos excluidos ausentes del menú |
| R-25 | PASS: texto oscuro sobre fondo claro; colores especificados para contraste y sin texto blanco sobre zonas variables |
| R-26 | PASS: controles incluidos tienen handler; condiciones no disponibles se explican al usuario |
| R-27 | PASS: recopilación, falta de muestras, hardware no accesible y errores con navegación de retorno |
| R-28 | PASS: no se creó FAQ de plantilla |
| R-29 | PASS: neutros y verde petróleo; ámbar/rojo reservados a uso alto |
| R-30 | PASS: composición propia de una tarjeta de recursos, sin copia de otro producto |
| R-31 | PASS: decisiones de color, fuente, tamaño, orden y botones explicadas en DESIGN.md |
| R-32 | PASS en alcance del bot: controles nativos de Telegram, sin componentes que anulen teclado o foco; comprobación del cliente fuera de esta prueba |
| R-33 | PASS: renderizado implementado directamente en Go, sin scripts que parcheen estilos |
| R-34 | PASS: no se entrega un selector de temas personalizado |
| R-35 | PASS: compilación, ejecución Linux/Alpine, recorridos API y revisión visual registrados; interacción real de Telegram no afirmada |
| R-36 | PASS: resultados y límites descritos sin certificaciones ni garantías inventadas |
| R-37 | PASS: dirección inicial clara y sobria documentada y comunicada durante el desarrollo |
| R-38 | PASS: datos reales o fixtures identificados en pruebas; sin estadísticas inventadas en producción |

Diales: ENERGY 1 / RHYTHM 2 / MOTION 1. El valor de cada recurso es el foco, el espacio separa las mediciones y el acento solo expresa consumo o estado.

## Límites de la verificación

- No se recibió un token real. La autenticación contra getMe y el arranque permanente deben verificarse al ejecutar --install en el servidor de destino.
- No se instaló ni eliminó un servicio real de systemd u OpenRC durante estas pruebas. Se validaron AppSpec y el contrato de integración con la revisión fijada de svcforge.
- arm64 se compiló y se inspeccionó como ELF estático; no se ejecutó en hardware arm64.
- No había GPU accesible en el entorno real de prueba. Parsers y disposición con varias GPU se comprobaron con fixtures.
- No se reinició el servidor ni se actuó sobre contenedores reales. Los callbacks de esas operaciones usaron sustitutos controlados.
- La actualización remota necesita una clave pública configurada y una release pública firmada. No se publicó ninguna release durante esta entrega.

## Repetir comprobaciones

```sh
make test
make vet
make race
GOTOOLCHAIN=go1.27.3 go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
make build
readelf -l dist/tavito-guard-linux-amd64
readelf -l dist/tavito-guard-linux-arm64
```
