// Example Negotiation Plugin (Go)
package plugins

type NegotiationPlugin struct{}

func (n *NegotiationPlugin) Register(agent *Agent) error {
	// Add negotiation methods to agent here
	return nil
}

func (n *NegotiationPlugin) Capabilities() []string {
	return []string{"negotiate deals", "contract drafting", "agreement protocols"}
}
