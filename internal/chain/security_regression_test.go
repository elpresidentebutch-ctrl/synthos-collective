package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func signedTestTx(t *testing.T, chainID uint64, balance uint64) (*Chain, Tx) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	from := AddressFromPublicKey(pub)
	c, err := NewChain(Genesis{
		ChainID:   "security-test",
		TxChainID: chainID,
		Alloc:     map[Address]uint64{from: balance},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx := Tx{
		ChainID:   chainID,
		From:      from,
		To:        Address("0x1111111111111111111111111111111111111111"),
		Amount:    10,
		Fee:       1,
		Nonce:     0,
		PublicKey: "0x" + encodeHex(pub),
	}
	if err := tx.Sign(priv); err != nil {
		t.Fatal(err)
	}
	return c, tx
}

func TestSubmitTxRejectsWrongChainDomain(t *testing.T) {
	c, tx := signedTestTx(t, 7, 100)
	c.TxChainID = 42
	if err := c.SubmitTx(tx); err == nil {
		t.Fatal("expected wrong chain ID to be rejected")
	}
}

func TestValidateBlockNeverSlashesCanonicalState(t *testing.T) {
	c, tx := signedTestTx(t, 42, 100)
	victim := Address("0xvictim")
	c.State.Set(victim, Account{Balance: 1_000})

	tx.Signature = "0x00"
	b := &Block{
		Header: BlockHeader{
			Height:     1,
			ParentHash: c.Tip().Hash,
			ProposerID: "victim",
			StateRoot:  c.State.Root(),
		},
		Tx: []Tx{tx},
	}
	if _, err := b.ComputeHash(); err != nil {
		t.Fatal(err)
	}
	if err := c.ValidateBlock(b); err == nil {
		t.Fatal("expected invalid block")
	}
	if got := c.State.Get(victim).Balance; got != 1_000 {
		t.Fatalf("validation mutated canonical state: got balance %d", got)
	}
}

func encodeHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = digits[v>>4]
		out[i*2+1] = digits[v&0x0f]
	}
	return string(out)
}
