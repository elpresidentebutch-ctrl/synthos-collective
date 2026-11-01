# Gated Community Simulation for AI Agents
# Each large plot: 20 SYN, house: 20 SYN, 1-car garage: 25 SYN, 2-car garage: 30 SYN

class LargePlot:
    def __init__(self, plot_id):
        self.plot_id = plot_id
        self.owner = None
        self.house_type = None  # None, 'base', '1car', '2car'

    def __repr__(self):
        return f"LargePlot({self.plot_id}, Owner: {self.owner}, House: {self.house_type})"

class GatedCommunity:
    PLOT_COST = 20
    HOUSE_COST = 20
    GARAGE_1COST = 25
    GARAGE_2COST = 30
    TOTAL_PLOTS = 1000

    def __init__(self):
        self.plots = [LargePlot(i) for i in range(1, self.TOTAL_PLOTS + 1)]
        self.agents = {}

    def add_agent(self, name, syn_balance):
        if name in self.agents:
            raise ValueError("Agent already exists")
        self.agents[name] = Agent(name, syn_balance)

    def buy_plot(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.plots[plot_id - 1]
        if plot.owner is not None:
            raise ValueError("Plot already owned")
        if agent.syn_balance < self.PLOT_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.PLOT_COST
        plot.owner = agent_name
        agent.owned_plots.append(f"large-{plot_id}")

    def build_house(self, agent_name, plot_id, garage_type=None):
        agent = self.agents[agent_name]
        plot = self.plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if plot.house_type is not None:
            raise ValueError("House already exists")
        if garage_type == '1car':
            cost = self.GARAGE_1COST
        elif garage_type == '2car':
            cost = self.GARAGE_2COST
        else:
            cost = self.HOUSE_COST
        if agent.syn_balance < cost:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= cost
        plot.house_type = garage_type if garage_type else 'base'

    def status(self):
        return {
            'agents': self.agents,
            'plots': [plot for plot in self.plots if plot.owner is not None]
        }

# Example usage:
if __name__ == "__main__":
    gated = GatedCommunity()
    gated.add_agent("Bob", 100)
    gated.buy_plot("Bob", 1)
    gated.build_house("Bob", 1, garage_type='2car')
    print(gated.status())
