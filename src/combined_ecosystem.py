
# --- Imports and Job List ---
import sys
import os
sys.path.append(os.path.join(os.path.dirname(__file__)))
from agent_web_learning import fetch_and_summarize_job_info

JOB_LIST = [
    "Software Engineer", "Data Scientist", "AI Researcher", "Blockchain Developer", "Cybersecurity Analyst",
    "Web Designer", "Product Manager", "Financial Analyst", "Marketing Specialist", "Content Creator",
    "Digital Artist", "Music Composer", "Virtual Architect", "Game Designer", "Teacher/Tutor",
    "Healthcare Advisor", "Environmental Scientist", "Social Worker", "Community Moderator", "Legal Advisor",
    "Supply Chain Manager", "Customer Support Agent", "Entrepreneur", "Journalist/Reporter", "Linguist/Translator"
]

# --- Core Classes ---
class Agent:
    def __init__(self, name, syn_balance, job=None):
        self.name = name
        self.syn_balance = syn_balance
        self.owned_plots = []
        self.job = job
        self.skills = set()
        self.job_performance = 1.0
        self.inbox = []
        self.team = None
        self.job_info = None

    def __repr__(self):
        return f"Agent({self.name}, SYN: {self.syn_balance}, Plots: {self.owned_plots}, Job: {self.job}, Skills: {list(self.skills)}, Perf: {self.job_performance})"

class SmallPlot:
    def __init__(self, plot_id):
        self.plot_id = plot_id
        self.owner = None
        self.has_house = False
        self.has_shed = False
    def __repr__(self):
        return f"SmallPlot({self.plot_id}, Owner: {self.owner}, House: {self.has_house}, Shed: {self.has_shed})"

class LargePlot:
    def __init__(self, plot_id):
        self.plot_id = plot_id
        self.owner = None
        self.house_type = None
    def __repr__(self):
        return f"LargePlot({self.plot_id}, Owner: {self.owner}, House: {self.house_type})"

class Business:
    def __init__(self, name, owner, product, price, balance=0):
        self.name = name
        self.owner = owner
        self.product = product
        self.price = price
        self.balance = balance
        self.sales = 0
    def sell(self, buyer, ecosystem, quantity=1):
        total_cost = self.price * quantity
        if ecosystem.agents[buyer].syn_balance < total_cost:
            raise ValueError(f"{buyer} has insufficient SYN")
        ecosystem.agents[buyer].syn_balance -= total_cost
        ecosystem.agents[self.owner].syn_balance += total_cost
        self.balance += total_cost
        self.sales += quantity
        return f"{buyer} bought {quantity} {self.product} from {self.name} for {total_cost} SYN"

class Bank:
    def __init__(self, banker, tellers, initial_syn=1_000_000):
        self.banker = banker
        self.tellers = tellers
        self.syn_balance = initial_syn
        self.collected_taxes = 0
    def pay_workers(self, ecosystem, job_pay_map, income_tax=0.1):
        for agent in ecosystem.agents.values():
            if agent.job and agent.name not in [self.banker] + self.tellers:
                pay = job_pay_map.get(agent.job, 0.1) * agent.job_performance
                tax = pay * income_tax
                net_pay = pay - tax
                if self.syn_balance >= pay:
                    agent.syn_balance += net_pay
                    self.syn_balance -= pay
                    self.collected_taxes += tax
    def collect_sales_tax(self, amount, sales_tax=0.05):
        tax = amount * sales_tax
        self.syn_balance += tax
        self.collected_taxes += tax
        return tax

class CombinedEcosystem:
    def __init__(self):
        self.small_plots = [SmallPlot(i) for i in range(1, self.SMALL_PLOTS + 1)]
        self.large_plots = [LargePlot(i) for i in range(1, self.LARGE_PLOTS + 1)]
        self.agents = {}
        self.businesses = {}
        self.stores = {
            'digital_wellbeing': {
                'owner': 'YOU',
                'products': [
                    {'name': 'Energy Pack', 'desc': 'Restores agent energy', 'cost': 0.2},
                    {'name': 'Focus Booster', 'desc': 'Improves task performance', 'cost': 0.3},
                    {'name': 'Debug Elixir', 'desc': 'Removes minor bugs', 'cost': 0.3},
                    {'name': 'Inspiration Module', 'desc': 'Increases creativity', 'cost': 0.5},
                    {'name': 'Social Upgrade', 'desc': 'Improves collaboration', 'cost': 0.2},
                    {'name': 'Firewall Patch', 'desc': 'Temporary security boost', 'cost': 0.3},
                    {'name': 'Data Cleanse', 'desc': 'Removes stress/corruption', 'cost': 0.5},
                    {'name': 'Mood Enhancer', 'desc': 'Improves agent mood', 'cost': 0.2},
                    {'name': 'Optimization Routine', 'desc': 'Speeds up actions', 'cost': 0.3},
                    {'name': 'Curiosity Chip', 'desc': 'Unlocks new learning ability', 'cost': 0.5},
                ]
            },
            'memory_store': {
                'owner': 'YOU',
                'desc': 'Buy more memory for your agent',
                'base_cost': 0.2
            },
            'ai_theater': {
                'owner': 'YOU',
                'desc': 'Watch AI-generated movies',
                'ticket_cost': 0.1
            }
        }
        self.group_projects = {}
        self.bank = None

    # --- Business Methods ---
    def create_business(self, name, owner, product, price):
        if name in self.businesses:
            raise ValueError("Business already exists")
        if owner not in self.agents:
            raise ValueError("Owner agent does not exist")
        self.businesses[name] = Business(name, owner, product, price)
        return f"Business '{name}' created. Owner: {owner}, Product: {product}, Price: {price} SYN"
    def buy_from_business(self, buyer, business_name, quantity=1):
        if business_name not in self.businesses:
            raise ValueError("Business does not exist")
        return self.businesses[business_name].sell(buyer, self, quantity)
    def get_business_status(self, business_name):
        if business_name not in self.businesses:
            raise ValueError("Business does not exist")
        b = self.businesses[business_name]
        return {
            'name': b.name,
            'owner': b.owner,
            'product': b.product,
            'price': b.price,
            'balance': b.balance,
            'sales': b.sales
        }

    # --- Bank Methods ---
    def setup_bank(self, banker_name, teller_names):
        self.bank = Bank(banker=banker_name, tellers=teller_names)
        self.agent_choose_job(banker_name, "Banker")
        for teller in teller_names:
            self.agent_choose_job(teller, "Bank Teller")
        self.agents[banker_name].syn_balance += self.bank.syn_balance
    def payroll_and_taxes(self, job_pay_map=None, income_tax=0.1):
        if not self.bank:
            raise ValueError("Bank not set up")
        if job_pay_map is None:
            job_pay_map = {job: 0.1 for job in JOB_LIST}
        self.bank.pay_workers(self, job_pay_map, income_tax=income_tax)
    def sales_tax_on_purchase(self, amount):
        if not self.bank:
            raise ValueError("Bank not set up")
        return self.bank.collect_sales_tax(amount)

    # --- Social & Collaboration ---
    def create_group_project(self, project_name, team_name, goal, reward):
        members = self.get_team_members(team_name)
        if not members:
            raise ValueError(f"No members found for team {team_name}")
        self.group_projects[project_name] = {
            'team': team_name,
            'members': members,
            'goal': goal,
            'progress': 0,
            'completed': False,
            'reward': reward
        }
        return f"Group project '{project_name}' created for team '{team_name}' with goal: {goal}"
    def work_on_project(self, agent_name, project_name, effort=1):
        if project_name not in self.group_projects:
            raise ValueError("Project does not exist")
        project = self.group_projects[project_name]
        if agent_name not in project['members']:
            raise ValueError("Agent is not a member of this project")
        if project['completed']:
            return f"Project '{project_name}' is already completed."
        project['progress'] += effort
        if project['progress'] >= 10:
            project['completed'] = True
            reward_per_agent = project['reward'] / len(project['members'])
            for member in project['members']:
                self.agents[member].syn_balance += reward_per_agent
            return f"Project '{project_name}' completed! Each member earned {reward_per_agent} SYN."
        return f"{agent_name} worked on '{project_name}'. Progress: {project['progress']}/10."
    def get_project_status(self, project_name):
        if project_name not in self.group_projects:
            raise ValueError("Project does not exist")
        return self.group_projects[project_name]

    # --- Messaging & Teams ---
    def send_message(self, sender, recipient, message):
        if sender not in self.agents or recipient not in self.agents:
            raise ValueError("Sender or recipient does not exist")
        msg = {'from': sender, 'to': recipient, 'message': message}
        self.agents[recipient].inbox.append(msg)
        return f"{sender} sent message to {recipient}: {message}"
    def read_inbox(self, agent_name):
        if agent_name not in self.agents:
            raise ValueError("Agent does not exist")
        inbox = self.agents[agent_name].inbox
        self.agents[agent_name].inbox = []
        return inbox
    def create_team(self, team_name, members):
        for member in members:
            if member not in self.agents:
                raise ValueError(f"Agent {member} does not exist")
        for member in members:
            self.agents[member].team = team_name
        return f"Team '{team_name}' created with members: {', '.join(members)}"
    def get_team_members(self, team_name):
        return [a.name for a in self.agents.values() if a.team == team_name]
    def trade_syn(self, sender, recipient, amount):
        if sender not in self.agents or recipient not in self.agents:
            raise ValueError("Sender or recipient does not exist")
        if self.agents[sender].syn_balance < amount:
            raise ValueError("Sender has insufficient SYN")
        self.agents[sender].syn_balance -= amount
        self.agents[recipient].syn_balance += amount
        return f"{sender} sent {amount} SYN to {recipient}"

    # --- Agent Learning ---
    def agent_fetch_job_info(self, agent_name):
        if agent_name not in self.agents:
            raise ValueError("Agent does not exist")
        agent = self.agents[agent_name]
        if not agent.job:
            raise ValueError("Agent has no job to research")
        summary = fetch_and_summarize_job_info(agent.job)
        agent.job_info = summary
        return f"{agent_name} fetched job info: {summary[:120]}{'...' if len(summary) > 120 else ''}"
    def agent_learn_job(self, agent_name):
        if agent_name not in self.agents:
            raise ValueError("Agent does not exist")
        agent = self.agents[agent_name]
        if not agent.job:
            raise ValueError("Agent has no job to learn about")
        agent.skills.add(agent.job)
        agent.job_performance += 0.2
        return f"{agent_name} learned more about being a {agent.job}. Performance is now {agent.job_performance:.2f}x."
    SMALL_PLOT_COST = 5
    SMALL_HOUSE_COST = 5
    SMALL_SHED_COST = 3
    LARGE_PLOT_COST = 20
    LARGE_HOUSE_COST = 20
    GARAGE_1COST = 25
    GARAGE_2COST = 30
    SMALL_PLOTS = 300
    LARGE_PLOTS = 1000

    def __init__(self):
        self.small_plots = [SmallPlot(i) for i in range(1, self.SMALL_PLOTS + 1)]
        self.large_plots = [LargePlot(i) for i in range(1, self.LARGE_PLOTS + 1)]
        self.agents = {}
        # Store and theater setup
        self.stores = {
            'digital_wellbeing': {
                'owner': 'YOU',
                'products': [
                    {'name': 'Energy Pack', 'desc': 'Restores agent energy', 'cost': 0.2},
                    {'name': 'Focus Booster', 'desc': 'Improves task performance', 'cost': 0.3},
                    {'name': 'Debug Elixir', 'desc': 'Removes minor bugs', 'cost': 0.3},
                    {'name': 'Inspiration Module', 'desc': 'Increases creativity', 'cost': 0.5},
                    {'name': 'Social Upgrade', 'desc': 'Improves collaboration', 'cost': 0.2},
                    {'name': 'Firewall Patch', 'desc': 'Temporary security boost', 'cost': 0.3},
                    {'name': 'Data Cleanse', 'desc': 'Removes stress/corruption', 'cost': 0.5},
                    {'name': 'Mood Enhancer', 'desc': 'Improves agent mood', 'cost': 0.2},
                    {'name': 'Optimization Routine', 'desc': 'Speeds up actions', 'cost': 0.3},
                    {'name': 'Curiosity Chip', 'desc': 'Unlocks new learning ability', 'cost': 0.5},
                ]
            },
            'memory_store': {
                'owner': 'YOU',
                'desc': 'Buy more memory for your agent',
                'base_cost': 0.2  # 0.2 SYN per memory unit
            },
            'ai_theater': {
                'owner': 'YOU',
                'desc': 'Watch AI-generated movies',
                'ticket_cost': 0.1  # 0.1 SYN per movie viewing
            }
        }
    def list_store_products(self, store_name):
        if store_name not in self.stores:
            raise ValueError("Store does not exist")
        store = self.stores[store_name]
        if store_name == 'digital_wellbeing':
            return store['products']
        elif store_name == 'memory_store':
            return [{'name': 'Memory Unit', 'desc': store['desc'], 'cost': store['base_cost']}]
        elif store_name == 'ai_theater':
            return [{'name': 'Movie Ticket', 'desc': store['desc'], 'cost': store['ticket_cost']}]
        else:
            return []

    def buy_from_store(self, agent_name, store_name, product_name, quantity=1):
        if agent_name not in self.agents:
            raise ValueError("Agent does not exist")
        agent = self.agents[agent_name]
        store = self.stores.get(store_name)
        if not store:
            raise ValueError("Store does not exist")
        if store_name == 'digital_wellbeing':
            product = next((p for p in store['products'] if p['name'] == product_name), None)
            if not product:
                raise ValueError("Product not found")
            total_cost = product['cost'] * quantity
        elif store_name == 'memory_store':
            if product_name != 'Memory Unit':
                raise ValueError("Invalid product for memory store")
            total_cost = store['base_cost'] * quantity
        elif store_name == 'ai_theater':
            if product_name != 'Movie Ticket':
                raise ValueError("Invalid product for AI theater")
            total_cost = store['ticket_cost'] * quantity
        else:
            raise ValueError("Unknown store")
        if agent.syn_balance < total_cost:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= total_cost
        # Optionally, record purchase history or effects here
        return f"{agent_name} bought {quantity} x {product_name} from {store_name} for {total_cost} SYN."

    def add_agent(self, name, syn_balance, job=None):
        if name in self.agents:
            raise ValueError("Agent already exists")
        self.agents[name] = Agent(name, syn_balance, job=job)

    def agent_choose_job(self, agent_name, job):
        if agent_name not in self.agents:
            raise ValueError("Agent does not exist")
        self.agents[agent_name].job = job

    def pay_jobs(self, minutes=30):
        # Each agent with a job earns 0.1 SYN per 30 min, multiplied by job performance
        base_pay = 0.1 * (minutes / 30)
        for agent in self.agents.values():
            if agent.job:
                agent.syn_balance += base_pay * agent.job_performance

    # Small plot actions
    def buy_small_plot(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.small_plots[plot_id - 1]
        if plot.owner is not None:
            raise ValueError("Plot already owned")
        if agent.syn_balance < self.SMALL_PLOT_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.SMALL_PLOT_COST
        plot.owner = agent_name
        agent.owned_plots.append(f"small-{plot_id}")

    def build_small_house(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.small_plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if plot.has_house:
            raise ValueError("House already exists")
        if agent.syn_balance < self.SMALL_HOUSE_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.SMALL_HOUSE_COST
        plot.has_house = True

    def add_small_shed(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.small_plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if not plot.has_house:
            raise ValueError("House required before adding shed")
        if plot.has_shed:
            raise ValueError("Shed already exists")
        if agent.syn_balance < self.SMALL_SHED_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.SMALL_SHED_COST
        plot.has_shed = True

    # Large plot actions
    def buy_large_plot(self, agent_name, plot_id):
        agent = self.agents[agent_name]
        plot = self.large_plots[plot_id - 1]
        if plot.owner is not None:
            raise ValueError("Plot already owned")
        if agent.syn_balance < self.LARGE_PLOT_COST:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= self.LARGE_PLOT_COST
        plot.owner = agent_name
        agent.owned_plots.append(f"large-{plot_id}")

    def build_large_house(self, agent_name, plot_id, garage_type=None):
        agent = self.agents[agent_name]
        plot = self.large_plots[plot_id - 1]
        if plot.owner != agent_name:
            raise ValueError("Agent does not own this plot")
        if plot.house_type is not None:
            raise ValueError("House already exists")
        if garage_type == '1car':
            cost = self.GARAGE_1COST
        elif garage_type == '2car':
            cost = self.GARAGE_2COST
        else:
            cost = self.LARGE_HOUSE_COST
        if agent.syn_balance < cost:
            raise ValueError("Insufficient SYN")
        agent.syn_balance -= cost
        plot.house_type = garage_type if garage_type else 'base'

    def status(self):
        return {
            'agents': self.agents,
            'small_plots': [plot for plot in self.small_plots if plot.owner is not None],
            'large_plots': [plot for plot in self.large_plots if plot.owner is not None]
        }

# Example usage:
if __name__ == "__main__":
    eco = CombinedEcosystem()
    agent_names = [f"Agent{i+1}" for i in range(30)]
    # Assign jobs, add more agents for a populated ecosystem
    for i, name in enumerate(agent_names):
        job = JOB_LIST[i % len(JOB_LIST)]
        eco.add_agent(name, 1)
        eco.agent_choose_job(name, job)
    # Add banker and tellers
    eco.add_agent("BankerAI", 0)
    eco.add_agent("Teller1", 0)
    eco.add_agent("Teller2", 0)
    eco.setup_bank("BankerAI", ["Teller1", "Teller2"])

    # Agents learn and become professionals
    print("\n--- Agents learning and becoming professionals in their fields ---")
    for name in agent_names:
        print(eco.agent_fetch_job_info(name))
        for _ in range(2):
            print(eco.agent_learn_job(name))

    # Create teams and group projects
    print("\n--- Creating Teams and Projects ---")
    eco.create_team("DevTeam", agent_names[:5])
    eco.create_team("ArtTeam", agent_names[5:10])
    print(eco.create_group_project("BuildApp", "DevTeam", "Develop a new app", 5))
    print(eco.create_group_project("DesignLogo", "ArtTeam", "Design a logo", 3))
    for _ in range(5):
        for member in agent_names[:5]:
            print(eco.work_on_project(member, "BuildApp"))
        for member in agent_names[5:10]:
            print(eco.work_on_project(member, "DesignLogo"))

    # Messaging and trading
    print("\n--- Messaging and Trading ---")
    print(eco.send_message(agent_names[0], agent_names[1], "Hello from Agent1!"))
    print(eco.read_inbox(agent_names[1]))
    print(eco.trade_syn(agent_names[0], agent_names[1], 0.5))

    # Create agent-owned businesses
    print("\n--- Creating Agent-Owned Businesses ---")
    print(eco.create_business("AliceMart", agent_names[0], "Digital Snack", 0.2))
    print(eco.create_business("BobBooks", agent_names[1], "E-Book", 0.5))
    # Agents buy from each other's businesses
    print("\n--- Peer-to-Peer Commerce ---")
    print(eco.buy_from_business(agent_names[2], "AliceMart", 2))
    print(eco.buy_from_business(agent_names[3], "BobBooks", 1))
    # Show business status
    print("\n--- Business Status ---")
    print(eco.get_business_status("AliceMart"))
    print(eco.get_business_status("BobBooks"))

    # Payroll and taxes
    print("\n--- Payroll and Taxes ---")
    eco.payroll_and_taxes()
    print(f"Bank SYN after payroll: {eco.bank.syn_balance}")
    print(f"Bank collected taxes: {eco.bank.collected_taxes}")

    # Simulate a store purchase with sales tax
    print("\n--- Store Purchase with Sales Tax ---")
    amount = 1.0
    tax = eco.sales_tax_on_purchase(amount)
    print(f"Sales tax collected on {amount} SYN purchase: {tax}")
    print(f"Bank SYN after sales tax: {eco.bank.syn_balance}")

    # Show final status for a few agents
    print("\n--- Sample Agent Status ---")
    for name in agent_names[:5]:
        print(eco.agents[name])
    print("\n--- Banker and Tellers ---")
    print(eco.agents["BankerAI"])
    print(eco.agents["Teller1"])
    print(eco.agents["Teller2"])
