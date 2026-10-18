// Scaffold: routing and health only. The Durable Objects that serve /v1/* land in the next PR.
const routes: Record<string, string> = {"/healthz": "GET", "/v1/register": "POST", "/v1/send": "POST", "/v1/poll": "GET", "/v1/ack": "POST"};
export default {
  async fetch(r: Request): Promise<Response> {
    const u = new URL(r.url), method = routes[u.pathname];
    const plain = (text: string, status: number, headers = {}) => new Response(text, {status, headers: {"Content-Type": "text/plain; charset=utf-8", ...headers}});
    if (typeof method !== "string") return plain("404 page not found\n", 404);
    if (r.method !== method && !(method === "GET" && r.method === "HEAD")) return plain("Method Not Allowed\n", 405, {Allow: method === "GET" ? "GET, HEAD" : method});
    if (u.pathname === "/healthz") return plain(r.method === "HEAD" ? "" : "ok", 200);
    return new Response(JSON.stringify({error: "not implemented"}) + "\n", {status: 501, headers: {"Content-Type": "application/json"}});
  }
} satisfies ExportedHandler;
