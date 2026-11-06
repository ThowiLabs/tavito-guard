# Fecha
2026-11-05
# Objetivo
Corregir la instalación fallida del usuario en Dex/OpenRC, versión 0.1.1.
# Decisiones tomadas
AppSpec utiliza RestartNever como base portable. Lifecycle detecta el gestor con la API pública de svcforge y activa RestartOnFailure solo para systemd. OpenRC conserva AutoStart y la ejecución al arranque, sin prometer supervisión de caídas no implementada por la biblioteca.
# Arquitectura actual
Mismo binario estático y misma dependencia svcforge. No cambia configuración, credenciales ni estado persistente.
# Librerías usadas
Sin cambios de versión.
# Archivos importantes modificados
internal/guard/lifecycle.go, lifecycle_test.go, testdata/openrcfixture, cmd/tavito-guard/main.go, Makefile y documentación.
# Problemas encontrados
El usuario confirmó getMe y configuración guardada, pero el renderizador OpenRC rechazó RestartOnFailure. La prueba previa solo ejecutaba --version en Alpine y no había detectado este contrato de servicio.
# Soluciones implementadas
Selección de política por gestor. Regresión de OpenRC y systemd. Fixture aislado que reproduce el error anterior usando el backend real de svcforge y luego completa install/repair/remove con comandos OpenRC simulados. SHA256 de configuración idéntico antes y después. Fixture protegido por entorno y marcador de chroot para impedir ejecución accidental sobre un servidor real.
# Pendientes
Comprobar --install con el token y OpenRC real del servidor del usuario.
# Próximos pasos
Publicar 0.1.1 con autor y committer ThowiLabs; el usuario reemplaza el binario y repite --install usando su configuración existente.
