package main

import (
	"strings"
	"time"
)

// One machine, one household, one reward.
//
// Node operators prove uptime with signed heartbeats, and a validator
// candidate with a full month of verified uptime becomes reward eligible.
//
// To prevent sybil farming:
// 1. Machine Level: Every node reports a hardware commitment (hash of machine).
//    Nodes sharing a hardware commitment are the same machine.
// 2. Household Level: Nodes connecting from the same external IP address share
//    the same household/local internet connection. If 5 computers are in the
//    same house on the same internet, only ONE of them is reward eligible.
//
// In both cases, the senior node (first to prove uptime / earliest registered)
// is designated as the primary for that machine and household. All other nodes
// sharing the hardware or household IP are marked SharedHardwareWith /
// SharedHouseholdWith, and their status reflects non-eligibility for rewards.

func peerIsStale(p peer, now time.Time) bool {
	return p.LastSeen == 0 || now.Sub(time.UnixMilli(p.LastSeen)) > staleAfter
}

func isHouseholdIP(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" || ip == "127.0.0.1" || ip == "::1" || ip == "localhost" || ip == "192.0.2.1" {
		return false
	}
	return true
}

// hardwarePrimaryRanksAbove reports whether a should be the primary node for
// a machine or household instead of b: the one that started proving uptime first,
// then the earliest registered, then the lowest name (stable tie-breaker).
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

// markSharedHardware sets SharedHardwareWith and SharedHouseholdWith on every
// live peer that shares a hardware commitment or household internet connection
// with a higher-ranked live peer.
func markSharedHardware(peers []peer, now time.Time) {
	primaryHw := make(map[string]int)
	primaryIP := make(map[string]int)

	for i := range peers {
		peers[i].SharedHardwareWith = ""
		peers[i].SharedHouseholdWith = ""
		if peerIsStale(peers[i], now) {
			continue
		}
		if c := peers[i].HardwareCommitment; c != "" {
			j, ok := primaryHw[c]
			if !ok || hardwarePrimaryRanksAbove(peers[i], peers[j]) {
				primaryHw[c] = i
			}
		}
		if ip := peers[i].HouseholdIP; isHouseholdIP(ip) {
			j, ok := primaryIP[ip]
			if !ok || hardwarePrimaryRanksAbove(peers[i], peers[j]) {
				primaryIP[ip] = i
			}
		}
	}

	for i := range peers {
		if peerIsStale(peers[i], now) {
			continue
		}
		if c := peers[i].HardwareCommitment; c != "" {
			if j := primaryHw[c]; j != i {
				peers[i].SharedHardwareWith = peers[j].Name
			}
		}
		if ip := peers[i].HouseholdIP; isHouseholdIP(ip) {
			if j := primaryIP[ip]; j != i {
				peers[i].SharedHouseholdWith = peers[j].Name
			}
		}
	}
}

// withSharedHardware returns p with SharedHardwareWith and SharedHouseholdWith
// filled in against the registry's current peers. The caller must not hold s.mu.
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
