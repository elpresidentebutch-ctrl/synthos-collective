package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

type accountResponse struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type submitResponse struct {
	OK   bool   `json:"ok"`
	TxID string `json:"tx_id"`
}

func main() {
	rpcURL := flag.String("rpc", "", "base RPC URL, e.g. http://127.0.0.1:8080")
	privHex := flag.String("priv", "", "sender ed25519 private key hex")
	addresses := flag.String("addresses", "", "comma-separated validator addresses")
	addressesFile := flag.String("addresses-file", "", "path to a newline-delimited address file")
	amount := flag.Uint64("amount", 0, "amount to send to each validator")
	fee := flag.Uint64("fee", 0, "fee per transaction")
	startNonce := flag.Uint64("start-nonce", 0, "starting nonce if /account is unavailable")
	useStartNonce := flag.Bool("use-start-nonce", false, "force use of --start-nonce instead of querying /account")
	dryRun := flag.Bool("dry-run", false, "build transactions without submitting them")
	proposeBlock := flag.Bool("propose-block", false, "call /proposeBlock after submitting transactions")
	timeout := flag.Duration("timeout", 10*time.Second, "HTTP timeout")
	flag.Parse()

	if *rpcURL == "" || *privHex == "" || *amount == 0 {
		fmt.Fprintln(os.Stderr, "usage: fundvalidators --rpc http://127.0.0.1:8080 --priv $SYNTHOS_FUNDER_PRIVATE_KEY --amount N [--addresses-file validators.txt]")
		os.Exit(2)
	}

	recipients, err := loadRecipients(*addresses, *addressesFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	client := &http.Client{Timeout: *timeout}
	baseURL := strings.TrimRight(*rpcURL, "/")

	if err := checkHealth(client, baseURL); err != nil {
		fmt.Fprintf(os.Stderr, "health check failed: %v\n", err)
		os.Exit(1)
	}

	w, err := wallet.FromPrivateKeyHex(*privHex)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid private key: %v\n", err)
		os.Exit(1)
	}

	fromAddr, err := w.Address()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to derive sender address: %v\n", err)
		os.Exit(1)
	}
	pubHex, err := w.PublicKeyHex()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to derive sender public key: %v\n", err)
		os.Exit(1)
	}

	senderAccount, accountErr := getAccount(client, baseURL, string(fromAddr))
	if accountErr != nil && !*useStartNonce {
		fmt.Fprintf(os.Stderr, "warning: /account unavailable, falling back to --start-nonce (%v)\n", accountErr)
		*useStartNonce = true
	}

	nonce := *startNonce
	if !*useStartNonce {
		nonce = senderAccount.Nonce
	}

	availableBalance := uint64(0)
	if accountErr == nil {
		availableBalance = senderAccount.Balance
		required := uint64(len(recipients)) * (*amount + *fee)
		if availableBalance < required {
			fmt.Fprintf(os.Stderr, "insufficient balance: have=%d need=%d\n", availableBalance, required)
			os.Exit(1)
		}
	}

	fmt.Printf("sender=%s recipients=%d amount_each=%d fee_each=%d start_nonce=%d dry_run=%v\n", fromAddr, len(recipients), *amount, *fee, nonce, *dryRun)
	if availableBalance > 0 {
		fmt.Printf("sender_balance=%d\n", availableBalance)
	}

	for index, recipient := range recipients {
		tx := chain.Tx{
			From:      fromAddr,
			To:        chain.Address(recipient),
			Amount:    *amount,
			Fee:       *fee,
			Nonce:     nonce + uint64(index),
			PublicKey: pubHex,
		}
		if err := tx.Sign(w.Private); err != nil {
			fmt.Fprintf(os.Stderr, "failed to sign tx for %s: %v\n", recipient, err)
			os.Exit(1)
		}

		if *dryRun {
			fmt.Printf("dry-run recipient=%s nonce=%d tx_id=%s\n", recipient, tx.Nonce, tx.ID)
			continue
		}

		result, err := submitTx(client, baseURL, tx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "submit failed recipient=%s nonce=%d err=%v\n", recipient, tx.Nonce, err)
			os.Exit(1)
		}

		fmt.Printf("submitted recipient=%s nonce=%d tx_id=%s ok=%v\n", recipient, tx.Nonce, result.TxID, result.OK)
	}

	if *proposeBlock && !*dryRun {
		if err := postNoBody(client, baseURL+"/proposeBlock"); err != nil {
			fmt.Fprintf(os.Stderr, "warning: proposeBlock failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("proposeBlock requested")
	}
}

func loadRecipients(csv string, filePath string) ([]string, error) {
	unique := make(map[string]struct{})
	ordered := make([]string, 0)
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := unique[value]; exists {
			return
		}
		unique[value] = struct{}{}
		ordered = append(ordered, value)
	}

	for _, part := range strings.Split(csv, ",") {
		add(part)
	}

	if filePath != "" {
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(string(content), "\n") {
			add(line)
		}
	}

	if len(ordered) == 0 {
		return nil, errors.New("no validator addresses provided")
	}

	return ordered, nil
}

func checkHealth(client *http.Client, baseURL string) error {
	resp, err := client.Get(baseURL + "/health")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func getAccount(client *http.Client, baseURL string, address string) (accountResponse, error) {
	resp, err := client.Get(baseURL + "/account?address=" + address)
	if err != nil {
		return accountResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return accountResponse{}, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var account accountResponse
	if err := json.NewDecoder(resp.Body).Decode(&account); err != nil {
		return accountResponse{}, err
	}
	return account, nil
}

func submitTx(client *http.Client, baseURL string, tx chain.Tx) (submitResponse, error) {
	body, err := json.Marshal(tx)
	if err != nil {
		return submitResponse{}, err
	}
	resp, err := client.Post(baseURL+"/submitTx", "application/json", bytes.NewReader(body))
	if err != nil {
		return submitResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		return submitResponse{}, fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var result submitResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return submitResponse{}, err
	}
	return result, nil
}

func postNoBody(client *http.Client, url string) error {
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	return nil
}
