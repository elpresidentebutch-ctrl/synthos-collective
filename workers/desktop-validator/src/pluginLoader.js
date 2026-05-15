// SYNTHOS Mobile Validator Plugin Loader (JS)
// Dynamically loads plugins and registers their capabilities with the mobile agent

export class MobileAgent {
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

// Example usage (in browser):
// import * as negotiation from '../../../plugins/js/negotiation.js';
// const agent = new MobileAgent();
// await agent.loadPlugin(negotiation);
// console.log(agent.listCapabilities());
