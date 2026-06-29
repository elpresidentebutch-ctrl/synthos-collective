package memory

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrInvalidKey        = errors.New("memory key must be 32 bytes")
	ErrInvalidIdentity   = errors.New("invalid memory identity key")
	ErrBrokenMemoryChain = errors.New("broken memory commitment chain")
)

// Envelope is an encrypted, signed, append-only individual memory record.
// Ciphertext is private agent data; Commitment and Signature are safe to anchor.
type Envelope struct {
	OwnerID            string `json:"owner_id"`
	MemoryID           string `json:"memory_id"`
	PreviousCommitment string `json:"previous_commitment,omitempty"`
	Ciphertext         []byte `json:"ciphertext"`
	Nonce              []byte `json:"nonce"`
	PolicyCommitment   string `json:"policy_commitment"`
	LogicalHeight      uint64 `json:"logical_height"`
	Commitment         string `json:"commitment"`
	Signature          []byte `json:"signature"`
}

// Vault owns one agent's encrypted memory chain.
type Vault struct {
	mu      sync.RWMutex
	ownerID string
	key     [32]byte
	private ed25519.PrivateKey
	public  ed25519.PublicKey
	records []Envelope
}

func NewVault(ownerID string, key []byte, private ed25519.PrivateKey) (*Vault, error) {
	if ownerID == "" {
		return nil, errors.New("owner identity required")
	}
	if len(key) != 32 {
		return nil, ErrInvalidKey
	}
	if len(private) != ed25519.PrivateKeySize {
		return nil, ErrInvalidIdentity
	}
	vault := &Vault{
		ownerID: ownerID,
		private: append(ed25519.PrivateKey(nil), private...),
		public:  append(ed25519.PublicKey(nil), private.Public().(ed25519.PublicKey)...),
		records: make([]Envelope, 0, 64),
	}
	copy(vault.key[:], key)
	return vault, nil
}

func (v *Vault) Append(plaintext []byte, policy []byte, logicalHeight uint64) (Envelope, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return Envelope{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}

	previous := ""
	if len(v.records) > 0 {
		previous = v.records[len(v.records)-1].Commitment
	}
	policyHash := sha256.Sum256(policy)
	policyCommitment := hexHash(policyHash[:])
	aad, err := associatedData(v.ownerID, previous, policyCommitment, logicalHeight)
	if err != nil {
		return Envelope{}, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, aad)

	env := Envelope{
		OwnerID:            v.ownerID,
		PreviousCommitment: previous,
		Ciphertext:         ciphertext,
		Nonce:              nonce,
		PolicyCommitment:   policyCommitment,
		LogicalHeight:      logicalHeight,
	}
	commitment, err := envelopeCommitment(env)
	if err != nil {
		return Envelope{}, err
	}
	env.Commitment = commitment
	env.MemoryID = commitment
	env.Signature = ed25519.Sign(v.private, []byte(commitment))
	v.records = append(v.records, cloneEnvelope(env))
	return cloneEnvelope(env), nil
}

func (v *Vault) Decrypt(env Envelope) ([]byte, error) {
	if err := VerifyEnvelope(env, v.public); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(v.key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	aad, err := associatedData(
		env.OwnerID,
		env.PreviousCommitment,
		env.PolicyCommitment,
		env.LogicalHeight,
	)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, env.Nonce, env.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt memory: %w", err)
	}
	return plaintext, nil
}

func (v *Vault) Records() []Envelope {
	v.mu.RLock()
	defer v.mu.RUnlock()
	out := make([]Envelope, len(v.records))
	for i, record := range v.records {
		out[i] = cloneEnvelope(record)
	}
	return out
}

func VerifyChain(records []Envelope, public ed25519.PublicKey) error {
	previous := ""
	for i, record := range records {
		if record.PreviousCommitment != previous {
			return fmt.Errorf("%w at record %d", ErrBrokenMemoryChain, i)
		}
		if err := VerifyEnvelope(record, public); err != nil {
			return fmt.Errorf("verify record %d: %w", i, err)
		}
		previous = record.Commitment
	}
	return nil
}

func VerifyEnvelope(env Envelope, public ed25519.PublicKey) error {
	if len(public) != ed25519.PublicKeySize {
		return ErrInvalidIdentity
	}
	expected, err := envelopeCommitment(env)
	if err != nil {
		return err
	}
	if env.Commitment != expected || env.MemoryID != expected {
		return errors.New("memory commitment mismatch")
	}
	if !ed25519.Verify(public, []byte(env.Commitment), env.Signature) {
		return errors.New("invalid memory signature")
	}
	return nil
}

func envelopeCommitment(env Envelope) (string, error) {
	payload := struct {
		OwnerID            string `json:"owner_id"`
		PreviousCommitment string `json:"previous_commitment,omitempty"`
		Ciphertext         []byte `json:"ciphertext"`
		Nonce              []byte `json:"nonce"`
		PolicyCommitment   string `json:"policy_commitment"`
		LogicalHeight      uint64 `json:"logical_height"`
	}{
		OwnerID:            env.OwnerID,
		PreviousCommitment: env.PreviousCommitment,
		Ciphertext:         env.Ciphertext,
		Nonce:              env.Nonce,
		PolicyCommitment:   env.PolicyCommitment,
		LogicalHeight:      env.LogicalHeight,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hexHash(sum[:]), nil
}

func associatedData(owner, previous, policy string, height uint64) ([]byte, error) {
	return json.Marshal(struct {
		OwnerID            string `json:"owner_id"`
		PreviousCommitment string `json:"previous_commitment,omitempty"`
		PolicyCommitment   string `json:"policy_commitment"`
		LogicalHeight      uint64 `json:"logical_height"`
	}{
		OwnerID:            owner,
		PreviousCommitment: previous,
		PolicyCommitment:   policy,
		LogicalHeight:      height,
	})
}

func hexHash(data []byte) string {
	return "0x" + hex.EncodeToString(data)
}

func cloneEnvelope(env Envelope) Envelope {
	env.Ciphertext = append([]byte(nil), env.Ciphertext...)
	env.Nonce = append([]byte(nil), env.Nonce...)
	env.Signature = append([]byte(nil), env.Signature...)
	return env
}
