package guard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}
type Message struct {
	ID   int64  `json:"message_id"`
	From *User  `json:"from"`
	Chat Chat   `json:"chat"`
	Text string `json:"text"`
}
type Callback struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}
type Update struct {
	ID       int64     `json:"update_id"`
	Message  *Message  `json:"message"`
	Callback *Callback `json:"callback_query"`
}
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type Keyboard struct {
	Rows [][]Button `json:"inline_keyboard"`
}

func (k Keyboard) MarshalJSON() ([]byte, error) {
	rows := make([][]Button, 0, len(k.Rows))
	for _, row := range k.Rows {
		if len(row) > 0 {
			rows = append(rows, row)
		}
	}
	return json.Marshal(struct {
		Rows [][]Button `json:"inline_keyboard"`
	}{Rows: rows})
}

func Btn(text, data string) Button { return Button{text, data} }
func KB(rows ...[]Button) Keyboard { return Keyboard{rows} }

type APIError struct {
	Code, RetryAfter int
	Method           string
	Description      string
}

func (e *APIError) Error() string {
	message := fmt.Sprintf("Telegram: %s falló (HTTP/API %d)", e.Method, e.Code)
	if e.Description != "" {
		message += ": " + e.Description
	}
	return message
}

type Telegram struct {
	Base string
	HTTP *http.Client
}

func NewTelegram(token string) *Telegram {
	return &Telegram{Base: "https://api.telegram.org/bot" + token, HTTP: &http.Client{Timeout: 45 * time.Second}}
}
func (t *Telegram) decode(method string, resp *http.Response, out any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return &APIError{Code: resp.StatusCode, Method: method}
	}
	var env struct {
		Description string          `json:"description"`
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Code        int             `json:"error_code"`
		Params      struct {
			Retry int `json:"retry_after"`
		} `json:"parameters"`
	}
	if e := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&env); e != nil {
		return errors.New("respuesta Telegram inválida")
	}
	if !env.OK {
		if strings.Contains(env.Description, "message is not modified") {
			return &APIError{Code: 304, Method: method}
		}
		if env.Code == 0 {
			env.Code = resp.StatusCode
		}
		description := Redact(env.Description, "")
		if strings.HasPrefix(t.Base, "https://api.telegram.org/bot") {
			description = Redact(description, strings.TrimPrefix(t.Base, "https://api.telegram.org/bot"))
		}
		return &APIError{Code: env.Code, RetryAfter: env.Params.Retry, Method: method, Description: trimText(description, 240)}
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}
func (t *Telegram) Call(ctx context.Context, method string, v, out any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", t.Base+"/"+method, bytes.NewReader(b))
	if e != nil {
		return errors.New("petición Telegram inválida")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := t.HTTP.Do(req)
	if e != nil {
		return errors.New("no se pudo conectar con Telegram")
	}
	return t.decode(method, resp, out)
}
func (t *Telegram) Me(ctx context.Context) (User, error) {
	var u User
	e := t.Call(ctx, "getMe", struct{}{}, &u)
	if e == nil && !u.IsBot {
		e = errors.New("el token no identifica un bot")
	}
	return u, e
}
func (t *Telegram) Updates(ctx context.Context, offset int64) ([]Update, error) {
	var u []Update
	e := t.Call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 30, "limit": 50, "allowed_updates": []string{"message", "callback_query"}}, &u)
	return u, e
}
func (t *Telegram) Send(ctx context.Context, chat int64, text string, k Keyboard) (Message, error) {
	var m Message
	payload := map[string]any{"chat_id": chat, "text": text}
	if len(k.Rows) > 0 {
		payload["reply_markup"] = k
	}
	e := t.Call(ctx, "sendMessage", payload, &m)
	return m, e
}
func (t *Telegram) Edit(ctx context.Context, chat, id int64, text string, k Keyboard) error {
	return t.Call(ctx, "editMessageText", map[string]any{"chat_id": chat, "message_id": id, "text": text, "reply_markup": k}, nil)
}
func (t *Telegram) Answer(ctx context.Context, id, text string) error {
	return t.Call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": id, "text": text}, nil)
}
func (t *Telegram) Delete(ctx context.Context, chat, id int64) error {
	return t.Call(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": id}, nil)
}
func (t *Telegram) File(ctx context.Context, chat int64, method, name, caption string, data []byte, k Keyboard) error {
	if len(data) > 48<<20 {
		return errors.New("archivo supera el límite local de envío de 48 MiB")
	}
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	field := "document"
	if method == "sendPhoto" {
		field = "photo"
	}
	f, e := w.CreateFormFile(field, name)
	if e != nil {
		return e
	}
	if _, e = f.Write(data); e != nil {
		return e
	}
	_ = w.WriteField("chat_id", strconv.FormatInt(chat, 10))
	_ = w.WriteField("caption", trimText(caption, 1000))
	if len(k.Rows) > 0 {
		kb, e := json.Marshal(k)
		if e != nil {
			return e
		}
		if e = w.WriteField("reply_markup", string(kb)); e != nil {
			return e
		}
	}
	if e = w.Close(); e != nil {
		return e
	}
	req, e := http.NewRequestWithContext(ctx, "POST", t.Base+"/"+method, &b)
	if e != nil {
		return errors.New("petición de archivo inválida")
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, e := t.HTTP.Do(req)
	if e != nil {
		return errors.New("falló el envío de archivo a Telegram")
	}
	return t.decode(method, resp, nil)
}
