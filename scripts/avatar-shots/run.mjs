// CI only: seed a throwaway root, start `xbot serve` on it, then take the 2x
// light/dark avatar shots with Playwright. No real key; nothing is published.
//   node run.mjs <xbot> <seed> <out> <label> <chromium|msedge|webkit>
import { spawn, execFileSync } from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
const [xbot, seed, out, label, engine] = process.argv.slice(2);
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'xbot-avatar-shots-'));
execFileSync(seed, [root, 'rich'], { stdio: 'inherit' });
const port = 18790;
const env = { ...process.env, XBOT_API_KEY: 'ci-screenshot-not-a-key', XBOT_AUTO_UPGRADE: '0' };
const srv = spawn(xbot, ['--root', root, 'serve', '--http', `127.0.0.1:${port}`], { env });
const token = await new Promise((ok, fail) => {
  let buf = '';
  const on = (d) => { buf += d; const m = /login token:\s*(\S+)/.exec(buf); if (m) ok(m[1]); };
  srv.stdout.on('data', on); srv.stderr.on('data', on);
  srv.on('exit', (c) => fail(new Error('serve exited ' + c + '\n' + buf.replace(/login token:\s*\S+/g, 'login token: <redacted>'))));
  setTimeout(() => fail(new Error('no login token after 60s')), 60000);
});
const here = path.dirname(new URL(import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1'));
try {
  execFileSync(process.execPath, [path.join(here, 'shots.mjs'), `http://127.0.0.1:${port}`, token, out, label, engine], { stdio: 'inherit' });
} finally { srv.kill(); }
