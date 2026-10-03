package legacyapi

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"synthos-collective/internal/chain"
)

// ---- chain activity --------------------------------------------------------
//
// GET /activity counts what people have done on the chain: wallets that
// used it, transactions, faucet drips, and the latest few transactions.
// It is built by reading committed blocks in order and kept up to date as
// new blocks arrive; nothing about it is stored on chain.

// ActivityItem is one transaction in the recent list.
type ActivityItem struct {
	Height int64  `json:"height"`
	Time   string `json:"time"`
	Kind   string `json:"kind"` // "faucet", "transfer" or the metadata type
	From   string `json:"from"`
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
}

type activity struct {
	mu        sync.Mutex
	indexed   int64 // last block counted
	wallets   map[string]bool
	txs       int
	drips     int
	firstTime string
	lastTime  string
	recent    []ActivityItem // newest last, at most recentKeep
}

const recentKeep = 10

// catchUp counts every committed block not counted yet. It holds the lock
// for the whole pass so concurrent requests don't count a block twice.
func (s *Server) catchUp(ctx context.Context) error {
	a := &s.act
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.wallets == nil {
		a.wallets = map[string]bool{}
	}
	st, err := s.Comet.Status(ctx)
	if err != nil {
		return err
	}
	latest := st.SyncInfo.LatestBlockHeight
	if a.indexed < st.SyncInfo.EarliestBlockHeight-1 {
		a.indexed = st.SyncInfo.EarliestBlockHeight - 1
	}
	faucet := ""
	if s.Faucet != nil {
		faucet = string(s.Faucet.address())
	}
	for h := a.indexed + 1; h <= latest; h++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		rb, err := s.Comet.Block(ctx, &h)
		if err != nil {
			return err
		}
		if len(rb.Block.Txs) > 0 {
			failed := map[int]bool{}
			if res, err := s.Comet.BlockResults(ctx, &h); err == nil {
				for i, r := range res.TxsResults {
					if r.Code != 0 {
						failed[i] = true
					}
				}
			} else {
				return err
			}
			when := rb.Block.Time.UTC().Format(time.RFC3339)
			for i, raw := range rb.Block.Txs {
				var tx chain.Tx
				if failed[i] || json.Unmarshal(raw, &tx) != nil {
					continue
				}
				kind := "transfer"
				for _, kv := range tx.Metadata {
					if kv.Key == "type" && kv.Value != "" {
						kind = kv.Value
					}
				}
				if faucet != "" && string(tx.From) == faucet {
					kind = "faucet"
					a.drips++
					a.wallets[string(tx.To)] = true
				} else {
					a.wallets[string(tx.From)] = true
				}
				a.txs++
				if a.firstTime == "" {
					a.firstTime = when
				}
				a.lastTime = when
				a.recent = append(a.recent, ActivityItem{Height: h, Time: when, Kind: kind,
					From: string(tx.From), To: string(tx.To), Amount: tx.Amount})
				if len(a.recent) > recentKeep {
					a.recent = a.recent[len(a.recent)-recentKeep:]
				}
			}
		}
		a.indexed = h
	}
	return nil
}

// WarmActivity counts the existing chain in the background, so the first
// visitor doesn't wait for it.
func (s *Server) WarmActivity(ctx context.Context) {
	for ctx.Err() == nil {
		_ = s.catchUp(ctx)
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *Server) activity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*requestTimeout)
	defer cancel()
	if err := s.catchUp(ctx); err != nil {
		http.Error(w, "consensus engine unavailable: "+err.Error(), http.StatusServiceUnavailable)
		return
	}
	a := &s.act
	a.mu.Lock()
	recent := make([]ActivityItem, len(a.recent))
	for i := range a.recent { // newest first
		recent[i] = a.recent[len(a.recent)-1-i]
	}
	body := map[string]any{
		"ok":             true,
		"wallets":        len(a.wallets),
		"transactions":   a.txs,
		"faucet_drips":   a.drips,
		"first_activity": a.firstTime,
		"last_activity":  a.lastTime,
		"counted_to":     a.indexed,
		"recent":         recent,
		"note":           "wallets: addresses that sent a transaction or received test SYN from the faucet",
	}
	a.mu.Unlock()
	writeJSON(w, body)
}

// ---- page visits -----------------------------------------------------------
//
// The test network page sends POST /visit once per page view with a random
// id it keeps in the visitor's browser. Only that id is stored: no IP
// address, no browser details. GET /visits shows the counts, and only to
// requests made on this machine directly (not through Cloudflare).

type visits struct {
	mu   sync.Mutex
	path string
	data visitData
}

type visitData struct {
	Views    int                  `json:"views"`
	Visitors map[string]bool      `json:"visitors"`
	Days     map[string]*dayVisit `json:"days"` // UTC date -> counts
}

type dayVisit struct {
	Views    int             `json:"views"`
	Visitors map[string]bool `json:"visitors"`
}

const (
	maxVisitors = 1_000_000
	keepDays    = 60
)

var visitIDPattern = regexp.MustCompile(`^[0-9a-f]{16,64}$`)

func (v *visits) load() {
	if v.data.Visitors != nil {
		return
	}
	v.data = visitData{Visitors: map[string]bool{}, Days: map[string]*dayVisit{}}
	if v.path == "" {
		return
	}
	raw, err := os.ReadFile(v.path)
	if err != nil {
		return
	}
	var d visitData
	if json.Unmarshal(raw, &d) == nil && d.Visitors != nil && d.Days != nil {
		v.data = d
	}
}

func (v *visits) save() {
	if v.path == "" {
		return
	}
	raw, err := json.Marshal(v.data)
	if err != nil {
		return
	}
	tmp := v.path + ".tmp"
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, v.path)
	}
}

func (v *visits) record(id string, now time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.load()
	day := now.UTC().Format("2006-01-02")
	d := v.data.Days[day]
	if d == nil {
		d = &dayVisit{Visitors: map[string]bool{}}
		v.data.Days[day] = d
		cutoff := now.UTC().AddDate(0, 0, -keepDays).Format("2006-01-02")
		for k := range v.data.Days {
			if k < cutoff {
				delete(v.data.Days, k)
			}
		}
	}
	v.data.Views++
	d.Views++
	if len(v.data.Visitors) < maxVisitors {
		v.data.Visitors[id] = true
	}
	if len(d.Visitors) < maxVisitors {
		d.Visitors[id] = true
	}
	v.save()
}

func (s *Server) visit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !visitIDPattern.MatchString(req.ID) {
		http.Error(w, "send {\"id\": \"<16-64 hex characters>\"}", http.StatusBadRequest)
		return
	}
	s.vis.record(req.ID, time.Now())
	writeJSON(w, map[string]any{"ok": true})
}

// localOnly reports whether r came straight from this machine: a loopback
// address and none of the headers Cloudflare (or another proxy) adds, which
// a visitor coming through the tunnel can't remove.
func localOnly(r *http.Request) bool {
	for _, h := range []string{"Cf-Connecting-Ip", "Cf-Ray", "X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type dayRow struct {
	Day      string `json:"day"`
	Views    int    `json:"views"`
	Visitors int    `json:"visitors"`
}

func (s *Server) visitStats(w http.ResponseWriter, r *http.Request) {
	if !localOnly(r) {
		http.Error(w, "visit counts are only shown on the node's own computer", http.StatusForbidden)
		return
	}
	v := &s.vis
	v.mu.Lock()
	v.load()
	rows := make([]dayRow, 0, len(v.data.Days))
	for day, d := range v.data.Days {
		rows = append(rows, dayRow{day, d.Views, len(d.Visitors)})
	}
	total, people := v.data.Views, len(v.data.Visitors)
	v.mu.Unlock()
	sort.Slice(rows, func(i, j int) bool { return rows[i].Day > rows[j].Day })

	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		writeJSON(w, map[string]any{"ok": true, "views": total, "visitors": people, "days": rows})
		return
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><meta charset="utf-8"><title>Test network visits</title>
<style>body{font-family:system-ui,sans-serif;margin:32px;color:#111}td,th{padding:6px 14px;text-align:right}th{background:#eee}td:first-child,th:first-child{text-align:left}</style>`)
	fmt.Fprintf(&b, "<h1>Test network page visits</h1><p><b>%d</b> different visitors, <b>%d</b> page views in total.</p>", people, total)
	b.WriteString("<p>A visitor is one browser; the same person on a phone and a computer counts twice. Days are UTC.</p>")
	b.WriteString("<table><tr><th>Day</th><th>Visitors</th><th>Page views</th></tr>")
	for _, row := range rows {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%d</td></tr>", html.EscapeString(row.Day), row.Visitors, row.Views)
	}
	b.WriteString("</table>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}

// visitsFile is where a node keeps its visit counts.
func visitsFile(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "testnet-visits.json")
}
