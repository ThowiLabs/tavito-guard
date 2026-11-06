package guard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func speedResponse(code int, typ string, data io.Reader) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{typ}}, Body: io.NopCloser(data)}
}
func speedText(code int, typ, body string) *http.Response {
	return speedResponse(code, typ, strings.NewReader(body))
}

var testSpeedCatalog = speedCatalog{"https://catalog.example/list", "ookla"}

func speedTestClient(t *testing.T, overrides func(*http.Request) (*http.Response, bool)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if overrides != nil {
			if resp, ok := overrides(r); ok {
				return resp, nil
			}
		}
		switch r.URL.Path {
		case "/list":
			return speedText(200, "application/json", `[{"url":"https://node.example/speedtest/upload.php","name":"Nodo","sponsor":"Prueba","https_functional":1}]`), nil
		case "/speedtest/latency.txt":
			return speedText(200, "text/plain", "test=test"), nil
		case "/speedtest/random4000x4000.jpg":
			return speedResponse(200, "image/jpeg", bytes.NewReader(make([]byte, speedChunk))), nil
		case "/speedtest/upload.php":
			if r.ContentLength <= 0 || r.ContentLength > speedChunk {
				t.Errorf("Content-Length=%d", r.ContentLength)
			}
			n, e := io.Copy(io.Discard, r.Body)
			r.Body.Close()
			if e != nil || n != r.ContentLength {
				t.Errorf("subida=%d: %v", n, e)
			}
			return speedText(200, "text/plain", "size=1048576"), nil
		}
		t.Errorf("destino inesperado: %s", r.URL)
		return speedText(404, "text/plain", ""), nil
	})}
}
func assertSpeedResult(t *testing.T, r SpeedResult) {
	t.Helper()
	if r.DownloadMbps <= 0 || r.UploadMbps <= 0 || math.IsInf(r.DownloadMbps, 0) || math.IsNaN(r.UploadMbps) || r.DownloadServer == "" || r.UploadServer == "" {
		t.Fatalf("resultado inválido: %+v", r)
	}
	if r.DownloadBytes != speedDownloadLimit || r.UploadBytes != speedUploadLimit {
		t.Fatalf("presupuesto global incorrecto: %+v", r)
	}
}
func TestSpeedNativeTransferLimits(t *testing.T) {
	var down, up atomic.Int64
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		if r.URL.Path == "/speedtest/random4000x4000.jpg" {
			down.Add(speedChunk)
		}
		if r.Method == "POST" {
			up.Add(r.ContentLength)
		}
		return nil, false
	})
	r, e := speedTestCatalogs(context.Background(), client, []speedCatalog{testSpeedCatalog})
	if e != nil {
		t.Fatal(e)
	}
	assertSpeedResult(t, r)
	if down.Load() != speedDownloadLimit || up.Load() != speedUploadLimit {
		t.Fatal("datos fuera de los límites", down.Load(), up.Load())
	}
}
func TestSpeedCatalog403UsesLibreSpeed(t *testing.T) {
	var attempted atomic.Bool
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		switch r.URL.Path {
		case "/blocked":
			attempted.Store(true)
			return speedText(403, "text/html", "Forbidden"), true
		case "/libre":
			return speedText(200, "application/json", `[{"name":"Alternativo","server":"https://node.example/speedtest/","pingURL":"latency.txt","dlURL":"random4000x4000.jpg","ulURL":"upload.php"}]`), true
		}
		return nil, false
	})
	r, e := speedTestCatalogs(context.Background(), client, []speedCatalog{{"https://catalog.example/blocked", "ookla"}, {"https://catalog.example/libre", "librespeed"}})
	if e != nil {
		t.Fatal(e)
	}
	assertSpeedResult(t, r)
	if !attempted.Load() || r.DownloadServer != "Alternativo" {
		t.Fatalf("no cambió de catálogo: %+v", r)
	}
}
func TestSpeedUpload403PreservesGlobalBudget(t *testing.T) {
	var failed, other atomic.Int64
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		switch r.URL.Path {
		case "/list":
			return speedText(200, "application/json", `[
{"url":"https://node.example/speedtest/upload.php","name":"Primario","sponsor":"A","https_functional":1},
{"url":"https://other.example/speedtest/upload.php","name":"Alternativo","sponsor":"B","https_functional":1}]`), true
		case "/speedtest/latency.txt":
			if r.URL.Host == "other.example" {
				time.Sleep(8 * time.Millisecond)
			}
		case "/speedtest/upload.php":
			if r.URL.Host == "node.example" {
				n, _ := io.Copy(io.Discard, r.Body)
				r.Body.Close()
				failed.Add(n)
				return speedText(403, "text/plain", "Forbidden"), true
			}
			other.Add(r.ContentLength)
		}
		return nil, false
	})
	r, e := speedTestCatalogs(context.Background(), client, []speedCatalog{testSpeedCatalog})
	if e != nil {
		t.Fatal(e)
	}
	assertSpeedResult(t, r)
	if failed.Load() != speedChunk || other.Load() != speedUploadLimit-speedChunk || !strings.HasPrefix(r.DownloadServer, "Primario") || !strings.HasPrefix(r.UploadServer, "Alternativo") {
		t.Fatalf("fallback/cuota: %+v, rechazo=%d alternativa=%d", r, failed.Load(), other.Load())
	}
}

type brokenSpeedReader struct{ done bool }

func (r *brokenSpeedReader) Read(p []byte) (int, error) {
	if r.done {
		return 0, io.ErrUnexpectedEOF
	}
	r.done = true
	n := copy(p, bytes.Repeat([]byte{1}, 16384))
	return n, nil
}
func TestSpeedPartialDownloadCountsBeforeFallback(t *testing.T) {
	var calls atomic.Int64
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		switch r.URL.Path {
		case "/list":
			return speedText(200, "application/json", `[{"url":"https://node.example/speedtest/upload.php","name":"Cortado"},{"url":"https://other.example/speedtest/upload.php","name":"Alternativo"}]`), true
		case "/speedtest/latency.txt":
			if r.URL.Host == "other.example" {
				time.Sleep(8 * time.Millisecond)
			}
		case "/speedtest/random4000x4000.jpg":
			if r.URL.Host == "node.example" {
				calls.Add(1)
				return speedResponse(200, "image/jpeg", &brokenSpeedReader{}), true
			}
		}
		return nil, false
	})
	r, e := speedTestCatalogs(context.Background(), client, []speedCatalog{testSpeedCatalog})
	if e != nil {
		t.Fatal(e)
	}
	assertSpeedResult(t, r)
	if calls.Load() != 1 || !strings.HasPrefix(r.DownloadServer, "Alternativo") {
		t.Fatalf("no cambió tras descarga cortada: %+v", r)
	}
}
func TestSpeedCancellationDuringTransfer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := speedTestClient(t, nil)
	next := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" {
			cancel()
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return next.RoundTrip(r)
	})
	start := time.Now()
	_, e := speedTestCatalogs(ctx, client, []speedCatalog{testSpeedCatalog})
	if !errors.Is(e, context.Canceled) || time.Since(start) > time.Second {
		t.Fatal("cancelación no propagada", e)
	}
}
func TestSpeedRejectsFakeDownloads(t *testing.T) {
	for _, body := range []struct{ typ, data string }{{"text/html", "<html>Blocked</html>"}, {"application/json", `{"error":"blocked"}`}, {"image/jpeg", ""}} {
		t.Run(body.typ+body.data, func(t *testing.T) {
			client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
				if strings.HasSuffix(r.URL.Path, ".jpg") {
					return speedText(200, body.typ, body.data), true
				}
				return nil, false
			})
			if _, e := speedTestCatalogs(context.Background(), client, []speedCatalog{testSpeedCatalog}); e == nil {
				t.Fatal("inventó velocidad a partir de datos inválidos")
			}
		})
	}
}
func TestSpeedCatalogValidation(t *testing.T) {
	for _, data := range []string{`not-json`, `[]`, `[{"url":"http://node.example/speedtest/upload.php"}]`, `[{"url":"https://127.0.0.1/upload.php"}]`, strings.Repeat(" ", (1<<20)+1)} {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return speedText(200, "application/json", data), nil })}
		if _, e := fetchSpeedServers(context.Background(), client, testSpeedCatalog); e == nil {
			t.Fatal("aceptó catálogo inválido")
		}
	}
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		return speedText(200, "application/json", `[{"url":"http://node.example:8080/speedtest/upload.php","https_functional":1,"name":"HTTPS"}]`), true
	})
	nodes, e := fetchSpeedServers(context.Background(), client, testSpeedCatalog)
	if e != nil || len(nodes) != 1 || nodes[0].Upload != "https://node.example:8080/speedtest/upload.php" {
		t.Fatal("no seleccionó HTTPS", nodes, e)
	}
}
func TestSpeedDestinationSafety(t *testing.T) {
	for _, raw := range []string{"http://example.org/data", "https://localhost/x", "https://127.0.0.1/x", "https://10.0.0.1/x", "https://192.168.1.1/x", "https://169.254.169.254/x", "https://[::1]/x", "https://[fc00::1]/x", "https://user:pass@example.org/x", "https://example.org:99999/x"} {
		if validSpeedURL(raw) == nil {
			t.Errorf("aceptó %s", raw)
		}
	}
	c := speedClient(nil)
	req, _ := http.NewRequest("GET", "https://127.0.0.1/private", nil)
	if c.CheckRedirect(req, []*http.Request{{}}) == nil {
		t.Fatal("permitió redirección privada")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, e := speedDial(ctx, "tcp", "127.0.0.1:1"); e == nil {
		conn.Close()
		t.Fatal("marcó dirección privada")
	}
	if !publicSpeedIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("rechazó IP pública")
	}
}
func TestSpeedEarlyUploadResponseCannotFakeSuccess(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		// A transport may read/close the body after RoundTrip returns.
		go func() { io.Copy(io.Discard, r.Body); r.Body.Close() }()
		return speedText(403, "text/plain", "Forbidden"), nil
	})}
	budget := &speedBudget{left: speedUploadLimit}
	_, _, e := measureSpeed(context.Background(), speedClient(client), speedServer{Upload: "https://node.example/upload"}, true, budget)
	if e == nil || budget.used() < 0 || budget.used() > speedChunk {
		t.Fatal("rechazo temprano o cuota incorrecta", e, budget.used())
	}
}
func TestSpeedRankingCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var once sync.Once
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		once.Do(cancel)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	nodes := make([]speedServer, 20)
	for i := range nodes {
		nodes[i].Ping = "https://node.example/ping"
	}
	if got := rankSpeedServers(ctx, client, nodes); len(got) != 0 {
		t.Fatal("sondeo cancelado exitoso")
	}
}

func TestSpeedRejectsHTMLUploadAcknowledgment(t *testing.T) {
	client := speedTestClient(t, func(r *http.Request) (*http.Response, bool) {
		if r.Method == "POST" {
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			return speedText(200, "text/html", "<!DOCTYPE html><html>Blocked</html>"), true
		}
		return nil, false
	})
	if _, e := speedTestCatalogs(context.Background(), client, []speedCatalog{testSpeedCatalog}); e == nil {
		t.Fatal("tomó una página de bloqueo como confirmación de subida")
	}
}
