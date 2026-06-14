const fs = require("fs");
const path = require("path");
const solc = require("solc");

const root = process.cwd();
const srcDir = path.join(root, "src");
const artifactsDir = path.join(root, "artifacts");

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) return walk(full);
    return entry.isFile() && entry.name.endsWith(".sol") ? [full] : [];
  });
}

function sourceName(file) {
  return path.relative(root, file).replaceAll(path.sep, "/");
}

const files = walk(srcDir);
const input = {
  language: "Solidity",
  sources: Object.fromEntries(
    files.map((file) => [sourceName(file), { content: fs.readFileSync(file, "utf8") }])
  ),
  settings: {
    viaIR: true,
    optimizer: { enabled: true, runs: 200 },
    outputSelection: {
      "*": {
        "*": [
          "abi",
          "evm.bytecode.object",
          "evm.bytecode.linkReferences",
          "evm.deployedBytecode.object",
          "evm.deployedBytecode.linkReferences",
        ],
      },
    },
  },
};

function findImports(importPath) {
  const candidates = [
    path.join(root, importPath),
    path.join(root, "node_modules", importPath),
    path.join(root, "src", importPath),
    path.join(root, "src/synthos", importPath),
    path.join(root, "src/test", importPath),
  ];
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) {
      return { contents: fs.readFileSync(candidate, "utf8") };
    }
  }
  return { error: `Import not found: ${importPath}` };
}

const output = JSON.parse(solc.compile(JSON.stringify(input), { import: findImports }));
for (const item of output.errors || []) {
  console.log(`${item.severity}: ${item.formattedMessage.split("\n")[0]}`);
}
if ((output.errors || []).some((item) => item.severity === "error")) {
  process.exit(1);
}

fs.rmSync(artifactsDir, { recursive: true, force: true });

for (const [source, contracts] of Object.entries(output.contracts || {})) {
  for (const [contractName, compiled] of Object.entries(contracts)) {
    const artifact = {
      _format: "hh-sol-artifact-1",
      contractName,
      sourceName: source,
      abi: compiled.abi,
      bytecode: `0x${compiled.evm.bytecode.object}`,
      deployedBytecode: `0x${compiled.evm.deployedBytecode.object}`,
      linkReferences: compiled.evm.bytecode.linkReferences || {},
      deployedLinkReferences: compiled.evm.deployedBytecode.linkReferences || {},
    };
    const outDir = path.join(artifactsDir, source);
    fs.mkdirSync(outDir, { recursive: true });
    fs.writeFileSync(
      path.join(outDir, `${contractName}.json`),
      JSON.stringify(artifact, null, 2)
    );
  }
}

console.log(`Wrote Hardhat artifacts for ${files.length} Solidity files`);
