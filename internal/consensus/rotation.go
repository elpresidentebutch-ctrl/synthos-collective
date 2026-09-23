package consensus

import "time"

// ExpectedProposer returns the validator ID that is scheduled to propose
// block `height`, `round` rounds into that height's window, under a plain
// round-robin rotation over `validators` (in the exact order given --
// every node in a rotation-enabled deployment must be configured with the
// identical order, e.g. straight from NodeConfig.Validators, for this to
// agree network-wide).
//
// round 0 is the height's normal, first-in-line producer. round 1, 2, ...
// are what a later validator becomes responsible for if the height still
// hasn't finalized after its round-0 producer's window passed -- see
// CurrentRound. The formula folds height and round into the same rotation
// so consecutive heights don't all start on the same validator's round 0
// (height 1 round 0 and height 2 round 0 are different validators),
// spreading load evenly rather than letting one validator's downtime
// always fall on the same neighbor.
//
// Returns "" if validators is empty. Never panics on a negative round
// (treated as 0).
func ExpectedProposer(validators []string, height uint64, round int) string {
	n := len(validators)
	if n == 0 {
		return ""
	}
	if round < 0 {
		round = 0
	}
	idx := (height + uint64(round)) % uint64(n)
	return validators[idx]
}

// CurrentRound turns "how long has this height's window been open" into a
// round number: round 0 for the first roundInterval, round 1 for the
// second, and so on. This is deliberately just elapsed-time arithmetic
// with no coordination between nodes -- each node computes its own
// estimate from its own clock, which is why the node.Node vote-lock (not
// this function) is what actually keeps rotation-enabled multi-producer
// failover safe: two nodes disagreeing by a fraction of a second about
// exactly when a round boundary passed can only ever cost a stalled
// attempt, never a conflicting finalization. See node.Node.
// SetProducerRotationLockTimeout's doc comment.
//
// roundInterval <= 0 always returns round 0 (never divides by zero or a
// negative duration).
func CurrentRound(elapsed, roundInterval time.Duration) int {
	if roundInterval <= 0 || elapsed <= 0 {
		return 0
	}
	return int(elapsed / roundInterval)
}

// ShouldPropose combines ExpectedProposer and CurrentRound into the single
// decision a rotation-enabled node's producer loop makes every tick: is it
// currently MY turn to attempt proposing the next block, and if not, who is
// it instead (useful for liveness/reputation bookkeeping such as Engine.
// NoteMissedSlot -- itself not a safety mechanism, see that method's doc
// comment).
//
// targetHeight is currentHeight+1 (the only height a producer loop should
// ever be attempting). elapsedSinceHeightChange is how long it's been,
// by this node's own clock, since currentHeight last advanced -- pass 0
// (or any non-positive value) right after a height change to mean "just
// started watching this height, round 0". This function does no I/O and
// keeps no state itself; the caller (a producer loop) is responsible for
// tracking elapsedSinceHeightChange, typically by resetting a local
// timestamp whenever it observes the chain's height move.
func ShouldPropose(selfID string, validators []string, currentHeight uint64, elapsedSinceHeightChange, roundInterval time.Duration) (mine bool, targetHeight uint64, expectedProposer string, round int) {
	targetHeight = currentHeight + 1
	round = CurrentRound(elapsedSinceHeightChange, roundInterval)
	expectedProposer = ExpectedProposer(validators, targetHeight, round)
	mine = expectedProposer != "" && expectedProposer == selfID
	return mine, targetHeight, expectedProposer, round
}
