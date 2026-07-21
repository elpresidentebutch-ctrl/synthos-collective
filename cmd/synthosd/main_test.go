package main

import (
	"testing"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/storage"
)

func TestShouldRefreshHeightZeroSnapshotWhenGenesisChanges(t *testing.T) {
	oldChain := mustTestChain(t, chain.Genesis{
		ChainID:   "synthos-mainnet-1",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0xfounder": 100,
		},
	})
	newChain := mustTestChain(t, chain.Genesis{
		ChainID:   "synthos-mainnet-1",
		TxChainID: 20260702,
		Alloc: map[chain.Address]uint64{
			"0xfounder": 90,
			"0xwallet":  10,
		},
	})
	snap := &storage.Snapshot{
		ChainID:   oldChain.ChainID,
		TxChainID: oldChain.TransactionChainID(),
		Blocks:    oldChain.Blocks,
		State:     oldChain.State,
	}

	if !shouldRefreshHeightZeroSnapshot(snap, newChain) {
		t.Fatal("expected height-zero snapshot to refresh when genesis state changes")
	}
	if shouldRefreshHeightZeroSnapshot(snap, oldChain) {
		t.Fatal("did not expect matching height-zero snapshot to refresh")
	}

	snap.Blocks = append(snap.Blocks, &chain.Block{Header: chain.BlockHeader{Height: 1}})
	if shouldRefreshHeightZeroSnapshot(snap, newChain) {
		t.Fatal("did not expect non-genesis snapshot to refresh")
	}
}

func mustTestChain(t *testing.T, genesis chain.Genesis) *chain.Chain {
	t.Helper()
	c, err := chain.NewChain(genesis)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
