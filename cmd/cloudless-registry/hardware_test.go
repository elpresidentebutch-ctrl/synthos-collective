package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newEmptyRegistry() *server {
	return &server{
		state: registryState{
			Peers:               map[string]peer{},
			Mailbox:             map[string][]mailboxMessage{},
			Contacts:            []contactMessage{},
			EarlyAccessPayments: map[string]earlyAccessPaymentIntent{},
		},
	}
}

func registerWithHardware(t *testing.T, s *server, nodeID, hardware string, wantStatus int) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", map[string]any{
		"publicId":           nodeID,
		"public_key":         hex.EncodeToString(pub),
		"role":               "validator_candidate",
		"machine_commitment": hardware,
	}, wantStatus)
	return priv
}

func networkStatusCounts(t *testing.T, s *server) (validators, machines, total int) {
	t.Helper()
	t.Setenv("SYNTHOS_NATIVE_RPC_URL", "")
	rec := httptest.NewRecorder()
	s.handleAPINetworkStatus(rec, httptest.NewRequest(http.MethodGet, "/api/network/status", nil))
	var body struct {
		Validators int `json:"validators_running"`
		Machines   int `json:"machines_running"`
		Total      int `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Validators, body.Machines, body.Total
}

// One computer running three node identities is one machine: one counted
// validator, and only the first node to prove uptime is reward eligible.
func TestNodesOnOneMachineCountOnceAndShareOneReward(t *testing.T) {
	s := newEmptyRegistry()
	keys := map[string]ed25519.PrivateKey{}
	for _, id := range []string{"fleet-a", "fleet-b", "fleet-c"} {
		keys[id] = registerWithHardware(t, s, id, "machine-1", http.StatusOK)
	}
	keys["other-pc"] = registerWithHardware(t, s, "other-pc", "machine-2", http.StatusOK)
	for _, id := range []string{"fleet-b", "fleet-a", "fleet-c", "other-pc"} {
		sendTestHeartbeat(t, s, id, keys[id], "0000000000000000001-00000001", http.StatusOK)
		time.Sleep(2 * time.Millisecond) // distinct first-heartbeat times
	}

	validators, machines, total := networkStatusCounts(t, s)
	if validators != 2 || machines != 2 || total != 4 {
		t.Fatalf("validators=%d machines=%d total=%d, want 2 2 4", validators, machines, total)
	}

	// Give every node a full month of uptime: only fleet-b (first to prove
	// uptime on machine-1) and other-pc may be reward eligible.
	s.mu.Lock()
	for id, p := range s.state.Peers {
		p.VerifiedUptimeMS = int64(rewardEpoch/time.Millisecond) + 1
		s.state.Peers[id] = p
	}
	s.mu.Unlock()
	want := map[string]string{
		"fleet-a":  "shared_hardware_not_reward_eligible",
		"fleet-b":  "eligible_next_payout",
		"fleet-c":  "shared_hardware_not_reward_eligible",
		"other-pc": "eligible_next_payout",
	}
	for id, status := range want {
		got := getNodeStatus(t, s, "/api/nodes/"+id+"/status")
		if got.Node.Reward.Status != status || got.Node.Reward.Eligible != (status == "eligible_next_payout") {
			t.Errorf("%s reward = %q eligible=%v, want %q", id, got.Node.Reward.Status, got.Node.Reward.Eligible, status)
		}
	}

	// The admin promotion list follows the same rule.
	rec := httptest.NewRecorder()
	s.handleAPIAdminValidatorsPending(rec, httptest.NewRequest(http.MethodGet, "/api/admin/validators/pending", nil))
	var pending struct {
		Count int `json:"count"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	if pending.Count != 2 {
		t.Fatalf("pending promotions = %d, want 2: %s", pending.Count, rec.Body.String())
	}
}

// A stale node doesn't shadow a live one on the same machine.
func TestStaleNodeDoesNotShadowLiveNode(t *testing.T) {
	now := time.Now()
	peers := []peer{
		{Name: "old", HardwareCommitment: "m", FirstHeartbeatAt: 1, LastSeen: now.Add(-time.Hour).UnixMilli()},
		{Name: "new", HardwareCommitment: "m", FirstHeartbeatAt: 2, LastSeen: now.UnixMilli()},
		{Name: "solo", HardwareCommitment: "", LastSeen: now.UnixMilli()},
	}
	markSharedHardware(peers, now)
	for _, p := range peers {
		if p.SharedHardwareWith != "" {
			t.Fatalf("%s marked shared with %s", p.Name, p.SharedHardwareWith)
		}
	}
}

// The unsigned register endpoint can't take over a live node's identity
// (and its uptime) with a different key, can't move it to another
// machine's commitment, and keeps its validator approval.
func TestRegisterCannotHijackLiveNode(t *testing.T) {
	s := newEmptyRegistry()
	priv := registerWithHardware(t, s, "victim", "machine-1", http.StatusOK)
	sendTestHeartbeat(t, s, "victim", priv, "0000000000000000001-00000001", http.StatusOK)
	registerWithHardware(t, s, "victim", "machine-1", http.StatusConflict)

	s.mu.Lock()
	v := s.state.Peers["victim"]
	v.ValidatorApproved = true
	s.state.Peers["victim"] = v
	s.mu.Unlock()
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", map[string]any{
		"publicId":            "victim",
		"public_key":          v.PublicKey,
		"role":                "validator_candidate",
		"hardware_commitment": "machine-2",
	}, http.StatusOK)
	got := s.state.Peers["victim"]
	if got.HardwareCommitment != "machine-1" || !got.ValidatorApproved || got.ValidHeartbeats != 1 {
		t.Fatalf("re-register changed the node: hw=%q approved=%v heartbeats=%d", got.HardwareCommitment, got.ValidatorApproved, got.ValidHeartbeats)
	}
}

// A node silent for a day can be re-registered with a new key (a reinstall
// that lost its key file), but its proof starts over.
func TestRegisterRekeysLongSilentNodeFromZero(t *testing.T) {
	s := newEmptyRegistry()
	priv := registerWithHardware(t, s, "laptop", "machine-1", http.StatusOK)
	sendTestHeartbeat(t, s, "laptop", priv, "0000000000000000001-00000001", http.StatusOK)
	s.mu.Lock()
	p := s.state.Peers["laptop"]
	p.LastSeen = time.Now().Add(-nodeRekeyAfter - time.Minute).UnixMilli()
	p.VerifiedUptimeMS = 12345
	s.state.Peers["laptop"] = p
	s.mu.Unlock()

	registerWithHardware(t, s, "laptop", "machine-1", http.StatusOK)
	got := s.state.Peers["laptop"]
	if got.PublicKey == p.PublicKey || got.VerifiedUptimeMS != 0 || got.ValidHeartbeats != 0 || got.LastNonce != "" {
		t.Fatalf("re-keyed node kept old proof: key changed=%v uptime=%d heartbeats=%d nonce=%q",
			got.PublicKey != p.PublicKey, got.VerifiedUptimeMS, got.ValidHeartbeats, got.LastNonce)
	}
}

// Older fleet builds send a per-node hardware_commitment that doesn't
// identify the machine; only machine_commitment is stored, so an old client
// can't pin a node to a meaningless value before it's updated.
func TestOldPerNodeHardwareCommitmentIsIgnored(t *testing.T) {
	s := newEmptyRegistry()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"publicId":            "old-fleet-node",
		"public_key":          hex.EncodeToString(pub),
		"role":                "validator_candidate",
		"hardware_commitment": "per-node-value",
	}
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", body, http.StatusOK)
	if got := s.state.Peers["old-fleet-node"].HardwareCommitment; got != "" {
		t.Fatalf("stored %q from an old client", got)
	}
	body["machine_commitment"] = "machine-1"
	postJSON(t, s.handleAPINodeRegister, "/api/nodes/register", body, http.StatusOK)
	if got := s.state.Peers["old-fleet-node"].HardwareCommitment; got != "machine-1" {
		t.Fatalf("updated client's machine commitment = %q", got)
	}
}
