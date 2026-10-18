import { DurableObject } from "cloudflare:workers";
import { empty, error, jsonResponse, result, waitSeconds, type Env } from "./index";
import { fields, idRE } from "./wire";
type Row = {id: string; sender: string; at: number; seq: number; size: number; data: string};
const TTL = 7 * 86400000, TABLES = "CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY,v TEXT); CREATE TABLE IF NOT EXISTS queue(id TEXT PRIMARY KEY,sender TEXT,at INTEGER,seq INTEGER,size INTEGER,data TEXT); CREATE TABLE IF NOT EXISTS acked(id TEXT PRIMARY KEY,at INTEGER); CREATE TABLE IF NOT EXISTS unreleased(id TEXT PRIMARY KEY)";
export class Mailbox extends DurableObject<Env> {
  private ready: boolean;
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    this.ready = ctx.storage.sql.exec("SELECT name FROM sqlite_master WHERE type='table' AND name='meta'").toArray().length > 0;
    if (this.ready) ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS unreleased(id TEXT PRIMARY KEY)"); // mailboxes made before this table existed
  }
  private sql<T extends Record<string, SqlStorageValue>>(q: string, ...args: SqlStorageValue[]) { return this.ctx.storage.sql.exec<T>(q, ...args).toArray(); }
  private meta(k: string) { return this.sql<{v: string}>("SELECT v FROM meta WHERE k=?", k)[0]?.v; }
  private set(k: string, v: string | number) { this.sql("INSERT OR REPLACE INTO meta VALUES(?,?)", k, String(v)); }
  private touch() { const now = Date.now(); if (now - Number(this.meta("seen") || 0) > 3600000) this.set("seen", now); }
  // Quota releases that failed are kept and retried (Directory releases by id, so retries are safe).
  private async release(ids: string[]) {
    const pending = this.sql<{id: string}>("SELECT id FROM unreleased").map(r => r.id), all = [...new Set([...ids, ...pending])];
    if (!all.length) return;
    try { await this.env.DIRECTORY.getByName("global").release(all); if (pending.length) this.sql("DELETE FROM unreleased"); }
    catch { for (const id of ids) this.sql("INSERT OR IGNORE INTO unreleased VALUES(?)", id); }
  }
  private async schedule() {
    const at = this.sql<{at: number | null}>("SELECT MIN(at) AS at FROM queue")[0].at;
    let wanted = at === null ? null : at + TTL;
    if (this.sql("SELECT id FROM unreleased LIMIT 1").length) wanted = Math.min(wanted ?? Infinity, Date.now() + 60000);
    if (!this.meta("registered")) {
      const pexp = Number(this.meta("pexp") || 0);
      if (pexp) wanted = Math.min(wanted ?? Infinity, pexp);
      else if (this.meta("pending")) wanted = Math.min(wanted ?? Infinity, Date.now() + 60000); // Retry dropAt.
    }
    if (await this.ctx.storage.getAlarm() !== wanted) {
      if (wanted === null) await this.ctx.storage.deleteAlarm(); else await this.ctx.storage.setAlarm(wanted);
    }
  }
  private async expire(now = Date.now()) {
    const rows = this.sql<Row>("DELETE FROM queue WHERE at<=? RETURNING *", now - TTL);
    this.sql("DELETE FROM acked WHERE at<=?", now - 600000);
    if (rows.length || this.sql("SELECT id FROM unreleased LIMIT 1").length) { await this.release(rows.map(r => r.id)); await this.schedule(); }
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
  // Removes what this pending install sent to owner, as Go does when it expires or moves.
  private async dropAt(owner: string) {
    try { await this.env.MAILBOX.getByName(owner).dropFrom(this.meta("self") || ""); return true; } catch { return false; }
  }
  // Members always; a pending install (G7) until its expiry, after which its mail both ways is dropped.
  // 'pending' outlives 'pexp' until the owner's side is cleaned; the alarm retries every minute.
  private async live(now = Date.now()) {
    if (this.meta("registered")) return true;
    const pexp = Number(this.meta("pexp") || 0);
    if (pexp && now < pexp) return true;
    if (pexp) {
      const rows = this.sql<Row>("DELETE FROM queue RETURNING *");
      this.sql("DELETE FROM acked"); this.sql("DELETE FROM meta WHERE k='pexp'"); this.wake(empty);
      await this.release(rows.map(r => r.id));
    }
    const owner = this.meta("pending");
    if (owner && await this.dropAt(owner)) this.sql("DELETE FROM meta WHERE k='pending'");
    if (this.meta("pending") || this.sql("SELECT id FROM unreleased LIMIT 1").length) await this.schedule();
    else { await this.ctx.storage.deleteAll(); this.ready = false; await this.ctx.storage.deleteAlarm(); }
    return false;
  }
  registered(pub: number[]) { return this.ctx.blockConcurrencyWhile(async () => {
    this.ctx.storage.sql.exec(TABLES);
    this.ready = true; await this.expire(); this.set("pub", JSON.stringify(pub)); this.set("registered", 1);
    this.sql("DELETE FROM meta WHERE k IN ('pending','pexp')"); this.touch(); await this.schedule(); }); }
  // Claiming a code makes this a pending install of owner; moving to another owner drops the old owner's mail.
  pend(self: string, pub: number[], owner: string, expires: number) { return this.ctx.blockConcurrencyWhile(async () => {
    if (this.ready) {
      if (this.meta("registered")) return;
      if (this.meta("pending") === owner && this.meta("pexp") === String(expires)) return; // Retried claim: no writes.
      await this.live(); // An expired previous lifetime is cleaned up first.
    }
    this.ctx.storage.sql.exec(TABLES); this.ready = true;
    const old = this.meta("pending");
    if (old && old !== owner) {
      await this.release(this.sql<Row>("DELETE FROM queue WHERE sender=? RETURNING *", old).map(r => r.id));
      if (!await this.dropAt(old)) throw Error("old owner unreachable"); // The claim retry tries again.
    }
    this.set("self", self); this.set("pub", JSON.stringify(pub)); this.set("pending", owner); this.set("pexp", expires); this.touch(); await this.schedule();
  }); }
  dropFrom(sender: string) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready || !sender) return;
    const rows = this.sql<Row>("DELETE FROM queue WHERE sender=? RETURNING *", sender);
    if (rows.length) { await this.release(rows.map(r => r.id)); await this.schedule(); }
  }); }
  idle(now: number) { return this.ctx.blockConcurrencyWhile(async () => { if (!this.ready) return {seen: 0, empty: true}; await this.expire(now); return {seen: Number(this.meta("seen") || 0), empty: !this.sql("SELECT id FROM queue LIMIT 1").length}; }); }
  forget(now: number) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return true;
    await this.expire(now);
    if (now - Number(this.meta("seen") || 0) <= 30 * 86400000 || this.sql("SELECT id FROM queue UNION ALL SELECT id FROM unreleased LIMIT 1").length) return false;
    this.wake(empty); await this.ctx.storage.deleteAll(); this.ready = false; await this.ctx.storage.deleteAlarm(); return true;
  }); }
  alarm() { return this.ctx.blockConcurrencyWhile(async () => { if (this.ready) { await this.expire(); await this.live(); if (this.ready) await this.schedule(); } }); }
  enqueue(data: string, sender: string, id: string, size: number, now: number) {
    return this.ctx.blockConcurrencyWhile(async () => {
      if (!this.ready) return error(404, "unknown recipient");
      await this.expire(now);
      if (!this.meta("registered")) {
        if (!(await this.live(now))) return error(404, "unknown recipient");
        if (this.meta("pending") !== sender) return error(403, "pending recipient belongs to another owner");
      }
      if (this.sql("SELECT id FROM queue WHERE id=? UNION ALL SELECT id FROM acked WHERE id=?", id, id).length) return result(200, {status: "duplicate"});
      const rows = this.sql<Row>("SELECT * FROM queue"), pair = rows.filter(r => r.sender === sender);
      if (rows.length >= 512 || pair.length >= 64 || rows.reduce((n, r) => n + r.size, size) > 8 * 1048576 || pair.reduce((n, r) => n + r.size, size) > 1048576) return error(507, "queue full");
      const seq = Number(this.meta("seq") || 0) + 1; this.set("seq", seq);
      // Occupy the slot synchronously before the cross-object reservation; the gate hides pending mail.
      this.sql("INSERT INTO queue VALUES(?,?,?,?,?,?)", id, sender, now, seq, size, data);
      let reserved: boolean;
      try { reserved = await this.env.DIRECTORY.getByName("global").reserve(id, sender, size); }
      catch (e) { // The reservation may have landed: undo it by id (kept for retry if that fails too).
        this.sql("DELETE FROM queue WHERE id=?", id); await this.release([id]); await this.schedule(); throw e;
      }
      if (!reserved) { this.sql("DELETE FROM queue WHERE id=?", id); return error(507, "queue full"); }
      await this.schedule(); this.wake(this.list()); return result(202, {status: "queued"});
    });
  }
  ack(raw: string) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return error(403, "not registered");
    await this.expire();
    if (!(await this.live())) return error(403, "not registered");
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
    if (removed.length) { await this.release(removed.map(r => r.id)); await this.schedule(); }
    return result(200, {acked: removed.length});
  }); }
  fetch(r: Request) { return this.ctx.blockConcurrencyWhile(async () => {
    if (!this.ready) return jsonResponse(error(403, "not registered"));
    await this.expire();
    if (!(await this.live())) return jsonResponse(error(403, "not registered"));
    this.touch(); this.wake(empty);
    const text = this.list(), wait = waitSeconds(new URL(r.url).searchParams.get("wait"));
    if (text !== empty || !wait) return new Response(text, {headers: {"Content-Type": "application/json"}});
    const pair = new WebSocketPair(); this.ctx.acceptWebSocket(pair[1]);
    return new Response(null, {status: 101, webSocket: pair[0]});
  }); }
  webSocketClose() {}
  webSocketError() {}
}
