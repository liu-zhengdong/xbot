import { DurableObject } from "cloudflare:workers";
import { error, result, type Env } from "./index";
import { enc, fields, hash, hex, idRE } from "./wire";
const ALPHABET = "23456789ABCDEFGHJKMNPQRSTVWXYZ", CODE_CLAIMS = 5, PENDING_TTL = 15 * 60000, PENDING_MAX = 16, PENDING_PER_OWNER = 5;
// Failed short-code lookups are counted per IPv6 /64, like the Go server.
function prefix(ip: string) {
  if (!ip.includes(":")) return ip;
  const [head, tail = ""] = ip.split("::"), h = head ? head.split(":") : [], t = tail ? tail.split(":") : [];
  const full = ip.includes("::") ? [...h, ...Array(Math.max(0, 8 - h.length - t.length)).fill("0"), ...t] : h;
  return full.slice(0, 4).map(x => parseInt(x || "0", 16).toString(16)).join(":") + "::/64";
}
export class Directory extends DurableObject<Env> {
  // In memory like Go: a restart forgets them, and failed lookups cost no storage writes.
  private fails: {k: string; at: number}[] = [];
  private codeTimes = new Map<string, number>();
  constructor(ctx: DurableObjectState, env: Env) {
    super(ctx, env);
    ctx.storage.sql.exec("CREATE TABLE IF NOT EXISTS installs(id TEXT PRIMARY KEY,pub TEXT,seen INTEGER); CREATE TABLE IF NOT EXISTS bytes(sender TEXT PRIMARY KEY,n INTEGER); CREATE TABLE IF NOT EXISTS res(id TEXT PRIMARY KEY,sender TEXT,n INTEGER); CREATE TABLE IF NOT EXISTS registrations(day TEXT,k TEXT PRIMARY KEY,n INTEGER); CREATE TABLE IF NOT EXISTS regkey(day TEXT PRIMARY KEY,key BLOB); CREATE TABLE IF NOT EXISTS pending(id TEXT PRIMARY KEY,owner TEXT,pub TEXT,expires INTEGER,k TEXT); CREATE TABLE IF NOT EXISTS codes(h TEXT PRIMARY KEY,owner TEXT,expires INTEGER,claims INTEGER,dead INTEGER,claimers TEXT);");
  }
  private sql<T extends Record<string, SqlStorageValue>>(q: string, ...args: SqlStorageValue[]) { return this.ctx.storage.sql.exec<T>(q, ...args).toArray(); }
  // Liveness is written at most hourly; eviction only looks at installs unseen for 30 days.
  isRegistered(id: string) {
    const now = Date.now();
    this.sql("UPDATE installs SET seen=? WHERE id=? AND seen<?", now, id, now - 3600000);
    return this.sql("SELECT id FROM installs WHERE id=?", id).length > 0;
  }
  private member(id: string) { return this.sql("SELECT id FROM installs WHERE id=?", id).length > 0; }
  // A live pending install (G7): a token-free short-code joiner that may only use its mailbox with its owner.
  private pending(id: string) {
    this.sql("DELETE FROM pending WHERE expires<=?", Date.now());
    return this.sql<{owner: string; pub: string; expires: number; k: string | null}>("SELECT owner,pub,expires,k FROM pending WHERE id=?", id)[0];
  }
  status(id: string) { return this.isRegistered(id) ? "member" : this.pending(id) ? "pending" : ""; }
  // Same order as Go: unknown sender, pending sender to someone else, then (unless too large) the recipient.
  check(caller: string, to: string, big: boolean) {
    if (!this.isRegistered(caller)) {
      const p = this.pending(caller);
      if (!p) return error(403, "not registered");
      if (to !== p.owner) return error(403, "pending may only send to owner");
    }
    if (big || this.member(to)) return null;
    const p = this.pending(to);
    if (!p) return error(404, "unknown recipient");
    return p.owner === caller ? null : error(403, "pending recipient belongs to another owner");
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
  alarm() { this.purge(new Date().toISOString().slice(0, 10)); this.sql("DELETE FROM codes WHERE expires<?", Date.now() - 3600000); }
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
    const err = await this.admit(id, pub, () => {
      if (limited()) return error(429, "too many registrations"); // Rechecked after the eviction awaits.
      this.sql("INSERT INTO registrations VALUES(?,?,1) ON CONFLICT(k) DO UPDATE SET n=n+1", day, k); return null;
    });
    if (err) return err;
    await this.env.MAILBOX.getByName(id).registered(pub);
    return result(200, {status: "ok"});
  }
  // Shared by register and admit: install cap, eviction of one stale idle install, the insert.
  private async admit(id: string, pub: number[], gate?: () => ReturnType<typeof error> | null) {
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
          if (this.sql("DELETE FROM installs WHERE id=? AND seen<? RETURNING id", row.id, stale).length) this.sql("DELETE FROM codes WHERE owner=?", row.id);
          break scan;
        }
        this.sql("UPDATE installs SET seen=MAX(seen,?) WHERE id=?", Math.max(state.seen, stale + 1), row.id);
      }
    }
    if (!this.member(id)) {
      if (count() >= cap) return error(403, "too many installs");
      const g = gate?.(); if (g) return g;
      this.sql("INSERT INTO installs VALUES(?,?,?)", id, JSON.stringify(pub), now);
    }
    this.sql("DELETE FROM pending WHERE id=?", id);
    return null;
  }
  // G7 short codes. Only the 4-character lookup reaches the gateway, and it is stored as its SHA-256.
  // A record lives one hour past expiry (or cancel) so late claims get 410, then 404.
  private input(raw: string) {
    let f: Record<string, unknown> | null;
    try { f = fields(raw); } catch { return null; }
    const lookup = f?.lookup ?? "", ttl = f?.ttl ?? 0;
    if (typeof lookup !== "string" || typeof ttl !== "number" || !Number.isInteger(ttl) || lookup.length !== 4 || [...lookup].some(c => !ALPHABET.includes(c))) return null;
    return {h: hash(enc.encode(lookup)).then(hex), ttl};
  }
  private record(h: string, now: number) {
    this.sql("DELETE FROM codes WHERE expires<?", now - 3600000);
    const r = this.sql<{owner: string; expires: number; claims: number; dead: number; claimers: string}>("SELECT owner,expires,claims,dead,claimers FROM codes WHERE h=?", h)[0];
    if (r && !this.member(r.owner)) { this.sql("DELETE FROM codes WHERE h=?", h); return undefined; }
    return r && {...r, claimers: JSON.parse(r.claimers) as string[]};
  }
  // Failed lookups (unknown, dead, full, 409): 10 a minute per client (IPv6 /64), 120 a minute in all.
  private limited(k: string, now: number) {
    this.fails = this.fails.filter(f => f.at > now - 60000);
    return this.fails.length >= 120 || this.fails.filter(f => f.k === k).length >= 10;
  }
  async codeRegister(caller: string, raw: string, ip: string) {
    if (!this.isRegistered(caller)) return error(403, "not registered");
    const inp = this.input(raw);
    if (!inp) return error(400, "bad lookup");
    if (inp.ttl < 1 || inp.ttl > 600) return error(400, "bad ttl");
    const h = await inp.h, k = prefix(ip), now = Date.now();
    for (const [o, at] of this.codeTimes) if (at <= now - 2000) this.codeTimes.delete(o);
    if (this.codeTimes.has(caller)) return error(429, "code registration too frequent");
    if (this.limited(k, now)) return error(429, "too many tries");
    this.codeTimes.set(caller, now); // A 409 uses the gap too.
    const old = this.record(h, now);
    if (old && old.owner !== caller && !old.dead && now < old.expires) { this.fails.push({k, at: now}); return error(409, "lookup taken"); }
    this.sql("DELETE FROM codes WHERE owner=?", caller);
    this.sql("INSERT OR REPLACE INTO codes VALUES(?,?,?,0,0,'[]')", h, caller, now + inp.ttl * 1000);
    return result(200, {status: "ok"});
  }
  async codeCancel(caller: string, raw: string) {
    if (!this.isRegistered(caller)) return error(403, "not registered");
    const inp = this.input(raw);
    if (!inp) return error(400, "bad lookup");
    const h = await inp.h, now = Date.now(), r = this.record(h, now);
    if (!r || r.owner !== caller) return error(404, "no such code");
    if (!r.dead) this.sql("UPDATE codes SET dead=1,expires=MIN(expires,?) WHERE h=?", now, h); // Retention runs from the cancel.
    return result(200, {status: "ok"});
  }
  async codeClaim(caller: string, pub: number[], raw: string, ip: string) {
    const inp = this.input(raw);
    if (!inp) return error(400, "bad lookup");
    const [h, rk] = await Promise.all([inp.h, this.ipKey(new Date().toISOString().slice(0, 10), ip)]), k = prefix(ip), now = Date.now();
    if (this.limited(k, now)) return error(429, "too many tries");
    const r = this.record(h, now), known = !!r?.claimers.includes(caller);
    const fail = !r ? error(404, "no such code") : r.dead || (!known && r.claims >= CODE_CLAIMS) ? error(410, "code used") : now >= r.expires ? error(410, "code expired") : null;
    if (fail || !r) { this.fails.push({k, at: now}); return fail!; }
    if (r.owner === caller) return error(400, "own code");
    let pend: {owner: string; expires: number} | undefined;
    if (!this.isRegistered(caller)) {
      const p = this.pending(caller);
      pend = p;
      if (!p || p.owner !== r.owner) {
        const live = this.sql<{owner: string}>("SELECT owner FROM pending");
        if (live.filter(q => q.owner === r.owner).length >= PENDING_PER_OWNER || (!p && live.length >= PENDING_MAX)) return error(429, "too many pending installs");
        // Re-pointing keeps the expiry, so hopping between codes never extends it.
        pend = {owner: r.owner, expires: p ? p.expires : now + PENDING_TTL};
        this.sql("INSERT OR REPLACE INTO pending VALUES(?,?,?,?,?)", caller, pend.owner, JSON.stringify(pub), pend.expires, rk);
      }
    }
    if (!known) this.sql("UPDATE codes SET claims=claims+1,claimers=? WHERE h=?", JSON.stringify([...r.claimers, caller]), h);
    if (pend) await this.env.MAILBOX.getByName(caller).pend(caller, pub, pend.owner, pend.expires);
    return result(200, {install: r.owner});
  }
  async codeAdmit(caller: string, raw: string) {
    if (!this.isRegistered(caller)) return error(403, "not registered");
    let id: unknown;
    try { id = fields(raw)?.install ?? ""; } catch { return error(400, "bad install"); }
    if (typeof id !== "string" || !idRE.test(id)) return error(400, "bad install");
    if (this.member(id)) return result(200, {status: "ok"}); // Idempotent.
    const p = this.pending(id);
    if (!p || p.owner !== caller) return error(404, "no such pending install");
    const pub = JSON.parse(p.pub) as number[], day = new Date().toISOString().slice(0, 10);
    this.purge(day);
    const err = await this.admit(id, pub, () => {
      // Rechecked after the eviction awaits: the install may have moved to another owner or expired.
      const q = this.pending(id);
      if (!q || q.owner !== caller) return error(404, "no such pending install");
      // Promotion counts against the claimant's daily new-install bucket, like open registration.
      if (q.k && (this.sql<{n: number}>("SELECT n FROM registrations WHERE k=?", q.k)[0]?.n || 0) >= 30) return error(429, "too many registrations");
      if (q.k) this.sql("INSERT INTO registrations VALUES(?,?,1) ON CONFLICT(k) DO UPDATE SET n=n+1", day, q.k);
      return null;
    });
    if (err) return err;
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
