import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import { stripTypeScriptTypes } from 'node:module';
import { DatabaseSync } from 'node:sqlite';
async function object(name) {
  const source = (await readFile(new URL(`./${name.toLowerCase()}.ts`, import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
  const prelude = `const enc=new TextEncoder(), hex=b=>Array.from(b,x=>x.toString(16).padStart(2,"0")).join(""), hash=async b=>new Uint8Array(await crypto.subtle.digest("SHA-256",b)); class DurableObject { constructor(ctx, env) { this.ctx=ctx; this.env=env; } } const result=(status,value)=>({status,value}), error=(s,e)=>result(s,{error:e}), jsonResponse=r=>new Response(JSON.stringify(r.value),{status:r.status}), empty='';`;
  const mod = await import('data:text/javascript;base64,' + Buffer.from(prelude + stripTypeScriptTypes(source)).toString('base64'));
  const db = new DatabaseSync(':memory:'), writes = [];
  const ctx = {blockConcurrencyWhile: fn=>fn(), getWebSockets: ()=>[], storage: {deleteAlarm: async()=>{}, deleteAll: async()=>db.exec('DROP TABLE meta; DROP TABLE queue; DROP TABLE acked'), sql: {exec(q,...args) {
    if (!q.startsWith('SELECT')) writes.push([q,...args]);
    if (q.startsWith('CREATE')) { db.exec(q); return {toArray:()=>[]}; }
    return {toArray:()=>db.prepare(q).all(...args)};
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
  db.exec("UPDATE registrations SET day='2000-01-01'"); assert.equal((await d.register('31',[],null,'192.0.2.1')).status,200); assert.equal(db.prepare('SELECT n FROM registrations').get().n,1);
});
test('eviction scans beyond eight stale installs and evicts only one', async()=>{
  const {instance:d,db}=await object('Directory'); d.env.MAX_INSTALLS='10'; for(let i=0;i<10;i++) db.prepare('INSERT INTO installs VALUES(?,?,0)').run(String(i),'[]'); const scanned=[],forgot=[]; d.env.MAILBOX.getByName=id=>({registered:async()=>{},idle:async()=>{scanned.push(id);return {seen:0,empty:Number(id)>=8};},forget:async()=>{forgot.push(id);return true;}}); assert.equal((await d.register('new',[],null,'')).status,200); assert.equal(scanned.length,9); assert.equal(forgot.length,1); assert.equal(db.prepare('SELECT COUNT(*) n FROM installs').get().n,10);
});
