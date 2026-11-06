package guard

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/elf"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/YahirHub/svcforge"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type UpdateManifest struct {
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature"`
}

func (m UpdateManifest) Payload() []byte {
	return []byte(strings.Join([]string{m.Version, m.OS, m.Arch, m.URL, m.SHA256}, "\n"))
}
func (m UpdateManifest) Verify(publicKey string) error {
	pub, e := base64.StdEncoding.DecodeString(publicKey)
	if e != nil || len(pub) != ed25519.PublicKeySize {
		return errors.New("clave pública de actualización inválida")
	}
	sig, e := base64.StdEncoding.DecodeString(m.Signature)
	if e != nil || !ed25519.Verify(pub, m.Payload(), sig) {
		return errors.New("firma de actualización inválida")
	}
	if m.OS != "linux" || (m.Arch != "amd64" && m.Arch != "arm64") {
		return errors.New("actualización para otra plataforma")
	}
	if _, e = svcforge.ParseVersion(m.Version); e != nil {
		return errors.New("versión de actualización inválida")
	}
	hash, e := hex.DecodeString(m.SHA256)
	if e != nil || len(hash) != 32 {
		return errors.New("SHA256 inválido")
	}
	return validReleaseURL(m.URL)
}
func validReleaseURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Host != "github.com" || !strings.HasPrefix(u.Path, "/ThowiLabs/tavito-guard/releases/download/") || u.Fragment != "" {
		return errors.New("la actualización debe proceder de releases de ThowiLabs/tavito-guard por HTTPS")
	}
	return nil
}
func releaseClient() *http.Client {
	return &http.Client{Timeout: 90 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errors.New("demasiadas redirecciones")
		}
		host := req.URL.Hostname()
		if req.URL.Scheme != "https" || (host != "github.com" && host != "release-assets.githubusercontent.com" && host != "objects.githubusercontent.com") {
			return errors.New("destino de redirección no permitido")
		}
		return nil
	}}
}
func FetchUpdate(ctx context.Context, c Config, current string) (UpdateManifest, error) {
	var m UpdateManifest
	link := "https://github.com/ThowiLabs/tavito-guard/releases/latest/download/manifest-linux-" + runtime.GOARCH + ".json"
	req, _ := http.NewRequestWithContext(ctx, "GET", link, nil)
	resp, e := releaseClient().Do(req)
	if e != nil {
		return m, errors.New("no se pudo consultar la versión publicada")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return m, fmt.Errorf("no hay manifiesto de actualización disponible (HTTP %d)", resp.StatusCode)
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 32<<10)).Decode(&m); e != nil {
		return m, errors.New("manifiesto inválido")
	}
	if e = m.Verify(c.UpdatePublicKey); e != nil {
		return m, e
	}
	if m.Arch != runtime.GOARCH {
		return m, errors.New("actualización para otra arquitectura")
	}
	n, e := svcforge.CompareVersions(m.Version, current)
	if e != nil {
		return m, e
	}
	if n <= 0 {
		return m, errors.New("ya tienes esta versión o una más reciente")
	}
	return m, nil
}
func DownloadUpdate(ctx context.Context, c Config, m UpdateManifest) (string, error) {
	if m.Arch != runtime.GOARCH {
		return "", errors.New("actualización para otra arquitectura")
	}
	if e := m.Verify(c.UpdatePublicKey); e != nil {
		return "", e
	}
	dir := filepath.Join(c.DataDir, "staging")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	f, e := os.CreateTemp(dir, "update-*")
	if e != nil {
		return "", e
	}
	path := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	req, _ := http.NewRequestWithContext(ctx, "GET", m.URL, nil)
	resp, e := releaseClient().Do(req)
	if e != nil {
		return "", errors.New("no se pudo descargar la actualización")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", errors.New("descarga de actualización rechazada")
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, (64<<20)+1))
	if e != nil {
		return "", e
	}
	if n > 64<<20 || hex.EncodeToString(h.Sum(nil)) != m.SHA256 {
		return "", errors.New("tamaño o integridad de actualización incorrectos")
	}
	if e = f.Sync(); e != nil {
		return "", e
	}
	if e = f.Close(); e != nil {
		return "", e
	}
	binary, e := elf.Open(path)
	if e != nil {
		return "", errors.New("el artefacto no es un ejecutable ELF")
	}
	defer binary.Close()
	want := elf.EM_X86_64
	if runtime.GOARCH == "arm64" {
		want = elf.EM_AARCH64
	}
	if binary.Machine != want {
		return "", errors.New("arquitectura ELF incorrecta")
	}
	for _, p := range binary.Progs {
		if p.Type == elf.PT_INTERP {
			return "", errors.New("el artefacto requiere cargador dinámico")
		}
	}
	if e = os.Chmod(path, 0700); e != nil {
		return "", e
	}
	ok = true
	return path, nil
}
func StartUpdate(ctx context.Context, path, config string) error {
	args := []string{"--apply-update", "--config", config}
	if _, e := os.Stat("/run/systemd/system"); e == nil {
		cmd := exec.CommandContext(ctx, "systemd-run", append([]string{"--quiet", "--collect", "--unit=tavito-guard-update-" + fmt.Sprint(time.Now().Unix()), "--", path}, args...)...)
		if e = cmd.Run(); e != nil {
			return errors.New("no se pudo iniciar el actualizador fuera del servicio")
		}
		return nil
	}
	cmd := exec.Command(path, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, e := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	defer null.Close()
	cmd.Stdin = null
	cmd.Stdout = null
	cmd.Stderr = null
	if e = cmd.Start(); e != nil {
		return e
	}
	return cmd.Process.Release()
}
