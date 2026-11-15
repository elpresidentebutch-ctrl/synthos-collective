package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RestoreCarToDirectory unpacks all blocks from a CAR archive into targetDir/blocks/<height>.json.
func RestoreCarToDirectory(carPath, targetDir string) (int, error) {
	f, err := os.Open(carPath)
	if err != nil {
		return 0, fmt.Errorf("opening car file %s: %w", carPath, err)
	}
	defer f.Close()

	_, entries, err := ReadCarArchive(f)
	if err != nil {
		return 0, fmt.Errorf("unpacking car file: %w", err)
	}

	blocksDir := filepath.Join(targetDir, "blocks")
	if err := os.MkdirAll(blocksDir, 0o755); err != nil {
		return 0, fmt.Errorf("creating blocks dir %s: %w", blocksDir, err)
	}

	restored := 0
	for _, entry := range entries {
		var bs BlockSummary
		if err := json.Unmarshal(entry.Data, &bs); err != nil {
			return restored, fmt.Errorf("decoding block data: %w", err)
		}

		outPath := filepath.Join(blocksDir, fmt.Sprintf("%d.json", bs.Header.Height))
		if err := os.WriteFile(outPath, entry.Data, 0o644); err != nil {
			return restored, fmt.Errorf("writing restored block %s: %w", outPath, err)
		}
		restored++
	}

	return restored, nil
}
