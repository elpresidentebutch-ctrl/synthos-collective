package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/pbkdf2"
)

var (
	ErrInvalidPassphrase = errors.New("invalid passphrase or corrupt keystore")
	ErrCorruptKeystore   = errors.New("corrupt keystore payload")
)

// Keystore holds password-encrypted private key data (standard Web3 AES-GCM + PBKDF2).
type Keystore struct {
	Version int            `json:"version"`
	Address string         `json:"address"`
	Crypto  KeystoreCrypto `json:"crypto"`
}

type KeystoreCrypto struct {
	Cipher       string       `json:"cipher"`
	Ciphertext   string       `json:"ciphertext"`
	Cipherparams Cipherparams `json:"cipherparams"`
	KDF          string       `json:"kdf"`
	KDFparams    KDFparams    `json:"kdfparams"`
	MAC          string       `json:"mac"`
}

type Cipherparams struct {
	IV string `json:"iv"`
}

type KDFparams struct {
	PRF   string `json:"prf"`
	C     int    `json:"c"`
	DKLen int    `json:"dklen"`
	Salt  string `json:"salt"`
}

// Encrypt encrypts the wallet's private key using AES-256-GCM and PBKDF2.
func (w *Wallet) Encrypt(passphrase string) (*Keystore, error) {
	if w.Private == nil {
		return nil, ErrNoKey
	}
	addr, err := w.Address()
	if err != nil {
		return nil, err
	}

	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generate salt: %w", err)
	}

	const iterations = 100_000
	const keyLen = 32
	derivedKey := pbkdf2.Key([]byte(passphrase), salt, iterations, keyLen, sha256.New)

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, fmt.Errorf("generate iv: %w", err)
	}

	ciphertext := gcm.Seal(nil, iv, []byte(w.Private), nil)

	macSum := sha256.Sum256(append(derivedKey[16:], ciphertext...))

	ks := &Keystore{
		Version: 1,
		Address: string(addr),
		Crypto: KeystoreCrypto{
			Cipher:     "aes-256-gcm",
			Ciphertext: hex.EncodeToString(ciphertext),
			Cipherparams: Cipherparams{
				IV: hex.EncodeToString(iv),
			},
			KDF: "pbkdf2",
			KDFparams: KDFparams{
				PRF:   "hmac-sha256",
				C:     iterations,
				DKLen: keyLen,
				Salt:  hex.EncodeToString(salt),
			},
			MAC: hex.EncodeToString(macSum[:]),
		},
	}
	return ks, nil
}

// DecryptKeystore decrypts a Keystore with the provided passphrase.
func DecryptKeystore(ks *Keystore, passphrase string) (*Wallet, error) {
	if ks == nil {
		return nil, ErrCorruptKeystore
	}
	if ks.Crypto.Cipher != "aes-256-gcm" || ks.Crypto.KDF != "pbkdf2" {
		return nil, fmt.Errorf("unsupported cipher %q or kdf %q", ks.Crypto.Cipher, ks.Crypto.KDF)
	}

	salt, err := hex.DecodeString(ks.Crypto.KDFparams.Salt)
	if err != nil || len(salt) == 0 {
		return nil, ErrCorruptKeystore
	}

	iterations := ks.Crypto.KDFparams.C
	if iterations <= 0 {
		iterations = 100_000
	}
	keyLen := ks.Crypto.KDFparams.DKLen
	if keyLen <= 0 {
		keyLen = 32
	}

	derivedKey := pbkdf2.Key([]byte(passphrase), salt, iterations, keyLen, sha256.New)

	ciphertext, err := hex.DecodeString(ks.Crypto.Ciphertext)
	if err != nil || len(ciphertext) == 0 {
		return nil, ErrCorruptKeystore
	}

	expectedMAC := sha256.Sum256(append(derivedKey[16:], ciphertext...))
	actualMAC, err := hex.DecodeString(ks.Crypto.MAC)
	if err != nil || subtle.ConstantTimeCompare(expectedMAC[:], actualMAC) != 1 {
		return nil, ErrInvalidPassphrase
	}

	iv, err := hex.DecodeString(ks.Crypto.Cipherparams.IV)
	if err != nil {
		return nil, ErrCorruptKeystore
	}

	block, err := aes.NewCipher(derivedKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, iv, ciphertext, nil)
	if err != nil {
		return nil, ErrInvalidPassphrase
	}

	return FromPrivateKeyHex(hex.EncodeToString(plaintext))
}

// SaveKeystore encrypts and writes a wallet keystore JSON to disk with 0600 permissions.
func (w *Wallet) SaveKeystore(filePath string, passphrase string) error {
	ks, err := w.Encrypt(passphrase)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(ks, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil && filepath.Dir(filePath) != "." {
		return err
	}
	return os.WriteFile(filePath, append(data, '\n'), 0o600)
}

// LoadKeystore reads and decrypts a wallet keystore JSON from disk.
func LoadKeystore(filePath string, passphrase string) (*Wallet, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read keystore: %w", err)
	}
	var ks Keystore
	if err := json.Unmarshal(data, &ks); err != nil {
		return nil, fmt.Errorf("parse keystore json: %w", err)
	}
	return DecryptKeystore(&ks, passphrase)
}
