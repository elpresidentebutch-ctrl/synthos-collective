const fs = require('fs');
const path = require('path');
const { langEn } = require('../contracts/node_modules/@ethersproject/wordlists/lib/lang-en.js');

const words = [];
for (let i = 0; i < 2048; i++) {
  words.push(langEn.getWord(i));
}

const outPath = path.resolve(__dirname, '../internal/wallet/wordlist.go');
const content = `package wallet

// BIP-39 English Wordlist (2048 words)
var englishWordlist = []string{
\t"${words.join('",\n\t"')}",
}

var wordMap map[string]int

func init() {
\twordMap = make(map[string]int, len(englishWordlist))
\tfor i, w := range englishWordlist {
\t\twordMap[w] = i
\t}
}
`;

fs.writeFileSync(outPath, content);
console.log(`Wrote ${words.length} words to ${outPath}`);
