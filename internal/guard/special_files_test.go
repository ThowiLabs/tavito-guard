package guard

import (
	"context"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSpecialFilesCannotBlockBackupOrLog(t *testing.T) {
	c := fixtureConfig(t)
	src := t.TempDir()
	pipe := filepath.Join(src, "fifo")
	if e := unix.Mkfifo(pipe, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(src, "dato"), []byte("contenido"), 0600); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := CreateBackup(context.Background(), c, Target{"fixture", src}); done <- e }()
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO bloqueó el respaldo")
	}
	go func() { _, e := ReadLog(pipe, "", ""); done <- e }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("aceptó FIFO como registro")
		}
	case <-time.After(time.Second):
		t.Fatal("FIFO bloqueó lectura")
	}
	link := filepath.Join(src, "enlace")
	if e := os.Symlink(filepath.Join(src, "dato"), link); e != nil {
		t.Fatal(e)
	}
	if _, e := ReadLog(link, "", ""); e == nil {
		t.Fatal("siguió enlace de registro")
	}
}
