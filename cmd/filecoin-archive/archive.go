package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ChunkMeta struct {
	StartHeight    uint64 `json:"start_height"`
	EndHeight      uint64 `json:"end_height"`
	BlockCount     int    `json:"block_count"`
	Engine         string `json:"engine"`
	CARFile        string `json:"car_file"`
	CARSizeBytes   int64  `json:"car_size_bytes"`
	CARSha256      string `json:"car_sha256"`
	CID            string `json:"cid"`
	FirstBlockHash string `json:"first_block_hash"`
	LastBlockHash  string `json:"last_block_hash"`
	FirstStateRoot string `json:"first_state_root"`
	LastStateRoot  string `json:"last_state_root"`
	ArchivedAt     string `json:"archived_at"`
}

type ArchiveIndex struct {
	Format             string      `json:"format"`
	ChainID            string      `json:"chain_id"`
	LastUpdated        string      `json:"last_updated"`
	TotalBlocksIndexed uint64      `json:"total_blocks_indexed"`
	TotalChunks        int         `json:"total_chunks"`
	Chunks             []ChunkMeta `json:"chunks"`
}

type BlockHeaderSummary struct {
	Height     uint64 `json:"height"`
	ParentHash string `json:"parent_hash"`
	StateRoot  string `json:"state_root"`
}

type BlockSummary struct {
	Header BlockHeaderSummary `json:"header"`
	Hash   string             `json:"hash"`
}

// ArchiveLegacyDirectory reads legacy block JSON files, partitions them into
// chunks of chunkSize, packages them into CAR files, and updates filecoin-index.json.
func ArchiveLegacyDirectory(sourceDir, outDir string, chunkSize uint64, chainID string) (*ArchiveIndex, error) {
	if chunkSize == 0 {
		chunkSize = 5000
	}
	if chainID == "" {
		chainID = "synthos-mainnet-1"
	}

	blocksDir := sourceDir
	if fi, err := os.Stat(filepath.Join(sourceDir, "blocks")); err == nil && fi.IsDir() {
		blocksDir = filepath.Join(sourceDir, "blocks")
	}

	entries, err := os.ReadDir(blocksDir)
	if err != nil {
		return nil, fmt.Errorf("reading blocks directory %s: %w", blocksDir, err)
	}

	var heights []uint64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		h, err := strconv.ParseUint(name, 10, 64)
		if err != nil {
			continue
		}
		heights = append(heights, h)
	}

	if len(heights) == 0 {
		return nil, fmt.Errorf("no block json files found in %s", blocksDir)
	}

	sort.Slice(heights, func(i, j int) bool { return heights[i] < heights[j] })

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating output dir %s: %w", outDir, err)
	}

	indexFile := filepath.Join(outDir, "filecoin-index.json")
	index := loadOrCreateIndex(indexFile, chainID)

	// Determine existing indexed chunks to avoid duplicate work
	indexedChunks := make(map[string]bool)
	for _, c := range index.Chunks {
		key := fmt.Sprintf("%d-%d", c.StartHeight, c.EndHeight)
		indexedChunks[key] = true
	}

	// Partition into chunks
	var totalNewBlocks uint64
	for i := 0; i < len(heights); i += int(chunkSize) {
		endIdx := i + int(chunkSize)
		if endIdx > len(heights) {
			endIdx = len(heights)
		}
		chunkHeights := heights[i:endIdx]
		startH := chunkHeights[0]
		endH := chunkHeights[len(chunkHeights)-1]

		chunkKey := fmt.Sprintf("%d-%d", startH, endH)
		if indexedChunks[chunkKey] {
			continue
		}

		// Read blocks for this chunk
		var carEntries []BlockEntry
		var firstBlock, lastBlock BlockSummary

		for idx, h := range chunkHeights {
			blockPath := filepath.Join(blocksDir, fmt.Sprintf("%d.json", h))
			raw, err := os.ReadFile(blockPath)
			if err != nil {
				return nil, fmt.Errorf("reading block %s: %w", blockPath, err)
			}

			var bs BlockSummary
			if err := json.Unmarshal(raw, &bs); err != nil {
				return nil, fmt.Errorf("parsing block summary %s: %w", blockPath, err)
			}

			if idx == 0 {
				firstBlock = bs
			}
			if idx == len(chunkHeights)-1 {
				lastBlock = bs
			}

			cid := ComputeRawCIDv1(raw)
			carEntries = append(carEntries, BlockEntry{
				Height: h,
				CID:    cid,
				Data:   raw,
			})
		}

		// The root CID for the CAR file is the CID of the last block in the chunk
		rootCID := carEntries[len(carEntries)-1].CID

		carFileName := fmt.Sprintf("synthos-blocks-%07d-%07d.car", startH, endH)
		carPath := filepath.Join(outDir, carFileName)

		carFile, err := os.OpenFile(carPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, fmt.Errorf("creating car file %s: %w", carPath, err)
		}

		if err := WriteCarArchive(carFile, rootCID, carEntries); err != nil {
			carFile.Close()
			return nil, fmt.Errorf("writing car archive: %w", err)
		}
		carFile.Close()

		// Compute SHA256 and CAR CID of the resulting CAR file
		carBytes, err := os.ReadFile(carPath)
		if err != nil {
			return nil, err
		}
		carHash := sha256.Sum256(carBytes)
		carCID := ComputeRawCIDv1(carBytes)

		meta := ChunkMeta{
			StartHeight:    startH,
			EndHeight:      endH,
			BlockCount:     len(chunkHeights),
			Engine:         "synthos-legacy",
			CARFile:        carFileName,
			CARSizeBytes:   int64(len(carBytes)),
			CARSha256:      hex.EncodeToString(carHash[:]),
			CID:            carCID.Str,
			FirstBlockHash: firstBlock.Hash,
			LastBlockHash:  lastBlock.Hash,
			FirstStateRoot: firstBlock.Header.StateRoot,
			LastStateRoot:  lastBlock.Header.StateRoot,
			ArchivedAt:     time.Now().UTC().Format(time.RFC3339),
		}

		index.Chunks = append(index.Chunks, meta)
		totalNewBlocks += uint64(len(chunkHeights))
		fmt.Printf("Archived chunk %d..%d -> %s (CID: %s, size: %d bytes)\n", startH, endH, carFileName, carCID.Str, len(carBytes))
	}

	// Update index summary
	index.LastUpdated = time.Now().UTC().Format(time.RFC3339)
	index.TotalChunks = len(index.Chunks)

	var maxIndexed uint64
	for _, c := range index.Chunks {
		if c.EndHeight > maxIndexed {
			maxIndexed = c.EndHeight
		}
	}
	index.TotalBlocksIndexed = maxIndexed + 1

	indexJSON, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshaling index: %w", err)
	}
	if err := os.WriteFile(indexFile, append(indexJSON, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("writing index file %s: %w", indexFile, err)
	}

	return index, nil
}

func loadOrCreateIndex(path, chainID string) *ArchiveIndex {
	raw, err := os.ReadFile(path)
	if err == nil {
		var idx ArchiveIndex
		if err := json.Unmarshal(raw, &idx); err == nil && idx.ChainID != "" {
			return &idx
		}
	}
	return &ArchiveIndex{
		Format:  "synthos-filecoin-archive-v1",
		ChainID: chainID,
		Chunks:  []ChunkMeta{},
	}
}
