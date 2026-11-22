package kvstate

import (
	"bytes"
	"errors"
	"sort"
	"sync"
)

var (
	ErrKeyNotFound = errors.New("key not found")
	ErrKeyRequired = errors.New("key cannot be empty")
)

// KVStore defines the low-level key-value storage engine interface.
// It supports atomic reads, writes, deletions, prefix scans, and batch commits.
type KVStore interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	Has(key []byte) (bool, error)
	Scan(prefix []byte, fn func(key, value []byte) error) error
	NewBatch() Batch
	Close() error
}

// Batch allows atomic multi-key writes.
type Batch interface {
	Set(key, value []byte)
	Delete(key []byte)
	Commit() error
	Reset()
}

// MemKVStore is a pure in-memory, thread-safe implementation of KVStore.
// It maintains keys in canonical lexicographical order for deterministic scanning.
type MemKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemKVStore creates a new in-memory KV store.
func NewMemKVStore() *MemKVStore {
	return &MemKVStore{
		data: make(map[string][]byte),
	}
}

func (m *MemKVStore) Get(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, ErrKeyRequired
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.data[string(key)]
	if !ok {
		return nil, ErrKeyNotFound
	}
	out := make([]byte, len(val))
	copy(out, val)
	return out, nil
}

func (m *MemKVStore) Set(key, value []byte) error {
	if len(key) == 0 {
		return ErrKeyRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	val := make([]byte, len(value))
	copy(val, value)
	m.data[string(key)] = val
	return nil
}

func (m *MemKVStore) Delete(key []byte) error {
	if len(key) == 0 {
		return ErrKeyRequired
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.data, string(key))
	return nil
}

func (m *MemKVStore) Has(key []byte) (bool, error) {
	if len(key) == 0 {
		return false, ErrKeyRequired
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	_, ok := m.data[string(key)]
	return ok, nil
}

func (m *MemKVStore) Scan(prefix []byte, fn func(key, value []byte) error) error {
	m.mu.RLock()
	keys := make([]string, 0, len(m.data))
	prefixStr := string(prefix)
	for k := range m.data {
		if bytes.HasPrefix([]byte(k), prefix) {
			keys = append(keys, k)
		}
	}
	m.mu.RUnlock()

	sort.Strings(keys) // Canonical sorted order

	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, k := range keys {
		val, ok := m.data[k]
		if !ok {
			continue
		}
		if err := fn([]byte(k), val); err != nil {
			return err
		}
	}
	_ = prefixStr
	return nil
}

func (m *MemKVStore) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[string][]byte)
	return nil
}

func (m *MemKVStore) NewBatch() Batch {
	return &memBatch{
		store: m,
		ops:   make([]batchOp, 0),
	}
}

type batchOp struct {
	isDelete bool
	key      []byte
	value    []byte
}

type memBatch struct {
	store *MemKVStore
	ops   []batchOp
}

func (b *memBatch) Set(key, value []byte) {
	k := make([]byte, len(key))
	copy(k, key)
	v := make([]byte, len(value))
	copy(v, value)
	b.ops = append(b.ops, batchOp{isDelete: false, key: k, value: v})
}

func (b *memBatch) Delete(key []byte) {
	k := make([]byte, len(key))
	copy(k, key)
	b.ops = append(b.ops, batchOp{isDelete: true, key: k})
}

func (b *memBatch) Commit() error {
	b.store.mu.Lock()
	defer b.store.mu.Unlock()

	for _, op := range b.ops {
		if op.isDelete {
			delete(b.store.data, string(op.key))
		} else {
			val := make([]byte, len(op.value))
			copy(val, op.value)
			b.store.data[string(op.key)] = val
		}
	}
	b.ops = b.ops[:0]
	return nil
}

func (b *memBatch) Reset() {
	b.ops = b.ops[:0]
}
