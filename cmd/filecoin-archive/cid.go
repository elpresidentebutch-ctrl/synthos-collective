package main

import (
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Base32 lowercase encoding without padding (RFC 4648), standard for IPFS CIDv1.
var base32IPFS = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

const (
	CIDv1Type          = 0x01
	CodecRaw           = 0x55
	CodecCar           = 0x0202 // multicodec for CAR files
	MultihashSha2_256  = 0x12
	MultihashDigestLen = 32
)

// CID represents an IPFS / Filecoin Content Identifier.
type CID struct {
	Version   uint64
	Codec     uint64
	HashType  uint64
	Digest    []byte
	Str       string
	RawBytes  []byte
}

// ComputeRawCIDv1 computes the standard CIDv1 (raw binary + sha2-256) for data.
// Prefix format is always "bafkrei..."
func ComputeRawCIDv1(data []byte) CID {
	h := sha256.Sum256(data)
	return FromSha256(CodecRaw, h[:])
}

// FromSha256 creates a CIDv1 from an existing 32-byte sha256 digest.
func FromSha256(codec uint64, digest []byte) CID {
	if len(digest) != 32 {
		panic("digest must be 32 bytes")
	}

	// Build raw multihash binary: [0x12, 0x20, <32 bytes>]
	mh := make([]byte, 2+32)
	mh[0] = MultihashSha2_256
	mh[1] = MultihashDigestLen
	copy(mh[2:], digest)

	// CIDv1 binary: [0x01, <codec varint>, <multihash>]
	var cidBytes []byte
	cidBytes = append(cidBytes, CIDv1Type)
	if codec == CodecRaw {
		cidBytes = append(cidBytes, byte(CodecRaw))
	} else if codec == CodecCar {
		// Varint for 0x0202 is [0x82, 0x04]
		cidBytes = append(cidBytes, 0x82, 0x04)
	} else {
		cidBytes = append(cidBytes, byte(codec))
	}
	cidBytes = append(cidBytes, mh...)

	str := "b" + base32IPFS.EncodeToString(cidBytes)

	return CID{
		Version:  CIDv1Type,
		Codec:    codec,
		HashType: MultihashSha2_256,
		Digest:   digest,
		Str:      str,
		RawBytes: cidBytes,
	}
}

// ParseCID parses a string CID (either bafy... or bafkrei... or hex) into CID.
func ParseCID(s string) (CID, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return CID{}, errors.New("empty CID string")
	}
	if strings.HasPrefix(s, "b") {
		raw, err := base32IPFS.DecodeString(s[1:])
		if err != nil {
			return CID{}, fmt.Errorf("decoding base32 CID: %w", err)
		}
		if len(raw) < 4 {
			return CID{}, errors.New("CID too short")
		}
		if raw[0] != CIDv1Type {
			return CID{}, fmt.Errorf("unsupported CID version: %d", raw[0])
		}
		// Extract digest from tail (32 bytes)
		if len(raw) < 34 {
			return CID{}, errors.New("CID multihash payload too short")
		}
		digest := raw[len(raw)-32:]
		return CID{
			Version:  CIDv1Type,
			Digest:   digest,
			Str:      s,
			RawBytes: raw,
		}, nil
	}
	return CID{}, fmt.Errorf("unsupported CID multibase prefix: %q", s)
}

// DigestHex returns the 32-byte hash formatted as lowercase hex string.
func (c CID) DigestHex() string {
	return hex.EncodeToString(c.Digest)
}
