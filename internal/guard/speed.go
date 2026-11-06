package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const speedDownloadLimit int64 = 16 << 20
const speedUploadLimit int64 = 4 << 20
const speedChunk int64 = 1 << 20
const speedAgent = "TavitoGuard (+https://github.com/ThowiLabs/tavito-guard)"

type SpeedResult struct {
	LatencyMS, DownloadMbps, UploadMbps float64
	DownloadBytes, UploadBytes          int64
	DownloadServer, UploadServer        string
}
type speedCatalog struct{ URL, Kind string }
type speedServer struct {
	Name, Ping, Download, Upload string
	Latency                      float64
}

var speedCatalogs = []speedCatalog{
	{"https://www.speedtest.net/api/js/servers?engine=js&https_functional=true&limit=8", "ookla"},
	{"https://librespeed.org/backend-servers/servers.json", "librespeed"},
}

type speedBudget struct {
	mu          sync.Mutex
	left, spent int64
}

func (b *speedBudget) reserve() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := min(b.left, speedChunk)
	b.left -= n
	return n
}
func (b *speedBudget) release(reserved, spent int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.left += reserved - spent
	b.spent += spent
}
func (b *speedBudget) used() int64 { b.mu.Lock(); defer b.mu.Unlock(); return b.spent }

func speedServerName(name, fallback string) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = fallback
	}
	runes := []rune(name)
	if len(runes) > 120 {
		name = string(runes[:120])
	}
	return name
}
func publicSpeedIP(ip net.IP) bool {
	return ip != nil && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast()
}
func validSpeedURL(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Fragment != "" || strings.Contains(u.Hostname(), "%") {
		return errors.New("destino de velocidad HTTPS inválido")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return errors.New("se rechazó un destino local")
	}
	if ip := net.ParseIP(host); ip != nil && !publicSpeedIP(ip) {
		return errors.New("se rechazó una dirección privada")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return errors.New("puerto inválido")
		}
	}
	return nil
}
func speedDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, e := net.SplitHostPort(address)
	if e != nil {
		return nil, e
	}
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		return nil, e
	}
	var last error
	for _, ip := range ips {
		if !publicSpeedIP(ip.IP) {
			continue
		}
		conn, e := (&net.Dialer{Timeout: 4 * time.Second, KeepAlive: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if e == nil {
			return conn, nil
		}
		last = e
	}
	if last == nil {
		last = errors.New("el destino no tiene una dirección pública permitida")
	}
	return nil, last
}
func speedClient(client *http.Client) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	c := *client
	if c.Transport == nil {
		c.Transport = &http.Transport{DialContext: speedDial, ForceAttemptHTTP2: true, DisableCompression: true, MaxIdleConns: 16, MaxIdleConnsPerHost: 4, MaxConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 6 * time.Second}
	}
	if c.Timeout == 0 || c.Timeout > 15*time.Second {
		c.Timeout = 15 * time.Second
	}
	previous := c.CheckRedirect
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 {
			return errors.New("demasiadas redirecciones en speedtest")
		}
		if e := validSpeedURL(req.URL.String()); e != nil {
			return e
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	return &c
}
func speedRequest(ctx context.Context, client *http.Client, method, raw string, body io.Reader) (*http.Response, error) {
	if e := validSpeedURL(raw); e != nil {
		return nil, e
	}
	u, e := url.Parse(raw)
	if e != nil {
		return nil, e
	}
	q := u.Query()
	q.Set("tg", strconv.FormatInt(time.Now().UnixNano(), 36))
	u.RawQuery = q.Encode()
	req, e := http.NewRequestWithContext(ctx, method, u.String(), body)
	if e != nil {
		return nil, e
	}
	if counter, ok := body.(*speedReadCounter); ok {
		req.ContentLength = counter.size
	}
	req.Header.Set("User-Agent", speedAgent)
	req.Header.Set("Cache-Control", "no-store")
	req.Header.Set("Accept-Encoding", "identity")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("no se pudo conectar con " + u.Host)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s devolvió HTTP %d", u.Host, resp.StatusCode)
	}
	return resp, nil
}
func fetchSpeedServers(ctx context.Context, client *http.Client, catalog speedCatalog) ([]speedServer, error) {
	c, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	resp, e := speedRequest(c, client, "GET", catalog.URL, nil)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if e != nil {
		return nil, e
	}
	if len(data) > 1<<20 {
		return nil, errors.New("catálogo demasiado grande")
	}
	var out []speedServer
	if catalog.Kind == "ookla" {
		var rows []struct {
			URL, Name, Sponsor, Country string
			HTTPS                       int `json:"https_functional"`
		}
		if e = json.Unmarshal(data, &rows); e != nil {
			return nil, errors.New("catálogo de Speedtest.net inválido")
		}
		for _, r := range rows {
			raw := strings.ReplaceAll(r.URL, "\\/", "/")
			u, e := url.Parse(raw)
			if e != nil {
				continue
			}
			if r.HTTPS > 0 {
				u.Scheme = "https"
			}
			u.RawQuery = ""
			u.Fragment = ""
			if validSpeedURL(u.String()) != nil {
				continue
			}
			base := *u
			base.Path = path.Dir(u.Path)
			ping := base.JoinPath("latency.txt")
			down := base.JoinPath("random4000x4000.jpg")
			name := strings.Trim(r.Name+" · "+r.Sponsor, " ·")
			name = speedServerName(name, u.Host)
			out = append(out, speedServer{name, ping.String(), down.String(), u.String(), 0})
			if len(out) >= 8 {
				break
			}
		}
	} else {
		var rows []struct{ Name, Server, DLURL, ULURL, PingURL string }
		if e = json.Unmarshal(data, &rows); e != nil {
			return nil, errors.New("catálogo de LibreSpeed inválido")
		}
		for _, r := range rows {
			raw := strings.ReplaceAll(r.Server, "\\/", "/")
			if strings.HasPrefix(raw, "//") {
				raw = "https:" + raw
			}
			base, e := url.Parse(strings.TrimRight(raw, "/") + "/")
			if e != nil || validSpeedURL(base.String()) != nil {
				continue
			}
			join := func(s string) string {
				ref, e := url.Parse(s)
				if e != nil || ref.IsAbs() || ref.Host != "" {
					return ""
				}
				return base.ResolveReference(ref).String()
			}
			download := join(r.DLURL)
			if u, e := url.Parse(download); e == nil {
				q := u.Query()
				q.Set("ckSize", "1")
				u.RawQuery = q.Encode()
				download = u.String()
			}
			node := speedServer{speedServerName(r.Name, base.Host), join(r.PingURL), download, join(r.ULURL), 0}
			if r.PingURL == "" || r.DLURL == "" || r.ULURL == "" || validSpeedURL(node.Ping) != nil || validSpeedURL(node.Download) != nil || validSpeedURL(node.Upload) != nil {
				continue
			}
			out = append(out, node)
			if len(out) >= 8 {
				break
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("el catálogo no contiene servidores HTTPS utilizables")
	}
	return out, nil
}
func probeSpeedServer(ctx context.Context, client *http.Client, s speedServer) (speedServer, error) {
	c, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	best := time.Hour
	for i := 0; i < 2; i++ {
		start := time.Now()
		resp, e := speedRequest(c, client, "GET", s.Ping, nil)
		if e != nil {
			return s, e
		}
		b, e := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		if e != nil {
			return s, e
		}
		typ := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(typ, "text/html") || strings.Contains(strings.ToLower(string(b)), "<html") {
			return s, errors.New("el servidor devolvió una página en lugar de la prueba")
		}
		best = min(best, time.Since(start))
	}
	s.Latency = float64(best.Microseconds()) / 1000
	return s, nil
}
func rankSpeedServers(ctx context.Context, client *http.Client, nodes []speedServer) []speedServer {
	var wg sync.WaitGroup
	var mu sync.Mutex
	out := []speedServer{}
	slots := make(chan struct{}, 4)
loop:
	for _, node := range nodes {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func(s speedServer) {
			defer wg.Done()
			defer func() { <-slots }()
			s, e := probeSpeedServer(ctx, client, s)
			if e == nil {
				mu.Lock()
				out = append(out, s)
				mu.Unlock()
			}
		}(node)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return out[i].Latency < out[j].Latency })
	return out
}

// Close synchronizes with Transport.Read, including an early HTTP rejection.
// Count bytes consumed by the transport even when the server rejects a request.
type speedReadCounter struct {
	mu      sync.Mutex
	r       io.Reader
	size, n int64
	closed  bool
}

func (r *speedReadCounter) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.EOF
	}
	n, e := r.r.Read(p)
	r.n += int64(n)
	return n, e
}
func (r *speedReadCounter) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}
func (r *speedReadCounter) count() int64 { r.mu.Lock(); defer r.mu.Unlock(); return r.n }
func speedChunkRequest(ctx context.Context, client *http.Client, node speedServer, upload bool, take int64) (useful, spent int64, err error) {
	method, endpoint := "GET", node.Download
	var reader *speedReadCounter
	var body io.Reader
	if upload {
		method = "POST"
		endpoint = node.Upload
		reader = &speedReadCounter{r: bytes.NewReader(bytes.Repeat([]byte{0xa7}, int(take))), size: take}
		body = reader
		defer func() { reader.Close(); spent = reader.count() }()
	}
	resp, e := speedRequest(ctx, client, method, endpoint, body)
	if e != nil {
		return 0, 0, e
	}
	defer resp.Body.Close()
	if upload {
		ack, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
		if e != nil {
			return 0, 0, e
		}
		text := strings.ToLower(strings.TrimSpace(string(ack)))
		if len(ack) > 4096 || strings.Contains(text, "<html") || strings.Contains(text, "<!doctype") {
			return 0, 0, errors.New("el servidor no confirmó la subida")
		}
		reader.Close()
		n := reader.count()
		if n != take {
			return 0, 0, errors.New("el servidor respondió antes de recibir la subida completa")
		}
		return n, n, nil
	}
	typ := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(typ, "text/html") || strings.Contains(typ, "application/json") {
		return 0, 0, errors.New("el destino de descarga no devolvió datos de prueba")
	}
	// Read only the reserved payload; close responses that exceed this sample.
	n, e := io.CopyN(io.Discard, resp.Body, take)
	if n == 0 && e == io.EOF {
		return 0, 0, errors.New("descarga vacía")
	}
	if e == io.EOF && n > 0 {
		e = nil
	}
	if e != nil {
		return 0, n, e
	}
	if n < min(take, 64<<10) {
		return 0, n, errors.New("muestra de descarga insuficiente")
	}
	return n, n, nil
}
func measureSpeed(ctx context.Context, client *http.Client, node speedServer, upload bool, budget *speedBudget) (int64, time.Duration, error) {
	phase, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	start := time.Now()
	once := func() (int64, error) {
		take := budget.reserve()
		if take == 0 {
			return 0, io.EOF
		}
		n, spent, e := speedChunkRequest(phase, client, node, upload, take)
		budget.release(take, spent)
		return n, e
	}
	first, e := once()
	if e != nil {
		return 0, time.Since(start), e
	}
	var mu sync.Mutex
	total := first
	var failure error
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 64 && phase.Err() == nil; j++ {
				n, e := once()
				mu.Lock()
				total += n
				if e != nil && !errors.Is(e, io.EOF) && !errors.Is(e, context.DeadlineExceeded) && !errors.Is(e, context.Canceled) && failure == nil {
					failure = e
					cancel()
				}
				mu.Unlock()
				if e != nil {
					return
				}
			}
		}()
	}
	wg.Wait()
	if ctx.Err() != nil {
		return 0, time.Since(start), ctx.Err()
	}
	if failure != nil {
		return 0, time.Since(start), failure
	}
	if total == 0 {
		return 0, time.Since(start), errors.New("no se transfirieron datos")
	}
	return total, time.Since(start), nil
}
func SpeedTest(ctx context.Context, client *http.Client) (SpeedResult, error) {
	return speedTestCatalogs(ctx, client, speedCatalogs)
}
func speedTestCatalogs(ctx context.Context, client *http.Client, catalogs []speedCatalog) (SpeedResult, error) {
	var result SpeedResult
	c, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	client = speedClient(client)
	defer client.CloseIdleConnections()
	downBudget := &speedBudget{left: speedDownloadLimit}
	upBudget := &speedBudget{left: speedUploadLimit}
	var last error
	for _, catalog := range catalogs {
		if c.Err() != nil {
			return result, c.Err()
		}
		nodes, e := fetchSpeedServers(c, client, catalog)
		if e != nil {
			last = e
			continue
		}
		nodes = rankSpeedServers(c, client, nodes)
		if len(nodes) == 0 {
			last = errors.New("ningún servidor de ese catálogo respondió")
			continue
		}
		for _, node := range nodes {
			if result.DownloadServer == "" {
				n, d, e := measureSpeed(c, client, node, false, downBudget)
				if e != nil {
					last = e
					continue
				}
				result.DownloadMbps = float64(n) * 8 / d.Seconds() / 1e6
				result.DownloadServer = node.Name
				result.LatencyMS = node.Latency
			}
			if result.UploadServer == "" {
				n, d, e := measureSpeed(c, client, node, true, upBudget)
				if e != nil {
					last = e
					continue
				}
				result.UploadMbps = float64(n) * 8 / d.Seconds() / 1e6
				result.UploadServer = node.Name
			}
			result.DownloadBytes = downBudget.used()
			result.UploadBytes = upBudget.used()
			return result, nil
		}
	}
	if c.Err() != nil {
		return result, c.Err()
	}
	if last == nil {
		last = errors.New("ningún servidor disponible")
	}
	return result, fmt.Errorf("no se pudo completar la medición con los servidores disponibles: %w", last)
}
