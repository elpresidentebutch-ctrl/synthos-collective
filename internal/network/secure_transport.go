package network

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
)

// secureHandshakeTimeout bounds the initial handshake (already enforced via
// authenticatePeer's own SetReadDeadline below). secureMsgReadTimeout is
// the same protection applied to the ongoing per-message read loop in
// handleConn, which -- like tcp_transport.go's TCPTransport, and for the
// exact same slow-loris reason documented there -- used to have no
// deadline at all once past the handshake (authenticatePeer resets the
// deadline to none via `defer conn.SetReadDeadline(time.Time{})` before
// handleConn's loop even starts). secureMaxConns mirrors
// tcp_transport.go's tcpMaxConns, capping concurrent inbound connections
// so acceptLoop's one-goroutine-per-connection can't grow unbounded.
// secureRespReadTimeout/secureMaxRespSize close the same gap on the
// OUTBOUND side: sendWithHandshake reads a length-prefixed auth response
// from whatever it dialed with no deadline and no upper bound on the
// claimed length, so a malicious or misbehaving peer address could either
// hang this node's send indefinitely or make it allocate an unbounded
// buffer from an attacker-chosen length field.
const (
	secureHandshakeTimeout = 15 * time.Second
	secureMsgReadTimeout   = 30 * time.Second
	secureWriteTimeout     = 10 * time.Second
	secureDialTimeout      = 10 * time.Second
	secureMaxConns         = 256
	secureRespReadTimeout  = 10 * time.Second
	secureMaxRespSize      = 4096
)

// SecureTCPTransport extends TCPTransport with TLS encryption and peer authentication.
// All node-to-node communication is encrypted and authenticated.
type SecureTCPTransport struct {
	nodeID          string
	listenAddr      string
	privateKey      ed25519.PrivateKey
	certManager     *CertificateManager
	peerAuth        *PeerAuth
	enableTLS       bool
	requirePeerAuth bool

	mu               sync.RWMutex
	listener         net.Listener
	peers            map[string]string // agentID -> "host:port"
	agentHandler     func(fromAgentID string, payload []byte)
	topicHandlers    map[string]func(fromAgentID string, payload []byte)
	peerConnections  map[string]net.Conn // agentID -> persistent connection
	connectionErrors map[string]int      // agentID -> error count
}

// NewSecureTCPTransport creates a new secure TCP transport with TLS and authentication.
// enableTLS: use TLS encryption for all connections
// requirePeerAuth: require peer signature verification (requires registered peer keys)
func NewSecureTCPTransport(
	nodeID string,
	listenAddr string,
	peerAddrs []string,
	privateKey ed25519.PrivateKey,
	enableTLS bool,
	requirePeerAuth bool,
) (*SecureTCPTransport, error) {
	// Parse peers.
	peers := make(map[string]string)
	for _, p := range peerAddrs {
		parts := strings.SplitN(p, "@", 2)
		if len(parts) != 2 {
			continue
		}
		peers[parts[0]] = parts[1]
	}

	// Create certificate manager if TLS is enabled.
	var certManager *CertificateManager
	var err error
	if enableTLS {
		certManager, err = NewCertificateManager(nodeID)
		if err != nil {
			return nil, fmt.Errorf("failed to create certificate manager: %w", err)
		}
	}

	// Create peer authentication manager.
	peerAuth := NewPeerAuth(nodeID, privateKey, requirePeerAuth)

	return &SecureTCPTransport{
		nodeID:           nodeID,
		listenAddr:       listenAddr,
		privateKey:       privateKey,
		certManager:      certManager,
		peerAuth:         peerAuth,
		enableTLS:        enableTLS,
		requirePeerAuth:  requirePeerAuth,
		peers:            peers,
		topicHandlers:    make(map[string]func(string, []byte)),
		peerConnections:  make(map[string]net.Conn),
		connectionErrors: make(map[string]int),
	}, nil
}

// Start begins listening for connections and authenticating peers.
func (t *SecureTCPTransport) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.listenAddr == "" {
		return errors.New("listen address required for secure TCP transport")
	}

	var ln net.Listener
	var err error

	// Create listener with or without TLS.
	if t.enableTLS {
		tlsConfig := t.certManager.GetServerTLSConfig()
		ln, err = tls.Listen("tcp", t.listenAddr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to create TLS listener: %w", err)
		}
	} else {
		ln, err = net.Listen("tcp", t.listenAddr)
		if err != nil {
			return fmt.Errorf("failed to create TCP listener: %w", err)
		}
	}

	t.listener = ln
	go t.acceptLoop()
	return nil
}

// Addr returns the address this transport is actually listening on, or ""
// if Start hasn't been called yet. Useful when listenAddr was ":0" (bind
// to an OS-assigned ephemeral port) and the caller needs to know which
// port that turned out to be -- e.g. to tell it to other peers, or in
// tests.
func (t *SecureTCPTransport) Addr() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.listener == nil {
		return ""
	}
	return t.listener.Addr().String()
}

// acceptLoop accepts new connections and spawns handlers.
func (t *SecureTCPTransport) acceptLoop() {
	// connSlots bounds the number of concurrently handled inbound
	// connections (see secureMaxConns's doc comment above).
	connSlots := make(chan struct{}, secureMaxConns)
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		select {
		case connSlots <- struct{}{}:
			go func() {
				defer func() { <-connSlots }()
				t.handleConn(conn)
			}()
		default:
			conn.Close()
		}
	}
}

// handleConn handles a single connection lifecycle.
// Performs authentication first, then reads messages.
func (t *SecureTCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()

	// Authenticate the peer first.
	authenticatedPeerID, err := t.authenticatePeer(conn)
	if err != nil {
		return // Connection rejected due to auth failure
	}

	// Read and dispatch messages from authenticated peer.
	r := bufio.NewReader(conn)
	for {
		// See secureMsgReadTimeout's doc comment above: authenticatePeer
		// clears the handshake deadline before returning, so this is the
		// only deadline protecting the steady-state message loop. Reset on
		// every iteration, so an authenticated peer actively exchanging
		// messages -- even slowly -- is never penalized.
		if err := conn.SetReadDeadline(time.Now().Add(secureMsgReadTimeout)); err != nil {
			return
		}
		// Read 4-byte big-endian length.
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 || n > 10*1024*1024 {
			return
		}

		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}

		// Verify signature if message contains peer identity.
		t.dispatch(authenticatedPeerID, buf)
	}
}

// authenticatePeer performs handshake-based authentication.
// Returns the authenticated peer ID or an error.
func (t *SecureTCPTransport) authenticatePeer(conn net.Conn) (string, error) {
	// Set handshake timeout.
	conn.SetReadDeadline(time.Now().Add(secureHandshakeTimeout))
	defer conn.SetReadDeadline(time.Time{})

	// This node's view of the TLS channel binding for THIS connection (nil
	// if TLS is disabled). See VerifyHandshake's doc comment for why this
	// has to be checked against what the peer signed.
	binding, err := channelBinding(conn)
	if err != nil {
		return "", fmt.Errorf("channel binding: %w", err)
	}

	// Receive peer's handshake message.
	r := bufio.NewReader(conn)
	lenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return "", fmt.Errorf("failed to read handshake length: %w", err)
	}

	n := binary.BigEndian.Uint32(lenBuf)
	if n == 0 || n > 1024 {
		return "", errors.New("invalid handshake size")
	}

	handshakeBuf := make([]byte, n)
	if _, err := io.ReadFull(r, handshakeBuf); err != nil {
		return "", fmt.Errorf("failed to read handshake: %w", err)
	}

	// Parse handshake message.
	var hsMsg HandshakeMessage
	if err := json.Unmarshal(handshakeBuf, &hsMsg); err != nil {
		return "", fmt.Errorf("failed to parse handshake: %w", err)
	}

	// Verify handshake.
	peerID, err := t.peerAuth.VerifyHandshake(&hsMsg, binding)
	if err != nil {
		// Send rejection response.
		t.sendAuthResponse(conn, false, err.Error())
		return "", fmt.Errorf("handshake verification failed: %w", err)
	}

	// Send acceptance response.
	if err := t.sendAuthResponse(conn, true, ""); err != nil {
		return "", fmt.Errorf("failed to send auth response: %w", err)
	}

	return peerID, nil
}

// sendAuthResponse sends a handshake response to the peer.
func (t *SecureTCPTransport) sendAuthResponse(conn net.Conn, accepted bool, reason string) error {
	response := map[string]interface{}{
		"accepted": accepted,
		"reason":   reason,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return err
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(data)))

	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err = conn.Write(data)
	return err
}

// SendToAgent sends a message to a specific peer.
// Establishes persistent connection with TLS if enabled.
func (t *SecureTCPTransport) SendToAgent(agentID string, payload []byte) error {
	t.mu.RLock()
	addr, ok := t.peers[agentID]
	t.mu.RUnlock()

	if !ok {
		return fmt.Errorf("unknown peer agentID=%s", agentID)
	}

	// Try to use or establish persistent connection.
	return t.sendWithHandshake(agentID, addr, payload)
}

// sendWithHandshake performs peer authentication before sending.
func (t *SecureTCPTransport) sendWithHandshake(agentID, addr string, payload []byte) error {
	// Check if we have a cached connection.
	t.mu.Lock()
	cachedConn, exists := t.peerConnections[agentID]
	t.mu.Unlock()

	if exists && cachedConn != nil {
		// Try to send on cached connection.
		if err := t.sendOnConn(cachedConn, payload); err == nil {
			return nil
		}
		// Connection is dead; remove it.
		t.mu.Lock()
		delete(t.peerConnections, agentID)
		t.mu.Unlock()
	}

	// Establish new connection.
	var conn net.Conn

	if t.enableTLS {
		tlsConfig := t.certManager.GetClientTLSConfig()
		dialer := &net.Dialer{Timeout: secureDialTimeout}
		tlsConn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err != nil {
			t.recordConnectionError(agentID)
			return fmt.Errorf("failed to dial %s (%s): %w", agentID, addr, err)
		}
		conn = tlsConn
	} else {
		var dialErr error
		conn, dialErr = net.DialTimeout("tcp", addr, secureDialTimeout)
		if dialErr != nil {
			t.recordConnectionError(agentID)
			return fmt.Errorf("failed to dial %s (%s): %w", agentID, addr, dialErr)
		}
	}
	defer conn.Close()

	// Our own view of the TLS channel binding for this connection (nil if
	// TLS is disabled) -- see VerifyHandshake's doc comment for why this
	// has to be folded into what we sign below.
	binding, err := channelBinding(conn)
	if err != nil {
		t.recordConnectionError(agentID)
		return fmt.Errorf("channel binding: %w", err)
	}

	// Perform handshake.
	nonce := randomNonce()
	hsMsg := t.peerAuth.CreateHandshake(nonce, binding)

	hsData, err := json.Marshal(hsMsg)
	if err != nil {
		return err
	}

	// Send handshake.
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(hsData)))

	if err := conn.SetWriteDeadline(time.Now().Add(secureWriteTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}
	if _, err := conn.Write(lenBuf); err != nil {
		return fmt.Errorf("failed to send handshake: %w", err)
	}
	if _, err := conn.Write(hsData); err != nil {
		return fmt.Errorf("failed to send handshake data: %w", err)
	}

	// Receive handshake response. Both a read deadline and an upper bound
	// on the claimed response length matter here: addr came from this
	// node's own peer configuration, but whatever actually answers on that
	// address is not yet authenticated at this point in the exchange (that
	// is the whole point of the handshake under way), so a misbehaving or
	// compromised endpoint could otherwise hang this send indefinitely, or
	// claim an enormous respLen and make this node allocate an unbounded
	// buffer for it.
	if err := conn.SetReadDeadline(time.Now().Add(secureRespReadTimeout)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}
	r := bufio.NewReader(conn)
	respLenBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, respLenBuf); err != nil {
		return fmt.Errorf("failed to read handshake response: %w", err)
	}

	respLen := binary.BigEndian.Uint32(respLenBuf)
	if respLen == 0 || respLen > secureMaxRespSize {
		return fmt.Errorf("invalid handshake response size %d", respLen)
	}
	respBuf := make([]byte, respLen)
	if _, err := io.ReadFull(r, respBuf); err != nil {
		return fmt.Errorf("failed to read handshake response data: %w", err)
	}

	var authResp map[string]interface{}
	if err := json.Unmarshal(respBuf, &authResp); err != nil {
		return fmt.Errorf("failed to parse auth response: %w", err)
	}

	if accepted, ok := authResp["accepted"].(bool); !ok || !accepted {
		reason, _ := authResp["reason"].(string)
		t.peerAuth.BanPeer(agentID, fmt.Sprintf("authentication rejected: %s", reason))
		return fmt.Errorf("peer %s rejected connection: %s", agentID, reason)
	}

	// Now send the actual payload.
	return t.sendOnConn(conn, payload)
}

// sendOnConn sends a message on an active connection -- either the one just
// established by sendWithHandshake, or a cached persistent connection to an
// already-authenticated peer. Either way, a deadline is needed: a cached
// connection in particular can go stale (the peer stopped reading without
// closing its end) long after the handshake that created it, and without
// this, a hung peer would block the send indefinitely.
func (t *SecureTCPTransport) sendOnConn(conn net.Conn, payload []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(secureWriteTimeout)); err != nil {
		return err
	}
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))

	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err := conn.Write(payload)
	return err
}

// Broadcast sends a message to all known peers.
func (t *SecureTCPTransport) Broadcast(topic string, payload []byte) error {
	t.mu.RLock()
	addrs := make(map[string]string)
	for agentID, addr := range t.peers {
		addrs[agentID] = addr
	}
	t.mu.RUnlock()

	for agentID, addr := range addrs {
		_ = t.sendWithHandshake(agentID, addr, payload)
	}
	return nil
}

// Close cleanly shuts down the transport.
func (t *SecureTCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Close listener.
	if t.listener != nil {
		t.listener.Close()
	}

	// Close persistent connections.
	for _, conn := range t.peerConnections {
		if conn != nil {
			conn.Close()
		}
	}

	return nil
}

// OnAgentMessage registers a handler for direct agent-to-agent messages.
func (t *SecureTCPTransport) OnAgentMessage(handler func(fromAgentID string, payload []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agentHandler = handler
}

// OnTopicMessage registers a handler for messages on a topic.
func (t *SecureTCPTransport) OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.topicHandlers[topic] = handler
}

// dispatch routes received messages to appropriate handlers.
func (t *SecureTCPTransport) dispatch(authenticatedPeerID string, payload []byte) {
	// Envelope routing.
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}

	// Verify the sender matches authenticated ID.
	if env.FromAgentID != authenticatedPeerID {
		// Spoofing attempt detected.
		t.peerAuth.BanPeer(authenticatedPeerID, "attempted message spoofing")
		return
	}

	t.mu.RLock()
	agentHandler := t.agentHandler
	topicHandler := t.topicHandlers[env.Topic]
	t.mu.RUnlock()

	if env.Topic != "" && topicHandler != nil {
		topicHandler(env.FromAgentID, payload)
		return
	}
	if agentHandler != nil {
		agentHandler(env.FromAgentID, payload)
	}
}

// RegisterTrustedPeer adds a peer's public key to the trusted list.
func (t *SecureTCPTransport) RegisterTrustedPeer(agentID string, publicKeyHex string) error {
	return t.peerAuth.RegisterTrustedPeer(agentID, publicKeyHex)
}

// GetPeerReputation returns reputation data for a peer.
func (t *SecureTCPTransport) GetPeerReputation(agentID string) *PeerInfo {
	return t.peerAuth.GetPeerReputation(agentID)
}

// recordConnectionError tracks connection failures for reputation.
func (t *SecureTCPTransport) recordConnectionError(agentID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.connectionErrors[agentID]++

	// Ban after persistent failures.
	if t.connectionErrors[agentID] > 10 {
		t.peerAuth.BanPeer(agentID, fmt.Sprintf("connection failures exceeded threshold (%d)", t.connectionErrors[agentID]))
	}
}

// randomNonce generates a cryptographic nonce for replay protection.
func randomNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// channelBindingLabel is the TLS exporter label used to derive the
// per-session value CreateHandshake/VerifyHandshake fold into their
// signed bytes (see peer_auth.go's doc comments for why). It only needs to
// be a fixed, unique-to-this-protocol string -- RFC 8446's exporter
// construction already guarantees the derived value is unique per TLS
// session for a fixed label.
const channelBindingLabel = "synthos-secure-transport-peer-auth"

// channelBinding returns this TLS connection's exported keying material,
// or nil if conn isn't a *tls.Conn (enableTLS=false: this transport falls
// back to no channel binding, matching the pre-TLS handshake format, since
// a plain TCP connection has no session secret to derive one from -- see
// handshakeSignedBytes in peer_auth.go).
func channelBinding(conn net.Conn) ([]byte, error) {
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, nil
	}
	// ExportKeyingMaterial requires the handshake to be complete; callers
	// of this helper only invoke it after Dial/Accept has returned (client
	// side) or after explicitly completing the handshake (server side, via
	// Handshake() below), so this should never actually block here, but
	// HandshakeContext-less Handshake() is idempotent/cheap to call again
	// if it already completed.
	if err := tlsConn.Handshake(); err != nil {
		return nil, fmt.Errorf("completing TLS handshake for channel binding: %w", err)
	}
	state := tlsConn.ConnectionState()
	binding, err := state.ExportKeyingMaterial(channelBindingLabel, nil, 32)
	if err != nil {
		return nil, fmt.Errorf("exporting TLS channel binding: %w", err)
	}
	return binding, nil
}
