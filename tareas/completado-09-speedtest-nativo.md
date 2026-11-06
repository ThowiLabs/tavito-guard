# Speedtest nativo en Go
Completado en 0.1.3.

El proveedor fijo Cloudflare devolvía HTTP 403. Se sustituyó por HTTP de la biblioteca estándar Go, selección de servidores HTTPS Speedtest.net y alternativa LibreSpeed. Sin intérpretes ni ejecutables externos. Cuotas compartidas de 16/4 MiB, cancelación y plazos, validación de destinos y resultado con nombres de servidores. El mismo motor funciona mediante el botón o --speedtest.

Pruebas completas -race correctas (114.385 s), pruebas específicas finales -race correctas (7.630 s), vet y revisión de whitespace correctos. Dos mediciones reales desde el entorno de desarrollo completaron descarga y subida. Binarios estáticos amd64/arm64; --version comprobado en Alpine chroot. Evidencia y límites en docs/VALIDACION.md y contexto/07-speedtest-nativo.md.

Autor y committer ThowiLabs.
