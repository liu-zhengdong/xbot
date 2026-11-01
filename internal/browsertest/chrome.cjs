// Shared by standalone browser evidence scripts. No system Chrome fallback.
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
module.exports = function chrome() {
  let executable = process.env.XBOT_TEST_CHROME;
  if (!executable) {
    executable = (process.env.PATH || '').split(path.delimiter)
      .map(dir => path.join(dir, 'chrome-headless-shell')).find(p => fs.existsSync(p));
  }
  if (!executable) {
    const cache = process.env.PLAYWRIGHT_BROWSERS_PATH || (process.platform === 'darwin'
      ? path.join(os.homedir(), 'Library/Caches/ms-playwright')
      : process.platform === 'win32' ? path.join(process.env.LOCALAPPDATA, 'ms-playwright')
      : path.join(process.env.XDG_CACHE_HOME || path.join(os.homedir(), '.cache'), 'ms-playwright'));
    if (fs.existsSync(cache)) {
      for (const revision of fs.readdirSync(cache).filter(n => n.startsWith('chromium_headless_shell-')).sort().reverse()) {
        const root = path.join(cache, revision);
        for (const platform of fs.readdirSync(root)) {
          const p = path.join(root, platform, process.platform === 'win32' ? 'chrome-headless-shell.exe' : 'chrome-headless-shell');
          if (fs.existsSync(p)) { executable = p; break; }
        }
        if (executable) break;
      }
    }
  }
  if (!executable) throw Error('chrome-headless-shell unavailable; set XBOT_TEST_CHROME');
  console.error('browser executable: ' + executable);
  return executable;
};
