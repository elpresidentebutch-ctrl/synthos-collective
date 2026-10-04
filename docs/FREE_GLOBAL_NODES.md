# Deploying Free Global SYNTHOS Nodes ($0/mo)

This guide documents how to launch autonomous validating and immune nodes across four global cloud providers completely for free ($0.00/month).

All nodes automatically connect to the primary consensus cluster (`synthos-validator-12`, `synthos-validator-13`, `synthos-rpc`), synchronize chain state, and report heartbeat proofs to `https://synthos-www.onrender.com`.

---

## 1. Render Multi-Region (Frankfurt & Singapore)

- **Config**: Already defined in root `render.yaml`.
- **Services**:
  - `synthos-node-europe` (Region: `frankfurt`)
  - `synthos-node-asia` (Region: `singapore`)
- **Cost**: $0.00 (Render Free Plan).
- **Deployment**: Automatic when `render.yaml` is deployed/synced in your Render Dashboard Blueprint.

---

## 2. Koyeb (Europe / Asia-Pacific Edge)

- **Config**: Root `koyeb.yaml`.
- **Instance Type**: Eco Nano ($0.00 / free tier).
- **Steps to Launch**:
  1. Go to [koyeb.com](https://www.koyeb.com) and create a free account.
  2. Click **Create App** -> Select **GitHub** or **GitLab**.
  3. Choose `synthos-collective`.
  4. Koyeb detects `koyeb.yaml` and deploys the node automatically to Frankfurt or Tokyo.

---

## 3. Hugging Face Spaces (2 vCPU / 16 GB RAM Free)

- **Config & Dockerfile**: `deploy/huggingface/`
- **Cost**: $0.00 (Permanent Free Docker Space).
- **Steps to Launch**:
  1. Go to [huggingface.co/spaces](https://huggingface.co/spaces) and click **Create new Space**.
  2. Name your Space (e.g. `synthos-global-node`).
  3. Select **Space SDK**: **Docker**.
  4. Upload or push the files from `deploy/huggingface/`:
     - `README.md`
     - `Dockerfile`
  5. The Space builds and starts syncing blocks within 2 minutes.

---

## 4. Oracle Cloud Always-Free (4 ARM Cores, 24 GB RAM, 200 GB SSD)

- **Script**: `scripts/setup-oracle-node.sh`
- **Cost**: $0.00 forever.
- **Steps to Launch**:
  1. Sign up at [oracle.com/cloud/free](https://www.oracle.com/cloud/free/).
  2. In the OCI Console, click **Create a VM instance**.
  3. Select Image: **Ubuntu 24.04** or **22.04 Minimal**.
  4. Select Shape: **Ampere ARM (VM.Standard.A1.Flex)** with up to 4 OCPUs and 24 GB RAM.
  5. SSH into your new VM:
     ```bash
     ssh ubuntu@<YOUR_ORACLE_IP>
     ```
  6. Run the 1-click installer:
     ```bash
     curl -sSL https://raw.githubusercontent.com/elpresidentebutch-ctrl/synthos-collective/main/scripts/setup-oracle-node.sh | sudo bash
     ```
  7. The script installs Docker, clones the repository, configures the node, and runs it as a background system service (`synthos-node.service`) that automatically restarts on reboot.

---

## Verification

Check if your new nodes are active and registered with the network:

```bash
curl https://synthos-www.onrender.com/api/nodes
```

Or view the live fleet on the web dashboard:
👉 `https://www.ishamwilliamsblockchains.com/nodes`
