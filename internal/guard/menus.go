package guard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func integer(s string) int {
	n, e := strconv.Atoi(s)
	if e != nil || n < 0 || n > 1000000000 {
		return -1
	}
	return n
}
func (b *Bot) route(ctx context.Context, id, chat int64, data string) error {
	cfg := b.cfg()
	p := splitRoute(data)
	if p[0] == "docker" || p[0] == "container" || p[0] == "containerlogs" || p[0] == "containeraction" {
		if !b.dockerAvailable(ctx) {
			return b.route(ctx, id, chat, "menu")
		}
	}
	switch p[0] {
	case "menu":
		b.mu.Lock()
		delete(b.inputs, id)
		b.mu.Unlock()
		host, _ := os.Hostname()
		return b.show(ctx, chat, "Tavito Guard\n"+host+" · v"+b.Version+"\n\nSelecciona qué necesitas consultar o administrar.", b.mainMenu(ctx))
	case "logout":
		return b.logout(ctx, id, chat)
	case "resources":
		return b.job(ctx, id, chat, "Estado de recursos", func(work context.Context) error {
			s, e := Collect(work)
			if e != nil {
				return e
			}
			img, e := ResourceCard(s)
			if e != nil {
				return e
			}
			return b.photo(work, chat, "recursos.png", ResourceText(s), img, KB([]Button{Btn("Actualizar", "resources"), Btn("Historial", "history")}, []Button{Btn("Menú", "menu")}))
		})
	case "history":
		return b.job(ctx, id, chat, "Historial de recursos", func(work context.Context) error {
			st := b.Store.View()
			img, e := HistoryCard(st.History, time.Now())
			if e != nil {
				return e
			}
			return b.photo(work, chat, "historial.png", fmt.Sprintf("Historial de 24 horas · %d muestras reales. Una muestra por minuto.", len(st.History)), img, KB([]Button{Btn("Actualizar", "history"), Btn("Recursos", "resources")}, []Button{Btn("Menú", "menu")}))
		})
	case "speed":
		return b.show(ctx, chat, "Test de velocidad del servidor\n\nElige servidores HTTPS por latencia. Si uno falla, prueba otro. Muestra de hasta 16 MiB de descarga y 4 MiB de subida.\n\nDuración máxima: 90 segundos. La latencia es HTTP.", KB([]Button{Btn("Iniciar prueba", "speed-run")}, []Button{Btn("Volver", "menu")}))
	case "speed-run":
		return b.job(ctx, id, chat, "Test de velocidad", func(work context.Context) error {
			r, e := SpeedTest(work, b.networkClient())
			if e != nil {
				return e
			}
			return b.show(work, chat, fmt.Sprintf("Test de velocidad completado\n\nDescarga: %.2f Mbps\nSubida: %.2f Mbps\nLatencia HTTP: %.1f ms\nMuestra: %s descargados · %s enviados\n\nServidor de descarga: %s\nServidor de subida: %s\n\nEstimación HTTP con muestra limitada.", r.DownloadMbps, r.UploadMbps, r.LatencyMS, Bytes(uint64(r.DownloadBytes)), Bytes(uint64(r.UploadBytes)), r.DownloadServer, r.UploadServer), KB([]Button{Btn("Repetir", "speed"), Btn("Menú", "menu")}))
		})
	case "disks":
		return b.disksView(ctx, chat, 0)
	case "diskpage":
		if len(p) == 2 {
			return b.disksView(ctx, chat, integer(p[1]))
		}
	case "folders":
		if len(p) != 2 {
			break
		}
		ds, e := Disks()
		i := integer(p[1])
		if e != nil {
			return e
		}
		if i < 0 || i >= len(ds) {
			return errors.New("disco no disponible; actualiza la lista")
		}
		d := ds[i]
		return b.job(ctx, id, chat, "Carpetas de "+d.Path, func(work context.Context) error {
			rows, partial, e := FolderSizes(work, d.Path)
			if e != nil {
				return e
			}
			text := "Carpetas · " + d.Path + "\nTamaño lógico de archivos regulares; los enlaces se omiten.\n"
			for _, r := range rows {
				text += fmt.Sprintf("\n%s · %s", r.Name, Bytes(r.Bytes))
			}
			if partial {
				text += "\n\nExploración parcial por límite de 20000 entradas, 5 segundos o permisos."
			}
			return b.show(work, chat, text, nav("disks"))
		})
	case "processes":
		return b.processesView(ctx, id, chat, "cpu", 0)
	case "proclist":
		if len(p) == 3 && (p[1] == "cpu" || p[1] == "ram") {
			return b.processesView(ctx, id, chat, p[1], integer(p[2]))
		}
	case "process":
		if len(p) != 3 {
			break
		}
		pr, e := ReadProcess(integer(p[1]))
		if e != nil || pr.Start != p[2] {
			return b.show(ctx, chat, "El proceso terminó o cambió. Actualiza la lista.", nav("processes"))
		}
		return b.show(ctx, chat, fmt.Sprintf("Proceso %d · %s\nRAM residente: %s\n\nDetener enviará SIGTERM al proceso seleccionado.", pr.PID, pr.Name, Bytes(pr.RSS)), KB([]Button{Btn("Detener proceso", fmt.Sprintf("kill:%d:%s", pr.PID, pr.Start))}, []Button{Btn("Volver", "processes"), Btn("Menú", "menu")}))
	case "kill":
		if len(p) != 3 {
			break
		}
		pr, e := ReadProcess(integer(p[1]))
		if e != nil || pr.Start != p[2] {
			return errors.New("el proceso cambió; actualiza la lista")
		}
		if pr.PID <= 1 || pr.PID == os.Getpid() || pr.PID == os.Getppid() {
			return b.show(ctx, chat, "Este proceso está protegido.", nav("processes"))
		}
		return b.confirm(ctx, id, chat, confirmation{Kind: "kill", Process: pr}, fmt.Sprintf("¿Detener %s (PID %d) mediante SIGTERM?", pr.Name, pr.PID))
	case "network":
		ns, e := Network()
		if e != nil {
			return e
		}
		text := "Red del servidor\nContadores acumulados desde el arranque de cada interfaz.\n"
		for _, n := range ns {
			text += fmt.Sprintf("\n%s\n%s\nRecibido: %s · Enviado: %s\n", n.Name, strings.Join(n.Addresses, ", "), Bytes(n.RX), Bytes(n.TX))
		}
		return b.show(ctx, chat, text, KB([]Button{Btn("IP pública", "publicip"), Btn("Comprobar conexión", "connectivity")}, []Button{Btn("Actualizar", "network"), Btn("Menú", "menu")}))
	case "publicip":
		return b.job(ctx, id, chat, "Consultar IP pública", func(work context.Context) error {
			req, _ := http.NewRequestWithContext(work, "GET", "https://api.ipify.org", nil)
			r, e := b.networkClient().Do(req)
			if e != nil {
				return errors.New("no se pudo consultar la IP pública")
			}
			defer r.Body.Close()
			body, e := io.ReadAll(io.LimitReader(r.Body, 128))
			ip := strings.TrimSpace(string(body))
			if e != nil || r.StatusCode != 200 || parseIP(ip) == nil {
				return errors.New("respuesta de IP pública inválida")
			}
			return b.show(work, chat, "IP pública de salida: "+ip, nav("network"))
		})
	case "connectivity":
		return b.job(ctx, id, chat, "Comprobar conexión", func(work context.Context) error {
			start := time.Now()
			conn, e := b.connect(work, "tcp", "1.1.1.1:443")
			if e != nil {
				return errors.New("no se pudo establecer conexión TCP con 1.1.1.1:443")
			}
			defer conn.Close()
			return b.show(work, chat, fmt.Sprintf("Conexión TCP disponible\nDestino: 1.1.1.1:443\nTiempo de conexión: %.1f ms\nNo es una medición ICMP.", float64(time.Since(start).Microseconds())/1000), nav("network"))
		})
	case "logs":
		return b.logsMenu(ctx, chat)
	case "log":
		if len(p) == 3 {
			return b.logView(ctx, id, chat, integer(p[1]), integer(p[2]), b.logFilter(id, integer(p[1])))
		}
	case "log-clear":
		if len(p) == 2 {
			b.mu.Lock()
			delete(b.filters, fmt.Sprintf("%d:%d", id, integer(p[1])))
			b.mu.Unlock()
			return b.logView(ctx, id, chat, integer(p[1]), 0, "")
		}
	case "logfilter":
		if len(p) == 2 {
			return b.ask(ctx, id, chat, "logfilter", p[1], "Escribe el texto que deseas buscar (máximo 60 caracteres). La búsqueda usa las últimas 256 KiB.")
		}
	case "logexport":
		if len(p) != 2 {
			break
		}
		i := integer(p[1])
		lines, e := b.logLines(i, "")
		if e != nil {
			return e
		}
		return b.API.File(ctx, chat, "sendDocument", "registro.txt", "Extracto de 256 KiB. Credenciales comunes ocultas.", []byte(strings.Join(lines, "\n")), nav("logs"))
	case "logadd":
		return b.ask(ctx, id, chat, "logadd", "", "Escribe la ruta absoluta de un archivo regular dentro de /var/log. No se admiten rutas de configuración.")
	case "docker":
		return b.dockerView(ctx, id, chat)
	case "container":
		if len(p) == 2 {
			return b.containerView(ctx, id, chat, p[1])
		}
	case "containerlogs":
		if len(p) != 2 {
			break
		}
		co, e := b.findContainer(ctx, p[1])
		if e != nil {
			return e
		}
		return b.job(ctx, id, chat, "Registros del contenedor", func(work context.Context) error {
			s, e := NewDocker(cfg.DockerSocket).Logs(work, co.ID)
			if e != nil {
				return e
			}
			return b.show(work, chat, Redact(s, cfg.Token), nav("container:"+co.ID[:12]))
		})
	case "containeraction":
		if len(p) != 3 || (p[2] != "start" && p[2] != "stop" && p[2] != "restart") {
			break
		}
		co, e := b.findContainer(ctx, p[1])
		if e != nil {
			return e
		}
		return b.confirm(ctx, id, chat, confirmation{Kind: "docker-" + p[2], Value: co.ID}, "¿Ejecutar "+p[2]+" en "+strings.Join(co.Names, ", ")+" ("+co.ID[:12]+")?")
	case "backups":
		return b.backupsView(ctx, chat)
	case "backupadd":
		return b.ask(ctx, id, chat, "backupadd", "", "Escribe la ruta absoluta de la carpeta que quieres respaldar. Se omiten enlaces simbólicos. La copia de datos que cambian durante la lectura puede ser inconsistente.")
	case "backup":
		if len(p) != 2 {
			break
		}
		i := integer(p[1])
		if i < 0 || i >= len(cfg.BackupRoots) {
			return errors.New("carpeta no disponible")
		}
		t := cfg.BackupRoots[i]
		return b.confirm(ctx, id, chat, confirmation{Kind: "backup", Value: t.Path}, "¿Crear respaldo local de "+t.Path+"?\nLímite: "+fmt.Sprint(cfg.BackupMaxMB)+" MiB. Conservación global: "+fmt.Sprint(cfg.BackupKeep)+" archivos.")
	case "backup-remove":
		if len(p) != 2 {
			break
		}
		i := integer(p[1])
		if i < 0 || i >= len(cfg.BackupRoots) {
			return errors.New("carpeta no disponible")
		}
		return b.confirm(ctx, id, chat, confirmation{Kind: "backup-remove", Value: cfg.BackupRoots[i].Path}, "¿Quitar esta carpeta de la programación?\n"+cfg.BackupRoots[i].Path+"\nLos archivos ya creados se conservan.")
	case "backupfiles":
		files, e := BackupList(cfg.DataDir)
		if e != nil {
			return e
		}
		k := nav("backups")
		for i, f := range files {
			if i >= 10 {
				break
			}
			k.Rows = append([][]Button{{Btn(f, "backupfile:"+strconv.Itoa(i))}}, k.Rows...)
		}
		return b.show(ctx, chat, fmt.Sprintf("Respaldos locales: %d\nSelecciona uno para solicitar su envío a Telegram. El envío incluye los datos de la carpeta respaldada.", len(files)), k)
	case "backupfile":
		if len(p) != 2 {
			break
		}
		files, e := BackupList(cfg.DataDir)
		i := integer(p[1])
		if e != nil {
			return e
		}
		if i < 0 || i >= len(files) {
			return errors.New("respaldo no disponible")
		}
		return b.confirm(ctx, id, chat, confirmation{Kind: "backup-send", Value: files[i]}, "¿Enviar "+files[i]+" a este chat de Telegram?\nEl archivo puede contener información privada de la carpeta.")
	case "backupperiod":
		e := b.Store.Change(func(s *State) error {
			switch s.Alerts.BackupEveryHours {
			case 0:
				s.Alerts.BackupEveryHours = 6
			case 6:
				s.Alerts.BackupEveryHours = 12
			case 12:
				s.Alerts.BackupEveryHours = 24
			default:
				s.Alerts.BackupEveryHours = 0
			}
			return nil
		})
		if e != nil {
			return e
		}
		return b.backupsView(ctx, chat)
	case "alerts":
		return b.alertsView(ctx, chat)
	case "alert-toggle":
		if e := b.Store.Change(func(s *State) error { s.Alerts.Enabled = !s.Alerts.Enabled; return nil }); e != nil {
			return e
		}
		return b.alertsView(ctx, chat)
	case "threshold":
		if len(p) != 2 {
			break
		}
		e := b.Store.Change(func(s *State) error {
			var v *float64
			switch p[1] {
			case "cpu":
				v = &s.Alerts.CPU
			case "ram":
				v = &s.Alerts.RAM
			case "disk":
				v = &s.Alerts.Disk
			default:
				return errors.New("umbral inválido")
			}
			*v += 5
			if *v > 95 {
				*v = 70
			}
			return nil
		})
		if e != nil {
			return e
		}
		return b.alertsView(ctx, chat)
	case "access":
		st := b.Store.View()
		text := fmt.Sprintf("Accesos\nPropietario Telegram: %d\nSesiones: %d\n", st.OwnerID, len(st.Sessions))
		for _, s := range st.Sessions {
			text += "\nDesde " + s.Since.UTC().Format(time.RFC3339) + "\n"
		}
		return b.show(ctx, chat, text, KB([]Button{Btn("Cambiar contraseña", "passwordchange")}, []Button{Btn("Revocar sesión", "logout"), Btn("Bitácora", "audit")}, []Button{Btn("Menú", "menu")}))
	case "passwordchange":
		return b.ask(ctx, id, chat, "newpassword", "", "Escribe la nueva contraseña de Tavito Guard (4 a 256 bytes). Se cerrará tu sesión.")
	case "audit":
		return b.show(ctx, chat, "Bitácora (últimos registros)\n"+tailLines(b.Store.AuditText(), 16), KB([]Button{Btn("Descargar bitácora", "auditexport"), Btn("Volver", "access")}, []Button{Btn("Menú", "menu")}))
	case "auditexport":
		return b.API.File(ctx, chat, "sendDocument", "bitacora.txt", "Últimas 256 acciones conservadas.", []byte(b.Store.AuditText()), nav("access"))
	case "maintenance":
		return b.show(ctx, chat, "Mantenimiento\nTavito Guard v"+b.Version+"\n\nLas operaciones se aplican únicamente a este servidor.", KB([]Button{Btn("Buscar actualización", "update")}, []Button{Btn("Reiniciar servidor", "reboot")}, []Button{Btn("Menú", "menu")}))
	case "reboot":
		return b.confirm(ctx, id, chat, confirmation{Kind: "reboot"}, "¿Reiniciar el servidor completo? Se interrumpirán sus conexiones y procesos.")
	case "update":
		if cfg.UpdatePublicKey == "" {
			return b.show(ctx, chat, "Actualizaciones firmadas\n\nFalta configurar update_public_key. Genera una clave con tavito-release y configura solo su parte pública. O instala una versión nueva con --install.", nav("maintenance"))
		}
		return b.job(ctx, id, chat, "Buscar actualización firmada", func(work context.Context) error {
			m, e := FetchUpdate(work, cfg, b.Version)
			if e != nil {
				return e
			}
			return b.confirm(work, id, chat, confirmation{Kind: "update", Value: m.Version}, "Versión disponible: "+m.Version+"\n¿Descargar, verificar firma e instalar? svcforge preservará datos y comprobará el arranque.")
		})
	case "confirm":
		if len(p) != 2 {
			break
		}
		c, e := b.consume(id, chat, p[1])
		if e != nil {
			return b.show(ctx, chat, e.Error(), nav("menu"))
		}
		return b.execute(ctx, id, chat, c)
	}
	return b.show(ctx, chat, "Esta opción ya no está disponible. Abre el menú para continuar.", b.mainMenu(ctx))
}
func tailLines(text string, n int) string {
	ls := strings.Split(strings.TrimSpace(text), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, "\n")
}
func (b *Bot) disksView(ctx context.Context, chat int64, page int) error {
	ds, e := Disks()
	if e != nil {
		return e
	}
	if page < 0 || page*6 > len(ds) {
		page = 0
	}
	text := "Discos y particiones\n"
	k := Keyboard{}
	end := page*6 + 6
	if end > len(ds) {
		end = len(ds)
	}
	for i := page * 6; i < end; i++ {
		d := ds[i]
		text += fmt.Sprintf("\n%s (%s)\nUsado: %s / %s (%.1f %%)\nDisponible: %s · Inodos: %.1f %%\n", d.Path, d.Type, Bytes(d.Used), Bytes(d.Total), Percent(d.Used, d.Total), Bytes(d.Available), Percent(d.InodesUsed, d.InodesTotal))
		k.Rows = append(k.Rows, []Button{Btn("Carpetas: "+trimText(d.Path, 30), "folders:"+strconv.Itoa(i))})
	}
	var row []Button
	if page > 0 {
		row = append(row, Btn("Anterior", "diskpage:"+strconv.Itoa(page-1)))
	}
	if end < len(ds) {
		row = append(row, Btn("Siguiente", "diskpage:"+strconv.Itoa(page+1)))
	}
	if len(row) > 0 {
		k.Rows = append(k.Rows, row)
	}
	k.Rows = append(k.Rows, []Button{Btn("Actualizar", "disks"), Btn("Menú", "menu")})
	return b.show(ctx, chat, text, k)
}
func (b *Bot) processesView(ctx context.Context, id, chat int64, by string, page int) error {
	return b.job(ctx, id, chat, "Procesos por "+strings.ToUpper(by), func(work context.Context) error {
		ps, e := Processes(work, by)
		if e != nil {
			return e
		}
		if page < 0 || page*8 >= len(ps) {
			page = 0
		}
		end := page*8 + 8
		if end > len(ps) {
			end = len(ps)
		}
		text := "Procesos\nCPU como porcentaje del total del sistema. RAM residente.\n"
		k := KB([]Button{Btn("Ordenar CPU", "proclist:cpu:0"), Btn("Ordenar RAM", "proclist:ram:0")})
		for _, pr := range ps[page*8 : end] {
			text += fmt.Sprintf("\n%d · %s\nCPU %.1f %% · RAM %s\n", pr.PID, pr.Name, pr.CPU, Bytes(pr.RSS))
			k.Rows = append(k.Rows, []Button{Btn(strconv.Itoa(pr.PID)+" · "+trimText(pr.Name, 28), fmt.Sprintf("process:%d:%s", pr.PID, pr.Start))})
		}
		var row []Button
		if page > 0 {
			row = append(row, Btn("Anterior", fmt.Sprintf("proclist:%s:%d", by, page-1)))
		}
		if end < len(ps) {
			row = append(row, Btn("Siguiente", fmt.Sprintf("proclist:%s:%d", by, page+1)))
		}
		if len(row) > 0 {
			k.Rows = append(k.Rows, row)
		}
		k.Rows = append(k.Rows, []Button{Btn("Menú", "menu")})
		return b.show(work, chat, text, k)
	})
}
func (b *Bot) logsMenu(ctx context.Context, chat int64) error {
	k := KB([]Button{Btn("Bitácora de Tavito", "log:0:0")})
	for i, t := range b.cfg().LogFiles {
		k.Rows = append(k.Rows, []Button{Btn(t.Name, fmt.Sprintf("log:%d:0", i+1))})
	}
	k.Rows = append(k.Rows, []Button{Btn("Agregar registro", "logadd"), Btn("Menú", "menu")})
	return b.show(ctx, chat, "Registros\nLee las últimas 256 KiB por archivo. Oculta campos comunes de credenciales.", k)
}
func (b *Bot) logLines(i int, filter string) ([]string, error) {
	if i == 0 {
		lines := strings.Split(strings.TrimSpace(b.Store.AuditText()), "\n")
		if filter == "" {
			return lines, nil
		}
		var out []string
		for _, l := range lines {
			if strings.Contains(strings.ToLower(l), strings.ToLower(filter)) {
				out = append(out, l)
			}
		}
		return out, nil
	}
	c := b.cfg()
	if i < 1 || i > len(c.LogFiles) {
		return nil, errors.New("registro no disponible")
	}
	return ReadLog(c.LogFiles[i-1].Path, c.Token, filter)
}
func (b *Bot) logView(ctx context.Context, id, chat int64, i, page int, filter string) error {
	lines, e := b.logLines(i, filter)
	if e != nil {
		return b.show(ctx, chat, "No se pudo leer este registro. Comprueba que exista y que Tavito Guard tenga permiso de lectura.", nav("logs"))
	}
	if page < 0 || page*6 >= len(lines) {
		page = 0
	}
	end := len(lines) - page*6
	start := end - 6
	if start < 0 {
		start = 0
	}
	text := "Registro\n"
	if filter != "" {
		text += "Filtro: " + filter + " (vista filtrada)\n"
	}
	if len(lines) == 0 {
		text += "No hay líneas disponibles para esta selección."
	} else {
		text += strings.Join(lines[start:end], "\n")
	}
	k := KB([]Button{Btn("Buscar texto", "logfilter:"+strconv.Itoa(i)), Btn("Descargar", fmt.Sprintf("logexport:%d", i))})
	{
		var row []Button
		if page > 0 {
			row = append(row, Btn("Más recientes", fmt.Sprintf("log:%d:%d", i, page-1)))
		}
		if start > 0 {
			row = append(row, Btn("Más antiguas", fmt.Sprintf("log:%d:%d", i, page+1)))
		}
		if len(row) > 0 {
			k.Rows = append(k.Rows, row)
		}
	}
	if filter != "" {
		k.Rows = append(k.Rows, []Button{Btn("Quitar filtro", fmt.Sprintf("log-clear:%d", i))})
	}
	k.Rows = append(k.Rows, []Button{Btn("Volver", "logs"), Btn("Menú", "menu")})
	return b.show(ctx, chat, text, k)
}
func (b *Bot) findContainer(ctx context.Context, prefix string) (Container, error) {
	if len(prefix) != 12 {
		return Container{}, errors.New("selección Docker inválida")
	}
	list, e := NewDocker(b.cfg().DockerSocket).List(ctx)
	if e != nil {
		return Container{}, e
	}
	var match []Container
	for _, c := range list {
		if dockerID.MatchString(c.ID) && strings.HasPrefix(c.ID, prefix) {
			match = append(match, c)
		}
	}
	if len(match) != 1 {
		return Container{}, errors.New("el contenedor cambió; actualiza la lista")
	}
	return match[0], nil
}
func (b *Bot) dockerView(ctx context.Context, id, chat int64) error {
	return b.job(ctx, id, chat, "Docker", func(work context.Context) error {
		list, e := NewDocker(b.cfg().DockerSocket).List(work)
		if e != nil {
			return b.show(work, chat, e.Error(), nav("menu"))
		}
		text := "Contenedores Docker\n"
		k := Keyboard{}
		if len(list) == 0 {
			text += "No hay contenedores disponibles."
		}
		for _, c := range list {
			if !dockerID.MatchString(c.ID) {
				continue
			}
			name := strings.TrimPrefix(strings.Join(c.Names, ", "), "/")
			text += "\n" + name + " · " + c.State
			k.Rows = append(k.Rows, []Button{Btn(trimText(name, 40), "container:"+c.ID[:12])})
		}
		k.Rows = append(k.Rows, []Button{Btn("Actualizar", "docker"), Btn("Menú", "menu")})
		return b.show(work, chat, text, k)
	})
}
func (b *Bot) containerView(ctx context.Context, id, chat int64, prefix string) error {
	return b.job(ctx, id, chat, "Estado del contenedor", func(work context.Context) error {
		c, e := b.findContainer(work, prefix)
		if e != nil {
			return e
		}
		text := strings.Join(c.Names, ", ") + "\nImagen: " + c.Image + "\nEstado: " + c.Status
		if c.State == "running" {
			u, e := NewDocker(b.cfg().DockerSocket).Usage(work, c.ID)
			if e == nil {
				text += fmt.Sprintf("\nCPU: %.1f %% (100 %% por núcleo)\nRAM: %s / %s", u.CPU, Bytes(u.Used), Bytes(u.Limit))
			} else {
				text += "\nNo se pudieron consultar las métricas."
			}
		}
		k := KB([]Button{Btn("Iniciar", "containeraction:"+prefix+":start"), Btn("Detener", "containeraction:"+prefix+":stop")}, []Button{Btn("Reiniciar", "containeraction:"+prefix+":restart"), Btn("Registros", "containerlogs:"+prefix)}, []Button{Btn("Volver", "docker"), Btn("Menú", "menu")})
		return b.show(work, chat, text, k)
	})
}
func (b *Bot) backupsView(ctx context.Context, chat int64) error {
	c := b.cfg()
	st := b.Store.View()
	period := "Desactivada"
	if st.Alerts.BackupEveryHours > 0 {
		period = fmt.Sprintf("Cada %d horas", st.Alerts.BackupEveryHours)
	}
	text := fmt.Sprintf("Respaldos\nLímite por copia: %d MiB\nConservar: %d archivos en total\nProgramación: %s\n", c.BackupMaxMB, c.BackupKeep, period)
	k := Keyboard{}
	if len(c.BackupRoots) == 0 {
		text += "\nAgrega una carpeta para crear o programar respaldos."
	}
	for i, t := range c.BackupRoots {
		text += "\n" + t.Name + " · " + t.Path
		k.Rows = append(k.Rows, []Button{Btn("Respaldar "+t.Name, fmt.Sprintf("backup:%d", i)), Btn("Quitar", fmt.Sprintf("backup-remove:%d", i))})
	}
	k.Rows = append(k.Rows, []Button{Btn("Agregar carpeta", "backupadd"), Btn("Ver archivos", "backupfiles")}, []Button{Btn("Cambiar programación", "backupperiod")}, []Button{Btn("Menú", "menu")})
	return b.show(ctx, chat, text, k)
}
func (b *Bot) alertsView(ctx context.Context, chat int64) error {
	a := b.Store.View().Alerts
	status := "Desactivadas"
	if a.Enabled {
		status = "Activadas"
	}
	text := fmt.Sprintf("Alertas · %s\nCPU: %.0f %%\nRAM: %.0f %%\nDiscos: %.0f %%\n\nSe revisan cada minuto. Se repite una alerta persistente como máximo cada 30 minutos. Recuperación: 5 puntos por debajo del umbral.\n\nSalir pausa las notificaciones. Para detectar una caída completa del servidor hace falta un monitor externo.", status, a.CPU, a.RAM, a.Disk)
	return b.show(ctx, chat, text, KB([]Button{Btn("Activar/desactivar", "alert-toggle")}, []Button{Btn("Umbral CPU +5", "threshold:cpu"), Btn("Umbral RAM +5", "threshold:ram")}, []Button{Btn("Umbral disco +5", "threshold:disk")}, []Button{Btn("Menú", "menu")}))
}
func (b *Bot) handleInput(ctx context.Context, id, chat, messageID int64, in input, text string) error {
	cfg := b.cfg()
	switch in.Step {
	case "newpassword":
		_ = b.API.Delete(ctx, chat, messageID)
		password, e := NewPassword(text)
		text = ""
		if e != nil {
			return b.show(ctx, chat, e.Error(), nav("access"))
		}
		if e = b.Store.Logout(id); e != nil {
			return e
		}
		b.clear(id)
		cfg.Password = password
		if e = b.saveConfig(cfg); e != nil {
			return e
		}
		return b.show(ctx, chat, "Contraseña actualizada. Inicia sesión de nuevo.", loginMenu())
	case "backupadd":
		if len(cfg.BackupRoots) >= 8 {
			return b.show(ctx, chat, "Límite de 8 carpetas alcanzado.", nav("backups"))
		}
		path, e := ValidateBackupRoot(strings.TrimSpace(text), cfg.DataDir)
		if e != nil {
			return b.show(ctx, chat, e.Error(), nav("backups"))
		}
		for _, t := range cfg.BackupRoots {
			if t.Path == path {
				return b.show(ctx, chat, "La carpeta ya está configurada.", nav("backups"))
			}
		}
		cfg.BackupRoots = append(cfg.BackupRoots, Target{trimText(filepath.Base(path), 40), path})
		if e = b.saveConfig(cfg); e != nil {
			return e
		}
		_ = b.Store.Record(id, "agregar-carpeta-respaldo", "correcto")
		return b.backupsView(ctx, chat)
	case "logadd":
		if len(cfg.LogFiles) >= 8 {
			return b.show(ctx, chat, "Límite de 8 registros alcanzado.", nav("logs"))
		}
		path, e := filepath.EvalSymlinks(strings.TrimSpace(text))
		if e != nil || !filepath.IsAbs(path) || !within(path, "/var/log") {
			return b.show(ctx, chat, "Selecciona un archivo dentro de /var/log.", nav("logs"))
		}
		fi, e := os.Stat(path)
		if e != nil || !fi.Mode().IsRegular() {
			return b.show(ctx, chat, "El registro no existe o no es un archivo regular.", nav("logs"))
		}
		for _, t := range cfg.LogFiles {
			if t.Path == path {
				return b.logsMenu(ctx, chat)
			}
		}
		cfg.LogFiles = append(cfg.LogFiles, Target{trimText(filepath.Base(path), 40), path})
		if e = b.saveConfig(cfg); e != nil {
			return e
		}
		return b.logsMenu(ctx, chat)
	case "logfilter":
		if len([]rune(text)) > 60 {
			return b.show(ctx, chat, "El filtro supera 60 caracteres.", nav("logs"))
		}
		b.mu.Lock()
		b.filters[fmt.Sprintf("%d:%d", id, integer(in.Value))] = text
		b.mu.Unlock()
		return b.logView(ctx, id, chat, integer(in.Value), 0, text)
	}
	return b.route(ctx, id, chat, "menu")
}
func (b *Bot) execute(ctx context.Context, id, chat int64, c confirmation) error {
	if !b.Store.Authorized(id, chat) {
		return errors.New("sesión revocada")
	}
	cfg := b.cfg()
	switch c.Kind {
	case "kill":
		return b.job(ctx, id, chat, "Detener proceso", func(work context.Context) error {
			if e := Terminate(c.Process); e != nil {
				return e
			}
			return b.show(work, chat, "Se envió SIGTERM. El proceso puede tardar en finalizar.", nav("processes"))
		})
	case "docker-start", "docker-stop", "docker-restart":
		if !b.dockerAvailable(ctx) {
			return errors.New("acción no permitida")
		}
		return b.job(ctx, id, chat, "Cambiar estado Docker", func(work context.Context) error {
			if e := NewDocker(cfg.DockerSocket).Action(work, c.Value, strings.TrimPrefix(c.Kind, "docker-")); e != nil {
				return e
			}
			return b.show(work, chat, "Docker completó la solicitud.", nav("docker"))
		})
	case "backup":
		return b.job(ctx, id, chat, "Crear respaldo", func(work context.Context) error {
			var target *Target
			for _, t := range cfg.BackupRoots {
				if t.Path == c.Value {
					copy := t
					target = &copy
					break
				}
			}
			if target == nil {
				return errors.New("carpeta ya no autorizada")
			}
			path, e := CreateBackup(work, cfg, *target)
			if e != nil {
				return e
			}
			return b.show(work, chat, "Respaldo local creado: "+filepath.Base(path), nav("backups"))
		})
	case "backup-remove":
		var out []Target
		for _, t := range cfg.BackupRoots {
			if t.Path != c.Value {
				out = append(out, t)
			}
		}
		cfg.BackupRoots = out
		if e := b.saveConfig(cfg); e != nil {
			return e
		}
		return b.backupsView(ctx, chat)
	case "backup-send":
		return b.job(ctx, id, chat, "Enviar respaldo", func(work context.Context) error {
			data, e := ReadBackup(cfg.DataDir, c.Value)
			if e != nil {
				return e
			}
			return b.API.File(work, chat, "sendDocument", c.Value, "Respaldo solicitado del servidor.", data, nav("backups"))
		})
	case "reboot":
		if e := b.Store.Record(id, "reiniciar-servidor", "solicitado"); e != nil {
			return e
		}
		if e := b.show(ctx, chat, "Reinicio solicitado. Tavito Guard volverá cuando arranque el servidor.", Keyboard{}); e != nil {
			return e
		}
		return b.requestReboot(ctx)
	case "update":
		return b.job(ctx, id, chat, "Instalar actualización", func(work context.Context) error {
			m, e := FetchUpdate(work, cfg, b.Version)
			if e != nil {
				return e
			}
			if m.Version != c.Value {
				return errors.New("la versión cambió; busca y confirma de nuevo")
			}
			path, e := DownloadUpdate(work, cfg, m)
			if e != nil {
				return e
			}
			if e = b.show(work, chat, "Firma verificada. Iniciando actualización a "+m.Version+". El servicio se reconectará al terminar.", Keyboard{}); e != nil {
				return e
			}
			return StartUpdate(work, path, b.ConfigPath)
		})
	}
	return errors.New("acción no permitida")
}
