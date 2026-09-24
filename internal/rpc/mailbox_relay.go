package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/consensus"
)

// This file is the producer side of Phase 3's mailbox relay: reaching a
// validator with no directly-callable URL (MailboxRelayPeers -- the common
// case for a home/laptop operator approved through Phase 1's queue) by
// routing its proposal and vote through the registry's existing, generic
// mailbox (POST/GET /mailbox, cmd/cloudless-registry/main.go's
// handleMailbox) instead of a direct HTTPS call. See
// docs/VALIDATOR_ONBOARDING.md's Phase 3 for the full design and the
// argument for why this doesn't weaken consensus safety: the mailbox never
// needs to be trusted, because collectMailboxVotes runs every relayed vote
// through the exact same verifyPeerVote check as a directly-received one
// before it's ever fed into this node's local Consensus tally, and Chain
// independently re-verifies every quorum signature again before finalizing
// regardless of what either check believed.
//
// The relay-validator side (polling its own mailbox, validating the
// proposal, and posting its signed vote back) lives in
// cmd/synthosd/mailbox_relay.go -- it needs node.Node's real
// proposal-validation and vote-signing logic, which this package's Server
// already wraps for the direct-HTTP path via handleConsensusPropose.

// mailboxEnvelope mirrors the registry's own mailboxMessage JSON shape
// (cmd/cloudless-registry/main.go) from the client side. Payload is kept as
// raw JSON rather than decoded eagerly, since a mailbox can in principle
// carry message types this relay doesn't recognize -- see
// collectMailboxVotes's handling of a message whose Type isn't
// "consensus_vote".
type mailboxEnvelope struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	From      string          `json:"from"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt int64           `json:"created_at"`
}

// sanitizeMailboxRelayPeers trims, dedups, and drops empty entries --
// mirroring sanitizePeerURLs's cleanup for ConsensusPeerURLs, minus the
// URL-scheme check (these are validator IDs, not URLs).
func sanitizeMailboxRelayPeers(peerIDs []string) []string {
	out := make([]string, 0, len(peerIDs))
	seen := map[string]bool{}
	for _, id := range peerIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// postMailbox POSTs one message to the registry's mailbox for delivery to
// `to`. Mirrors startRegistryHeartbeat's X-Registry-Secret convention: the
// header is only set when a secret is actually configured, matching the
// registry's own authorized() check (open by default when REGISTRY_SECRET
// is unset in production).
func (s *Server) postMailbox(client *http.Client, to, msgType string, payload any) error {
	selfID := ""
	if s.Node != nil && s.Node.Agent != nil {
		selfID = s.Node.Agent.Identity.AgentID
	}
	body, err := json.Marshal(map[string]any{
		"to":      to,
		"from":    selfID,
		"type":    msgType,
		"payload": payload,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(s.RegistryURL, "/")+"/mailbox", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.RegistrySecret != "" {
		req.Header.Set("X-Registry-Secret", s.RegistrySecret)
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

// pollMailbox fetches (and, per the registry's pull-and-clear contract,
// atomically empties) this node's own mailbox.
func (s *Server) pollMailbox(client *http.Client, name string) ([]mailboxEnvelope, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(s.RegistryURL, "/")+"/mailbox?name="+strings.TrimSpace(name), nil)
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
	var messages []mailboxEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decoding mailbox response: %w", err)
	}
	return messages, nil
}

// mailboxPollInterval is how often collectMailboxVotes re-checks this
// node's own mailbox for relay-validator votes while a round is open.
// Short enough to comfortably fit several polls inside one consensus round
// (see consensusRoundTimeout in cmd/synthosd/main.go), long enough not to
// hammer the registry every round.
const mailboxPollInterval = 400 * time.Millisecond

// collectMailboxVotes sends b's proposal to every relayPeers validator's
// mailbox, then polls this node's own mailbox until either every relay
// peer has answered or timeout elapses, returning every reply that passes
// verifyPeerVote -- the identical independent-signature check a
// directly-received vote goes through in collectConsensusVotes. A relay
// peer that never answers, answers late, or returns something that doesn't
// verify is simply absent from the result, matching normal BFT handling of
// a peer that's down or misbehaving; it never errors the round.
//
// No-op (returns nil immediately) when there's nothing to relay to, or no
// registry configured to relay through -- the common case for a
// deployment with no home-PC validators approved yet.
func (s *Server) collectMailboxVotes(b *chain.Block, relayPeers []string, timeout time.Duration) []consensus.BlockVote {
	if len(relayPeers) == 0 || s.RegistryURL == "" || s.Node == nil {
		return nil
	}
	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	proposalPayload := map[string]any{
		"proposal": consensus.BlockProposal{Block: *b, Height: b.Header.Height},
	}
	for _, peerID := range relayPeers {
		peerID := peerID
		go func() {
			if err := s.postMailbox(client, peerID, "consensus_proposal", proposalPayload); err != nil {
				log.Printf("consensus: mailbox relay proposal to %s failed: %v", peerID, err)
			}
		}()
	}

	want := make(map[string]bool, len(relayPeers))
	for _, peerID := range relayPeers {
		want[peerID] = true
	}

	selfID := ""
	if s.Node.Agent != nil {
		selfID = s.Node.Agent.Identity.AgentID
	}

	var out []consensus.BlockVote
	answered := make(map[string]bool, len(relayPeers))
	deadline := time.Now().Add(timeout)
	for {
		messages, err := s.pollMailbox(client, selfID)
		if err != nil {
			log.Printf("consensus: mailbox relay poll failed: %v", err)
		}
		for _, m := range messages {
			if m.Type != "consensus_vote" || !want[m.From] || answered[m.From] {
				continue
			}
			var payload struct {
				Vote consensus.BlockVote `json:"vote"`
			}
			if err := json.Unmarshal(m.Payload, &payload); err != nil {
				log.Printf("consensus: mailbox relay vote from %s did not decode: %v", m.From, err)
				continue
			}
			if !s.verifyPeerVote(payload.Vote, b) {
				log.Printf("consensus: discarding mailbox relay vote from %s that failed independent verification (claimed voter_id=%s)", m.From, payload.Vote.VoterID)
				continue
			}
			answered[m.From] = true
			out = append(out, payload.Vote)
		}
		if len(answered) >= len(relayPeers) || time.Now().After(deadline) {
			return out
		}
		time.Sleep(mailboxPollInterval)
	}
}
