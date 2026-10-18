export const enc = new TextEncoder();
export const idRE = /^[a-f0-9]{32}$/;
export const hex = (b: Uint8Array) => Array.from(b, x => x.toString(16).padStart(2, "0")).join("");
export const hash = async (b: Uint8Array) => new Uint8Array(await crypto.subtle.digest("SHA-256", new Uint8Array(b)));
export const b64 = (b: Uint8Array) => { // Chunked: spreading ~64 KiB of arguments can hit V8's argument limit.
  let s = ""; for (let i = 0; i < b.length; i += 8192) s += String.fromCharCode(...b.subarray(i, i + 8192));
  return btoa(s);
};
export function decode(s: unknown, url = false): Uint8Array {
  // encoding/json also fills []byte from an array of integers 0-255 (fields() turns 1.0 into NaN).
  if (!url && Array.isArray(s) && s.every(x => Number.isInteger(x) && x >= 0 && x <= 255)) return Uint8Array.from(s);
  if (typeof s !== "string") throw Error();
  const clean = s.replace(/[\r\n]/g, ""); // Go's base64 decoder ignores CR and LF.
  if (url ? !/^[A-Za-z0-9_-]*$/.test(clean) || clean.length % 4 === 1 : !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(clean)) throw Error();
  const raw = atob(url ? clean.replace(/-/g, "+").replace(/_/g, "/") : clean);
  return Uint8Array.from(raw, c => c.charCodeAt(0));
}
export async function verify(pub: Uint8Array, sig: Uint8Array, message: string) {
  try {
    const key = await crypto.subtle.importKey("raw", new Uint8Array(pub), {name: "Ed25519"}, false, ["verify"]);
    return await crypto.subtle.verify("Ed25519", key, new Uint8Array(sig), enc.encode(message));
  } catch { return false; }
}
export async function authenticate(r: Request, body: Uint8Array, now: number) {
  let pub: Uint8Array;
  try { pub = decode(r.headers.get("X-Xbot-Key"), true); if (pub.length !== 32) throw Error(); }
  catch { throw Error("gateway: bad key"); }
  const t = r.headers.get("X-Xbot-Time") || "";
  let secs: bigint;
  try {
    if (!/^[+-]?\d+$/.test(t)) throw Error();
    secs = BigInt(t);
    if (secs < -(1n << 63n) || secs >= 1n << 63n) throw Error();
  } catch { throw Error("gateway: bad time"); }
  if (Math.abs(now / 1000 - Number(secs)) > 300) throw Error("gateway: bad time, server time " + new Date(now).toISOString().replace(/\.\d{3}Z$/, "Z"));
  // Go signs RequestURI(): the raw path and query, keeping a bare trailing "?". Characters that
  // WHATWG URL parsing re-encodes in the query would still differ; clients only send ?wait=N.
  const u = new URL(r.url), target = u.href.slice(u.origin.length).replace(/#.*$/, "");
  let sig: Uint8Array;
  try { sig = decode(r.headers.get("X-Xbot-Sig"), true); } catch { throw Error("gateway: bad signature"); }
  if (sig.length !== 64 || !await verify(pub, sig, `xbot-gw-v1\n${r.method}\n${target}\n${secs}\n${hex(await hash(body))}`)) throw Error("gateway: bad signature");
  return {pub, caller: hex((await hash(pub)).slice(0, 16))};
}
const ZERO = "0001-01-01T00:00:00Z";
function timestamp(s: unknown) {
  if (s === undefined || s === null) return {utc: ZERO, stored: ZERO}; // Go leaves time.Time zero.
  if (typeof s !== "string") throw Error();
  // Go's RFC 3339 parser accepts a one-digit hour and prints two.
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{1,2}):(\d{2}):(\d{2})([.,]\d+)?(Z|[+-]\d{2}:\d{2})$/.exec(s);
  if (!m) throw Error();
  const [y, mo, d, h, mi, sec] = m.slice(1, 7).map(Number);
  const date = new Date(0); date.setUTCFullYear(y, mo - 1, d); date.setUTCHours(h, mi, sec, 0);
  if (date.getUTCFullYear() !== y || date.getUTCMonth() !== mo - 1 || date.getUTCDate() !== d || h > 23 || mi > 59 || sec > 59) throw Error();
  const zone = m[8], zh = zone === "Z" ? 0 : Number(zone.slice(1, 3)), zm = zone === "Z" ? 0 : Number(zone.slice(4));
  if (zh > 23 || zm > 59) throw Error();
  date.setTime(date.getTime() - (zone[0] === "-" ? -1 : 1) * (zh * 60 + zm) * 60000);
  if (date.getUTCFullYear() < 0 || date.getUTCFullYear() > 9999) throw Error();
  const digits = (m[7] || "").slice(1, 10).replace(/0+$/, ""), fraction = digits ? "." + digits : "";
  return {utc: date.toISOString().slice(0, 19) + fraction + "Z", stored: `${m[1]}-${m[2]}-${m[3]}T${m[4].padStart(2, "0")}:${m[5]}:${m[6]}` + fraction + (zh === 0 && zm === 0 ? "Z" : zone)};
}
// encoding/json matches struct field names without regard to case (ſ folds to s, K to k);
// unknown fields are ignored. All protocol numbers are Go integers, which reject literals
// like 1.0 or 1e0: those become NaN so every integer check fails, as in Go.
type Reviver = (key: string, value: unknown, context?: {source?: string}) => unknown;
export function fields(raw: string): Record<string, unknown> | null {
  const v = (JSON.parse as (text: string, reviver: Reviver) => unknown)(raw, (_, v, c) => typeof v === "number" && c?.source !== undefined && !/^-?\d+$/.test(c.source) ? NaN : v);
  if (v === null) return null;
  if (typeof v !== "object" || Array.isArray(v)) throw Error();
  return Object.fromEntries(Object.entries(v).map(([k, v]) => [k.toLowerCase().replace(/ſ/g, "s"), v]));
}
export async function envelope(raw: string, caller: string, pub: Uint8Array) {
  const e = fields(raw);
  if (!e || e.v !== 1 || typeof e.id !== "string" || !idRE.test(e.id) || e.from !== caller || typeof e.to !== "string" || !idRE.test(e.to) || typeof e.kind !== "string" || !/^[a-z]{1,16}$/.test(e.kind)) throw Error();
  const time = timestamp(e.time), body = e.body == null ? new Uint8Array() : decode(e.body), sig = decode(e.sig);
  if (sig.length !== 64 || !await verify(pub, sig, `xbot-env-v1\n1|${e.id}|${caller}|${e.to}|${time.utc}|${e.kind}|${hex(await hash(body))}`)) throw Error();
  const json = JSON.stringify({v: 1, id: e.id, from: caller, to: e.to, time: time.stored, kind: e.kind, body: e.body == null ? null : b64(body), sig: b64(sig)});
  return {json, to: e.to, id: e.id, size: enc.encode(json).length};
}
