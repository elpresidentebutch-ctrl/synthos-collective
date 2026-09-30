package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"
)

func signedCitizenTx(t *testing.T, priv ed25519.PrivateKey, nonce, amount uint64, txType string, ts int64) Tx {
	t.Helper()
	pub := priv.Public().(ed25519.PublicKey)
	from := AddressFromPublicKey(pub)
	tx := Tx{ChainID: 1, From: from, To: from, Amount: amount, Fee: 1, Nonce: nonce,
		PublicKey: "0x" + hex.EncodeToString(pub),
		Metadata:  []KeyValuePair{{Key: "type", Value: txType}}, Timestamp: ts}
	if err := tx.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return tx
}

// TestSubmitTxRejectsForgedCitizenTimestamps covers the legacy chain,
// where the reward clock is still the transaction's timestamp: the
// mempool refuses citizen transactions whose timestamp is far from now.
func TestSubmitTxRejectsForgedCitizenTimestamps(t *testing.T) {
	now := int64(1_790_000_000)
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	for _, c := range []struct {
		txType string
		ts     int64
		ok     bool
	}{
		{"citizen_stake", now, true},
		{"citizen_stake", now - MaxCitizenClockSkew, true},
		{"citizen_stake", 1, false},
		{"citizen_stake", 0, false},
		{"citizen_claim_rewards", now + MaxCitizenClockSkew + 1, false},
		{"citizen_claim_rewards", now + 50*365*86400, false},
		{"citizen_unstake", 0, true}, // no clock involved
	} {
		err := checkCitizenClock(signedCitizenTx(t, priv, 0, 5, c.txType, c.ts), now)
		if (err == nil) != c.ok {
			t.Errorf("%s at %d: err = %v, want ok=%v", c.txType, c.ts, err, c.ok)
		}
	}
	plain := signedCitizenTx(t, priv, 0, 5, "transfer", 0)
	if err := checkCitizenClock(plain, now); err != nil {
		t.Errorf("ordinary transfer rejected: %v", err)
	}
}

// TestChainSubmitTxAppliesCitizenClockCheck: the check is wired into the
// real mempool entry point.
func TestChainSubmitTxAppliesCitizenClockCheck(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	staker := AddressFromPublicKey(priv.Public().(ed25519.PublicKey))
	c, err := NewChain(Genesis{ChainID: "citizen-clock-test", Alloc: map[Address]uint64{staker: 1_000_000}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.SubmitTx(signedCitizenTx(t, priv, 0, 100, "citizen_stake", 1)); err == nil {
		t.Fatal("mempool accepted a citizen stake backdated to 1970")
	}
	if err := c.SubmitTx(signedCitizenTx(t, priv, 0, 100, "citizen_stake", time.Now().Unix())); err != nil {
		t.Fatalf("mempool rejected a current citizen stake: %v", err)
	}
}
