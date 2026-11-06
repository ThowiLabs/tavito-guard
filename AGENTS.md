# Desarrollo

Mantener Go puro y compilación CGO_ENABLED=0. Linux amd64 y arm64 son objetivos de entrega.
Aplicar Ponytail y AntiSlop durante desarrollo. Leer DESIGN.md para las tarjetas.
Antes de cambiar código, revisar contexto/ y tareas/. Mantener una sola tarea en-proceso.
Usar gofmt, go test -race ./..., go vet ./... y compilaciones estáticas.
No agregar gestión de servicios ni monitoreo de sitios/aplicaciones. svcforge administra únicamente la instalación de Tavito Guard.
No registrar tokens ni contraseñas. Las operaciones sensibles requieren autorización y confirmación de un solo uso.
