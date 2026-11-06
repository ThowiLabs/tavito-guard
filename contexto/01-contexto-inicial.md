# Fecha
2026-11-05
# Objetivo
Construir Tavito Guard (tavito-guard), bot privado de administración Linux, Go puro y estático.
# Decisiones tomadas
Sesión persistente vinculada al ID de Telegram; propietario vinculado tras login correcto. /exit y Salir revocan sesión. Navegación por botones salvo credenciales y filtros.
Incluir recursos, velocidad, discos, procesos, logs, red, alertas, accesos, Docker, respaldos, mantenimiento e historial. Excluir servicios y sitios/aplicaciones. svcforge se usa solo para el propio ciclo de instalación.
# Arquitectura actual
Un binario, librería estándar y dependencias Go puras justificadas. Datos JSON atómicos privados, API Telegram por long polling, PNG local sin navegador.
# Librerías usadas
svcforge fijado a d170b162eddab1b719e52d0ee297516e9a60744a. Go 1.27.3 disponible según go.dev.
# Archivos importantes modificados
AGENTS.md, DESIGN.md, go.mod y tareas/.
# Problemas encontrados
El acceso web a svcforge falló; se verificó el código oficial mediante cl.
# Soluciones implementadas
Se inspeccionaron App, Runner, Options, rutas y gestores Linux.
# Pendientes
Implementación, pruebas de botones y seguridad, compilación estática y entrega.
# Próximos pasos
Persistencia/autenticación, métricas/tarjetas, módulos y Telegram, ciclo de instalación, validación.
