package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"synthos-collective/internal/consensus"
	"synthos-collective/internal/node"
)

// This file is the relay-validator side of Phase 3's mailbox relay: it lets
// THIS node cast real, independently-verified consensus votes even though
// it has no reachable public URL of its own (see
// docs/VALIDATOR_ONBOARDING.md's Phase 3). It deliberately reuses
// node.Node's real proposal-validation and vote-signing logic
// (HandleProposal) -- the exact same call internal/rpc/server.go's
// handleConsensusPropose makes for a directly-reachable validator -- driven
// by polling its own mailbox instead of an inbound HTTP listener. This is
// NOT what cmd/silentnode does: that binary is the public "candidate"
// download, deliberately without a validator identity or real chain state
// to vote with; this only runs inside synthosd, which already has both.
//
// The producer side (sending the proposal to a relay validator's mailbox,
// then polling its own mailbox for the reply) lives in
// internal/rpc/mailbox_relay.go.

// relayMailboxEnvelope mirrors the registry's mailboxMessage JSON shape
// (cmd/cloudless-registry/main.go), same as internal/rpc/mailbox_relay.go's
// mailboxEnvelope -- kept as a separate small type here rather than shared
// between the two packages, matching this codebase's existing style of
// each binary/package owning its own tiny client-side request/response
// shapes (e.g. rosterEntry above, requestConsensusVote in internal/rpc).
type relayMailboxEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	From      string          `json:"from"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt int64           `json:"created_at"`
}

func postRelayMailbox(client *http.Client, registryURL, secret, to, from, msgType string, payload any) error {
	body, err := json.Marshal(map[string]any{
		"to":      to,
		"from":    from,
		"type":    msgType,
		"payload": payload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, registryURL+"/mailbox", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Registry-Secret", secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("registry returned status %d posting to mailbox", resp.StatusCode)
	}
	return nil
}

func pollRelayMailbox(client *http.Client, registryURL, name string) ([]relayMailboxEnvelope, error) {
	req, err := http.NewRequest(http.MethodGet, registryURL+"/mailbox?name="+strings.TrimSpace(name), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("registry returned status %d polling mailbox", resp.StatusCode)
	}
	var messages []relayMailboxEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decoding mailbox response: %w", err)
	}
	return messages, nil
}

// startMailboxRelayListener polls this node's own mailbox for consensus
// proposals relayed by a producer that has this node listed in its
// MailboxRelayPeers (internal/rpc/mailbox_relay.go), independently
// validates and signs a real vote on each one via node.Node.HandleProposal
// -- identical validation to the direct-HTTP path, since it's the same
// method -- and posts that vote back to the proposal's sender.
//
// Deliberately gated the same way as startValidatorRosterSync
// (consensusEnabled and a configured registryURL): with no real
// vote-collecting transport enabled, or no registry to poll, there is
// nothing this loop could usefully do. Returns immediately, doing nothing,
// when either gate fails.
//
// Safe to run unconditionally on every validator, reachable or not: a
// directly-reachable validator is never listed in any producer's
// MailboxRelayPeers (mailboxRelayPeersFromRoster only includes entries
// with no usable public URL), so its own mailbox simply stays empty and
// every poll is a cheap no-op.
//
// A proposal this node rejects (wrong height, invalid signature, not
// actually a registered validator, cross-round vote lock) is logged and
// dropped, exactly like handleConsensusPropose returning an HTTP error
// would be for a directly-reachable peer -- it never crashes this loop or
// retries; the producer's own round simply doesn't get this node's vote
// that round; there will be a fresh proposal, and a fresh chance, next
// tick.
func startMailboxRelayListener(n *node.Node, selfID string, registryURL string, registrySecret string, consensusEnabled bool, pollInterval time.Duration) {
	if !consensusEnabled || registryURL == "" {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	go func() {
		for {
			time.Sleep(pollInterval)
			messages, err := pollRelayMailbox(client, registryURL, selfID)
			if err != nil {
				log.Printf("mailbox relay: poll failed: %v -- will retry next tick", err)
				continue
			}
			for _, m := range messages {
				if m.Type != "consensus_proposal" {
					continue
				}
				var payload struct {
					Proposal consensus.BlockProposal `json:"proposal"`
				}
				if err := json.Unmarshal(m.Payload, &payload); err != nil {
					log.Printf("mailbox relay: proposal from %s did not decode: %v", m.From, err)
					continue
				}
				b := payload.Proposal.Block
				vote, err := n.HandleProposal(&b)
				if err != nil {
					log.Printf("mailbox relay: rejecting proposal from %s at height %d: %v", m.From, b.Header.Height, err)
					continue
				}
				if m.From == "" {
					log.Printf("mailbox relay: proposal for height %d had no sender to reply to, dropping the vote", b.Header.Height)
					continue
				}
				if err := postRelayMailbox(client, registryURL, registrySecret, m.From, selfID, "consensus_vote", map[string]any{"vote": vote}); err != nil {
					log.Printf("mailbox relay: posting vote back to %s failed: %v", m.From, err)
				}
			}
		}
	}()
}
