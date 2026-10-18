import { DurableObject } from "cloudflare:workers";
import { empty, error, jsonResponse, result, waitSeconds, type Env } from "./index";
import { fields, idRE } from "./wire";
type Row = {id: string; sender: string; at: number; seq: number; size: number; data: string};
const TTL = 7 * 86400000;
export class Mailbox extends DurableObject<Env> {
  private ready: boolean;
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    this.ready = ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'").toArray().length > 0;
  }
  private sql<T extends Record<string, SqlStorageValue>>(q: string, ...args: SqlStorageValue[]) { return this.ctx.storage.sql.exec<T>(q, ...args).toArray(); }
  private meta(k: string) { return this.sql<{v: string}>("SELECT v FROM meta WHERE k=?", k)[0]?.v; }
  private set(k: string, v: string | number) { this.sql("INSERT OR REPLACE INTO meta VALUES(?,?)", k, String(v)); }
  private touch() { const now = Date.now(); if (now - Number(this.meta("seen") || 0) > 3600000) this.set("seen", now); }
  private async release(rows: Row[]) {
    if (!rows.length) return;
    const bytes: Record<string, number> = {};
    for (const r of rows) bytes[r.sender] = (bytes[r.sender] || 0) + r.size;
    await this.env.DIRECTORY.getByName("global").release(bytes);
  }
  private async schedule() {
    const at = this.sql<{at: number | null}>("SELECT MIN(at) AS at FROM queue")[0].at;
    const wanted = at === null ? null : at + TTL;
    if (await this.ctx.storage.getAlarm() !== wanted) {
      if (wanted === null) await this.ctx.storage.deleteAlarm(); else await this.ctx.storage.setAlarm(wanted);
    }
  }
  private async expire(now = Date.now()) {
    const rows = this.sql<Row>("DELETE FROM queue WHERE at<=? RETURNING *", now - TTL);
    this.sql("DELETE FROM acked WHERE at<=?", now - 600000);
    if (rows.length) { await this.release(rows); await this.schedule(); }
  }
  private list() {
    const out: string[] = []; let size = 16;
    for (const r of this.sql<Row>("SELECT * FROM queue ORDER BY at,seq LIMIT 32")) {
      if (out.length && size + r.size + 1 > 786432) break;
      out.push(r.data); size += r.size + 1;
    }
    return '{"envelopes":[' + out.join(",") + ']}\n';
  }
  private wake(text: string) {
    for (const ws of this.ctx.getWebSockets()) try { ws.send(text); ws.close(1000); } catch {}
  }
  registered(pub: number[]) { return this.ctx.blockConcurrencyWhile(async () => {
    this.ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY,v TEXT); CREATE TABLE IF NOT EXISTS queue(id TEXT PRIMARY KEY,sender TEXT,at INTEGER,seq INTEGER,size INTEGER,data TEXT); CREATE TABLE IF NOT EXISTS acked(id TEXT PRIMARY KEY,at INTEGER)");
    this.ready = true; await this.expire(); this.set("pub", JSON.stringify(pub)); this.set("registered", 1); this.touch(); }); }
  idle(now: number) { return this.ctx.blockConcurrencyWhile(async () => { if (!this.ready) return {seen: 0, empty: true}; await this.expire(now); return {seen: Number(this.meta("seen") || 0), empty: !this.sql("SELECT id FROM queue LIMIT 1").length}; }); }
  forget(now: number) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return true;
    await this.expire(now);
    if (now - Number(this.meta("seen") || 0) <= 30 * 86400000 || this.sql("SELECT id FROM queue LIMIT 1").length) return false;
    this.wake(empty); await this.ctx.storage.deleteAll(); this.ready = false; await this.ctx.storage.deleteAlarm(); return true;
  }); }
  alarm() { return this.ctx.blockConcurrencyWhile(async () => { if (this.ready) await this.expire(); }); }
  enqueue(data: string, sender: string, id: string, size: number, now: number) {
    return this.ctx.blockConcurrencyWhile(async () => {
      if (!this.ready) return error(404, "unknown recipient");
      await this.expire(now);
      if (!this.meta("registered")) return error(404, "unknown recipient");
      if (this.sql("SELECT id FROM queue WHERE id=? UNION ALL SELECT id FROM acked WHERE id=?", id, id).length) return result(200, {status: "duplicate"});
      const rows = this.sql<Row>("SELECT * FROM queue"), pair = rows.filter(r => r.sender === sender);
      if (rows.length >= 512 || pair.length >= 64 || rows.reduce((n, r) => n + r.size, size) > 8 * 1048576 || pair.reduce((n, r) => n + r.size, size) > 1048576) return error(507, "queue full");
      const seq = Number(this.meta("seq") || 0) + 1; this.set("seq", seq);
      // Occupy the slot synchronously before the cross-object reservation; the gate hides pending mail.
      this.sql("INSERT INTO queue VALUES(?,?,?,?,?,?)", id, sender, now, seq, size, data);
      let reserved = false;
      try { reserved = await this.env.DIRECTORY.getByName("global").reserve(sender, size); }
      finally { if (!reserved) this.sql("DELETE FROM queue WHERE id=?", id); }
      if (!reserved) return error(507, "queue full");
      await this.schedule(); this.wake(this.list()); return result(202, {status: "queued"});
    });
  }
  ack(raw: string) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return error(403, "not registered");
    await this.expire();
    if (!this.meta("registered")) return error(403, "not registered");
    this.touch(); let ids: unknown;
    try {
      ids = fields(raw)?.ids ?? [];
      if (!Array.isArray(ids) || ids.length > 64 || ids.some(id => typeof id !== "string" || !idRE.test(id))) throw Error();
    } catch { return error(400, "bad ids"); }
    const removed: Row[] = [], now = Date.now();
    for (const id of ids as string[]) {
      const rows = this.sql<Row>("DELETE FROM queue WHERE id=? RETURNING *", id);
      if (rows.length) { removed.push(...rows); this.sql("INSERT OR REPLACE INTO acked VALUES(?,?)", id, now); }
    }
    if (removed.length) { await this.release(removed); await this.schedule(); }
    return result(200, {acked: removed.length});
  }); }
  fetch(r: Request) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return jsonResponse(error(403, "not registered"));
    await this.expire();
    if (!this.meta("registered")) return jsonResponse(error(403, "not registered"));
    this.touch(); this.wake(empty);
    const text = this.list(), wait = waitSeconds(new URL(r.url).searchParams.get("wait"));
    if (text !== empty || !wait) return new Response(text, {headers: {"Content-Type": "application/json"}});
    const pair = new WebSocketPair(); this.ctx.acceptWebSocket(pair[1]);
    return new Response(null, {status: 101, webSocket: pair[0]});
  }); }
  webSocketClose() {}
  webSocketError() {}
}
