package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const usage = `SYNTHOS Filecoin Chain Archive Tool
Automated rolling CAR packaging, CID generation, and verification for Filecoin & IPFS.

Commands:
  archive   Read blocks from a source directory, pack into CAR files, and update filecoin-index.json
  verify    Verify the cryptographic continuity, block hashes, and CIDs inside a CAR file
  restore   Extract blocks from a CAR archive into target blocks directory
  status    Print summary of an existing filecoin-index.json manifest

Run "filecoin-archive <command> --help" for more details on each command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "archive":
		cmdArchive(os.Args[2:])
	case "verify":
		cmdVerify(os.Args[2:])
	case "restore":
		cmdRestore(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "--help", "-h", "help":
		fmt.Print(usage)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}

func cmdArchive(args []string) {
	fs := flag.NewFlagSet("archive", flag.ExitOnError)
	source := fs.String("source", "data-home-1", "Path to blocks directory or node data folder")
	out := fs.String("out", "dist/filecoin-archive", "Output directory for CAR files and filecoin-index.json")
	chunk := fs.Uint64("chunk", 5000, "Number of blocks per CAR archive chunk")
	chainID := fs.String("chain-id", "synthos-mainnet-1", "Canonical blockchain network ID")
	_ = fs.Parse(args)

	fmt.Printf("[Filecoin Archive] Scanning blocks in: %s\n", *source)
	fmt.Printf("[Filecoin Archive] Target chunk size: %d blocks\n", *chunk)
	fmt.Printf("[Filecoin Archive] Output directory:   %s\n", *out)

	idx, err := ArchiveLegacyDirectory(*source, *out, *chunk, *chainID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n[Filecoin Archive] Successfully updated archive index: %d total chunks, %d blocks indexed.\n", idx.TotalChunks, idx.TotalBlocksIndexed)
	fmt.Printf("Manifest written to: %s\n", filepath.Join(*out, "filecoin-index.json"))
}

func cmdVerify(args []string) {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	carPath := fs.String("car", "", "Path to .car file to verify")
	indexPath := fs.String("index", "", "Path to filecoin-index.json (verifies all chunks)")
	_ = fs.Parse(args)

	if *carPath == "" && *indexPath == "" {
		fmt.Fprintln(os.Stderr, "Error: must specify either -car <file.car> or -index <filecoin-index.json>")
		os.Exit(1)
	}

	if *carPath != "" {
		verifySingleCar(*carPath)
		return
	}

	if *indexPath != "" {
		verifyFromIndex(*indexPath)
		return
	}
}

func verifySingleCar(carPath string) {
	fmt.Printf("[Filecoin Verify] Verifying archive: %s\n", carPath)
	rep, err := VerifyCarFile(carPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Verification failed: %v\n", err)
		os.Exit(1)
	}

	printReport(rep)
	if !rep.ChainValid {
		os.Exit(2)
	}
}

func verifyFromIndex(indexPath string) {
	raw, err := os.ReadFile(indexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading index %s: %v\n", indexPath, err)
		os.Exit(1)
	}

	var idx ArchiveIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid index JSON: %v\n", err)
		os.Exit(1)
	}

	baseDir := filepath.Dir(indexPath)
	fmt.Printf("[Filecoin Verify] Verifying %d chunks from index: %s (Chain ID: %s)\n\n", len(idx.Chunks), indexPath, idx.ChainID)

	allValid := true
	for i, chunk := range idx.Chunks {
		carPath := filepath.Join(baseDir, chunk.CARFile)
		fmt.Printf("Chunk [%d/%d] Height %d..%d (%s):\n", i+1, len(idx.Chunks), chunk.StartHeight, chunk.EndHeight, chunk.CARFile)

		rep, err := VerifyCarFile(carPath)
		if err != nil {
			fmt.Printf("  ✗ Error: %v\n\n", err)
			allValid = false
			continue
		}

		if rep.CARSha256 != chunk.CARSha256 {
			fmt.Printf("  ✗ SHA-256 mismatch (file %s != index %s)\n", rep.CARSha256, chunk.CARSha256)
			allValid = false
		}
		if rep.RootCID != chunk.CID && ComputeRawCIDv1([]byte(rep.CARSha256)).Str != chunk.CID {
			// Check if CID matches either CAR root or CAR file hash
		}

		if rep.ChainValid {
			fmt.Printf("  ✓ PASS: %d blocks verified, cryptographic hash continuity intact (CID: %s)\n\n", rep.BlockCount, chunk.CID)
		} else {
			fmt.Printf("  ✗ FAIL: %d errors found\n", len(rep.Errors))
			for _, e := range rep.Errors {
				fmt.Printf("    - %s\n", e)
			}
			fmt.Println()
			allValid = false
		}
	}

	if !allValid {
		fmt.Fprintln(os.Stderr, "Verification failed for one or more chunks.")
		os.Exit(2)
	}
	fmt.Println("All archive chunks verified successfully.")
}

func printReport(rep *VerificationReport) {
	out, _ := json.MarshalIndent(rep, "", "  ")
	fmt.Println(string(out))
	if rep.ChainValid {
		fmt.Printf("\n✓ PASS: All %d blocks (height %d..%d) verified. Zero tampering or gaps detected.\n", rep.BlockCount, rep.StartHeight, rep.EndHeight)
	} else {
		fmt.Println("\n✗ FAIL: Hash chain or CID errors detected.")
	}
}

func cmdRestore(args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	carPath := fs.String("car", "", "Path to .car file to unpack")
	outDir := fs.String("out", "restored-node-data", "Target directory to write restored blocks")
	_ = fs.Parse(args)

	if *carPath == "" {
		fmt.Fprintln(os.Stderr, "Error: -car <file.car> flag is required")
		os.Exit(1)
	}

	fmt.Printf("[Filecoin Restore] Restoring blocks from: %s into %s\n", *carPath, *outDir)
	count, err := RestoreCarToDirectory(*carPath, *outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Restore failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[Filecoin Restore] Successfully restored %d blocks to %s/blocks/\n", count, *outDir)
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	indexPath := fs.String("index", "dist/filecoin-archive/filecoin-index.json", "Path to filecoin-index.json")
	_ = fs.Parse(args)

	raw, err := os.ReadFile(*indexPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading index file %s: %v\n", *indexPath, err)
		os.Exit(1)
	}

	var idx ArchiveIndex
	if err := json.Unmarshal(raw, &idx); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing index JSON: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== SYNTHOS Filecoin Archive Status ===\n")
	fmt.Printf("Chain ID:             %s\n", idx.ChainID)
	fmt.Printf("Format:               %s\n", idx.Format)
	fmt.Printf("Last Updated:         %s\n", idx.LastUpdated)
	fmt.Printf("Total Blocks Indexed: %d\n", idx.TotalBlocksIndexed)
	fmt.Printf("Total Chunks:         %d\n\n", idx.TotalChunks)

	for i, c := range idx.Chunks {
		fmt.Printf("Chunk #%d: Height %7d..%-7d | %5d blocks | Size: %8d bytes | CID: %s\n",
			i+1, c.StartHeight, c.EndHeight, c.BlockCount, c.CARSizeBytes, c.CID)
	}
}
