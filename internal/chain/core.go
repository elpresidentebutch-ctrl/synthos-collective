package chain

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// -----------------------------
// Constants & Types
// -----------------------------
const (
	MIN_FEE              = 10
	MAX_SUPPLY           = 100_000_000_000 // 100 Billion cap
	ESCROW_COMMISSION    = 1
	INNER_CIRCLE_PRICE   = 200_000_000
	MAX_THREATS_PER_ADDR = 100
	BURN_PERCENT         = 50
)

type KeyValuePair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// -----------------------------
// Transaction Layer (Timeless)
// -----------------------------
type Tx struct {
	ID        string         `json:"id"`
	ChainID   uint64         `json:"chain_id"`
	From      Address        `json:"from"`
	To        Address        `json:"to"`
	Amount    uint64         `json:"amount"`
	Fee       uint64         `json:"fee"`
	Nonce     uint64         `json:"nonce"`
	PublicKey string         `json:"public_key"`
	Signature string         `json:"signature"`
	AssetID   string         `json:"asset_id,omitempty"`
	Metadata  []KeyValuePair `json:"metadata,omitempty"`
	Timestamp int64          `json:"timestamp,omitempty"` // for auditing only
}

var (
	ErrBadTxSig          = errors.New("bad transaction signature")
	ErrTxFromMismatch    = errors.New("from address does not match public key")
	ErrInsufficientFunds = errors.New("insufficient funds")
)

// Sign returns a signed Tx with timeless signing (ignores timestamp)
func (t *Tx) Sign(priv ed25519.PrivateKey) error {
	if err := t.validateBasic(); err != nil {
		return err
	}

	signerPub := priv.Public().(ed25519.PublicKey)
	if AddressFromPublicKey(signerPub) != t.From {
		return ErrTxFromMismatch
	}

	payload, err := t.signingBytes()
	if err != nil {
		return err
	}

	t.Signature = "0x" + hex.EncodeToString(ed25519.Sign(priv, payload))
	hash := sha256.Sum256(payload)
	t.ID = "0x" + hex.EncodeToString(hash[:])
	return nil
}

func (t *Tx) ComputeID() error {
	payload, err := t.signingBytes()
	if err != nil {
		return err
	}
	hash := sha256.Sum256(payload)
	t.ID = "0x" + hex.EncodeToString(hash[:])
	return nil
}

// Verify checks the signature against the canonical payload
func (t *Tx) Verify() error {
	if err := t.validateBasic(); err != nil {
		return err
	}

	payload, err := t.signingBytes()
	if err != nil {
		return err
	}

	sigBytes, err := hex.DecodeString(strings.TrimPrefix(t.Signature, "0x"))
	if err != nil || len(sigBytes) != 64 {
		return fmt.Errorf("invalid signature")
	}

	pubBytes, err := hex.DecodeString(strings.TrimPrefix(t.PublicKey, "0x"))
	if err != nil || len(pubBytes) != 32 {
		return fmt.Errorf("invalid public key")
	}

	if AddressFromPublicKey(pubBytes) != t.From {
		return ErrTxFromMismatch
	}

	if !ed25519.Verify(pubBytes, payload, sigBytes) {
		return ErrBadTxSig
	}

	hash := sha256.Sum256(payload)
	if t.ID != "0x"+hex.EncodeToString(hash[:]) {
		return fmt.Errorf("transaction ID mismatch")
	}

	return nil
}

func (t *Tx) validateBasic() error {
	if t.ChainID == 0 {
		return fmt.Errorf("missing chain ID")
	}
	if t.From == "" || t.To == "" {
		return fmt.Errorf("missing from/to address")
	}
	if t.Amount == 0 {
		return fmt.Errorf("amount must be greater than zero")
	}
	if t.Fee == 0 {
		return fmt.Errorf("fee must be greater than zero")
	}
	if strings.TrimPrefix(t.PublicKey, "0x") == "" {
		return fmt.Errorf("missing public key")
	}
	return nil
}

// signingBytes returns canonical JSON payload excluding signature and ID
func (t *Tx) signingBytes() ([]byte, error) {
	payload := struct {
		Version   uint16         `json:"version"`
		ChainID   uint64         `json:"chain_id"`
		From      Address        `json:"from"`
		To        Address        `json:"to"`
		Amount    uint64         `json:"amount"`
		Fee       uint64         `json:"fee"`
		Nonce     uint64         `json:"nonce"`
		PublicKey string         `json:"public_key"`
		AssetID   string         `json:"asset_id,omitempty"`
		Metadata  []KeyValuePair `json:"metadata,omitempty"`
	}{
		Version:   1,
		ChainID:   t.ChainID,
		From:      t.From,
		To:        t.To,
		Amount:    t.Amount,
		Fee:       t.Fee,
		Nonce:     t.Nonce,
		PublicKey: t.PublicKey,
		AssetID:   t.AssetID,
		Metadata:  t.Metadata,
	}

	// Sort metadata for deterministic serialization
	sort.Slice(payload.Metadata, func(i, j int) bool {
		return payload.Metadata[i].Key < payload.Metadata[j].Key
	})

	return json.Marshal(payload)
}

// -----------------------------
// State Layer
// -----------------------------
type Account struct {
	Balance uint64            `json:"balance"`
	Assets  map[string]uint64 `json:"assets"`
	Nonce   uint64            `json:"nonce"`
}

type ImmuneNodeRecord struct {
	Address           Address `json:"address"`
	HardwareHash      string  `json:"hardware_hash"`
	ActivatedAt       int64   `json:"activated_at"`
	LastProofAt       int64   `json:"last_proof_at,omitempty"`
	SovereignProofs   uint64  `json:"sovereign_proofs"`
	OptInLocalOnly    bool    `json:"opt_in_local_only"`
	CryptographicMode string  `json:"cryptographic_mode"`
}

type SovereignProofRecord struct {
	ID            string  `json:"id"`
	Address       Address `json:"address"`
	ProofHash     string  `json:"proof_hash"`
	NoiseClass    string  `json:"noise_class"`
	DeclaredScope string  `json:"declared_scope"`
	Timestamp     int64   `json:"timestamp"`
}

type BridgeRecord struct {
	ID                   string  `json:"id"`
	Type                 string  `json:"type"`
	Address              Address `json:"address"`
	Amount               uint64  `json:"amount"`
	AssetID              string  `json:"asset_id,omitempty"`
	SourceChainID        string  `json:"source_chain_id,omitempty"`
	DestinationChainID   string  `json:"destination_chain_id,omitempty"`
	SourceEventID        string  `json:"source_event_id,omitempty"`
	DestinationRecipient string  `json:"destination_recipient,omitempty"`
	TxID                 string  `json:"tx_id"`
	Timestamp            int64   `json:"timestamp"`
}

type BridgeValidatorSignature struct {
	ValidatorID string `json:"validator_id"`
	Signature   string `json:"signature"`
}

type ImmuneStats struct {
	ActiveImmuneNodes    int    `json:"active_immune_nodes"`
	SovereignProofs      uint64 `json:"sovereign_proofs"`
	LastProofHash        string `json:"last_proof_hash,omitempty"`
	CryptographicSilence bool   `json:"cryptographic_silence"`
	InboundPortsRequired int    `json:"inbound_ports_required"`
}

type BridgeStats struct {
	NativeLocks       int    `json:"native_locks"`
	NativeReleases    int    `json:"native_releases"`
	ProcessedMessages int    `json:"processed_messages"`
	LastBridgeEventID string `json:"last_bridge_event_id,omitempty"`
}

type AccountLeaf struct {
	Address Address           `json:"address"`
	Balance uint64            `json:"balance"`
	Assets  map[string]uint64 `json:"assets"`
	Nonce   uint64            `json:"nonce"`
}

func (a *Account) LeafHash(addr Address) []byte {
	leaf := AccountLeaf{
		Address: addr,
		Balance: a.Balance,
		Assets:  a.Assets,
		Nonce:   a.Nonce,
	}
	data, _ := json.Marshal(leaf)
	hash := sha256.Sum256(data)
	return hash[:]
}

type State struct {
	Accounts              map[Address]Account
	ImmuneNodes           map[Address]ImmuneNodeRecord
	SovereignProofs       map[string]SovereignProofRecord
	BridgeEvents          map[string]BridgeRecord
	ProcessedBridgeEvents map[string]bool
	BridgeValidators      map[string]string
	BridgeQuorum          uint64
	LastSovereignProofID  string
	LastBridgeEventID     string
	mu                    sync.RWMutex
	TotalSupply           uint64
}

func NewState() *State {
	return &State{
		Accounts:              make(map[Address]Account),
		ImmuneNodes:           make(map[Address]ImmuneNodeRecord),
		SovereignProofs:       make(map[string]SovereignProofRecord),
		BridgeEvents:          make(map[string]BridgeRecord),
		ProcessedBridgeEvents: make(map[string]bool),
		BridgeValidators:      make(map[string]string),
		TotalSupply:           MAX_SUPPLY,
	}
}

func (s *State) Get(a Address) Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	acc, ok := s.Accounts[a]
	if !ok {
		return Account{Assets: make(map[string]uint64)}
	}
	assets := make(map[string]uint64, len(acc.Assets))
	for assetID, balance := range acc.Assets {
		assets[assetID] = balance
	}
	acc.Assets = assets
	return acc
}

func (s *State) Set(a Address, ac Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ac.Assets == nil {
		ac.Assets = make(map[string]uint64)
	}
	assets := make(map[string]uint64, len(ac.Assets))
	for assetID, balance := range ac.Assets {
		assets[assetID] = balance
	}
	ac.Assets = assets
	s.Accounts[a] = ac
}

// ApplyTx applies a transaction to state.
func (s *State) ApplyTx(tx Tx) error {
	if err := tx.Verify(); err != nil {
		return err
	}
	from := s.Get(tx.From)
	to := s.Get(tx.To)

	if tx.Nonce != from.Nonce {
		return errors.New("bad nonce")
	}

	if len(tx.Metadata) > 0 {
		for _, kv := range tx.Metadata {
			if kv.Key == "type" && kv.Value == "inner_circle_purchase" && tx.Amount < INNER_CIRCLE_PRICE {
				return errors.New("insufficient amount for Inner Circle slot")
			}
		}
	}

	if tx.AssetID == "" || tx.AssetID == "syn" {
		totalAmount, err := safeAdd(tx.Amount, tx.Fee)
		if err != nil {
			return errors.New("amount overflow detected")
		}
		if from.Balance < totalAmount {
			return ErrInsufficientFunds
		}
		from.Balance -= totalAmount
	} else {
		if from.Assets[tx.AssetID] < tx.Amount {
			return errors.New("insufficient asset balance")
		}
		if from.Balance < tx.Fee {
			return ErrInsufficientFunds
		}
		from.Balance -= tx.Fee
		from.Assets[tx.AssetID] -= tx.Amount
	}

	from.Nonce += 1

	// Every transaction credits the recipient the sender named, in full.
	// There is no hidden redirect, commission skim, or special-cased
	// address in this path — what the sender specifies is what gets paid.
	nextRecipient := to
	if tx.AssetID == "" || tx.AssetID == "syn" {
		nextBalance, err := safeAdd(nextRecipient.Balance, tx.Amount)
		if err != nil {
			return errors.New("recipient balance overflow detected")
		}
		nextRecipient.Balance = nextBalance
	} else {
		nextAssetBalance, err := safeAdd(nextRecipient.Assets[tx.AssetID], tx.Amount)
		if err != nil {
			return errors.New("recipient asset balance overflow detected")
		}
		nextRecipient.Assets[tx.AssetID] = nextAssetBalance
	}

	if err := s.applyImmuneMetadata(tx); err != nil {
		return err
	}
	if err := s.applyBridgeMetadata(tx); err != nil {
		return err
	}

	s.Set(tx.To, nextRecipient)
	s.Set(tx.From, from)
	return nil
}

func (s *State) applyBridgeMetadata(tx Tx) error {
	txType := metadataValue(tx.Metadata, "type")
	if txType != "bridge_lock_native" && txType != "bridge_release_native" {
		return nil
	}
	assetID := tx.AssetID
	if assetID == "" {
		assetID = "syn"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.BridgeEvents == nil {
		s.BridgeEvents = make(map[string]BridgeRecord)
	}
	if s.ProcessedBridgeEvents == nil {
		s.ProcessedBridgeEvents = make(map[string]bool)
	}
	switch txType {
	case "bridge_lock_native":
		destinationChainID := metadataValue(tx.Metadata, "destination_chain_id")
		destinationRecipient := metadataValue(tx.Metadata, "destination_recipient")
		if destinationChainID == "" {
			return errors.New("missing destination_chain_id for bridge lock")
		}
		if destinationRecipient == "" {
			return errors.New("missing destination_recipient for bridge lock")
		}
		eventID := metadataValue(tx.Metadata, "bridge_event_id")
		if eventID == "" {
			eventID = tx.ID
		}
		if _, exists := s.BridgeEvents[eventID]; exists {
			return errors.New("duplicate bridge event")
		}
		s.BridgeEvents[eventID] = BridgeRecord{
			ID:                   eventID,
			Type:                 txType,
			Address:              tx.From,
			Amount:               tx.Amount,
			AssetID:              assetID,
			DestinationChainID:   destinationChainID,
			DestinationRecipient: destinationRecipient,
			TxID:                 tx.ID,
			Timestamp:            tx.Timestamp,
		}
		s.LastBridgeEventID = eventID
	case "bridge_release_native":
		sourceChainID := metadataValue(tx.Metadata, "source_chain_id")
		sourceEventID := metadataValue(tx.Metadata, "source_event_id")
		if sourceChainID == "" {
			return errors.New("missing source_chain_id for bridge release")
		}
		if sourceEventID == "" {
			return errors.New("missing source_event_id for bridge release")
		}
		if err := s.verifyBridgeReleaseProofLocked(tx, sourceChainID, sourceEventID, assetID); err != nil {
			return err
		}
		messageID := bridgeMessageID(sourceChainID, sourceEventID, string(tx.To), assetID, tx.Amount)
		if s.ProcessedBridgeEvents[messageID] {
			return errors.New("bridge source event already processed")
		}
		s.ProcessedBridgeEvents[messageID] = true
		s.BridgeEvents[messageID] = BridgeRecord{
			ID:            messageID,
			Type:          txType,
			Address:       tx.To,
			Amount:        tx.Amount,
			AssetID:       assetID,
			SourceChainID: sourceChainID,
			SourceEventID: sourceEventID,
			TxID:          tx.ID,
			Timestamp:     tx.Timestamp,
		}
		s.LastBridgeEventID = messageID
	}
	return nil
}

func (s *State) verifyBridgeReleaseProofLocked(tx Tx, sourceChainID, sourceEventID, assetID string) error {
	if len(s.BridgeValidators) == 0 {
		return nil
	}
	raw := metadataValue(tx.Metadata, "validator_signatures")
	if raw == "" {
		return errors.New("missing validator_signatures for bridge release")
	}
	var signatures []BridgeValidatorSignature
	if err := json.Unmarshal([]byte(raw), &signatures); err != nil {
		return errors.New("invalid validator_signatures")
	}
	required := s.BridgeQuorum
	if required == 0 {
		required = uint64((2*len(s.BridgeValidators) + 2) / 3)
	}
	if required == 0 || required > uint64(len(s.BridgeValidators)) {
		return errors.New("invalid bridge quorum")
	}
	message := BridgeReleaseSigningMessage(sourceChainID, sourceEventID, string(tx.To), assetID, tx.Amount)
	seen := map[string]bool{}
	var valid uint64
	for _, item := range signatures {
		if item.ValidatorID == "" || seen[item.ValidatorID] {
			continue
		}
		pubHex, ok := s.BridgeValidators[item.ValidatorID]
		if !ok {
			continue
		}
		pub, err := hex.DecodeString(strings.TrimPrefix(pubHex, "0x"))
		if err != nil || len(pub) != ed25519.PublicKeySize {
			return errors.New("invalid bridge validator public key")
		}
		sig, err := hex.DecodeString(strings.TrimPrefix(item.Signature, "0x"))
		if err != nil || len(sig) != ed25519.SignatureSize {
			continue
		}
		if ed25519.Verify(ed25519.PublicKey(pub), message, sig) {
			seen[item.ValidatorID] = true
			valid++
		}
	}
	if valid < required {
		return fmt.Errorf("bridge validator quorum not met: got %d required %d", valid, required)
	}
	return nil
}

func (s *State) applyImmuneMetadata(tx Tx) error {
	txType := metadataValue(tx.Metadata, "type")
	s.mu.Lock()
	defer s.mu.Unlock()
	switch txType {
	case "immune_node_activate":
		hardwareHash := metadataValue(tx.Metadata, "hardware_hash")
		if hardwareHash == "" {
			return errors.New("missing hardware_hash for immune node activation")
		}
		record := s.ImmuneNodes[tx.From]
		if record.Address == "" {
			record.Address = tx.From
			record.ActivatedAt = tx.Timestamp
			record.CryptographicMode = "absolute_silence"
		}
		record.HardwareHash = hardwareHash
		record.OptInLocalOnly = metadataValue(tx.Metadata, "scope") != "external"
		s.ImmuneNodes[tx.From] = record
	case "sovereign_noise_proof":
		proofHash := metadataValue(tx.Metadata, "proof_hash")
		if proofHash == "" {
			return errors.New("missing proof_hash for sovereign noise proof")
		}
		scope := metadataValue(tx.Metadata, "scope")
		if scope == "" {
			scope = "local_opt_in"
		}
		if scope != "local_opt_in" && scope != "local_browser" && scope != "testnet" {
			return errors.New("sovereign noise proofs must be opt-in local/testnet scope")
		}
		record := s.ImmuneNodes[tx.From]
		if record.Address == "" {
			return errors.New("immune node must be activated before submitting proofs")
		}
		record.SovereignProofs++
		record.LastProofAt = tx.Timestamp
		s.ImmuneNodes[tx.From] = record
		proof := SovereignProofRecord{
			ID:            tx.ID,
			Address:       tx.From,
			ProofHash:     proofHash,
			NoiseClass:    metadataValue(tx.Metadata, "noise_class"),
			DeclaredScope: scope,
			Timestamp:     tx.Timestamp,
		}
		s.SovereignProofs[tx.ID] = proof
		s.LastSovereignProofID = tx.ID
	}
	return nil
}

func bridgeMessageID(sourceChainID, sourceEventID, recipient, assetID string, amount uint64) string {
	payload := fmt.Sprintf("SYNTHOS_BRIDGE_NATIVE_RELEASE_V1|%s|%s|%s|%s|%d", sourceChainID, sourceEventID, strings.ToLower(recipient), assetID, amount)
	sum := sha256.Sum256([]byte(payload))
	return "0x" + hex.EncodeToString(sum[:])
}

func BridgeReleaseSigningMessage(sourceChainID, sourceEventID, recipient, assetID string, amount uint64) []byte {
	if assetID == "" {
		assetID = "syn"
	}
	return []byte(fmt.Sprintf(
		"SYNTHOS_BRIDGE_RELEASE_V1\nsource_chain_id=%s\nsource_event_id=%s\nrecipient=%s\nasset_id=%s\namount=%d",
		sourceChainID,
		sourceEventID,
		strings.ToLower(recipient),
		assetID,
		amount,
	))
}

func (s *State) BridgeStatus() BridgeStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var locks int
	var releases int
	for _, event := range s.BridgeEvents {
		switch event.Type {
		case "bridge_lock_native":
			locks++
		case "bridge_release_native":
			releases++
		}
	}
	return BridgeStats{
		NativeLocks:       locks,
		NativeReleases:    releases,
		ProcessedMessages: len(s.ProcessedBridgeEvents),
		LastBridgeEventID: s.LastBridgeEventID,
	}
}

func (s *State) BridgeEventsSnapshot() []BridgeRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := make([]BridgeRecord, 0, len(s.BridgeEvents))
	for _, event := range s.BridgeEvents {
		events = append(events, event)
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp == events[j].Timestamp {
			return events[i].ID < events[j].ID
		}
		return events[i].Timestamp > events[j].Timestamp
	})
	return events
}

func (s *State) EnsureImmuneNode(address Address, hardwareHash string, activatedAt int64) bool {
	if address == "" || hardwareHash == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.ImmuneNodes[address]; exists {
		return false
	}
	s.ImmuneNodes[address] = ImmuneNodeRecord{
		Address:           address,
		HardwareHash:      hardwareHash,
		ActivatedAt:       activatedAt,
		OptInLocalOnly:    true,
		CryptographicMode: "absolute_silence",
	}
	return true
}

func metadataValue(items []KeyValuePair, key string) string {
	for _, kv := range items {
		if kv.Key == key {
			return kv.Value
		}
	}
	return ""
}

func (s *State) ImmuneStatus() ImmuneStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var last string
	if s.LastSovereignProofID != "" {
		last = s.SovereignProofs[s.LastSovereignProofID].ProofHash
	}
	return ImmuneStats{
		ActiveImmuneNodes:    len(s.ImmuneNodes),
		SovereignProofs:      uint64(len(s.SovereignProofs)),
		LastProofHash:        last,
		CryptographicSilence: true,
		InboundPortsRequired: 0,
	}
}

// Slash executes the DMAS immune penalty (Auto-Slashing)
func (s *State) Slash(a Address, penaltyPercent int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	acc, ok := s.Accounts[a]
	if !ok {
		return
	}
	fine := (acc.Balance * uint64(penaltyPercent)) / 100
	if acc.Balance >= fine {
		acc.Balance -= fine
	} else {
		acc.Balance = 0
	}
	s.Accounts[a] = acc
}

func (s *State) GetNextNonce(a Address) uint64 {
	acc := s.Get(a)
	return acc.Nonce
}

func (s *State) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	addrs := make([]string, 0, len(s.Accounts))
	for a := range s.Accounts {
		addrs = append(addrs, string(a))
	}
	sort.Strings(addrs) // Canonical order

	leaves := make([][]byte, 0, len(addrs))
	for _, a := range addrs {
		acc := s.Accounts[Address(a)]
		leaves = append(leaves, acc.LeafHash(Address(a)))
	}
	immunePayload := struct {
		ImmuneNodes           map[Address]ImmuneNodeRecord    `json:"immune_nodes"`
		SovereignProofs       map[string]SovereignProofRecord `json:"sovereign_proofs"`
		LastSovereignProofID  string                          `json:"last_sovereign_proof_id"`
		BridgeEvents          map[string]BridgeRecord         `json:"bridge_events"`
		ProcessedBridgeEvents map[string]bool                 `json:"processed_bridge_events"`
		BridgeValidators      map[string]string               `json:"bridge_validators"`
		BridgeQuorum          uint64                          `json:"bridge_quorum"`
		LastBridgeEventID     string                          `json:"last_bridge_event_id"`
	}{
		ImmuneNodes:           s.ImmuneNodes,
		SovereignProofs:       s.SovereignProofs,
		LastSovereignProofID:  s.LastSovereignProofID,
		BridgeEvents:          s.BridgeEvents,
		ProcessedBridgeEvents: s.ProcessedBridgeEvents,
		BridgeValidators:      s.BridgeValidators,
		BridgeQuorum:          s.BridgeQuorum,
		LastBridgeEventID:     s.LastBridgeEventID,
	}
	immuneData, _ := json.Marshal(immunePayload)
	immuneHash := sha256.Sum256(immuneData)
	leaves = append(leaves, immuneHash[:])

	return "0x" + hex.EncodeToString(buildMerkleRoot(leaves))
}

func buildMerkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		empty := sha256.Sum256([]byte{})
		return empty[:]
	}
	for len(leaves) > 1 {
		var next [][]byte
		for i := 0; i < len(leaves); i += 2 {
			if i+1 == len(leaves) {
				next = append(next, leaves[i])
			} else {
				concat := append(leaves[i], leaves[i+1]...)
				h := sha256.Sum256(concat)
				next = append(next, h[:])
			}
		}
		leaves = next
	}
	return leaves[0]
}

func (s *State) Clone() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := NewState()
	for k, v := range s.Accounts {
		assets := make(map[string]uint64)
		for ak, av := range v.Assets {
			assets[ak] = av
		}
		v.Assets = assets
		out.Accounts[k] = v
	}
	for k, v := range s.ImmuneNodes {
		out.ImmuneNodes[k] = v
	}
	for k, v := range s.SovereignProofs {
		out.SovereignProofs[k] = v
	}
	for k, v := range s.BridgeEvents {
		out.BridgeEvents[k] = v
	}
	for k, v := range s.ProcessedBridgeEvents {
		out.ProcessedBridgeEvents[k] = v
	}
	for k, v := range s.BridgeValidators {
		out.BridgeValidators[k] = v
	}
	out.BridgeQuorum = s.BridgeQuorum
	out.LastSovereignProofID = s.LastSovereignProofID
	out.LastBridgeEventID = s.LastBridgeEventID
	out.TotalSupply = s.TotalSupply
	return out
}

// -----------------------------
// Safe Math
// -----------------------------
func safeAdd(a, b uint64) (uint64, error) {
	if math.MaxUint64-a < b {
		return 0, fmt.Errorf("uint64 overflow")
	}
	return a + b, nil
}

func safeSub(a, b uint64) (uint64, error) {
	if a < b {
		return 0, ErrInsufficientFunds
	}
	return a - b, nil
}

// -----------------------------
// Immune System (Timeless)
// -----------------------------
type ThreatProfile struct {
	ReporterAddress Address
	TargetAddress   Address
	Reason          string
}

type ImmuneSystem struct {
	ActiveThreats  map[Address][]ThreatProfile
	SlashThreshold uint64
	mu             sync.Mutex
	GetStake       func(addr Address) uint64
	ExecuteSlash   func(target Address)
}

// NewImmuneSystem constructs the slashing/threat-tracking system. Every
// address is subject to the same rules — there is no anchor address with
// built-in slash immunity.
func NewImmuneSystem(getStake func(Address) uint64, executeSlash func(Address)) *ImmuneSystem {
	return &ImmuneSystem{
		ActiveThreats:  make(map[Address][]ThreatProfile),
		SlashThreshold: 1_000_000,
		GetStake:       getStake,
		ExecuteSlash:   executeSlash,
	}
}

func (is *ImmuneSystem) RecordThreat(p ThreatProfile) bool {
	is.mu.Lock()
	defer is.mu.Unlock()

	threats := is.ActiveThreats[p.TargetAddress]
	if len(threats) >= MAX_THREATS_PER_ADDR {
		threats = threats[1:] // prune oldest
	}
	threats = append(threats, p)
	is.ActiveThreats[p.TargetAddress] = threats

	var totalStake uint64
	for _, t := range threats {
		stake := is.GetStake(t.ReporterAddress)
		if math.MaxUint64-totalStake < stake {
			totalStake = math.MaxUint64
			break
		}
		totalStake += stake
	}

	if totalStake >= is.SlashThreshold {
		is.ExecuteSlash(p.TargetAddress)
		delete(is.ActiveThreats, p.TargetAddress)
		return true
	}

	return false
}
