package guard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type input struct {
	Step, Value string
	Expires     time.Time
}
type confirmation struct {
	User, Chat  int64
	Kind, Value string
	Process     Process
	Expires     time.Time
}
type Bot struct {
	filters map[string]string
	HTTP    *http.Client
	Dial    func(context.Context, string, string) (net.Conn, error)
	Reboot  func(context.Context) error

	API                 *Telegram
	Store               *Store
	ConfigPath, Version string
	mu                  sync.Mutex
	config              Config
	inputs              map[int64]input
	confirms            map[string]confirmation
	panels              map[int64]int64
	jobs                map[int64]context.CancelFunc
	workers             chan struct{}
	wg                  sync.WaitGroup
}

func NewBot(api *Telegram, store *Store, c Config, path, version string) *Bot {
	return &Bot{API: api, Store: store, config: c, ConfigPath: path, Version: version, inputs: map[int64]input{}, filters: map[string]string{}, confirms: map[string]confirmation{}, panels: map[int64]int64{}, jobs: map[int64]context.CancelFunc{}, workers: make(chan struct{}, 2)}
}
func (b *Bot) cfg() Config { b.mu.Lock(); defer b.mu.Unlock(); return b.config }
func (b *Bot) saveConfig(c Config) error {
	if e := SaveConfig(b.ConfigPath, c); e != nil {
		return e
	}
	b.mu.Lock()
	b.config = c
	b.mu.Unlock()
	return nil
}
func nav(back string) Keyboard {
	if back == "" {
		back = "menu"
	}
	return KB([]Button{Btn("Volver", back), Btn("Menú", "menu")})
}
func MainMenu(docker bool) Keyboard {
	rows := [][]Button{
		{Btn("Recursos", "resources"), Btn("Historial", "history")},
		{Btn("Test de velocidad", "speed"), Btn("Red", "network")},
		{Btn("Discos", "disks"), Btn("Procesos", "processes")},
	}
	if docker {
		rows = append(rows, []Button{Btn("Docker", "docker"), Btn("Registros", "logs")})
	} else {
		rows = append(rows, []Button{Btn("Registros", "logs")})
	}
	rows = append(rows,
		[]Button{Btn("Respaldos", "backups"), Btn("Alertas", "alerts")},
		[]Button{Btn("Accesos", "access"), Btn("Mantenimiento", "maintenance")},
		[]Button{Btn("Salir", "logout")},
	)
	return KB(rows...)
}

func (b *Bot) dockerAvailable(ctx context.Context) bool {
	return NewDocker(b.cfg().DockerSocket).Available(ctx)
}

func (b *Bot) mainMenu(ctx context.Context) Keyboard {
	return MainMenu(b.dockerAvailable(ctx))
}
func loginMenu() Keyboard { return KB([]Button{Btn("Iniciar sesión", "login")}) }
func trimText(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
func (b *Bot) show(ctx context.Context, chat int64, text string, k Keyboard) error {
	text = trimText(Redact(text, b.cfg().Token), 3800)
	b.mu.Lock()
	id := b.panels[chat]
	b.mu.Unlock()
	if id != 0 {
		if e := b.API.Edit(ctx, chat, id, text, k); e == nil {
			return nil
		} else {
			var api *APIError
			if errors.As(e, &api) && api.Code == 304 {
				return nil
			}
		}
	}
	m, e := b.API.Send(ctx, chat, text, k)
	if e == nil {
		b.mu.Lock()
		b.panels[chat] = m.ID
		b.mu.Unlock()
	}
	return e
}
func (b *Bot) setInput(id int64, step, value string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	for k, v := range b.inputs {
		if now.After(v.Expires) {
			delete(b.inputs, k)
		}
	}
	if len(b.inputs) >= 32 {
		return
	}
	b.inputs[id] = input{step, value, now.Add(5 * time.Minute)}
}
func (b *Bot) clear(id int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.inputs, id)
	for token, c := range b.confirms {
		if c.User == id {
			delete(b.confirms, token)
		}
	}
	if stop := b.jobs[id]; stop != nil {
		stop()
	}
}
func (b *Bot) ask(ctx context.Context, id, chat int64, step, value, prompt string) error {
	b.setInput(id, step, value)
	return b.show(ctx, chat, prompt, KB([]Button{Btn("Cancelar", "cancel")}))
}
func (b *Bot) confirm(ctx context.Context, id, chat int64, c confirmation, prompt string) error {
	token := make([]byte, 12)
	if _, e := rand.Read(token); e != nil {
		return e
	}
	c.User = id
	c.Chat = chat
	c.Expires = time.Now().Add(60 * time.Second)
	s := hex.EncodeToString(token)
	b.mu.Lock()
	for k, v := range b.confirms {
		if time.Now().After(v.Expires) {
			delete(b.confirms, k)
		}
	}
	if len(b.confirms) >= 32 {
		b.mu.Unlock()
		return errors.New("hay demasiadas confirmaciones pendientes")
	}
	b.confirms[s] = c
	b.mu.Unlock()
	host, _ := os.Hostname()
	return b.show(ctx, chat, prompt+"\n\nServidor: "+host+"\nLa confirmación vence en 60 segundos.", KB([]Button{Btn("Confirmar", "confirm:"+s), Btn("Cancelar", "cancel")}))
}
func (b *Bot) consume(id, chat int64, token string) (confirmation, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	c, ok := b.confirms[token]
	if !ok || c.User != id || c.Chat != chat || time.Now().After(c.Expires) {
		return c, errors.New("confirmación vencida; vuelve a seleccionar la acción")
	}
	delete(b.confirms, token)
	return c, nil
}
func (b *Bot) job(ctx context.Context, id, chat int64, name string, fn func(context.Context) error) error {
	b.mu.Lock()
	if b.jobs[id] != nil {
		b.mu.Unlock()
		return b.show(ctx, chat, "Ya tienes una operación en curso. Puedes cancelarla.", KB([]Button{Btn("Cancelar operación", "cancel")}, []Button{Btn("Menú", "menu")}))
	}
	select {
	case b.workers <- struct{}{}:
	default:
		b.mu.Unlock()
		return errors.New("hay dos operaciones en curso; reintenta en un momento")
	}
	work, cancel := context.WithTimeout(ctx, 2*time.Minute)
	b.jobs[id] = cancel
	b.mu.Unlock()
	if e := b.show(ctx, chat, name+"\nRecopilando datos…", KB([]Button{Btn("Cancelar operación", "cancel")})); e != nil {
		cancel()
		b.mu.Lock()
		delete(b.jobs, id)
		b.mu.Unlock()
		<-b.workers
		return e
	}
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		defer cancel()
		defer func() { <-b.workers; b.mu.Lock(); delete(b.jobs, id); b.mu.Unlock() }()
		e := fn(work)
		result := "correcto"
		if e != nil {
			result = "falló"
		}
		if err := b.Store.Record(id, name, result); err != nil {
			log.Print("No se pudo guardar bitácora")
		}
		if e != nil && b.Store.Authorized(id, chat) {
			msg := "La operación falló: " + e.Error()
			if work.Err() != nil {
				msg = "Operación cancelada o tiempo agotado."
			}
			if err := b.show(ctx, chat, msg, nav("menu")); err != nil {
				log.Print("No se pudo enviar resultado")
			}
		}
	}()
	return nil
}
func (b *Bot) photo(ctx context.Context, chat int64, name, caption string, data []byte, k Keyboard) error {
	if e := b.show(ctx, chat, caption, Keyboard{}); e != nil {
		return e
	}
	if e := b.API.File(ctx, chat, "sendPhoto", name, caption, data, k); e != nil {
		return e
	}
	b.mu.Lock()
	delete(b.panels, chat)
	b.mu.Unlock()
	return nil
}
func (b *Bot) handle(ctx context.Context, u Update) error {
	if cb := u.Callback; cb != nil {
		if cb.Message == nil || cb.From.IsBot || cb.Message.Chat.Type != "private" || cb.From.ID != cb.Message.Chat.ID {
			return nil
		}
		id, chat := cb.From.ID, cb.Message.Chat.ID
		if cb.Message.Text != "" {
			b.mu.Lock()
			b.panels[chat] = cb.Message.ID
			b.mu.Unlock()
		} else {
			b.mu.Lock()
			delete(b.panels, chat)
			b.mu.Unlock()
		}
		_ = b.API.Answer(ctx, cb.ID, "")
		if cb.Data == "login" {
			st := b.Store.View()
			if st.OwnerID != 0 && st.OwnerID != id {
				return b.show(ctx, chat, "Esta cuenta no está autorizada.", Keyboard{})
			}
			if b.Store.Authorized(id, chat) {
				return b.route(ctx, id, chat, "menu")
			}
			return b.ask(ctx, id, chat, "username", "", "Escribe tu usuario de Tavito Guard.")
		}
		if cb.Data == "cancel" {
			b.clear(id)
			if b.Store.Authorized(id, chat) {
				return b.route(ctx, id, chat, "menu")
			}
			return b.show(ctx, chat, "Operación cancelada.", loginMenu())
		}
		if !b.Store.Authorized(id, chat) {
			return b.show(ctx, chat, "Inicia sesión para administrar este servidor.", loginMenu())
		}
		return b.route(ctx, id, chat, cb.Data)
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot || m.Chat.Type != "private" || m.From.ID != m.Chat.ID {
		return nil
	}
	id, chat := m.From.ID, m.Chat.ID
	text := m.Text
	if text == "/exit" {
		b.clear(id)
		if e := b.Store.Logout(id); e != nil {
			return e
		}
		return b.show(ctx, chat, "Sesión cerrada.", loginMenu())
	}
	if text == "/start" || text == "/help" || text == "/settings" {
		b.clear(id)
		if b.Store.Authorized(id, chat) {
			return b.route(ctx, id, chat, "menu")
		}
		return b.show(ctx, chat, "Tavito Guard\nAdministración de tu servidor Linux. Inicia sesión para abrir el menú.", loginMenu())
	}
	b.mu.Lock()
	in, ok := b.inputs[id]
	if ok {
		delete(b.inputs, id)
	}
	b.mu.Unlock()
	if !ok || time.Now().After(in.Expires) {
		if b.Store.Authorized(id, chat) {
			return b.route(ctx, id, chat, "menu")
		}
		return b.show(ctx, chat, "Usa el botón para iniciar sesión.", loginMenu())
	}
	if in.Step == "username" {
		if len(text) > 64 || len(text) < 3 {
			return b.show(ctx, chat, "Usuario inválido. Vuelve a iniciar sesión.", loginMenu())
		}
		return b.ask(ctx, id, chat, "password", text, "Escribe tu contraseña de Tavito Guard. Intentaré borrar tu mensaje al recibirlo.")
	}
	if in.Step == "password" {
		erased := b.API.Delete(ctx, chat, m.ID) == nil
		e := b.Store.Login(id, chat, in.Value, text, b.cfg(), time.Now())
		text = ""
		if e != nil {
			return b.show(ctx, chat, e.Error(), loginMenu())
		}
		if !erased {
			_, _ = b.API.Send(ctx, chat, "No pude borrar el mensaje de contraseña. Elimínalo del chat.", Keyboard{})
		}
		return b.route(ctx, id, chat, "menu")
	}
	if !b.Store.Authorized(id, chat) {
		return b.show(ctx, chat, "Tu sesión ya no está activa.", loginMenu())
	}
	return b.handleInput(ctx, id, chat, m.ID, in, text)
}
func (b *Bot) Run(ctx context.Context) error {
	if _, e := b.API.Me(ctx); e != nil {
		return e
	}
	hb := filepath.Join(b.cfg().DataDir, "heartbeat.json")
	heartbeat := func() {
		if e := AtomicJSON(hb, map[string]any{"version": b.Version, "pid": os.Getpid(), "time": time.Now()}); e != nil {
			log.Print("No se pudo guardar heartbeat")
		}
	}
	heartbeat()
	loop, cancel := context.WithCancel(ctx)
	defer func() { cancel(); b.wg.Wait() }()
	b.wg.Add(1)
	go func() { defer b.wg.Done(); b.monitor(loop, heartbeat) }()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-loop.Done():
				return
			case <-ticker.C:
				heartbeat()
			}
		}
	}()
	delay := time.Second
	for ctx.Err() == nil {
		updates, e := b.API.Updates(ctx, b.Store.View().Offset)
		if e != nil {
			var api *APIError
			if errors.As(e, &api) && api.Code == 409 {
				return errors.New("otra instancia o un webhook está usando este bot")
			}
			if errors.As(e, &api) && api.RetryAfter > 0 {
				delay = time.Duration(api.RetryAfter) * time.Second
			}
			log.Print(e)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			if delay < 30*time.Second {
				delay *= 2
			}
			continue
		}
		delay = time.Second
		for _, u := range updates {
			// Se confirma el offset antes de actuar para que un reinicio no repita una operación sensible.
			if e = b.Store.Change(func(s *State) error { s.Offset = u.ID + 1; return nil }); e != nil {
				return e
			}
			if e = b.Handle(loop, u); e != nil {
				log.Print(Redact(e.Error(), b.cfg().Token))
			}
		}
	}
	return ctx.Err()
}
func (b *Bot) monitor(ctx context.Context, heartbeat func()) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		heartbeat()
		if s, e := Collect(ctx); e == nil {
			if e = b.Store.AddSnapshot(s); e != nil {
				log.Print("No se pudo persistir la muestra")
			}
			b.checkAlerts(ctx, s)
		}
		st := b.Store.View()
		if hours := st.Alerts.BackupEveryHours; hours > 0 && time.Since(st.LastBackup) >= time.Duration(hours)*time.Hour {
			cfg := b.cfg()
			if len(cfg.BackupRoots) > 0 {
				// Se limita cada intento de respaldo programado, incluso si falla, para evitar bucles.
				if b.Store.Change(func(s *State) error { s.LastBackup = time.Now(); return nil }) == nil {
					for _, target := range cfg.BackupRoots {
						c, cancel := context.WithTimeout(ctx, 2*time.Minute)
						_, e := CreateBackup(c, cfg, target)
						cancel()
						result := "correcto"
						if e != nil {
							result = "falló"
						}
						_ = b.Store.Record(0, "respaldo-programado", result)
						if st.OwnerID != 0 && b.Store.Authorized(st.OwnerID, st.OwnerID) {
							_, _ = b.API.Send(ctx, st.OwnerID, "Respaldo programado de "+target.Name+": "+result, nav("backups"))
						}
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (b *Bot) checkAlerts(ctx context.Context, s Snapshot) {
	st := b.Store.View()
	if !st.Alerts.Enabled || !b.Store.Authorized(st.OwnerID, st.OwnerID) {
		return
	}
	values := map[string]struct{ value, limit float64 }{"CPU": {s.CPU, st.Alerts.CPU}, "RAM": {Percent(s.RAMUsed, s.RAMTotal), st.Alerts.RAM}}
	if disks, e := Disks(); e == nil {
		for _, d := range disks {
			values["Disco "+d.Path] = struct{ value, limit float64 }{Percent(d.Used, d.Total), st.Alerts.Disk}
		}
	}
	now := time.Now()
	for name, v := range values {
		old := st.AlertEvents[name]
		active := v.value >= v.limit
		recover := old.Active && v.value < v.limit-5
		if (!active || old.Active && now.Sub(old.LastSent) < 30*time.Minute) && !recover {
			continue
		}
		label := "Alerta"
		if recover {
			label = "Recuperado"
		}
		_, e := b.API.Send(ctx, st.OwnerID, fmt.Sprintf("%s · %s\nUso: %.1f %% · Límite: %.0f %%\n%s UTC", label, name, v.value, v.limit, now.UTC().Format("15:04")), nav("alerts"))
		if e == nil {
			_ = b.Store.Change(func(s *State) error { s.AlertEvents[name] = AlertEvent{active, now}; return nil })
		}
	}
}
func (b *Bot) networkClient() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 50 * time.Second}
}
func (b *Bot) logout(ctx context.Context, id, chat int64) error {
	b.clear(id)
	if e := b.Store.Logout(id); e != nil {
		return e
	}
	return b.show(ctx, chat, "Sesión cerrada. Las alertas se reanudarán al iniciar sesión.", loginMenu())
}
func splitRoute(data string) []string { return strings.Split(data, ":") }

func (b *Bot) Handle(ctx context.Context, u Update) error {
	err := b.handle(ctx, u)
	if err == nil {
		return nil
	}
	var id, chat int64
	if u.Callback != nil && u.Callback.Message != nil {
		id = u.Callback.From.ID
		chat = u.Callback.Message.Chat.ID
	}
	if u.Message != nil && u.Message.From != nil {
		id = u.Message.From.ID
		chat = u.Message.Chat.ID
	}
	if b.Store.Authorized(id, chat) {
		return b.show(ctx, chat, "No se pudo completar la operación: "+err.Error(), nav("menu"))
	}
	return err
}

func (b *Bot) connect(ctx context.Context, network, address string) (net.Conn, error) {
	if b.Dial != nil {
		return b.Dial(ctx, network, address)
	}
	return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
}
func (b *Bot) requestReboot(ctx context.Context) error {
	if b.Reboot != nil {
		return b.Reboot(ctx)
	}
	return RequestReboot(ctx)
}

func (b *Bot) logFilter(id int64, index int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.filters[fmt.Sprintf("%d:%d", id, index)]
}
