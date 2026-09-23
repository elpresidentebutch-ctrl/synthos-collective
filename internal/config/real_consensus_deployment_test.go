package config_test

import (
	"testing"

	"synthos-collective/internal/config"
)

// TestRealConsensusDeploymentConfigsAgree loads the actual deployed config
// files for the 3 validators wired for real multi-party consensus
// (synthos-validator-12, synthos-validator-13, synthos-render-validator-1 --
// see config/render-validator-12.json, config/render-validator-13.json,
// config/render-node.json) and checks the properties that actually matter
// for that wiring to work correctly in production:
//
//   - All three agree on the exact same 3-node validator roster (a
//     mismatch here would mean the nodes disagree about RequiredForFinality
//     and could either never reach quorum, or -- worse -- reach a quorum
//     that isn't real consensus on one of the nodes).
//   - All three carry the same 3 real public keys for that roster, so every
//     node can verify every other node's proposer signature and votes.
//   - Every validator has consensus_peers configured -- the OTHER two, never
//     itself -- so that any one of them can act as producer for a height
//     under SYNTHOS_PRODUCER_ROTATION (see internal/consensus/rotation.go
//     and cmd/synthosd/main.go's startBlockProducer). Before rotation
//     existed, only synthos-validator-12 (the sole hardcoded producer) had
//     consensus_peers; now that any validator may become the scheduled
//     producer for a given height/round, every validator needs a real path
//     to ask the other two for their votes, regardless of which one(s)
//     currently have SYNTHOS_BLOCK_PRODUCER=true set live.
//
// This exists because a typo or copy-paste mistake in these JSON files
// would only otherwise surface once deployed against the live chain, which
// is exactly the kind of mistake this test is meant to catch first.
func TestRealConsensusDeploymentConfigsAgree(t *testing.T) {
	v12, err := config.LoadNodeConfig("../../config/render-validator-12.json")
	if err != nil {
		t.Fatalf("loading render-validator-12.json: %v", err)
	}
	v13, err := config.LoadNodeConfig("../../config/render-validator-13.json")
	if err != nil {
		t.Fatalf("loading render-validator-13.json: %v", err)
	}
	rpcNode, err := config.LoadNodeConfig("../../config/render-node.json")
	if err != nil {
		t.Fatalf("loading render-node.json: %v", err)
	}

	wantRoster := []string{"synthos-validator-12", "synthos-validator-13", "synthos-render-validator-1"}
	wantKeys := map[string]string{
		"synthos-validator-12":       "0x5528a57c397d8539d88b3ef5dbecdec3ec8ca261afd2eb44e9992098c652c355",
		"synthos-validator-13":       "0xc0985da91bf00d997d6f9731e681af62bfa1ae13b10ea67e0e7d901b45ae38f1",
		"synthos-render-validator-1": "0x14d371a7ed892a1fe5a16413f483c3730b3d52ef0ac8b6663e70389fae10e4f5",
	}
	selfURL := map[string]string{
		"synthos-validator-12":       "https://synthos-validator-12.onrender.com",
		"synthos-validator-13":       "https://synthos-validator-13.onrender.com",
		"synthos-render-validator-1": "https://synthos-rpc.onrender.com",
	}

	for _, tc := range []struct {
		name string
		cfg  *config.NodeConfig
	}{
		{"synthos-validator-12", v12},
		{"synthos-validator-13", v13},
		{"synthos-render-validator-1", rpcNode},
	} {
		if got := tc.cfg.Validators; !equalStringSets(got, wantRoster) {
			t.Errorf("%s: validators = %v, want %v", tc.name, got, wantRoster)
		}
		if got := tc.cfg.TrustedValidators; !equalStringSets(got, wantRoster) {
			t.Errorf("%s: trusted_validators = %v, want %v", tc.name, got, wantRoster)
		}
		for id, wantKey := range wantKeys {
			if id == tc.cfg.NodeID {
				continue // a node's own key isn't required in its own peer_keys
			}
			if got := tc.cfg.PeerKeys[id]; got != wantKey {
				t.Errorf("%s: peer_keys[%q] = %q, want %q", tc.name, id, got, wantKey)
			}
		}

		// Every validator needs a real path to ask each OTHER validator for
		// a vote (rotation means any of them may become producer for a
		// given height), and must never list itself (a node doesn't ask
		// itself over HTTP for a vote it already cast via SelfVote).
		if len(tc.cfg.ConsensusPeers) != 2 {
			t.Errorf("%s: consensus_peers = %v, want exactly 2 (the other two validators)", tc.name, tc.cfg.ConsensusPeers)
		}
		self := selfURL[tc.cfg.NodeID]
		for _, peer := range tc.cfg.ConsensusPeers {
			if peer == self {
				t.Errorf("%s: consensus_peers must not include itself (%s): %v", tc.name, self, tc.cfg.ConsensusPeers)
			}
		}
	}
}

func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := make(map[string]bool, len(a))
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}
