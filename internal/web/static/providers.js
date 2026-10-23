// Provider surfaces read the same runtime capability table used by bindings.
(() => {
  const labels = { chat_completions: "Completions", responses: "Responses", anthropic_messages: "Messages" };
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (text, cls, click) => { const n = node("button", cls, text); n.type = "button"; n.onclick = click; return n; };
  const badge = (protocol) => node("span", "provider-badge " + protocol, labels[protocol] || protocol);
  const pname = p => p.name || "默认端点";
  const detail = m => `${m.hosted_web_search ? "厂商搜索" : "本地搜索"} · ${m.vision ? "可看图" : "不看图"}${m.context ? " · " + m.context.toLocaleString() + " tokens" : ""}`;
  function safeURL(value) { try { const u = new URL(value); return ["http:", "https:"].includes(u.protocol) ? u.href : null; } catch { return null; } }
  function step(part) {
    const h = part.hosted;
    const running = !["completed", "failed"].includes(h.status);
    const n = node("div", "hs-step" + (running ? " running" : ""));
    n.dataset.hostedId = h.id;
    n.setAttribute("role", "status");
    n.append(node("span", "hs-dot"), node("span", "", (running ? "正在搜索" : h.status === "failed" ? "搜索失败" : "已搜索") + (h.query ? "「" + h.query + "」" : "")), node("small", "", "厂商搜索 · 不进任务"));
    return n;
  }
  function progress(host, part) {
    if (!host || !part.hosted) return;
    const old = [...host.querySelectorAll(".hs-step")].find(n => n.dataset.hostedId === part.hosted.id);
    if (old) { if (!part.hosted.query) part = { ...part, hosted: { ...part.hosted, query: old._query || "" } }; const n = step(part); n._query = part.hosted.query; old.replaceWith(n); }
    else { const n = step(part); n._query = part.hosted.query; host.prepend(n); }
  }
  function citations(parts) { return (parts || []).filter(p => p.citation && safeURL(p.citation.url)); }
  function citedText(text, parts) {
    const cs = citations(parts), chars = Array.from(text);
    const marks = cs.map((p, i) => ({ at: Math.min(chars.length, Math.max(0, p.citation.end)), text: ` [${i + 1}](${safeURL(p.citation.url).replace(/\(/g, "%28").replace(/\)/g, "%29")})` }));
    marks.sort((a,b) => b.at - a.at);
    for (const m of marks) chars.splice(m.at, 0, m.text);
    return chars.join("");
  }
  function decorate(host, parts) {
    const hs = (parts || []).filter(p => p.hosted);
    for (const p of hs.slice().reverse()) host.prepend(step(p));
    const cs = citations(parts); if (!cs.length) return;
    for (let i=0;i<cs.length;i++) {
      const url=safeURL(cs[i].citation.url);
      const a=[...host.querySelectorAll("a")].find(a=>a.href===url && a.textContent===String(i+1));
      if (a) {a.textContent=`[${i+1}]`;a.classList.add("hs-cite-mark");a.setAttribute("aria-label",`来源 ${i+1}`);}
    }
    const list = node("ol", "hs-cites");
    for (const p of cs) { const c = p.citation, url = safeURL(c.url), li = node("li"), a = node("a", "", c.title || url); a.href = url; a.target = "_blank"; a.rel = "noopener noreferrer"; a.append(node("small", "", new URL(url).hostname)); li.append(a); list.append(li); }
    host.append(list);
  }
  async function settings({ api, host }) {
    const data = await api("/api/providers");
    host.replaceChildren();
    const list = node("div", "provider-list"); host.append(list);
    function capability(p) {
      host.replaceChildren(button("返回供应商", "secondary", () => paint()), node("h3", "provider-heading", pname(p)), badge(p.protocol));
      const table = node("table", "provider-table"), head = node("tr");
      for (const t of ["模型", "厂商搜索", "看图", "窗口"]) head.append(node("th", "", t)); table.append(head);
      for (const m of p.models) { const row = node("tr"); for (const t of [m.model, m.hosted_web_search ? "✓" : "—", m.vision ? "✓" : "—", m.context ? m.context.toLocaleString() : "—"]) row.append(node("td", "", t)); table.append(row); }
      host.append(table);
    }
    function paint() {
      host.replaceChildren();list.replaceChildren();host.append(list);
      for (const p of data.providers) {
        const row = button("", "provider-row", () => capability(p));
        const info = node("span"); info.append(node("strong", "", pname(p)));
        const meta = node("span", "provider-meta"); meta.append(badge(p.protocol), node("span", "", (p.key_configured ? "密钥已配置" : "密钥未配置") + " · " + p.models.length + " 个模型"));
        if (p.models.some(m => m.hosted_web_search)) meta.append(node("span", "provider-hosted", "hosted 搜索"));
        info.append(meta);row.append(info, node("span", "", "查看能力"));list.append(row);
      }
      host.append(node("h4", "provider-heading", "登录方式"));
      const oauth = button("用 ChatGPT 登录（后做）", "provider-row provider-oauth");oauth.disabled = true;host.append(oauth);
    }
    paint();
  }
  let activeDialog = null;
  async function open({ api, data, onBound, manage, opener }) {
    if (activeDialog) return;
    const dialog = node("dialog", "binding-dialog");activeDialog = dialog;
    const close = () => { dialog.close();dialog.remove();activeDialog = null;opener?.isConnected && opener.focus(); };
    dialog.addEventListener("cancel", ev => { ev.preventDefault();close(); });
    dialog.append(node("h2", "", "更换绑定"), node("p", "provider-meta", `${data.name} · 当前 ${data.provider || "默认端点"} / ${data.model}`));
    const body = node("div", "binding-body"), note = node("p", "binding-note"), error = node("p", "bad");error.setAttribute("role", "alert");
    const footer = node("div", "binding-footer");let selected = null, saving = false;
    const cancel = button("取消", "secondary", () => { if (!saving) close(); });
    const use = button("使用此绑定", "primary", async () => {
      if (!selected || saving) return; saving = true;use.disabled = cancel.disabled = true;error.textContent = "";
      try { await api(`/api/bots/${encodeURIComponent(data.name)}/binding`, {method:"PUT",body:{provider:selected.p.name,model:selected.m.model}});close();await onBound(); }
      catch (err) {error.textContent = err.message || String(err);}
      finally {saving=false;use.disabled = cancel.disabled = false;}
    });use.disabled = true;
    footer.append(button("管理供应商…", "secondary", () => { if (!saving) {close();manage();} }), cancel, use);
    dialog.append(body,note,error,footer);document.body.append(dialog);dialog.showModal();
    try {
      const {providers} = await api("/api/providers");
      if (!dialog.isConnected) return;
      const options = [];
      const choose = (p,m,b) => {
        selected = {p,m};for (const o of options) o.setAttribute("aria-checked", String(o === b));
        note.textContent = m.hosted_web_search ? "将改用厂商搜索：下一轮工具列表不再含本地 web_search。旧对话仍可读。" : "将使用本地搜索：下一轮恢复本地 web_search。旧对话仍可读。";use.disabled = false;
      };
      for (const protocol of Object.keys(labels)) {
        const groups = providers.filter(p => p.protocol === protocol);
        if (!groups.length) {body.append(node("h3", "provider-heading", labels[protocol]), node("p", "provider-meta", protocol === "anthropic_messages" ? "Messages · 后做" : "尚未配置"));continue;}
        for (const p of groups) {
          body.append(node("h3", "provider-heading", pname(p) + " · " + labels[protocol]));
          const group = node("div", "binding-group");group.setAttribute("role","radiogroup");group.setAttribute("aria-label",pname(p));body.append(group);
          for (const m of p.models) {
            const b = button("", "binding-option", () => choose(p,m,b));b.setAttribute("role", "radio");b.setAttribute("aria-checked","false");
            b.append(node("strong", "", m.model));const meta = node("span", "provider-meta");meta.append(badge(protocol),node("span", "", detail(m)));b.append(meta);
            if (!m.available || !p.key_configured) {b.disabled = true;b.append(node("small", "", !m.available ? "该协议后做" : "请先配置 " + p.api_key_env));}
            options.push(b);group.append(b);
            if (p.name === (data.provider || "") && m.model === data.model && !b.disabled) choose(p,m,b);
          }
        }
      }
    } catch (err) {error.textContent = err.message || String(err);}
  }
  window.XBotProviders = { open, settings, decorate, citedText, progress };
})();
