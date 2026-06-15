/* One-shot compile for every contracts/*.sol → contracts/<Name>.json.
 * Run via `npm run compile:contracts`. The resulting JSON files are checked in,
 * so the test suite has no solc dependency at runtime. */
const fs = require('node:fs');
const path = require('node:path');
const solc = require('solc');

const root = path.join(__dirname, '..');
const contractsDir = path.join(root, 'contracts');

const sources = {};
for (const entry of fs.readdirSync(contractsDir)) {
  if (entry.endsWith('.sol')) {
    sources[entry] = { content: fs.readFileSync(path.join(contractsDir, entry), 'utf8') };
  }
}

const input = {
  language: 'Solidity',
  sources,
  settings: {
    optimizer: { enabled: true, runs: 200 },
    evmVersion: 'paris',
    outputSelection: {
      '*': { '*': ['abi', 'evm.bytecode.object'] },
    },
  },
};

const output = JSON.parse(solc.compile(JSON.stringify(input)));

if (output.errors) {
  const fatal = output.errors.filter((e) => e.severity === 'error');
  for (const e of output.errors) console.error(e.formattedMessage);
  if (fatal.length > 0) process.exit(1);
}

for (const [sourceFile, byName] of Object.entries(output.contracts)) {
  for (const [contractName, contract] of Object.entries(byName)) {
    const artifact = {
      contractName,
      abi: contract.abi,
      bytecode: '0x' + contract.evm.bytecode.object,
    };
    const outputPath = path.join(contractsDir, `${contractName}.json`);
    fs.writeFileSync(outputPath, JSON.stringify(artifact, null, 2) + '\n');
    console.log(`wrote ${outputPath} (${artifact.bytecode.length / 2 - 1} bytes from ${sourceFile})`);
  }
}
