package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const defaultRelayURLs = "https://synthos-www.onrender.com"

type silentNode struct {
	NodeID             string   `json:"node_id"`
	HardwareCommitment string   `json:"hardware_commitment"`
	Mode               string   `json:"mode"`
	StartedAt          string   `json:"started_at"`
	HeartbeatCount     uint64   `json:"heartbeat_count"`
	LastProof          string   `json:"last_proof"`
	RelayURLs          []string `json:"relay_urls"`
	LastRelayOK        []string `json:"last_relay_ok"`
	LastRelayFailed    []string `json:"last_relay_failed"`
	StatusPath         string   `json:"status_path"`
}

func main() {
	node := silentNode{
		NodeID:             "desktop-" + hardwareCommitment()[:12],
		HardwareCommitment: hardwareCommitment(),
		Mode:               "absolute_silence_outbound_only",
		StartedAt:          time.Now().UTC().Format(time.RFC3339),
		StatusPath:         statusPath(),
	}

	relayURLs := relayURLSet()
	node.RelayURLs = relayURLs

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("SYNTHOS silent node started: %s", node.NodeID)
	log.Printf("Mode: outbound polling only; no inbound ports; no local server")
	log.Printf("Relay set: %s", strings.Join(relayURLs, ", "))

	heartbeatAll(ctx, relayURLs, &node)
	pollMailboxAll(ctx, relayURLs, node.NodeID)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("SYNTHOS silent node stopped")
			return
		case <-ticker.C:
			heartbeatAll(ctx, relayURLs, &node)
			pollMailboxAll(ctx, relayURLs, node.NodeID)
		}
	}
}

func hardwareCommitment() string {
	hostname, _ := os.Hostname()
	currentUser, _ := user.Current()
	username := ""
	if currentUser != nil {
		username = currentUser.Username
	}
	sum := sha256.Sum256([]byte(hostname + "|" + username + "|synthos-silent-node-v1"))
	return hex.EncodeToString(sum[:])
}

func heartbeatAll(ctx context.Context, relayURLs []string, node *silentNode) {
	node.HeartbeatCount++
	proof := heartbeatProof(*node, time.Now().UTC())
	node.LastProof = proof
	node.LastRelayOK = nil
	node.LastRelayFailed = nil

	payload := map[string]any{
		"name":                node.NodeID,
		"url":                 "",
		"cloud":               "desktop-silent",
		"background":          true,
		"inbound_ports":       0,
		"mode":                node.Mode,
		"hardware_commitment": node.HardwareCommitment,
		"heartbeat_count":     node.HeartbeatCount,
		"heartbeat_proof":     proof,
		"heartbeat_at":        time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	for _, relayURL := range relayURLs {
		if heartbeat(ctx, relayURL, body, node.NodeID) {
			node.LastRelayOK = append(node.LastRelayOK, relayURL)
		} else {
			node.LastRelayFailed = append(node.LastRelayFailed, relayURL)
		}
	}
	writeStatus(*node)
}

func heartbeat(ctx context.Context, relayURL string, body []byte, nodeID string) bool {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, relayURL+"/register", bytes.NewReader(body))
	if err != nil {
		log.Printf("heartbeat request error on %s: %v", relayURL, err)
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("heartbeat outbound error on %s: %v", relayURL, err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("heartbeat rejected by %s: %s", relayURL, resp.Status)
		return false
	}
	log.Printf("heartbeat sent: %s via %s", nodeID, relayURL)
	return true
}

func pollMailboxAll(ctx context.Context, relayURLs []string, nodeID string) {
	for _, relayURL := range relayURLs {
		pollMailbox(ctx, relayURL, nodeID)
	}
}

func pollMailbox(ctx context.Context, relayURL string, nodeID string) {
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, relayURL+"/mailbox?name="+url.QueryEscape(nodeID), nil)
	if err != nil {
		log.Printf("mailbox request error on %s: %v", relayURL, err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("mailbox outbound error on %s: %v", relayURL, err)
		return
	}
	defer resp.Body.Close()
	log.Printf("mailbox poll via %s: %s", relayURL, resp.Status)
}

func env(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func relayURLSet() []string {
	raw := firstNonEmpty(
		os.Getenv("SYNTHOS_RELAY_URLS"),
		os.Getenv("SYNTHOS_REGISTRY_URLS"),
		os.Getenv("SYNTHOS_MAILBOX_URLS"),
		os.Getenv("SYNTHOS_REGISTRY_URL"),
		os.Getenv("SYNTHOS_MAILBOX_URL"),
		defaultRelayURLs,
	)
	return parseURLList(raw)
}

func parseURLList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\t' || r == ' '
	}) {
		item = strings.TrimRight(strings.TrimSpace(item), "/")
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func heartbeatProof(node silentNode, t time.Time) string {
	sum := sha256.Sum256([]byte(node.NodeID + "|" + node.HardwareCommitment + "|" + node.StartedAt + "|" + t.Format("2006-01-02T15:04")))
	return "0x" + hex.EncodeToString(sum[:])
}

func statusPath() string {
	if value := os.Getenv("SYNTHOS_SILENT_STATUS_PATH"); value != "" {
		return value
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "synthos-silent-node-status.json"
	}
	return filepath.Join(dir, "SynthosCollective", "silent-node-status.json")
}

func writeStatus(node silentNode) {
	if node.StatusPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(node.StatusPath), 0o700); err != nil {
		return
	}
	body, _ := json.MarshalIndent(node, "", "  ")
	_ = os.WriteFile(node.StatusPath, body, 0o600)
}
