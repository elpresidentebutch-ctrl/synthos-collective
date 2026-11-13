package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"synthos-collective/internal/chain"
)

// Client interacts with the SYNTHOS L1 RPC and registry endpoints.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient returns a new RPC client for interacting with the SYNTHOS network.
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://82-29-54-7.sslip.io"
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// AccountInfo represents account state returned by the RPC endpoint.
type AccountInfo struct {
	Address chain.Address     `json:"address"`
	Balance uint64            `json:"balance"`
	Nonce   uint64            `json:"nonce"`
	Assets  map[string]uint64 `json:"assets,omitempty"`
}

// NetworkStatus represents status metadata returned by /status.
type NetworkStatus struct {
	ChainID     string `json:"chain_id"`
	TxChainID   uint64 `json:"tx_chain_id"`
	Height      int64  `json:"height"`
	Engine      string `json:"engine"`
	StateRoot   string `json:"state_root"`
	Tip         string `json:"tip"`
	CatchingUp  bool   `json:"catching_up"`
}

// BroadcastResult represents the response from /submitTx.
type BroadcastResult struct {
	OK     bool   `json:"ok"`
	TxID   string `json:"tx_id"`
	TxHash string `json:"tx_hash,omitempty"`
	Error  string `json:"error,omitempty"`
}

// GetStatus queries node and consensus network status.
func (c *Client) GetStatus(ctx context.Context) (*NetworkStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("status HTTP %d: %s", resp.StatusCode, string(body))
	}

	var status NetworkStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	return &status, nil
}

// GetAccount queries account balance, nonce, and assets.
func (c *Client) GetAccount(ctx context.Context, addr chain.Address) (*AccountInfo, error) {
	endpoint := fmt.Sprintf("%s/account?address=%s", c.BaseURL, url.QueryEscape(string(addr)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query account: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("account HTTP %d: %s", resp.StatusCode, string(body))
	}

	var acc AccountInfo
	if err := json.NewDecoder(resp.Body).Decode(&acc); err != nil {
		return nil, fmt.Errorf("decode account: %w", err)
	}
	return &acc, nil
}

// GetBalance queries the coin balance for a specific address.
func (c *Client) GetBalance(ctx context.Context, addr chain.Address) (uint64, error) {
	endpoint := fmt.Sprintf("%s/balance?address=%s", c.BaseURL, url.QueryEscape(string(addr)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("query balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, fmt.Errorf("balance HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res struct {
		Address chain.Address `json:"address"`
		Balance uint64        `json:"balance"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return 0, fmt.Errorf("decode balance: %w", err)
	}
	return res.Balance, nil
}

// BroadcastTx submits a signed transaction to /submitTx (or fallback /tx).
func (c *Client) BroadcastTx(ctx context.Context, tx *chain.Tx) (*BroadcastResult, error) {
	if tx == nil {
		return nil, fmt.Errorf("nil transaction")
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		return nil, fmt.Errorf("marshal tx: %w", err)
	}

	// Try /submitTx first
	endpoints := []string{c.BaseURL + "/submitTx", c.BaseURL + "/tx"}
	var lastErr error

	for _, ep := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusOK {
			var result BroadcastResult
			if err := json.Unmarshal(body, &result); err == nil && result.OK {
				if result.TxID == "" {
					result.TxID = tx.ID
				}
				return &result, nil
			}
			return &BroadcastResult{OK: true, TxID: tx.ID}, nil
		}

		// If 404 on /submitTx, loop to try /tx
		if resp.StatusCode == http.StatusNotFound {
			lastErr = fmt.Errorf("endpoint %s not found", ep)
			continue
		}

		return nil, fmt.Errorf("broadcast rejected (HTTP %d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil, fmt.Errorf("broadcast failed: %w", lastErr)
}
