package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// BlockEntry represents a block packaged inside a CAR archive.
type BlockEntry struct {
	Height uint64 `json:"height"`
	CID    CID    `json:"cid"`
	Data   []byte `json:"-"`
}

// WriteCarArchive writes a standard CAR v1 file containing block entries.
func WriteCarArchive(w io.Writer, rootCID CID, entries []BlockEntry) error {
	headerBytes := encodeCarHeader(rootCID.RawBytes)

	// Prepend varint length of header
	var lenBuf [10]byte
	n := binary.PutUvarint(lenBuf[:], uint64(len(headerBytes)))
	if _, err := w.Write(lenBuf[:n]); err != nil {
		return fmt.Errorf("writing car header length: %w", err)
	}
	if _, err := w.Write(headerBytes); err != nil {
		return fmt.Errorf("writing car header: %w", err)
	}

	// Write each block entry
	for _, entry := range entries {
		cidBytes := entry.CID.RawBytes
		sectionLen := uint64(len(cidBytes) + len(entry.Data))

		n := binary.PutUvarint(lenBuf[:], sectionLen)
		if _, err := w.Write(lenBuf[:n]); err != nil {
			return fmt.Errorf("writing section length for height %d: %w", entry.Height, err)
		}
		if _, err := w.Write(cidBytes); err != nil {
			return fmt.Errorf("writing cid for height %d: %w", entry.Height, err)
		}
		if _, err := w.Write(entry.Data); err != nil {
			return fmt.Errorf("writing data for height %d: %w", entry.Height, err)
		}
	}
	return nil
}

// ReadCarArchive reads and unpacks a CAR v1 file.
func ReadCarArchive(r io.Reader) (CID, []BlockEntry, error) {
	// Read header varint length
	headerLen, err := binary.ReadUvarint(asByteReader(r))
	if err != nil {
		return CID{}, nil, fmt.Errorf("reading car header length: %w", err)
	}
	if headerLen == 0 || headerLen > 65536 {
		return CID{}, nil, fmt.Errorf("invalid car header length: %d", headerLen)
	}

	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(r, headerBytes); err != nil {
		return CID{}, nil, fmt.Errorf("reading car header: %w", err)
	}

	rootCID, err := parseCarHeader(headerBytes)
	if err != nil {
		return CID{}, nil, fmt.Errorf("parsing car header: %w", err)
	}

	var entries []BlockEntry
	bufR := asByteReader(r)

	for {
		sectionLen, err := binary.ReadUvarint(bufR)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return rootCID, nil, fmt.Errorf("reading section length: %w", err)
		}
		if sectionLen == 0 {
			break
		}

		sectionBytes := make([]byte, sectionLen)
		if _, err := io.ReadFull(r, sectionBytes); err != nil {
			return rootCID, nil, fmt.Errorf("reading section data: %w", err)
		}

		// Extract CID (CIDv1 is 36 bytes for raw sha2-256)
		if len(sectionBytes) < 36 {
			return rootCID, nil, fmt.Errorf("corrupt section data: length %d too short", len(sectionBytes))
		}

		// Check if first byte is CIDv1 (0x01)
		var cidLen int
		if sectionBytes[0] == 0x01 {
			// [0x01, codec, 0x12, 0x20, <32 bytes>] = 36 bytes for 1-byte codec
			if sectionBytes[1] == 0x55 || sectionBytes[1] == 0x71 {
				cidLen = 36
			} else if sectionBytes[1] == 0x82 && sectionBytes[2] == 0x04 {
				// 2-byte varint codec (0x0202) -> 37 bytes
				cidLen = 37
			} else {
				cidLen = 36
			}
		} else {
			// Fallback CIDv0 (34 bytes multihash)
			cidLen = 34
		}

		cidRaw := sectionBytes[:cidLen]
		data := sectionBytes[cidLen:]

		cidParsed, err := ParseCID("b" + base32IPFS.EncodeToString(cidRaw))
		if err != nil {
			cidParsed = CID{RawBytes: cidRaw, Str: "b" + base32IPFS.EncodeToString(cidRaw)}
		}

		entries = append(entries, BlockEntry{
			CID:  cidParsed,
			Data: data,
		})
	}

	return rootCID, entries, nil
}

// encodeCarHeader creates canonical DAG-CBOR header bytes.
func encodeCarHeader(rootCIDBytes []byte) []byte {
	var b []byte
	b = append(b, 0xa2) // Map of 2 keys

	// 1. "roots"
	b = append(b, 0x65)
	b = append(b, []byte("roots")...)
	b = append(b, 0x81) // Array of 1
	b = append(b, 0xd8, 0x2a) // Tag 42 (CID)

	cidWithPrefix := append([]byte{0x00}, rootCIDBytes...)
	b = append(b, 0x58, byte(len(cidWithPrefix)))
	b = append(b, cidWithPrefix...)

	// 2. "version"
	b = append(b, 0x67)
	b = append(b, []byte("version")...)
	b = append(b, 0x01) // uint 1

	return b
}

// parseCarHeader extracts root CID from DAG-CBOR header bytes.
func parseCarHeader(b []byte) (CID, error) {
	// Look for Tag 42: 0xd8, 0x2a
	tagIdx := bytes.Index(b, []byte{0xd8, 0x2a})
	if tagIdx != -1 && tagIdx+3 < len(b) {
		lenByte := int(b[tagIdx+3])
		start := tagIdx + 4
		if start+lenByte <= len(b) {
			rawTagged := b[start : start+lenByte]
			// Drop leading 0x00 multibase prefix if present
			if len(rawTagged) > 0 && rawTagged[0] == 0x00 {
				rawTagged = rawTagged[1:]
			}
			return CID{
				Version:  CIDv1Type,
				RawBytes: rawTagged,
				Str:      "b" + base32IPFS.EncodeToString(rawTagged),
			}, nil
		}
	}
	return CID{}, errors.New("root CID not found in CAR header")
}

type byteReaderWrapper struct {
	io.Reader
}

func (b *byteReaderWrapper) ReadByte() (byte, error) {
	var buf [1]byte
	_, err := b.Read(buf[:])
	return buf[0], err
}

func asByteReader(r io.Reader) io.ByteReader {
	if br, ok := r.(io.ByteReader); ok {
		return br
	}
	return &byteReaderWrapper{Reader: r}
}
