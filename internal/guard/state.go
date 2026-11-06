package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type Session struct {
	UserID int64     `json:"user_id"`
	ChatID int64     `json:"chat_id"`
	Since  time.Time `json:"since"`
}
type Audit struct {
	Time   time.Time `json:"time"`
	UserID int64     `json:"user_id"`
	Action string    `json:"action"`
	Result string    `json:"result"`
}
type Failure struct {
	Count int       `json:"count"`
	Until time.Time `json:"until"`
}
type AlertSettings struct {
	Enabled          bool `json:"enabled"`
	CPU, RAM, Disk   float64
	BackupEveryHours int
}
type AlertEvent struct {
	Active   bool
	LastSent time.Time
}
type State struct {
	Schema      int                   `json:"schema"`
	OwnerID     int64                 `json:"owner_id"`
	Sessions    map[string]Session    `json:"sessions"`
	Failures    map[string]Failure    `json:"failures"`
	Audit       []Audit               `json:"audit"`
	History     []Snapshot            `json:"history"`
	Alerts      AlertSettings         `json:"alerts"`
	AlertEvents map[string]AlertEvent `json:"alert_events"`
	LastBackup  time.Time             `json:"last_backup"`
	Offset      int64                 `json:"telegram_offset"`
}
type Store struct {
	mu    sync.Mutex
	path  string
	state State
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 || fi.Mode().Perm()&0077 != 0 {
		return nil, errors.New("directorio de datos debe ser privado (0700)")
	}
	s := &Store{path: filepath.Join(dir, "state.json")}
	s.state = State{Schema: 1, Sessions: map[string]Session{}, Failures: map[string]Failure{}, AlertEvents: map[string]AlertEvent{}, Alerts: AlertSettings{true, 85, 90, 90, 0}}
	b, err := os.ReadFile(s.path)
	if err == nil {
		fi, e := os.Lstat(s.path)
		if e != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0077 != 0 {
			return nil, errors.New("estado debe ser regular y privado (0600)")
		}
		if len(b) > 8<<20 {
			return nil, errors.New("estado demasiado grande")
		}
		if err = json.Unmarshal(b, &s.state); err != nil {
			return nil, errors.New("estado inválido; conservar archivo y restaurar respaldo")
		}
		if s.state.Schema != 1 {
			return nil, errors.New("versión del estado no soportada")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.state.Sessions == nil {
		s.state.Sessions = map[string]Session{}
	}
	if s.state.Failures == nil {
		s.state.Failures = map[string]Failure{}
	}
	if s.state.AlertEvents == nil {
		s.state.AlertEvents = map[string]AlertEvent{}
	}
	return s, nil
}
func (s *Store) View() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.state)
	var c State
	_ = json.Unmarshal(b, &c)
	return c
}
func (s *Store) Change(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	var c State
	if err = json.Unmarshal(b, &c); err != nil {
		return err
	}
	if err = fn(&c); err != nil {
		return err
	}
	if len(c.Audit) > 256 {
		c.Audit = c.Audit[len(c.Audit)-256:]
	}
	if len(c.History) > 1440 {
		c.History = c.History[len(c.History)-1440:]
	}
	if err = AtomicJSON(s.path, c); err != nil {
		return err
	}
	s.state = c
	return nil
}
func key(id int64) string { return strconv.FormatInt(id, 10) }
func (s *Store) Authorized(id, chat int64) bool {
	st := s.View()
	v, ok := st.Sessions[key(id)]
	return ok && st.OwnerID == id && id == chat && v.ChatID == chat
}
func (s *Store) Login(id, chat int64, username, password string, c Config, now time.Time) error {
	if id <= 0 || id != chat {
		return errors.New("el acceso requiere un chat privado")
	}
	st := s.View()
	if st.OwnerID != 0 && st.OwnerID != id {
		return errors.New("esta cuenta no está autorizada")
	}
	if now.Before(st.Failures["_login"].Until) {
		return errors.New("demasiados intentos; espera 15 minutos")
	}
	good := c.Password.Verify(password) && username == c.Username
	err := s.Change(func(st *State) error {
		if st.OwnerID != 0 && st.OwnerID != id {
			return errors.New("esta cuenta no está autorizada")
		}
		f := st.Failures["_login"]
		if now.Before(f.Until) {
			return errors.New("demasiados intentos; espera 15 minutos")
		}
		if !good {
			f.Count++
			if f.Count >= 5 {
				f.Until = now.Add(15 * time.Minute)
				f.Count = 0
			}
			st.Failures["_login"] = f
			return nil
		}
		st.OwnerID = id
		delete(st.Failures, "_login")
		st.Sessions[key(id)] = Session{id, chat, now}
		st.Audit = append(st.Audit, Audit{now, id, "iniciar-sesion", "correcto"})
		return nil
	})
	if err != nil {
		return err
	}
	if !good {
		return errors.New("usuario o contraseña incorrectos")
	}
	return nil
}
func (s *Store) Logout(id int64) error {
	return s.Change(func(st *State) error {
		delete(st.Sessions, key(id))
		st.Audit = append(st.Audit, Audit{time.Now(), id, "cerrar-sesion", "correcto"})
		return nil
	})
}
func (s *Store) Record(id int64, action, result string) error {
	return s.Change(func(st *State) error { st.Audit = append(st.Audit, Audit{time.Now(), id, action, result}); return nil })
}
func (s *Store) AddSnapshot(v Snapshot) error {
	return s.Change(func(st *State) error {
		cutoff := v.Time.Add(-24 * time.Hour)
		i := 0
		for i < len(st.History) && st.History[i].Time.Before(cutoff) {
			i++
		}
		st.History = append(st.History[i:], v)
		return nil
	})
}
func (s *Store) AuditText() string {
	st := s.View()
	out := ""
	for _, a := range st.Audit {
		out += fmt.Sprintf("%s | %d | %s | %s\n", a.Time.Format(time.RFC3339), a.UserID, a.Action, a.Result)
	}
	return out
}
