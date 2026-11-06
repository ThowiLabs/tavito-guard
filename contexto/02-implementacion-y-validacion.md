# Fecha
2026-11-05
# Objetivo
Entregar Tavito Guard 0.1.0 con el alcance acordado.
# Decisiones tomadas
Sin gestión general de servicios ni monitoreo de sitios/aplicaciones. svcforge solo para el propio bot.
Una cuenta propietaria vinculada al primer login válido; sesión persistente; --reset-owner local para recuperación.
Confirmaciones de 60 segundos, de un solo uso y ligadas a usuario/chat. Operaciones largas con contexto cancelable.
Actualizaciones remotas requieren firma Ed25519 y releases públicas. No se incluye ninguna clave ni token real.
# Arquitectura actual
cmd/tavito-guard (CLI); cmd/tavito-release (firmas); internal/guard (bot y módulos).
Config 0600; directorio de datos 0700; JSON atómico; bitácora 256 y muestras 1440/24h.
Lecturas proc/sysfs, cliente HTTP de Telegram sin SDK pesado, Docker por Unix y PNG en Go.
# Librerías usadas
svcforge v0.0.0-20260921000210-d170b162edda; x/image v0.47.0; x/sys v0.49.0; x/term v0.47.0; x/text v0.43.0 indirecta.
go.mod requiere Go 1.26.0 y fija toolchain go1.27.3.
# Archivos importantes modificados
cmd/, internal/guard/, README.md, Makefile, THIRD_PARTY.md, licenses/, docs/VALIDACION.md, go.mod y go.sum.
# Problemas encontrados
La primera revisión de vulnerabilidades se compiló con Go 1.26 y no pudo interpretar paquetes Go 1.27.
Un timeout del comando síncrono de race fue insuficiente para los hashes de prueba con instrumentación.
Se detectó un encabezado Authorization cuyo valor necesitaba ocultarse completo.
# Soluciones implementadas
Analizador fijado v1.9.0 ejecutado con GOTOOLCHAIN=go1.27.3; resultado sin vulnerabilidades conocidas.
Pruebas race mediante proceso persistente; suite correcta y pruebas adicionales correctas.
Redacción completa del valor sensible de la línea; prueba de regresión.
# Pendientes
Verificar el bot en Telegram real con el token del propietario y el ciclo de instalación en el servidor destino.
Configurar clave pública y publicar releases firmadas si se desea actualización remota.
# Próximos pasos
Completar empaquetado con Git y binarios estáticos, entregar el archivo e instrucciones de --install.
