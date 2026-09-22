package main

import (
	"reflect"
	"testing"

	"synthos-collective/internal/config"
)

// TestResolveConsensusValidators_ConfigDeployAheadOfTokenStaysInert is the
// core rollout-safety property this whole feature depends on: deploying new
// code AND a config file naming the real 3-validator roster must NOT, by
// itself, change what this node requires to finalize a block. Only turning
// on SYNTHOS_CONSENSUS_TOKEN (consensusEnabled=true) should do that. Without
// this, updating config/render-validator-12.json before every validator is
// actually ready to vote would silently double this node's own local quorum
// requirement and halt block production.
func TestResolveConsensusValidators_ConfigDeployAheadOfTokenStaysInert(t *testing.T) {
	cfg := &config.NodeConfig{
		IsValidator: true,
		Validators:  []string{"synthos-validator-12", "synthos-validator-13", "synthos-render-validator-1"},
	}

	got := resolveConsensusValidators(cfg, false /* consensusEnabled */, "synthos-validator-12")
	want := []string{"synthos-validator-12"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with token unset: validators = %v, want %v (must fall back to self-only regardless of cfg.Validators)", got, want)
	}
}

// TestResolveConsensusValidators_TokenEnabledUsesRealRoster proves the flip
// side: once the token is turned on, the real multi-validator roster from
// config is actually used, giving the Engine its real 2-of-3 threshold.
func TestResolveConsensusValidators_TokenEnabledUsesRealRoster(t *testing.T) {
	cfg := &config.NodeConfig{
		IsValidator: true,
		Validators:  []string{"synthos-validator-12", "synthos-validator-13", "synthos-render-validator-1"},
	}

	got := resolveConsensusValidators(cfg, true /* consensusEnabled */, "synthos-validator-12")
	want := cfg.Validators
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("with token set: validators = %v, want %v", got, want)
	}
}

// TestResolveConsensusValidators_NonValidatorNodeUnaffected proves a
// read-only, non-validator deployment (cfg.IsValidator=false) is
// unaffected either way -- it never gets a self-fallback roster.
func TestResolveConsensusValidators_NonValidatorNodeUnaffected(t *testing.T) {
	cfg := &config.NodeConfig{IsValidator: false}
	for _, consensusEnabled := range []bool{true, false} {
		if got := resolveConsensusValidators(cfg, consensusEnabled, "some-id"); len(got) != 0 {
			t.Fatalf("consensusEnabled=%v: validators = %v, want empty for a non-validator node", consensusEnabled, got)
		}
	}
}

// TestResolveChainQuorum_StaysAtOneUntilConsensusEnabled proves the other
// half of rollout safety: Chain's own independently-enforced quorum stays
// at 1 (today's exact behavior) even once trusted_validators names all 3
// real validators in config, right up until consensusEnabled flips true.
func TestResolveChainQuorum_StaysAtOneUntilConsensusEnabled(t *testing.T) {
	trusted := []string{"synthos-validator-12", "synthos-validator-13", "synthos-render-validator-1"}
	selfOnly := []string{"synthos-validator-12"}

	gotValidators, gotQuorum := resolveChainQuorum(trusted, selfOnly, false, 2)
	if !reflect.DeepEqual(gotValidators, trusted) {
		t.Fatalf("chainValidators = %v, want %v (Chain should still recognize all 3 keys)", gotValidators, trusted)
	}
	if gotQuorum != 1 {
		t.Fatalf("chainQuorum = %d, want 1 (must not require real quorum until consensus is deliberately enabled)", gotQuorum)
	}
}

// TestResolveChainQuorum_RequiresRealThresholdOnceEnabled proves the flip
// side: once enabled, Chain requires the genuine engineQuorum threshold.
func TestResolveChainQuorum_RequiresRealThresholdOnceEnabled(t *testing.T) {
	trusted := []string{"synthos-validator-12", "synthos-validator-13", "synthos-render-validator-1"}

	_, gotQuorum := resolveChainQuorum(trusted, trusted, true, 2)
	if gotQuorum != 2 {
		t.Fatalf("chainQuorum = %d, want 2 (the real 2-of-3 threshold)", gotQuorum)
	}
}

// TestResolveChainQuorum_NoTrustedValidatorsUnchanged proves a deployment
// that never sets trusted_validators at all (most nodes: 11, 14, 15) keeps
// its exact original behavior -- chainValidators/chainQuorum come straight
// from its own validators/engineQuorum, regardless of consensusEnabled.
func TestResolveChainQuorum_NoTrustedValidatorsUnchanged(t *testing.T) {
	own := []string{"synthos-validator-11"}
	gotValidators, gotQuorum := resolveChainQuorum(nil, own, false, 1)
	if !reflect.DeepEqual(gotValidators, own) {
		t.Fatalf("chainValidators = %v, want %v", gotValidators, own)
	}
	if gotQuorum != 1 {
		t.Fatalf("chainQuorum = %d, want 1", gotQuorum)
	}
}
