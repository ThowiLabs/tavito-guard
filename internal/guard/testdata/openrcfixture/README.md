# Fixture de regresión OpenRC

Harness de prueba para un chroot desechable. No es un comando del producto ni se instala con Tavito Guard.

Solo funciona con TAVITO_OPENRC_QA=1 y un marcador /.tavito-openrc-fixture con el valor TAVITO-OPENRC-ISOLATED-QA dentro del chroot preparado. Usa credenciales ficticias y llamadas simuladas a rc-service/rc-update. La biblioteca svcforge genera realmente la definición, procesa manifiestos y ejecuta instalación/reparación/eliminación.

--reproduce fuerza RestartOnFailure y exige que el backend produzca el error reportado por el usuario. --operation install/repair/remove usa Lifecycle de Tavito Guard con la política corregida.

No ejecuta el bot real ni sustituye una prueba con OpenRC nativo y un token real. Nunca preparar el marcador en un servidor productivo.
