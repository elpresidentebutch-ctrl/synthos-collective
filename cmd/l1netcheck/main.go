package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/config"
	synthoscrypto "synthos-collective/internal/crypto"
)

const (
	chainID        = "synthos-l1-process-proof"
	validatorCount = 4
	genesisBalance = 1_000_000
	transferAmount = 25_000
	transferFee    = 10
)

type statusResponse struct {
	ChainID   string `json:"chain_id"`
	Height    uint64 `json:"height"`
	Tip       string `json:"tip"`
	StateRoot string `json:"state_root"`
}

type accountResponse struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type nodeSpec struct {
	ID         string
	RPCPort    int
	P2PPort    int
	RPCURL     string
	Config     string
	DataDir    string
	PublicKey  string
	PrivateKey string
	Cmd        *exec.Cmd
	LogPath    string
}

type nodeProof struct {
	ID          string `json:"id"`
	RPCURL      string `json:"rpc_url"`
	Height      uint64 `json:"height"`
	Tip         string `json:"tip"`
	StateRoot   string `json:"state_root"`
	FromBalance uint64 `json:"from_balance"`
	ToBalance   uint64 `json:"to_balance"`
	Restarted   bool   `json:"restarted"`
}

type summary struct {
	OK             bool        `json:"ok"`
	ChainID        string      `json:"chain_id"`
	Validators     int         `json:"validators"`
	SubmittedTx    string      `json:"submitted_tx"`
	FinalizedBlock string      `json:"finalized_block"`
	StateRoot      string      `json:"state_root"`
	ElapsedMillis  int64       `json:"elapsed_ms"`
	Checks         []string    `json:"checks"`
	NodeProofs     []nodeProof `json:"node_proofs"`
}

func main() {
	started := time.Now()
	if err := run(started); err != nil {
		_ = writeJSON(os.Stdout, map[string]any{
			"ok":         false,
			"error":      err.Error(),
			"elapsed_ms": time.Since(started).Milliseconds(),
		})
		os.Exit(1)
	}
}

func run(started time.Time) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "synthos-l1netcheck-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)

	synthosdPath := filepath.Join(workDir, exeName("synthosd"))
	if err := goBuild(root, synthosdPath); err != nil {
		return err
	}

	sender, err := synthoscrypto.NewKeyPair()
	if err != nil {
		return err
	}
	recipient, err := synthoscrypto.NewKeyPair()
	if err != nil {
		return err
	}
	from := chain.AddressFromPublicKey(sender.Public)
	to := chain.AddressFromPublicKey(recipient.Public)
	genesisPath := filepath.Join(workDir, "genesis.json")
	if err := writeJSONFile(genesisPath, map[string]any{
		"chain_id": chainID,
		"alloc": map[string]uint64{
			string(from): genesisBalance,
		},
		"symbol":   "SYN",
		"decimals": 0,
	}); err != nil {
		return err
	}

	nodes, err := makeNodeSpecs(workDir)
	if err != nil {
		return err
	}
	validators := make([]string, len(nodes))
	peerKeys := make(map[string]string, len(nodes))
	for i, n := range nodes {
		validators[i] = n.ID
		peerKeys[n.ID] = n.PublicKey
	}
	for i := range nodes {
		peers := make([]string, 0, len(nodes)-1)
		for _, peer := range nodes {
			if peer.ID == nodes[i].ID {
				continue
			}
			peers = append(peers, fmt.Sprintf("%s@127.0.0.1:%d", peer.ID, peer.P2PPort))
		}
		cfg := config.NodeConfig{
			NodeID:      nodes[i].ID,
			DataDir:     nodes[i].DataDir,
			IsValidator: true,
			RPCListen:   fmt.Sprintf("127.0.0.1:%d", nodes[i].RPCPort),
			GenesisPath: genesisPath,
			Peers:       peers,
			ListenAddr:  fmt.Sprintf("127.0.0.1:%d", nodes[i].P2PPort),
			PrivateKey:  nodes[i].PrivateKey,
			Validators:  validators,
			PeerKeys:    peerKeys,
		}
		if err := writeJSONFile(nodes[i].Config, cfg); err != nil {
			return err
		}
	}

	for i := range nodes {
		if err := startNode(root, synthosdPath, &nodes[i]); err != nil {
			return err
		}
		defer stopNode(&nodes[i])
	}
	for _, n := range nodes {
		if err := waitForHealth(n.RPCURL, 10*time.Second); err != nil {
			return err
		}
	}

	account, err := getAccount(nodes[0].RPCURL, string(from))
	if err != nil {
		return err
	}
	tx := chain.Tx{
		ChainID:   1,
		From:      from,
		To:        to,
		Amount:    transferAmount,
		Fee:       transferFee,
		Nonce:     account.Nonce,
		PublicKey: synthoscrypto.PublicKeyHex(sender.Public),
	}
	if err := tx.Sign(sender.Private); err != nil {
		return err
	}
	if err := postJSON(nodes[0].RPCURL+"/submitTx", tx, nil); err != nil {
		return err
	}
	if err := postJSON(nodes[0].RPCURL+"/proposeBlock", map[string]any{}, nil); err != nil {
		return err
	}

	expectedFrom := uint64(genesisBalance - transferAmount - transferFee)
	expectedTo := uint64(transferAmount)
	if err := waitForConvergence(nodes, from, to, expectedFrom, expectedTo, 15*time.Second); err != nil {
		return err
	}

	stopNode(&nodes[2])
	nodes[2].Cmd = nil
	if err := startNode(root, synthosdPath, &nodes[2]); err != nil {
		return err
	}
	if err := waitForHealth(nodes[2].RPCURL, 10*time.Second); err != nil {
		return err
	}
	if err := waitForConvergence(nodes, from, to, expectedFrom, expectedTo, 10*time.Second); err != nil {
		return err
	}

	proofs, tip, stateRoot, err := collectProofs(nodes, from, to)
	if err != nil {
		return err
	}
	for i := range proofs {
		if proofs[i].ID == nodes[2].ID {
			proofs[i].Restarted = true
		}
	}

	return writeJSON(os.Stdout, summary{
		OK:             true,
		ChainID:        chainID,
		Validators:     validatorCount,
		SubmittedTx:    tx.ID,
		FinalizedBlock: tip,
		StateRoot:      stateRoot,
		ElapsedMillis:  time.Since(started).Milliseconds(),
		Checks: []string{
			"built a synthosd executable",
			"launched four real validator processes",
			"connected validators over TCP transport",
			"submitted a signed transaction over HTTP RPC",
			"proposed and finalized a block through live consensus messages",
			"verified all nodes converge on height, tip, state root, and balances",
			"killed and restarted one validator",
			"verified restarted validator reloads finalized state from disk",
		},
		NodeProofs: proofs,
	})
}

func makeNodeSpecs(workDir string) ([]nodeSpec, error) {
	nodes := make([]nodeSpec, 0, validatorCount)
	for i := 0; i < validatorCount; i++ {
		kp, err := synthoscrypto.NewKeyPair()
		if err != nil {
			return nil, err
		}
		rpcPort, err := freePort()
		if err != nil {
			return nil, err
		}
		p2pPort, err := freePort()
		if err != nil {
			return nil, err
		}
		id := fmt.Sprintf("validator-%d", i)
		nodes = append(nodes, nodeSpec{
			ID:         id,
			RPCPort:    rpcPort,
			P2PPort:    p2pPort,
			RPCURL:     fmt.Sprintf("http://127.0.0.1:%d", rpcPort),
			Config:     filepath.Join(workDir, id+".json"),
			DataDir:    filepath.Join(workDir, id+"-data"),
			PublicKey:  synthoscrypto.PublicKeyHex(kp.Public),
			PrivateKey: "0x" + hex.EncodeToString(kp.Private),
			LogPath:    filepath.Join(workDir, id+".log"),
		})
	}
	return nodes, nil
}

func goBuild(root string, output string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-o", output, "./cmd/synthosd")
	cmd.Dir = root
	cmd.Env = goEnv(root)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build synthosd: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func startNode(root string, synthosdPath string, node *nodeSpec) error {
	logFile, err := os.Create(node.LogPath)
	if err != nil {
		return err
	}
	cmd := exec.Command(synthosdPath)
	cmd.Dir = root
	cmd.Env = append(goEnv(root), "SYNTHOS_CONFIG="+node.Config)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	node.Cmd = cmd
	return nil
}

func stopNode(node *nodeSpec) {
	if node.Cmd == nil || node.Cmd.Process == nil {
		return
	}
	_ = node.Cmd.Process.Kill()
	_, _ = node.Cmd.Process.Wait()
}

func waitForHealth(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/health")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s did not become healthy", baseURL)
}

func waitForConvergence(nodes []nodeSpec, from chain.Address, to chain.Address, expectedFrom uint64, expectedTo uint64, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		_, _, _, err := collectProofs(nodes, from, to)
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(150 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out")
	}
	return fmt.Errorf("network did not converge: %w", lastErr)
}

func collectProofs(nodes []nodeSpec, from chain.Address, to chain.Address) ([]nodeProof, string, string, error) {
	var tip string
	var stateRoot string
	proofs := make([]nodeProof, 0, len(nodes))
	for _, n := range nodes {
		status, err := getStatus(n.RPCURL)
		if err != nil {
			return nil, "", "", err
		}
		fromAccount, err := getAccount(n.RPCURL, string(from))
		if err != nil {
			return nil, "", "", err
		}
		toAccount, err := getAccount(n.RPCURL, string(to))
		if err != nil {
			return nil, "", "", err
		}
		if status.Height != 1 {
			return nil, "", "", fmt.Errorf("%s height=%d", n.ID, status.Height)
		}
		if fromAccount.Balance != genesisBalance-transferAmount-transferFee || toAccount.Balance != transferAmount {
			return nil, "", "", fmt.Errorf("%s balances from=%d to=%d", n.ID, fromAccount.Balance, toAccount.Balance)
		}
		if tip == "" {
			tip = status.Tip
			stateRoot = status.StateRoot
		}
		if status.Tip != tip || status.StateRoot != stateRoot {
			return nil, "", "", fmt.Errorf("%s diverged tip/root", n.ID)
		}
		proofs = append(proofs, nodeProof{
			ID:          n.ID,
			RPCURL:      n.RPCURL,
			Height:      status.Height,
			Tip:         status.Tip,
			StateRoot:   status.StateRoot,
			FromBalance: fromAccount.Balance,
			ToBalance:   toAccount.Balance,
		})
	}
	return proofs, tip, stateRoot, nil
}

func getStatus(baseURL string) (statusResponse, error) {
	var out statusResponse
	err := getJSON(baseURL+"/status", &out)
	return out, err
}

func getAccount(baseURL string, address string) (accountResponse, error) {
	var out accountResponse
	err := getJSON(baseURL+"/account?address="+address, &out)
	return out, err
}

func getJSON(url string, out any) error {
	client := http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s status=%d body=%s", url, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func postJSON(url string, value any, out any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("POST %s status=%d body=%s", url, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if out != nil {
		return json.Unmarshal(payload, out)
	}
	return nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func writeJSONFile(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeJSON(f, value)
}

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func goEnv(root string) []string {
	env := os.Environ()
	hasCache := false
	for _, item := range env {
		if strings.HasPrefix(item, "GOCACHE=") {
			hasCache = true
			break
		}
	}
	if !hasCache {
		env = append(env, "GOCACHE="+filepath.Join(root, ".gocache"))
	}
	return env
}

func exeName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
