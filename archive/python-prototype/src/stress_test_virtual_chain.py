"""
stress_test_virtual_chain.py

Simulates a stress test of the fragmented ledger/agent-native blockchain.
- Launches multiple agent threads, each holding random fragments
- Simulates high transaction volume and random agent failures
- Collects and prints metrics for throughput, latency, and recovery
"""
import threading
import time
import random
from collections import defaultdict
from fragmented_ledger import LedgerFragment, Agent

NUM_AGENTS = 20
NUM_FRAGMENTS = 10
NUM_TRANSACTIONS = 200
FRAGMENT_REPLICAS = 3
FAILURE_RATE = 0.05  # Probability an agent goes offline per tick
RECOVERY_RATE = 0.02  # Probability an offline agent recovers per tick

# Global state for simulation
fragments = {}
agents = {}
fragment_holders = defaultdict(list)
offline_agents = set()
metrics = {
    'tx_success': 0,
    'tx_failed': 0,
    'recovery_events': 0,
    'latencies': [],
}

# Setup fragments and agents
def setup():
    for i in range(NUM_FRAGMENTS):
        frag = LedgerFragment(f"frag_{i}")
        fragments[frag.fragment_id] = frag
    for i in range(NUM_AGENTS):
        agent = Agent(agent_id=f"agent_{i}")
        agents[agent.agent_id] = agent
    # Assign each fragment to FRAGMENT_REPLICAS random agents
    agent_ids = list(agents.keys())
    for frag in fragments.values():
        holders = random.sample(agent_ids, FRAGMENT_REPLICAS)
        frag.holders = holders
        for aid in holders:
            fragment_holders[frag.fragment_id].append(aid)

# Agent thread logic
def agent_thread(agent_id):
    agent = agents[agent_id]
    while True:
        if agent_id in offline_agents:
            time.sleep(0.1)
            continue
        # Randomly process a transaction
        frag_ids = random.sample(list(fragments.keys()), k=random.randint(1, 3))
        tx = {"fragments": frag_ids, "amount": random.randint(1, 100), "from": agent_id, "to": f"agent_{random.randint(0, NUM_AGENTS-1)}"}
        start = time.time()
        success = agent.submit_transaction(tx, fragments)
        latency = time.time() - start
        metrics['latencies'].append(latency)
        if success:
            metrics['tx_success'] += 1
        else:
            metrics['tx_failed'] += 1
        time.sleep(random.uniform(0.01, 0.05))

def simulate_failures():
    # Randomly take agents offline and recover them
    while True:
        for agent_id in agents:
            if agent_id not in offline_agents and random.random() < FAILURE_RATE:
                offline_agents.add(agent_id)
                metrics['recovery_events'] += 1
            elif agent_id in offline_agents and random.random() < RECOVERY_RATE:
                offline_agents.remove(agent_id)
        time.sleep(0.1)

def main():
    setup()
    threads = []
    for agent_id in agents:
        t = threading.Thread(target=agent_thread, args=(agent_id,), daemon=True)
        t.start()
        threads.append(t)
    fail_thread = threading.Thread(target=simulate_failures, daemon=True)
    fail_thread.start()
    # Run simulation
    print("Starting stress test...")
    start_time = time.time()
    for _ in range(NUM_TRANSACTIONS):
        time.sleep(0.01)
    # Allow threads to finish
    time.sleep(2)
    duration = time.time() - start_time
    # Print results
    print("\n--- Stress Test Results ---")
    print(f"Total transactions attempted: {metrics['tx_success'] + metrics['tx_failed']}")
    print(f"Successful transactions: {metrics['tx_success']}")
    print(f"Failed transactions: {metrics['tx_failed']}")
    print(f"Recovery events (agent offline/online): {metrics['recovery_events']}")
    if metrics['latencies']:
        print(f"Average transaction latency: {sum(metrics['latencies'])/len(metrics['latencies']):.4f} seconds")
        print(f"Max transaction latency: {max(metrics['latencies']):.4f} seconds")
    print(f"Test duration: {duration:.2f} seconds")

if __name__ == "__main__":
    main()
