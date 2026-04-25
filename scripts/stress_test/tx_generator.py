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
    sender = f"0x{binascii.hexlify(os.urandom(4)).decode()}"
    recipient = f"0x{binascii.hexlify(os.urandom(4)).decode()}"
    amount = random.randint(1, 1000)
    
    msg = {
        "id": tx_id,
        "type": "transaction",
        "sender": sender,
        "height": height,
        "timestamp": int(time.time()),
        "payload": json.dumps({
            "from": sender,
            "to": recipient,
            "amount": amount,
            "fee": 1
        }),
        "signature": "mock-sig-" + binascii.hexlify(os.urandom(8)).decode(),
        "created_at": int(time.time())
    }
    return msg

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
    TARGET_SYN = 20000
    
    try:
        while total_syn_moved < TARGET_SYN:
            # Generate 10 transactions per batch for higher stress
            batch_syn = 0
            for _ in range(10):
                msg = generate_tx(height)
                payload = json.loads(msg['payload'])
                total_syn_moved += payload['amount']
                batch_syn += payload['amount']
                
                file_path = os.path.join(MESSAGES_DIR, f"{msg['id']}.json")
                with open(file_path, "w") as f:
                    json.dump(msg, f)
                count += 1
            
            progress = (total_syn_moved / TARGET_SYN) * 100
            print(f"[{time.strftime('%H:%M:%S')}] Broadcasted 10 TXs. Batch: {batch_syn} SYN. Total: {total_syn_moved}/{TARGET_SYN} SYN ({progress:.1f}%)")
            
            height += 1
            time.sleep(1) # Higher frequency: 1 second

            
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

        print(f"\n✅ STRESS TEST COMPLETE: {total_syn_moved} SYN moved across {count} transactions.")

    except KeyboardInterrupt:
        print("\nStopping transaction generator.")

if __name__ == "__main__":
    main()
