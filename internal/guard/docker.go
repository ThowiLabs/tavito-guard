package guard

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type Docker struct {
	HTTP *http.Client
	Base string
}

func NewDocker(socket string) *Docker {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}}
	return &Docker{&http.Client{Transport: tr, Timeout: 20 * time.Second}, "http://docker"}
}
func (d *Docker) request(ctx context.Context, method, path string) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, method, d.Base+path, nil)
	if e != nil {
		return nil, e
	}
	resp, e := d.HTTP.Do(req)
	if e != nil {
		return nil, errors.New("Docker no está disponible o faltan permisos sobre su socket")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Docker devolvió HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
func (d *Docker) version(ctx context.Context) (string, error) {
	b, e := d.request(ctx, "GET", "/version")
	if e != nil {
		return "", e
	}
	var v struct {
		API string `json:"ApiVersion"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return "", e
	}
	if !regexp.MustCompile(`^1\.[0-9]{1,3}$`).MatchString(v.API) {
		return "", errors.New("versión API Docker inválida")
	}
	return "/v" + v.API, nil
}

type Container struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

func (d *Docker) List(ctx context.Context) ([]Container, error) {
	v, e := d.version(ctx)
	if e != nil {
		return nil, e
	}
	b, e := d.request(ctx, "GET", v+"/containers/json?all=true&limit=30")
	if e != nil {
		return nil, e
	}
	var out []Container
	e = json.Unmarshal(b, &out)
	if len(out) > 30 {
		out = out[:30]
	}
	return out, e
}

var dockerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (d *Docker) Action(ctx context.Context, id, action string) error {
	if !dockerID.MatchString(id) {
		return errors.New("ID de contenedor inválido")
	}
	if action != "start" && action != "stop" && action != "restart" {
		return errors.New("acción Docker no permitida")
	}
	v, e := d.version(ctx)
	if e != nil {
		return e
	}
	_, e = d.request(ctx, "POST", v+"/containers/"+id+"/"+action+"?t=10")
	return e
}

type DockerUsage struct {
	CPU         float64
	Used, Limit uint64
}

func (d *Docker) Usage(ctx context.Context, id string) (DockerUsage, error) {
	var out DockerUsage
	if !dockerID.MatchString(id) {
		return out, errors.New("ID Docker inválido")
	}
	v, e := d.version(ctx)
	if e != nil {
		return out, e
	}
	b, e := d.request(ctx, "GET", v+"/containers/"+id+"/stats?stream=false")
	if e != nil {
		return out, e
	}
	type cpustats struct {
		Usage struct {
			Total uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		System uint64 `json:"system_cpu_usage"`
		Cores  int    `json:"online_cpus"`
	}
	var s struct {
		CPU      cpustats `json:"cpu_stats"`
		Previous cpustats `json:"precpu_stats"`
		Memory   struct {
			Usage, Limit uint64
			Stats        map[string]uint64
		} `json:"memory_stats"`
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return out, e
	}
	if s.CPU.Usage.Total >= s.Previous.Usage.Total && s.CPU.System > s.Previous.System {
		out.CPU = 100 * float64(s.CPU.Usage.Total-s.Previous.Usage.Total) / float64(s.CPU.System-s.Previous.System) * float64(s.CPU.Cores)
	}
	out.Used = s.Memory.Usage
	cache := s.Memory.Stats["inactive_file"]
	if cache == 0 {
		cache = s.Memory.Stats["total_inactive_file"]
	}
	if cache < out.Used {
		out.Used -= cache
	}
	out.Limit = s.Memory.Limit
	return out, nil
}
func (d *Docker) Logs(ctx context.Context, id string) (string, error) {
	if !dockerID.MatchString(id) {
		return "", errors.New("ID Docker inválido")
	}
	v, e := d.version(ctx)
	if e != nil {
		return "", e
	}
	b, e := d.request(ctx, "GET", v+"/containers/"+id+"/logs?stdout=true&stderr=true&tail=60")
	if e != nil {
		return "", e
	}
	var out strings.Builder
	for len(b) >= 8 && (b[0] == 1 || b[0] == 2) && b[1] == 0 && b[2] == 0 && b[3] == 0 {
		n := int(binary.BigEndian.Uint32(b[4:8]))
		if n > len(b)-8 {
			break
		}
		out.Write(b[8 : 8+n])
		b = b[8+n:]
	}
	out.Write(b)
	return out.String(), nil
}
