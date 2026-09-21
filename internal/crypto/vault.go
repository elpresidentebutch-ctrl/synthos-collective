package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// saltSize is the random per-vault salt length, in bytes.
const saltSize = 16

// pbkdf2Iterations follows current OWASP guidance for PBKDF2-HMAC-SHA256
// (as of this writing, >=600,000). This is deliberately expensive: the whole
// point of a KDF here is to make brute-forcing a guessed/weak passphrase
// costly, which a single unsalted SHA-256 call (the previous implementation)
// did not do at all.
const pbkdf2Iterations = 600_000

// Vault stores the encrypted identity of the agent.
type Vault struct {
	PublicKey  []byte `json:"public_key"`
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce"`
	HardwareID string `json:"hardware_id"`

	// Salt is a random, per-vault value mixed into key derivation (see
	// deriveKey). Required for every vault created by this version of
	// SaveEncryptedKey. A vault with no salt was created by a previous,
	// vulnerable version of this code (unsalted SHA-256(passphrase+hwID),
	// with hwID a hardcoded constant and a hardcoded default passphrase
	// fallback in cmd/node/main.go) -- that key derivation was a fixed,
	// publicly-computable value, not a secret. LoadEncryptedKey refuses to
	// open such a vault: treat any identity stored that way as already
	// compromised and generate a fresh one rather than silently keep
	// supporting the broken derivation.
	Salt []byte `json:"salt,omitempty"`
}

// SaveEncryptedKey protects the agent's soul using AES-256-GCM. Both
// passphrase and hwID must be non-empty and are required inputs to key
// derivation, together with a fresh random salt generated here -- there is
// no fallback to a default passphrase; callers (see cmd/node/main.go) must
// require the operator to supply one.
func SaveEncryptedKey(path string, priv ed25519.PrivateKey, passphrase string, hwID string) error {
	if passphrase == "" {
		return errors.New("vault: passphrase must not be empty")
	}
	if hwID == "" {
		return errors.New("vault: hardware ID must not be empty")
	}

	salt := make([]byte, saltSize)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return fmt.Errorf("generating vault salt: %w", err)
	}

	key := deriveKey(passphrase, hwID, salt)

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}

	// 2. Encrypt the private key
	ciphertext := gcm.Seal(nil, nonce, priv, nil)

	vault := Vault{
		PublicKey:  priv.Public().(ed25519.PublicKey),
		Ciphertext: ciphertext,
		Nonce:      nonce,
		HardwareID: hwID,
		Salt:       salt,
	}

	data, err := json.Marshal(vault)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600) // Restricted permissions
}

// LoadEncryptedKey restores the agent identity if the passphrase and hardware match.
func LoadEncryptedKey(path string, passphrase string, hwID string) (ed25519.PrivateKey, error) {
	if passphrase == "" {
		return nil, errors.New("vault: passphrase must not be empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var vault Vault
	if err := json.Unmarshal(data, &vault); err != nil {
		return nil, err
	}

	if len(vault.Salt) == 0 {
		return nil, errors.New("vault: this vault was created before per-vault salting was added and its key derivation is no longer trusted (see Vault.Salt doc comment) -- treat any identity in it as compromised, delete it, and let a fresh one be generated")
	}

	// 3. Hardware Check: If the hardware ID doesn't match, we stop immediately.
	if vault.HardwareID != hwID {
		return nil, fmt.Errorf("HARDWARE MISMATCH: This vault belongs to another device")
	}

	key := deriveKey(passphrase, hwID, vault.Salt)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	// 4. Decrypt
	priv, err := gcm.Open(nil, vault.Nonce, vault.Ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("DECRYPTION FAILED: Invalid passphrase")
	}

	return ed25519.PrivateKey(priv), nil
}

// deriveKey turns a passphrase + hardware ID + random salt into a 32-byte
// AES-256 key via PBKDF2-HMAC-SHA256 (RFC 8018), implemented directly
// against the standard library so this package takes on no new dependency.
// This replaces a single unsalted sha256.Sum(passphrase+hwID) call, which
// made the derived key a fixed, offline-brute-forceable (in fact, for the
// default passphrase, precomputable) value rather than one that actually
// costs an attacker per guess.
func deriveKey(passphrase, hwID string, salt []byte) []byte {
	return pbkdf2HMACSHA256([]byte(passphrase+hwID), salt, pbkdf2Iterations, 32)
}

// pbkdf2HMACSHA256 implements PBKDF2 (RFC 8018) with HMAC-SHA256 as the
// pseudorandom function, returning a derived key of keyLen bytes.
func pbkdf2HMACSHA256(password, salt []byte, iterations, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen

	dk := make([]byte, 0, numBlocks*hashLen)
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(buf, uint32(block))
		prf.Write(buf)
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iterations; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}
