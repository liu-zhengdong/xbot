import { test } from 'node:test';
import assert from 'node:assert/strict';
import { authenticate, b64, decode, enc, envelope, hash, hex } from './wire.ts';
const key = await crypto.subtle.generateKey('Ed25519', true, ['sign', 'verify']);
const pub = new Uint8Array(await crypto.subtle.exportKey('raw', key.publicKey));
const caller = hex((await hash(pub)).slice(0, 16));
const sign = async s => new Uint8Array(await crypto.subtle.sign('Ed25519', key.privateKey, enc.encode(s)));
const url64 = b => b64(b).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
test('request authentication binds method, query, timestamp and body', async () => {
  const secs = 1792281600, body = enc.encode('{}'), path = '/v1/send?x=%2F';
  const sig = await sign(`xbot-gw-v1\nPOST\n${path}\n${secs}\n${hex(await hash(body))}`);
  const headers = {'X-Xbot-Key': url64(pub), 'X-Xbot-Time': `+0${secs}`, 'X-Xbot-Sig': url64(sig)};
  const request = new Request('https://gateway' + path, {method: 'POST', headers});
  assert.equal((await authenticate(request, body, secs * 1000)).caller, caller);
  await assert.rejects(authenticate(request, enc.encode('[]'), secs * 1000), /bad signature/);
  await assert.rejects(authenticate(request, body, (secs + 301) * 1000), /bad time, server time .*Z/);
  await assert.rejects(authenticate(new Request('https://gateway'), body, secs * 1000), /bad key/);
});
test('strict alphabets and padded envelope encoding', () => {
  assert.deepEqual(decode('YQ=='), enc.encode('a'));
  for (const s of ['YQ', 'Y Q==', '____']) assert.throws(() => decode(s));
  for (const s of ['YQ==', '+w', '/w']) assert.throws(() => decode(s, true));
});
test('envelope UTC signature preserves nanoseconds and original offset', async () => {
  for (const [time, utc, stored] of [
    ['2026-10-18T08:00:00.123456789+08:00', '2026-10-18T00:00:00.123456789Z', '2026-10-18T08:00:00.123456789+08:00'],
    ['2026-10-18T08:00:00,123456789999+08:00', '2026-10-18T00:00:00.123456789Z', '2026-10-18T08:00:00.123456789+08:00'],
    ['0099-01-01T00:00:00.120000000-00:00', '0099-01-01T00:00:00.12Z', '0099-01-01T00:00:00.12Z'],
    ['2024-03-01T00:30:00+01:00', '2024-02-29T23:30:00Z', '2024-03-01T00:30:00+01:00'],
  ]) {
    const e = {v: 1, id: 'a'.repeat(32), from: caller, to: 'b'.repeat(32), time, kind: 'msg', body: null};
    const sig = b64(await sign(`xbot-env-v1\n1|${e.id}|${caller}|${e.to}|${utc}|msg|${hex(await hash(new Uint8Array()))}`));
    const raw = JSON.stringify({...e, sig});
    const normalized = await envelope(raw, caller, pub);
    assert.equal(normalized.json, JSON.stringify({...e, time: stored, sig}));
    assert.equal(normalized.size, enc.encode(normalized.json).length);
  }
});

test('base64 round-trips a MaxBody-sized payload', () => {
  const big = crypto.getRandomValues(new Uint8Array(65536));
  assert.deepEqual(decode(b64(big)), big);
});
const signed = async (e, utc = e.time, body = new Uint8Array()) => ({...e, sig: b64(await sign(`xbot-env-v1\n1|${e.id}|${caller}|${e.to}|${utc}|${e.kind}|${hex(await hash(body))}`))});
const base = {v: 1, id: 'a'.repeat(32), from: caller, to: 'b'.repeat(32), time: '2026-10-18T08:00:00Z', kind: 'msg', body: null};
test('invalid fields are rejected even when correctly signed', async () => {
  for (const e of [{...base, time: '2026-02-30T00:00:00Z'}, {...base, kind: 'Msg'}, {...base, time: '2026-10-18T08:00:00+24:00'}])
    await assert.rejects(envelope(JSON.stringify(await signed(e, e.time.replace('+24:00', 'Z'))), caller, pub));
  const raw = JSON.stringify(await signed(base));
  for (const v of ['1.0', '1e0']) await assert.rejects(envelope(raw.replace('"v":1', `"v":${v}`), caller, pub));
});
test('Go-compatible envelope decoding', async () => {
  const zero = '0001-01-01T00:00:00Z', bytes = new Uint8Array([1, 2, 255]);
  const {time, ...untimed} = await signed(base, zero); void time;
  assert.match((await envelope(JSON.stringify(untimed), caller, pub)).json, /"time":"0001-01-01T00:00:00Z"/);
  const late = await signed({...base, time: '2026-10-18T8:00:00Z'}, '2026-10-18T08:00:00Z');
  assert.match((await envelope(JSON.stringify(late), caller, pub)).json, /"time":"2026-10-18T08:00:00Z"/);
  const arr = await signed({...base, body: [1, 2, 255]}, base.time, bytes);
  assert.match((await envelope(JSON.stringify(arr), caller, pub)).json, /"body":"AQL\/"/);
  await assert.rejects(envelope(JSON.stringify({...arr, body: [1, 2, 256]}), caller, pub));
  const str = await signed({...base, body: 'AQL/'}, base.time, bytes);
  assert.match((await envelope(JSON.stringify(str).replace('"AQL/"', '"AQ\\nL/"'), caller, pub)).json, /"body":"AQL\/"/);
  const blank = await signed({...base, body: ''});
  assert.match((await envelope(JSON.stringify(blank), caller, pub)).json, /"body":""/);
  const mixed = JSON.stringify(await signed(base)).replace('"kind"', '"KIND"').replace('"sig"', '"ſig"');
  assert.match((await envelope(mixed, caller, pub)).json, /"kind":"msg"/);
});
test('a bare trailing ? is part of the signed request target, as in Go', async () => {
  const secs = 1792281600, body = new Uint8Array();
  const sig = await sign(`xbot-gw-v1\nGET\n/v1/poll?\n${secs}\n${hex(await hash(body))}`);
  const request = new Request('https://gateway/v1/poll?', {headers: {'X-Xbot-Key': url64(pub), 'X-Xbot-Time': String(secs), 'X-Xbot-Sig': url64(sig)}});
  assert.equal((await authenticate(request, body, secs * 1000)).caller, caller);
});
