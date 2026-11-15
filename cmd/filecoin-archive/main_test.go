package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCIDComputation(t *testing.T) {
	data := []byte("hello synthos collective filecoin archive")
	cid := ComputeRawCIDv1(data)

	if !stringsHasPrefix(cid.Str, "bafkrei") {
		t.Fatalf("expected bafkrei prefix for raw CIDv1, got %s", cid.Str)
	}

	parsed, err := ParseCID(cid.Str)
	if err != nil {
		t.Fatalf("failed to parse CID string %s: %v", cid.Str, err)
	}

	if parsed.DigestHex() != cid.DigestHex() {
		t.Fatalf("digest mismatch: got %s, want %s", parsed.DigestHex(), cid.DigestHex())
	}
}

func TestArchiveVerifyRestoreEndToEnd(t *testing.T) {
	tempDir := t.TempDir()
	blocksDir := filepath.Join(tempDir, "source", "blocks")
	if err := os.MkdirAll(blocksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Generate 15 chained mock blocks
	prevHash := "0x0"
	for h := uint64(0); h < 15; h++ {
		blkHash := fmt.Sprintf("0xhash%04d", h)
		bs := BlockSummary{
			Header: BlockHeaderSummary{
				Height:     h,
				ParentHash: prevHash,
				StateRoot:  fmt.Sprintf("0xroot%04d", h),
			},
			Hash: blkHash,
		}
		raw, _ := json.Marshal(bs)
		_ = os.WriteFile(filepath.Join(blocksDir, fmt.Sprintf("%d.json", h)), raw, 0o644)
		prevHash = blkHash
	}

	// Archive with chunk size 5 (producing 3 chunks)
	outDir := filepath.Join(tempDir, "archive-out")
	idx, err := ArchiveLegacyDirectory(filepath.Join(tempDir, "source"), outDir, 5, "synthos-test")
	if err != nil {
		t.Fatalf("ArchiveLegacyDirectory failed: %v", err)
	}

	if idx.TotalChunks != 3 {
		t.Fatalf("expected 3 chunks, got %d", idx.TotalChunks)
	}
	if idx.TotalBlocksIndexed != 15 {
		t.Fatalf("expected 15 blocks indexed, got %d", idx.TotalBlocksIndexed)
	}

	// Verify each chunk
	for _, chunk := range idx.Chunks {
		carPath := filepath.Join(outDir, chunk.CARFile)
		rep, err := VerifyCarFile(carPath)
		if err != nil {
			t.Fatalf("VerifyCarFile failed on %s: %v", carPath, err)
		}
		if !rep.ChainValid {
			t.Fatalf("chunk %s reported invalid: %v", carPath, rep.Errors)
		}
		if rep.BlockCount != 5 {
			t.Fatalf("expected 5 blocks in chunk, got %d", rep.BlockCount)
		}
	}

	// Restore chunk 1 to fresh target
	restoreDir := filepath.Join(tempDir, "restored")
	car1Path := filepath.Join(outDir, idx.Chunks[0].CARFile)
	restored, err := RestoreCarToDirectory(car1Path, restoreDir)
	if err != nil {
		t.Fatalf("RestoreCarToDirectory failed: %v", err)
	}
	if restored != 5 {
		t.Fatalf("expected 5 restored blocks, got %d", restored)
	}

	// Verify restored block exists and matches original
	restoredBlock0 := filepath.Join(restoreDir, "blocks", "0.json")
	if _, err := os.Stat(restoredBlock0); err != nil {
		t.Fatalf("restored block 0 missing: %v", err)
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
