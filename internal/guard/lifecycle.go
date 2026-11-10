package guard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YahirHub/svcforge"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func Setup(ctx context.Context, path, dataDir string, in io.Reader, out io.Writer, validate func(context.Context, string) error) (Config, error) {
	c := DefaultConfig()
	if dataDir != "" {
		c.DataDir = dataDir
	}
	reader := bufio.NewReader(in)
	read := func(prompt string, secret bool) (string, error) {
		fmt.Fprint(out, prompt)
		if f, ok := in.(*os.File); ok && secret && term.IsTerminal(int(f.Fd())) && reader.Buffered() == 0 {
			return readSecret(f, out)
		}
		s, e := reader.ReadString('\n')
		if e != nil && !(e == io.EOF && s != "") {
			return "", e
		}
		return strings.TrimSpace(s), nil
	}
	for attempts := 0; attempts < 3; attempts++ {
		token, e := read("Token del bot: ", true)
		if e != nil {
			return c, e
		}
		if e = validate(ctx, token); e != nil {
			fmt.Fprintln(out, "Token no válido o Telegram no respondió. Reintenta.")
			continue
		}
		c.Token = token
		break
	}
	if c.Token == "" {
		return c, errors.New("no se pudo validar el token")
	}
	fmt.Fprintln(out, "Token validado.")
	user, e := read("Usuario de Tavito Guard: ", false)
	if e != nil {
		return c, e
	}
	c.Username = user
	pass, e := read("Contraseña de Tavito Guard (4 a 256 bytes): ", true)
	if e != nil {
		return c, e
	}
	repeat, e := read("Repite la contraseña: ", true)
	if e != nil {
		return c, e
	}
	if pass != repeat {
		return c, errors.New("las contraseñas no coinciden")
	}
	c.Password, e = NewPassword(pass)
	pass = ""
	repeat = ""
	if e != nil {
		return c, e
	}
	if e = SaveConfig(path, c); e != nil {
		return c, e
	}
	if _, e = OpenStore(c.DataDir); e != nil {
		return c, e
	}
	fmt.Fprintln(out, "Configuración guardada. El primer inicio de sesión correcto vinculará tu ID de Telegram.")
	return c, nil
}
func AppSpec(c Config, path, version string) svcforge.App {
	path, _ = filepath.Abs(path)
	return svcforge.App{
		ID: "tavito-guard", Name: "tavito-guard", DisplayName: "Tavito Guard", Description: "Administración Linux desde Telegram", Version: version,
		Executable: svcforge.Executable{InstallPath: "/usr/local/bin/tavito-guard", Mode: 0755},
		Service:    svcforge.Service{Enabled: true, Name: "tavito-guard", Description: "Tavito Guard", Arguments: []string{"--serve", "--config", path}, WorkingDirectory: c.DataDir, AutoStart: true, Restart: svcforge.RestartNever, DependOnNetwork: true, LogPath: filepath.Join(c.DataDir, "runtime.log")},
		Upgrade: svcforge.UpgradePolicy{BackupTargets: []svcforge.BackupTarget{{Path: path}, {Path: filepath.Join(c.DataDir, "state.json"), Optional: true}}, KeepBackups: 2, HealthCheck: svcforge.HealthCheck{Timeout: 40 * time.Second, Check: func(ctx context.Context) error {
			var h struct {
				Version string
				PID     int
				Time    time.Time
			}
			data, e := os.ReadFile(filepath.Join(c.DataDir, "heartbeat.json"))
			if e != nil {
				return e
			}
			if e = json.Unmarshal(data, &h); e != nil {
				return e
			}
			if h.Version != version || h.PID <= 0 || time.Since(h.Time) > 90*time.Second || h.Time.After(time.Now().Add(time.Second)) {
				return errors.New("Tavito Guard no ha confirmado el arranque")
			}
			if e = unix.Kill(h.PID, 0); e != nil {
				return e
			}
			return nil
		}}},
	}
}
func Lifecycle(ctx context.Context, c Config, path, version string, op svcforge.Operation) (svcforge.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	manager, err := svcforge.DetectServiceManager()
	if err != nil {
		return svcforge.Result{}, err
	}
	return (svcforge.Runner{App: appForServiceManager(c, path, version, manager)}).Execute(ctx, op)
}
func LockRuntime(dir string) (func(), error) {
	f, e := os.OpenFile(filepath.Join(dir, "runtime.lock"), os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return nil, e
	}
	if e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); e != nil {
		f.Close()
		return nil, errors.New("otra instancia está usando este directorio de datos")
	}
	return func() { _ = unix.Flock(int(f.Fd()), unix.LOCK_UN); _ = f.Close() }, nil
}
func RequestReboot(ctx context.Context) error {
	for _, path := range []string{"/sbin/reboot", "/usr/sbin/reboot"} {
		if fi, e := os.Stat(path); e == nil && !fi.IsDir() {
			if e = exec.CommandContext(ctx, path).Run(); e != nil {
				return errors.New("el sistema rechazó el reinicio; revisa los permisos locales")
			}
			return nil
		}
	}
	return errors.New("no se encontró el comando nativo reboot")
}

func appForServiceManager(c Config, path, version string, manager svcforge.ServiceManager) svcforge.App {
	app := AppSpec(c, path, version)
	if manager == svcforge.ServiceManagerSystemd {
		app.Service.Restart = svcforge.RestartOnFailure
	}
	return app
}

func readSecret(f *os.File, out io.Writer) (string, error) {
	fd := int(f.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, old)
	return readSecretBytes(f, out)
}

func readSecretBytes(r io.Reader, out io.Writer) (string, error) {
	var buf []byte
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if err != nil {
			return "", err
		}
		if n == 0 {
			continue
		}
		switch b := one[0]; {
		case b == '\r' || b == '\n':
			fmt.Fprint(out, "\r\n")
			return strings.TrimSpace(string(buf)), nil
		case b == 0x03:
			fmt.Fprint(out, "\r\n")
			return "", errors.New("interrumpido")
		case b == 0x7f || b == 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(out, "\b \b")
			}
		case b == 0x1b:
			for {
				m, err := r.Read(one)
				if err != nil {
					return "", err
				}
				if m == 0 {
					continue
				}
				c := one[0]
				if c == '[' || c == ';' || c >= '0' && c <= '9' {
					continue
				}
				break
			}
		default:
			buf = append(buf, b)
			fmt.Fprint(out, "*")
		}
	}
}
