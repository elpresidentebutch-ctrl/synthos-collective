package devnode

import "github.com/cometbft/cometbft/crypto/ed25519"

func ed25519PubKey(b []byte) ed25519.PubKey { return ed25519.PubKey(b) }
