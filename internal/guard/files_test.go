package guard

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBackupLimitsSymlinksAndRetention(t *testing.T) {
	c := fixtureConfig(t)
	c.BackupKeep = 2
	c.BackupMaxMB = 1
	src := t.TempDir()
	outside := t.TempDir()
	if e := os.WriteFile(filepath.Join(src, "archivo.txt"), []byte("contenido esperado"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(outside, "secreto"), []byte("no incluir"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(filepath.Join(outside, "secreto"), filepath.Join(src, "enlace")); e != nil {
		t.Fatal(e)
	}
	target := Target{"datos", src}
	var last string
	for i := 0; i < 3; i++ {
		p, e := CreateBackup(context.Background(), c, target)
		if e != nil {
			t.Fatal(e)
		}
		last = p
	}
	names, e := BackupList(c.DataDir)
	if e != nil || len(names) != 2 {
		t.Fatalf("retención: %v %v", names, e)
	}
	f, e := os.Open(last)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	gz, e := gzip.NewReader(f)
	if e != nil {
		t.Fatal(e)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	h, e := tr.Next()
	if e != nil || h.Name != "archivo.txt" {
		t.Fatalf("archivo: %+v %v", h, e)
	}
	data, e := io.ReadAll(tr)
	if e != nil || string(data) != "contenido esperado" {
		t.Fatal("contenido incorrecto")
	}
	if _, e = tr.Next(); e != io.EOF {
		t.Fatal("incluyó enlace o entrada inesperada")
	}
	if e = os.WriteFile(filepath.Join(src, "grande"), make([]byte, 2<<20), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = CreateBackup(context.Background(), c, target); e == nil {
		t.Fatal("omitió límite")
	}
	files, e := os.ReadDir(filepath.Join(c.DataDir, "backups"))
	if e != nil {
		t.Fatal(e)
	}
	for _, fi := range files {
		if strings.HasSuffix(fi.Name(), ".tmp") {
			t.Fatal("temporal filtrado")
		}
	}
	if _, e = ReadBackup(c.DataDir, "../config.json"); e == nil {
		t.Fatal("permitió traversal")
	}
	if _, e = ValidateBackupRoot("/", c.DataDir); e == nil {
		t.Fatal("permitió raíz del sistema")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = CreateBackup(ctx, c, target); e == nil {
		t.Fatal("ignoró cancelación")
	}
}
func TestLogBoundAndRedaction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.log")
	secret := "123456789:abcdefghijklmnopqrstuvwxyz_token"
	text := strings.Repeat("línea antigua\n", 25000) + "password=clave123\nAuthorization: Bearer credencial-privada\nbot=" + secret + "\núltima línea\n"
	if e := os.WriteFile(path, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	lines, e := ReadLog(path, secret, "")
	if e != nil {
		t.Fatal(e)
	}
	s := strings.Join(lines, "\n")
	if len(s) > 300000 {
		t.Fatal("lectura sin límite")
	}
	if strings.Contains(s, secret) || strings.Contains(s, "clave123") || strings.Contains(s, "credencial-privada") {
		t.Fatal("credenciales visibles")
	}
	lines, e = ReadLog(path, secret, "ÚLTIMA")
	if e != nil || len(lines) != 1 {
		t.Fatal("filtro no coincide")
	}
}
