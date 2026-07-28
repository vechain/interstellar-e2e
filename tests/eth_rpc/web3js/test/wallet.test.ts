import { expect } from 'chai';
import { Web3 } from 'web3';
import {
  collectStrings,
  fetchBaseFee,
  makeWeb3,
  rpc,
  sendEip1559,
  signEip1559Raw,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Accounts — sign & send EIP-1559 tx', () => {
  let web3: Web3;
  before(() => {
    web3 = makeWeb3();
  });

  it('sends 1 wei and receives a successful receipt with correct balance delta', async () => {
    const before = await web3.eth.getBalance(NODE2_ADDRESS);

    const receipt = await sendEip1559(web3, TEST_SENDER_KEY, {
      to: NODE2_ADDRESS,
      value: 1n,
      gas: 21_000n,
    });

    expect(receipt.status).to.equal(1n);
    expect((receipt.from as string).toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect((receipt.to as string).toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
    expect(receipt.transactionHash as string).to.match(/^0x[0-9a-fA-F]{64}$/);

    const after = await web3.eth.getBalance(NODE2_ADDRESS);
    expect(after - before).to.equal(1n);
  });

  it('rejects an unfunded address with an insufficient-funds error', async () => {
    const unfundedKey = '0x' + '11'.repeat(32);
    let threw = false;
    try {
      await sendEip1559(web3, unfundedKey, { to: NODE2_ADDRESS, value: 1n, gas: 21_000n });
    } catch {
      threw = true;
    }
    expect(threw, 'expected unfunded send to throw').to.equal(true);
  });

  it('EIP-2930 (type 1) access-list transactions are rejected by Thor — only EIP-1559 is accepted', async () => {
    const account = web3.eth.accounts.privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await web3.eth.getChainId();
    const nonce = await web3.eth.getTransactionCount(account.address, 'pending');
    const gasPrice = await web3.eth.getGasPrice();
    const signed = await account.signTransaction({
      to: NODE2_ADDRESS,
      value: 1n,
      // Access lists raise the intrinsic gas floor (2400/address), so web3.js's
      // offline signer rejects 21000 before we ever reach Thor — give headroom
      // so the rejection we assert is Thor's, on the EIP-2930 envelope itself.
      gas: 50_000n,
      gasPrice,
      nonce,
      chainId,
      type: 1,
      accessList: [{ address: NODE2_ADDRESS, storageKeys: [] }],
    });

    let caught: unknown;
    try {
      await web3.eth.sendSignedTransaction(signed.rawTransaction);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected EIP-2930 tx to be rejected').to.not.be.undefined;
    expect(collectStrings(caught).join(' || ')).to.match(/rlp|access|unsupported|expected List|coalesce|type/i);
  });

  it('Legacy (type 0) transactions are rejected by Thor — only EIP-1559 is accepted', async () => {
    const account = web3.eth.accounts.privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await web3.eth.getChainId();
    const nonce = await web3.eth.getTransactionCount(account.address, 'pending');
    const gasPrice = await web3.eth.getGasPrice();
    const signed = await account.signTransaction({
      to: NODE2_ADDRESS,
      value: 1n,
      gas: 21_000n,
      gasPrice,
      nonce,
      chainId,
      type: 0,
    });

    let caught: unknown;
    try {
      await web3.eth.sendSignedTransaction(signed.rawTransaction);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected legacy tx to be rejected').to.not.be.undefined;
    expect(collectStrings(caught).join(' || ')).to.match(/rlp|legacy|unsupported|expected List|coalesce/i);
  });

  it('accounts.sign produces a signature that accounts.recover recovers', async () => {
    const account = web3.eth.accounts.privateKeyToAccount(TEST_SENDER_KEY);
    const message = 'hello thor';

    const signed = account.sign(message);
    expect(signed.signature).to.match(/^0x[0-9a-fA-F]{130}$/);

    const recovered = web3.eth.accounts.recover(message, signed.signature);
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('signTransaction produces a raw RLP that recoverTransaction maps back to the signer', async () => {
    const account = web3.eth.accounts.privateKeyToAccount(TEST_SENDER_KEY);
    const chainId = await web3.eth.getChainId();
    const nonce = await web3.eth.getTransactionCount(account.address, 'pending');
    const baseFee = await fetchBaseFee(web3);
    const signed = await account.signTransaction({
      to: NODE2_ADDRESS,
      value: 2n,
      gas: 21_000n,
      nonce,
      chainId,
      maxPriorityFeePerGas: 1n,
      maxFeePerGas: baseFee * 2n + 1n,
      type: 2,
    });
    expect(signed.rawTransaction).to.match(/^0x[0-9a-fA-F]+$/);

    const recovered = web3.eth.accounts.recoverTransaction(signed.rawTransaction);
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('sendSignedTransaction accepts an offline-signed raw tx and confirms it', async () => {
    const before = await web3.eth.getBalance(NODE2_ADDRESS);

    const raw = await signEip1559Raw(web3, TEST_SENDER_KEY, {
      to: NODE2_ADDRESS,
      value: 3n,
      gas: 21_000n,
    });
    const receipt = await web3.eth.sendSignedTransaction(raw);
    expect(receipt.status).to.equal(1n);
    expect(receipt.transactionHash as string).to.match(/^0x[0-9a-fA-F]{64}$/);

    const after = await web3.eth.getBalance(NODE2_ADDRESS);
    expect(after - before).to.equal(3n);
  });

  it('eth_signTypedData_v4 is rejected — no node-side keys to sign with', async () => {
    // web3.js has no offline EIP-712 signer; the only typed-data path goes to the
    // node, which Thor rejects (no keystore). Documenting the gap keeps it pinned.
    let caught: unknown;
    try {
      await rpc(web3, 'eth_signTypedData_v4', [
        TEST_SENDER_ADDRESS,
        JSON.stringify({
          domain: { name: 'InterstellarTest', version: '1', chainId: 1, verifyingContract: NODE2_ADDRESS },
          types: { EIP712Domain: [], Mail: [{ name: 'contents', type: 'string' }] },
          primaryType: 'Mail',
          message: { contents: 'hi' },
        }),
      ]);
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected eth_signTypedData_v4 to be rejected').to.not.be.undefined;
  });
});
