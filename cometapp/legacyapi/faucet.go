package legacyapi

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"synthos-collective/cometapp/app"
	"synthos-collective/internal/chain"
)

// Faucet hands out test coins from its own account on a test network.
// Each address, and each client, can ask once per Cooldown.
type Faucet struct {
	Key      ed25519.PrivateKey
	Amount   uint64
	Fee      uint64        // default 1
	Cooldown time.Duration // default 24h
	// MinInterval spaces out drips from everyone together (default 2s),
	// so a crowd of new clients can't drain it in a burst.
	MinInterval time.Duration

	mu       sync.Mutex
	byAddr   map[string]time.Time
	byClient map[string]time.Time
	lastAny  time.Time
	next     uint64 // next nonce to use; 0 = read it from committed state
}

var addrPattern = regexp.MustCompile(`^0x[0-9a-f]{40}$`)

func (f *Faucet) address() chain.Address {
	return chain.AddressFromPublicKey(f.Key.Public().(ed25519.PublicKey))
}

func (f *Faucet) defaults() (fee uint64, cooldown, minInterval time.Duration) {
	fee, cooldown, minInterval = f.Fee, f.Cooldown, f.MinInterval
	if fee == 0 {
		fee = 1
	}
	if cooldown == 0 {
		cooldown = 24 * time.Hour
	}
	if minInterval == 0 {
		minInterval = 2 * time.Second
	}
	return
}

func (s *Server) faucet(w http.ResponseWriter, r *http.Request) {
	f := s.Faucet
	if f == nil {
		http.Error(w, "this node has no faucet", http.StatusNotFound)
		return
	}
	_, cooldown, _ := f.defaults()
	switch r.Method {
	case http.MethodGet:
		var bal uint64
		s.App.Read(func(c app.Committed) { bal = c.State.Get(f.address()).Balance })
		writeJSON(w, map[string]any{"ok": true, "address": f.address(), "balance": bal,
			"amount": f.Amount, "cooldown_hours": cooldown.Hours()})
	case http.MethodPost:
		var req struct {
			Address string `json:"address"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "send {\"address\": \"0x...\"}", http.StatusBadRequest)
			return
		}
		addr := strings.ToLower(strings.TrimSpace(req.Address))
		if !addrPattern.MatchString(addr) {
			http.Error(w, "address must be 0x followed by 40 hex characters", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		txID, status, err := s.drip(ctx, addr, s.clientKey(r), time.Now())
		if err != nil {
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, map[string]any{"ok": true, "tx_id": txID, "amount": f.Amount, "to": addr,
			"status": "sent; it arrives in the next block"})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) drip(ctx context.Context, to, client string, now time.Time) (string, int, error) {
	f := s.Faucet
	fee, cooldown, minInterval := f.defaults()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.byAddr == nil {
		f.byAddr, f.byClient = map[string]time.Time{}, map[string]time.Time{}
	}
	for k, t := range f.byAddr {
		if now.Sub(t) >= cooldown {
			delete(f.byAddr, k)
		}
	}
	for k, t := range f.byClient {
		if now.Sub(t) >= cooldown {
			delete(f.byClient, k)
		}
	}
	if t, ok := f.byAddr[to]; ok {
		return "", http.StatusTooManyRequests, fmt.Errorf("this address already got test SYN; try again in %s", wait(cooldown-now.Sub(t)))
	}
	if t, ok := f.byClient[client]; ok {
		return "", http.StatusTooManyRequests, fmt.Errorf("you already got test SYN; try again in %s", wait(cooldown-now.Sub(t)))
	}
	if now.Sub(f.lastAny) < minInterval {
		return "", http.StatusTooManyRequests, fmt.Errorf("the faucet is busy; try again in a few seconds")
	}

	from := f.address()
	var acc chain.Account
	var txChainID uint64
	if !s.App.Read(func(c app.Committed) { acc, txChainID = c.State.Get(from), c.TxChainID }) {
		return "", http.StatusServiceUnavailable, fmt.Errorf("chain not initialized yet")
	}
	if acc.Balance < f.Amount+fee {
		return "", http.StatusServiceUnavailable, fmt.Errorf("the faucet is empty")
	}
	nonce := acc.Nonce
	if f.next > nonce {
		nonce = f.next // earlier drips still waiting for a block
	}
	tx := chain.Tx{ChainID: txChainID, From: from, To: chain.Address(to), Amount: f.Amount, Fee: fee, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(f.Key.Public().(ed25519.PublicKey)), Timestamp: now.Unix()}
	if err := tx.Sign(f.Key); err != nil {
		return "", http.StatusInternalServerError, err
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	res, err := s.Comet.BroadcastTxSync(ctx, raw)
	if err == nil && res.Code != 0 {
		err = fmt.Errorf("%s", res.Log)
	}
	if err != nil {
		f.next = 0 // re-read the nonce from the chain next time
		return "", http.StatusServiceUnavailable, fmt.Errorf("faucet transaction failed: %v", err)
	}
	f.next = nonce + 1
	f.byAddr[to], f.byClient[client], f.lastAny = now, now, now
	return tx.ID, 0, nil
}

func wait(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%d minutes", int(d.Minutes())+1)
	}
	return fmt.Sprintf("%d hours", int(d.Hours())+1)
}

// NewFaucetKey makes a faucet key and returns it with its address.
func NewFaucetKey() (ed25519.PrivateKey, chain.Address, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, "", err
	}
	return priv, chain.AddressFromPublicKey(priv.Public().(ed25519.PublicKey)), nil
}
