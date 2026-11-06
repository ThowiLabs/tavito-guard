package guard

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Target struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type Password struct {
	Salt string `json:"salt"`
	Hash string `json:"hash"`
}
type Config struct {
	Token           string   `json:"bot_token"`
	Username        string   `json:"username"`
	Password        Password `json:"password"`
	DataDir         string   `json:"data_dir"`
	LogFiles        []Target `json:"log_files"`
	BackupRoots     []Target `json:"backup_roots"`
	BackupMaxMB     int64    `json:"backup_max_mb"`
	BackupKeep      int      `json:"backup_keep"`
	DockerSocket    string   `json:"docker_socket"`
	UpdatePublicKey string   `json:"update_public_key"`
}

func DefaultConfig() Config {
	return Config{DataDir: "/var/lib/tavito-guard", BackupMaxMB: 256, BackupKeep: 3, DockerSocket: "/var/run/docker.sock"}
}
func NewPassword(password string) (Password, error) {
	if len(password) < 12 || len(password) > 256 {
		return Password{}, errors.New("la contraseña debe tener entre 12 y 256 bytes")
	}
	salt := make([]byte, 24)
	if _, err := rand.Read(salt); err != nil {
		return Password{}, err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	if err != nil {
		return Password{}, err
	}
	return Password{base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(key)}, nil
}
func (p Password) Verify(password string) bool {
	if len(password) > 256 {
		return false
	}
	salt, e1 := base64.StdEncoding.DecodeString(p.Salt)
	expected, e2 := base64.StdEncoding.DecodeString(p.Hash)
	if e1 != nil || e2 != nil || len(salt) != 24 || len(expected) != 32 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, 600000, 32)
	return err == nil && subtle.ConstantTimeCompare(key, expected) == 1
}
func (c Config) Validate() error {
	if c.Token == "" || strings.ContainsAny(c.Token, " \r\n\x00") {
		return errors.New("token inválido")
	}
	if len(c.Username) < 3 || len(c.Username) > 64 || strings.ContainsAny(c.Username, "\r\n\x00") {
		return errors.New("usuario inválido")
	}
	if !filepath.IsAbs(c.DataDir) || filepath.Clean(c.DataDir) == "/" {
		return errors.New("data_dir debe ser una ruta absoluta privada")
	}
	if c.BackupKeep < 1 || c.BackupKeep > 30 || c.BackupMaxMB < 1 || c.BackupMaxMB > 4096 {
		return errors.New("límites de respaldo inválidos")
	}
	for _, t := range append(append([]Target{}, c.LogFiles...), c.BackupRoots...) {
		if !filepath.IsAbs(t.Path) || t.Name == "" || len(t.Name) > 48 {
			return errors.New("ruta o alias inválido")
		}
	}
	if c.DockerSocket != "" && !filepath.IsAbs(c.DockerSocket) {
		return errors.New("socket Docker debe ser absoluto")
	}
	if s, e := base64.StdEncoding.DecodeString(c.Password.Salt); e != nil || len(s) != 24 {
		return errors.New("salt de contraseña inválido")
	}
	if h, e := base64.StdEncoding.DecodeString(c.Password.Hash); e != nil || len(h) != 32 {
		return errors.New("hash de contraseña inválido")
	}
	return nil
}
func LoadConfig(path string) (Config, error) {
	var c Config
	fi, err := os.Lstat(path)
	if err != nil {
		return c, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
		return c, errors.New("la configuración debe ser un archivo regular con permisos 0600")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if len(data) > 1<<20 {
		return c, errors.New("configuración demasiado grande")
	}
	if err = json.Unmarshal(data, &c); err != nil {
		return c, errors.New("JSON de configuración inválido")
	}
	return c, c.Validate()
}
func SaveConfig(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	return AtomicJSON(path, c)
}
func AtomicJSON(path string, v any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
		return errors.New("directorio de estado inválido")
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tavito-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		return fmt.Errorf("guardar datos privados: %w", err)
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
