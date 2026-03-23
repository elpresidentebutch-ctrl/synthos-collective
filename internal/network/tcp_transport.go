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
)

// TCPTransport is a minimal TCP-based Transport implementation.
// It is intended for small devnets and two-node setups, not Internet-scale P2P.
// Messages are sent as length-prefixed JSON envelopes over TCP.
type TCPTransport struct {
	nodeID     string
	listenAddr string

	mu        sync.RWMutex
	listener  net.Listener
	peers     map[string]string // agentID -> "host:port"
	agentHandler  func(fromAgentID string, payload []byte)
	topicHandlers map[string]func(fromAgentID string, payload []byte)
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
	for {
		conn, err := t.listener.Accept()
		if err != nil {
			return
		}
		go t.handleConn(conn)
	}
}

func (t *TCPTransport) handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
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
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

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

