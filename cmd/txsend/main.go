// Command txsend is a minimal real wallet client for the SYNTHOS L1: it
// checks an account's on-chain balance, and constructs, signs, and submits a
// real signed transfer transaction against a running node's HTTP API
// (internal/rpc.Server — the same code cmd/synthosd runs).
//
// This exists because the chain layer (internal/chain) already has a fully
// tested transaction pipeline — Tx.Sign/Verify, State.ApplyTx, mempool,
// block inclusion — but until now nothing in this repo could actually build
// and submit one. cmd/wallet only ever generated a throwaway keypair.
//
// The private key never leaves the machine this runs on: it is read from an
// environment variable or a local file, never taken as a CLI flag (shell
// history) and never sent anywhere except to locally sign the transaction.
//
// Usage:
//
//	# check a balance / nonce
//	txsend -mode=balance -rpc=https://rpc.ishamwilliamsblockchains.com -address=0x...
//
//	# send SYN (reads the private key from SYNTHOS_PRIVATE_KEY, or -keyfile)
//	SYNTHOS_PRIVATE_KEY=0x... txsend -mode=send \
//	    -rpc=https://rpc.ishamwilliamsblockchains.com \
//	    -to=0x... -amount=1000 -fee=10
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

func main() {
	mode := flag.String("mode", "", "balance | send")
	rpcURL := flag.String("rpc", "", "node base URL, e.g. https://rpc.ishamwilliamsblockchains.com")
	address := flag.String("address", "", "address to check (mode=balance)")
	to := flag.String("to", "", "recipient address (mode=send)")
	amount := flag.Uint64("amount", 0, "amount to send, in the chain's base unit (mode=send)")
	fee := flag.Uint64("fee", 10, "transaction fee (mode=send)")
	assetID := flag.String("asset", "", "asset ID; empty means the native SYN balance")
	chainIDFlag := flag.Uint64("chain-id", 0, "override tx_chain_id instead of fetching it from -rpc /status")
	keyFile := flag.String("keyfile", "", "path to a file containing the hex private key (alternative to SYNTHOS_PRIVATE_KEY)")
	flag.Parse()

	if *rpcURL == "" {
		fatal("missing -rpc")
	}
	base := strings.TrimRight(*rpcURL, "/")

	switch *mode {
	case "balance":
		if *address == "" {
			fatal("mode=balance requires -address")
		}
		acct, err := fetchAccount(base, *address)
		if err != nil {
			fatal("fetching account: %v", err)
		}
		printJSON(acct)

	case "send":
		if *to == "" || *amount == 0 {
			fatal("mode=send requires -to and -amount")
		}
		w, err := loadWallet(*keyFile)
		if err != nil {
			fatal("loading private key: %v", err)
		}
		if err := sendTx(base, w, *to, *amount, *fee, *assetID, *chainIDFlag); err != nil {
			fatal("send failed: %v", err)
		}

	default:
		fatal("must set -mode=balance or -mode=send")
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "txsend: "+format+"\n", args...)
	os.Exit(1)
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func httpClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}

// loadWallet reads the hex private key from -keyfile if given, otherwise
// from the SYNTHOS_PRIVATE_KEY environment variable. It is never accepted
// as a command-line flag, to keep it out of shell history.
func loadWallet(keyFile string) (*wallet.Wallet, error) {
	var hexKey string
	if keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			return nil, err
		}
		hexKey = strings.TrimSpace(string(b))
	} else {
		hexKey = strings.TrimSpace(os.Getenv("SYNTHOS_PRIVATE_KEY"))
	}
	if hexKey == "" {
		return nil, fmt.Errorf("no private key: set SYNTHOS_PRIVATE_KEY or pass -keyfile")
	}
	return wallet.FromPrivateKeyHex(hexKey)
}

type accountResp struct {
	Address string            `json:"address"`
	Balance uint64            `json:"balance"`
	Nonce   uint64            `json:"nonce"`
	Assets  map[string]uint64 `json:"assets"`
}

func fetchAccount(base, address string) (*accountResp, error) {
	resp, err := httpClient().Get(base + "/account?address=" + address)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	var out accountResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

type statusResp struct {
	TxChainID uint64 `json:"tx_chain_id"`
}

func fetchTxChainID(base string) (uint64, error) {
	resp, err := httpClient().Get(base + "/status")
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("%s: %s", resp.Status, string(body))
	}
	var out statusResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, err
	}
	return out.TxChainID, nil
}

func sendTx(base string, w *wallet.Wallet, to string, amount, fee uint64, assetID string, chainIDOverride uint64) error {
	from, err := w.Address()
	if err != nil {
		return err
	}
	pubHex, err := w.PublicKeyHex()
	if err != nil {
		return err
	}

	acct, err := fetchAccount(base, string(from))
	if err != nil {
		return fmt.Errorf("fetching sender account: %w", err)
	}

	chainID := chainIDOverride
	if chainID == 0 {
		chainID, err = fetchTxChainID(base)
		if err != nil {
			return fmt.Errorf("fetching tx_chain_id: %w", err)
		}
	}

	tx := chain.Tx{
		ChainID:   chainID,
		From:      from,
		To:        chain.Address(to),
		Amount:    amount,
		Fee:       fee,
		Nonce:     acct.Nonce,
		PublicKey: pubHex,
		AssetID:   assetID,
	}
	if err := tx.Sign(w.Private); err != nil {
		return fmt.Errorf("signing: %w", err)
	}

	body, err := json.Marshal(tx)
	if err != nil {
		return err
	}
	resp, err := httpClient().Post(base+"/submitTx", "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", resp.Status, string(respBody))
	}
	fmt.Printf("submitted tx %s from %s to %s (amount=%d fee=%d nonce=%d chain_id=%d)\nresponse: %s\n",
		tx.ID, from, to, amount, fee, tx.Nonce, chainID, string(respBody))
	return nil
}
