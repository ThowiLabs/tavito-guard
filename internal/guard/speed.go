package guard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

type SpeedResult struct {
	LatencyMS, DownloadMbps, UploadMbps float64
	DownloadBytes, UploadBytes          int64
}

func SpeedTest(ctx context.Context, client *http.Client, base string) (SpeedResult, error) {
	var result SpeedResult
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	request := func(method, path string, body io.Reader, n int64) (int64, time.Duration, error) {
		req, e := http.NewRequestWithContext(ctx, method, base+path, body)
		if e != nil {
			return 0, 0, e
		}
		if n >= 0 {
			req.ContentLength = n
		}
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Accept-Encoding", "identity")
		if method == "POST" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
		start := time.Now()
		resp, e := client.Do(req)
		if e != nil {
			return 0, 0, errors.New("no se pudo completar la prueba de red")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return 0, 0, fmt.Errorf("el servidor de prueba devolvió HTTP %d", resp.StatusCode)
		}
		size, e := io.Copy(io.Discard, io.LimitReader(resp.Body, 17<<20))
		return size, time.Since(start), e
	}
	for i := 0; i < 3; i++ {
		_, d, e := request("GET", "/__down?bytes=0", nil, -1)
		if e != nil {
			return result, e
		}
		result.LatencyMS += float64(d.Microseconds()) / 3000
	}
	n, d, e := request("GET", "/__down?bytes=16777216", nil, -1)
	if e != nil {
		return result, e
	}
	if n != 16<<20 {
		return result, errors.New("la descarga de prueba llegó incompleta")
	}
	result.DownloadBytes = n
	result.DownloadMbps = float64(n) * 8 / d.Seconds() / 1e6
	body := bytes.NewReader(make([]byte, 4<<20))
	_, d, e = request("POST", "/__up", body, 4<<20)
	if e != nil {
		return result, e
	}
	result.UploadBytes = 4 << 20
	result.UploadMbps = float64(result.UploadBytes) * 8 / d.Seconds() / 1e6
	return result, nil
}
