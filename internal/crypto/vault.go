package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Vault stores the encrypted identity of the agent.
type Vault struct {
	PublicKey  []byte `json:"public_key"`
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce"`
	HardwareID string `json:"hardware_id"`
}

// SaveEncryptedKey protects the agent's soul using AES-256-GCM.
// It binds the encryption to the hardware ID so it can't be moved to another machine.
func SaveEncryptedKey(path string, priv ed25519.PrivateKey, passphrase string, hwID string) error {
	// 1. Derive key from passphrase + hardware ID
	key := deriveKey(passphrase, hwID)

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
	}

	data, _ := json.Marshal(vault)
	return os.WriteFile(path, data, 0600) // Restricted permissions
}

// LoadEncryptedKey restores the agent identity if the passphrase and hardware match.
func LoadEncryptedKey(path string, passphrase string, hwID string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var vault Vault
	if err := json.Unmarshal(data, &vault); err != nil {
		return nil, err
	}

	// 3. Hardware Check: If the hardware ID doesn't match, we stop immediately.
	if vault.HardwareID != hwID {
		return nil, fmt.Errorf("HARDWARE MISMATCH: This vault belongs to another device")
	}

	key := deriveKey(passphrase, hwID)
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

func deriveKey(passphrase, hwID string) []byte {
	// Combining passphrase and hardware ID for a 32-byte AES key
	h := sha256.New()
	h.Write([]byte(passphrase + hwID))
	return h.Sum(nil)
}
