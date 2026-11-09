package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const helpText = `Synthos Collective bot

/status – is the chain running, block height, validators signing
/validators – the 4 validators and whether each signed the latest block
/nodes – uptime nodes online, and any outside operators
/node <node id> – one node's uptime and reward status
/balance <0x address> – an address's SYN balance

You can also ask a question in plain words.
Never share your private key or seed phrase with anyone, including this bot.`

var addressRE = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
var nodeIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Bot answers commands. latest holds the most recent snapshot.
type Bot struct {
	Sources *Sources
	AI      *AI
	Cfg     WatchdogConfig

	mu     sync.Mutex
	latest *Snapshot
	limit  *rateLimiter
}

func (b *Bot) setLatest(s *Snapshot) {
	b.mu.Lock()
	b.latest = s
	b.mu.Unlock()
}

func (b *Bot) snapshot() *Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.latest
}

// online counts registry nodes heard from in the last 10 minutes.
func online(nodes []Node, now time.Time) []Node {
	var out []Node
	for _, n := range nodes {
		if now.Sub(n.LastSeen) <= 10*time.Minute {
			out = append(out, n)
		}
	}
	return out
}

func (b *Bot) statusText(now time.Time) string {
	s := b.snapshot()
	if s == nil {
		return "I haven't been able to read the chain yet. Try again in a minute."
	}
	signed := 0
	for _, v := range s.Validators {
		if s.Signed[v.Address] {
			signed++
		}
	}
	age := now.Sub(s.BlockTime).Round(time.Second)
	state := "running"
	if age > b.Cfg.HaltAfter {
		state = "STOPPED"
	}
	text := fmt.Sprintf("Chain: %s\nBlock: %d (%s ago)\nValidators signing the latest block: %d of %d",
		state, s.Height, age, signed, len(s.Validators))
	if s.NodesErr == nil {
		on := online(s.Nodes, now)
		outside := 0
		for _, n := range on {
			if b.Cfg.IsOutside(n) {
				outside++
			}
		}
		text += fmt.Sprintf("\nUptime nodes online: %d (outside operators: %d)", len(on), outside)
	}
	return text
}

func (b *Bot) validatorsText() string {
	s := b.snapshot()
	if s == nil {
		return "I haven't been able to read the chain yet. Try again in a minute."
	}
	lines := []string{fmt.Sprintf("Validators at block %d:", s.Height)}
	for _, v := range s.Validators {
		mark := "✅ signed"
		if !s.Signed[v.Address] {
			mark = "❌ did not sign"
		}
		lines = append(lines, fmt.Sprintf("• %s, %d voting power, %s", label(v), v.Power, mark))
	}
	return strings.Join(lines, "\n")
}

func (b *Bot) nodesText(now time.Time) string {
	s := b.snapshot()
	if s == nil {
		return "I haven't been able to read the chain yet. Try again in a minute."
	}
	if s.NodesErr != nil {
		return "The node registry isn't answering right now. Try again in a few minutes."
	}
	on := online(s.Nodes, now)
	var outside []string
	for _, n := range on {
		if b.Cfg.IsOutside(n) {
			outside = append(outside, fmt.Sprintf("• %s (%d heartbeats)", n.Name, n.ValidHeartbeats))
		}
	}
	sort.Strings(outside)
	text := fmt.Sprintf("Uptime nodes online: %d of %d registered.", len(on), len(s.Nodes))
	if len(outside) == 0 {
		return text + "\nNo outside operators online yet. Run one: www.ishamwilliamsblockchains.com/nodes (type the www)"
	}
	return text + "\nOutside operators online:\n" + strings.Join(outside, "\n")
}

func (b *Bot) nodeText(ctx context.Context, id string) string {
	if !nodeIDRE.MatchString(id) {
		return "Usage: /node <node id>, for example /node syn-02dbfc711cbc"
	}
	st, err := b.Sources.NodeStatus(ctx, id)
	if err != nil {
		return "I couldn't find that node. Check the ID (it's shown on your node page or in the node's console)."
	}
	health := "no recent heartbeat"
	if st.Health {
		health = "healthy"
	}
	return fmt.Sprintf("Node %s\nRole: %s\nStatus: %s, %s\nVerified heartbeats: %d\nVerified uptime: %.1f of 30 days\nLast heartbeat: %s\nReward status: %s",
		st.NodeID, st.Role, st.ProofStatus, health, st.ValidHeartbeats, st.VerifiedDays, st.LastHeartbeatAt,
		strings.ReplaceAll(st.RewardStatus, "_", " "))
}

func (b *Bot) balanceText(ctx context.Context, addr string) string {
	if !addressRE.MatchString(addr) {
		return "Usage: /balance <address>. A Synthos Collective address is 0x followed by 40 hex characters."
	}
	bal, err := b.Sources.Balance(ctx, addr)
	if err != nil {
		return "I couldn't read that balance right now. Try again in a minute."
	}
	return fmt.Sprintf("%s holds %d SYN.", addr, bal)
}

// liveSummary is the context the AI gets: facts only, no secrets.
func (b *Bot) liveSummary(now time.Time) string {
	return b.statusText(now) + "\n" + b.validatorsText()
}

// Handle answers one message. It returns "" when there's nothing to say.
func (b *Bot) Handle(ctx context.Context, chatID int64, text string, now time.Time) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if !b.limit.allow(chatID, "any", now) {
		return ""
	}
	fields := strings.Fields(text)
	cmd := strings.ToLower(fields[0])
	if i := strings.Index(cmd, "@"); i > 0 { // /status@SynthosBot in groups
		cmd = cmd[:i]
	}
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch cmd {
	case "/start", "/help":
		return helpText
	case "/status":
		return b.statusText(now)
	case "/validators":
		return b.validatorsText()
	case "/nodes":
		return b.nodesText(now)
	case "/node":
		return b.nodeText(ctx, arg)
	case "/balance":
		return b.balanceText(ctx, arg)
	}
	if strings.HasPrefix(cmd, "/") {
		return "I don't know that command.\n\n" + helpText
	}
	if !b.AI.Enabled() {
		return "I can answer the commands below. Plain-language questions aren't switched on yet.\n\n" + helpText
	}
	if !b.limit.allow(chatID, "ai", now) {
		return "That's a lot of questions at once. Try again in a little while, or use the commands in /help."
	}
	ans, err := b.AI.Answer(ctx, text, b.liveSummary(now))
	if err != nil {
		return "I couldn't answer that right now. The commands in /help still work."
	}
	return ans
}

// rateLimiter keeps one chat from flooding the bot or running up AI costs.
type rateLimiter struct {
	mu        sync.Mutex
	last      map[int64]time.Time
	ai        map[int64][]time.Time
	aiAll     []time.Time
	MinGap    time.Duration // between any two messages from one chat
	AIPerChat int           // AI answers per chat per hour
	AIPerHour int           // AI answers per hour for everyone together
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{last: map[int64]time.Time{}, ai: map[int64][]time.Time{},
		MinGap: 2 * time.Second, AIPerChat: 10, AIPerHour: 100}
}

func within(ts []time.Time, now time.Time) []time.Time {
	out := ts[:0]
	for _, t := range ts {
		if now.Sub(t) < time.Hour {
			out = append(out, t)
		}
	}
	return out
}

func (r *rateLimiter) allow(chatID int64, kind string, now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if kind == "ai" {
		r.ai[chatID] = within(r.ai[chatID], now)
		r.aiAll = within(r.aiAll, now)
		if len(r.ai[chatID]) >= r.AIPerChat || len(r.aiAll) >= r.AIPerHour {
			return false
		}
		r.ai[chatID] = append(r.ai[chatID], now)
		r.aiAll = append(r.aiAll, now)
		return true
	}
	if t, ok := r.last[chatID]; ok && now.Sub(t) < r.MinGap {
		return false
	}
	r.last[chatID] = now
	return true
}
