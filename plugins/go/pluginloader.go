// SYNTHOS Agent Plugin Loader (Go)
// Usage: import plugins and register them with the agent
package plugins

type Agent struct {
	Plugins      []AgentPlugin
	Capabilities []string
}

type AgentPlugin interface {
	Register(agent *Agent) error
	Capabilities() []string
}

func (a *Agent) LoadPlugin(p AgentPlugin) error {
	err := p.Register(a)
	if err != nil {
		return err
	}
	a.Plugins = append(a.Plugins, p)
	a.Capabilities = append(a.Capabilities, p.Capabilities()...)
	return nil
}

func (a *Agent) ListCapabilities() []string {
	return a.Capabilities
}
