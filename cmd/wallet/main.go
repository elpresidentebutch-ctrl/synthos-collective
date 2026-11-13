package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"

	"synthos-collective/internal/chain"
	"synthos-collective/internal/wallet"
)

const defaultRPC = "https://82-29-54-7.sslip.io"
const defaultTxChainID = uint64(20260702)

func printUsage() {
	fmt.Println(`SYNTHOS Collective — Sovereign L1 Wallet Utility

Usage:
  synthos-wallet new [flags]              Generate a new BIP-39 mnemonic wallet / keystore
  synthos-wallet recover [flags]          Restore a wallet from a BIP-39 recovery seed phrase
  synthos-wallet inspect [flags] <source> Inspect a keystore JSON or private key
  synthos-wallet balance [flags] <addr>   Query account balance and nonce from RPC
  synthos-wallet send [flags]             Sign and broadcast a native coin transfer

Flags for 'new':
  -words int        Seed phrase word count: 12 (standard), 15, or 24 (high-sec) (default: 12)
  -out string       Save encrypted keystore to file path
  -password string  Passphrase for keystore encryption (prompts securely if omitted)
  -passphrase string BIP-39 optional seed passphrase (default: "")
  -show-private     Output unencrypted private key to stdout (DANGEROUS)

Flags for 'recover':
  -mnemonic string  BIP-39 mnemonic recovery phrase (prompts if omitted)
  -passphrase string BIP-39 optional seed passphrase (default: "")
  -out string       Save encrypted keystore to file path
  -password string  Passphrase for keystore encryption
  -show-private     Output unencrypted private key to stdout (DANGEROUS)

Flags for 'inspect':
  -password string  Passphrase to decrypt keystore file (if inspecting a keystore)

Flags for 'balance':
  -rpc string       RPC endpoint URL (default: https://82-29-54-7.sslip.io)

Flags for 'send':
  -to string        Recipient SYNTHOS address (required)
  -amount uint      Amount of SYN coins to send (required)
  -fee uint         Transaction fee in SYN (default: the network's required fee,
                    0.1% of the amount, at least 10 and at most 1,000)
  -key string       Path to keystore JSON file or raw private key hex (required)
  -password string  Passphrase for keystore decryption
  -rpc string       RPC endpoint URL (default: https://82-29-54-7.sslip.io)
  -nonce int        Account nonce override (-1 to auto-fetch from RPC)
  -yes              Send without asking for confirmation

Passing -password, -mnemonic or a raw private key on the command line saves
it in your shell history. Leave them out and the wallet asks for them instead.`)
}

func readPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	passBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(passBytes)), nil
}

func readLine(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// warnSecretFlag tells the user when a secret was passed on the command line,
// where shell history and process listings can keep it.
func warnSecretFlag(name string) {
	fmt.Fprintf(os.Stderr, "warning: %s was passed on the command line and may be saved in your shell history; leave it out to be prompted instead\n", name)
}

// validAddress reports whether s is a SYNTHOS address: 0x followed by 40 hex characters.
func validAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, c := range s[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// transferFee returns the fee to use for a native transfer: the network's
// required fee (chain.RequiredFee), or the user's fee if that is higher.
// A user fee below the requirement is an error, because the chain rejects it.
func transferFee(amount, userFee uint64) (uint64, error) {
	required := chain.RequiredFee(chain.Tx{Amount: amount})
	if userFee == 0 {
		return required, nil
	}
	if userFee < required {
		return 0, fmt.Errorf("fee %d SYN is below the network's required fee of %d SYN for this amount", userFee, required)
	}
	return userFee, nil
}

func printMnemonicBox(mnemonic string) {
	words := strings.Fields(mnemonic)
	fmt.Println()
	fmt.Println("==========================================================================")
	fmt.Printf("⚠️  SECRET RECOVERY PHRASE (%d WORDS - BIP-39)\n", len(words))
	fmt.Println("Write down these words in order. Never share them with anyone.")
	fmt.Println("==========================================================================")
	cols := 3
	if len(words) >= 24 {
		cols = 4
	}
	rows := (len(words) + cols - 1) / cols
	for r := 0; r < rows; r++ {
		line := ""
		for c := 0; c < cols; c++ {
			idx := r + c*rows
			if idx < len(words) {
				line += fmt.Sprintf(" %2d. %-14s", idx+1, words[idx])
			}
		}
		fmt.Println(line)
	}
	fmt.Println("==========================================================================")
	fmt.Println()
}

func main() {
	if len(os.Args) < 2 {
		runNew(nil)
		return
	}

	cmd := os.Args[1]

	// Handle flags passed directly without subcommand
	if strings.HasPrefix(cmd, "-") {
		runNew(os.Args[1:])
		return
	}

	switch cmd {
	case "new":
		runNew(os.Args[2:])
	case "recover":
		runRecover(os.Args[2:])
	case "inspect":
		runInspect(os.Args[2:])
	case "balance":
		runBalance(os.Args[2:])
	case "send":
		runSend(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func runNew(args []string) {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	wordCount := fs.Int("words", 12, "Seed phrase word count: 12, 15, or 24")
	outFile := fs.String("out", "", "Save encrypted keystore to file")
	password := fs.String("password", "", "Passphrase for keystore encryption")
	passphrase := fs.String("passphrase", "", "Optional BIP-39 seed passphrase")
	showPrivate := fs.Bool("show-private", false, "Print unencrypted private key hex (DANGEROUS)")
	_ = fs.Parse(args)
	if *password != "" {
		warnSecretFlag("-password")
	}

	mnemonic, err := wallet.NewMnemonic(*wordCount)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating mnemonic: %v\n", err)
		os.Exit(1)
	}

	w, err := wallet.FromMnemonic(mnemonic, *passphrase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error deriving wallet: %v\n", err)
		os.Exit(1)
	}

	addr, _ := w.Address()
	pub, _ := w.PublicKeyHex()
	fp, _ := w.Fingerprint()

	printMnemonicBox(mnemonic)

	fmt.Printf("   Address:     %s\n", addr)
	fmt.Printf("   Public Key:  %s\n", pub)
	fmt.Printf("   Fingerprint: %s\n", fp)

	if *outFile != "" {
		pass := *password
		if pass == "" {
			var err error
			pass, err = readPassword(fmt.Sprintf("\nEnter passphrase to encrypt keystore (%s): ", *outFile))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading passphrase: %v\n", err)
				os.Exit(1)
			}
			confirm, _ := readPassword("Confirm passphrase: ")
			if pass != confirm {
				fmt.Fprintln(os.Stderr, "error: passphrases do not match")
				os.Exit(1)
			}
		}
		if err := w.SaveKeystore(*outFile, pass); err != nil {
			fmt.Fprintf(os.Stderr, "error saving keystore: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n✅ Keystore encrypted and saved to %s\n", *outFile)
	}

	if *showPrivate {
		priv, _ := w.PrivateKeyHex()
		seed, _ := w.SeedHex()
		fmt.Printf("   Seed (hex):  %s\n", seed)
		fmt.Printf("   Private Key: %s\n", priv)
	}
}

func runRecover(args []string) {
	fs := flag.NewFlagSet("recover", flag.ExitOnError)
	mnemonicArg := fs.String("mnemonic", "", "BIP-39 mnemonic recovery phrase")
	passphrase := fs.String("passphrase", "", "Optional BIP-39 seed passphrase")
	outFile := fs.String("out", "", "Save encrypted keystore to file")
	password := fs.String("password", "", "Passphrase for keystore encryption")
	showPrivate := fs.Bool("show-private", false, "Print unencrypted private key hex (DANGEROUS)")
	_ = fs.Parse(args)
	if *password != "" {
		warnSecretFlag("-password")
	}
	if *mnemonicArg != "" {
		warnSecretFlag("-mnemonic")
	}

	mnemonic := *mnemonicArg
	if mnemonic == "" {
		var err error
		mnemonic, err = readLine("Enter BIP-39 recovery seed phrase (12, 15, or 24 words): ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading mnemonic: %v\n", err)
			os.Exit(1)
		}
	}

	w, err := wallet.FromMnemonic(mnemonic, *passphrase)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to recover wallet: %v\n", err)
		os.Exit(1)
	}

	addr, _ := w.Address()
	pub, _ := w.PublicKeyHex()
	fp, _ := w.Fingerprint()

	fmt.Println("\n✅ Wallet Successfully Restored from Recovery Phrase")
	fmt.Printf("   Address:     %s\n", addr)
	fmt.Printf("   Public Key:  %s\n", pub)
	fmt.Printf("   Fingerprint: %s\n", fp)

	if *outFile != "" {
		pass := *password
		if pass == "" {
			var err error
			pass, err = readPassword(fmt.Sprintf("\nEnter passphrase to encrypt keystore (%s): ", *outFile))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading passphrase: %v\n", err)
				os.Exit(1)
			}
			confirm, _ := readPassword("Confirm passphrase: ")
			if pass != confirm {
				fmt.Fprintln(os.Stderr, "error: passphrases do not match")
				os.Exit(1)
			}
		}
		if err := w.SaveKeystore(*outFile, pass); err != nil {
			fmt.Fprintf(os.Stderr, "error saving keystore: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n✅ Keystore encrypted and saved to %s\n", *outFile)
	}

	if *showPrivate {
		priv, _ := w.PrivateKeyHex()
		seed, _ := w.SeedHex()
		fmt.Printf("   Seed (hex):  %s\n", seed)
		fmt.Printf("   Private Key: %s\n", priv)
	}
}

func runInspect(args []string) {
	fs := flag.NewFlagSet("inspect", flag.ExitOnError)
	password := fs.String("password", "", "Passphrase for keystore decryption")
	_ = fs.Parse(args)
	if *password != "" {
		warnSecretFlag("-password")
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: specify keystore file path or private key hex")
		os.Exit(1)
	}

	source := fs.Arg(0)
	var w *wallet.Wallet
	var err error

	// If source is a file on disk
	if _, statErr := os.Stat(source); statErr == nil {
		pass := *password
		if pass == "" {
			pass, err = readPassword(fmt.Sprintf("Enter passphrase for %s: ", source))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading passphrase: %v\n", err)
				os.Exit(1)
			}
		}
		w, err = wallet.LoadKeystore(source, pass)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to decrypt keystore: %v\n", err)
			os.Exit(1)
		}
	} else {
		w, err = wallet.FromPrivateKeyHex(source)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid private key: %v\n", err)
			os.Exit(1)
		}
	}

	addr, _ := w.Address()
	pub, _ := w.PublicKeyHex()
	fp, _ := w.Fingerprint()

	fmt.Printf("Address:     %s\n", addr)
	fmt.Printf("Public Key:  %s\n", pub)
	fmt.Printf("Fingerprint: %s\n", fp)
}

func runBalance(args []string) {
	fs := flag.NewFlagSet("balance", flag.ExitOnError)
	rpcURL := fs.String("rpc", defaultRPC, "RPC endpoint URL")
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "error: specify account address (0x...)")
		os.Exit(1)
	}

	addrStr := fs.Arg(0)
	if !validAddress(addrStr) {
		fmt.Fprintf(os.Stderr, "error: %q is not a valid 20-byte SYNTHOS address\n", addrStr)
		os.Exit(1)
	}

	client := wallet.NewClient(*rpcURL)
	ctx := context.Background()

	acc, err := client.GetAccount(ctx, chain.Address(addrStr))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch account: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Address: %s\n", acc.Address)
	fmt.Printf("Balance: %d SYN\n", acc.Balance)
	fmt.Printf("Nonce:   %d\n", acc.Nonce)
}

func runSend(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	to := fs.String("to", "", "Recipient address (0x...)")
	amount := fs.Uint64("amount", 0, "Amount of SYN coins")
	fee := fs.Uint64("fee", 0, "Transaction fee in SYN (0 = the network's required fee)")
	keySource := fs.String("key", "", "Path to keystore JSON file or raw private key hex")
	password := fs.String("password", "", "Passphrase for keystore decryption")
	rpcURL := fs.String("rpc", defaultRPC, "RPC endpoint URL")
	nonceOverride := fs.Int64("nonce", -1, "Account nonce override (-1 to auto-fetch)")
	yes := fs.Bool("yes", false, "Send without asking for confirmation")
	_ = fs.Parse(args)

	if *to == "" || *amount == 0 || *keySource == "" {
		fmt.Fprintln(os.Stderr, "error: -to, -amount, and -key are required")
		fs.Usage()
		os.Exit(1)
	}
	if !validAddress(*to) {
		fmt.Fprintf(os.Stderr, "error: %q is not a valid SYNTHOS address (0x followed by 40 hex characters)\n", *to)
		os.Exit(1)
	}
	txFee, err := transferFee(*amount, *fee)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if *password != "" {
		warnSecretFlag("-password")
	}

	var w *wallet.Wallet

	if _, statErr := os.Stat(*keySource); statErr == nil {
		pass := *password
		if pass == "" {
			pass, err = readPassword(fmt.Sprintf("Enter passphrase for %s: ", *keySource))
			if err != nil {
				fmt.Fprintf(os.Stderr, "error reading passphrase: %v\n", err)
				os.Exit(1)
			}
		}
		w, err = wallet.LoadKeystore(*keySource, pass)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to decrypt keystore: %v\n", err)
			os.Exit(1)
		}
	} else {
		warnSecretFlag("a raw private key (-key)")
		w, err = wallet.FromPrivateKeyHex(*keySource)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid private key: %v\n", err)
			os.Exit(1)
		}
	}

	fromAddr, _ := w.Address()
	client := wallet.NewClient(*rpcURL)
	ctx := context.Background()

	// Query network status
	status, err := client.GetStatus(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not verify network status: %v\n", err)
	}
	txChainID := defaultTxChainID
	if status != nil && status.TxChainID > 0 {
		txChainID = status.TxChainID
	}

	// Resolve nonce
	var nonce uint64
	if *nonceOverride >= 0 {
		nonce = uint64(*nonceOverride)
	} else {
		acc, err := client.GetAccount(ctx, fromAddr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to fetch account nonce: %v\n", err)
			os.Exit(1)
		}
		nonce = acc.Nonce
	}

	// Create and sign transaction
	tx, err := w.CreateTransferTx(chain.Address(*to), *amount, txFee, nonce, txChainID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to construct transaction: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Transaction ready:")
	fmt.Printf("  From:   %s\n", tx.From)
	fmt.Printf("  To:     %s\n", tx.To)
	fmt.Printf("  Amount: %d SYN\n", tx.Amount)
	fmt.Printf("  Fee:    %d SYN\n", tx.Fee)
	fmt.Printf("  Nonce:  %d\n", tx.Nonce)
	fmt.Printf("  TxID:   %s\n", tx.ID)

	if !*yes {
		answer, err := readLine(fmt.Sprintf("\nSend %d SYN (plus %d SYN fee) to %s? Type yes to confirm: ", tx.Amount, tx.Fee, tx.To))
		if err != nil || strings.ToLower(answer) != "yes" {
			fmt.Fprintln(os.Stderr, "Cancelled. Nothing was sent.")
			os.Exit(1)
		}
	}

	res, err := client.BroadcastTx(ctx, tx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "broadcast failed: %v\n", err)
		os.Exit(1)
	}

	if !res.OK {
		fmt.Fprintf(os.Stderr, "transaction rejected by node: %s\n", res.Error)
		os.Exit(1)
	}

	fmt.Println("\n✅ Transaction successfully submitted to SYNTHOS L1 mempool!")
	fmt.Printf("   Tx ID:   %s\n", res.TxID)
	if res.TxHash != "" {
		fmt.Printf("   Tx Hash: %s\n", res.TxHash)
	}
}
