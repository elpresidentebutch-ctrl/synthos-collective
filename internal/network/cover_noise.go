package network

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	MessageCoverNoise = "cover_noise"
	TopicCoverNoise   = "privacy/cover-noise"

	CoverNoiseDomain        = "synthos.cover_noise.v1"
	MaxCoverNoisePaddingLen = 4096
)

var (
	ErrNotCoverNoise       = errors.New("not a cover-noise envelope")
	ErrBadCoverNoiseDomain = errors.New("bad cover-noise domain")
	ErrBadCoverNoiseScope  = errors.New("bad cover-noise scope")
	ErrCoverNoiseTooLarge  = errors.New("cover-noise padding too large")
)

// CoverNoisePayload is authenticated privacy cover traffic.
//
// It is intentionally transport-only: receivers may verify and count/drop it,
// but must never feed it into consensus, governance, DEX, or canonical state.
type CoverNoisePayload struct {
	Domain    string    `json:"domain"`
	Scope     string    `json:"scope"`
	NoiseID   string    `json:"noise_id"`
	Padding   string    `json:"padding"`
	CreatedAt time.Time `json:"created_at"`
}

func (p CoverNoisePayload) Validate() error {
	if p.Domain != CoverNoiseDomain {
		return ErrBadCoverNoiseDomain
	}
	if p.Scope != "local_opt_in" && p.Scope != "local_browser" && p.Scope != "testnet" {
		return ErrBadCoverNoiseScope
	}
	if p.NoiseID == "" || p.CreatedAt.IsZero() {
		return ErrMissingField
	}
	if len(p.Padding) > MaxCoverNoisePaddingLen {
		return ErrCoverNoiseTooLarge
	}
	return nil
}

// DecodeCoverNoise verifies envelope routing and payload domain separation.
func DecodeCoverNoise(env Envelope) (CoverNoisePayload, error) {
	var payload CoverNoisePayload
	if env.MessageType != MessageCoverNoise {
		return payload, ErrNotCoverNoise
	}
	if env.Topic != "" && env.Topic != TopicCoverNoise {
		return payload, ErrBadCoverNoiseDomain
	}
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		return payload, err
	}
	if err := payload.Validate(); err != nil {
		return payload, err
	}
	return payload, nil
}
