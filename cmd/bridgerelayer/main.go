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

// synBurnedForNativeReleaseTopic is keccak256("SynBurnedForNativeRelease(bytes32,address,bytes,uint256,uint256)"),
// emitted by SYNTHOSSynBridgeMinter.burnToNative when a holder sends their
// wrapped SYN home for release on the native chain.
const synBurnedForNativeReleaseTopic = "0xa24b8f1fbe9297092be42f355cae9efc17ac2f4086a52b7d8577707a458ca97d"

// wrappedSynUnitScale converts between the EVM-side wrapped SYN ERC20's
// 18-decimal units and the native chain's own SYN, which the native chain
// represents as whole, indivisible units (decimals = 0, see genesis
// metadata across cmd/opennet, cmd/rpcnode, etc.). Every amount that
// crosses this specific bridge has to pass through this scale factor
// exactly once, in the right direction, or value is created or destroyed
// at the boundary. This does not apply to SYNTHOSBridgeVault's foreign
// assets, which are a separate flow with their own (currently unscaled)
// accounting.
var wrappedSynUnitScale = new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

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
	SourceChainID       string                           `json:"source_chain_id"`
	SourceEventID       string                           `json:"source_event_id"`
	Recipient           chain.Address                    `json:"recipient"`
	Amount              uint64                           `json:"amount"`
	AssetID             string                           `json:"asset_id,omitempty"`
	Confirmations       uint64                           `json:"confirmations,omitempty"`
	MinConfirmations    uint64                           `json:"min_confirmations,omitempty"`
	ObservedBlock       uint64                           `json:"observed_block,omitempty"`
	ObservedTxHash      string                           `json:"observed_tx_hash,omitempty"`
	DestinationChainID  string                           `json:"destination_chain_id,omitempty"`
	ValidatorSignatures []chain.BridgeValidatorSignature `json:"validator_signatures,omitempty"`
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
	evmSynMinter := flag.String("evm-syn-minter", os.Getenv("SYNTHOS_BRIDGE_EVM_SYN_MINTER"), "EVM SYNTHOSSynBridgeMinter address (watches wrapped-SYN burn-for-release events)")
	startBlock := flag.Uint64("from-block", envUint64("SYNTHOS_BRIDGE_EVM_FROM_BLOCK", 0), "EVM start block for SYNTHOSBridgeVault")
	synMinterStartBlock := flag.Uint64("syn-minter-from-block", envUint64("SYNTHOS_BRIDGE_EVM_SYN_MINTER_FROM_BLOCK", 0), "EVM start block for SYNTHOSSynBridgeMinter")
	minConfirmations := flag.Uint64("min-confirmations", envUint64("SYNTHOS_BRIDGE_MIN_CONFIRMATIONS", 12), "EVM confirmations required before proof/release")
	autoSubmitNative := flag.Bool("auto-submit-native", os.Getenv("SYNTHOS_BRIDGE_AUTO_SUBMIT_NATIVE") == "true", "submit native releases from confirmed EVM BridgeLocked logs")
	poll := flag.Duration("poll", envDuration("SYNTHOS_BRIDGE_POLL_INTERVAL", 15*time.Second), "poll interval")
	once := flag.Bool("once", os.Getenv("SYNTHOS_BRIDGE_ONCE") == "true", "run one pass and exit")
	outbox := flag.String("outbox", env("SYNTHOS_BRIDGE_OUTBOX", ".synthos/bridge-outbox.jsonl"), "native bridge event outbox path")
	proofOutbox := flag.String("proof-outbox", env("SYNTHOS_BRIDGE_PROOF_OUTBOX", ".synthos/evm-bridge-proofs.jsonl"), "confirmed EVM lock proof JSONL path")
	proofFile := flag.String("proof", os.Getenv("SYNTHOS_BRIDGE_PROOF_FILE"), "external lock proof JSON for submit-native-release")
	privateKey := flag.String("priv", os.Getenv("SYNTHOS_BRIDGE_AUTHORITY_PRIVATE_KEY"), "Ed25519 bridge authority private key for native release tx")
	fee := flag.Uint64("fee", chain.MIN_FEE, "native SYN fee")
	proposeBlockToken := flag.String("propose-block-token", os.Getenv("SYNTHOS_PROPOSE_BLOCK_TOKEN"), "X-Propose-Block-Token value used to nudge immediate block production after a release tx; optional (best-effort), required only if the target node has SYNTHOS_PROPOSE_BLOCK_TOKEN configured (see internal/rpc/server.go's handleProposeBlock)")
	flag.Parse()

	switch *mode {
	case "watch-native":
		if err := watchNative(*rpcURL, *outbox, *poll, *once); err != nil {
			fatal(err)
		}
	case "watch-evm":
		err := watchEVM(*evmRPCURL, *evmVault, *evmSynMinter, *rpcURL, *proofOutbox, *privateKey, *fee, *startBlock, *synMinterStartBlock, *minConfirmations, *poll, *once, *autoSubmitNative, *proposeBlockToken)
		if err != nil {
			fatal(err)
		}
	case "submit-native-release":
		if err := submitNativeRelease(*rpcURL, *proofFile, *privateKey, *fee, *proposeBlockToken); err != nil {
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

// evmLogSource is one EVM contract this relayer watches for release-worthy
// events, each tracked with its own block cursor and dedup set since the
// vault and the SYN bridge minter are independent contracts that can start
// at different blocks and confirm independently.
type evmLogSource struct {
	label     string
	address   string
	topic     string
	nextBlock uint64
	seen      map[string]bool
	decode    func(log evmLog, currentHead, minConfirmations uint64) (externalLockProof, error)
}

func watchEVM(evmRPCURL, vault, synMinter, nativeRPCURL, proofOutbox, privHex string, fee uint64, startBlock, synMinterStartBlock, minConfirmations uint64, poll time.Duration, once bool, autoSubmitNative bool, proposeBlockToken string) error {
	if evmRPCURL == "" {
		return fmt.Errorf("evm-rpc required")
	}
	if vault == "" && synMinter == "" {
		return fmt.Errorf("at least one of evm-vault or evm-syn-minter is required")
	}

	var sources []*evmLogSource
	if vault != "" {
		vault = strings.ToLower(vault)
		if !strings.HasPrefix(vault, "0x") || len(vault) != 42 {
			return fmt.Errorf("evm-vault must be a 20-byte hex address")
		}
		sources = append(sources, &evmLogSource{
			label:     "bridge vault lock",
			address:   vault,
			topic:     bridgeLockedTopic,
			nextBlock: startBlock,
			seen:      map[string]bool{},
			decode:    proofFromBridgeLockedLog,
		})
	}
	if synMinter != "" {
		synMinter = strings.ToLower(synMinter)
		if !strings.HasPrefix(synMinter, "0x") || len(synMinter) != 42 {
			return fmt.Errorf("evm-syn-minter must be a 20-byte hex address")
		}
		chainID, err := evmChainID(evmRPCURL)
		if err != nil {
			return fmt.Errorf("fetching EVM chain id for syn bridge minter: %w", err)
		}
		sourceChainID := new(big.Int).SetUint64(chainID).String()
		sources = append(sources, &evmLogSource{
			label:     "SYN burn-for-release",
			address:   synMinter,
			topic:     synBurnedForNativeReleaseTopic,
			nextBlock: synMinterStartBlock,
			seen:      map[string]bool{},
			decode: func(log evmLog, currentHead, minConfirmations uint64) (externalLockProof, error) {
				return proofFromSynBurnedLog(log, sourceChainID, currentHead, minConfirmations)
			},
		})
	}

	for {
		head, err := evmBlockNumber(evmRPCURL)
		if err != nil {
			return err
		}
		if head >= minConfirmations {
			confirmedHead := head - minConfirmations
			for _, source := range sources {
				if source.nextBlock == 0 {
					source.nextBlock = confirmedHead
				}
				if source.nextBlock > confirmedHead {
					continue
				}
				logs, err := evmLogsForAddress(evmRPCURL, source.address, source.topic, source.nextBlock, confirmedHead)
				if err != nil {
					return err
				}
				for _, log := range logs {
					proof, err := source.decode(log, head, minConfirmations)
					if err != nil {
						return err
					}
					if source.seen[proof.SourceEventID] {
						continue
					}
					if err := appendJSONL(proofOutbox, proof); err != nil {
						return err
					}
					source.seen[proof.SourceEventID] = true
					fmt.Printf("observed confirmed %s: source_event=%s amount=%d recipient=%s confirmations=%d\n", source.label, proof.SourceEventID, proof.Amount, proof.Recipient, proof.Confirmations)
					if autoSubmitNative {
						if err := submitNativeReleaseProof(nativeRPCURL, proof, privHex, fee, proposeBlockToken); err != nil {
							return err
						}
					}
				}
				source.nextBlock = confirmedHead + 1
			}
		}
		if once {
			return nil
		}
		time.Sleep(poll)
	}
}

func submitNativeRelease(rpcURL, proofPath, privHex string, fee uint64, proposeBlockToken string) error {
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
	return submitNativeReleaseProof(rpcURL, proof, privHex, fee, proposeBlockToken)
}

func submitNativeReleaseProof(rpcURL string, proof externalLockProof, privHex string, fee uint64, proposeBlockToken string) error {
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
	metadata := []chain.KeyValuePair{
		{Key: "type", Value: "bridge_release_native"},
		{Key: "source_chain_id", Value: proof.SourceChainID},
		{Key: "source_event_id", Value: proof.SourceEventID},
		{Key: "observed_tx_hash", Value: proof.ObservedTxHash},
	}
	if len(proof.ValidatorSignatures) > 0 {
		signatureJSON, _ := json.Marshal(proof.ValidatorSignatures)
		metadata = append(metadata, chain.KeyValuePair{Key: "validator_signatures", Value: string(signatureJSON)})
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
		Metadata:  metadata,
		Timestamp: time.Now().UTC().Unix(),
	}
	if err := tx.Sign(w.Private); err != nil {
		return err
	}
	if err := postNativeTx(rpcURL, tx); err != nil {
		return err
	}
	_ = postProposeBlock(rpcURL, proposeBlockToken)
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

// evmLogsForAddress fetches logs for a single event topic emitted by a
// single contract address, shared by both the bridge vault watcher and the
// SYN bridge minter watcher below.
func evmLogsForAddress(rpcURL string, address string, topic string, fromBlock uint64, toBlock uint64) ([]evmLog, error) {
	var logs []evmLog
	filter := map[string]any{
		"address":   address,
		"fromBlock": uint64Hex(fromBlock),
		"toBlock":   uint64Hex(toBlock),
		"topics":    []any{topic},
	}
	if err := evmRPC(rpcURL, "eth_getLogs", []any{filter}, &logs); err != nil {
		return nil, err
	}
	return logs, nil
}

func evmChainID(rpcURL string) (uint64, error) {
	var out string
	if err := evmRPC(rpcURL, "eth_chainId", []any{}, &out); err != nil {
		return 0, err
	}
	return parseHexUint64(out)
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

// proofFromSynBurnedLog builds a native release proof from a
// SynBurnedForNativeRelease log emitted by SYNTHOSSynBridgeMinter.
// burnId and sender are indexed (topics[1], topics[2]); nativeRecipient,
// amount, and nonce are ABI-encoded in the log data. sourceChainID is
// passed in because, unlike SYNTHOSBridgeVault's BridgeLocked event, this
// event doesn't index the chain id itself.
func proofFromSynBurnedLog(log evmLog, sourceChainID string, currentHead uint64, minConfirmations uint64) (externalLockProof, error) {
	if log.Removed {
		return externalLockProof{}, fmt.Errorf("removed EVM log")
	}
	if len(log.Topics) < 3 || strings.ToLower(log.Topics[0]) != synBurnedForNativeReleaseTopic {
		return externalLockProof{}, fmt.Errorf("not a SynBurnedForNativeRelease log")
	}
	blockNumber, err := parseHexUint64(log.BlockNumber)
	if err != nil {
		return externalLockProof{}, err
	}
	if currentHead < blockNumber {
		return externalLockProof{}, fmt.Errorf("log block is above current head")
	}
	confirmations := currentHead - blockNumber + 1
	recipientBytes, rawAmount, _, err := decodeSynBurnedData(log.Data)
	if err != nil {
		return externalLockProof{}, err
	}
	recipient, err := nativeRecipientFromBytes(recipientBytes)
	if err != nil {
		return externalLockProof{}, err
	}
	nativeAmount, err := nativeAmountFromWrappedSynUnits(rawAmount)
	if err != nil {
		return externalLockProof{}, fmt.Errorf("converting burned wrapped-SYN amount to native units: %w", err)
	}
	sourceEventID := strings.ToLower(log.Topics[1])
	if log.TransactionHash != "" && log.LogIndex != "" {
		sourceEventID = strings.ToLower(log.TransactionHash + ":" + log.LogIndex)
	}
	return externalLockProof{
		SourceChainID:    sourceChainID,
		SourceEventID:    sourceEventID,
		Recipient:        recipient,
		Amount:           nativeAmount,
		AssetID:          "syn",
		Confirmations:    confirmations,
		MinConfirmations: minConfirmations,
		ObservedBlock:    blockNumber,
		ObservedTxHash:   log.TransactionHash,
	}, nil
}

// decodeSynBurnedData decodes the non-indexed fields of
// SynBurnedForNativeRelease(bytes32 indexed, address indexed, bytes
// nativeRecipient, uint256 amount, uint256 nonce): a 3-word head (an offset
// to the dynamic nativeRecipient tail, then amount, then nonce) followed by
// the length-prefixed nativeRecipient bytes. amount is returned as a
// big.Int, uncapped at uint64, because wrapped SYN carries 18 decimals and
// realistic amounts overflow uint64 well before they overflow the native
// chain's own whole-unit accounting.
func decodeSynBurnedData(dataHex string) (nativeRecipient []byte, amount *big.Int, nonce uint64, err error) {
	dataHex = strings.TrimPrefix(dataHex, "0x")
	data, err := hex.DecodeString(dataHex)
	if err != nil {
		return nil, nil, 0, err
	}
	if len(data) < 3*32 {
		return nil, nil, 0, fmt.Errorf("SynBurnedForNativeRelease data too short")
	}
	offset, err := wordUint64(data[0:32])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("recipient offset: %w", err)
	}
	amount = wordBig(data[32:64])
	nonce, err = wordUint64(data[64:96])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("nonce: %w", err)
	}
	if offset > uint64(len(data)) || offset+32 > uint64(len(data)) {
		return nil, nil, 0, fmt.Errorf("recipient offset out of range")
	}
	length, err := wordUint64(data[offset : offset+32])
	if err != nil {
		return nil, nil, 0, fmt.Errorf("recipient length: %w", err)
	}
	start := offset + 32
	end := start + length
	if end > uint64(len(data)) {
		return nil, nil, 0, fmt.Errorf("recipient bytes out of range")
	}
	return data[start:end], amount, nonce, nil
}

// nativeAmountFromWrappedSynUnits converts an 18-decimal wrapped-SYN amount
// into the native chain's own whole-unit SYN accounting. It refuses to
// round: a burn that isn't an exact multiple of one whole SYN can't be
// released natively without either creating or destroying a fraction of a
// coin, so the relayer must not process it (the underlying transfer that
// produced such a fractional wrapped balance is a bug or an attack
// elsewhere, not something to paper over here).
func nativeAmountFromWrappedSynUnits(raw *big.Int) (uint64, error) {
	if raw == nil || raw.Sign() <= 0 {
		return 0, fmt.Errorf("burned amount must be positive")
	}
	quotient, remainder := new(big.Int).QuoRem(raw, wrappedSynUnitScale, new(big.Int))
	if remainder.Sign() != 0 {
		return 0, fmt.Errorf("burned amount %s is not a whole number of SYN (not a multiple of 10^18)", raw.String())
	}
	if !quotient.IsUint64() {
		return 0, fmt.Errorf("converted native amount overflows uint64")
	}
	return quotient.Uint64(), nil
}

// wordBig reads a 32-byte big-endian ABI word as an unsigned big.Int.
func wordBig(word []byte) *big.Int {
	return new(big.Int).SetBytes(word)
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

// postProposeBlock is a best-effort nudge for immediate block production
// after a release tx (its own error is always discarded by callers) --
// token is the X-Propose-Block-Token /proposeBlock now requires (see
// internal/rpc/server.go's handleProposeBlock audit fix); an empty token
// simply won't be accepted if the target node has one configured, in which
// case this falls back to the automatic block-producer loop picking the tx
// up on its own schedule.
func postProposeBlock(rpcURL, token string) error {
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(rpcURL, "/")+"/proposeBlock", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Propose-Block-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
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
