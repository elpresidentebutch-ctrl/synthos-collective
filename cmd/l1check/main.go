package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/storage"
)

const (
	chainID         = "synthos-l1-local-proof"
	validatorCount  = 4
	requiredVotes   = 3
	genesisBalance  = 1_000_000
	transferAmount  = 12_345
	transferFee     = 10
	proposerID      = "validator-0"
	proposerAddress = "0x76616c696461746f722d30000000000000000000"
)

type validatorProof struct {
	ID          string `json:"id"`
	Height      uint64 `json:"height"`
	Tip         string `json:"tip"`
	StateRoot   string `json:"state_root"`
	FromBalance uint64 `json:"from_balance"`
	ToBalance   uint64 `json:"to_balance"`
	Finalized   bool   `json:"finalized"`
	VotesFor    int    `json:"votes_for"`
	Required    int    `json:"required"`
	Reloaded    bool   `json:"reloaded"`
}

type proofSummary struct {
	OK               bool             `json:"ok"`
	ChainID          string           `json:"chain_id"`
	Validators       int              `json:"validators"`
	RequiredFinality int              `json:"required_finality"`
	SubmittedTx      string           `json:"submitted_tx"`
	FinalizedBlock   string           `json:"finalized_block"`
	ElapsedMillis    int64            `json:"elapsed_ms"`
	Checks           []string         `json:"checks"`
	ValidatorProofs  []validatorProof `json:"validator_proofs"`
}

func main() {
	started := time.Now()
	if err := run(started); err != nil {
		writeFailure(started, err)
		os.Exit(1)
	}
}

func run(started time.Time) error {
	workDir, err := os.MkdirTemp("", "synthos-l1check-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	sender, err := synthoscrypto.NewKeyPair()
	if err != nil {
		return err
	}
	recipient, err := synthoscrypto.NewKeyPair()
	if err != nil {
		return err
	}

	from := chain.AddressFromPublicKey(sender.Public)
	to := chain.AddressFromPublicKey(recipient.Public)
	genesis := chain.Genesis{
		ChainID: chainID,
		Alloc: map[chain.Address]uint64{
			from: genesisBalance,
		},
		Metadata: map[string]any{
			"symbol":   "SYN",
			"decimals": 0,
			"purpose":  "local L1 finality proof",
		},
	}

	chains := make([]*chain.Chain, 0, validatorCount)
	engines := make([]*consensus.Engine, 0, validatorCount)
	stores := make([]*storage.Store, 0, validatorCount)

	for i := 0; i < validatorCount; i++ {
		c, err := chain.NewChain(genesis)
		if err != nil {
			return err
		}
		chains = append(chains, c)
		engines = append(engines, consensus.NewEngine(validatorCount))
		store, err := storage.New(filepath.Join(workDir, fmt.Sprintf("validator-%d", i)))
		if err != nil {
			return err
		}
		stores = append(stores, store)
	}

	tx := chain.Tx{
		ChainID:   1,
		From:      from,
		To:        to,
		Amount:    transferAmount,
		Fee:       transferFee,
		Nonce:     chains[0].State.GetNextNonce(from),
		PublicKey: synthoscrypto.PublicKeyHex(sender.Public),
	}
	if err := tx.Sign(sender.Private); err != nil {
		return err
	}
	for _, c := range chains {
		if err := c.SubmitTx(tx); err != nil {
			return fmt.Errorf("submit tx: %w", err)
		}
	}

	proposal, err := chains[0].BuildBlock(proposerID, "local-proof", 100)
	if err != nil {
		return err
	}
	for _, eng := range engines {
		eng.OnProposal(proposal)
	}

	for i := 0; i < requiredVotes; i++ {
		vote := consensus.BlockVote{
			Height:    proposal.Header.Height,
			BlockHash: proposal.Hash,
			VoterID:   fmt.Sprintf("validator-%d", i),
			Vote:      1,
		}
		for _, eng := range engines {
			_, _, _, _ = eng.OnVote(vote)
		}
	}

	for i, c := range chains {
		finalized, votesFor, required, ok := engines[i].FinalityStatus(proposal.Hash)
		if !ok || !finalized || votesFor < required {
			return fmt.Errorf("validator-%d did not reach finality: ok=%v finalized=%v votes=%d required=%d", i, ok, finalized, votesFor, required)
		}
		if err := c.FinalizeBlock(proposal); err != nil {
			return fmt.Errorf("validator-%d finalize: %w", i, err)
		}
		if err := stores[i].Save(c); err != nil {
			return fmt.Errorf("validator-%d save: %w", i, err)
		}
	}

	height := chains[0].Height()
	tip := chains[0].Tip().Hash
	root := chains[0].State.Root()
	fromBalance := chains[0].State.Get(from).Balance
	toBalance := chains[0].State.Get(to).Balance
	expectedFrom := uint64(genesisBalance - transferAmount - transferFee)
	expectedTo := uint64(transferAmount)

	if height != 1 {
		return fmt.Errorf("expected height 1, got %d", height)
	}
	if fromBalance != expectedFrom || toBalance != expectedTo {
		return fmt.Errorf("unexpected balances: from=%d to=%d", fromBalance, toBalance)
	}

	proofs := make([]validatorProof, 0, validatorCount)
	for i, c := range chains {
		if c.Height() != height || c.Tip().Hash != tip || c.State.Root() != root {
			return fmt.Errorf("validator-%d diverged", i)
		}
		snap, err := stores[i].Load()
		if err != nil {
			return fmt.Errorf("validator-%d reload: %w", i, err)
		}
		reloaded := snap.ChainID == chainID &&
			len(snap.Blocks) == 2 &&
			snap.Blocks[len(snap.Blocks)-1].Hash == tip &&
			snap.State.Root() == root
		if !reloaded {
			return fmt.Errorf("validator-%d reload mismatch", i)
		}
		finalized, votesFor, required, _ := engines[i].FinalityStatus(proposal.Hash)
		proofs = append(proofs, validatorProof{
			ID:          fmt.Sprintf("validator-%d", i),
			Height:      c.Height(),
			Tip:         c.Tip().Hash,
			StateRoot:   c.State.Root(),
			FromBalance: c.State.Get(from).Balance,
			ToBalance:   c.State.Get(to).Balance,
			Finalized:   finalized,
			VotesFor:    votesFor,
			Required:    required,
			Reloaded:    reloaded,
		})
	}

	summary := proofSummary{
		OK:               true,
		ChainID:          chainID,
		Validators:       validatorCount,
		RequiredFinality: engines[0].RequiredForFinality(),
		SubmittedTx:      tx.ID,
		FinalizedBlock:   tip,
		ElapsedMillis:    time.Since(started).Milliseconds(),
		Checks: []string{
			"created four validator ledgers from one genesis",
			"signed and submitted an Ed25519 SYN transfer",
			"built a deterministic candidate block",
			"collected 2/3+ validator attestations",
			"finalized the block on every validator",
			"verified height, tip, state root, and balances converge",
			"saved and reloaded finalized state from disk",
		},
		ValidatorProofs: proofs,
	}
	return writeJSON(os.Stdout, summary)
}

func writeFailure(started time.Time, err error) {
	_ = writeJSON(os.Stdout, map[string]any{
		"ok":         false,
		"error":      err.Error(),
		"elapsed_ms": time.Since(started).Milliseconds(),
	})
}

func writeJSON(f *os.File, v any) error {
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
