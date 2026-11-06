package guard

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEmptyKeyboardUsesArray(t *testing.T) {
	for _, k := range []Keyboard{{}, KB(), KB(nil)} {
		data, e := json.Marshal(k)
		if e != nil {
			t.Fatal(e)
		}
		var wire map[string]json.RawMessage
		if e = json.Unmarshal(data, &wire); e != nil {
			t.Fatal(e)
		}
		if string(wire["inline_keyboard"]) != "[]" {
			t.Fatalf("teclado incompatible con Telegram: %s", data)
		}
	}
}
func TestResourcesSendsPhotoWithStrictTelegram(t *testing.T) {
	b, r := testBot(t)
	click(t, b, "resources")
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.files != 1 {
		t.Fatalf("se esperó una imagen de Recursos, se recibieron %d", r.files)
	}
	for _, s := range r.texts {
		if strings.Contains(s, "falló") {
			t.Fatal(s)
		}
	}
}
func TestTelegramErrorReasonIsUsefulAndRedacted(t *testing.T) {
	token := "123456789:abcdefghijklmnopqrstuvwxyz_token"
	description := "Bad Request: image rejected (provided " + token + ")"
	body, _ := json.Marshal(map[string]any{"ok": false, "error_code": 400, "description": description})
	api := NewTelegram(token)
	api.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})}
	e := api.Call(context.Background(), "sendPhoto", struct{}{}, nil)
	if e == nil || !strings.Contains(e.Error(), "image rejected") {
		t.Fatalf("no mostró el motivo: %v", e)
	}
	if strings.Contains(e.Error(), token) {
		t.Fatal("token en mensaje de error")
	}
}
