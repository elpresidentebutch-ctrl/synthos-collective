package kvstate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"synthos-collective/internal/chain"
)

const (
	accKeyPrefix = "acc:"
	sysKeyPrefix = "sys:"

	keyImmunePayload        = "sys:immune_payload"
	keyValidators           = "sys:validators"
	keyValidatorSetSnapshot = "sys:validator_set_snapshot"
	keyFeeEpoch             = "sys:fee_epoch"
	keyFeeV3BaseFee         = "sys:fee_v3_base_fee"
)

// StateStore defines the storage interface for chain state.
// It matches the core read/write methods of *chain.State while allowing
// underlying persistence via arbitrary key-value backends.
type StateStore interface {
	Get(a chain.Address) chain.Account
	Set(a chain.Address, ac chain.Account)
	Delete(a chain.Address)
	TotalStake() uint64
	AccountCount() int
	Root() string
	SyncFromState(st *chain.State) error
	ExportToState() *chain.State
}

// KVStateStore implements StateStore backed by a KVStore.
type KVStateStore struct {
	mu sync.RWMutex
	kv KVStore
}

// NewKVStateStore creates a StateStore backed by the provided KVStore.
func NewKVStateStore(kv KVStore) *KVStateStore {
	if kv == nil {
		kv = NewMemKVStore()
	}
	return &KVStateStore{
		kv: kv,
	}
}

// accountKey returns the KV key for an account address.
func accountKey(a chain.Address) []byte {
	return []byte(accKeyPrefix + string(a))
}

// addressFromKey parses the address from an account KV key.
func addressFromKey(k []byte) chain.Address {
	return chain.Address(strings.TrimPrefix(string(k), accKeyPrefix))
}

func (s *KVStateStore) Get(a chain.Address) chain.Account {
	s.mu.RLock()
	defer s.mu.RUnlock()

	raw, err := s.kv.Get(accountKey(a))
	if err != nil || len(raw) == 0 {
		return chain.Account{Assets: make(map[string]uint64)}
	}
	var acc chain.Account
	if err := json.Unmarshal(raw, &acc); err != nil {
		return chain.Account{Assets: make(map[string]uint64)}
	}
	if acc.Assets == nil {
		acc.Assets = make(map[string]uint64)
	}
	return acc
}

func (s *KVStateStore) Set(a chain.Address, ac chain.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ac.Assets == nil {
		ac.Assets = make(map[string]uint64)
	}
	raw, _ := json.Marshal(ac)
	_ = s.kv.Set(accountKey(a), raw)
}

func (s *KVStateStore) Delete(a chain.Address) {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = s.kv.Delete(accountKey(a))
}

func (s *KVStateStore) TotalStake() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var total uint64
	_ = s.kv.Scan([]byte(accKeyPrefix), func(key, value []byte) error {
		var acc chain.Account
		if err := json.Unmarshal(value, &acc); err == nil {
			total += acc.Balance
		}
		return nil
	})
	return total
}

func (s *KVStateStore) AccountCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	_ = s.kv.Scan([]byte(accKeyPrefix), func(key, value []byte) error {
		count++
		return nil
	})
	return count
}

// SyncFromState populates this KVStateStore from an existing *chain.State.
func (s *KVStateStore) SyncFromState(st *chain.State) error {
	if st == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	batch := s.kv.NewBatch()

	// 1. Write accounts
	for addr, acc := range st.Accounts {
		raw, err := json.Marshal(acc)
		if err != nil {
			return err
		}
		batch.Set(accountKey(addr), raw)
	}

	// 2. Write immune / bridge payload
	immunePayload := struct {
		BridgeEvents          map[string]chain.BridgeRecord `json:"bridge_events"`
		ProcessedBridgeEvents map[string]bool               `json:"processed_bridge_events"`
		BridgeValidators      map[string]string             `json:"bridge_validators"`
		BridgeQuorum          uint64                        `json:"bridge_quorum"`
		LastBridgeEventID     string                        `json:"last_bridge_event_id"`
	}{
		BridgeEvents:          st.BridgeEvents,
		ProcessedBridgeEvents: st.ProcessedBridgeEvents,
		BridgeValidators:      st.BridgeValidators,
		BridgeQuorum:          st.BridgeQuorum,
		LastBridgeEventID:     st.LastBridgeEventID,
	}
	immuneData, _ := json.Marshal(immunePayload)
	batch.Set([]byte(keyImmunePayload), immuneData)

	// 3. Write validators (if non-empty)
	if len(st.Validators) > 0 {
		valData, _ := json.Marshal(st.Validators)
		batch.Set([]byte(keyValidators), valData)
	} else {
		batch.Delete([]byte(keyValidators))
	}

	// 4. Write validator set snapshot (if non-empty)
	if len(st.ValidatorSetSnapshot) > 0 {
		snapData, _ := json.Marshal(st.ValidatorSetSnapshot)
		batch.Set([]byte(keyValidatorSetSnapshot), snapData)
	} else {
		batch.Delete([]byte(keyValidatorSetSnapshot))
	}

	// 5. Write fee epoch state (if non-zero/non-empty)
	if st.FeeEpochTreasuryIncome > 0 || st.FeeEpochTotalPower > 0 || len(st.FeeEpochSignedPower) > 0 {
		feePayload := struct {
			TreasuryIncome uint64                   `json:"treasury_income"`
			TotalPower     uint64                   `json:"total_power"`
			SignedPower    map[chain.Address]uint64 `json:"signed_power"`
		}{
			TreasuryIncome: st.FeeEpochTreasuryIncome,
			TotalPower:     st.FeeEpochTotalPower,
			SignedPower:    st.FeeEpochSignedPower,
		}
		feeData, _ := json.Marshal(feePayload)
		batch.Set([]byte(keyFeeEpoch), feeData)
	} else {
		batch.Delete([]byte(keyFeeEpoch))
	}

	// 6. Write Fee Model V3 Base Fee (if non-zero)
	if st.FeeModelV3BaseFee > 0 {
		batch.Set([]byte(keyFeeV3BaseFee), []byte(fmt.Sprintf("%d", st.FeeModelV3BaseFee)))
	} else {
		batch.Delete([]byte(keyFeeV3BaseFee))
	}

	return batch.Commit()
}

// ExportToState exports the current KVStateStore into a new *chain.State.
func (s *KVStateStore) ExportToState() *chain.State {
	s.mu.RLock()
	defer s.mu.RUnlock()

	st := chain.NewState()

	// Load accounts
	_ = s.kv.Scan([]byte(accKeyPrefix), func(key, value []byte) error {
		addr := addressFromKey(key)
		var acc chain.Account
		if err := json.Unmarshal(value, &acc); err == nil {
			st.Accounts[addr] = acc
		}
		return nil
	})

	// Load immune / bridge payload
	if raw, err := s.kv.Get([]byte(keyImmunePayload)); err == nil && len(raw) > 0 {
		var immunePayload struct {
			BridgeEvents          map[string]chain.BridgeRecord `json:"bridge_events"`
			ProcessedBridgeEvents map[string]bool               `json:"processed_bridge_events"`
			BridgeValidators      map[string]string             `json:"bridge_validators"`
			BridgeQuorum          uint64                        `json:"bridge_quorum"`
			LastBridgeEventID     string                        `json:"last_bridge_event_id"`
		}
		if err := json.Unmarshal(raw, &immunePayload); err == nil {
			st.BridgeEvents = immunePayload.BridgeEvents
			st.ProcessedBridgeEvents = immunePayload.ProcessedBridgeEvents
			st.BridgeValidators = immunePayload.BridgeValidators
			st.BridgeQuorum = immunePayload.BridgeQuorum
			st.LastBridgeEventID = immunePayload.LastBridgeEventID
		}
	}

	// Load validators
	if raw, err := s.kv.Get([]byte(keyValidators)); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &st.Validators)
	}

	// Load validator snapshot
	if raw, err := s.kv.Get([]byte(keyValidatorSetSnapshot)); err == nil && len(raw) > 0 {
		_ = json.Unmarshal(raw, &st.ValidatorSetSnapshot)
	}

	// Load fee epoch
	if raw, err := s.kv.Get([]byte(keyFeeEpoch)); err == nil && len(raw) > 0 {
		var feePayload struct {
			TreasuryIncome uint64                   `json:"treasury_income"`
			TotalPower     uint64                   `json:"total_power"`
			SignedPower    map[chain.Address]uint64 `json:"signed_power"`
		}
		if err := json.Unmarshal(raw, &feePayload); err == nil {
			st.FeeEpochTreasuryIncome = feePayload.TreasuryIncome
			st.FeeEpochTotalPower = feePayload.TotalPower
			st.FeeEpochSignedPower = feePayload.SignedPower
		}
	}

	// Load fee v3 base fee
	if raw, err := s.kv.Get([]byte(keyFeeV3BaseFee)); err == nil && len(raw) > 0 {
		var fee uint64
		if _, err := fmt.Sscanf(string(raw), "%d", &fee); err == nil {
			st.FeeModelV3BaseFee = fee
		}
	}

	return st
}

// Root computes the Merkle root across all accounts and consensus metadata
// using the identical canonical sorting and duplicate-and-hash algorithm as *chain.State.Root().
func (s *KVStateStore) Root() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 1. Gather account addresses in canonical order
	addrs := make([]string, 0)
	accounts := make(map[string]chain.Account)

	_ = s.kv.Scan([]byte(accKeyPrefix), func(key, value []byte) error {
		addrStr := strings.TrimPrefix(string(key), accKeyPrefix)
		var acc chain.Account
		if err := json.Unmarshal(value, &acc); err == nil {
			addrs = append(addrs, addrStr)
			accounts[addrStr] = acc
		}
		return nil
	})

	sort.Strings(addrs) // Canonical order

	leaves := make([][]byte, 0, len(addrs)+4)
	for _, a := range addrs {
		acc := accounts[a]
		addr := chain.Address(a)
		leaf := chain.AccountLeaf{
			Address: addr,
			Balance: acc.Balance,
			Assets:  acc.Assets,
			Nonce:   acc.Nonce,
		}
		data, _ := json.Marshal(leaf)
		hash := sha256.Sum256(data)
		leafHash := make([]byte, len(hash))
		copy(leafHash, hash[:])
		leaves = append(leaves, leafHash)
	}

	// 2. Immune / bridge payload
	var immuneData []byte
	if raw, err := s.kv.Get([]byte(keyImmunePayload)); err == nil && len(raw) > 0 {
		immuneData = raw
	} else {
		immunePayload := struct {
			BridgeEvents          map[string]chain.BridgeRecord `json:"bridge_events"`
			ProcessedBridgeEvents map[string]bool               `json:"processed_bridge_events"`
			BridgeValidators      map[string]string             `json:"bridge_validators"`
			BridgeQuorum          uint64                        `json:"bridge_quorum"`
			LastBridgeEventID     string                        `json:"last_bridge_event_id"`
		}{
			BridgeEvents:          make(map[string]chain.BridgeRecord),
			ProcessedBridgeEvents: make(map[string]bool),
			BridgeValidators:      make(map[string]string),
		}
		immuneData, _ = json.Marshal(immunePayload)
	}
	immuneHash := sha256.Sum256(immuneData)
	leaves = append(leaves, immuneHash[:])

	// 3. Validators (if non-empty)
	if raw, err := s.kv.Get([]byte(keyValidators)); err == nil && len(raw) > 0 {
		valHash := sha256.Sum256(append([]byte("synthos/validators/v1:"), raw...))
		leaves = append(leaves, valHash[:])
	}

	// 4. Validator set snapshot (if non-empty)
	if raw, err := s.kv.Get([]byte(keyValidatorSetSnapshot)); err == nil && len(raw) > 0 {
		snapHash := sha256.Sum256(append([]byte("synthos/validator-set-snapshot/v1:"), raw...))
		leaves = append(leaves, snapHash[:])
	}

	// 5. Fee epoch state (if non-zero/non-empty)
	if raw, err := s.kv.Get([]byte(keyFeeEpoch)); err == nil && len(raw) > 0 {
		feeHash := sha256.Sum256(append([]byte("synthos/fee-epoch/v1:"), raw...))
		leaves = append(leaves, feeHash[:])
	}

	// 6. Fee Model V3 Base Fee (if non-zero)
	if raw, err := s.kv.Get([]byte(keyFeeV3BaseFee)); err == nil && len(raw) > 0 {
		var baseFee uint64
		if _, err := fmt.Sscanf(string(raw), "%d", &baseFee); err == nil && baseFee > 0 {
			feeData := []byte(fmt.Sprintf("synthos/fee-v3-base-fee:%d", baseFee))
			feeHash := sha256.Sum256(feeData)
			leaves = append(leaves, feeHash[:])
		}
	}

	return "0x" + hex.EncodeToString(buildMerkleRoot(leaves))
}

// buildMerkleRoot computes the Merkle root over leaves using the canonical
// odd-leaf duplicate-and-hash rule.
func buildMerkleRoot(leaves [][]byte) []byte {
	if len(leaves) == 0 {
		empty := sha256.Sum256([]byte{})
		return empty[:]
	}
	level := make([][]byte, len(leaves))
	copy(level, leaves)

	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			buf := make([]byte, 0, len(left)+len(right))
			buf = append(buf, left...)
			buf = append(buf, right...)
			sum := sha256.Sum256(buf)
			node := make([]byte, len(sum))
			copy(node, sum[:])
			next = append(next, node)
		}
		level = next
	}
	return level[0]
}
