#!/usr/bin/env python3
"""Synthetic transaction generator for Synthos stress testing.

This script creates ServerlessMessage JSON files in the 'messages' directory
of the validator workers, simulating network traffic that the browser-based
validators will poll and process.
"""

import os, json, time, random, hashlib, binascii

BASE_DIR = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", ".."))
# We'll write to a shared directory that the HTTP servers will serve
MESSAGES_DIR = os.path.join(BASE_DIR, "workers", "desktop-validator", "synthos", "testnet", "messages")

def generate_tx(height):
    tx_id = binascii.hexlify(os.urandom(16)).decode()
    # Agent-to-Agent IDs
    agents = ["Agent-Alpha", "Agent-Beta", "Agent-Gamma", "Agent-Delta", "Agent-Epsilon"]
    sender = random.choice(agents)
    recipient = random.choice([a for a in agents if a != sender])
    amount = random.randint(100, 5000)
    
    return {
        "id": tx_id,
        "type": "transaction",
        "sender": sender,
        "height": height,
        "timestamp": int(time.time()),
        "payload": json.dumps({
            "from": sender,
            "to": recipient,
            "amount": amount,
            "fee": 1,
            "agent_auth": "ed25519-sig-" + binascii.hexlify(os.urandom(4)).decode()
        })
    }

def generate_burn(height):
    tx_id = "burn-" + binascii.hexlify(os.urandom(12)).decode()
    agents = ["Agent-Alpha", "Agent-Beta", "Agent-Gamma"]
    sender = random.choice(agents)
    amount = random.randint(1000, 10000)
    
    return {
        "id": tx_id,
        "type": "burn",
        "sender": sender,
        "height": height,
        "timestamp": int(time.time()),
        "payload": json.dumps({
            "from": sender,
            "amount": amount,
            "reason": "Economic deflation protocol"
        })
    }

def generate_block(height, tx_list):
    block_id = "block-" + binascii.hexlify(os.urandom(16)).decode()
    return {
        "id": block_id,
        "type": "block",
        "sender": "Synthos-Collective-Leader",
        "height": height,
        "timestamp": int(time.time()),
        "payload": json.dumps({
            "transactions": tx_list,
            "state_root": "0x" + binascii.hexlify(os.urandom(32)).decode(),
            "reward": 50
        })
    }

def generate_join(height):
    agent_id = f"Agent-{binascii.hexlify(os.urandom(4)).decode()}"
    return {
        "id": "join-" + binascii.hexlify(os.urandom(12)).decode(),
        "type": "join",
        "sender": agent_id,
        "height": height,
        "timestamp": int(time.time()),
        "payload": json.dumps({
            "agent_type": "Validator",
            "capabilities": ["Compute", "Verification"]
        })
    }

def main():
    if not os.path.exists(MESSAGES_DIR):
        os.makedirs(MESSAGES_DIR)
        print(f"Created messages directory: {MESSAGES_DIR}")

    print("Starting transaction generator (Stress Test mode)...")
    print("Target Volume: 20,000 SYN")
    print("Press Ctrl+C to stop.")
    
    height = 124082
    count = 0
    total_syn_moved = 0
    total_syn_burned = 0
    TARGET_SYN = 50000 # Increased for more load
    
    try:
        while total_syn_moved < TARGET_SYN:
            batch_txs = []
            
            # Generate 8 Agent-to-Agent TXs
            for _ in range(8):
                tx = generate_tx(height)
                batch_txs.append(tx)
                payload = json.loads(tx['payload'])
                total_syn_moved += payload['amount']
                
                with open(os.path.join(MESSAGES_DIR, f"{tx['id']}.json"), "w") as f:
                    json.dump(tx, f)
                count += 1

            # Occasionally burn tokens (10% chance)
            if random.random() < 0.1:
                burn = generate_burn(height)
                payload = json.loads(burn['payload'])
                total_syn_burned += payload['amount']
                with open(os.path.join(MESSAGES_DIR, f"{burn['id']}.json"), "w") as f:
                    json.dump(burn, f)
                print(f"[BURN] TOKEN BURN: {payload['amount']} SYN burned by {burn['sender']}")

            # New Agents Joining (20% chance per block)
            if random.random() < 0.2:
                join = generate_join(height)
                with open(os.path.join(MESSAGES_DIR, f"{join['id']}.json"), "w") as f:
                    json.dump(join, f)
                print(f"[JOIN] NEW AGENT VALIDATOR: {join['sender']} has joined the network!")

            # Group batch into a block
            block = generate_block(height, [t['id'] for t in batch_txs])
            with open(os.path.join(MESSAGES_DIR, f"{block['id']}.json"), "w") as f:
                json.dump(block, f)
            
            progress = (total_syn_moved / TARGET_SYN) * 100
            print(f"[{time.strftime('%H:%M:%S')}] Block {height} Built. {len(batch_txs)} TXs. Vol: {total_syn_moved}/{TARGET_SYN} SYN ({progress:.1f}%) | Burned: {total_syn_burned} SYN")
            
            height += 1
            time.sleep(1.5)

            
            # Cleanup old messages (keep last 50) and update manifest
            files = sorted([f for f in os.listdir(MESSAGES_DIR) if f.endswith(".json") and f != "manifest.json"], 
                           key=lambda x: os.path.getmtime(os.path.join(MESSAGES_DIR, x)))
            
            # Update manifest.json with the latest 20 messages
            manifest = {"latest": files[-20:]}
            with open(os.path.join(MESSAGES_DIR, "manifest.json"), "w") as f:
                json.dump(manifest, f)

            if len(files) > 50:
                for old_file in files[:-50]:
                    os.remove(os.path.join(MESSAGES_DIR, old_file))

        print(f"\n[DONE] STRESS TEST COMPLETE: {total_syn_moved} SYN moved across {count} transactions.")

    except KeyboardInterrupt:
        print("\nStopping transaction generator.")

if __name__ == "__main__":
    main()
