package network

import (
	"bufio"
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

// This transport is the one actually wired into cmd/synthosd/main.go for
// every deployed validator/RPC node on this chain -- despite the doc
// comment above calling it minimal, it is Internet-facing in production
// (Render exposes each node's listen port publicly), not just a devnet
// toy. It used to have no read deadline on inbound connections at all: a
// peer (or anyone who could reach the port) that opened a connection, sent
// a length prefix, and then simply stopped sending -- deliberately or by
// accident -- would tie up handleConn's goroutine, its already-allocated
// buffer, and a file descriptor forever, since io.ReadFull blocks with no
// timeout. Because acceptLoop spawns one such goroutine per accepted
// connection with no cap, repeating this from a handful of connections was
// enough to exhaust goroutines/file descriptors: a classic, cheap
// slow-loris denial of service. Fixed with a per-message read deadline
// (tcpReadTimeout, reset before each framed message so a connection
// sending real traffic, even slowly, is never penalized) and a cap on
// concurrent inbound connections (tcpMaxConns) so a flood of connections
// that never send anything useful can't grow this node's goroutine count
// without bound. Outbound sends (sendToAddr) similarly had no dial or
// write timeout, so a hung or malicious peer could block this node's own
// Broadcast calls indefinitely; both now have deadlines too.
const (
	tcpReadTimeout  = 30 * time.Second
	tcpWriteTimeout = 10 * time.Second
	tcpDialTimeout  = 10 * time.Second
	tcpMaxConns     = 256
)

// TCPTransport is a minimal TCP-based Transport implementation.
// It is intended for small devnets and two-node setups, not Internet-scale P2P.
// Messages are sent as length-prefixed JSON envelopes over TCP.
type TCPTransport struct {
	nodeID     string
	listenAddr string

	// ReadTimeout bounds how long a connection may take to deliver one
	// complete framed message (see tcpReadTimeout's doc comment above).
	// NewTCPTransport sets it to tcpReadTimeout; tests may override it to
	// something short, before calling Start, to exercise the timeout
	// without waiting the real duration. readTimeout() falls back to
	// tcpReadTimeout if a TCPTransport is ever constructed without going
	// through NewTCPTransport and this is left at its zero value.
	ReadTimeout time.Duration

	mu            sync.RWMutex
	listener      net.Listener
	peers         map[string]string // agentID -> "host:port"
	agentHandler  func(fromAgentID string, payload []byte)
	topicHandlers map[string]func(fromAgentID string, payload []byte)
}

func (t *TCPTransport) readTimeout() time.Duration {
	if t.ReadTimeout > 0 {
		return t.ReadTimeout
	}
	return tcpReadTimeout
}

// NewTCPTransport creates a new TCP transport for a given node.
// peerAddrs has entries of the form "agentID@host:port".
func NewTCPTransport(nodeID, listenAddr string, peerAddrs []string) *TCPTransport {
	peers := make(map[string]string)
	for _, p := range peerAddrs {
		parts := strings.SplitN(p, "@", 2)
		if len(parts) != 2 {
			continue
		}
		peers[parts[0]] = parts[1]
	}
	return &TCPTransport{
		nodeID:        nodeID,
		listenAddr:    listenAddr,
		peers:         peers,
		topicHandlers: make(map[string]func(string, []byte)),
		ReadTimeout:   tcpReadTimeout,
	}
}

func (t *TCPTransport) Start() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.listenAddr == "" {
		return errors.New("listen address required for TCP transport")
	}
	ln, err := net.Listen("tcp", t.listenAddr)
	if err != nil {
		return err
	}
	t.listener = ln

	go t.acceptLoop()
	return nil
}

func (t *TCPTransport) acceptLoop() {
	// connSlots bounds the number of concurrently handled inbound
	// connections (see tcpMaxConns's doc comment above). A connection that
	// arrives once this many are already active is closed immediately
	// rather than adding another unbounded goroutine.
	connSlots := make(chan struct{}, tcpMaxConns)
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

func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		// Bound how long this connection may take to deliver one complete
		// framed message. Reset on every iteration, so a connection
		// actively exchanging messages -- even slowly -- is never
		// penalized; only one that goes silent mid-message is dropped.
		if err := conn.SetReadDeadline(time.Now().Add(t.readTimeout())); err != nil {
			return
		}
		// Read 4-byte big-endian length.
		lenBuf := make([]byte, 4)
		if _, err := io.ReadFull(r, lenBuf); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(lenBuf)
		if n == 0 || n > 10*1024*1024 {
			// Ignore unreasonable sizes.
			return
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return
		}
		t.dispatch(buf)
	}
}

func (t *TCPTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listener != nil {
		return t.listener.Close()
	}
	return nil
}

func (t *TCPTransport) SendToAgent(agentID string, payload []byte) error {
	t.mu.RLock()
	addr, ok := t.peers[agentID]
	t.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown peer agentID=%s", agentID)
	}
	return t.sendToAddr(addr, payload)
}

func (t *TCPTransport) Broadcast(topic string, payload []byte) error {
	// For now, broadcast = send to all peers.
	t.mu.RLock()
	addrs := make([]string, 0, len(t.peers))
	for _, addr := range t.peers {
		addrs = append(addrs, addr)
	}
	t.mu.RUnlock()

	for _, addr := range addrs {
		_ = t.sendToAddr(addr, payload)
	}
	return nil
}

func (t *TCPTransport) sendToAddr(addr string, payload []byte) error {
	conn, err := net.DialTimeout("tcp", addr, tcpDialTimeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	// Without a write deadline, a peer that accepts the connection but
	// never reads from it (hung, overloaded, or malicious) could block
	// this call indefinitely -- and Broadcast calls sendToAddr for every
	// peer in sequence, so one stuck peer would stall delivery to every
	// peer after it.
	if err := conn.SetWriteDeadline(time.Now().Add(tcpWriteTimeout)); err != nil {
		return err
	}

	// Write 4-byte length prefix followed by payload.
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(payload)))
	if _, err := conn.Write(lenBuf); err != nil {
		return err
	}
	_, err = conn.Write(payload)
	return err
}

func (t *TCPTransport) OnAgentMessage(handler func(fromAgentID string, payload []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.agentHandler = handler
}

func (t *TCPTransport) OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.topicHandlers[topic] = handler
}

func (t *TCPTransport) dispatch(payload []byte) {
	// TCP transport receives raw envelope JSON; route it using Envelope.Topic
	// to match the pub/sub semantics expected by internal/node.
	var env Envelope
	if err := json.Unmarshal(payload, &env); err != nil {
		// If we can't decode the envelope, don't try to route.
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
