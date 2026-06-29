const crypto = require('crypto');
const fs = require('fs');

const { publicKey, privateKey } = crypto.generateKeyPairSync('ed25519');

const pubHex = "0x" + publicKey.export({ type: 'spki', format: 'der' }).toString('hex').slice(-64);
const privHex = "0x" + privateKey.export({ type: 'pkcs8', format: 'der' }).toString('hex').slice(-64);

// Address derivation in Synthos seems to be based on public key.
// Looking at internal/chain/address.go or similar.
// In cmd/node/main.go: addr := chain.AddressFromPublicKey(pub)

// Let's just output the keys and I'll derive the address if I can see the code.
const output = `Public Key: ${pubHex}\nPrivate Key: ${privHex}\n`;
fs.writeFileSync('scratch/USER_WALLET_NODE.local.txt', output, { mode: 0o600 });
console.log(`Public Key: ${pubHex}`);
console.log('Private Key: <written to scratch/USER_WALLET_NODE.local.txt>');
