import { spawn, ChildProcess } from 'node:child_process';
import { Readable } from 'node:stream';
import * as readline from 'node:readline';

const NETWORK_BINARY = '/tmp/interstellar-network';
const STARTUP_TIMEOUT_MS = 15 * 60 * 1000;
const RUNTIME_URL_ENV = 'INTERSTELLAR_RUNTIME_NODE_URL';
const RUNTIME_P2P_ENV = 'INTERSTELLAR_RUNTIME_P2P_PORTS';

let networkProc: ChildProcess | null = null;

interface ReadyLine {
  nodes: string[];
  p2pPorts: number[];
}

function readReadyLine(proc: ChildProcess): Promise<ReadyLine> {
  return new Promise((resolve, reject) => {
    const stdout = proc.stdout;
    if (!stdout) {
      reject(new Error('network process has no stdout pipe'));
      return;
    }
    const rl = readline.createInterface({ input: stdout as Readable });
    let timer: NodeJS.Timeout;

    const cleanup = () => {
      clearTimeout(timer);
      rl.close();
    };

    timer = setTimeout(() => {
      cleanup();
      reject(new Error(`network did not emit ready line within ${STARTUP_TIMEOUT_MS}ms`));
    }, STARTUP_TIMEOUT_MS);

    rl.on('line', (line) => {
      try {
        const parsed = JSON.parse(line);
        if (Array.isArray(parsed.nodes) && parsed.nodes.length > 0) {
          cleanup();
          resolve(parsed as ReadyLine);
          return;
        }
      } catch {
        // Non-JSON stdout line (e.g. git output from ThorBuilder) — forward to stderr
        // so it remains visible without blocking the ready-line scan.
        process.stderr.write(`[network] ${line}\n`);
      }
    });

    proc.once('exit', (code, signal) => {
      cleanup();
      reject(new Error(`network exited before ready (code=${code}, signal=${signal})`));
    });

    proc.once('error', (err) => {
      cleanup();
      reject(err);
    });
  });
}

export async function mochaGlobalSetup(): Promise<void> {
  if (process.env.NODE_URL) {
    process.env[RUNTIME_URL_ENV] = process.env.NODE_URL;
    console.log(`[ethersjs] using external NODE_URL: ${process.env.NODE_URL}`);
    return;
  }

  // The Ethereum-compatible RPC endpoint (POST /rpc) lives on the thor
  // `pedro/eth_eq_json_rpc` branch. The default `evm-upgrades` branch built by
  // network/setup/network.go does not expose it, so set THOR_BRANCH here unless
  // the caller explicitly overrode it. Same trick the schema TestMain used
  // before commit 416eed1 — kept here so ethers tests are self-contained.
  const childEnv = { ...process.env };
  if (!childEnv.THOR_BRANCH) {
    childEnv.THOR_BRANCH = 'pedro/eth_eq_json_rpc';
  }

  console.log(
    `[ethersjs] spawning ${NETWORK_BINARY} start (THOR_BRANCH=${childEnv.THOR_BRANCH})`,
  );
  networkProc = spawn(NETWORK_BINARY, ['start'], {
    stdio: ['ignore', 'pipe', 'inherit'],
    env: childEnv,
  });

  const ready = await readReadyLine(networkProc);
  process.env[RUNTIME_URL_ENV] = ready.nodes[0];
  process.env[RUNTIME_P2P_ENV] = ready.p2pPorts.join(',');
  console.log(
    `[ethersjs] network ready — node: ${ready.nodes[0]}, p2p: ${ready.p2pPorts.join(',')}`,
  );
}

export async function mochaGlobalTeardown(): Promise<void> {
  if (!networkProc) return;
  console.log('[ethersjs] stopping network');
  await new Promise<void>((resolve) => {
    networkProc!.once('exit', () => resolve());
    networkProc!.kill('SIGTERM');
  });
  networkProc = null;
}

export function getNodeUrl(): string {
  const url = process.env[RUNTIME_URL_ENV];
  if (!url) {
    throw new Error('node URL not initialized — globalSetup must run first');
  }
  return url;
}

export function getP2PPorts(): number[] {
  const raw = process.env[RUNTIME_P2P_ENV];
  if (!raw) return [];
  return raw.split(',').map((s) => Number(s));
}
