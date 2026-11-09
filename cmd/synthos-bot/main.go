// Command synthos-bot is the Synthos Collective Telegram bot.
//
//   - Watchdog: messages the alert chat when the chain stops making blocks,
//     when a validator misses blocks, and when a new outside node proves
//     real uptime. One alert per problem, one when it recovers.
//   - Node helper: /node <id> shows a node's uptime and reward status.
//   - Chain Q&A: /status, /validators, /nodes, /balance, and (optionally)
//     plain-language questions answered by Claude using live chain data.
//
// It only reads the network. It holds no wallet and cannot send SYN.
//
// Secrets come from files or the environment, never from flags (other users
// can read command lines):
//
//	SYNTHOS_BOT_TELEGRAM_TOKEN_FILE  file holding the Telegram bot token (required)
//	SYNTHOS_BOT_ANTHROPIC_KEY_FILE   file holding an Anthropic API key (optional)
//	SYNTHOS_BOT_MODEL                Claude model ID for plain-language answers (optional)
//
// Example (on the Salt Lake City server):
//
//	synthos-bot -alert-chat 123456789 -home-ips 76.158.149.182
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func readSecretFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("secret file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("secret file %s can be read by other users; run: chmod 600 %s", path, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

type savedState struct {
	KnownOutside []string `json:"known_outside"`
}

func loadState(path string) savedState {
	var st savedState
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func saveState(path string, st savedState) {
	b, _ := json.MarshalIndent(st, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

func main() {
	rpc := flag.String("rpc", "http://127.0.0.1:26657", "CometBFT RPC of a synthos-comet node")
	chainAPI := flag.String("chain-api", "http://127.0.0.1:8080", "synthosd-compatible API of the same node (balances)")
	registry := flag.String("registry", "https://82-29-54-7.sslip.io", "node registry base URL")
	alertChat := flag.Int64("alert-chat", 0, "Telegram chat ID that receives watchdog alerts (required)")
	poll := flag.Duration("poll", 30*time.Second, "how often the watchdog checks the network")
	haltAfter := flag.Duration("halt-after", 3*time.Minute, "no new block for this long counts as stopped")
	missFor := flag.Duration("miss-for", 5*time.Minute, "alert when a validator hasn't signed for this long")
	minHB := flag.Int64("min-heartbeats", 20, "an outside node must send this many verified heartbeats before it's announced")
	homeIPs := flag.String("home-ips", "", "comma-separated IP prefixes of your own machines (not 'outside')")
	fleetClouds := flag.String("fleet-clouds", "fleet,hosteons,gitlab,lovable,render", "registry cloud values that are your own nodes")
	fleetPrefixes := flag.String("fleet-prefixes", "synthos-,syn-fleet-,syn-gitlab-cloud-,desktop-", "node-name prefixes that are your own nodes")
	stateFile := flag.String("state", "synthos-bot-state.json", "where announced outside nodes are remembered")
	flag.Parse()

	token, err := readSecretFile(os.Getenv("SYNTHOS_BOT_TELEGRAM_TOKEN_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	if token == "" {
		log.Fatal("set SYNTHOS_BOT_TELEGRAM_TOKEN_FILE to a file holding the Telegram bot token")
	}
	if *alertChat == 0 {
		log.Fatal("-alert-chat is required (your Telegram chat ID; see README)")
	}
	key, err := readSecretFile(os.Getenv("SYNTHOS_BOT_ANTHROPIC_KEY_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	ai := &AI{Key: key, Model: os.Getenv("SYNTHOS_BOT_MODEL")}

	cfg := WatchdogConfig{
		HaltAfter: *haltAfter, MissFor: *missFor, MinHeartbeats: *minHB,
		FleetClouds: splitList(*fleetClouds), FleetPrefixes: splitList(*fleetPrefixes), HomeIPs: splitList(*homeIPs),
	}
	src := &Sources{RPC: *rpc, ChainAPI: *chainAPI, Registry: *registry}
	tg := &Telegram{Token: token}
	bot := &Bot{Sources: src, AI: ai, Cfg: cfg, limit: newRateLimiter()}
	wd := NewWatchdog(cfg, loadState(*stateFile).KnownOutside)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("synthos-bot starting (AI answers: %v)", ai.Enabled())

	// First look: remember outside nodes that already exist, so a restart
	// doesn't re-announce them.
	if s, err := src.Snapshot(ctx); err == nil {
		bot.setLatest(s)
		for _, a := range wd.Observe(time.Now(), s) {
			if strings.HasPrefix(a, "🎉") {
				continue // existing nodes: remembered quietly on first start
			}
			_ = tg.Send(ctx, *alertChat, a)
		}
		saveState(*stateFile, savedState{KnownOutside: wd.KnownOutside()})
	}
	_ = tg.Send(ctx, *alertChat, "Synthos Collective watchdog is on. I'll message you here if something needs attention.")

	go watchLoop(ctx, src, bot, wd, tg, *alertChat, *poll, *stateFile)
	chatLoop(ctx, bot, tg)
}

func watchLoop(ctx context.Context, src *Sources, bot *Bot, wd *Watchdog, tg *Telegram, chat int64, poll time.Duration, stateFile string) {
	unreachableSince := time.Time{}
	unreachableAlerted := false
	t := time.NewTicker(poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		s, err := src.Snapshot(ctx)
		if err != nil {
			// The node the bot reads from may be down even if the chain isn't.
			if unreachableSince.IsZero() {
				unreachableSince = now
			}
			if !unreachableAlerted && now.Sub(unreachableSince) > 5*time.Minute {
				unreachableAlerted = true
				_ = tg.Send(ctx, chat, "⚠️ I can't read the chain from this server for 5 minutes. This server's synthos-comet node may be down: "+err.Error())
			}
			continue
		}
		if unreachableAlerted {
			_ = tg.Send(ctx, chat, "✅ I can read the chain again.")
		}
		unreachableSince, unreachableAlerted = time.Time{}, false
		bot.setLatest(s)
		alerts := wd.Observe(now, s)
		for _, a := range alerts {
			if err := tg.Send(ctx, chat, a); err != nil {
				log.Printf("alert not sent: %v", err)
			}
		}
		if len(alerts) > 0 {
			saveState(stateFile, savedState{KnownOutside: wd.KnownOutside()})
		}
	}
}

func chatLoop(ctx context.Context, bot *Bot, tg *Telegram) {
	var offset int64
	for ctx.Err() == nil {
		ups, err := tg.Updates(ctx, offset)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			log.Printf("%v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		for _, u := range ups {
			offset = u.UpdateID + 1
			if u.Message == nil {
				continue
			}
			reply := bot.Handle(ctx, u.Message.Chat.ID, u.Message.Text, time.Now())
			if reply == "" {
				continue
			}
			if err := tg.Send(ctx, u.Message.Chat.ID, reply); err != nil {
				log.Printf("reply not sent: %v", err)
			}
		}
	}
}
