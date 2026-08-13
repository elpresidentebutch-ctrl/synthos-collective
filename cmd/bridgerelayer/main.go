package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

const bridgeLockedTopic = "0x50f709ab204aa3a58cdfc578dffa863d2b371f796ec7a5f3b01ec247d686ea98"

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

type evmLog struct {
	Address          string   `json:"address"`
	Topics           []string `json:"topics"`
	Data             string   `json:"data"`
	BlockNumber      string   `json:"blockNumber"`
	TransactionHash  string   `json:"transactionHash"`
	TransactionIndex string   `json:"transactionIndex"`
	LogIndex         string   `json:"logIndex"`
	Removed          bool     `json:"removed"`
}

func main() {
	mode := flag.String("mode", env("SYNTHOS_BRIDGE_RELAYER_MODE", "watch-native"), "watch-native, watch-evm, or submit-native-release")
	rpcURL := flag.String("rpc", env("SYNTHOS_NATIVE_RPC_URL", "http://127.0.0.1:8080"), "SYNTHOS native RPC URL")
	evmRPCURL := flag.String("evm-rpc", os.Getenv("SYNTHOS_BRIDGE_EVM_RPC_URL"), "EVM JSON-RPC URL")
	evmVault := flag.String("evm-vault", os.Getenv("SYNTHOS_BRIDGE_EVM_VAULT"), "EVM SYNTHOSBridgeVault address")
	startBlock := flag.Uint64("from-block", envUint64("SYNTHOS_BRIDGE_EVM_FROM_BLOCK", 0), "EVM start block")
	minConfirmations := flag.Uint64("min-confirmations", envUint64("SYNTHOS_BRIDGE_MIN_CONFIRMATIONS", 12), "EVM confirmations required before proof/release")
	autoSubmitNative := flag.Bool("auto-submit-native", os.Getenv("SYNTHOS_BRIDGE_AUTO_SUBMIT_NATIVE") == "true", "submit native releases from confirmed EVM BridgeLocked logs")
	poll := flag.Duration("poll", envDuration("SYNTHOS_BRIDGE_POLL_INTERVAL", 15*time.Second), "poll interval")
	once := flag.Bool("once", os.Getenv("SYNTHOS_BRIDGE_ONCE") == "true", "run one pass and exit")
	outbox := flag.String("outbox", env("SYNTHOS_BRIDGE_OUTBOX", ".synthos/bridge-outbox.jsonl"), "native bridge event outbox path")
	proofOutbox := flag.String("proof-outbox", env("SYNTHOS_BRIDGE_PROOF_OUTBOX", ".synthos/evm-bridge-proofs.jsonl"), "confirmed EVM lock proof JSONL path")
	proofFile := flag.String("proof", os.Getenv("SYNTHOS_BRIDGE_PROOF_FILE"), "external lock proof JSON for submit-native-release")
	privateKey := flag.String("priv", os.Getenv("SYNTHOS_BRIDGE_AUTHORITY_PRIVATE_KEY"), "Ed25519 bridge authority private key for native release tx")
	fee := flag.Uint64("fee", chain.MIN_FEE, "native SYN fee")
	flag.Parse()

	switch *mode {
	case "watch-native":
		if err := watchNative(*rpcURL, *outbox, *poll, *once); err != nil {
			fatal(err)
		}
	case "watch-evm":
		err := watchEVM(*evmRPCURL, *evmVault, *rpcURL, *proofOutbox, *privateKey, *fee, *startBlock, *minConfirmations, *poll, *once, *autoSubmitNative)
		if err != nil {
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

func watchEVM(evmRPCURL, vault, nativeRPCURL, proofOutbox, privHex string, fee uint64, startBlock, minConfirmations uint64, poll time.Duration, once bool, autoSubmitNative bool) error {
	if evmRPCURL == "" {
		return fmt.Errorf("evm-rpc required")
	}
	if vault == "" {
		return fmt.Errorf("evm-vault required")
	}
	vault = strings.ToLower(vault)
	if !strings.HasPrefix(vault, "0x") || len(vault) != 42 {
		return fmt.Errorf("evm-vault must be a 20-byte hex address")
	}
	nextBlock := startBlock
	seen := map[string]bool{}
	for {
		head, err := evmBlockNumber(evmRPCURL)
		if err != nil {
			return err
		}
		if head >= minConfirmations {
			confirmedHead := head - minConfirmations
			if nextBlock == 0 {
				nextBlock = confirmedHead
			}
			if nextBlock <= confirmedHead {
				logs, err := evmBridgeLockedLogs(evmRPCURL, vault, nextBlock, confirmedHead)
				if err != nil {
					return err
				}
				for _, log := range logs {
					proof, err := proofFromBridgeLockedLog(log, head, minConfirmations)
					if err != nil {
						return err
					}
					if seen[proof.SourceEventID] {
						continue
					}
					if err := appendJSONL(proofOutbox, proof); err != nil {
						return err
					}
					seen[proof.SourceEventID] = true
					fmt.Printf("observed confirmed EVM bridge lock: source_event=%s amount=%d recipient=%s confirmations=%d\n", proof.SourceEventID, proof.Amount, proof.Recipient, proof.Confirmations)
					if autoSubmitNative {
						if err := submitNativeReleaseProof(nativeRPCURL, proof, privHex, fee); err != nil {
							return err
						}
					}
				}
				nextBlock = confirmedHead + 1
			}
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
	return submitNativeReleaseProof(rpcURL, proof, privHex, fee)
}

func submitNativeReleaseProof(rpcURL string, proof externalLockProof, privHex string, fee uint64) error {
	if privHex == "" {
		return fmt.Errorf("bridge authority private key required")
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

func evmBlockNumber(rpcURL string) (uint64, error) {
	var out string
	if err := evmRPC(rpcURL, "eth_blockNumber", []any{}, &out); err != nil {
		return 0, err
	}
	return parseHexUint64(out)
}

func evmBridgeLockedLogs(rpcURL string, vault string, fromBlock uint64, toBlock uint64) ([]evmLog, error) {
	var logs []evmLog
	filter := map[string]any{
		"address":   vault,
		"fromBlock": uint64Hex(fromBlock),
		"toBlock":   uint64Hex(toBlock),
		"topics":    []any{bridgeLockedTopic},
	}
	if err := evmRPC(rpcURL, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func proofFromBridgeLockedLog(log evmLog, currentHead uint64, minConfirmations uint64) (externalLockProof, error) {
	if log.Removed {
		return externalLockProof{}, fmt.Errorf("removed EVM log")
	}
	if len(log.Topics) < 4 || strings.ToLower(log.Topics[0]) != bridgeLockedTopic {
		return externalLockProof{}, fmt.Errorf("not a BridgeLocked log")
	}
	sourceChainID, err := topicUintString(log.Topics[2])
	if err != nil {
		return externalLockProof{}, fmt.Errorf("source chain topic: %w", err)
	}
	destinationChainID, err := topicUintString(log.Topics[3])
	if err != nil {
		return externalLockProof{}, fmt.Errorf("destination chain topic: %w", err)
	}
	blockNumber, err := parseHexUint64(log.BlockNumber)
	if err != nil {
		return externalLockProof{}, err
	}
	if currentHead < blockNumber {
		return externalLockProof{}, fmt.Errorf("log block is above current head")
	}
	confirmations := currentHead - blockNumber + 1
	_, _, recipientBytes, amount, err := decodeBridgeLockedData(log.Data)
	if err != nil {
		return externalLockProof{}, err
	}
	recipient, err := nativeRecipientFromBytes(recipientBytes)
	if err != nil {
		return externalLockProof{}, err
	}
	sourceEventID := strings.ToLower(log.Topics[1])
	if log.TransactionHash != "" && log.LogIndex != "" {
		sourceEventID = strings.ToLower(log.TransactionHash + ":" + log.LogIndex)
	}
	return externalLockProof{
		SourceChainID:      sourceChainID,
		SourceEventID:      sourceEventID,
		Recipient:          recipient,
		Amount:             amount,
		AssetID:            "syn",
		Confirmations:      confirmations,
		MinConfirmations:   minConfirmations,
		ObservedBlock:      blockNumber,
		ObservedTxHash:     log.TransactionHash,
		DestinationChainID: destinationChainID,
	}, nil
}

func decodeBridgeLockedData(dataHex string) (asset string, sender string, destinationRecipient []byte, amount uint64, err error) {
	dataHex = strings.TrimPrefix(dataHex, "0x")
	data, err := hex.DecodeString(dataHex)
	if err != nil {
		return "", "", nil, 0, err
	}
	if len(data) < 5*32 {
		return "", "", nil, 0, fmt.Errorf("BridgeLocked data too short")
	}
	asset = "0x" + hex.EncodeToString(data[12:32])
	sender = "0x" + hex.EncodeToString(data[44:64])
	offset, err := wordUint64(data[64:96])
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("recipient offset: %w", err)
	}
	amount, err = wordUint64(data[96:128])
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("amount: %w", err)
	}
	if offset > uint64(len(data)) || offset+32 > uint64(len(data)) {
		return "", "", nil, 0, fmt.Errorf("recipient offset out of range")
	}
	length, err := wordUint64(data[offset : offset+32])
	if err != nil {
		return "", "", nil, 0, fmt.Errorf("recipient length: %w", err)
	}
	start := offset + 32
	end := start + length
	if end > uint64(len(data)) {
		return "", "", nil, 0, fmt.Errorf("recipient bytes out of range")
	}
	return asset, sender, data[start:end], amount, nil
}

func nativeRecipientFromBytes(raw []byte) (chain.Address, error) {
	text := strings.TrimSpace(string(raw))
	if strings.HasPrefix(text, "0x") && len(text) >= 4 {
		return chain.Address(text), nil
	}
	if len(raw) == 20 {
		return chain.Address("0x" + hex.EncodeToString(raw)), nil
	}
	return "", fmt.Errorf("destination recipient must be a native address string or 20-byte address")
}

func evmRPC(rpcURL string, method string, params []any, result any) error {
	payload := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}
	data, _ := json.Marshal(payload)
	resp, err := http.Post(strings.TrimRight(rpcURL, "/"), "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("EVM RPC %s failed: %s", method, strings.TrimSpace(string(body)))
	}
	var envelope struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return fmt.Errorf("EVM RPC %s error %d: %s", method, envelope.Error.Code, envelope.Error.Message)
	}
	return json.Unmarshal(envelope.Result, result)
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

func topicUintString(topic string) (string, error) {
	n, err := hexBig(topic)
	if err != nil {
		return "", err
	}
	return n.String(), nil
}

func parseHexUint64(value string) (uint64, error) {
	n, err := hexBig(value)
	if err != nil {
		return 0, err
	}
	if !n.IsUint64() {
		return 0, fmt.Errorf("hex value overflows uint64")
	}
	return n.Uint64(), nil
}

func wordUint64(word []byte) (uint64, error) {
	if len(word) != 32 {
		return 0, fmt.Errorf("ABI word must be 32 bytes")
	}
	n := new(big.Int).SetBytes(word)
	if !n.IsUint64() {
		return 0, fmt.Errorf("ABI word overflows uint64")
	}
	return n.Uint64(), nil
}

func hexBig(value string) (*big.Int, error) {
	value = strings.TrimPrefix(strings.TrimSpace(value), "0x")
	if value == "" {
		return big.NewInt(0), nil
	}
	n := new(big.Int)
	if _, ok := n.SetString(value, 16); !ok {
		return nil, fmt.Errorf("invalid hex integer")
	}
	return n, nil
}

func uint64Hex(value uint64) string {
	return "0x" + strconv.FormatUint(value, 16)
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envUint64(key string, fallback uint64) uint64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseUint(value, 10, 64); err == nil {
			return parsed
		}
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
