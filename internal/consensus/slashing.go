package consensus

import (
	"fmt"
	"sync"
	"time"
)

// SlashingEvent represents a validator misbehavior event
type SlashingEvent struct {
	ValidatorID string
	EventType   SlashingType
	BlockHeight uint64
	Timestamp   time.Time
	Evidence    string // Description of the violation
}

// SlashingType represents the kind of validator misbehavior
type SlashingType string

const (
	DoubleSigning  SlashingType = "double_signing"  // Signed two blocks at same height
	InvalidBlock   SlashingType = "invalid_block"   // Proposed block with invalid txs
	Downtime       SlashingType = "downtime"        // Validator offline for N blocks
	Equivocation   SlashingType = "equivocation"    // Conflicting validator votes
)

// SlashingParams defines penalties for misbehavior
type SlashingParams struct {
	DoubleSignPenalty  uint64        // Tokens slashed for double-signing (e.g., 5% of stake)
	InvalidBlockPenalty uint64       // Tokens slashed for invalid block proposal
	DowntimePenalty    uint64        // Tokens slashed per block missed
	SlashingWindow     uint64        // Number of recent blocks to track for downtime
	JailDuration       time.Duration // How long validator is jailed after slashing
}

// SlashingTracker tracks validator misbehavior and applies penalties
type SlashingTracker struct {
	mu              sync.RWMutex
	params          SlashingParams
	events          []SlashingEvent
	validatorStakes map[string]uint64       // Validator -> stake amount
	jailedUntil     map[string]time.Time    // Validator -> jail end time
	signedBlocks    map[string][]uint64     // Validator -> heights of blocks signed (for double-sign detection)
	missedBlocks    map[string]uint64       // Validator -> count of blocks missed in window
}

// NewSlashingTracker creates a new slashing tracker with default penalties
func NewSlashingTracker(params SlashingParams) *SlashingTracker {
	if params.SlashingWindow == 0 {
		params.SlashingWindow = 100 // Default: track last 100 blocks
	}
	if params.JailDuration == 0 {
		params.JailDuration = 24 * time.Hour // Default: 24 hour jail
	}
	return &SlashingTracker{
		params:          params,
		events:          make([]SlashingEvent, 0),
		validatorStakes: make(map[string]uint64),
		jailedUntil:     make(map[string]time.Time),
		signedBlocks:    make(map[string][]uint64),
		missedBlocks:    make(map[string]uint64),
	}
}

// SetValidatorStake records or updates a validator's stake amount
func (st *SlashingTracker) SetValidatorStake(validatorID string, stake uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.validatorStakes[validatorID] = stake
}

// DetectDoubleSigning checks if a validator signed two blocks at the same height
func (st *SlashingTracker) DetectDoubleSigning(validatorID string, blockHeight uint64) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Check if validator already signed at this height
	if heights, exists := st.signedBlocks[validatorID]; exists {
		for _, h := range heights {
			if h == blockHeight {
				return st.recordSlashingLocked(validatorID, DoubleSigning, blockHeight,
					fmt.Sprintf("validator signed multiple blocks at height %d", blockHeight))
			}
		}
	}

	// Record this block signature
	st.signedBlocks[validatorID] = append(st.signedBlocks[validatorID], blockHeight)
	return nil
}

// RecordMissedBlock increments the missed block counter for a validator
func (st *SlashingTracker) RecordMissedBlock(validatorID string) error {
	st.mu.Lock()
	defer st.mu.Unlock()

	st.missedBlocks[validatorID]++

	// Check if validator exceeded downtime threshold
	if st.missedBlocks[validatorID] > 10 { // threshold: 10 blocks in SlashingWindow
		return st.recordSlashingLocked(validatorID, Downtime, 0,
			fmt.Sprintf("validator missed %d blocks", st.missedBlocks[validatorID]))
	}
	return nil
}

// RecordInvalidBlock records a slashing event for an invalid block proposal
func (st *SlashingTracker) RecordInvalidBlock(validatorID string, blockHeight uint64, evidence string) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.recordSlashingLocked(validatorID, InvalidBlock, blockHeight, evidence)
}

// recordSlashingLocked (internal) records a slashing event and reduces validator stake
func (st *SlashingTracker) recordSlashingLocked(validatorID string, eventType SlashingType, blockHeight uint64, evidence string) error {
	// Record the event
	event := SlashingEvent{
		ValidatorID: validatorID,
		EventType:   eventType,
		BlockHeight: blockHeight,
		Timestamp:   time.Now(),
		Evidence:    evidence,
	}
	st.events = append(st.events, event)

	// Calculate penalty based on event type
	var penalty uint64
	switch eventType {
	case DoubleSigning:
		penalty = st.params.DoubleSignPenalty
	case InvalidBlock:
		penalty = st.params.InvalidBlockPenalty
	case Downtime:
		penalty = st.params.DowntimePenalty
	default:
		penalty = 0
	}

	// Apply penalty (slash stake)
	if stake, exists := st.validatorStakes[validatorID]; exists {
		if stake > penalty {
			st.validatorStakes[validatorID] = stake - penalty
		} else {
			st.validatorStakes[validatorID] = 0
		}
	}

	// Jail validator
	st.jailedUntil[validatorID] = time.Now().Add(st.params.JailDuration)

	return fmt.Errorf("validator %s slashed for %s: penalty=%d stake_remaining=%d", 
		validatorID, eventType, penalty, st.validatorStakes[validatorID])
}

// IsJailed checks if a validator is currently jailed
func (st *SlashingTracker) IsJailed(validatorID string) bool {
	st.mu.RLock()
	defer st.mu.RUnlock()

	jailEnd, exists := st.jailedUntil[validatorID]
	if !exists {
		return false
	}
	return time.Now().Before(jailEnd)
}

// GetValidatorStake returns current stake after any slashing
func (st *SlashingTracker) GetValidatorStake(validatorID string) uint64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.validatorStakes[validatorID]
}

// GetSlashingHistory returns all slashing events (read-only)
func (st *SlashingTracker) GetSlashingHistory() []SlashingEvent {
	st.mu.RLock()
	defer st.mu.RUnlock()

	history := make([]SlashingEvent, len(st.events))
	copy(history, st.events)
	return history
}

// CleanupOldSignatures removes signatures older than SlashingWindow to prevent memory bloat
func (st *SlashingTracker) CleanupOldSignatures(currentHeight uint64) {
	st.mu.Lock()
	defer st.mu.Unlock()

	cutoff := int64(currentHeight) - int64(st.params.SlashingWindow)
	if cutoff <= 0 {
		return
	}

	for validatorID, heights := range st.signedBlocks {
		newHeights := make([]uint64, 0)
		for _, h := range heights {
			if int64(h) > cutoff {
				newHeights = append(newHeights, h)
			}
		}
		if len(newHeights) > 0 {
			st.signedBlocks[validatorID] = newHeights
		} else {
			delete(st.signedBlocks, validatorID)
		}
	}

	// Reset missed blocks for new window
	st.missedBlocks = make(map[string]uint64)
}
