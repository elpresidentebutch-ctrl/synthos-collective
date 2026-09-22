package network

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net"
	"strings"
	"testing"
	"time"
)

// genKey is a small helper for tests below: a fresh ed25519 keypair.
func genKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("ed25519.GenerateKey: %v", err)
	}
	return pub, priv
}

// loopbackAddr rewrites a listener's bound address (which net.Listen(":0")
// may report as "[::]:PORT" or "0.0.0.0:PORT") to an explicit loopback
// address a test dial can reliably reach.
func loopbackAddr(t *testing.T, addr string) string {
	t.Helper()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	return "127.0.0.1:" + port
}

// TestSecureTCPTransport_EndToEndHandshakeAndMessage is the happy path:
// two transports, both TLS-enabled and both requiring peer auth, each
// trusting the other's real key, successfully complete a TLS + ed25519
// handshake and exchange an application message over it.
func TestSecureTCPTransport_EndToEndHandshakeAndMessage(t *testing.T) {
	serverPub, serverPriv := genKey(t)
	clientPub, clientPriv := genKey(t)

	server, err := NewSecureTCPTransport("server-node", "127.0.0.1:0", nil, serverPriv, true, true)
	if err != nil {
		t.Fatalf("NewSecureTCPTransport(server): %v", err)
	}
	if err := server.RegisterTrustedPeer("client-node", hex.EncodeToString(clientPub)); err != nil {
		t.Fatalf("RegisterTrustedPeer(client on server): %v", err)
	}

	received := make(chan string, 1)
	server.OnAgentMessage(func(fromAgentID string, payload []byte) {
		received <- fromAgentID + ":" + string(payload)
	})

	if err := server.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	defer server.Close()

	serverAddr := loopbackAddr(t, server.Addr())

	client, err := NewSecureTCPTransport("client-node", "127.0.0.1:0", []string{"server-node@" + serverAddr}, clientPriv, true, true)
	if err != nil {
		t.Fatalf("NewSecureTCPTransport(client): %v", err)
	}
	// The client also needs to trust the server's key: authenticatePeer on
	// the SERVER side verifies the CLIENT's handshake, but nothing here
	// verifies the server's identity back to the client at the transport
	// level (SendToAgent is fire-and-forget once the server accepts the
	// connection) -- registering it anyway matches how a real deployment
	// would configure cfg.PeerKeys symmetrically on both sides.
	if err := client.RegisterTrustedPeer("server-node", hex.EncodeToString(serverPub)); err != nil {
		t.Fatalf("RegisterTrustedPeer(server on client): %v", err)
	}
	if err := client.Start(); err != nil {
		t.Fatalf("client.Start: %v", err)
	}
	defer client.Close()

	// dispatch (see secure_transport.go) only forwards payloads that parse
	// as an Envelope with a FromAgentID matching the TLS+handshake
	// authenticated peer -- a real caller always sends one of these, so
	// the test does too rather than a bare string, which dispatch would
	// (correctly) drop as unparseable.
	payload := []byte(`{"version":"1","message_type":"communicator","from_agent":"client-node","nonce":"n1","timestamp":"2026-01-01T00:00:00Z","payload":{},"hardware_id_hash":"h","signature":"0xdeadbeef"}`)
	if err := client.SendToAgent("server-node", payload); err != nil {
		t.Fatalf("SendToAgent: %v", err)
	}

	select {
	case got := <-received:
		want := "client-node:" + string(payload)
		if got != want {
			t.Fatalf("received %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message: TLS handshake or peer-auth handshake likely failed")
	}
}

// TestSecureTCPTransport_RejectsUnregisteredPeer confirms the actual
// security property this transport exists for: with requirePeerAuth
// enabled, a sender whose key the receiver never registered cannot get a
// message accepted, even though it can freely open the underlying TLS
// connection (TLS by itself authenticates nothing here -- see
// tls_cert.go's doc comments).
func TestSecureTCPTransport_RejectsUnregisteredPeer(t *testing.T) {
	serverPub, serverPriv := genKey(t)
	_, strangerPriv := genKey(t)
	_ = serverPub

	server, err := NewSecureTCPTransport("server-node", "127.0.0.1:0", nil, serverPriv, true, true)
	if err != nil {
		t.Fatalf("NewSecureTCPTransport(server): %v", err)
	}
	// Deliberately do NOT register "stranger-node" as trusted.

	received := make(chan string, 1)
	server.OnAgentMessage(func(fromAgentID string, payload []byte) {
		received <- fromAgentID
	})

	if err := server.Start(); err != nil {
		t.Fatalf("server.Start: %v", err)
	}
	defer server.Close()

	serverAddr := loopbackAddr(t, server.Addr())

	stranger, err := NewSecureTCPTransport("stranger-node", "127.0.0.1:0", []string{"server-node@" + serverAddr}, strangerPriv, true, false)
	if err != nil {
		t.Fatalf("NewSecureTCPTransport(stranger): %v", err)
	}
	if err := stranger.Start(); err != nil {
		t.Fatalf("stranger.Start: %v", err)
	}
	defer stranger.Close()

	// The send establishes a real TLS connection and completes a
	// self-signed handshake message, but the server should reject it at
	// the peer-auth layer since "stranger-node" isn't a trusted peer.
	_ = stranger.SendToAgent("server-node", []byte("should not be accepted"))

	select {
	case got := <-received:
		t.Fatalf("server accepted a message from an unregistered peer: %q", got)
	case <-time.After(500 * time.Millisecond):
		// Expected: nothing arrived.
	}
}

// TestPeerAuth_VerifyHandshake_RejectsMismatchedChannelBinding is the
// direct, transport-independent regression test for the actual property
// channel binding exists to provide: a handshake signed for one TLS
// session must not verify against a different session's binding value.
// This is what stands between "TLS with no real peer-cert verification"
// and an active man-in-the-middle that terminates TLS separately with
// each side and relays the plaintext -- see CreateHandshake's doc comment
// in peer_auth.go for the full explanation. Without folding channel
// binding into the signed bytes at all, this test would trivially pass
// (a relayed handshake is byte-for-byte valid), which is exactly the gap
// this closes.
func TestPeerAuth_VerifyHandshake_RejectsMismatchedChannelBinding(t *testing.T) {
	pub, priv := genKey(t)

	sender := NewPeerAuth("sender-node", priv, true)
	receiver := NewPeerAuth("receiver-node", priv, true) // unused as a signer; only its trust store/VerifyHandshake matter here
	if err := receiver.RegisterTrustedPeer("sender-node", hex.EncodeToString(pub)); err != nil {
		t.Fatalf("RegisterTrustedPeer: %v", err)
	}

	realSessionBinding := []byte("this-tls-sessions-real-exported-keying-material")
	attackerSessionBinding := []byte("a-different-tls-session-the-mitm-set-up-instead")

	// Sender signs the handshake for what it believes is its one TLS
	// session with the intended peer.
	msg := sender.CreateHandshake("nonce-1", realSessionBinding)

	// A relaying MITM forwards these exact bytes unmodified, but the
	// receiver computes ITS OWN channel binding for the (different) TLS
	// session it actually has -- with the attacker, not with the sender.
	if _, err := receiver.VerifyHandshake(msg, attackerSessionBinding); err == nil {
		t.Fatal("VerifyHandshake accepted a handshake signed for a different TLS session -- channel binding is not being enforced")
	}

	// Sanity check: the exact same handshake DOES verify against the
	// binding it was actually signed for, so the rejection above is about
	// the mismatch specifically, not some unrelated break.
	receiver2 := NewPeerAuth("receiver-node-2", priv, true)
	if err := receiver2.RegisterTrustedPeer("sender-node", hex.EncodeToString(pub)); err != nil {
		t.Fatalf("RegisterTrustedPeer: %v", err)
	}
	if _, err := receiver2.VerifyHandshake(msg, realSessionBinding); err != nil {
		t.Fatalf("VerifyHandshake rejected a handshake against its own matching channel binding: %v", err)
	}
}

// TestPeerAuth_RegisterTrustedPeer_AcceptsHexPrefix is a regression test:
// RegisterTrustedPeer used to hard-reject a "0x"-prefixed key with
// "invalid public key hex", but that's exactly the format cfg.PeerKeys
// (and every other hex-encoded key in this codebase, via
// synthoscrypto.PublicKeyBytes) stores its values in.
func TestPeerAuth_RegisterTrustedPeer_AcceptsHexPrefix(t *testing.T) {
	pub, priv := genKey(t)
	_ = priv

	pa := NewPeerAuth("node-a", priv, true)
	prefixed := "0x" + hex.EncodeToString(pub)
	if err := pa.RegisterTrustedPeer("node-b", prefixed); err != nil {
		t.Fatalf("RegisterTrustedPeer with 0x-prefixed key: %v", err)
	}

	// Confirm it's usable, not just accepted: the stored key should be the
	// exact bytes the hex (once the prefix is stripped) decodes to.
	pa.mu.RLock()
	stored, ok := pa.trustedPeers["node-b"]
	pa.mu.RUnlock()
	if !ok {
		t.Fatal("node-b not found in trustedPeers after RegisterTrustedPeer")
	}
	if !strings.EqualFold(hex.EncodeToString(stored), hex.EncodeToString(pub)) {
		t.Fatalf("stored key %x != registered key %x", stored, pub)
	}
}
