package node

import (
	"encoding/json"
	"strings"
	"testing"

	"synthos-collective/internal/agent"
	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
	synthoscrypto "synthos-collective/internal/crypto"
	"synthos-collective/internal/network"
)

func testNoiseAgent(t *testing.T, id string) (*agent.Agent, synthoscrypto.KeyPair) {
	t.Helper()
	keys, err := synthoscrypto.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	a := agent.NewAgent(id, synthoscrypto.PublicKeyHex(keys.Public), synthoscrypto.PrivateKeyHashHex(keys.Private), id+"-hardware", 100)
	if err := a.AttachKeys(keys); err != nil {
		t.Fatal(err)
	}
	return a, keys
}

func testNoiseNode(t *testing.T, receiver *agent.Agent) *Node {
	t.Helper()
	c, err := chain.NewChain(chain.Genesis{
		ChainID: "cover-noise-test",
		Alloc: map[chain.Address]uint64{
			chain.Address("0x1111111111111111111111111111111111111111"): 1_000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewNode(receiver, c, consensus.NewEngine(1), network.NewMemoryTransport().NodeTransport(receiver.Identity.AgentID))
}

func TestCoverNoiseEnvelopeIsVerifiedAndDroppedBeforeConsensus(t *testing.T) {
	receiver, _ := testNoiseAgent(t, "receiver")
	sender, senderKeys := testNoiseAgent(t, "sender")
	n := testNoiseNode(t, receiver)
	if err := n.AddPeer(sender.Identity.AgentID, synthoscrypto.PublicKeyHex(senderKeys.Public)); err != nil {
		t.Fatal(err)
	}

	beforeHeight := n.Chain.Height()
	beforeRoot := n.Chain.State.Root()
	env, err := sender.BuildCoverNoiseEnvelope("", "local_opt_in", 128)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	n.handleRaw("relay", raw)

	if got := n.Chain.Height(); got != beforeHeight {
		t.Fatalf("cover noise changed chain height: got %d want %d", got, beforeHeight)
	}
	if got := n.Chain.State.Root(); got != beforeRoot {
		t.Fatalf("cover noise changed state root: got %s want %s", got, beforeRoot)
	}
	if len(n.Chain.State.ImmuneStatus().LastProofHash) != 0 {
		t.Fatal("cover noise was recorded as immune proof state")
	}
	if _, _, _, ok := n.Consensus.FinalityStatus(env.Nonce); ok {
		t.Fatal("cover noise was visible to consensus")
	}
}

func TestCoverNoiseRequiresDomainSeparatedPayload(t *testing.T) {
	receiver, _ := testNoiseAgent(t, "receiver")
	sender, senderKeys := testNoiseAgent(t, "sender")
	n := testNoiseNode(t, receiver)
	if err := n.AddPeer(sender.Identity.AgentID, synthoscrypto.PublicKeyHex(senderKeys.Public)); err != nil {
		t.Fatal(err)
	}
	var logs []string
	n.Logf = func(format string, args ...any) {
		logs = append(logs, format)
	}

	env, err := sender.BuildCoverNoiseEnvelope("", "local_opt_in", 8)
	if err != nil {
		t.Fatal(err)
	}
	var payload network.CoverNoisePayload
	if err := json.Unmarshal(env.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Domain = "transaction"
	env, err = sender.BuildEnvelope(network.MessageCoverNoise, "", network.TopicCoverNoise, payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	n.handleRaw("relay", raw)

	for _, log := range logs {
		if strings.Contains(log, "verified transport-only cover noise") {
			t.Fatal("bad-domain cover noise was accepted")
		}
	}
}
