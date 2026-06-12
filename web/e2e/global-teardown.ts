import * as fs from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const PID_FILE = path.resolve(__dirname, '.e2e-server-pid');

export default async function teardown(): Promise<void> {
  if (!fs.existsSync(PID_FILE)) {
    console.log('No PID file found, skipping teardown');
    return;
  }

  const pid = parseInt(fs.readFileSync(PID_FILE, 'utf-8').trim(), 10);
  if (!pid) {
    console.log('Invalid PID, skipping teardown');
    fs.unlinkSync(PID_FILE);
    return;
  }

  try {
    process.kill(pid, 'SIGTERM');
    console.log(`🛑 Stopped server (PID ${pid})`);
  } catch {
    console.log(`Process ${pid} already exited`);
  }

  try {
    fs.unlinkSync(PID_FILE);
  } catch {
    // ignore
  }
}
