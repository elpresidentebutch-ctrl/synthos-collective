package network

import (
	"sync"
)

// RelayTransport is a skeleton implementation of Transport that is intended
// to connect outbound-only to one or more relay servers (e.g. via WebSocket).
// For now this is a stub that defines the structure and callbacks, without
// binding to any specific protocol. This keeps your home machine completely
// free of inbound listeners.
type RelayTransport struct {
	relayEndpoints []string

	mu                sync.RWMutex
	agentHandler      func(fromAgentID string, payload []byte)
	topicHandlers     map[string]func(fromAgentID string, payload []byte)
	started           bool
}

// NewRelayTransport constructs a RelayTransport targeting the given relay URLs.
func NewRelayTransport(relays []string) *RelayTransport {
	return &RelayTransport{
		relayEndpoints: relays,
		topicHandlers:  make(map[string]func(string, []byte)),
	}
}

// Start would establish outbound connections to relays and begin processing.
// For now, it's a no-op placeholder so we can wire the abstraction into the agent.
func (r *RelayTransport) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = true
	return nil
}

// Close shuts down the transport.
func (r *RelayTransport) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = false
	return nil
}

// SendToAgent sends a direct message to a logical agent ID through the relay mesh.
// This is a stub; protocol-specific sending will be added later.
func (r *RelayTransport) SendToAgent(agentID string, payload []byte) error {
	// TODO: implement outbound send via relay protocol
	_ = agentID
	_ = payload
	return nil
}

// Broadcast sends a message to all peers subscribed to a topic.
func (r *RelayTransport) Broadcast(topic string, payload []byte) error {
	// TODO: implement outbound broadcast via relay protocol
	_ = topic
	_ = payload
	return nil
}

// OnAgentMessage registers a handler for direct messages.
func (r *RelayTransport) OnAgentMessage(handler func(fromAgentID string, payload []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentHandler = handler
}

// OnTopicMessage registers a handler for messages on a topic.
func (r *RelayTransport) OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.topicHandlers[topic] = handler
}

