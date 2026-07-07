package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"synthos-collective/internal/chain"
)

const totalSupply uint64 = 100_000_000_000

type nodeConfig struct {
	NodeID      string            `json:"node_id"`
	DataDir     string            `json:"data_dir"`
	IsValidator bool              `json:"is_validator"`
	RPCListen   string            `json:"rpc_listen"`
	GenesisPath string            `json:"genesis_path"`
	Peers       []string          `json:"peers"`
	ListenAddr  string            `json:"listen_addr"`
	PrivateKey  string            `json:"private_key"`
	Validators  []string          `json:"validators"`
	PeerKeys    map[string]string `json:"peer_keys"`
}

type generatedKey struct {
	Name       string `json:"name"`
	Address    string `json:"address"`
	PublicKey  string `json:"public_key"`
	PrivateKey string `json:"private_key"`
}

type manifest struct {
	ChainID          string         `json:"chain_id"`
	TransactionID    uint64         `json:"transaction_chain_id"`
	RPCURL           string         `json:"rpc_url"`
	PublicRPCURL     string         `json:"public_rpc_url"`
	FounderWallet    generatedKey   `json:"founder_wallet"`
	ValidatorWallets []generatedKey `json:"validator_wallets"`
	Files            map[string]any `json:"files"`
	NextSteps        []string       `json:"next_steps"`
}

func main() {
	var outDir string
	var chainID string
	var txChainID uint64
	var validators int
	var publicRPCURL string
	flag.StringVar(&outDir, "out", ".synthos/open-network", "output directory for generated open-network files")
	flag.StringVar(&chainID, "chain-id", "synthos-mainnet-1", "human-readable chain ID")
	flag.Uint64Var(&txChainID, "tx-chain-id", 20260702, "numeric transaction chain ID")
	flag.IntVar(&validators, "validators", 4, "validator count")
	flag.StringVar(&publicRPCURL, "public-rpc", "https://rpc.ishamwilliamsblockchains.com", "public RPC URL")
	flag.Parse()

	if validators < 1 {
		panic("validators must be at least 1")
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		panic(err)
	}

	founder := mustKey("founder-launch-wallet")
	validatorKeys := make([]generatedKey, 0, validators)
	validatorIDs := make([]string, 0, validators)
	peerKeys := make(map[string]string, validators)
	for i := 1; i <= validators; i++ {
		name := fmt.Sprintf("validator-%d", i)
		key := mustKey(name)
		validatorKeys = append(validatorKeys, key)
		validatorIDs = append(validatorIDs, name)
		peerKeys[name] = key.PublicKey
	}

	genesis := chain.Genesis{
		ChainID:   chainID,
		TxChainID: txChainID,
		Alloc: map[chain.Address]uint64{
			chain.Address(founder.Address): totalSupply,
		},
		Metadata: map[string]any{
			"symbol":              "SYN",
			"decimals":            0,
			"network":             "SYNTHOS Collective",
			"public_rpc_url":      publicRPCURL,
			"founder_allocation":  totalSupply,
			"early_presale_note":  "Use the EVM presale contract for crypto payments; this genesis opens the SYNTHOS L1 ledger.",
			"early_presale_syn":   250_000_000,
			"early_presale_price": "0.05 USD",
		},
	}
	writeJSON(filepath.Join(outDir, "genesis.json"), genesis, 0o600)
	writeJSON(filepath.Join(outDir, "founder-wallet.private.json"), founder, 0o600)
	writeJSON(filepath.Join(outDir, "validator-wallets.private.json"), validatorKeys, 0o600)
	writeEarlyAccessEnv(filepath.Join(outDir, "early-access.env"), founder, publicRPCURL)

	for i, key := range validatorKeys {
		nodeID := key.Name
		peers := make([]string, 0, validators-1)
		for _, peerID := range validatorIDs {
			if peerID == nodeID {
				continue
			}
			peers = append(peers, fmt.Sprintf("%s@%s:9001", peerID, peerID))
		}
		cfg := nodeConfig{
			NodeID:      nodeID,
			DataDir:     "/data",
			IsValidator: true,
			RPCListen:   ":8080",
			GenesisPath: "/config/genesis.json",
			Peers:       peers,
			ListenAddr:  ":9001",
			PrivateKey:  key.PrivateKey,
			Validators:  validatorIDs,
			PeerKeys:    peerKeys,
		}
		writeJSON(filepath.Join(outDir, fmt.Sprintf("%s.json", nodeID)), cfg, 0o600)

		localCfg := cfg
		localCfg.DataDir = filepath.Join(outDir, nodeID+"-data")
		localCfg.RPCListen = fmt.Sprintf(":%d", 8080+i)
		localCfg.ListenAddr = fmt.Sprintf(":%d", 9001+i)
		localCfg.GenesisPath = filepath.Join(outDir, "genesis.json")
		localPeers := make([]string, 0, validators-1)
		for j, peerID := range validatorIDs {
			if peerID == nodeID {
				continue
			}
			localPeers = append(localPeers, fmt.Sprintf("%s@127.0.0.1:%d", peerID, 9001+j))
		}
		localCfg.Peers = localPeers
		writeJSON(filepath.Join(outDir, fmt.Sprintf("%s.local.json", nodeID)), localCfg, 0o600)
	}

	writeCompose(filepath.Join(outDir, "docker-compose.yml"), validators)
	writeReadme(filepath.Join(outDir, "README.md"), chainID, txChainID, publicRPCURL)

	m := manifest{
		ChainID:       chainID,
		TransactionID: txChainID,
		RPCURL:        "http://127.0.0.1:8080",
		PublicRPCURL:  publicRPCURL,
		FounderWallet: generatedKey{
			Name:      founder.Name,
			Address:   founder.Address,
			PublicKey: founder.PublicKey,
		},
		ValidatorWallets: publicKeysOnly(validatorKeys),
		Files: map[string]any{
			"genesis":        "genesis.json",
			"docker_compose": "docker-compose.yml",
			"founder_wallet": "founder-wallet.private.json",
			"validator_keys": "validator-wallets.private.json",
			"backend_env":    "early-access.env",
			"validator_configs": []string{
				"validator-1.json",
				"validator-2.json",
				"validator-3.json",
				"validator-4.json",
			},
		},
		NextSteps: []string{
			"Keep this folder private because validator private keys are inside it.",
			"Run docker compose up --build from the generated folder.",
			"Point rpc.ishamwilliamsblockchains.com to the host running validator-1 port 8080 through HTTPS.",
			"Point the Lovable early access page at the backend running on port 8090 through HTTPS.",
			"Verify /health and /status before inviting early adopters.",
		},
	}
	writeJSON(filepath.Join(outDir, "manifest.public.json"), m, 0o644)
	fmt.Printf("SYNTHOS open-network files generated in %s\n", outDir)
	fmt.Printf("Founder address: %s\n", founder.Address)
	fmt.Printf("Local RPC after start: http://127.0.0.1:8080\n")
}

func mustKey(name string) generatedKey {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return generatedKey{
		Name:       name,
		Address:    string(chain.AddressFromPublicKey(pub)),
		PublicKey:  "0x" + hex.EncodeToString(pub),
		PrivateKey: "0x" + hex.EncodeToString(priv),
	}
}

func publicKeysOnly(keys []generatedKey) []generatedKey {
	out := make([]generatedKey, 0, len(keys))
	for _, key := range keys {
		out = append(out, generatedKey{
			Name:      key.Name,
			Address:   key.Address,
			PublicKey: key.PublicKey,
		})
	}
	return out
}

func writeJSON(path string, value any, perm os.FileMode) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, perm); err != nil {
		panic(err)
	}
}

func writeCompose(path string, validators int) {
	content := "services:\n"
	for i := 1; i <= validators; i++ {
		name := fmt.Sprintf("validator-%d", i)
		content += fmt.Sprintf(`  %s:
    build:
      context: ../..
      dockerfile: Dockerfile
    entrypoint: ["/usr/local/bin/synthosd"]
    environment:
      SYNTHOS_CONFIG: /config/%s.json
    volumes:
      - ./:/config:ro
      - %s-data:/data
    ports:
      - "%d:8080"

`, name, name, name, 8079+i)
	}
	content += `  early-access-backend:
    build:
      context: ../..
      dockerfile: Dockerfile
    entrypoint: ["/usr/local/bin/cloudless-registry"]
    command: ["-listen", ":8090", "-state", "/data/cloudless-registry.json"]
    env_file:
      - ./early-access.env
    volumes:
      - backend-data:/data
    ports:
      - "8090:8090"
    depends_on:
      - validator-1

`
	content += "volumes:\n"
	for i := 1; i <= validators; i++ {
		content += fmt.Sprintf("  validator-%d-data:\n", i)
	}
	content += "  backend-data:\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

func writeEarlyAccessEnv(path string, founder generatedKey, publicRPCURL string) {
	assets := `[{"symbol":"ETH","network":"Ethereum","chainId":"1","rpcUrl":"https://eth.llamarpc.com","treasuryAddress":"0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2","native":true,"decimals":18,"usdPrice":"2000.00","enabled":true},{"symbol":"USDC","network":"Ethereum","chainId":"1","rpcUrl":"https://eth.llamarpc.com","address":"0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48","treasuryAddress":"0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2","decimals":6,"usdPrice":"1.00","enabled":true},{"symbol":"USDT","network":"Ethereum","chainId":"1","rpcUrl":"https://eth.llamarpc.com","address":"0xdAC17F958D2ee523a2206206994597C13D831ec7","treasuryAddress":"0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2","decimals":6,"usdPrice":"1.00","enabled":true}]`
	content := fmt.Sprintf(`SYNTHOS_NATIVE_RPC_URL=http://validator-1:8080
SYNTHOS_DISTRIBUTION_AGENT_ID=synthos-early-adopter-distributor
SYNTHOS_DISTRIBUTION_AGENT_PRIVATE_KEY=%s
SYNTHOS_EARLY_ACCESS_ALLOCATION_PRIVATE_KEY=%s
SYNTHOS_EARLY_ACCESS_TREASURY_WALLET=0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
SYNTHOS_EARLY_ACCESS_PAYMENT_TREASURY=0x5d6f8FbAAB199E788ed9Cfcb3F7Fe2ac9c0450d2
SYNTHOS_EARLY_ACCESS_CHAIN_ID=20260702
SYNTHOS_EARLY_ACCESS_TX_CHAIN_ID=20260702
SYNTHOS_EARLY_ACCESS_CHAIN_NAME=synthos-mainnet-1
SYNTHOS_EARLY_ACCESS_RPC_URLS=%s
SYNTHOS_CORS_ORIGINS=https://www.ishamwilliamsblockchains.com,https://ishamwilliamsblockchains.com,https://lovable.dev
SYNTHOS_EARLY_ACCESS_WIDGET_PATH=/website/assets/early-access-sale.js
SYNTHOS_EARLY_ACCESS_ASSETS_JSON=%s
`, founder.PrivateKey, founder.PrivateKey, publicRPCURL, assets)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

func writeReadme(path string, chainID string, txChainID uint64, publicRPCURL string) {
	content := fmt.Sprintf(`# SYNTHOS Open Network

This folder was generated locally and contains private validator keys.

Network:

- Chain ID: %s
- Transaction chain ID: %d
- Public RPC target: %s
- Local RPC after Docker start: http://127.0.0.1:8080

Start the founder-operated validator set:

`+"```bash"+`
docker compose up --build
`+"```"+`

Health checks:

`+"```bash"+`
curl http://127.0.0.1:8080/health
curl http://127.0.0.1:8080/status
`+"```"+`

To make the public RPC live, run this stack on a server, terminate HTTPS at a
reverse proxy, and point DNS for rpc.ishamwilliamsblockchains.com to it.

Do not commit this folder. It contains private keys.
`, chainID, txChainID, publicRPCURL)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}
