import { expect } from 'chai';
import {
  encodeFunctionData,
  getContract,
  getContractAddress,
  keccak256,
  parseEventLogs,
  toHex,
  type Address,
} from 'viem';
import {
  loadCreate2FactoryArtifact,
  loadStorageArtifact,
  makePublicClient,
  makeWalletClient,
  collectStrings,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Contract — deploy & call via viem', () => {
  const artifact = loadStorageArtifact();
  let client: ReturnType<typeof makePublicClient>;
  let wallet: Awaited<ReturnType<typeof makeWalletClient>>;
  let address: Address;

  before(async () => {
    client = makePublicClient();
    wallet = await makeWalletClient(TEST_SENDER_KEY);
    const hash = await wallet.deployContract({ abi: artifact.abi, bytecode: artifact.bytecode });
    const receipt = await client.waitForTransactionReceipt({ hash });
    address = receipt.contractAddress!;
  });

  it('deploys to a non-empty contract address', async () => {
    expect(address).to.match(/^0x[0-9a-fA-F]{40}$/);
    const code = await client.getCode({ address });
    expect(code, 'deployed code').to.match(/^0x[0-9a-fA-F]+$/);
    expect(code!.length).to.be.greaterThan(2);
  });

  it('initial value() and get() both return 0n', async () => {
    expect(await client.readContract({ address, abi: artifact.abi, functionName: 'value' })).to.equal(0n);
    expect(await client.readContract({ address, abi: artifact.abi, functionName: 'get' })).to.equal(0n);
  });

  it('set(42) persists the value and the receipt status is success', async () => {
    const hash = await wallet.writeContract({
      address,
      abi: artifact.abi,
      functionName: 'set',
      args: [42n],
    });
    const receipt = await client.waitForTransactionReceipt({ hash });
    expect(receipt.status).to.equal('success');
    expect(await client.readContract({ address, abi: artifact.abi, functionName: 'get' })).to.equal(42n);
  });

  it('setStrict(0) reverts with the declared reason (via simulateContract)', async () => {
    let caught: unknown;
    try {
      await client.simulateContract({
        address,
        abi: artifact.abi,
        functionName: 'setStrict',
        args: [0n],
        account: wallet.account,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected setStrict(0) to revert').to.not.be.undefined;
    const blob = collectStrings(caught).join(' || ');
    expect(blob).to.match(/value must be non-zero|reverted/i);
  });

  it('setStrictCustomError(0) reverts with a decoded custom error', async () => {
    let caught: unknown;
    try {
      await client.simulateContract({
        address,
        abi: artifact.abi,
        functionName: 'setStrictCustomError',
        args: [0n],
        account: wallet.account,
      });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected custom-error revert').to.not.be.undefined;
    const blob = collectStrings(caught).join(' || ');
    expect(blob).to.match(/MustBeNonZero/);
  });

  it('simulateContract on a write returns without sending (no nonce change)', async () => {
    const nonceBefore = await client.getTransactionCount({ address: wallet.account.address });
    await client.simulateContract({
      address,
      abi: artifact.abi,
      functionName: 'set',
      args: [999n],
      account: wallet.account,
    });
    const nonceAfter = await client.getTransactionCount({ address: wallet.account.address });
    expect(nonceAfter).to.equal(nonceBefore);
    expect(await client.readContract({ address, abi: artifact.abi, functionName: 'get' })).to.equal(42n);
  });

  it('parseEventLogs decodes a Set event from a raw receipt', async () => {
    const hash = await wallet.writeContract({
      address,
      abi: artifact.abi,
      functionName: 'set',
      args: [99n],
    });
    const receipt = await client.waitForTransactionReceipt({ hash });
    const events = parseEventLogs({ abi: artifact.abi, logs: receipt.logs });
    const set = events.find((e) => e.eventName === 'Set');
    expect(set, 'Set event').to.exist;
    expect((set!.args as { value: bigint }).value).to.equal(99n);
  });

  it('estimateContractGas returns a positive bigint via the method-level API', async () => {
    const gas = await client.estimateContractGas({
      address,
      abi: artifact.abi,
      functionName: 'set',
      args: [7n],
      account: wallet.account,
    });
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('encodeFunctionData returns ABI-encoded calldata with the right selector', () => {
    const data = encodeFunctionData({ abi: artifact.abi, functionName: 'set', args: [99n] });
    // selector for set(uint256) = first 4 bytes of keccak256("set(uint256)")
    expect(data.startsWith('0x60fe47b1'), 'selector mismatch').to.equal(true);
    expect(data.length).to.equal(2 + 4 * 2 + 32 * 2);
  });

  it('getContract gives a bound handle that reads current state', async () => {
    const contract = getContract({ address, abi: artifact.abi, client });
    expect(await contract.read.value()).to.equal(99n);
  });

  it('CREATE2 parity — getContractAddress matches the on-chain deployed address', async function () {
    this.timeout(60_000);
    const factoryArtifact = loadCreate2FactoryArtifact();
    const factoryHash = await wallet.deployContract({
      abi: factoryArtifact.abi,
      bytecode: factoryArtifact.bytecode,
    });
    const factoryReceipt = await client.waitForTransactionReceipt({ hash: factoryHash });
    const factoryAddress = factoryReceipt.contractAddress!;

    const salt = keccak256(toHex(`viem-create2-${factoryAddress}`));
    const initCode = artifact.bytecode;
    const expected = getContractAddress({
      opcode: 'CREATE2',
      from: factoryAddress,
      salt,
      bytecode: initCode,
    });

    // Thor's compat estimateGas undercounts CREATE2 + inner constructor; set an
    // explicit generous gas like the ethersjs test does.
    const deployHash = await wallet.writeContract({
      address: factoryAddress,
      abi: factoryArtifact.abi,
      functionName: 'deploy',
      args: [salt, initCode],
      gas: 3_000_000n,
    });
    const receipt = await client.waitForTransactionReceipt({ hash: deployHash });
    expect(receipt.status).to.equal('success');

    const events = parseEventLogs({ abi: factoryArtifact.abi, logs: receipt.logs });
    const deployed = events.find((e) => e.eventName === 'Deployed');
    expect(deployed, 'Deployed event').to.exist;
    const onChainAddr = (deployed!.args as { addr: string }).addr;

    expect(onChainAddr.toLowerCase()).to.equal(expected.toLowerCase());
    const code = await client.getCode({ address: onChainAddr as Address });
    expect(code!.length).to.be.greaterThan(2);
  });

  it('payable tip() accepts value, increments contract balance, emits Tipped', async () => {
    const balanceBefore = await client.getBalance({ address });
    const hash = await wallet.writeContract({
      address,
      abi: artifact.abi,
      functionName: 'tip',
      value: 1234n,
    });
    const receipt = await client.waitForTransactionReceipt({ hash });
    expect(receipt.status).to.equal('success');

    const balanceAfter = await client.getBalance({ address });
    expect(balanceAfter - balanceBefore).to.equal(1234n);

    const events = parseEventLogs({ abi: artifact.abi, logs: receipt.logs });
    const tipped = events.find((e) => e.eventName === 'Tipped');
    expect(tipped, 'Tipped event').to.exist;
    expect((tipped!.args as { amount: bigint }).amount).to.equal(1234n);
  });
});
