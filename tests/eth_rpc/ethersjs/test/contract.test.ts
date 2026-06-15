import { expect } from 'chai';
import {
  Contract,
  ContractFactory,
  Interface,
  JsonRpcProvider,
  Wallet,
  getCreate2Address,
  id,
  keccak256,
  randomBytes,
  hexlify,
} from 'ethers';
import {
  loadCreate2FactoryArtifact,
  loadStorageArtifact,
  makeProvider,
  makeWallet,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Contract — deploy & call via ethers.Contract', () => {
  const artifact = loadStorageArtifact();
  let provider: JsonRpcProvider;
  let wallet: Wallet;
  let contract: Contract;
  let address: string;

  before(async () => {
    provider = makeProvider();
    wallet = makeWallet(TEST_SENDER_KEY, provider);
    const factory = new ContractFactory(artifact.abi, artifact.bytecode, wallet);
    const deployed = await factory.deploy();
    await deployed.waitForDeployment();
    address = await deployed.getAddress();
    contract = new Contract(address, artifact.abi, wallet);
  });

  it('deploys to a non-empty contract address', async () => {
    expect(address).to.match(/^0x[0-9a-fA-F]{40}$/);
    const code = await provider.getCode(address);
    expect(code.length).to.be.greaterThan(2);
  });

  it('initial value() and get() both return 0n', async () => {
    expect(await contract.value()).to.equal(0n);
    expect(await contract.get()).to.equal(0n);
  });

  it('set(42) persists the value and the receipt status is 1', async () => {
    const tx = await contract.set(42n);
    const receipt = await tx.wait();
    expect(receipt.status).to.equal(1);
    expect(await contract.get()).to.equal(42n);
    expect(await contract.value()).to.equal(42n);
  });

  it('setStrict(0) reverts with the declared reason', async () => {
    let caught: unknown;
    try {
      const tx = await contract.setStrict(0n);
      await tx.wait();
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected setStrict(0) to revert').to.not.be.undefined;
    const msg = String((caught as { shortMessage?: string; message: string }).shortMessage
      ?? (caught as Error).message);
    expect(msg).to.match(/value must be non-zero|reverted/i);
  });

  it('setStrictCustomError(0) reverts with a decoded custom error', async () => {
    // staticCall surfaces revert data directly from eth_call, which is the
    // pathway ethers' decoder is most reliable on.
    let caught: unknown;
    try {
      await contract.setStrictCustomError.staticCall(0n);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected custom-error revert').to.not.be.undefined;
    const expectedSelector = id('MustBeNonZero(uint256)').slice(0, 10);

    // Scan every string-typed field in the error object (and one level of
    // nested object) for either the decoded name or the raw selector.
    const haystack: string[] = [];
    const walk = (obj: unknown, depth: number) => {
      if (depth > 3 || obj == null) return;
      if (typeof obj === 'string') haystack.push(obj);
      else if (typeof obj === 'object') {
        for (const v of Object.values(obj as Record<string, unknown>)) walk(v, depth + 1);
      }
    };
    walk(caught, 0);

    const found = haystack.some(
      (s) => s.includes('MustBeNonZero') || s.includes(expectedSelector.slice(2)),
    );
    expect(found, `no MustBeNonZero / ${expectedSelector} in error`).to.equal(true);
  });

  it('staticCall on a write function returns the call data without sending a tx', async () => {
    const nonceBefore = await provider.getTransactionCount(wallet.address);
    // get() is view, but staticCall on a writer just simulates without changing state.
    await contract.set.staticCall(999n);
    const nonceAfter = await provider.getTransactionCount(wallet.address);
    expect(nonceAfter).to.equal(nonceBefore);
    // And state should not have changed:
    expect(await contract.get()).to.equal(42n);
  });

  it('Interface.parseLog decodes a Set event from a raw receipt log', async () => {
    const tx = await contract.set(99n);
    const receipt = await tx.wait();
    expect(receipt.logs.length).to.be.greaterThan(0);

    const iface = new Interface(artifact.abi);
    const parsed = iface.parseLog({
      topics: receipt.logs[0].topics as string[],
      data: receipt.logs[0].data,
    });
    expect(parsed, 'parseLog result').to.not.be.null;
    expect(parsed!.name).to.equal('Set');
    expect(parsed!.args.value).to.equal(99n);
  });

  it('contract.<method>.estimateGas returns a positive bigint via the method-level API', async () => {
    // contract.set.estimateGas(...) wraps Interface.encodeFunctionData +
    // provider.estimateGas — a distinct code path from the top-level
    // provider.estimateGas covered elsewhere. Asserting a positive bigint
    // result confirms ethers correctly threads the contract address, sender,
    // and ABI-encoded calldata through Thor's eth_estimateGas.
    const gas = await contract.set.estimateGas(7n);
    expect(gas).to.be.a('bigint');
    expect(gas > 0n, `gas was ${gas}`).to.equal(true);
  });

  it('contract.<method>.populateTransaction returns a fully-populated TransactionRequest', async () => {
    // Method-level populateTransaction goes through the contract's runner +
    // Interface, separate from wallet.populateTransaction in wallet.test.ts.
    // We assert the populated tx has `to`, `data` (the ABI-encoded set(99n)
    // call), and the fee fields ethers fills in for EIP-1559.
    const populated = await contract.set.populateTransaction(99n);
    expect(populated.to, 'populated.to').to.exist;
    expect(populated.to!.toLowerCase()).to.equal(address.toLowerCase());
    expect(populated.data, 'populated.data').to.match(/^0x[0-9a-fA-F]+$/);
    // ABI selector for set(uint256) — first 4 bytes of keccak256("set(uint256)")
    expect(populated.data!.startsWith('0x60fe47b1'), 'selector mismatch').to.equal(true);
    // The encoded uint256 argument 99 must be the last 32 bytes of calldata
    // (selector 4B + 32B arg = 36B = 72 hex chars after 0x).
    expect(populated.data!.length).to.equal(2 + 4 * 2 + 32 * 2);
    expect(populated.data!.toLowerCase()).to.match(/0+63$/, 'uint256 99 encoding');
  });

  it('contract.attach returns a fresh handle bound to the same address', async () => {
    const reattached = contract.attach(address) as Contract;
    expect(await reattached.value()).to.equal(99n);
  });

  it('CREATE2 parity — getCreate2Address(deployer, salt, hash(initCode)) matches the on-chain deployed address', async function () {
    this.timeout(60_000);
    // Deploy Create2Factory once, then ask it to CREATE2-deploy the Storage
    // contract. The address Thor actually places the new contract at must match
    // ethers' client-side `getCreate2Address` prediction, byte-for-byte, or any
    // dApp relying on counterfactual addresses (Safe, account abstraction) will
    // silently break against this node.
    const factoryArtifact = loadCreate2FactoryArtifact();
    const factoryDeploy = await new ContractFactory(
      factoryArtifact.abi,
      factoryArtifact.bytecode,
      wallet,
    ).deploy();
    await factoryDeploy.waitForDeployment();
    const factoryAddress = await factoryDeploy.getAddress();
    const factory = new Contract(factoryAddress, factoryArtifact.abi, wallet);

    const salt = hexlify(randomBytes(32));
    const initCode = artifact.bytecode; // reuse Storage's deploy bytecode
    const expected = getCreate2Address(factoryAddress, salt, keccak256(initCode));

    // Set gasLimit explicitly: ethers' estimateGas undercounts CREATE2 + the
    // inner Storage constructor in Thor's compat layer, leaving the inner
    // create2 OOG and returning addr=0x0 from the opcode (which trips the
    // factory's require). 3M is well above the actual ~250k consumed.
    const tx = await factory.deploy(salt, initCode, { gasLimit: 3_000_000n });
    const receipt = await tx.wait();
    expect(receipt.status).to.equal(1);

    // Locate the Deployed(address) event and decode its argument.
    const iface = new Interface(factoryArtifact.abi);
    const parsed = receipt.logs
      .map((l: { topics: readonly string[]; data: string }) =>
        iface.parseLog({ topics: l.topics as string[], data: l.data }),
      )
      .find((p: { name: string } | null) => p?.name === 'Deployed');
    expect(parsed, 'Deployed event').to.not.be.null;
    const onChainAddr = parsed!.args.addr as string;

    expect(onChainAddr.toLowerCase()).to.equal(expected.toLowerCase());
    // And the contract really lives there — getCode must be non-empty.
    const code = await provider.getCode(onChainAddr);
    expect(code.length).to.be.greaterThan(2);
  });

  it('payable tip() accepts value, increments contract balance, emits Tipped', async () => {
    const balanceBefore = await provider.getBalance(address);

    const tx = await contract.tip({ value: 1234n });
    const receipt = await tx.wait();
    expect(receipt.status).to.equal(1);

    const balanceAfter = await provider.getBalance(address);
    expect(balanceAfter - balanceBefore).to.equal(1234n);

    const totalTipped = await contract.totalTipped();
    expect(totalTipped).to.equal(balanceAfter);

    // Tipped(address indexed who, uint256 amount) — locate in receipt logs
    const iface = new Interface(artifact.abi);
    const parsed = receipt.logs
      .map((l: { topics: readonly string[]; data: string }) =>
        iface.parseLog({ topics: l.topics as string[], data: l.data }),
      )
      .find((p: { name: string } | null) => p?.name === 'Tipped');
    expect(parsed, 'Tipped event').to.not.be.undefined;
    expect(parsed!.args.who.toLowerCase()).to.equal(wallet.address.toLowerCase());
    expect(parsed!.args.amount).to.equal(1234n);
  });
});
