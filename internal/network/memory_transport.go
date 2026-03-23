package network

import (
	"errors"
	"sync"
)

// MemoryTransport is a local-only transport useful for testing the full
// envelope verification pipeline without standing up any real relay servers.
// It simulates an untrusted relay fabric by allowing arbitrary fanout.
type MemoryTransport struct {
	mu            sync.RWMutex
	nodes         map[string]*memoryNode
	started       bool
}

type memoryNode struct {
	agentHandler  func(fromAgentID string, payload []byte)
	topicHandlers map[string]func(fromAgentID string, payload []byte)
}

func NewMemoryTransport() *MemoryTransport {
	return &MemoryTransport{
		nodes: make(map[string]*memoryNode),
	}
}

func (m *MemoryTransport) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = true
	return nil
}

func (m *MemoryTransport) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// Attach registers handlers for a logical agent ID.
func (m *MemoryTransport) Attach(agentID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.nodes[agentID]; ok {
		return
	}
	m.nodes[agentID] = &memoryNode{topicHandlers: make(map[string]func(string, []byte))}
}

// NodeTransport returns a Transport view scoped to a specific agent ID.
func (m *MemoryTransport) NodeTransport(agentID string) Transport {
	m.Attach(agentID)
	return &memoryNodeTransport{bus: m, self: agentID}
}

type memoryNodeTransport struct {
	bus  *MemoryTransport
	self string
}

func (t *memoryNodeTransport) Start() error  { return t.bus.Start() }
func (t *memoryNodeTransport) Close() error  { return t.bus.Close() }

func (t *memoryNodeTransport) SendToAgent(agentID string, payload []byte) error {
	t.bus.mu.RLock()
	defer t.bus.mu.RUnlock()
	if !t.bus.started {
		return errors.New("transport not started")
	}
	n, ok := t.bus.nodes[agentID]
	if !ok || n.agentHandler == nil {
		return nil
	}
	// untrusted relay: deliver as-is
	n.agentHandler(t.self, payload)
	return nil
}

func (t *memoryNodeTransport) Broadcast(topic string, payload []byte) error {
	t.bus.mu.RLock()
	defer t.bus.mu.RUnlock()
	if !t.bus.started {
		return errors.New("transport not started")
	}
	for _, n := range t.bus.nodes {
		if h := n.topicHandlers[topic]; h != nil {
			h(t.self, payload)
		}
	}
	return nil
}

func (t *memoryNodeTransport) OnAgentMessage(handler func(fromAgentID string, payload []byte)) {
	t.bus.mu.Lock()
	defer t.bus.mu.Unlock()
	n := t.bus.nodes[t.self]
	n.agentHandler = handler
}

func (t *memoryNodeTransport) OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte)) {
	t.bus.mu.Lock()
	defer t.bus.mu.Unlock()
	n := t.bus.nodes[t.self]
	n.topicHandlers[topic] = handler
}

