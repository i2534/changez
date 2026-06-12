import { execSync, spawn } from 'child_process';
import * as path from 'path';
import * as fs from 'fs';
import * as http from 'http';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const WEB_DIR = path.resolve(__dirname, '..');
const ROOT_DIR = path.resolve(WEB_DIR, '..');
const DIST_DIR = path.resolve(WEB_DIR, 'dist');
const CONFIG = path.resolve(ROOT_DIR, 'config.yaml');
const PID_FILE = path.resolve(__dirname, '.e2e-server-pid');
const PORT = 8760;
const HOST = '127.0.0.1';

function waitForPort(timeoutMs = 30000): Promise<void> {
  return new Promise((resolve, reject) => {
    const start = Date.now();
    const tryConnect = () => {
      const req = http.get(`http://${HOST}:${PORT}/api/stats`, (res) => {
        res.resume();
        resolve();
      });
      req.on('error', () => {
        if (Date.now() - start >= timeoutMs) {
          reject(new Error(`Server on ${HOST}:${PORT} not ready within ${timeoutMs}ms`));
        } else {
          setTimeout(tryConnect, 500);
        }
      });
      req.setTimeout(2000, () => {
        req.destroy();
        if (Date.now() - start >= timeoutMs) {
          reject(new Error(`Server on ${HOST}:${PORT} not ready within ${timeoutMs}ms`));
        } else {
          setTimeout(tryConnect, 500);
        }
      });
    };
    tryConnect();
  });
}

export default async function setup(): Promise<void> {
  // 1. Build frontend if dist/ doesn't exist
  if (!fs.existsSync(path.join(DIST_DIR, 'index.html'))) {
    console.log('📦 Building frontend...');
    execSync('npm run build', { cwd: WEB_DIR, stdio: 'inherit' });
  }

  // 2. Start Go server
  console.log(`🚀 Starting Go server on ${HOST}:${PORT}...`);
  const proc = spawn('go', ['run', './cmd/changez', '-c', CONFIG], {
    cwd: ROOT_DIR,
    stdio: 'pipe',
    env: { ...process.env },
  });

  proc.stdout?.on('data', (d: Buffer) => {
    for (const line of d.toString().trim().split('\n')) {
      if (line) console.log(`  [server] ${line}`);
    }
  });
  proc.stderr?.on('data', (d: Buffer) => {
    for (const line of d.toString().trim().split('\n')) {
      if (line) console.log(`  [server] ${line}`);
    }
  });
  proc.on('exit', (code) => {
    if (code !== 0 && code !== null) {
      console.log(`  ⚠️  Server exited with code ${code}`);
    }
  });

  // 3. Save PID for teardown
  fs.writeFileSync(PID_FILE, String(proc.pid), 'utf-8');

  // 4. Wait for server to be ready
  console.log('⏳ Waiting for server to be ready...');
  await waitForPort();
  console.log('✅ Server ready!');
}
