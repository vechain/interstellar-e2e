import { expect } from 'chai';
import { Web3 } from 'web3';
import type { Contract } from 'web3-eth-contract';
import {
  collectStrings,
  loadCreate2FactoryArtifact,
  loadStorageArtifact,
  makeWeb3,
  sendEip1559,
  StorageArtifact,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Contract — deploy & call via web3.eth.Contract', () => {
  const artifact = loadStorageArtifact();
  let web3: Web3;
  let contract: Contract<StorageArtifact['abi']>;
  let address: string;

  // Deploy `artifact` and return the new contract address (via the deterministic
  // EIP-1559 raw-tx path; web3.js wallet auto-signing is avoided everywhere).
  async function deploy(a: StorageArtifact): Promise<string> {
    const deployTx = new web3.eth.Contract(a.abi).deploy({ data: a.bytecode });
    const data = deployTx.encodeABI();
    const gas = await deployTx.estimateGas({ from: TEST_SENDER_ADDRESS });
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, { data, gas });
    expect(receipt.status, 'deploy receipt status').to.equal(1n);
    return receipt.contractAddress as string;
  }

  before(async () => {
    web3 = makeWeb3();
    address = await deploy(artifact);
    contract = new web3.eth.Contract(artifact.abi, address);
  });

  it('deploys to a non-empty contract address', async () => {
    expect(address).to.match(/^0x[0-9a-fA-F]{40}$/);
    const code = await web3.eth.getCode(address);
    expect(code.length).to.be.greaterThan(2);
  });

  it('initial value() and get() both return 0n', async () => {
    expect(await contract.methods.value().call()).to.equal(0n);
    expect(await contract.methods.get().call()).to.equal(0n);
  });

  it('set(42) persists the value and the receipt status is 1', async () => {
    const method = contract.methods.set(42n);
    const gas = await method.estimateGas({ from: TEST_SENDER_ADDRESS });
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: address,
      data: method.encodeABI(),
      gas,
    });
    expect(receipt.status).to.equal(1n);
    expect(await contract.methods.get().call()).to.equal(42n);
    expect(await contract.methods.value().call()).to.equal(42n);
  });

  it('setStrict(0) reverts with the declared reason (via eth_call)', async () => {
    let caught: unknown;
    try {
      await contract.methods.setStrict(0n).call({ from: TEST_SENDER_ADDRESS });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected setStrict(0) to revert').to.not.be.undefined;
    expect(collectStrings(caught).join(' || ')).to.match(/value must be non-zero|revert/i);
  });

  it('setStrictCustomError(0) reverts with a decoded custom error (via eth_call)', async () => {
    let caught: unknown;
    try {
      await contract.methods.setStrictCustomError(0n).call({ from: TEST_SENDER_ADDRESS });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected custom-error revert').to.not.be.undefined;
    const selector = web3.utils.keccak256('MustBeNonZero(uint256)').slice(0, 10);
    const haystack = collectStrings(caught).join(' || ');
    const found = haystack.includes('MustBeNonZero') || haystack.toLowerCase().includes(selector.slice(2).toLowerCase());
    expect(found, `no MustBeNonZero / ${selector} in error: ${haystack.slice(0, 200)}`).to.equal(true);
  });

  it('eth_call on a write function returns without sending a tx (no nonce change)', async () => {
    const nonceBefore = await web3.eth.getTransactionCount(TEST_SENDER_ADDRESS);
    await contract.methods.set(999n).call({ from: TEST_SENDER_ADDRESS });
    const nonceAfter = await web3.eth.getTransactionCount(TEST_SENDER_ADDRESS);
    expect(nonceAfter).to.equal(nonceBefore);
    // State unchanged — still 42 from the earlier set.
    expect(await contract.methods.get().call()).to.equal(42n);
  });

  it('decodeLog decodes a Set event from a raw receipt log', async () => {
    const method = contract.methods.set(99n);
    const gas = await method.estimateGas({ from: TEST_SENDER_ADDRESS });
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: address,
      data: method.encodeABI(),
      gas,
    });
    const logs = receipt.logs ?? [];
    expect(logs.length).to.be.greaterThan(0);

    const setSig = web3.utils.keccak256('Set(address,uint256)');
    const setLog = logs.find((l) => (l.topics ?? [])[0] === setSig);
    expect(setLog, 'Set log').to.not.be.undefined;
    const decoded = web3.eth.abi.decodeLog(
      [
        { indexed: true, name: 'who', type: 'address' },
        { indexed: false, name: 'value', type: 'uint256' },
      ],
      setLog!.data as string,
      (setLog!.topics as string[]).slice(1),
    );
    expect(decoded.value).to.equal(99n);
    expect((decoded.who as string).toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('contract.<method>.estimateGas returns a positive bigint via the method-level API', async () => {
    const gas = await contract.methods.set(7n).estimateGas({ from: TEST_SENDER_ADDRESS });
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('contract.<method>.encodeABI returns ABI-encoded calldata with the right selector', async () => {
    const data = contract.methods.set(99n).encodeABI();
    expect(data).to.match(/^0x[0-9a-fA-F]+$/);
    // selector for set(uint256) = first 4 bytes of keccak256("set(uint256)")
    expect(data.startsWith('0x60fe47b1'), 'selector mismatch').to.equal(true);
    // selector (4B) + uint256 arg (32B) = 36B => 72 hex chars after 0x
    expect(data.length).to.equal(2 + 4 * 2 + 32 * 2);
    expect(data.toLowerCase()).to.match(/0+63$/, 'uint256 99 encoding');
  });

  it('a fresh Contract handle bound to the same address reads current state', async () => {
    const reattached = new web3.eth.Contract(artifact.abi, address);
    expect(await reattached.methods.value().call()).to.equal(99n);
  });

  it('CREATE2 parity — locally computed address matches the on-chain deployed address', async function () {
    this.timeout(90_000);
    const factoryArtifact = loadCreate2FactoryArtifact();
    const factoryAddress = await deploy(factoryArtifact);
    const factory = new web3.eth.Contract(factoryArtifact.abi, factoryAddress);

    const salt = web3.utils.randomHex(32);
    const initCode = artifact.bytecode; // reuse Storage's deploy bytecode
    const initCodeHash = web3.utils.keccak256(initCode);
    const expected = create2Address(web3, factoryAddress, salt, initCodeHash);

    // Set gas explicitly: estimateGas undercounts CREATE2 + the inner Storage
    // constructor in Thor's compat layer, leaving the inner create2 OOG.
    const method = factory.methods.deploy(salt, initCode);
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: factoryAddress,
      data: method.encodeABI(),
      gas: 3_000_000n,
    });
    expect(receipt.status).to.equal(1n);

    const deployedSig = web3.utils.keccak256('Deployed(address)');
    const log = (receipt.logs ?? []).find((l) => (l.topics ?? [])[0] === deployedSig);
    expect(log, 'Deployed event').to.not.be.undefined;
    const onChainAddr = web3.eth.abi.decodeParameter('address', log!.data as string) as string;

    expect(onChainAddr.toLowerCase()).to.equal(expected.toLowerCase());
    const code = await web3.eth.getCode(onChainAddr);
    expect(code.length).to.be.greaterThan(2);
  });

  it('payable tip() accepts value, increments contract balance, emits Tipped', async () => {
    const balanceBefore = await web3.eth.getBalance(address);

    const method = contract.methods.tip();
    const gas = await method.estimateGas({ from: TEST_SENDER_ADDRESS, value: 1234n });
    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: address,
      data: method.encodeABI(),
      value: 1234n,
      gas,
    });
    expect(receipt.status).to.equal(1n);

    const balanceAfter = await web3.eth.getBalance(address);
    expect(balanceAfter - balanceBefore).to.equal(1234n);
    expect(await contract.methods.totalTipped().call()).to.equal(balanceAfter);

    const tippedSig = web3.utils.keccak256('Tipped(address,uint256)');
    const log = (receipt.logs ?? []).find((l) => (l.topics ?? [])[0] === tippedSig);
    expect(log, 'Tipped event').to.not.be.undefined;
    const who = web3.eth.abi.decodeParameter('address', (log!.topics as string[])[1]) as string;
    const amount = web3.eth.abi.decodeParameter('uint256', log!.data as string) as bigint;
    expect(who.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(amount).to.equal(1234n);
  });
});

// CREATE2: address = keccak256(0xff ++ deployer ++ salt ++ keccak256(initCode))[12:]
function create2Address(web3: Web3, deployer: string, salt: string, initCodeHash: string): string {
  const payload = '0xff' + strip0x(deployer) + strip0x(salt) + strip0x(initCodeHash);
  const hash = web3.utils.keccak256(payload);
  return web3.utils.toChecksumAddress('0x' + hash.slice(-40));
}

function strip0x(hex: string): string {
  return hex.startsWith('0x') ? hex.slice(2) : hex;
}
