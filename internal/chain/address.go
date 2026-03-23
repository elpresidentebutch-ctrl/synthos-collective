package chain

import (
	"crypto/sha256"
	"encoding/hex"
)

// Address is a 20-byte account identifier.
// For now we derive it from sha256(pubkey) and take the first 20 bytes.
// (We can swap to a different scheme later without changing the ledger model.)
type Address string

func AddressFromPublicKey(pub []byte) Address {
	sum := sha256.Sum256(pub)
	return Address("0x" + hex.EncodeToString(sum[:20]))
}

