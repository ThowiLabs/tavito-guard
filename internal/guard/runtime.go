package guard

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
)

func parseIP(s string) net.IP { return net.ParseIP(s) }

type RotatingLog struct {
	mu   sync.Mutex
	Path string
}

func (w *RotatingLog) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) > 16384 {
		p = p[:16384]
	}
	if fi, e := os.Lstat(w.Path); e == nil {
		if !fi.Mode().IsRegular() {
			return 0, errors.New("registro de ejecución inválido")
		}
		if fi.Size()+int64(len(p)) > 1<<20 {
			if e = os.Rename(w.Path, w.Path+".1"); e != nil {
				return 0, e
			}
		}
	} else if !os.IsNotExist(e) {
		return 0, e
	}
	f, e := os.OpenFile(w.Path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if e != nil {
		return 0, e
	}
	defer f.Close()
	return f.Write(p)
}
func NewRuntimeLog(dir string) *RotatingLog {
	return &RotatingLog{Path: filepath.Join(dir, "runtime.log")}
}
