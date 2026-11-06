# Tavito Guard

Bot privado de administración de servidores Linux mediante Telegram. Go puro, compilación estática y navegación por botones.

## Inicio rápido

El paquete incluye binarios para Linux amd64 y arm64 dentro de `dist/`. No necesitas instalar Go para usarlos. Selecciona la arquitectura correspondiente a tu servidor.

Como root, en amd64:

```sh
chmod +x dist/tavito-guard-linux-amd64
./dist/tavito-guard-linux-amd64 --install
```

En arm64:

```sh
chmod +x dist/tavito-guard-linux-arm64
./dist/tavito-guard-linux-arm64 --install
```

El instalador inicial realiza este orden:

1. Pide el token del bot y lo valida con `getMe` de Telegram.
2. Pide el usuario de Tavito Guard.
3. Pide y confirma una contraseña de 12 a 256 bytes.
4. Guarda configuración privada e instala el propio bot mediante [svcforge](https://github.com/YahirHub/svcforge).

Después abre el chat privado de tu bot, pulsa **Iniciar sesión** e introduce esas credenciales. El primer acceso correcto vincula el servidor a tu ID de Telegram. La sesión sobrevive a reinicios del bot y del sistema. **Salir** y `/exit` cierran la sesión. Se conserva la vinculación con el propietario.

Usa una contraseña exclusiva para Tavito Guard: el inicio de sesión se escribe en un chat de Telegram. El bot intenta borrar el mensaje de contraseña al recibirlo. El token se introduce únicamente en la consola local.

## Qué incluye

| Menú | Funciones |
|---|---|
| Recursos | CPU, RAM, GPU accesible y tarjeta PNG generada dentro del binario |
| Historial | Una muestra por minuto, hasta 24 horas; curvas con períodos sin datos visibles |
| Test de velocidad | 16 MiB de descarga y 4 MiB de subida por HTTPS a Cloudflare; latencia HTTP |
| Discos | Uso, disponibilidad, inodos y exploración acotada de carpetas |
| Procesos | Listado por CPU/RAM, detalles y SIGTERM con confirmación y verificación de identidad |
| Red | Interfaces, IP locales, contadores RX/TX, IP pública de salida y prueba TCP |
| Registros | Bitácora, archivos seleccionados de /var/log, búsqueda, páginas y descarga acotada |
| Docker | Contenedores, uso, logs, iniciar/detener/reiniciar con confirmación |
| Respaldos | Agregar carpetas, crear copias locales, retención, programación y envío opcional al chat |
| Alertas | CPU, RAM y discos; umbrales por botones, recuperación e intervalo entre repeticiones |
| Accesos | Estado de sesión, cambio de contraseña, revocación y bitácora |
| Mantenimiento | Reinicio del servidor y actualización con firma Ed25519 |

La gestión general de servicios y el monitoreo de sitios o aplicaciones quedaron fuera del alcance. El servicio del propio Tavito Guard se utiliza solo para su instalación y ejecución.

La navegación usa botones. Las credenciales, las rutas nuevas y el texto de búsqueda requieren escritura.

## Linux y dependencias de ejecución

- Binarios Linux amd64 y arm64 con `CGO_ENABLED=0`, sin glibc, musl, navegador, Python ni Node como dependencias del bot.
- Para instalación automática: un sistema con systemd u OpenRC activos. svcforge detecta el gestor.
- Para HTTPS: conexión de salida a Telegram y certificados raíz del sistema. En Alpine instala `ca-certificates` si tu imagen no incluye certificados.
- CPU, memoria y procesos se obtienen de `/proc`; GPU de `/sys` cuando el controlador expone métricas.
- NVIDIA puede aportar datos adicionales si su herramienta `nvidia-smi` ya existe. No se instala automáticamente.
- Docker requiere su daemon y permiso de acceso al socket Unix configurado. El resto del bot funciona sin Docker.
- La terminación protegida de procesos requiere soporte del kernel para pidfd. Si falta, la operación devuelve un error; no recurre a enviar señales por PID sin fijar la identidad.
- La instalación utiliza root para las acciones de administración solicitadas. En ejecución manual, las operaciones se limitan a los permisos del usuario del proceso.

Las métricas reflejan lo visible desde el espacio de nombres del bot. Dentro de un contenedor, `/proc`, GPU y red pueden representar un ámbito distinto del host físico.

## Otras formas de ejecutar

Crear configuración sin instalar:

```sh
./dist/tavito-guard-linux-amd64 --setup --config /ruta/privada/config.json --data-dir /ruta/privada/data
./dist/tavito-guard-linux-amd64 --serve --config /ruta/privada/config.json
```

Consultar, reparar o quitar la instalación:

```sh
tavito-guard --status
tavito-guard --repair
tavito-guard --remove
```

`--uninstall` es alias de `--remove`. La desinstalación conserva configuración, sesión, historial y respaldos. Reinstalar con una configuración existente conserva las credenciales y vuelve a validar el token. Para instalar una versión más nueva usa `--install`; para la misma versión usa `--repair`.

Crear una tarjeta de recursos sin iniciar el bot:

```sh
./dist/tavito-guard-linux-amd64 --render recursos.png
```

## Datos y límites

| Dato | Ruta o límite |
|---|---|
| Ejecutable instalado | /usr/local/bin/tavito-guard |
| Configuración | /etc/tavito-guard/config.json, modo 0600 |
| Datos | /var/lib/tavito-guard, modo 0700 |
| Estado persistente | state.json, escritura atómica |
| Historial | 1440 muestras y ventana de 24 horas |
| Bitácora | 256 acciones |
| Log de ejecución | runtime.log y runtime.log.1, aproximadamente 1 MiB cada uno |
| Registros externos | Últimas 256 KiB por consulta |
| Operaciones largas | Máximo dos simultáneas; una por usuario; tiempo límite de dos minutos |
| Confirmaciones | Un solo uso, ligadas al usuario/chat, vencen en 60 segundos |
| Acceso | Bloqueo global de 15 minutos tras cinco intentos incorrectos |
| Respaldos | 256 MiB de datos y de archivo comprimido, 20000 entradas, tres archivos por defecto |
| Envío de archivos | Hasta 48 MiB por envío |
| Carpetas y registros configurados | Hasta ocho de cada uno mediante el menú |

El límite y la retención de respaldos se pueden modificar en el archivo de configuración con el bot detenido. La retención es global para todos los respaldos, no por carpeta.

Los respaldos contienen archivos regulares; omiten enlaces simbólicos. Se impide respaldar rutas virtuales y carpetas que se superpongan con los datos o la configuración del propio bot. Las bases de datos y archivos que cambian mientras se copian necesitan su propio mecanismo de consistencia; la copia no realiza snapshots del sistema de archivos.

La programación se cambia por botones entre desactivada, seis, doce y veinticuatro horas. Solo se ejecuta mientras el bot está funcionando. Un intento fallido también cuenta para el intervalo, evitando reintentos continuos.

Las alertas se revisan cada minuto. Se informa al superar el umbral, como máximo una vez cada 30 minutos mientras persista, y al recuperarse cinco puntos por debajo. Salir pausa las notificaciones; las muestras y respaldos programados continúan. Para detectar la caída completa del servidor se necesita supervisión externa.

La ocultación de credenciales en registros cubre el token del bot y campos comunes; no garantiza reconocer todos los secretos que cualquier aplicación pueda escribir. Selecciona los registros que necesitas consultar.

## Actualizaciones firmadas

El botón de actualización funciona cuando existe una release con manifiesto firmado y `update_public_key` está configurado. El paquete inicial no incorpora claves privadas ni presupone una release publicada.

Genera una clave local con la utilidad incluida (amd64) o compílala en tu arquitectura:

```sh
./dist/tavito-release --generate-key --key tavito-update.key
```

Guarda la clave privada fuera del repositorio. Copia únicamente la clave pública mostrada al campo `update_public_key` de la configuración y reinicia el bot.

Para firmar un artefacto de una versión nueva:

```sh
./dist/tavito-release --key tavito-update.key --artifact dist/tavito-guard-linux-amd64 --version 0.2.0 --arch amd64 --url https://github.com/YahirHub/tavito-guard/releases/download/v0.2.0/tavito-guard-linux-amd64 --out manifest-linux-amd64.json
```

Genera también el manifiesto arm64 con su binario y arquitectura. Publica ambos binarios y `manifest-linux-amd64.json` / `manifest-linux-arm64.json` en la release correspondiente.

El bot consulta el manifiesto de la última release, valida su firma, plataforma, versión y destino HTTPS, descarga con límite de 64 MiB, verifica SHA256 y ELF estático y solicita confirmación. svcforge realiza la instalación con respaldo de configuración/estado y comprobación de arranque. En systemd el actualizador se inicia mediante una unidad transitoria para sobrevivir al cierre del servicio.

También puedes actualizar localmente ejecutando `--install` desde el binario nuevo. No necesitas habilitar la actualización desde Telegram para usar el resto del bot.

## Recuperar el propietario

Con acceso local al servidor, detén **el propio Tavito Guard** antes de restablecer la vinculación:

```sh
tavito-guard --reset-owner
```

El comando rechaza ejecutarse si el mismo directorio de datos tiene otra instancia activa. Después vuelve a iniciar Tavito Guard y realiza un login correcto desde la cuenta deseada.

## Compilar y probar

Se utilizó Go 1.27.3. `go.mod` fija el compilador y `go.sum` fija las dependencias. Se requiere Go para compilar; no para ejecutar los binarios.

```sh
make test
make vet
make race
make build
```

Las pruebas usan API de Telegram y Docker simuladas. La prueba de señales crea su propio proceso desechable. No ejecutan reinicios ni detienen procesos ajenos.

Consulta [docs/VALIDACION.md](docs/VALIDACION.md) para la evidencia y las comprobaciones que requieren una instalación real.

## Estructura

- `cmd/tavito-guard`: línea de comandos e inicio del bot.
- `cmd/tavito-release`: generación de claves y manifiestos.
- `internal/guard`: autenticación, persistencia, métricas, PNG, Telegram y módulos.
- `contexto/`: decisiones y continuidad.
- `tareas/`: estado del trabajo.
- `dist/`: ejecutables incluidos en el paquete descargable, excluidos de Git.
- `docs/`: validación.
- `licenses/`: avisos de las dependencias redistribuidas.

Las dependencias Go puras son svcforge (ciclo de instalación), x/image (tipografía y PNG), x/sys (interfaces Linux), x/term (entrada oculta) y x/text (dependencia de tipografía). No se usa SQLite, un navegador ni un proceso externo para generar tarjetas.
