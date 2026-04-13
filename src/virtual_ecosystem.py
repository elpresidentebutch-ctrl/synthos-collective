# Virtual Ecosystem Simulation for AI Agents
# Each plot: 5 SYN, house: 5 SYN, shed: 3 SYN (only if house exists)

class Agent:
    def __init__(self, name, syn_balance):
        self.name = name
        self.syn_balance = syn_balance
        self.owned_plots = []

    def __repr__(self):
        return f"Agent({self.name}, SYN: {self.syn_balance}, Plots: {self.owned_plots})"

class Plot:
    def __init__(self, plot_id):
        self.plot_id = plot_id
        self.owner = None
        self.has_house = False
        self.has_shed = False

    def __repr__(self):
        return f"Plot({self.plot_id}, Owner: {self.owner}, House: {self.has_house}, Shed: {self.has_shed})"

class Ecosystem:
    PLOT_COST = 5
    HOUSE_COST = 5
    SHED_COST = 3
    TOTAL_PLOTS = 300

    def __init__(self):
        self.plots = [Plot(i) for i in range(1, self.TOTAL_PLOTS + 1)]
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
        agent.owned_plots.append(plot_id)

    def build_house(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if plot.has_house:
            raise ValueError("House already exists")
        if agent.syn_balance < self.HOUSE_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.HOUSE_COST
        plot.has_house = True

    def add_shed(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if not plot.has_house:
            raise ValueError("House required before adding shed")
        if plot.has_shed:
            raise ValueError("Shed already exists")
        if agent.syn_balance < self.SHED_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.SHED_COST
        plot.has_shed = True

    def status(self):
        return {
            'agents': self.agents,
            'plots': [plot for plot in self.plots if plot.owner is not None]
        }

# Example usage:
if __name__ == "__main__":
    eco = Ecosystem()
    eco.add_agent("Alice", 20)
    eco.buy_plot("Alice", 1)
    eco.build_house("Alice", 1)
    eco.add_shed("Alice", 1)
    print(eco.status())
