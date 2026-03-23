package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"synthos-collective/internal/chain"
)

// Store persists chain data to disk in a simple JSON form.
// This is intentionally minimal to keep the codebase auditable; we can later
// migrate to BoltDB/Badger/RocksDB without changing the Chain API.
type Store struct {
	Dir string
}

var ErrNoDir = errors.New("store dir required")

func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, ErrNoDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

type snapshot struct {
	ChainID string        `json:"chain_id"`
	Blocks  []*chain.Block `json:"blocks"`
	State   *chain.State  `json:"state"`
}

// Snapshot is the exported view for loading.
type Snapshot = snapshot

func (s *Store) Save(c *chain.Chain) error {
	snap := snapshot{
		ChainID: c.ChainID,
		Blocks:  c.Blocks,
		State:   c.State,
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Dir, "chain.json.tmp")
	final := filepath.Join(s.Dir, "chain.json")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func (s *Store) Load() (*Snapshot, error) {
	path := filepath.Join(s.Dir, "chain.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snap snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

