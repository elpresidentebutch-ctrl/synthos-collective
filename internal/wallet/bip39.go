package wallet

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

var (
	ErrInvalidWordCount = errors.New("invalid mnemonic word count: must be 12, 15, 18, 21, or 24 words")
	ErrInvalidWord      = errors.New("word not in BIP-39 English wordlist")
	ErrInvalidChecksum  = errors.New("invalid BIP-39 mnemonic checksum")
)

// NewMnemonic generates a new cryptographically secure BIP-39 mnemonic phrase.
// Supported word counts: 12 (128 bits), 15 (160 bits), 18 (192 bits), 21 (224 bits), 24 (256 bits).
func NewMnemonic(wordCount int) (string, error) {
	var entBytes int
	switch wordCount {
	case 12:
		entBytes = 16
	case 15:
		entBytes = 20
	case 18:
		entBytes = 24
	case 21:
		entBytes = 28
	case 24:
		entBytes = 32
	default:
		return "", ErrInvalidWordCount
	}

	entropy := make([]byte, entBytes)
	if _, err := io.ReadFull(rand.Reader, entropy); err != nil {
		return "", fmt.Errorf("read secure random entropy: %w", err)
	}

	return EntropyToMnemonic(entropy)
}

// EntropyToMnemonic encodes raw entropy bytes into a BIP-39 mnemonic phrase.
func EntropyToMnemonic(entropy []byte) (string, error) {
	entLen := len(entropy)
	if entLen != 16 && entLen != 20 && entLen != 24 && entLen != 28 && entLen != 32 {
		return "", fmt.Errorf("invalid entropy length: %d bytes", entLen)
	}

	// Calculate checksum length: ENT / 32 bits
	csLen := (entLen * 8) / 32
	hash := sha256.Sum256(entropy)

	// Collect all bits: ENT bits + CS bits
	var bits []bool
	for _, b := range entropy {
		for i := 7; i >= 0; i-- {
			bits = append(bits, (b>>i)&1 == 1)
		}
	}
	for i := 0; i < csLen; i++ {
		bits = append(bits, (hash[0]>>(7-i))&1 == 1)
	}

	// Group into 11-bit indices
	numWords := len(bits) / 11
	words := make([]string, numWords)
	for i := 0; i < numWords; i++ {
		var idx int
		for j := 0; j < 11; j++ {
			idx <<= 1
			if bits[i*11+j] {
				idx |= 1
			}
		}
		words[i] = englishWordlist[idx]
	}

	return strings.Join(words, " "), nil
}

// MnemonicToEntropy decodes and validates a BIP-39 mnemonic phrase into raw entropy bytes.
func MnemonicToEntropy(mnemonic string) ([]byte, error) {
	words := strings.Fields(strings.TrimSpace(strings.ToLower(mnemonic)))
	numWords := len(words)
	if numWords != 12 && numWords != 15 && numWords != 18 && numWords != 21 && numWords != 24 {
		return nil, ErrInvalidWordCount
	}

	var bits []bool
	for _, word := range words {
		idx, ok := wordMap[word]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrInvalidWord, word)
		}
		for j := 10; j >= 0; j-- {
			bits = append(bits, (idx>>j)&1 == 1)
		}
	}

	// Calculate ENT and CS
	totalBits := len(bits)
	csLen := totalBits / 33
	entBits := totalBits - csLen
	entBytes := entBits / 8

	entropy := make([]byte, entBytes)
	for i := 0; i < entBytes; i++ {
		var b byte
		for j := 0; j < 8; j++ {
			b <<= 1
			if bits[i*8+j] {
				b |= 1
			}
		}
		entropy[i] = b
	}

	// Validate checksum
	hash := sha256.Sum256(entropy)
	for i := 0; i < csLen; i++ {
		expectedBit := (hash[0]>>(7-i))&1 == 1
		actualBit := bits[entBits+i]
		if expectedBit != actualBit {
			return nil, ErrInvalidChecksum
		}
	}

	return entropy, nil
}

// ValidateMnemonic verifies if a mnemonic string is valid BIP-39 with correct checksum.
func ValidateMnemonic(mnemonic string) error {
	_, err := MnemonicToEntropy(mnemonic)
	return err
}

// MnemonicToSeed derives a 64-byte seed from a BIP-39 mnemonic and optional passphrase.
// Uses PBKDF2 HMAC-SHA512 with 2048 iterations as per the BIP-39 standard.
func MnemonicToSeed(mnemonic string, passphrase string) []byte {
	cleanMnemonic := strings.Join(strings.Fields(strings.TrimSpace(strings.ToLower(mnemonic))), " ")
	salt := []byte("mnemonic" + passphrase)
	return pbkdf2.Key([]byte(cleanMnemonic), salt, 2048, 64, sha512.New)
}

// FromMnemonic creates a SYNTHOS Ed25519 wallet from a BIP-39 mnemonic phrase and optional passphrase.
// Uses SLIP-0010 master derivation: HMAC-SHA512("ed25519 seed", seed)[:32] to produce the 32-byte Ed25519 seed.
func FromMnemonic(mnemonic string, passphrase string) (*Wallet, error) {
	if err := ValidateMnemonic(mnemonic); err != nil {
		return nil, fmt.Errorf("validate mnemonic: %w", err)
	}

	seed64 := MnemonicToSeed(mnemonic, passphrase)

	// SLIP-0010 Ed25519 master root key derivation
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	mac.Write(seed64)
	master := mac.Sum(nil)
	edSeed := master[:32]

	wallet, err := FromSeed(edSeed)
	if err != nil {
		return nil, fmt.Errorf("create wallet from derived seed: %w", err)
	}
	return wallet, nil
}
