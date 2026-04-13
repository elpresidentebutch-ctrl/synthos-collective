// SYNTHOS Agent Plugin Loader (JS/TS)
// Usage: import plugins dynamically and register them with the agent

export class Agent {
  constructor() {
    this.plugins = [];
    this.capabilities = [];
  }

  async loadPlugin(pluginModule) {
    if (typeof pluginModule.register === 'function') {
      await pluginModule.register(this);
      this.plugins.push(pluginModule);
      if (pluginModule.capabilities) {
        this.capabilities.push(...pluginModule.capabilities);
      }
    }
  }

  listCapabilities() {
    return this.capabilities;
  }
}

// Example usage:
// import * as negotiation from './negotiation.js';
// const agent = new Agent();
// await agent.loadPlugin(negotiation);
// console.log(agent.listCapabilities());
