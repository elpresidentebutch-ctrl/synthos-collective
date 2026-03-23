package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

type KeyPair struct {
	Public  ed25519.PublicKey
	Private ed25519.PrivateKey
}

// NewKeyPair creates a new ed25519 keypair.
func NewKeyPair() (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, err
	}
	return KeyPair{Public: pub, Private: priv}, nil
}

func PublicKeyHex(pub ed25519.PublicKey) string {
	return "0x" + hex.EncodeToString(pub)
}

func PrivateKeyHashHex(priv ed25519.PrivateKey) string {
	sum := sha256.Sum256(priv)
	return "0x" + hex.EncodeToString(sum[:])
}

func Sign(priv ed25519.PrivateKey, msg []byte) []byte {
	return ed25519.Sign(priv, msg)
}

func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	return ed25519.Verify(pub, msg, sig)
}

// PublicKeyBytes parses a hex-encoded public key (0x...).
func PublicKeyBytes(pubHex string) ([]byte, error) {
	if len(pubHex) >= 2 && pubHex[:2] == "0x" {
		pubHex = pubHex[2:]
	}
	b, err := hex.DecodeString(pubHex)
	if err != nil {
		return nil, err
	}
	return b, nil
}

