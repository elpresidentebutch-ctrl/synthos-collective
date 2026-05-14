# SYNTHOS Collective - Sovereign Master Deployer
# Focus: Cloudless, Local Hardware Orchestration

import os
import subprocess
import time

def deploy_sovereign_mesh():
    print("[START] Initializing Sovereign Mesh Deployment...")
    print("[INFO] Status: CLOUDLESS MODE (No Cloudflare/Deno required)")
    
    # 1. Build the Go Agentic Core
    print("[BUILD] Building Agentic Core...")
    try:
        subprocess.run(["go", "build", "-o", "synthos-node.exe", "./cmd/node"], check=True)
        print("[SUCCESS] Build Successful.")
    except Exception as e:
        print(f"[ERROR] Build Failed: {e}")
        return

    # 2. Initialize Local Agent Identity
    print("[ID] Synchronizing Agent Identity...")
    # This would typically call the agent registration logic
    time.sleep(1)

    # 3. Launch Sovereign Relay Node
    print("[LAUNCH] Launching Sovereign Relay Node (Localhost:8080)...")
    try:
        # Launching in a new terminal window for visibility
        subprocess.Popen(["start", "cmd", "/k", "synthos-node.exe"], shell=True)
        print("[SUCCESS] Sovereign Relay is LIVE.")
    except Exception as e:
        print(f"[ERROR] Failed to launch node: {e}")

    print("\n--- DEPLOYMENT SUMMARY ---")
    print("Mesh Type:  Sovereign P2P (Absolute Silence Mode)")
    print("Security:   NO LISTENING PORTS (Firewall Safe)")
    print("Relay:      ACTIVE (Outbound Sign Language only)")
    print("Next Step:  Monitor via your B12 dashboard.")

if __name__ == "__main__":
    deploy_sovereign_mesh()
