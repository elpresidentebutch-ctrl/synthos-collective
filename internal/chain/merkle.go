package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// EmptyTxMerkleRoot is the deterministic root for an empty transaction list.
const EmptyTxMerkleRoot = "0x0000000000000000000000000000000000000000000000000000000000000000"

// TxMerkleRoot commits to the exact ordered transaction list.
// Leaves are SHA-256(canonical JSON tx bytes). Parent nodes are
// SHA-256(left || right). An odd final node is duplicated.
func TxMerkleRoot(txs []Tx) (string, error) {
	if len(txs) == 0 {
		return EmptyTxMerkleRoot, nil
	}

	level := make([][]byte, 0, len(txs))
	for _, tx := range txs {
		data, err := json.Marshal(tx)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		leaf := make([]byte, len(sum))
		copy(leaf, sum[:])
		level = append(level, leaf)
	}

	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			buf := make([]byte, 0, len(left)+len(right))
			buf = append(buf, left...)
			buf = append(buf, right...)
			sum := sha256.Sum256(buf)
			node := make([]byte, len(sum))
			copy(node, sum[:])
			next = append(next, node)
		}
		level = next
	}

	return "0x" + hex.EncodeToString(level[0]), nil
}
