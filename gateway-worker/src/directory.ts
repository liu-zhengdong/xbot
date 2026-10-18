import { DurableObject } from "cloudflare:workers";
import { error, result, type Env } from "./index";
import { enc, hash, hex } from "./wire";
export class Directory extends DurableObject<Env> {
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS installs(id TEXT PRIMARY KEY,pub TEXT,seen INTEGER); CREATE TABLE IF NOT EXISTS bytes(sender TEXT PRIMARY KEY,n INTEGER); CREATE TABLE IF NOT EXISTS res(id TEXT PRIMARY KEY,sender TEXT,n INTEGER); CREATE TABLE IF NOT EXISTS registrations(day TEXT,k TEXT PRIMARY KEY,n INTEGER); CREATE TABLE IF NOT EXISTS regkey(day TEXT PRIMARY KEY,key BLOB)");
  }
  private sql<T extends Record<string, SqlStorageValue>>(q: string, ...args: SqlStorageValue[]) { return this.ctx.storage.sql.exec<T>(q, ...args).toArray(); }
  // Liveness is written at most hourly; eviction only looks at installs unseen for 30 days.
  isRegistered(id: string) {
    const now = Date.now();
    this.sql("UPDATE installs SET seen=? WHERE id=? AND seen<?", now, id, now - 3600000);
    return this.sql("SELECT id FROM installs WHERE id=?", id).length > 0;
  }
  check(caller: string, to?: string) {
    if (!this.isRegistered(caller)) return error(403, "not registered");
    if (to && !this.sql("SELECT id FROM installs WHERE id=?", to).length) return error(404, "unknown recipient");
    return null;
  }
  // Per-IP counters are keyed by HMAC under a random key that lives only for that UTC day.
  private purge(day: string) { this.sql("DELETE FROM registrations WHERE day<?", day); this.sql("DELETE FROM regkey WHERE day<?", day); }
  private async ipKey(day: string, ip: string) {
    let key = this.sql<{key: ArrayBuffer}>("SELECT key FROM regkey WHERE day=?", day)[0]?.key;
    if (!key) {
      key = crypto.getRandomValues(new Uint8Array(32)).buffer as ArrayBuffer;
      this.sql("INSERT OR IGNORE INTO regkey VALUES(?,?)", day, key);
      key = this.sql<{key: ArrayBuffer}>("SELECT key FROM regkey WHERE day=?", day)[0].key;
      await this.ctx.storage.setAlarm(Date.parse(day + "T00:00:00Z") + 86400000 + 60000);
    }
    const k = await crypto.subtle.importKey("raw", key, {name: "HMAC", hash: "SHA-256"}, false, ["sign"]);
    return hex(new Uint8Array(await crypto.subtle.sign("HMAC", k, enc.encode(ip)))).slice(0, 32);
  }
  alarm() { this.purge(new Date().toISOString().slice(0, 10)); }
  // Not blockConcurrencyWhile: Mailbox calls made from here may call release() back on this object.
  async register(id: string, pub: number[], authorization: string | null, ip: string) {
    const day = new Date().toISOString().slice(0, 10);
    this.purge(day);
    if (this.isRegistered(id)) { await this.env.MAILBOX.getByName(id).registered(pub); return result(200, {status: "ok"}); }
    if (this.env.REGISTER_TOKEN) {
      if (!authorization?.startsWith("Bearer ")) return error(403, "registration closed");
      const [a, b] = await Promise.all([hash(enc.encode(authorization.slice(7))), hash(enc.encode(this.env.REGISTER_TOKEN))]);
      let diff = 0; for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
      if (diff) return error(401, "bad token");
    }
    const k = await this.ipKey(day, ip);
    const limited = () => (this.sql<{n: number}>("SELECT n FROM registrations WHERE k=?", k)[0]?.n || 0) >= 30;
    if (limited()) return error(429, "too many registrations");
    const now = Date.now(), cap = Number(this.env.MAX_INSTALLS || "1000"), stale = now - 30 * 86400000;
    const count = () => this.sql<{n: number}>("SELECT COUNT(*) AS n FROM installs")[0].n;
    // Up to 256 candidates in batches; each scan copies the Mailbox's liveness back, so later scans move on.
    scan: for (let batch = 0; batch < 4 && count() >= cap; batch++) {
      const rows = this.sql<{id: string}>("SELECT id FROM installs WHERE seen<? ORDER BY seen LIMIT 64", stale);
      if (!rows.length) break;
      for (const row of rows) {
        const box = this.env.MAILBOX.getByName(row.id), state = await box.idle(now);
        if (state.empty && now - state.seen > 30 * 86400000 && await box.forget(now)) {
          // Conditional: a re-registration that interleaved here refreshed seen and keeps its row.
          this.sql("DELETE FROM installs WHERE id=? AND seen<?", row.id, stale); break scan;
        }
        this.sql("UPDATE installs SET seen=MAX(seen,?) WHERE id=?", Math.max(state.seen, stale + 1), row.id);
      }
    }
    if (!this.isRegistered(id)) {
      if (limited()) return error(429, "too many registrations"); // Rechecked after the awaits above.
      if (count() >= cap) return error(403, "too many installs");
      this.sql("INSERT INTO registrations VALUES(?,?,1) ON CONFLICT(k) DO UPDATE SET n=n+1", day, k);
      this.sql("INSERT INTO installs VALUES(?,?,?)", id, JSON.stringify(pub), now);
    }
    await this.env.MAILBOX.getByName(id).registered(pub);
    return result(200, {status: "ok"});
  }
  // Reservations are keyed by envelope id, so a retried reserve or release after a lost reply is a no-op.
  reserve(id: string, sender: string, size: number) {
    if (this.sql("SELECT id FROM res WHERE id=?", id).length) return true;
    const rows = this.sql<{sender: string; n: number}>("SELECT * FROM bytes");
    if (rows.reduce((n, r) => n + r.n, size) > 256 * 1048576 || (rows.find(r => r.sender === sender)?.n || 0) + size > 16 * 1048576) return false;
    this.sql("INSERT INTO res VALUES(?,?,?)", id, sender, size);
    this.sql("INSERT INTO bytes VALUES(?,?) ON CONFLICT(sender) DO UPDATE SET n=n+excluded.n", sender, size); return true;
  }
  release(ids: string[]) {
    for (const id of ids) for (const r of this.sql<{sender: string; n: number}>("DELETE FROM res WHERE id=? RETURNING sender,n", id)) this.sql("UPDATE bytes SET n=MAX(0,n-?) WHERE sender=?", r.n, r.sender);
    this.sql("DELETE FROM bytes WHERE n=0");
  }
}
