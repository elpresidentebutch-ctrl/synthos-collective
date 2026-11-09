package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AI answers free-text questions with Claude, grounded in live chain facts.
// It is optional: with no API key or model it stays off.
type AI struct {
	Key   string
	Model string
	API   string // https://api.anthropic.com (overridable for tests)
	HTTP  *http.Client
}

func (a *AI) Enabled() bool { return a != nil && a.Key != "" && a.Model != "" }

const aiSystemPrompt = `You are the Synthos Collective network assistant in a public Telegram chat.
Synthos Collective is an independent Layer 1 blockchain written in Go, running CometBFT consensus.
Facts you can rely on:
- Native coin: SYN, fixed supply of 100 billion. SYN is not listed or traded on any exchange.
- Validators bond at least 10,000 SYN. Blocks come about every 12 seconds and are final when committed.
- Fees: a SYN send costs 0.1% of the amount, at least 10 SYN and at most 1,000 SYN; other transactions cost 10 SYN.
  Half of every fee is burned and half goes to the treasury; each month the treasury pays half of that month's fee income to validators by the blocks they signed.
- Uptime nodes (downloads at www.ishamwilliamsblockchains.com/nodes, type the www) prove they are online with signed heartbeats. They are not validators.
  Approved operators get a 10,000 SYN first reward after 30 days of verified uptime, then 5,000 SYN a month while online; it halves every 2 years from 2028. One paid node per household.
Rules:
- Answer briefly and plainly. Use the live data below when it is relevant.
- If you don't know, say so. Never invent numbers, addresses or features.
- Never predict SYN's price, never call SYN an investment, and never give financial or legal advice.
- Never ask for or accept private keys, seed phrases or passwords. Tell anyone who offers one to keep it secret.`

// Answer asks Claude one question with the live snapshot as context.
func (a *AI) Answer(ctx context.Context, question, live string) (string, error) {
	api := a.API
	if api == "" {
		api = "https://api.anthropic.com"
	}
	body, _ := json.Marshal(map[string]any{
		"model":      a.Model,
		"max_tokens": 600,
		"system":     aiSystemPrompt + "\n\nLive network data right now:\n" + live,
		"messages":   []map[string]string{{"role": "user", "content": question}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.Key)
	req.Header.Set("anthropic-version", "2023-06-01")
	client := a.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("AI request failed")
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI request failed: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("AI reply unreadable")
	}
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	if b.Len() == 0 {
		return "", fmt.Errorf("AI reply was empty")
	}
	return b.String(), nil
}
