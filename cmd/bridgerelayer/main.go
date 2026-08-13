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

type bridgeEvent struct {
	ID                   string        `json:"id"`
	Type                 string        `json:"type"`
	Address              chain.Address `json:"address"`
	Amount               uint64        `json:"amount"`
	AssetID              string        `json:"asset_id,omitempty"`
	SourceChainID        string        `json:"source_chain_id,omitempty"`
	DestinationChainID   string        `json:"destination_chain_id,omitempty"`
	SourceEventID        string        `json:"source_event_id,omitempty"`
	DestinationRecipient string        `json:"destination_recipient,omitempty"`
	TxID                 string        `json:"tx_id"`
	Timestamp            int64         `json:"timestamp"`
}

type externalLockProof struct {
	SourceChainID      string        `json:"source_chain_id"`
	SourceEventID      string        `json:"source_event_id"`
	Recipient          chain.Address `json:"recipient"`
	Amount             uint64        `json:"amount"`
	AssetID            string        `json:"asset_id,omitempty"`
	Confirmations      uint64        `json:"confirmations,omitempty"`
	MinConfirmations   uint64        `json:"min_confirmations,omitempty"`
	ObservedBlock      uint64        `json:"observed_block,omitempty"`
	ObservedTxHash     string        `json:"observed_tx_hash,omitempty"`
	DestinationChainID string        `json:"destination_chain_id,omitempty"`
}

func main() {
	mode := flag.String("mode", env("SYNTHOS_BRIDGE_RELAYER_MODE", "watch-native"), "watch-native or submit-native-release")
	rpcURL := flag.String("rpc", env("SYNTHOS_NATIVE_RPC_URL", "http://127.0.0.1:8080"), "SYNTHOS native RPC URL")
	poll := flag.Duration("poll", envDuration("SYNTHOS_BRIDGE_POLL_INTERVAL", 15*time.Second), "poll interval")
	once := flag.Bool("once", os.Getenv("SYNTHOS_BRIDGE_ONCE") == "true", "run one pass and exit")
	outbox := flag.String("outbox", env("SYNTHOS_BRIDGE_OUTBOX", ".synthos/bridge-outbox.jsonl"), "native bridge event outbox path")
	proofFile := flag.String("proof", os.Getenv("SYNTHOS_BRIDGE_PROOF_FILE"), "external lock proof JSON for submit-native-release")
	privateKey := flag.String("priv", os.Getenv("SYNTHOS_BRIDGE_AUTHORITY_PRIVATE_KEY"), "Ed25519 bridge authority private key for native release tx")
	fee := flag.Uint64("fee", chain.MIN_FEE, "native SYN fee")
	flag.Parse()

	switch *mode {
	case "watch-native":
		if err := watchNative(*rpcURL, *outbox, *poll, *once); err != nil {
			fatal(err)
		}
	case "submit-native-release":
		if err := submitNativeRelease(*rpcURL, *proofFile, *privateKey, *fee); err != nil {
			fatal(err)
		}
	default:
		fatal(fmt.Errorf("unknown mode %q", *mode))
	}
}

func watchNative(rpcURL, outbox string, poll time.Duration, once bool) error {
	seen := map[string]bool{}
	for {
		events, err := fetchNativeBridgeEvents(rpcURL)
		if err != nil {
			return err
		}
		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]
			if event.Type != "bridge_lock_native" || seen[event.ID] {
				continue
			}
			if err := appendJSONL(outbox, event); err != nil {
				return err
			}
			seen[event.ID] = true
			fmt.Printf("observed native bridge lock: id=%s amount=%d destination_chain=%s recipient=%s\n", event.ID, event.Amount, event.DestinationChainID, event.DestinationRecipient)
		}
		if once {
			return nil
		}
		time.Sleep(poll)
	}
}

func submitNativeRelease(rpcURL, proofPath, privHex string, fee uint64) error {
	if proofPath == "" {
		return fmt.Errorf("proof file required")
	}
	if privHex == "" {
		return fmt.Errorf("bridge authority private key required")
	}
	var proof externalLockProof
	data, err := os.ReadFile(proofPath)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &proof); err != nil {
		return err
	}
	if proof.SourceChainID == "" || proof.SourceEventID == "" || proof.Recipient == "" || proof.Amount == 0 {
		return fmt.Errorf("proof requires source_chain_id, source_event_id, recipient, and amount")
	}
	if proof.MinConfirmations > 0 && proof.Confirmations < proof.MinConfirmations {
		return fmt.Errorf("proof has %d confirmations, needs %d", proof.Confirmations, proof.MinConfirmations)
	}
	w, err := wallet.FromPrivateKeyHex(privHex)
	if err != nil {
		return err
	}
	from, _ := w.Address()
	pubHex, _ := w.PublicKeyHex()
	account, err := fetchNativeAccount(rpcURL, from)
	if err != nil {
		return err
	}
	status, err := fetchNativeStatus(rpcURL)
	if err != nil {
		return err
	}
	assetID := proof.AssetID
	if assetID == "" {
		assetID = "syn"
	}
	tx := chain.Tx{
		ChainID:   status.TxChainID,
		From:      from,
		To:        proof.Recipient,
		Amount:    proof.Amount,
		Fee:       fee,
		Nonce:     account.Nonce,
		PublicKey: pubHex,
		AssetID:   map[bool]string{true: "", false: assetID}[assetID == "syn"],
		Metadata: []chain.KeyValuePair{
			{Key: "type", Value: "bridge_release_native"},
			{Key: "source_chain_id", Value: proof.SourceChainID},
			{Key: "source_event_id", Value: proof.SourceEventID},
			{Key: "observed_tx_hash", Value: proof.ObservedTxHash},
		},
		Timestamp: time.Now().UTC().Unix(),
	}
	if err := tx.Sign(w.Private); err != nil {
		return err
	}
	if err := postNativeTx(rpcURL, tx); err != nil {
		return err
	}
	_ = postProposeBlock(rpcURL)
	fmt.Printf("submitted native bridge release: tx_id=%s source_event=%s amount=%d recipient=%s\n", tx.ID, proof.SourceEventID, proof.Amount, proof.Recipient)
	return nil
}

func fetchNativeBridgeEvents(rpcURL string) ([]bridgeEvent, error) {
	var out struct {
		Events []bridgeEvent `json:"events"`
	}
	if err := getJSON(rpcURL, "/bridge/events?type=bridge_lock_native&limit=500", &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}

type nativeAccount struct {
	Nonce uint64 `json:"nonce"`
}

func fetchNativeAccount(rpcURL string, address chain.Address) (nativeAccount, error) {
	var out nativeAccount
	err := getJSON(rpcURL, "/account?address="+string(address), &out)
	return out, err
}

type nativeStatus struct {
	TxChainID uint64 `json:"tx_chain_id"`
}

func fetchNativeStatus(rpcURL string) (nativeStatus, error) {
	var out nativeStatus
	if err := getJSON(rpcURL, "/status", &out); err != nil {
		return out, err
	}
	if out.TxChainID == 0 {
		return out, fmt.Errorf("native RPC did not return tx_chain_id")
	}
	return out, nil
}

func postNativeTx(rpcURL string, tx chain.Tx) error {
	data, _ := json.Marshal(tx)
	resp, err := http.Post(strings.TrimRight(rpcURL, "/")+"/submitTx", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("submitTx failed: %s", strings.TrimSpace(string(body)))
	}
	return nil
}

func postProposeBlock(rpcURL string) error {
	resp, err := http.Post(strings.TrimRight(rpcURL, "/")+"/proposeBlock", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func getJSON(baseURL, path string, out any) error {
	resp, err := http.Get(strings.TrimRight(baseURL, "/") + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("GET %s failed: %s", path, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}

func appendJSONL(path string, value any) error {
	if err := os.MkdirAll(dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, _ := json.Marshal(value)
	_, err = f.Write(append(data, '\n'))
	return err
}

func dir(path string) string {
	i := strings.LastIndexAny(path, `/\`)
	if i < 0 {
		return "."
	}
	return path[:i]
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "bridge relayer:", err)
	os.Exit(1)
}
