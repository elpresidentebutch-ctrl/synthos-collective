package network

// CommunicatorPayload is the envelope payload for a Communicator role
// (docs/AGENTS_SPECIFICATION.md role 4) send_message/handle_incoming_message
// exchange: an arbitrary, agent-authored text body sent directly to one
// named peer. It deliberately carries nothing else -- routing, delivery
// confirmation, and consensus-relevant messages all have their own
// dedicated envelope types (block_proposal, block_vote, bridge/cover-noise
// topics) and are not this.
type CommunicatorPayload struct {
	Body string `json:"body"`
}

// MessageCommunicator is the network.Envelope.MessageType value used for a
// Communicator role message.
const MessageCommunicator = "communicator_message"
