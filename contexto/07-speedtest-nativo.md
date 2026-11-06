# Fecha
2026-11-06 UTC
# Objetivo
Corregir el HTTP 403 del test de velocidad sin depender de Python ni de un programa externo.
# Decisiones tomadas
Motor HTTP nativo con biblioteca estándar de Go. Selección de servidores HTTPS de Speedtest.net por latencia y alternativa LibreSpeed. Conserva muestra máxima de 16 MiB leídos y 4 MiB enviados, compartida por todos los intentos; catálogos, latencia y transporte añaden tráfico. Hasta cuatro transferencias simultáneas, 12 segundos por fase y 90 segundos por operación. Mostrar ambos servidores cuando difieren.
# Arquitectura actual
speed.go valida catálogos acotados, destinos HTTPS y direcciones públicas en conexión/redirección. Los presupuestos reservan y restituyen bytes no consumidos. El contador de subida sincroniza Read/Close para respuestas HTTP anticipadas. Se rechazan páginas de bloqueo, descargas vacías/insuficientes y confirmaciones HTML de subida. El botón usa el mismo motor que --speedtest, que funciona sin token ni configuración.
# Librerías usadas
Sin nuevas dependencias. Se revisaron sivel/speedtest-cli (Python, Apache-2.0) y showwin/speedtest-go (Go, MIT) como referencia de protocolos y selección. Implementación independiente; no se copió código. Catálogos públicos Speedtest.net y LibreSpeed. Ninguna llamada a Python, curl ni speedtest-cli desde el bot.
# Archivos importantes modificados
internal/guard/speed.go, speed_test.go, integration_test.go, menus.go; cmd/tavito-guard/main.go, Makefile, README.md y docs/VALIDACION.md.
# Problemas encontrados
El proveedor fijo anterior rechazó la petición con HTTP 403. La prueba anterior no verificaba alternativas. Las peticiones POST necesitan Content-Length, y los bytes consumidos antes de un rechazo deben permanecer contados.
# Soluciones implementadas
Cambio de servidor/catálogo y cuotas globales incluso con rechazos o respuestas cortadas; cancelación propagada sin inventar resultados. Versión 0.1.3. Autoría y committer ThowiLabs.
# Validación realizada
Pruebas de velocidad correctas, incluido el botón mediante HTTP local y Telegram simulado. Suite completa -race: 114.385 s. Tras reforzar el reconocimiento de subida se repitieron las pruebas del motor con -race: 7.630 s. go vet y git diff --check correctos. Compilación CGO_ENABLED=0 amd64/arm64 e inspección ELF sin INTERP/DYNAMIC. amd64 ejecutado con --version dentro de Alpine 3.24.4 por chroot.
Medición real inicial desde el entorno disponible: Karlsruhe · Contabo, 65.006 Mbps descarga, 67.663 Mbps subida, 7.45 ms HTTP; 16777216 bytes de descarga y 4194304 de subida. No son datos del servidor del usuario.
# Pendientes
El servidor de destino debe repetir el botón tras actualizar. No se recibió acceso al bot real. Servidores y catálogos públicos pueden cambiar.
# Próximos pasos
Entregar los binarios estáticos 0.1.3 y conservar configuración, propietario y sesión al reinstalar.
