import { expect } from 'chai';
import {
  HDNodeWallet,
  Mnemonic,
  Transaction,
  verifyMessage,
  verifyTypedData,
} from 'ethers';
import {
  makeProvider,
  makeWallet,
  NODE2_ADDRESS,
  TEST_SENDER_ADDRESS,
  TEST_SENDER_KEY,
} from '../src/fixtures';

describe('Wallet — sign & send EIP-1559 tx', () => {
  it('sends 1 wei and receives a successful receipt with correct balance delta', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    const before = await provider.getBalance(NODE2_ADDRESS);

    const tx = await wallet.sendTransaction({
      to: NODE2_ADDRESS,
      value: 1n,
      type: 2,
    });
    const receipt = await tx.wait();

    expect(receipt, 'receipt').to.not.be.null;
    expect(receipt!.status).to.equal(1);
    expect(receipt!.from.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
    expect(receipt!.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
    expect(receipt!.hash).to.match(/^0x[0-9a-fA-F]{64}$/);

    const after = await provider.getBalance(NODE2_ADDRESS);
    expect(after - before).to.equal(1n);
  });

  it('rejects an unfunded address with insufficient funds error', async () => {
    const provider = makeProvider();
    // Random unfunded key
    const unfundedKey =
      '0x' + '11'.repeat(32);
    const wallet = makeWallet(unfundedKey, provider);

    let threw = false;
    try {
      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 2,
      });
      await tx.wait();
    } catch (err) {
      threw = true;
    }
    expect(threw, 'expected unfunded send to throw').to.equal(true);
  });

  it('EIP-2930 (type 1) access-list transactions are rejected by Thor — only EIP-1559 is accepted', async () => {
    // Same gap as the legacy rejection below, just one tx-type up. Thor's
    // eth_sendRawTransaction enforces TypeEthDynamicFee on the wire; everything
    // else round-trips back with an RLP / unsupported-type error. If Thor ever
    // accepts EIP-2930, drop this assertion and update README.
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    let caught: unknown;
    try {
      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 1,
        accessList: [
          { address: NODE2_ADDRESS, storageKeys: [] },
        ],
      });
      await tx.wait();
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected EIP-2930 tx to be rejected').to.not.be.undefined;
    const haystack: string[] = [];
    const walk = (obj: unknown, depth: number) => {
      if (depth > 3 || obj == null) return;
      if (typeof obj === 'string') haystack.push(obj);
      else if (typeof obj === 'object') {
        for (const v of Object.values(obj as Record<string, unknown>)) walk(v, depth + 1);
      }
    };
    walk(caught, 0);
    const blob = haystack.join(' || ');
    expect(blob).to.match(/rlp|access|unsupported|expected List|coalesce|type/i);
  });

  it('Legacy (type 0) transactions are rejected by Thor — only EIP-1559 is accepted', async () => {
    // Documenting a real compatibility gap: Thor's Ethereum-compat RPC only
    // accepts EIP-1559 (type 2) envelopes. A legacy RLP submission comes back
    // as `rlp: expected List` from eth_sendRawTransaction. If this assertion
    // ever flips to a successful send, drop the test and update the README.
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    let caught: unknown;
    try {
      const tx = await wallet.sendTransaction({
        to: NODE2_ADDRESS,
        value: 1n,
        type: 0,
      });
      await tx.wait();
    } catch (err) {
      caught = err;
    }
    expect(caught, 'expected legacy tx to be rejected').to.not.be.undefined;
    // The diagnostic text we care about can live in .message, .shortMessage,
    // .info.error.message, etc. — scan everything and assert the RLP gripe is
    // somewhere in there.
    const haystack: string[] = [];
    const walk = (obj: unknown, depth: number) => {
      if (depth > 3 || obj == null) return;
      if (typeof obj === 'string') haystack.push(obj);
      else if (typeof obj === 'object') {
        for (const v of Object.values(obj as Record<string, unknown>)) walk(v, depth + 1);
      }
    };
    walk(caught, 0);
    const blob = haystack.join(' || ');
    expect(blob).to.match(/rlp|legacy|unsupported|expected List|coalesce/i);
  });

  it('signMessage produces a signature that verifyMessage recovers', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);
    const message = 'hello thor';

    const sig = await wallet.signMessage(message);
    expect(sig).to.match(/^0x[0-9a-fA-F]{130}$/);

    const recovered = verifyMessage(message, sig);
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('signTypedData (EIP-712) produces a signature that verifyTypedData recovers', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    const chainId = (await provider.getNetwork()).chainId;
    const domain = {
      name: 'InterstellarTest',
      version: '1',
      chainId,
      verifyingContract: '0x0000000000000000000000000000000000000000',
    };
    const types = {
      Mail: [
        { name: 'from', type: 'address' },
        { name: 'to', type: 'address' },
        { name: 'contents', type: 'string' },
      ],
    };
    const message = {
      from: TEST_SENDER_ADDRESS,
      to: NODE2_ADDRESS,
      contents: 'hi',
    };

    const sig = await wallet.signTypedData(domain, types, message);
    expect(sig).to.match(/^0x[0-9a-fA-F]{130}$/);

    const recovered = verifyTypedData(domain, types, message, sig);
    expect(recovered.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('populateTransaction fills nonce, gasLimit, and EIP-1559 fee fields', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    const populated = await wallet.populateTransaction({
      to: NODE2_ADDRESS,
      value: 1n,
      type: 2,
    });

    expect(populated.nonce).to.be.a('number').and.to.be.at.least(0);
    expect(typeof populated.gasLimit === 'bigint' || typeof populated.gasLimit === 'string')
      .to.equal(true);
    expect(populated.maxFeePerGas, 'maxFeePerGas').to.not.be.undefined;
    expect(populated.maxPriorityFeePerGas, 'maxPriorityFeePerGas').to.not.be.undefined;
    expect(Number(populated.chainId)).to.be.greaterThan(0);
    expect(populated.from!.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('signTransaction produces a raw RLP that Transaction.from parses back', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    const populated = await wallet.populateTransaction({
      to: NODE2_ADDRESS,
      value: 2n,
      type: 2,
    });
    // signTransaction needs `from` stripped — ethers v6 rejects the populated `from`.
    const { from: _from, ...toSign } = populated;
    const raw = await wallet.signTransaction(toSign);
    expect(raw).to.match(/^0x[0-9a-fA-F]+$/);

    const parsed = Transaction.from(raw);
    expect(parsed.to!.toLowerCase()).to.equal(NODE2_ADDRESS.toLowerCase());
    expect(parsed.value).to.equal(2n);
    expect(parsed.type).to.equal(2);
    expect(parsed.from!.toLowerCase()).to.equal(TEST_SENDER_ADDRESS.toLowerCase());
  });

  it('broadcastTransaction accepts an offline-signed raw tx and confirms it', async () => {
    const provider = makeProvider();
    const wallet = makeWallet(TEST_SENDER_KEY, provider);

    const before = await provider.getBalance(NODE2_ADDRESS);

    const populated = await wallet.populateTransaction({
      to: NODE2_ADDRESS,
      value: 3n,
      type: 2,
    });
    const { from: _from, ...toSign } = populated;
    const raw = await wallet.signTransaction(toSign);

    const sent = await provider.broadcastTransaction(raw);
    const receipt = await sent.wait();
    expect(receipt!.status).to.equal(1);
    expect(sent.hash).to.match(/^0x[0-9a-fA-F]{64}$/);

    const after = await provider.getBalance(NODE2_ADDRESS);
    expect(after - before).to.equal(3n);
  });

  it('HDNodeWallet.fromPhrase produces a deterministic address from a mnemonic', () => {
    const phrase =
      'test test test test test test test test test test test junk';
    // fromPhrase defaults to the standard Ethereum path m/44'/60'/0'/0/0,
    // which is what the well-known Hardhat/Anvil "test junk" mnemonic uses.
    const child = HDNodeWallet.fromPhrase(phrase);
    expect(child.address).to.equal('0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266');
    // Sanity-check the Mnemonic struct round-trips the same phrase.
    expect(Mnemonic.fromPhrase(phrase).phrase).to.equal(phrase);
  });
});
