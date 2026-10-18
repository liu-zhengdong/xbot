import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { DatabaseSync } from 'node:sqlite';
async function object(name) {
  const source = (await readFile(new URL(`./${name.toLowerCase()}.ts`, import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
  const prelude = `const enc=new TextEncoder(), hex=b=>Array.from(b,x=>x.toString(16).padStart(2,"0")).join(""), hash=async b=>new Uint8Array(await crypto.subtle.digest("SHA-256",b)); class DurableObject { constructor(ctx, env) { this.ctx=ctx; this.env=env; } } const idRE=/^[a-f0-9]{32}$/, fields=raw=>{const v=JSON.parse(raw); if(v===null) return null; if(typeof v!=='object'||Array.isArray(v)) throw Error(); return Object.fromEntries(Object.entries(v).map(([k,v])=>[k.toLowerCase(),v]));}; const result=(status,value)=>({status,value}), error=(s,e)=>result(s,{error:e}), jsonResponse=r=>new Response(JSON.stringify(r.value),{status:r.status}), empty='';`;
  const mod = await import('data:text/javascript;base64,' + Buffer.from(prelude + stripTypeScriptTypes(source)).toString('base64'));
  const db = new DatabaseSync(':memory:'), writes = [];
  const ctx = {blockConcurrencyWhile: fn=>fn(), getWebSockets: ()=>[], storage: {deleteAlarm: async()=>{}, setAlarm: async()=>{}, getAlarm: async()=>null, deleteAll: async()=>db.exec('DROP TABLE meta; DROP TABLE queue; DROP TABLE acked; DROP TABLE unreleased'), sql: {exec(q,...args) { args = args.map(a=>a instanceof ArrayBuffer ? new Uint8Array(a) : a);
    if (!q.startsWith('SELECT')) writes.push([q,...args]);
    if (q.startsWith('CREATE')) { db.exec(q); return {toArray:()=>[]}; }
    return {toArray:()=>db.prepare(q).all(...args).map(row=>Object.fromEntries(Object.entries(row).map(([k,v])=>[k, v instanceof Uint8Array ? v.buffer.slice(v.byteOffset, v.byteOffset+v.byteLength) : v])))};
  }}}};
  return {instance: new mod[name](ctx, {MAILBOX:{getByName:()=>({registered:async()=>{}})}}), db, writes};
}
test('unregistered mailboxes stay read-only, including after eviction', async () => {
  const {instance:b, writes} = await object('Mailbox'); assert.equal(writes.length,0);
  const check = async()=>{ const n=writes.length; assert.deepEqual(await b.ack('{}'),{status:403,value:{error:'not registered'}}); const r=await b.fetch(new Request('https://mailbox')); assert.equal(r.status,403); assert.deepEqual(await r.json(),{error:'not registered'}); assert.equal((await b.enqueue('', '', '', 0, Date.now())).status,404); assert.deepEqual(await b.idle(Date.now()),{seen:0,empty:true}); assert.equal(await b.forget(Date.now()),true); await b.alarm(); assert.equal(writes.length,n); };
  await check(); await b.registered([]); assert.equal(await b.forget(Date.now()+31*86400000),true); await check();
});
test('daily registration limit exempts existing installs and stores hashed IPs', async () => {
  const {instance:d, db, writes} = await object('Directory'); d.env.MAX_INSTALLS='1000';
  for (let i=0;i<30;i++) assert.equal((await d.register(String(i),[],null,'192.0.2.1')).status,200);
  assert.deepEqual(await d.register('31',[],null,'192.0.2.1'),{status:429,value:{error:'too many registrations'}}); assert.equal((await d.register('0',[],null,'192.0.2.1')).status,200);
  assert.equal((await d.register('32',[],null,'192.0.2.2')).status,200); assert.ok(!JSON.stringify(writes).includes('192.0.2.'));
  db.exec("UPDATE registrations SET day='2000-01-01'; UPDATE regkey SET day='2000-01-01'"); d.alarm(); assert.equal(db.prepare('SELECT COUNT(*) n FROM regkey').get().n,0); assert.equal(db.prepare('SELECT COUNT(*) n FROM registrations').get().n,0); assert.equal((await d.register('31',[],null,'192.0.2.1')).status,200); assert.equal(db.prepare('SELECT n FROM registrations').get().n,1);
});
test('eviction scans beyond eight stale installs and evicts only one', async()=>{
  const {instance:d,db}=await object('Directory'); d.env.MAX_INSTALLS='10'; for(let i=0;i<10;i++) db.prepare('INSERT INTO installs VALUES(?,?,0)').run(String(i),'[]'); const scanned=[],forgot=[]; d.env.MAILBOX.getByName=id=>({registered:async()=>{},idle:async()=>{scanned.push(id);return {seen:0,empty:Number(id)>=8};},forget:async()=>{forgot.push(id);return true;}}); assert.equal((await d.register('new',[],null,'')).status,200); assert.equal(scanned.length,9); assert.equal(forgot.length,1); assert.equal(db.prepare('SELECT COUNT(*) n FROM installs').get().n,10);
});
test('reservations are idempotent by envelope id', async () => {
  const {instance:d, db} = await object('Directory'); const total = () => db.prepare('SELECT COALESCE(SUM(n),0) n FROM bytes').get().n;
  assert.equal(d.reserve('e1','s',100), true); assert.equal(d.reserve('e1','s',100), true); assert.equal(total(), 100);
  d.release(['e1']); d.release(['e1']); assert.equal(total(), 0);
});
test('a lost reserve reply is undone, and a failed release is retried later', async () => {
  const {instance:b, db} = await object('Mailbox'); await b.registered([]); const calls = []; let fail = true;
  b.env.DIRECTORY = {getByName:()=>({reserve:async()=>{ calls.push('reserve'); throw Error('lost reply'); }, release:async ids=>{ calls.push(['release', ...ids]); if (fail) throw Error('down'); }})};
  await assert.rejects(b.enqueue('{}','s','a'.repeat(32),2,Date.now()), /lost reply/);
  assert.equal(db.prepare('SELECT COUNT(*) n FROM queue').get().n, 0); assert.equal(db.prepare('SELECT id FROM unreleased').get().id, 'a'.repeat(32));
  fail = false; await b.alarm(); assert.deepEqual(calls.at(-1), ['release','a'.repeat(32)]); assert.equal(db.prepare('SELECT COUNT(*) n FROM unreleased').get().n, 0);
});
test('eviction keeps an install that re-registered meanwhile', async () => {
  const {instance:d, db} = await object('Directory'); d.env.MAX_INSTALLS='1'; db.prepare('INSERT INTO installs VALUES(?,?,0)').run('old','[]');
  d.env.MAILBOX.getByName = id => ({registered:async()=>{}, idle:async()=>({seen:0,empty:true}), forget:async()=>{ db.prepare('UPDATE installs SET seen=? WHERE id=?').run(Date.now(), 'old'); return true; }});
  assert.equal((await d.register('new',[],null,'')).status, 403); assert.equal(db.prepare("SELECT COUNT(*) n FROM installs WHERE id='old'").get().n, 1);
});
const A='a'.repeat(32), B='b'.repeat(32), C='c'.repeat(32), L='{"lookup":"K7QM","ttl":60}';
async function codes() {
  const o = await object('Directory'), pends = []; o.instance.env.MAX_INSTALLS = '1000';
  o.instance.env.MAILBOX.getByName = id => ({registered:async()=>{}, pend:async(...a)=>pends.push([id,...a])});
  for (const id of [A, C]) await o.instance.register(id, [], null, '');
  return {...o, pends};
}
test('short codes are stored hashed, answer 410 for an hour after expiry, then 404', async () => {
  const {instance:d, db, writes} = await codes(); const realNow = Date.now; let now = realNow(); Date.now = () => now;
  try {
    assert.equal((await d.codeRegister(A, L, '')).status, 200); assert.ok(!JSON.stringify(writes).includes('K7QM'));
    assert.deepEqual(await d.codeClaim(B, [1], L, ''), {status:200, value:{install:A}});
    now += 61000; assert.equal((await d.codeClaim(B, [1], L, '')).status, 410);
    now += 3600000; assert.equal((await d.codeClaim(B, [1], L, '')).status, 404); assert.equal(db.prepare('SELECT COUNT(*) n FROM codes').get().n, 0);
  } finally { Date.now = realNow; }
});
test('failed claims are limited per IPv6 /64, successes are not counted', async () => {
  const {instance:d} = await codes(); await d.codeRegister(A, L, '');
  for (let i = 0; i < 3; i++) assert.equal((await d.codeClaim(B, [1], L, '2001:db8::1')).status, 200);
  for (let i = 0; i < 10; i++) assert.equal((await d.codeClaim(B, [1], '{"lookup":"2222"}', `2001:db8:0:0:${i}::1`)).status, 404);
  assert.deepEqual(await d.codeClaim(B, [1], L, '2001:db8::9'), {status:429, value:{error:'too many tries'}});
  assert.equal((await d.codeClaim(B, [1], L, '2001:db9::1')).status, 200);
});
test('a pending install moved to another owner keeps its expiry; admission uses install capacity', async () => {
  const {instance:d, db, pends} = await codes(); await d.codeRegister(A, L, ''); await d.codeClaim(B, [1], L, '');
  await d.codeRegister(C, '{"lookup":"P9XZ","ttl":60}', ''); await d.codeClaim(B, [1], '{"lookup":"P9XZ"}', '');
  assert.equal(pends[1][2], C); assert.equal(pends[1][3], pends[0][3]);
  assert.equal((await d.codeAdmit(A, JSON.stringify({install:B}))).status, 404);
  d.env.MAX_INSTALLS = '2'; assert.deepEqual(await d.codeAdmit(C, JSON.stringify({install:B})), {status:403, value:{error:'too many installs'}});
  d.env.MAX_INSTALLS = '3'; assert.equal((await d.codeAdmit(C, JSON.stringify({install:B}))).status, 200); assert.equal(db.prepare('SELECT COUNT(*) n FROM pending').get().n, 0);
});
test('an expired pending mailbox drops its mail and releases the quota', async () => {
  const {instance:b, db} = await object('Mailbox'); const released = [];
  b.env.DIRECTORY = {getByName:()=>({reserve:async()=>true, release:async ids=>{ released.push(...ids); }})};
  await b.pend([1], A, Date.now() + 1000);
  assert.equal((await b.enqueue('{}', C, 'e'.repeat(32), 2, Date.now())).status, 403);
  assert.equal((await b.enqueue('{}', A, 'e'.repeat(32), 2, Date.now())).status, 202);
  db.exec("UPDATE meta SET v='1' WHERE k='pexp'"); await b.alarm();
  assert.deepEqual(released, ['e'.repeat(32)]); assert.equal((await b.ack('{}')).status, 403);
});
