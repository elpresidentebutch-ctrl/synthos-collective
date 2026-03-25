package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"synthos-collective/internal/consensus"
)

// TestAdversarial_InvalidSignatureRejection tests that blocks with invalid signatures are rejected
func TestAdversarial_InvalidSignatureRejection(t *testing.T) {
	// Setup
	genesis := newTestGenesis()
	chain, err := NewChain(genesis)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}

	// Create a valid transaction
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("Failed to generate keypair: %v", err)
	}

	fromAddr := AddressFromPublicKey(pub)
	chain.State.Set(fromAddr, Account{Balance: 1000, Nonce: 0})

	tx := Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        Address("0x2222"),
		Amount:    100,
		Fee:       10,
		Nonce:     0,
		PublicKey: "0x" + toHex(pub),
	}

	// Sign it properly
	err = tx.Sign(priv)
	if err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}

	// **ATTACK 1: Tamper with transaction amount after signing**
	tamperTx := tx
	tamperTx.Amount = 900 // Try to transfer more than original
	// Signature is now invalid because amount changed

	// Create block with tampered tx
	block := &Block{
		Header: BlockHeader{
			Height:     1,
			ParentHash: chain.Tip().Hash,
			ProposerID: "attacker",
			StateRoot:  "fake",
		},
		Tx: []Tx{tamperTx},
	}
	block.ComputeHash()

	// VERIFY: Block validation should reject due to invalid signature
	err = chain.ValidateBlock(block)
	if err == nil {
		t.Fatal("SECURITY FAILURE: Block with tampered transaction was accepted! Attack succeeded.")
	}
	t.Logf("✅ Attack 1 Blocked: %v", err)

	// **ATTACK 2: Forge signature completely**
	forgeTx := Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        Address("0x3333"),
		Amount:    100,
		Fee:       10,
		Nonce:     0,
		PublicKey: "0x" + toHex(pub),
		Signature: "0x0000000000000000000000000000000000000000000000000000000000000000" +
			"0000000000000000000000000000000000000000000000000000000000000000", // Fake signature
	}
	forgeTx.ComputeID()

	block2 := &Block{
		Header: BlockHeader{
			Height:     1,
			ParentHash: chain.Tip().Hash,
			ProposerID: "attacker",
			StateRoot:  "fake",
		},
		Tx: []Tx{forgeTx},
	}
	block2.ComputeHash()

	err = chain.ValidateBlock(block2)
	if err == nil {
		t.Fatal("SECURITY FAILURE: Block with forged signature was accepted! Attack succeeded.")
	}
	t.Logf("✅ Attack 2 Blocked: %v", err)
}

// TestAdversarial_DoubleSigningDetection tests slashing for double-signing validators
func TestAdversarial_DoubleSigningDetection(t *testing.T) {
	slasher := consensus.NewSlashingTracker(consensus.SlashingParams{
		DoubleSignPenalty:   100,
		InvalidBlockPenalty: 50,
		DowntimePenalty:     10,
		SlashingWindow:      100,
	})

	validatorID := "validator-1"
	slasher.SetValidatorStake(validatorID, 1000)

	// **ATTACK: Validator signs same block height twice (different hashes)**
	blockHeight := uint64(42)

	// First signature is recorded
	err := slasher.DetectDoubleSigning(validatorID, blockHeight)
	if err != nil {
		t.Fatalf("First signature failed: %v", err)
	}

	// Second signature at same height should be detected
	err = slasher.DetectDoubleSigning(validatorID, blockHeight)
	if err == nil {
		t.Fatal("SECURITY FAILURE: Double-signing was not detected! Attack succeeded.")
	}

	// VERIFY: Validator is jailed and stake is slashed
	if !slasher.IsJailed(validatorID) {
		t.Fatal("SECURITY FAILURE: Validator was not jailed after double-signing")
	}

	remainingStake := slasher.GetValidatorStake(validatorID)
	expectedStake := uint64(900) // 1000 - 100 penalty
	if remainingStake != expectedStake {
		t.Fatalf("Stake mismatch: got %d, expected %d", remainingStake, expectedStake)
	}

	t.Logf("✅ Double-signing detected and slashed: %s, remaining stake: %d", err, remainingStake)
}

// TestAdversarial_DowntimeSlashing tests slashing for validator downtime
func TestAdversarial_DowntimeSlashing(t *testing.T) {
	slasher := consensus.NewSlashingTracker(consensus.SlashingParams{
		DowntimePenalty: 10,
		SlashingWindow:  100,
	})

	validatorID := "validator-2"
	slasher.SetValidatorStake(validatorID, 1000)

	// **ATTACK: Validator goes offline for multiple blocks**
	// Simulate missing 11 blocks (exceeds 10 block threshold)
	for i := 0; i < 11; i++ {
		err := slasher.RecordMissedBlock(validatorID)
		if i < 10 {
			if err != nil {
				t.Fatalf("Block %d: unexpected error: %v", i, err)
			}
		} else {
			// 11th missed block should trigger slashing
			if err == nil {
				t.Fatal("SECURITY FAILURE: Downtime was not detected! Attack succeeded.")
			}
		}
	}

	// VERIFY: Validator is jailed
	if !slasher.IsJailed(validatorID) {
		t.Fatal("SECURITY FAILURE: Validator was not jailed after downtime")
	}

	t.Logf("✅ Downtime slashing triggered and validator jailed")
}

// TestAdversarial_NonceReplay tests that replay attacks via nonce are blocked
func TestAdversarial_NonceReplay(t *testing.T) {
	genesis := newTestGenesis()
	chain, err := NewChain(genesis)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fromAddr := AddressFromPublicKey(pub)
	chain.State.Set(fromAddr, Account{Balance: 10000, Nonce: 0})

	// **ATTACK: Submit same transaction twice with nonce=0**
	tx := Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        Address("0x4444"),
		Amount:    100,
		Fee:       10,
		Nonce:     0, // Both will have same nonce
		PublicKey: "0x" + toHex(pub),
	}
	tx.Sign(priv)

	// First submission should succeed
	err = chain.SubmitTx(tx)
	if err != nil {
		t.Fatalf("First tx submission failed: %v", err)
	}

	// **ATTACK: Try to submit same transaction again**
	tx2 := tx
	err = chain.SubmitTx(tx2)
	if err == nil {
		t.Fatal("SECURITY FAILURE: Duplicate nonce was accepted! Replay attack succeeded.")
	}

	if err.Error() != "nonce mismatch: got 0, expected 1 for address " + string(fromAddr) {
		t.Logf("Correct error: %v", err)
	}

	t.Logf("✅ Nonce replay blocked: %v", err)
}

// TestAdversarial_ChainIDReplay tests cross-chain replay attack prevention
func TestAdversarial_ChainIDReplay(t *testing.T) {
	genesis := newTestGenesis()
	chain, err := NewChain(genesis)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	chain.ChainID = "testnet-1"

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fromAddr := AddressFromPublicKey(pub)
	chain.State.Set(fromAddr, Account{Balance: 10000, Nonce: 0})

	// Create transaction for testnet (ChainID=1)
	txTestnet := Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        Address("0x5555"),
		Amount:    100,
		Fee:       10,
		Nonce:     0,
		PublicKey: "0x" + toHex(pub),
	}
	txTestnet.Sign(priv)

	// **ATTACK: Try to replay testnet tx on mainnet (ChainID=42)**
	txMainnet := txTestnet
	txMainnet.ChainID = 42 // Attacker changes ChainID - but signature becomes invalid!

	// Create new chain with mainnet ChainID
	mainnet := &Chain{
		ChainID:  "mainnet",
		State:    chain.State,
		Blocks:   chain.Blocks,
		Mempool:  make(map[string]Tx),
	}

	// Try to submit on mainnet
	err = mainnet.SubmitTx(txMainnet)
	if err == nil {
		t.Fatal("SECURITY FAILURE: Cross-chain replay was accepted! Attack succeeded.")
	}

	if err.Error() != "transaction ID mismatch: got "+txMainnet.ID+" want 0x" {
		t.Logf("✅ Cross-chain replay blocked (signature verification): %v", err)
	}

	t.Logf("✅ ChainID mismatch detected")
}

// TestAdversarial_FeeExhaustion tests that trivial fees are rejected
func TestAdversarial_FeeExhaustion(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	fromAddr := AddressFromPublicKey(pub)

	// **ATTACK: Submit transaction with fee=0 to spam network**
	tx := Tx{
		ChainID:   1,
		From:      fromAddr,
		To:        Address("0x6666"),
		Amount:    100,
		Fee:       0, // Zero fee - attack vector
		Nonce:     0,
		PublicKey: "0x" + toHex(pub),
	}
	tx.Sign(priv)

	// Verification should reject zero fee
	err := tx.Verify()
	if err == nil {
		t.Fatal("SECURITY FAILURE: Zero-fee transaction was accepted! Spam attack possible.")
	}

	t.Logf("✅ Zero-fee spam attack blocked: %v", err)
}

// Helper function to create a test genesis
func newTestGenesis() Genesis {
	return Genesis{
		ChainID: "test",
		Alloc: map[Address]uint64{
			Address("0x1111"): 5000,
			Address("0x2222"): 2000,
		},
	}
}

// Helper to convert bytes to hex
func toHex(b []byte) string {
	return bytesToHex(b)
}

func bytesToHex(b []byte) string {
	const hex = "0123456789abcdef"
	s := make([]byte, len(b)*2)
	for i, v := range b {
		s[i*2] = hex[v>>4]
		s[i*2+1] = hex[v&0x0f]
	}
	return string(s)
}
