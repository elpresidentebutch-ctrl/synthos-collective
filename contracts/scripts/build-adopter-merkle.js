const fs = require("fs");
const path = require("path");
const { ethers } = require("ethers");

const ZERO_HASH = `0x${"0".repeat(64)}`;
const abiCoder = ethers.AbiCoder.defaultAbiCoder();

function usage() {
  console.log("Usage: node scripts/build-adopter-merkle.js [input.json] [output.json]");
  console.log("Default input:  merkle/adopters.json");
  console.log("Default output: merkle/adopter-merkle.json");
}

function asBytes32(value, field) {
  if (!/^0x[a-fA-F0-9]{64}$/.test(value || "")) {
    throw new Error(`${field} must be a bytes32 hex string`);
  }
  return ethers.hexlify(value).toLowerCase();
}

function normalizeRecord(record, index) {
  const operator = ethers.getAddress(record.operator);
  const hardwareCommitment = asBytes32(record.hardwareCommitment || record.hardware_commitment, `adopters[${index}].hardwareCommitment`);
  const nodeType = String(record.nodeType || record.node_type || "DESKTOP");
  if (!nodeType) throw new Error(`adopters[${index}].nodeType is required`);
  return { operator, hardwareCommitment, nodeType };
}

function adopterLeaf(record) {
  const nodeTypeHash = ethers.keccak256(ethers.toUtf8Bytes(record.nodeType));
  const inner = ethers.keccak256(
    abiCoder.encode(
      ["address", "bytes32", "bytes32"],
      [record.operator, record.hardwareCommitment, nodeTypeHash]
    )
  );
  return ethers.keccak256(ethers.concat([inner]));
}

function hashPair(a, b) {
  const left = a.toLowerCase() < b.toLowerCase() ? a : b;
  const right = left === a ? b : a;
  return ethers.keccak256(ethers.concat([left, right]));
}

function buildTree(records) {
  if (records.length === 0) {
    return { root: ZERO_HASH, entries: [] };
  }

  const entries = records
    .map((record, index) => ({
      ...record,
      inputIndex: index,
      leaf: adopterLeaf(record),
      proof: [],
    }))
    .sort((a, b) => a.leaf.localeCompare(b.leaf));

  const leafSeen = new Set();
  for (const entry of entries) {
    if (leafSeen.has(entry.leaf)) {
      throw new Error(`duplicate adopter Merkle leaf: ${entry.leaf}`);
    }
    leafSeen.add(entry.leaf);
  }

  let level = entries.map((entry, index) => ({
    hash: entry.leaf,
    entryIndexes: [index],
  }));

  while (level.length > 1) {
    const next = [];
    for (let i = 0; i < level.length; i += 2) {
      const left = level[i];
      const right = level[i + 1];
      if (!right) {
        next.push(left);
        continue;
      }

      for (const index of left.entryIndexes) entries[index].proof.push(right.hash);
      for (const index of right.entryIndexes) entries[index].proof.push(left.hash);

      next.push({
        hash: hashPair(left.hash, right.hash),
        entryIndexes: [...left.entryIndexes, ...right.entryIndexes],
      });
    }
    level = next;
  }

  return { root: level[0].hash, entries };
}

function verifyProof(leaf, proof, root) {
  const computed = proof.reduce((hash, sibling) => hashPair(hash, sibling), leaf);
  return computed.toLowerCase() === root.toLowerCase();
}

function readInput(file) {
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  const adopters = Array.isArray(parsed) ? parsed : parsed.adopters;
  if (!Array.isArray(adopters)) {
    throw new Error("input must be an array or an object with an adopters array");
  }
  return adopters.map(normalizeRecord);
}

function main() {
  const root = process.cwd();
  const inputFile = path.resolve(root, process.argv[2] || "merkle/adopters.json");
  const outputFile = path.resolve(root, process.argv[3] || "merkle/adopter-merkle.json");

  if (!fs.existsSync(inputFile)) {
    usage();
    throw new Error(`input file not found: ${inputFile}`);
  }

  const records = readInput(inputFile);
  const tree = buildTree(records);

  for (const entry of tree.entries) {
    if (!verifyProof(entry.leaf, entry.proof, tree.root)) {
      throw new Error(`proof self-check failed for ${entry.operator}`);
    }
  }

  const output = {
    format: "synthos-adopter-merkle-v1",
    generatedAt: new Date().toISOString(),
    inputFile: path.relative(root, inputFile).replaceAll(path.sep, "/"),
    merkleRoot: tree.root,
    count: tree.entries.length,
    leafEncoding: "keccak256(bytes.concat(keccak256(abi.encode(operator, hardwareCommitment, keccak256(bytes(nodeType))))))",
    pairHashing: "OpenZeppelin MerkleProof sorted-pair keccak256(bytes32,bytes32)",
    adopters: tree.entries
      .sort((a, b) => a.inputIndex - b.inputIndex)
      .map((entry) => ({
        operator: entry.operator,
        hardwareCommitment: entry.hardwareCommitment,
        nodeType: entry.nodeType,
        leaf: entry.leaf,
        proof: entry.proof,
      })),
  };

  fs.mkdirSync(path.dirname(outputFile), { recursive: true });
  fs.writeFileSync(outputFile, `${JSON.stringify(output, null, 2)}\n`);
  console.log(`ADOPTER_MERKLE_ROOT=${tree.root}`);
  console.log(`Wrote ${tree.entries.length} adopter proofs to ${path.relative(root, outputFile)}`);
}

main();
