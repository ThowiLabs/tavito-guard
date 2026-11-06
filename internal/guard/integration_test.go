package guard

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type apiRecorder struct {
	mu      sync.Mutex
	texts   []string
	methods []string
	files   int
	id      int64
}

func (r *apiRecorder) serve(w http.ResponseWriter, req *http.Request) {
	method := filepath.Base(req.URL.Path)
	var payload map[string]any
	if strings.HasPrefix(req.Header.Get("Content-Type"), "multipart/form-data") {
		if e := req.ParseMultipartForm(50 << 20); e != nil {
			http.Error(w, "multipart", 400)
			return
		}
		r.mu.Lock()
		r.files++
		r.mu.Unlock()
		if method == "sendPhoto" {
			f, _, e := req.FormFile("photo")
			if e != nil {
				http.Error(w, "photo", 400)
				return
			}
			defer f.Close()
			if _, e = png.Decode(f); e != nil {
				http.Error(w, "png", 400)
				return
			}
		}
		payload = map[string]any{"text": req.FormValue("caption")}
	} else {
		_ = json.NewDecoder(req.Body).Decode(&payload)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, method)
	if s, ok := payload["text"].(string); ok {
		r.texts = append(r.texts, s)
	}
	if raw, ok := payload["reply_markup"]; ok {
		data, _ := json.Marshal(raw)
		var k Keyboard
		_ = json.Unmarshal(data, &k)
		if k.Rows == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: inline_keyboard must be an Array"})
			return
		}
		for _, row := range k.Rows {
			if len(row) > 2 {
				http.Error(w, "too many buttons", 400)
				return
			}
			for _, b := range row {
				if len(b.Data) > 64 || b.Text == "" {
					http.Error(w, "invalid button", 400)
					return
				}
			}
		}
	}
	r.id++
	w.Header().Set("Content-Type", "application/json")
	result := any(map[string]any{"message_id": r.id, "chat": map[string]any{"id": 42, "type": "private"}})
	if method == "getMe" {
		result = User{ID: 1, IsBot: true}
	}
	if method == "answerCallbackQuery" || method == "deleteMessage" {
		result = true
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}
func (r *apiRecorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.texts) == 0 {
		return ""
	}
	return r.texts[len(r.texts)-1]
}
func testBot(t *testing.T) (*Bot, *apiRecorder) {
	t.Helper()
	c := fixtureConfig(t)
	path := filepath.Join(filepath.Dir(c.DataDir), "config.json")
	if e := SaveConfig(path, c); e != nil {
		t.Fatal(e)
	}
	st, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = st.Login(42, 42, c.Username, "una-clave-de-prueba-2026", c, time.Now()); e != nil {
		t.Fatal(e)
	}
	r := &apiRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(r.serve))
	t.Cleanup(srv.Close)
	b := NewBot(&Telegram{Base: srv.URL, HTTP: srv.Client()}, st, c, path, "0.1.0")
	t.Cleanup(func() { b.clear(42); b.wg.Wait() })
	return b, r
}
func click(t *testing.T, b *Bot, data string) {
	t.Helper()
	e := b.Handle(context.Background(), Update{Callback: &Callback{ID: "click", From: User{ID: 42}, Message: &Message{ID: 100, Text: "panel", Chat: Chat{42, "private"}}, Data: data}})
	if e != nil {
		t.Fatal(e)
	}
	b.wg.Wait()
}
func confirmationToken(t *testing.T, b *Bot) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for k := range b.confirms {
		return k
	}
	t.Fatal("sin confirmación")
	return ""
}
func fakeDocker(t *testing.T) string {
	t.Helper()
	dir, e := os.MkdirTemp("", "tgd-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "docker.sock")
	ln, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	id := strings.Repeat("a", 64)
	var mu sync.Mutex
	state := "running"
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/version":
			io.WriteString(w, `{"ApiVersion":"1.53"}`)
		case r.URL.Path == "/v1.53/containers/json":
			json.NewEncoder(w).Encode([]Container{{ID: id, Names: []string{"/fixture"}, Image: "fixture:test", State: state, Status: state}})
		case strings.HasSuffix(r.URL.Path, "/stats"):
			io.WriteString(w, `{"cpu_stats":{"cpu_usage":{"total_usage":30},"system_cpu_usage":200,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":10},"system_cpu_usage":100},"memory_stats":{"usage":1000,"limit":2000,"stats":{"inactive_file":100}}}`)
		case strings.HasSuffix(r.URL.Path, "/logs"):
			text := []byte("línea de prueba\n")
			header := make([]byte, 8)
			header[0] = 1
			binary.BigEndian.PutUint32(header[4:], uint32(len(text)))
			w.Write(header)
			w.Write(text)
		case r.Method == "POST":
			if strings.HasSuffix(r.URL.Path, "/stop") {
				state = "exited"
			} else {
				state = "running"
			}
			w.WriteHeader(204)
		default:
			w.WriteHeader(404)
		}
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return path
}
func TestTelegramMenuNavigationAndSession(t *testing.T) {
	b, r := testBot(t)
	cfg := b.cfg()
	cfg.DockerSocket = fakeDocker(t)
	if e := b.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	for _, data := range []string{"menu", "resources", "history", "disks", "processes", "network", "logs", "log:0:0", "logexport:0", "docker", "container:" + strings.Repeat("a", 12), "containerlogs:" + strings.Repeat("a", 12), "backups", "backupfiles", "alerts", "threshold:cpu", "threshold:ram", "threshold:disk", "alert-toggle", "alert-toggle", "access", "audit", "auditexport", "maintenance", "update", "speed"} {
		t.Run(data, func(t *testing.T) {
			click(t, b, data)
			if r.last() == "" {
				t.Fatal("pantalla vacía")
			}
		})
	}
	r.mu.Lock()
	files := r.files
	r.mu.Unlock()
	if files < 4 {
		t.Fatal("no envió imágenes y archivos")
	}
	for _, row := range MainMenu().Rows {
		for _, button := range row {
			if strings.Contains(button.Data, "service") || strings.Contains(button.Data, "application") {
				t.Fatal("módulo excluido presente")
			}
		}
	}
	click(t, b, "logout")
	if b.Store.Authorized(42, 42) {
		t.Fatal("Salir no revocó sesión")
	}
	before := len(b.Store.View().Audit)
	click(t, b, "reboot")
	if len(b.Store.View().Audit) != before || !strings.Contains(r.last(), "Inicia sesión") {
		t.Fatal("callback sin sesión ejecutó acción")
	}
}
func TestTelegramLoginAndExit(t *testing.T) {
	b, r := testBot(t)
	if e := b.Store.Logout(42); e != nil {
		t.Fatal(e)
	}
	click(t, b, "login")
	msg := func(text string) {
		t.Helper()
		if e := b.Handle(context.Background(), Update{Message: &Message{ID: 321, From: &User{ID: 42}, Chat: Chat{42, "private"}, Text: text}}); e != nil {
			t.Fatal(e)
		}
	}
	msg("administrador")
	msg("una-clave-de-prueba-2026")
	if !b.Store.Authorized(42, 42) {
		t.Fatal("login de Telegram falló")
	}
	r.mu.Lock()
	joined := strings.Join(r.texts, " ")
	r.mu.Unlock()
	if strings.Contains(joined, "una-clave-de-prueba-2026") {
		t.Fatal("contraseña en respuesta")
	}
	msg("/exit")
	if b.Store.Authorized(42, 42) {
		t.Fatal("/exit no revocó")
	}
}
func TestConfirmationIsBoundExpiringAndSingleUse(t *testing.T) {
	b, _ := testBot(t)
	click(t, b, "reboot")
	token := confirmationToken(t, b)
	if _, e := b.consume(99, 99, token); e == nil {
		t.Fatal("token aceptado por otro ID")
	}
	if _, e := b.consume(42, 42, token); e != nil {
		t.Fatal(e)
	}
	if _, e := b.consume(42, 42, token); e == nil {
		t.Fatal("reutilizó confirmación")
	}
	click(t, b, "reboot")
	token = confirmationToken(t, b)
	b.mu.Lock()
	c := b.confirms[token]
	c.Expires = time.Now().Add(-time.Second)
	b.confirms[token] = c
	b.mu.Unlock()
	if _, e := b.consume(42, 42, token); e == nil {
		t.Fatal("aceptó confirmación vencida")
	}
	calls := 0
	b.Reboot = func(context.Context) error { calls++; return nil }
	click(t, b, "reboot")
	token = confirmationToken(t, b)
	click(t, b, "confirm:"+token)
	click(t, b, "confirm:"+token)
	if calls != 1 {
		t.Fatalf("reinicio simulado: %d", calls)
	}
}
func TestDockerButtonsRequireConfirmation(t *testing.T) {
	b, _ := testBot(t)
	cfg := b.cfg()
	cfg.DockerSocket = fakeDocker(t)
	if e := b.saveConfig(cfg); e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"stop", "start", "restart"} {
		click(t, b, "containeraction:"+strings.Repeat("a", 12)+":"+action)
		token := confirmationToken(t, b)
		click(t, b, "confirm:"+token)
	}
	d := NewDocker(cfg.DockerSocket)
	u, e := d.Usage(context.Background(), strings.Repeat("a", 64))
	if e != nil || u.CPU != 40 || u.Used != 900 {
		t.Fatalf("stats: %+v %v", u, e)
	}
	if e = d.Action(context.Background(), "../escape", "stop"); e == nil {
		t.Fatal("aceptó ID inválido")
	}
	if e = d.Action(context.Background(), strings.Repeat("a", 64), "exec"); e == nil {
		t.Fatal("aceptó acción no autorizada")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestSpeedAndNetworkButtons(t *testing.T) {
	b, r := testBot(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Hostname() == "api.ipify.org" || r.URL.Path == "/ip" {
			io.WriteString(w, "203.0.113.2")
			return
		}
		if r.Method == "POST" {
			n, _ := io.Copy(io.Discard, r.Body)
			if n != 4<<20 {
				t.Error("tamaño de subida incorrecto")
			}
			io.WriteString(w, "{}")
			return
		}
		if r.URL.Query().Get("bytes") == "16777216" {
			io.CopyN(w, bytes.NewReader(make([]byte, 16<<20)), 16<<20)
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	b.HTTP = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		clone := req.Clone(req.Context())
		link := *req.URL
		clone.URL = &link
		if link.Host == "api.ipify.org" {
			clone.URL.Path = "/ip"
		}
		clone.URL.Host = u.Host
		clone.URL.Scheme = u.Scheme
		return http.DefaultTransport.RoundTrip(clone)
	}), Timeout: 5 * time.Second}
	b.Dial = func(context.Context, string, string) (net.Conn, error) {
		a, z := net.Pipe()
		go z.Close()
		return a, nil
	}
	for _, route := range []string{"speed-run", "publicip", "connectivity"} {
		click(t, b, route)
		if strings.Contains(r.last(), "falló") {
			t.Fatal(r.last())
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := SpeedTest(ctx, b.HTTP, "https://speed.cloudflare.com"); e == nil {
		t.Fatal("ignoró cancelación")
	}
}
func TestSignedUpdateRejectsTampering(t *testing.T) {
	pub, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	m := UpdateManifest{Version: "0.2.0", OS: "linux", Arch: "amd64", URL: "https://github.com/ThowiLabs/tavito-guard/releases/download/v0.2.0/tavito-guard-linux-amd64", SHA256: strings.Repeat("0", 64)}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(private, m.Payload()))
	key := base64.StdEncoding.EncodeToString(pub)
	if e = m.Verify(key); e != nil {
		t.Fatal(e)
	}
	m.Version = "0.3.0"
	if e = m.Verify(key); e == nil {
		t.Fatal("aceptó manifiesto alterado")
	}
	if e = validReleaseURL("http://127.0.0.1/bin"); e == nil {
		t.Fatal("aceptó origen no autorizado")
	}
}
func TestControlledProcessTermination(t *testing.T) {
	child := exec.Command(os.Args[0], "-test.run=TestProcessFixture")
	child.Env = append(os.Environ(), "TG_PROCESS_FIXTURE=1")
	if e := child.Start(); e != nil {
		t.Fatal(e)
	}
	defer child.Process.Kill()
	var p Process
	var e error
	for i := 0; i < 20; i++ {
		p, e = ReadProcess(child.Process.Pid)
		if e == nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e != nil {
		t.Fatal(e)
	}
	wrong := p
	wrong.Start = "0"
	if e = Terminate(wrong); e == nil {
		t.Fatal("aceptó identidad de proceso cambiada")
	}
	if e = Terminate(p); e != nil {
		t.Fatal(e)
	}
	if e = child.Wait(); e == nil {
		t.Fatal("fixture no recibió señal")
	}
}
func TestProcessFixture(t *testing.T) {
	if os.Getenv("TG_PROCESS_FIXTURE") != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}
func TestRedactionAndAPIFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		io.WriteString(w, `{"ok":false,"error_code":429,"parameters":{"retry_after":2}}`)
	}))
	defer srv.Close()
	api := &Telegram{Base: srv.URL, HTTP: srv.Client()}
	_, e := api.Me(context.Background())
	if e == nil || strings.Contains(fmt.Sprint(e), srv.URL) {
		t.Fatal("error inesperado")
	}
}
