// 2x screenshots of the bot avatars: sidebar, chat header and the profile
// drawer's avatar picker, light and dark. Usage:
//   node shots.mjs <base-url> <login-token> <out-dir> <label> <chromium|msedge|webkit>
import { chromium, webkit } from 'playwright';
import fs from 'node:fs';
import path from 'node:path';

const [base, token, out, label, engine] = process.argv.slice(2);
fs.mkdirSync(out, { recursive: true });
const browser = engine === 'webkit' ? await webkit.launch() : await chromium.launch(engine === 'msedge' ? { channel: 'msedge' } : {});
const log = (...a) => console.log(new Date().toISOString(), ...a);
for (const theme of ['light', 'dark']) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, deviceScaleFactor: 2, locale: 'zh-CN', colorScheme: theme });
  await ctx.addInitScript((t) => { try { localStorage.setItem('xbot-theme', t); localStorage.setItem('xbot-panel-open', 'false'); } catch (e) {} }, theme);
  const page = await ctx.newPage();
  page.on('console', (m) => { if (m.type() === 'error') log('console', m.text()); });
  await page.goto(base + '/');
  const res = await page.evaluate(async (tok) => (await fetch('/api/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token: tok }) })).status, token);
  log(theme, 'login', res);
  await page.goto(base + '/');
  await page.waitForSelector('#shell:not([hidden])', { timeout: 30000 });
  await page.waitForFunction(() => document.querySelectorAll('.avatar img, .avatar.glyph').length > 2, null, { timeout: 30000 });
  await page.evaluate(() => document.fonts.ready);
  await page.waitForTimeout(800);
  const dpr = await page.evaluate(() => window.devicePixelRatio);
  const bundle = await page.evaluate(() => !!window.XBotAvatar);
  log(theme, 'dpr', dpr, 'bundle', bundle);
  const shot = async (name, locator) => {
    const file = path.join(out, `${label}-${name}-${theme}.png`);
    if (locator) await page.locator(locator).first().screenshot({ path: file });
    else await page.screenshot({ path: file });
    log('shot', file);
  };
  await shot('window');
  await shot('sidebar', '.sidebar');
  await shot('chat-header', '.title');
  // Open the info panel, then 编辑资料.
  if ((await page.locator('#info').getAttribute('aria-expanded')) !== 'true') await page.locator('#info').click();
  await page.locator('#panel-actions button[aria-label="编辑资料"]').first().click();
  await page.waitForSelector('.profile-drawer:not([hidden])', { timeout: 10000 });
  await page.waitForTimeout(600);
  await shot('settings-avatar', '.profile-modal');
  await shot('settings-window');
  await ctx.close();
}
await browser.close();
