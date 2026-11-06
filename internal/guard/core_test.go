package guard

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureConfig(t *testing.T) Config {
	t.Helper()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	p, e := NewPassword("una-clave-de-prueba-2026")
	if e != nil {
		t.Fatal(e)
	}
	c := DefaultConfig()
	c.Token = "123456789:token_de_prueba_no_real"
	c.Username = "administrador"
	c.Password = p
	c.DataDir = filepath.Join(dir, "data")
	return c
}
func TestLoginPersistsAndRevocation(t *testing.T) {
	c := fixtureConfig(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Login(42, 42, c.Username, "incorrecta", c, time.Now()); e == nil {
		t.Fatal("aceptó contraseña incorrecta")
	}
	if s.Authorized(42, 42) {
		t.Fatal("sesión no autorizada")
	}
	if e = s.Login(42, 42, c.Username, "una-clave-de-prueba-2026", c, time.Now()); e != nil {
		t.Fatal(e)
	}
	reload, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if !reload.Authorized(42, 42) || reload.Authorized(42, 43) {
		t.Fatal("sesión no ligada a chat privado")
	}
	if e = reload.Login(99, 99, c.Username, "una-clave-de-prueba-2026", c, time.Now()); e == nil {
		t.Fatal("otro ID pudo tomar el servidor")
	}
	if e = reload.Logout(42); e != nil {
		t.Fatal(e)
	}
	reload, e = OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if reload.Authorized(42, 42) {
		t.Fatal("logout no persistió")
	}
}
func TestLoginRateLimitSurvivesRestart(t *testing.T) {
	c := fixtureConfig(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		if s.Login(42, 42, c.Username, "incorrecta", c, now) == nil {
			t.Fatal("aceptó acceso")
		}
	}
	s, e = OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Login(42, 42, c.Username, "una-clave-de-prueba-2026", c, now); e == nil {
		t.Fatal("bloqueo perdido")
	}
	if e = s.Login(42, 42, c.Username, "una-clave-de-prueba-2026", c, now.Add(16*time.Minute)); e != nil {
		t.Fatal(e)
	}
}
func TestStateChangeIsTransactional(t *testing.T) {
	c := fixtureConfig(t)
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	e = s.Change(func(st *State) error { st.OwnerID = 99; return errors.New("fallo") })
	if e == nil || s.View().OwnerID != 0 {
		t.Fatal("mutación parcial")
	}
	for i := 0; i < 270; i++ {
		if e = s.Record(1, "prueba", "correcto"); e != nil {
			t.Fatal(e)
		}
	}
	if len(s.View().Audit) != 256 {
		t.Fatal("bitácora sin límite")
	}
}
func TestConfigPermissionsAndCorruption(t *testing.T) {
	c := fixtureConfig(t)
	path := filepath.Join(filepath.Dir(c.DataDir), "config.json")
	if e := SaveConfig(path, c); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadConfig(path); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := LoadConfig(path); e == nil {
		t.Fatal("aceptó configuración pública")
	}
	s, e := OpenStore(c.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(s.path, []byte("datos dañados"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = OpenStore(c.DataDir); e == nil {
		t.Fatal("silenció estado dañado")
	}
	b, _ := os.ReadFile(s.path)
	if string(b) != "datos dañados" {
		t.Fatal("sobrescribió estado")
	}
}
func TestSetupValidatesTokenBeforeCredentials(t *testing.T) {
	dir := t.TempDir()
	var output bytes.Buffer
	calls := 0
	in := strings.NewReader("mal-token\n12345:token-prueba\nadministrador\nuna-clave-de-prueba-2026\nuna-clave-de-prueba-2026\n")
	_, e := Setup(context.Background(), filepath.Join(dir, "config.json"), filepath.Join(dir, "data"), in, &output, func(_ context.Context, token string) error {
		calls++
		if strings.Contains(output.String(), "Usuario de") {
			t.Fatal("pidió credenciales antes de validar")
		}
		if calls == 1 {
			return errors.New("token malo")
		}
		return nil
	})
	if e != nil || calls != 2 {
		t.Fatalf("setup: %v (%d)", e, calls)
	}
	if strings.Contains(output.String(), "una-clave-de-prueba") || strings.Contains(output.String(), "12345:token-prueba") {
		t.Fatal("filtró secretos")
	}
}
func TestParsersAndPNG(t *testing.T) {
	total, idle, e := parseCPU("cpu 10 2 8 70 5 3 1 1 10 2\n")
	if e != nil || total != 100 || idle != 75 {
		t.Fatalf("CPU: %d %d %v", total, idle, e)
	}
	used, ram, e := parseMemory("MemTotal: 1000 kB\nMemAvailable: 400 kB\n")
	if e != nil || used != 600*1024 || ram != 1000*1024 {
		t.Fatal("RAM incorrecta")
	}
	if _, _, e = parseCPU("texto"); e == nil {
		t.Fatal("CPU inválida aceptada")
	}
	fields := make([]string, 22)
	for i := range fields {
		fields[i] = "0"
	}
	fields[11] = "12"
	fields[12] = "3"
	fields[19] = "987654"
	fields[21] = "20"
	p, e := parseProcess(23, "23 (un proceso ) complicado) "+strings.Join(fields, " "), 4096)
	if e != nil || p.Start != "987654" || p.RSS != 81920 || p.Ticks != 15 {
		t.Fatalf("stat: %+v %v", p, e)
	}
	s, e := Collect(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if s.CPU < 0 || s.CPU > 100 || s.RAMUsed > s.RAMTotal {
		t.Fatal("métricas fuera de rango")
	}
	data, e := ResourceCard(s)
	if e != nil {
		t.Fatal(e)
	}
	img, e := png.Decode(bytes.NewReader(data))
	if e != nil || img.Bounds().Dx() != 1000 {
		t.Fatal("PNG inválido")
	}
	data, e = HistoryCard([]Snapshot{s}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = png.Decode(bytes.NewReader(data)); e != nil {
		t.Fatal(e)
	}
}
func TestInstanceLock(t *testing.T) {
	dir := t.TempDir()
	unlock, e := LockRuntime(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if second, e := LockRuntime(dir); e == nil {
		second()
		t.Fatal("permitió dos instancias")
	}
}
