package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

type VerificationReport struct {
	CARPath     string   `json:"car_path"`
	CARSize     int64    `json:"car_size"`
	CARSha256   string   `json:"car_sha256"`
	RootCID     string   `json:"root_cid"`
	BlockCount  int      `json:"block_count"`
	StartHeight uint64   `json:"start_height"`
	EndHeight   uint64   `json:"end_height"`
	ChainValid  bool     `json:"chain_valid"`
	Errors      []string `json:"errors,omitempty"`
}

// VerifyCarFile unpacks a CAR file and validates every block, CID, and hash linkage.
func VerifyCarFile(carPath string) (*VerificationReport, error) {
	data, err := os.ReadFile(carPath)
	if err != nil {
		return nil, fmt.Errorf("reading car file %s: %w", carPath, err)
	}

	carHash := sha256.Sum256(data)
	carHashHex := hex.EncodeToString(carHash[:])

	f, err := os.Open(carPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	rootCID, entries, err := ReadCarArchive(f)
	if err != nil {
		return nil, fmt.Errorf("unpacking car file: %w", err)
	}

	report := &VerificationReport{
		CARPath:    carPath,
		CARSize:    int64(len(data)),
		CARSha256:  carHashHex,
		RootCID:    rootCID.Str,
		BlockCount: len(entries),
		ChainValid: true,
	}

	if len(entries) == 0 {
		report.ChainValid = false
		report.Errors = append(report.Errors, "archive contains zero block entries")
		return report, nil
	}

	var prevHash string
	var prevHeight uint64
	hasPrev := false

	for idx, entry := range entries {
		// 1. Verify individual block CID matches its payload data
		computedCID := ComputeRawCIDv1(entry.Data)
		if computedCID.DigestHex() != entry.CID.DigestHex() {
			report.ChainValid = false
			report.Errors = append(report.Errors, fmt.Sprintf("block index %d: CID mismatch (computed %s != header %s)", idx, computedCID.Str, entry.CID.Str))
		}

		// 2. Parse block structure
		var bs BlockSummary
		if err := json.Unmarshal(entry.Data, &bs); err != nil {
			report.ChainValid = false
			report.Errors = append(report.Errors, fmt.Sprintf("block index %d: invalid JSON: %v", idx, err))
			continue
		}

		if idx == 0 {
			report.StartHeight = bs.Header.Height
		}
		if idx == len(entries)-1 {
			report.EndHeight = bs.Header.Height
		}

		// 3. Verify sequence continuity and parent hash linkage
		if hasPrev {
			if bs.Header.Height != prevHeight+1 {
				report.ChainValid = false
				report.Errors = append(report.Errors, fmt.Sprintf("height gap at block index %d: got height %d, expected %d", idx, bs.Header.Height, prevHeight+1))
			}
			if bs.Header.ParentHash != prevHash {
				report.ChainValid = false
				report.Errors = append(report.Errors, fmt.Sprintf("hash chain broken at height %d: parent_hash %s != previous block hash %s", bs.Header.Height, bs.Header.ParentHash, prevHash))
			}
		}

		prevHeight = bs.Header.Height
		prevHash = bs.Hash
		hasPrev = true
	}

	return report, nil
}
