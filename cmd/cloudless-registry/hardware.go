package main

import "time"

// One machine, one reward.
//
// Node operators prove uptime with signed heartbeats, and a validator
// candidate with a full month of verified uptime becomes reward eligible.
// Nothing stopped one computer from running many node identities at once
// (the desktop fleet manager ran 21 from a single PC), and each identity
// would have qualified for its own monthly reward and counted as its own
// "validator running" on the public network status.
//
// Every node reports a hardware commitment: a hash of the machine it runs
// on. Live nodes that report the same commitment are the same machine. The
// registry keeps all of them (they are still useful, signed heartbeats) but
// only one of them, the "primary", is reward eligible or counted as a
// running validator; the others are marked SharedHardwareWith=<primary>.
//
// The commitment is self-reported, so this stops honest duplication and
// casual farming, not a determined attacker who lies about it. Real
// protection against that is stake: on the CometBFT network a validator's
// voting power and rewards come from bonded SYN, which can't be multiplied
// by inventing identities.

func peerIsStale(p peer, now time.Time) bool {
	return p.LastSeen == 0 || now.Sub(time.UnixMilli(p.LastSeen)) > staleAfter
}

// hardwarePrimaryRanksAbove reports whether a should be the primary node for
// a machine instead of b: the one that started proving uptime first, then
// the earliest registered, then the lowest name (so the choice is stable).
// Seniority, not uptime, decides it, so a newcomer that copies a machine's
// commitment can't take the reward away from the node already there.
func hardwarePrimaryRanksAbove(a, b peer) bool {
	af, bf := a.FirstHeartbeatAt, b.FirstHeartbeatAt
	if (af == 0) != (bf == 0) {
		return af != 0
	}
	if af != bf {
		return af < bf
	}
	if a.RegisteredAt != b.RegisteredAt {
		return a.RegisteredAt < b.RegisteredAt
	}
	return a.Name < b.Name
}

// markSharedHardware sets SharedHardwareWith on every live peer that shares
// a hardware commitment with a higher-ranked live peer, and clears it on
// everything else. Stale peers never shadow live ones.
func markSharedHardware(peers []peer, now time.Time) {
	primary := make(map[string]int)
	for i := range peers {
		peers[i].SharedHardwareWith = ""
		c := peers[i].HardwareCommitment
		if c == "" || peerIsStale(peers[i], now) {
			continue
		}
		j, ok := primary[c]
		if !ok || hardwarePrimaryRanksAbove(peers[i], peers[j]) {
			primary[c] = i
		}
	}
	for i := range peers {
		c := peers[i].HardwareCommitment
		if c == "" || peerIsStale(peers[i], now) {
			continue
		}
		if j := primary[c]; j != i {
			peers[i].SharedHardwareWith = peers[j].Name
		}
	}
}

// withSharedHardware returns p with SharedHardwareWith filled in against the
// registry's current peers. The caller must not hold s.mu.
func (s *server) withSharedHardware(p peer, now time.Time) peer {
	s.mu.RLock()
	peers := make([]peer, 0, len(s.state.Peers))
	for _, q := range s.state.Peers {
		if q.Name != p.Name {
			peers = append(peers, q)
		}
	}
	s.mu.RUnlock()
	peers = append(peers, p)
	markSharedHardware(peers, now)
	return peers[len(peers)-1]
}

// sharedHardwareLocked is withSharedHardware for callers already holding
// s.mu (read or write): it returns name -> primary for every shadowed peer.
func (s *server) sharedHardwareLocked(now time.Time) map[string]string {
	peers := make([]peer, 0, len(s.state.Peers))
	for _, q := range s.state.Peers {
		peers = append(peers, q)
	}
	markSharedHardware(peers, now)
	out := make(map[string]string)
	for _, q := range peers {
		if q.SharedHardwareWith != "" {
			out[q.Name] = q.SharedHardwareWith
		}
	}
	return out
}
