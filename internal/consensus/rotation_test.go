package consensus

import (
	"testing"
	"time"
)

func TestExpectedProposer_RoundRobinByHeightAndRound(t *testing.T) {
	vals := []string{"a", "b", "c"}

	tests := []struct {
		height uint64
		round  int
		want   string
	}{
		{height: 1, round: 0, want: "b"}, // (1+0)%3 = 1 -> "b"
		{height: 2, round: 0, want: "c"}, // (2+0)%3 = 2 -> "c"
		{height: 3, round: 0, want: "a"}, // (3+0)%3 = 0 -> "a"
		{height: 1, round: 1, want: "c"}, // (1+1)%3 = 2 -> "c" (next in line after "b" missed round 0)
		{height: 1, round: 2, want: "a"}, // (1+2)%3 = 0 -> "a"
		{height: 1, round: 3, want: "b"}, // wraps back around to round 0's producer
	}
	for _, tc := range tests {
		if got := ExpectedProposer(vals, tc.height, tc.round); got != tc.want {
			t.Errorf("ExpectedProposer(height=%d, round=%d) = %q, want %q", tc.height, tc.round, got, tc.want)
		}
	}
}

func TestExpectedProposer_ConsecutiveHeightsDontShareRound0Producer(t *testing.T) {
	vals := []string{"a", "b", "c"}
	seen := map[string]bool{}
	for h := uint64(1); h <= 3; h++ {
		seen[ExpectedProposer(vals, h, 0)] = true
	}
	if len(seen) != 3 {
		t.Fatalf("expected all 3 validators to take round 0 across 3 consecutive heights, got %v", seen)
	}
}

func TestExpectedProposer_EmptyOrNegativeRound(t *testing.T) {
	if got := ExpectedProposer(nil, 5, 0); got != "" {
		t.Fatalf("expected empty string for no validators, got %q", got)
	}
	if got := ExpectedProposer([]string{}, 5, 0); got != "" {
		t.Fatalf("expected empty string for empty validators, got %q", got)
	}
	vals := []string{"a", "b"}
	if got, want := ExpectedProposer(vals, 4, -3), ExpectedProposer(vals, 4, 0); got != want {
		t.Fatalf("negative round should behave like round 0: got %q, want %q", got, want)
	}
}

func TestCurrentRound_StepsWithElapsedTime(t *testing.T) {
	interval := 10 * time.Second
	tests := []struct {
		elapsed time.Duration
		want    int
	}{
		{0, 0},
		{5 * time.Second, 0},
		{9999 * time.Millisecond, 0},
		{10 * time.Second, 1},
		{15 * time.Second, 1},
		{20 * time.Second, 2},
		{100 * time.Second, 10},
	}
	for _, tc := range tests {
		if got := CurrentRound(tc.elapsed, interval); got != tc.want {
			t.Errorf("CurrentRound(elapsed=%s) = %d, want %d", tc.elapsed, got, tc.want)
		}
	}
}

func TestCurrentRound_NeverPanicsOnBadInput(t *testing.T) {
	if got := CurrentRound(5*time.Second, 0); got != 0 {
		t.Fatalf("zero interval should yield round 0, got %d", got)
	}
	if got := CurrentRound(5*time.Second, -1*time.Second); got != 0 {
		t.Fatalf("negative interval should yield round 0, got %d", got)
	}
	if got := CurrentRound(-5*time.Second, 10*time.Second); got != 0 {
		t.Fatalf("negative elapsed should yield round 0, got %d", got)
	}
}

func TestShouldPropose_OnlyTheExpectedProposerIsToldToActEachRound(t *testing.T) {
	vals := []string{"a", "b", "c"}
	interval := 10 * time.Second

	// height 1, round 0 (elapsed < interval) -> expected proposer is "b"
	// (see TestExpectedProposer_RoundRobinByHeightAndRound's table).
	for _, id := range vals {
		mine, target, expected, round := ShouldPropose(id, vals, 0, 2*time.Second, interval)
		if target != 1 {
			t.Fatalf("targetHeight = %d, want 1 (currentHeight+1)", target)
		}
		if round != 0 {
			t.Fatalf("round = %d, want 0", round)
		}
		if expected != "b" {
			t.Fatalf("expected proposer = %q, want %q", expected, "b")
		}
		wantMine := id == "b"
		if mine != wantMine {
			t.Fatalf("ShouldPropose(%q) mine = %v, want %v", id, mine, wantMine)
		}
	}
}

func TestShouldPropose_AdvancesToNextValidatorOncePastRoundWindow(t *testing.T) {
	vals := []string{"a", "b", "c"}
	interval := 10 * time.Second

	// Still height 1, but now well past round 0's window: round becomes 1,
	// and the schedule mechanically hands the height to the next validator
	// in rotation ("c") with no explicit "producer is down" signal needed.
	mine, _, expected, round := ShouldPropose("c", vals, 0, 15*time.Second, interval)
	if round != 1 {
		t.Fatalf("round = %d, want 1", round)
	}
	if expected != "c" {
		t.Fatalf("expected proposer after round advance = %q, want %q", expected, "c")
	}
	if !mine {
		t.Fatal("expected ShouldPropose to report it's now c's turn")
	}

	// "b" (round 0's producer, presumably the one that's down) must no
	// longer be told it's its turn once the round has advanced past it.
	if mine, _, _, _ := ShouldPropose("b", vals, 0, 15*time.Second, interval); mine {
		t.Fatal("round-0 producer must not still be told it's its turn after the round advanced")
	}
}

func TestShouldPropose_EmptyValidatorsNeverClaimsAnyonesTurn(t *testing.T) {
	if mine, _, expected, _ := ShouldPropose("a", nil, 0, time.Second, time.Second); mine || expected != "" {
		t.Fatalf("ShouldPropose with no validators = (mine=%v expected=%q), want (false, \"\")", mine, expected)
	}
}
