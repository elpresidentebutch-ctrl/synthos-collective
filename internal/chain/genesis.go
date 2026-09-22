package chain

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// Genesis defines initial state distribution for the SYNTHOS L1.
type Genesis struct {
	ChainID   string             `json:"chain_id"`
	TxChainID uint64             `json:"tx_chain_id,omitempty"`
	Alloc     map[Address]uint64 `json:"alloc"`
	Metadata  map[string]any     `json:"metadata,omitempty"`
}

var ErrBadGenesis = errors.New("bad genesis")

func (g Genesis) Validate() error {
	if g.ChainID == "" || len(g.Alloc) == 0 {
		return ErrBadGenesis
	}
	// Deliberately not validating address *format* here (e.g. requiring
	// "0x" + 40 hex chars, matching AddressFromPublicKey's output): both the
	// example genesis config and this package's own test suite use
	// non-hex placeholder addresses (config/genesis.example.json's
	// "agent-0", "0xgenesis" throughout the _test.go files), and Address is
	// treated as an opaque string key everywhere else in this package. An
	// empty address is never valid anywhere, though.
	for addr := range g.Alloc {
		if addr == "" {
			return fmt.Errorf("%w: alloc has an empty address", ErrBadGenesis)
		}
	}
	if g.TxChainID == 0 {
		// Legacy configurations use transaction chain ID 1.
		return nil
	}
	return nil
}

func (g Genesis) TransactionChainID() uint64 {
	if g.TxChainID == 0 {
		return 1
	}
	return g.TxChainID
}

func (g Genesis) ValidateTransactionChainID(id uint64) error {
	if id != g.TransactionChainID() {
		return fmt.Errorf("wrong transaction chain ID: got %d, want %d", id, g.TransactionChainID())
	}
	return nil
}

func (g Genesis) ToState() (*State, error) {
	if err := g.Validate(); err != nil {
		return nil, err
	}
	s := NewState()
	for addr, bal := range g.Alloc {
		s.Set(addr, Account{Balance: bal, Nonce: 0})
	}
	if validators, ok := g.Metadata["bridge_validators"]; ok {
		parsed, err := parseBridgeValidators(validators)
		if err != nil {
			return nil, err
		}
		s.BridgeValidators = parsed
	}
	if quorum, ok := g.Metadata["bridge_quorum"]; ok {
		parsed, err := parseMetadataUint64(quorum)
		if err != nil {
			return nil, fmt.Errorf("invalid bridge_quorum: %w", err)
		}
		s.BridgeQuorum = parsed
	}
	// treasury_address and citizen_reward_rate_bps_per_year configure
	// Citizen staking rewards (see citizen.go). Both default to their zero
	// value, which is exactly what ClaimCitizenRewards treats as "rewards
	// not configured for this deployment" -- so a genesis file that omits
	// them simply never pays Citizen rewards, the same way omitting
	// founder_address/treasury_address leaves Governor unconfigured.
	if treasury, ok := g.Metadata["treasury_address"]; ok {
		addr, _ := treasury.(string)
		if addr == "" {
			return nil, fmt.Errorf("treasury_address must be a non-empty string")
		}
		s.TreasuryAddress = Address(addr)
	}
	if rate, ok := g.Metadata["citizen_reward_rate_bps_per_year"]; ok {
		parsed, err := parseMetadataUint64(rate)
		if err != nil {
			return nil, fmt.Errorf("invalid citizen_reward_rate_bps_per_year: %w", err)
		}
		s.CitizenRewardRateBpsPerYear = parsed
	}
	// founder_address configures who may create governance proposals (see
	// governance.go, State.GovernanceFounder). Same default-empty,
	// deployment-optional treatment as treasury_address above. An operator
	// env var can still override this after the fact -- see
	// cmd/synthosd/main.go's initGovernance, which re-syncs
	// State.GovernanceFounder the same way it already re-syncs
	// State.TreasuryAddress when SYNTHOS_FOUNDER_ADDRESS/
	// SYNTHOS_TREASURY_ADDRESS are set.
	if founder, ok := g.Metadata["founder_address"]; ok {
		addr, _ := founder.(string)
		if addr == "" {
			return nil, fmt.Errorf("founder_address must be a non-empty string")
		}
		s.GovernanceFounder = Address(addr)
	}
	return s, nil
}

func (g Genesis) Bytes() ([]byte, error) {
	return json.MarshalIndent(g, "", "  ")
}

// PinnedGenesisStateRoot and PinnedGenesisHash let a genesis.json declare the
// exact state_root/hash its genesis block must carry, instead of NewChain
// recomputing them fresh from ToState()+State.Root()+Block.ComputeHash().
//
// This matters because that computation depends on the exact hashing formula
// in State.Root()/buildMerkleRoot (core.go), and that formula has changed
// over time as real bugs were fixed (e.g. the commit that stopped folding
// self-attested ImmuneNodes/SovereignProofs into Root(), and the Merkle
// odd-leaf fix -- both correct, and both silently change what any *fresh*
// computation of Root() returns for the same account data). A node that has
// been running continuously since before one of those fixes never
// recomputes its own genesis block -- on every restart it just reloads
// whatever it already persisted to disk -- so its real, live genesis stays
// pinned to whichever formula was in effect the very first time it ever
// booted, permanently, regardless of later code changes. A brand-new or
// freshly-resynced node building NewChain(genesis) from that exact same
// genesis.json would otherwise compute a *different* genesis block under
// today's corrected formula, and so could never agree with the long-running
// node on block 0 -- the one block every other block's hash chain is
// ultimately anchored to via ParentHash -- no matter what trust, auth, or
// state-root-enforcement config is layered on top.
//
// Declaring the known-correct, already-live values here makes every node --
// new, resynced, or long-running -- agree on the exact same genesis block by
// construction, permanently, regardless of any future change to how
// State.Root() or block hashing work. Omit both (as
// config/genesis.example.json and every test fixture do) to keep computing
// them fresh, which is the only sane default for a genesis that has never
// actually been deployed anywhere yet.
func (g Genesis) PinnedGenesisStateRoot() (string, bool) {
	v, ok := g.Metadata["pinned_genesis_state_root"].(string)
	return v, ok && v != ""
}

func (g Genesis) PinnedGenesisHash() (string, bool) {
	v, ok := g.Metadata["pinned_genesis_hash"].(string)
	return v, ok && v != ""
}

func parseBridgeValidators(raw any) (map[string]string, error) {
	out := map[string]string{}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("bridge_validators must be an array")
	}
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("bridge validator must be an object")
		}
		id, _ := obj["id"].(string)
		if id == "" {
			id, _ = obj["validator_id"].(string)
		}
		pub, _ := obj["public_key"].(string)
		if id == "" || pub == "" {
			return nil, fmt.Errorf("bridge validator requires id and public_key")
		}
		out[id] = pub
	}
	return out, nil
}

func parseMetadataUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case float64:
		if v < 0 || v != float64(uint64(v)) {
			return 0, fmt.Errorf("not a uint64")
		}
		return uint64(v), nil
	case string:
		return strconv.ParseUint(v, 10, 64)
	default:
		return 0, fmt.Errorf("unsupported type")
	}
}
