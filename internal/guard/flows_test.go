package guard

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBackupButtonsAndPasswordChange(t *testing.T) {
	b, r := testBot(t)
	src := t.TempDir()
	if e := os.WriteFile(filepath.Join(src, "dato.txt"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	msg := func(text string) {
		t.Helper()
		if e := b.Handle(context.Background(), Update{Message: &Message{ID: 222, From: &User{ID: 42}, Chat: Chat{42, "private"}, Text: text}}); e != nil {
			t.Fatal(e)
		}
		b.wg.Wait()
	}
	click(t, b, "backupadd")
	msg(src)
	if len(b.cfg().BackupRoots) != 1 {
		t.Fatal("no agregó carpeta")
	}
	click(t, b, "backup:0")
	click(t, b, "confirm:"+confirmationToken(t, b))
	files, e := BackupList(b.cfg().DataDir)
	if e != nil || len(files) != 1 {
		t.Fatalf("respaldo desde botón: %v %v", files, e)
	}
	click(t, b, "backupfiles")
	click(t, b, "backupfile:0")
	click(t, b, "confirm:"+confirmationToken(t, b))
	for _, hours := range []int{6, 12, 24, 0} {
		click(t, b, "backupperiod")
		if b.Store.View().Alerts.BackupEveryHours != hours {
			t.Fatal("programación incorrecta")
		}
	}
	click(t, b, "backup-remove:0")
	click(t, b, "confirm:"+confirmationToken(t, b))
	if len(b.cfg().BackupRoots) != 0 {
		t.Fatal("no quitó carpeta")
	}
	if e = b.Store.Record(42, "búsqueda-fixture", "correcto"); e != nil {
		t.Fatal(e)
	}
	click(t, b, "logfilter:0")
	msg("búsqueda-fixture")
	if !strings.Contains(r.last(), "búsqueda-fixture") {
		t.Fatal("no filtró")
	}
	click(t, b, "log-clear:0")
	if b.logFilter(42, 0) != "" {
		t.Fatal("no quitó filtro")
	}
	click(t, b, "passwordchange")
	msg("otra-clave-nueva-2026")
	if b.Store.Authorized(42, 42) {
		t.Fatal("cambio de contraseña no cerró sesión")
	}
	c, e := LoadConfig(b.ConfigPath)
	if e != nil || !c.Password.Verify("otra-clave-nueva-2026") {
		t.Fatal("contraseña no persistió")
	}
}
func TestProcessSelectionAndProtectedPID(t *testing.T) {
	b, r := testBot(t)
	p, e := ReadProcess(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	click(t, b, "process:"+strconv.Itoa(p.PID)+":"+p.Start)
	click(t, b, "kill:"+strconv.Itoa(p.PID)+":"+p.Start)
	if !strings.Contains(r.last(), "protegido") {
		t.Fatal("proceso propio no protegido")
	}
	click(t, b, "process:"+strconv.Itoa(p.PID)+":0")
	if !strings.Contains(r.last(), "cambió") {
		t.Fatal("selección obsoleta aceptada")
	}
}
func TestLayoutLimitsAndLifecycleSpec(t *testing.T) {
	c := fixtureConfig(t)
	spec := AppSpec(c, "/etc/tavito-guard/config.json", "0.1.0")
	if e := spec.Validate(); e != nil {
		t.Fatal(e)
	}
	if !spec.Service.Enabled || spec.Service.Name != "tavito-guard" {
		t.Fatal("servicio de instalación incorrecto")
	}
	if len(spec.Upgrade.BackupTargets) != 2 {
		t.Fatal("respaldo de actualización incorrecto")
	}
	s := Snapshot{Time: time.Now(), Host: strings.Repeat("host-largo", 40), CPU: 99, RAMUsed: 1024, RAMTotal: 2048}
	for i := 0; i < 8; i++ {
		busy := 50.0
		s.GPUs = append(s.GPUs, GPU{Name: strings.Repeat("GPU de fixture ", 30), Busy: &busy, Used: 1024, Total: 2048})
	}
	if _, e := ResourceCard(s); e != nil {
		t.Fatal(e)
	}
}
