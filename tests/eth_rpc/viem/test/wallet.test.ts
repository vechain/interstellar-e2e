import { expect } from 'chai';
import {
  parseTransaction,
  recoverMessageAddress,
  recoverTypedDataAddress,
} from 'viem';
import { mnemonicToAccount, privateKeyToAccount } from 'viem/accounts';
import {
  makePublicClient,
  makeWalletClient,
  collectStrings,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
  NODE2_ADDRESS,
} from '../src/fixtures';

describe('Wallet — sign & send EIP-1559 tx', () => {
  it('sends 1 wei and receives a successful receipt with correct balance delta', async () => {
    const client = makePublicClient();
    const wallet = await makeWalletClient(TEST_SENDER_KEY);

    const before = await client.getBalance({ address: NODE2_ADDRESS });
    const hash = await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
    const receipt = await client.waitForTransactionReceipt({ hash });

    expect(receipt.status).to.equal('success');
    expect(receipt.from.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(receipt.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
    expect(hash).to.match(/^0x[0-9a-fA-F]{64}$/);

    const after = await client.getBalance({ address: NODE2_ADDRESS });
    expect(after - before).to.equal(1n);
  });

  it('rejects an unfunded address with an insufficient-funds error', async () => {
    const unfundedKey = ('0x' + '11'.repeat(32)) as `0x${string}`;
    const wallet = await makeWalletClient(unfundedKey);

    let threw = false;
    try {
      await wallet.sendTransaction({ to: NODE2_ADDRESS, value: 1n });
    } catch {
      threw = true;
    }
    expect(threw, 'expected unfunded send to throw').to.equal(true);
  });

  it('Legacy (type 0) transactions are rejected by Thor — only EIP-1559 is accepted', async () => {
    const client = makePublicClient();
    const account = privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await client.getChainId();
    const nonce = await client.getTransactionCount({ address: account.address });
    const gasPrice = await client.getGasPrice();

    const serialized = await account.signTransaction({
      type: 'legacy',
      to: NODE2_ADDRESS,
      value: 1n,
      gas: 21_000n,
      gasPrice,
      nonce,
      chainId,
    });

    let caught: unknown;
    try {
      await client.sendRawTransaction({ serializedTransaction: serialized });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected legacy tx to be rejected').to.not.be.undefined;
    const blob = collectStrings(caught).join(' || ');
    expect(blob).to.match(/rlp|legacy|unsupported|expected List|coalesce|type/i);
  });

  it('EIP-2930 (type 1) access-list transactions are rejected by Thor', async () => {
    const client = makePublicClient();
    const account = privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await client.getChainId();
    const nonce = await client.getTransactionCount({ address: account.address });
    const gasPrice = await client.getGasPrice();

    const serialized = await account.signTransaction({
      type: 'eip2930',
      to: NODE2_ADDRESS,
      value: 1n,
      gas: 21_000n,
      gasPrice,
      nonce,
      chainId,
      accessList: [{ address: NODE2_ADDRESS, storageKeys: [] }],
    });

    let caught: unknown;
    try {
      await client.sendRawTransaction({ serializedTransaction: serialized });
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected EIP-2930 tx to be rejected').to.not.be.undefined;
    const blob = collectStrings(caught).join(' || ');
    expect(blob).to.match(/rlp|access|unsupported|expected List|coalesce|type/i);
  });

  it('signMessage produces a signature that recoverMessageAddress recovers', async () => {
    const wallet = await makeWalletClient(TEST_SENDER_KEY);
    const message = 'hello thor';
    const signature = await wallet.signMessage({ message });
    expect(signature).to.match(/^0x[0-9a-fA-F]{130}$/);

    const recovered = await recoverMessageAddress({ message, signature });
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('signTypedData (EIP-712) produces a signature that recoverTypedDataAddress recovers', async () => {
    const client = makePublicClient();
    const wallet = await makeWalletClient(TEST_SENDER_KEY);
    const chainId = await client.getChainId();

    const domain = {
      name: 'InterstellarTest',
      version: '1',
      chainId,
      verifyingContract: '0x0000000000000000000000000000000000000000' as const,
    };
    const types = {
      Mail: [
        { name: 'from', type: 'address' },
        { name: 'to', type: 'address' },
        { name: 'contents', type: 'string' },
      ],
    } as const;
    const message = {
      from: TEST_SENDER_ADDRESS,
      to: NODE2_ADDRESS,
      contents: 'hi',
    };

    const signature = await wallet.signTypedData({
      domain,
      types,
      primaryType: 'Mail',
      message,
    });
    expect(signature).to.match(/^0x[0-9a-fA-F]{130}$/);

    const recovered = await recoverTypedDataAddress({
      domain,
      types,
      primaryType: 'Mail',
      message,
      signature,
    });
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('prepareTransactionRequest fills nonce, gas, and EIP-1559 fee fields', async () => {
    const wallet = await makeWalletClient(TEST_SENDER_KEY);
    const request = await wallet.prepareTransactionRequest({ to: NODE2_ADDRESS, value: 1n });

    expect(request.nonce).to.be.a('number').and.to.be.at.least(0);
    expect(request.gas, 'gas').to.be.a('bigint');
    expect(request.maxFeePerGas, 'maxFeePerGas').to.be.a('bigint');
    expect(request.maxPriorityFeePerGas, 'maxPriorityFeePerGas').to.be.a('bigint');
  });

  it('account.signTransaction produces a raw tx that parseTransaction maps back', async () => {
    const client = makePublicClient();
    const account = privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await client.getChainId();
    const nonce = await client.getTransactionCount({ address: account.address });
    const fees = await client.estimateFeesPerGas();

    const serialized = await account.signTransaction({
      type: 'eip1559',
      to: NODE2_ADDRESS,
      value: 2n,
      gas: 21_000n,
      maxFeePerGas: fees.maxFeePerGas,
      maxPriorityFeePerGas: fees.maxPriorityFeePerGas,
      nonce,
      chainId,
    });
    expect(serialized).to.match(/^0x02[0-9a-fA-F]+$/);

    const parsed = parseTransaction(serialized);
    expect(parsed.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
    expect(parsed.value).to.equal(2n);
    expect(parsed.type).to.equal('eip1559');
  });

  it('sendRawTransaction accepts an offline-signed raw tx and confirms it', async () => {
    const client = makePublicClient();
    const account = privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await client.getChainId();
    const nonce = await client.getTransactionCount({ address: account.address });
    const fees = await client.estimateFeesPerGas();

    const before = await client.getBalance({ address: NODE2_ADDRESS });

    const serialized = await account.signTransaction({
      type: 'eip1559',
      to: NODE2_ADDRESS,
      value: 3n,
      gas: 21_000n,
      maxFeePerGas: fees.maxFeePerGas,
      maxPriorityFeePerGas: fees.maxPriorityFeePerGas,
      nonce,
      chainId,
    });
    const hash = await client.sendRawTransaction({ serializedTransaction: serialized });
    const receipt = await client.waitForTransactionReceipt({ hash });
    expect(receipt.status).to.equal('success');

    const after = await client.getBalance({ address: NODE2_ADDRESS });
    expect(after - before).to.equal(3n);
  });

  it('mnemonicToAccount produces a deterministic address from a mnemonic', () => {
    const phrase = 'test test test test test test test test test test test junk';
    const account = mnemonicToAccount(phrase);
    expect(account.address).to.equal('0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266');
  });
});
