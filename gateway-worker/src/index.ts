import { authenticate, envelope } from "./wire";
import type { Mailbox } from "./mailbox";
import type { Directory } from "./directory";
export { Mailbox } from "./mailbox";
export { Directory } from "./directory";
export interface Env { MAILBOX: DurableObjectNamespace<Mailbox>; DIRECTORY: DurableObjectNamespace<Directory>; MAX_INSTALLS: string; REGISTER_TOKEN?: string }
export type Result = {status: number; value: unknown};
export const result = (status: number, value: unknown): Result => ({status, value});
export const error = (status: number, message: string) => result(status, {error: message});
export const jsonResponse = (r: Result) => new Response(JSON.stringify(r.value) + "\n", {status: r.status, headers: {"Content-Type": "application/json"}});
export const empty = '{"envelopes":[]}\n';
export function waitSeconds(s: string | null) {
  if (!s || !/^[+-]?\d+$/.test(s)) return 25;
  const n = BigInt(s);
  return n < -(1n << 63n) || n >= 1n << 63n ? 25 : Number(n < 0n ? 0n : n > 30n ? 30n : n);
}
export default {
  async fetch(r: Request, env: Env): Promise<Response> {
    const u = new URL(r.url), method = ({"/healthz": "GET", "/v1/register": "POST", "/v1/send": "POST", "/v1/poll": "GET", "/v1/ack": "POST", "/v1/code": "POST", "/v1/code/cancel": "POST", "/v1/code/claim": "POST", "/v1/code/admit": "POST"} as Record<string, string>)[u.pathname];
    const plain = (text: string, status: number, headers = {}) => new Response(text, {status, headers: {"Content-Type": "text/plain; charset=utf-8", ...headers}});
    if (typeof method !== "string") return plain("404 page not found\n", 404);
    if (r.method !== method && !(method === "GET" && r.method === "HEAD")) return plain("Method Not Allowed\n", 405, {Allow: method === "GET" ? "GET, HEAD" : method});
    if (u.pathname === "/healthz") return plain(r.method === "HEAD" ? "" : "ok", 200);
    const fail = (status: number, msg: string) => jsonResponse(error(status, msg));
    if (Number(r.headers.get("Content-Length")) > 65536) return fail(413, "body too large");
    const chunks: Uint8Array[] = []; let length = 0;
    const reader = r.body?.getReader();
    try { if (reader) for (;;) {
      const {done, value} = await reader.read(); if (done) break;
      length += value.length; if (length > 65536) { await reader.cancel(); return fail(413, "body too large"); } chunks.push(value);
    } } catch { return fail(413, "body too large"); }
    const body = new Uint8Array(length); let offset = 0;
    for (const c of chunks) { body.set(c, offset); offset += c.length; }
    let auth;
    try { auth = await authenticate(r, body, Date.now()); } catch (e) { return fail(401, (e as Error).message); }
    const {caller, pub} = auth, dir = env.DIRECTORY.getByName("global"), box = env.MAILBOX.getByName(caller), raw = new TextDecoder().decode(body);
    const ip = r.headers.get("CF-Connecting-IP") || "";
    if (u.pathname === "/v1/register") return jsonResponse(await dir.register(caller, Array.from(pub), r.headers.get("Authorization"), ip));
    if (u.pathname === "/v1/code") return jsonResponse(await dir.codeRegister(caller, raw, ip));
    if (u.pathname === "/v1/code/cancel") return jsonResponse(await dir.codeCancel(caller, raw));
    if (u.pathname === "/v1/code/claim") return jsonResponse(await dir.codeClaim(caller, Array.from(pub), raw, ip));
    if (u.pathname === "/v1/code/admit") return jsonResponse(await dir.codeAdmit(caller, raw));
    if (u.pathname === "/v1/send") {
      let e;
      try { e = await envelope(raw, caller, pub); } catch { const known = await dir.status(caller) !== ""; return fail(known ? 400 : 403, known ? "bad envelope" : "not registered"); }
      const check = await dir.check(caller, e.to, e.size > 65536);
      if (check) return jsonResponse(check);
      if (e.size > 65536) return fail(413, "envelope too large");
      return jsonResponse(await env.MAILBOX.getByName(e.to).enqueue(e.json, caller, e.id, e.size, Date.now()));
    }
    if (u.pathname === "/v1/ack") return jsonResponse(await box.ack(raw));
    const wait = waitSeconds(u.searchParams.get("wait"));
    const response = await box.fetch(`https://mailbox/poll?wait=${wait}`, {headers: {Upgrade: "websocket"}});
    if (response.status !== 101) return response;
    const ws = response.webSocket!; ws.accept();
    const text = await new Promise<string>(resolve => {
      let finished = false;
      const finish = (s = empty) => { if (finished) return; finished = true; clearTimeout(timer); resolve(s); try { ws.close(1000); } catch {} };
      const timer = setTimeout(() => finish(), wait * 1000);
      ws.addEventListener("message", e => finish(typeof e.data === "string" ? e.data : empty));
      ws.addEventListener("close", () => finish()); ws.addEventListener("error", () => finish());
      r.signal.addEventListener("abort", () => finish(), {once: true});
    });
    return new Response(r.method === "HEAD" ? null : text, {headers: {"Content-Type": "application/json"}});
  }
} satisfies ExportedHandler<Env>;
