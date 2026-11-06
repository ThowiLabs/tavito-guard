# Fecha
2026-11-05
# Objetivo
Evitar bloqueos y seguimiento de enlaces durante lectura de archivos administrados.
# Decisiones tomadas
Los respaldos solo abren archivos regulares. Las aperturas de respaldo y logs usan O_NONBLOCK y O_NOFOLLOW; los logs de /var/log se confinan con os.Root.
# Arquitectura actual
Sin cambios de estructura ni dependencias. Se usa x/sys/unix ya existente.
# Librerías usadas
Las mismas versiones fijadas.
# Archivos importantes modificados
internal/guard/files.go; internal/guard/special_files_test.go.
# Problemas encontrados
Una FIFO dentro de una carpeta de respaldo podía bloquear una apertura normal antes de comprobar su tipo.
# Soluciones implementadas
Filtrar tipos antes de abrir y comprobar tipo del descriptor abierto. Prueba de regresión con FIFO real y rechazo de enlace.
# Pendientes
Verificación en el servidor de destino con token real, descrita en docs/VALIDACION.md.
# Próximos pasos
Empaquetado y entrega del proyecto completo.
