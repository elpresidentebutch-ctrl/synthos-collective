package network

// Transport is an abstract messaging layer for agents.
// It deliberately hides any notion of IP addresses or listening sockets so that
// agents can operate in outbound-only mode behind firewalls and NAT.
//
// A Transport implementation is expected to:
//   - Maintain one or more outbound connections to relay infrastructure.
//   - Deliver opaque, authenticated messages between agents identified by ID.
//   - Provide pub/sub style topics for broader coordination if needed.
type Transport interface {
	// Start establishes any required outbound connections and background
	// goroutines. It should return only after initial setup is complete.
	Start() error

	// Close cleanly tears down connections and background work.
	Close() error

	// SendToAgent sends a message addressed to a specific logical agent ID.
	SendToAgent(agentID string, payload []byte) error

	// Broadcast sends a message to all interested peers on a logical topic.
	Broadcast(topic string, payload []byte) error

	// OnAgentMessage registers a handler for direct agent-to-agent messages.
	OnAgentMessage(handler func(fromAgentID string, payload []byte))

	// OnTopicMessage registers a handler for messages received on a topic.
	OnTopicMessage(topic string, handler func(fromAgentID string, payload []byte))
}

