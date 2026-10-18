import { DurableObject } from "cloudflare:workers";
import { error, result, type Env } from "./index";
import { enc, hash } from "./wire";
export class Directory extends DurableObject<Env> {
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS installs(id TEXT PRIMARY KEY,pub TEXT,seen INTEGER); CREATE TABLE IF NOT EXISTS bytes(sender TEXT PRIMARY KEY,n INTEGER)");
  }
  private sql<T extends Record<string, SqlStorageValue>>(q: string, ...args: SqlStorageValue[]) { return this.ctx.storage.sql.exec<T>(q, ...args).toArray(); }
  isRegistered(id: string) {
    return this.sql("UPDATE installs SET seen=? WHERE id=? RETURNING id", Date.now(), id).length > 0;
  }
  check(caller: string, to?: string) {
    if (!this.isRegistered(caller)) return error(403, "not registered");
    if (to && !this.sql("SELECT id FROM installs WHERE id=?", to).length) return error(404, "unknown recipient");
    return null;
  }
  async register(id: string, pub: number[], authorization: string | null) {
    if (this.isRegistered(id)) { await this.env.MAILBOX.getByName(id).registered(pub); return result(200, {status: "ok"}); }
    if (this.env.REGISTER_TOKEN) {
      if (!authorization?.startsWith("Bearer ")) return error(403, "registration closed");
      const [a, b] = await Promise.all([hash(enc.encode(authorization.slice(7))), hash(enc.encode(this.env.REGISTER_TOKEN))]);
      let diff = 0; for (let i = 0; i < a.length; i++) diff |= a[i] ^ b[i];
      if (diff) return error(401, "bad token");
    }
    const now = Date.now(), cap = Number(this.env.MAX_INSTALLS || "1000");
    const count = () => this.sql<{n: number}>("SELECT COUNT(*) AS n FROM installs")[0].n;
    if (count() >= cap) for (const row of this.sql<{id: string; seen: number}>("SELECT id,seen FROM installs WHERE seen<? ORDER BY seen LIMIT 8", now - 30 * 86400000)) {
      const box = this.env.MAILBOX.getByName(row.id), state = await box.idle(now);
      if (state.empty && now - state.seen > 30 * 86400000 && await box.forget(now)) {
        this.sql("DELETE FROM installs WHERE id=?", row.id); break;
      }
      this.sql("UPDATE installs SET seen=MAX(seen,?) WHERE id=?", state.seen, row.id);
    }
    if (!this.isRegistered(id)) {
      if (count() >= cap) return error(403, "too many installs");
      this.sql("INSERT INTO installs VALUES(?,?,?)", id, JSON.stringify(pub), now);
    }
    await this.env.MAILBOX.getByName(id).registered(pub);
    return result(200, {status: "ok"});
  }
  reserve(sender: string, size: number) {
    const rows = this.sql<{sender: string; n: number}>("SELECT * FROM bytes");
    if (rows.reduce((n, r) => n + r.n, size) > 256 * 1048576 || (rows.find(r => r.sender === sender)?.n || 0) + size > 16 * 1048576) return false;
    this.sql("INSERT INTO bytes VALUES(?,?) ON CONFLICT(sender) DO UPDATE SET n=n+excluded.n", sender, size); return true;
  }
  release(bytes: Record<string, number>) {
    for (const [sender, n] of Object.entries(bytes)) this.sql("UPDATE bytes SET n=MAX(0,n-?) WHERE sender=?", n, sender);
    this.sql("DELETE FROM bytes WHERE n=0");
  }
}
