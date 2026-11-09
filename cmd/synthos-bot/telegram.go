package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Telegram is a minimal Telegram Bot API client: long-poll for messages,
// send replies. The token is a secret: it is never logged.
type Telegram struct {
	Token string
	API   string // https://api.telegram.org (overridable for tests)
	HTTP  *http.Client
}

type tgMessage struct {
	MessageID int64 `json:"message_id"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Text string `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message"`
}

func (t *Telegram) call(ctx context.Context, method string, params url.Values, out any) error {
	api := t.API
	if api == "" {
		api = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/bot"+t.Token+"/"+method, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return fmt.Errorf("telegram %s: building request failed", method)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 70 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// The URL holds the token; never echo it.
		return fmt.Errorf("telegram %s: request failed", method)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var env struct {
		OK          bool            `json:"ok"`
		Result      json.RawMessage `json:"result"`
		Description string          `json:"description"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d", method, resp.StatusCode)
	}
	if !env.OK {
		return fmt.Errorf("telegram %s: %s", method, env.Description)
	}
	if out != nil {
		return json.Unmarshal(env.Result, out)
	}
	return nil
}

// Updates waits up to 50 s for new messages after offset.
func (t *Telegram) Updates(ctx context.Context, offset int64) ([]tgUpdate, error) {
	var ups []tgUpdate
	err := t.call(ctx, "getUpdates", url.Values{
		"offset":          {strconv.FormatInt(offset, 10)},
		"timeout":         {"50"},
		"allowed_updates": {`["message"]`},
	}, &ups)
	return ups, err
}

// Send posts a plain-text message to a chat.
func (t *Telegram) Send(ctx context.Context, chatID int64, text string) error {
	if len(text) > 4000 { // Telegram's limit is 4096 characters
		text = text[:4000] + "…"
	}
	return t.call(ctx, "sendMessage", url.Values{
		"chat_id":                  {strconv.FormatInt(chatID, 10)},
		"text":                     {text},
		"disable_web_page_preview": {"true"},
	}, nil)
}
