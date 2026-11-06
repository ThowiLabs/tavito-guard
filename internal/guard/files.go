package guard

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var credentialPattern = regexp.MustCompile(`(?i)(password|passwd|token|authorization|secret|api[_-]?key)(["']?\s*[:=]\s*["']?)([^\r\n]*)`)
var tokenPattern = regexp.MustCompile(`[0-9]{6,}:[A-Za-z0-9_-]{20,}`)

func Redact(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "[oculto]")
	}
	s = tokenPattern.ReplaceAllString(s, "[token oculto]")
	s = credentialPattern.ReplaceAllString(s, "${1}${2}[oculto]")
	return s
}
func ReadLog(path, token, filter string) ([]string, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("el registro requiere ruta absoluta")
	}
	base := filepath.Dir(path)
	name := filepath.Base(path)
	if within(path, "/var/log") {
		base = "/var/log"
		name, _ = filepath.Rel(base, path)
	}
	root, e := os.OpenRoot(base)
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("el registro debe ser un archivo regular")
	}

	start := fi.Size() - 256*1024
	if start < 0 {
		start = 0
	}
	if _, e = f.Seek(start, io.SeekStart); e != nil {
		return nil, e
	}
	b, e := io.ReadAll(io.LimitReader(f, 256*1024))
	if e != nil {
		return nil, e
	}
	lines := strings.Split(Redact(string(b), token), "\n")
	if start > 0 && len(lines) > 0 {
		lines = lines[1:]
	}
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" && (filter == "" || strings.Contains(strings.ToLower(l), strings.ToLower(filter))) {
			r := []rune(l)
			if len(r) > 500 {
				l = string(r[:500]) + "…"
			}
			out = append(out, l)
		}
	}
	return out, nil
}

type FolderSize struct {
	Name  string
	Bytes uint64
}

func FolderSizes(ctx context.Context, path string) ([]FolderSize, bool, error) {
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, false, e
	}
	defer root.Close()
	totals := map[string]uint64{}
	count := 0
	partial := false
	deadline := time.Now().Add(5 * time.Second)
	e = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if count >= 20000 || time.Now().After(deadline) {
			partial = true
			return fs.SkipAll
		}
		count++
		if err != nil {
			partial = true
			return nil
		}
		if name == "." {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		first := strings.SplitN(name, "/", 2)[0]
		if path == "/" && (first == "proc" || first == "sys" || first == "dev" || first == "run") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			partial = true
			return nil
		}
		if info.Mode().IsRegular() {
			totals[first] += uint64(info.Size())
		}
		return nil
	})
	out := []FolderSize{}
	for n, b := range totals {
		out = append(out, FolderSize{n, b})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	if len(out) > 15 {
		out = out[:15]
	}
	return out, partial, e
}
func within(path, base string) bool {
	r, e := filepath.Rel(base, path)
	return e == nil && (r == "." || r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)))
}
func ValidateBackupRoot(path, dataDir string) (string, error) {
	path, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	path, e = filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	fi, e := os.Stat(path)
	if e != nil || !fi.IsDir() {
		return "", errors.New("selecciona una carpeta existente")
	}
	for _, bad := range []string{"/proc", "/sys", "/dev", "/run", dataDir, "/etc/tavito-guard"} {
		if within(path, bad) || within(bad, path) {
			return "", errors.New("la carpeta se superpone con rutas protegidas")
		}
	}
	return path, nil
}

type limitedWriter struct {
	w         io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, errors.New("el respaldo excede el tamaño permitido")
	}
	n, e := w.w.Write(p)
	w.remaining -= int64(n)
	return n, e
}

var backupMu sync.Mutex

func CreateBackup(ctx context.Context, c Config, target Target) (string, error) {
	backupMu.Lock()
	defer backupMu.Unlock()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	src, e := ValidateBackupRoot(target.Path, c.DataDir)
	if e != nil {
		return "", e
	}
	dir := filepath.Join(c.DataDir, "backups")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	fi, e := os.Lstat(dir)
	if e != nil || !fi.IsDir() || fi.Mode().Perm()&0077 != 0 {
		return "", errors.New("directorio de respaldos no es privado")
	}
	root, e := os.OpenRoot(src)
	if e != nil {
		return "", e
	}
	defer root.Close()
	f, e := os.CreateTemp(dir, ".backup-*.tmp")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return "", e
	}
	limit := c.BackupMaxMB << 20
	compressed := &limitedWriter{f, limit}
	gz := gzip.NewWriter(compressed)
	tw := tar.NewWriter(gz)
	remaining := limit
	count := 0
	e = fs.WalkDir(root.FS(), ".", func(name string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		count++
		if count > 20000 {
			return errors.New("respaldo supera 20000 entradas")
		}
		if name == "." || d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		file, err := root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if info.Size() > remaining {
			return errors.New("respaldo supera el límite de datos")
		}
		h, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		h.Name = name
		h.Uname = ""
		h.Gname = ""
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		n, err := io.CopyN(tw, file, info.Size())
		remaining -= n
		return err
	})
	e1 := tw.Close()
	e2 := gz.Close()
	if e == nil {
		e = e1
	}
	if e == nil {
		e = e2
	}
	if e == nil {
		e = f.Sync()
	}
	if e == nil {
		e = f.Close()
	}
	if e != nil {
		return "", e
	}
	name := "tg-" + time.Now().UTC().Format("20060102T150405.000000000") + ".tar.gz"
	dest := filepath.Join(dir, name)
	if e = os.Rename(f.Name(), dest); e != nil {
		return "", e
	}
	files, e := BackupList(c.DataDir)
	if e != nil {
		return dest, e
	}
	for i := c.BackupKeep; i < len(files); i++ {
		if e = os.Remove(filepath.Join(dir, files[i])); e != nil {
			return dest, e
		}
	}
	return dest, nil
}
func BackupList(dir string) ([]string, error) {
	list, e := os.ReadDir(filepath.Join(dir, "backups"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, f := range list {
		if f.Type().IsRegular() && strings.HasPrefix(f.Name(), "tg-") && strings.HasSuffix(f.Name(), ".tar.gz") {
			out = append(out, f.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(out)))
	return out, nil
}
func ReadBackup(dir, name string) ([]byte, error) {
	if filepath.Base(name) != name || !strings.HasPrefix(name, "tg-") || !strings.HasSuffix(name, ".tar.gz") {
		return nil, errors.New("respaldo inválido")
	}
	root, e := os.OpenRoot(filepath.Join(dir, "backups"))
	if e != nil {
		return nil, e
	}
	defer root.Close()
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	fi, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !fi.Mode().IsRegular() || fi.Size() > 48<<20 {
		return nil, fmt.Errorf("solo se pueden enviar respaldos regulares de hasta 48 MiB")
	}
	return io.ReadAll(io.LimitReader(f, 48<<20))
}
