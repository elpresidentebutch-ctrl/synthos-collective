package main

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// WatchdogConfig sets when the watchdog speaks up.
type WatchdogConfig struct {
	HaltAfter     time.Duration // no new block for this long = chain stopped
	MissFor       time.Duration // a validator absent from every commit we check for this long = offline
	MinHeartbeats int64         // an outside node must prove this much before we announce it
	FleetClouds   []string      // registry "cloud" values that are Jim's own nodes
	FleetPrefixes []string      // node-name prefixes that are Jim's own nodes
	HomeIPs       []string      // IP prefixes of Jim's own machines
}

// Watchdog turns snapshots into alerts. It only reports changes: one alert
// when something goes wrong and one when it recovers, never a repeat.
type Watchdog struct {
	cfg WatchdogConfig

	lastHeight   int64
	haltAlerted  bool
	missingSince map[string]time.Time // validator address -> first check it was absent
	missAlerted  map[string]bool
	knownOutside map[string]bool // outside nodes already announced
}

func NewWatchdog(cfg WatchdogConfig, knownOutside []string) *Watchdog {
	w := &Watchdog{cfg: cfg, missingSince: map[string]time.Time{}, missAlerted: map[string]bool{}, knownOutside: map[string]bool{}}
	for _, n := range knownOutside {
		w.knownOutside[n] = true
	}
	return w
}

// KnownOutside lists announced outside nodes, so they survive a restart.
func (w *Watchdog) KnownOutside() []string {
	out := make([]string, 0, len(w.knownOutside))
	for n := range w.knownOutside {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// IsOutside reports whether a registry node belongs to someone other than Jim.
func (c WatchdogConfig) IsOutside(n Node) bool {
	for _, cl := range c.FleetClouds {
		if n.Cloud == cl {
			return false
		}
	}
	for _, p := range c.FleetPrefixes {
		if strings.HasPrefix(n.Name, p) {
			return false
		}
	}
	if strings.HasPrefix(n.IP, "127.") || n.IP == "" {
		return false
	}
	for _, ip := range c.HomeIPs {
		if ip != "" && strings.HasPrefix(n.IP, ip) {
			return false
		}
	}
	return true
}

func label(v Validator) string {
	short := v.Address
	if len(short) > 8 {
		short = short[:8]
	}
	if v.Name != "" {
		return v.Name + " (" + short + ")"
	}
	return short
}

// Observe compares a new snapshot with what the watchdog saw before.
func (w *Watchdog) Observe(now time.Time, s *Snapshot) []string {
	var alerts []string

	// 1. Has the chain stopped making blocks?
	stalled := now.Sub(s.BlockTime) > w.cfg.HaltAfter
	switch {
	case stalled && !w.haltAlerted:
		w.haltAlerted = true
		alerts = append(alerts, fmt.Sprintf("🚨 Synthos Collective has stopped making blocks. Last block %d at %s (%s ago). Don't send SYN until it's running again.",
			s.Height, s.BlockTime.UTC().Format("15:04 UTC"), now.Sub(s.BlockTime).Round(time.Second)))
	case !stalled && w.haltAlerted && s.Height > w.lastHeight:
		w.haltAlerted = false
		alerts = append(alerts, fmt.Sprintf("✅ Blocks are being made again. Now at block %d.", s.Height))
	}

	// 2. Is a validator missing from the commits? A validator's signature can
	// be left out of one commit now and then, so only a validator that is
	// absent from every check for MissFor counts as offline.
	if s.Height > w.lastHeight {
		for _, v := range s.Validators {
			if s.Signed[v.Address] {
				if w.missAlerted[v.Address] {
					alerts = append(alerts, fmt.Sprintf("✅ Validator %s is signing blocks again (block %d).", label(v), s.Height))
				}
				delete(w.missingSince, v.Address)
				w.missAlerted[v.Address] = false
				continue
			}
			since, ok := w.missingSince[v.Address]
			if !ok {
				w.missingSince[v.Address] = now
				since = now
			}
			if now.Sub(since) >= w.cfg.MissFor && !w.missAlerted[v.Address] {
				w.missAlerted[v.Address] = true
				alerts = append(alerts, fmt.Sprintf("⚠️ Validator %s hasn't signed a block for %s (as of block %d). Check that it's running.",
					label(v), now.Sub(since).Round(time.Minute), s.Height))
			}
		}
	}
	if s.Height > w.lastHeight {
		w.lastHeight = s.Height
	}

	// 3. Has a new outside node proven real uptime? Bots that load the node
	// page send one or two heartbeats; MinHeartbeats filters them out.
	for _, n := range s.Nodes {
		if !w.cfg.IsOutside(n) || w.knownOutside[n.Name] || n.ValidHeartbeats < w.cfg.MinHeartbeats {
			continue
		}
		w.knownOutside[n.Name] = true
		alerts = append(alerts, fmt.Sprintf("🎉 New outside node: %s has sent %d verified heartbeats (registered %s). Check it with /node %s",
			n.Name, n.ValidHeartbeats, n.RegisteredAt.UTC().Format("Jan 2 15:04 UTC"), n.Name))
	}
	return alerts
}
