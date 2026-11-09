package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func testCfg() WatchdogConfig {
	return WatchdogConfig{
		HaltAfter: 3 * time.Minute, MissFor: 5 * time.Minute, MinHeartbeats: 20,
		FleetClouds: []string{"fleet", "hosteons"}, FleetPrefixes: []string{"synthos-", "syn-fleet-"},
		HomeIPs: []string{"76.158.149.182"},
	}
}

func vals() []Validator {
	return []Validator{{Address: "AAAA1111", Power: 1, Name: "hosteons-validator-1"}, {Address: "BBBB2222", Power: 1, Name: "jim-tower-validator-4"}}
}

func snap(h int64, blockAt time.Time, signed ...string) *Snapshot {
	s := &Snapshot{Height: h, BlockTime: blockAt, Validators: vals(), Signed: map[string]bool{}}
	for _, a := range signed {
		s.Signed[a] = true
	}
	return s
}

func TestHaltAlertsOnceAndRecovers(t *testing.T) {
	w := NewWatchdog(testCfg(), nil)
	if a := w.Observe(t0, snap(100, t0, "AAAA1111", "BBBB2222")); len(a) != 0 {
		t.Fatalf("healthy chain should be quiet, got %v", a)
	}
	a := w.Observe(t0.Add(4*time.Minute), snap(100, t0, "AAAA1111", "BBBB2222"))
	if len(a) != 1 || !strings.Contains(a[0], "stopped making blocks") {
		t.Fatalf("want one halt alert, got %v", a)
	}
	if a := w.Observe(t0.Add(5*time.Minute), snap(100, t0, "AAAA1111", "BBBB2222")); len(a) != 0 {
		t.Fatalf("halt must not repeat, got %v", a)
	}
	a = w.Observe(t0.Add(6*time.Minute), snap(101, t0.Add(6*time.Minute), "AAAA1111", "BBBB2222"))
	if len(a) != 1 || !strings.Contains(a[0], "being made again") {
		t.Fatalf("want one recovery message, got %v", a)
	}
}

func TestValidatorAbsentLongEnoughAlertsThenRecovers(t *testing.T) {
	w := NewWatchdog(testCfg(), nil)
	var all []string
	// The tower is absent from every commit for 6 minutes, checked every 30 s.
	for i := 0; i <= 12; i++ {
		now := t0.Add(time.Duration(i) * 30 * time.Second)
		all = append(all, w.Observe(now, snap(int64(100+i), now, "AAAA1111"))...)
	}
	if len(all) != 1 || !strings.Contains(all[0], "jim-tower-validator-4") || !strings.Contains(all[0], "hasn't signed") {
		t.Fatalf("want exactly one offline alert for the tower, got %v", all)
	}
	now := t0.Add(7 * time.Minute)
	a := w.Observe(now, snap(120, now, "AAAA1111", "BBBB2222"))
	if len(a) != 1 || !strings.Contains(a[0], "signing blocks again") {
		t.Fatalf("want one recovery message, got %v", a)
	}
}

func TestOneMissedCommitIsNotAnAlert(t *testing.T) {
	w := NewWatchdog(testCfg(), nil)
	var all []string
	for i := 0; i <= 20; i++ {
		now := t0.Add(time.Duration(i) * 30 * time.Second)
		signed := []string{"AAAA1111", "BBBB2222"}
		if i%4 == 0 { // the tower is left out of an occasional commit
			signed = []string{"AAAA1111"}
		}
		all = append(all, w.Observe(now, snap(int64(100+i), now, signed...))...)
	}
	if len(all) != 0 {
		t.Fatalf("an occasional missing signature must not alert, got %v", all)
	}
}

func TestOutsideNodeAnnouncedOnceAfterRealUptime(t *testing.T) {
	w := NewWatchdog(testCfg(), nil)
	nodes := []Node{
		{Name: "synthos-hosteons-validator-2", Cloud: "hosteons", IP: "127.0.0.1:1", ValidHeartbeats: 900},
		{Name: "syn-fleet-1", Cloud: "fleet", IP: "127.0.0.1:2", ValidHeartbeats: 900},
		{Name: "syn-71d34cadab77", Cloud: "operator", IP: "76.158.149.182", ValidHeartbeats: 9000}, // Jim's home
		{Name: "syn-bot", Cloud: "operator", IP: "34.53.168.88", ValidHeartbeats: 2},               // a crawler
		{Name: "syn-real", Cloud: "operator", IP: "203.0.113.7", ValidHeartbeats: 25, RegisteredAt: t0},
	}
	s := snap(100, t0, "AAAA1111", "BBBB2222")
	s.Nodes = nodes
	a := w.Observe(t0, s)
	if len(a) != 1 || !strings.Contains(a[0], "syn-real") {
		t.Fatalf("want only syn-real announced, got %v", a)
	}
	s2 := snap(101, t0.Add(time.Minute), "AAAA1111", "BBBB2222")
	s2.Nodes = nodes
	if a := w.Observe(t0.Add(time.Minute), s2); len(a) != 0 {
		t.Fatalf("an outside node is announced only once, got %v", a)
	}
	// After a restart the remembered list stops a repeat.
	w2 := NewWatchdog(testCfg(), w.KnownOutside())
	if a := w2.Observe(t0, s); len(a) != 0 {
		t.Fatalf("remembered nodes must not be re-announced, got %v", a)
	}
}

// fakeNetwork serves the RPC, chain API and registry endpoints the bot reads.
func fakeNetwork(t *testing.T) *httptest.Server {
	t.Helper()
	write := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/status":
			write(w, map[string]any{"result": map[string]any{"sync_info": map[string]any{
				"latest_block_height": "134711", "latest_block_time": t0.Format(time.RFC3339Nano)}}})
		case "/validators":
			write(w, map[string]any{"result": map[string]any{"validators": []map[string]any{
				{"address": "2EFA14D4903A1644185FA63E9E5F1B1872F664F6", "voting_power": "1000000"},
				{"address": "FADB813BD6A969C0BCF70E9FFA75957C6CDE63AE", "voting_power": "1000000"}}}})
		case "/commit":
			if r.URL.Query().Get("height") != "134711" {
				t.Errorf("commit asked for height %q", r.URL.Query().Get("height"))
			}
			write(w, map[string]any{"result": map[string]any{"signed_header": map[string]any{"commit": map[string]any{
				"signatures": []map[string]any{
					{"block_id_flag": 2, "validator_address": "2EFA14D4903A1644185FA63E9E5F1B1872F664F6"},
					{"block_id_flag": 1, "validator_address": ""}}}}}})
		case "/api/explorer/blocks":
			write(w, map[string]any{"blocks": []map[string]any{{"header": map[string]any{
				"proposer_id": "jim-tower-validator-4", "proposer_address": "0xfadb813bd6a969c0bcf70e9ffa75957c6cde63ae"}}}})
		case "/api/nodes":
			write(w, map[string]any{"peers": []map[string]any{
				{"name": "syn-real", "cloud": "operator", "household_ip": "203.0.113.7", "valid_heartbeats": 30,
					"registered_at": t0.UnixMilli(), "last_seen": t0.UnixMilli()}}})
		case "/api/nodes/syn-real/status":
			write(w, map[string]any{"node": map[string]any{"node_id": "syn-real", "role": "validator_candidate",
				"proof_status": "proving", "valid_heartbeats": 30, "verified_days": 1.5, "health": true,
				"last_heartbeat_at": "2026-11-09T12:00:00Z", "reward_status": "proving_uptime"}})
		case "/balance":
			write(w, map[string]any{"address": r.URL.Query().Get("address"), "balance": 1234})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestSnapshotReadsTheNetwork(t *testing.T) {
	srv := fakeNetwork(t)
	defer srv.Close()
	src := &Sources{RPC: srv.URL, ChainAPI: srv.URL, Registry: srv.URL}
	s, err := src.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Height != 134711 || !s.BlockTime.Equal(t0) || len(s.Validators) != 2 {
		t.Fatalf("snapshot: %+v", s)
	}
	if !s.Signed["2EFA14D4903A1644185FA63E9E5F1B1872F664F6"] || s.Signed["FADB813BD6A969C0BCF70E9FFA75957C6CDE63AE"] {
		t.Fatalf("signatures read wrong: %v", s.Signed)
	}
	if s.Validators[1].Name != "jim-tower-validator-4" {
		t.Fatalf("validator name not mapped: %+v", s.Validators[1])
	}
	if s.NodesErr != nil || len(s.Nodes) != 1 || s.Nodes[0].ValidHeartbeats != 30 {
		t.Fatalf("nodes: %+v %v", s.Nodes, s.NodesErr)
	}
}

func TestCommands(t *testing.T) {
	srv := fakeNetwork(t)
	defer srv.Close()
	src := &Sources{RPC: srv.URL, ChainAPI: srv.URL, Registry: srv.URL}
	bot := &Bot{Sources: src, AI: &AI{}, Cfg: testCfg(), limit: newRateLimiter()}
	s, err := src.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	bot.setLatest(s)
	ctx := context.Background()
	at := t0.Add(time.Minute)
	next := func() time.Time { at = at.Add(3 * time.Second); return at }

	cases := []struct{ in, want string }{
		{"/help", "/node <node id>"},
		{"/status", "Validators signing the latest block: 1 of 2"},
		{"/status@SynthosBot", "Chain: running"},
		{"/validators", "jim-tower-validator-4 (FADB813B), 1000000 voting power, ❌ did not sign"},
		{"/nodes", "syn-real (30 heartbeats)"},
		{"/node syn-real", "Verified uptime: 1.5 of 30 days"},
		{"/node", "Usage: /node"},
		{"/node nosuchnode", "I couldn't find that node"},
		{"/balance 0x1111111111111111111111111111111111111111", "holds 1234 SYN"},
		{"/balance nope", "0x followed by 40 hex characters"},
		{"/doesnotexist", "I don't know that command"},
		{"what is the treasury?", "Plain-language questions aren't switched on yet"},
	}
	for _, c := range cases {
		if got := bot.Handle(ctx, 42, c.in, next()); !strings.Contains(got, c.want) {
			t.Errorf("%q: want %q in reply, got:\n%s", c.in, c.want, got)
		}
	}
	// Two messages within 2 seconds: the second is ignored.
	now := next()
	if bot.Handle(ctx, 7, "/status", now) == "" || bot.Handle(ctx, 7, "/status", now.Add(time.Second)) != "" {
		t.Error("rate limit did not apply")
	}
}

func TestStoppedChainShowsInStatus(t *testing.T) {
	bot := &Bot{Cfg: testCfg(), limit: newRateLimiter()}
	bot.setLatest(snap(100, t0, "AAAA1111"))
	if got := bot.statusText(t0.Add(10 * time.Minute)); !strings.Contains(got, "Chain: STOPPED") {
		t.Fatalf("got %s", got)
	}
}

func TestAIAnswerUsesLiveDataAndKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" || r.Header.Get("x-api-key") != "test-key" || r.Header.Get("anthropic-version") == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		var body struct {
			Model  string `json:"model"`
			System string `json:"system"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "test-model" || !strings.Contains(body.System, "Block: 134711") || !strings.Contains(body.System, "Never predict SYN's price") {
			http.Error(w, "bad body", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]string{{"type": "text", "text": "Half of each fee goes to the treasury."}}})
	}))
	defer srv.Close()
	ai := &AI{Key: "test-key", Model: "test-model", API: srv.URL}
	got, err := ai.Answer(context.Background(), "where do fees go?", "Block: 134711")
	if err != nil || got != "Half of each fee goes to the treasury." {
		t.Fatalf("got %q %v", got, err)
	}
	// A rate-limited chat gets a polite refusal instead of more AI calls.
	bot := &Bot{AI: ai, Cfg: testCfg(), limit: newRateLimiter()}
	bot.limit.AIPerChat = 1
	bot.setLatest(snap(134711, t0, "AAAA1111"))
	if r := bot.Handle(context.Background(), 9, "first question", t0); r == "" || strings.Contains(r, "a lot of questions") {
		t.Fatalf("first AI question should be answered, got %q", r)
	}
	if r := bot.Handle(context.Background(), 9, "second question", t0.Add(5*time.Second)); !strings.Contains(r, "a lot of questions") {
		t.Fatalf("second AI question should be limited, got %q", r)
	}
}

func TestSecretFileMustBePrivate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte("secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecretFile(p); err == nil {
		t.Fatal("a world-readable token file must be refused")
	}
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := readSecretFile(p); err != nil || got != "secret" {
		t.Fatalf("got %q %v", got, err)
	}
}

func TestTelegramSendHidesTokenOnError(t *testing.T) {
	tg := &Telegram{Token: "123:SECRET", API: "http://127.0.0.1:1"}
	err := tg.Send(context.Background(), 1, "hi")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error must not contain the token: %v", err)
	}
}
