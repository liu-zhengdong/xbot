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
  const brands = Object.assign(Object.create(null), { deepseek: "DeepSeek", openai: "OpenAI", anthropic: "Anthropic", openrouter: "OpenRouter" });
  const mounts = new WeakMap();
  function glyph(brand) {
    if (!brands[brand]) {
      const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("fill", "none"); svg.setAttribute("stroke", "currentColor"); svg.setAttribute("stroke-width", "1.8");
      const path = document.createElementNS("http://www.w3.org/2000/svg", "path"); path.setAttribute("d", "m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5"); svg.append(path); svg.classList.add("provider-glyph"); svg.setAttribute("aria-hidden", "true"); return svg;
    }
    const img = node("img", "provider-glyph provider-glyph-" + brand); img.src = `/provider-icons/${brand}.svg`; img.alt = ""; return img;
  }
  function lineIcon(kind) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24"); svg.setAttribute("fill", "none"); svg.setAttribute("stroke", "currentColor"); svg.setAttribute("stroke-width", "1.8"); svg.setAttribute("stroke-linecap", "round"); svg.setAttribute("stroke-linejoin", "round"); svg.setAttribute("aria-hidden", "true");
    const paths = { back: "m15 18-6-6 6-6", search: "M21 21l-4.4-4.4M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0", vision: "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Zm13 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0" };
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path"); path.setAttribute("d", paths[kind]); svg.append(path); return svg;
  }
  async function settings({ api, host, isCurrent = () => true }) {
    const mount = {}; mounts.set(host, mount);
    const alive = () => mounts.get(host) === mount && isCurrent();
    let data, query = "", selected = "", revision = 0;
    const key = p => p.name ?? p.brand;
    function entries() {
      const rows = data.providers.filter(p => p.oauth !== "chatgpt").map(p => {
        const brand = p.brand || (brands[p.name.toLowerCase()] ? p.name.toLowerCase() : "");
        return { ...p, brand, title: p.name && p.name.toLowerCase() !== brand ? p.name : brands[brand] || pname(p) };
      });
      for (const brand of Object.keys(brands)) if (!rows.some(p => p.brand === brand)) rows.push({ brand, title: brands[brand], key_configured: false, protocol: {openai:"responses",anthropic:"anthropic_messages"}[brand] || "chat_completions" });
      for (const p of rows) if (p.name === "" && rows.some(other => other !== p && other.brand === p.brand)) p.title += "（默认）";
      const order = Object.keys(brands);
      return rows.sort((a, b) => (order.indexOf(a.brand) < 0 ? 99 : order.indexOf(a.brand)) - (order.indexOf(b.brand) < 0 ? 99 : order.indexOf(b.brand)));
    }
    function state(message, retry) {
      const box = node("div", "provider-empty"); box.setAttribute("role", "status"); box.append(node("p", "", message));
      if (retry) box.append(button("重试", "btn", retry)); return box;
    }
    function section(title, subtitle) {
      const part = node("section", "provider-section"), h = node("h3", "", title);
      part.append(h, node("p", "provider-section-note", subtitle)); return part;
    }
    function paint(focusKey) {
      if (!alive()) return; ++revision;
      host.replaceChildren(node("h2", "provider-title", "模型与供应商"), node("p", "setlead", "管理密钥与可用模型。每个 Bot 绑定一对「供应商 + 模型」。"));
      const tools = node("div", "provider-tools"), search = node("label", "provider-search"); search.append(lineIcon("search"));
      const input = node("input"); input.type = "search"; input.placeholder = "查找供应商…"; input.setAttribute("aria-label", "查找供应商"); input.value = query; search.append(input); tools.append(search); host.append(tools);
      const subscription = section("订阅", "登录即可，无需密钥"), subgrid = node("div", "provider-grid");
      const chatgpt = button("", "provider-card provider-subscription", null); chatgpt.disabled = true;
      chatgpt.append(node("span", "provider-glyph provider-chatgpt-glyph"), node("strong", "", "ChatGPT"), node("span", "provider-later", "后做")); subgrid.append(chatgpt); subscription.append(subgrid);
      const vendors = section("供应商", "模型厂商的 API"), grid = node("div", "provider-grid"); vendors.append(grid); host.append(subscription, vendors);
      function filter() {
        const term = query.trim().toLowerCase(); subscription.hidden = !"chatgpt".includes(term); grid.replaceChildren();
        for (const p of entries().filter(p => p.title.toLowerCase().includes(term))) {
          const card = button("", "provider-card", () => { selected = key(p); return capability(p); }); card.setAttribute("aria-current", String(selected === key(p))); card.append(glyph(p.brand), node("strong", "", p.title));
          if (p.key_configured) { const dot = node("span", "sdot ok"); dot.setAttribute("role", "img"); dot.setAttribute("aria-label", "已配置"); card.append(dot); }
          grid.append(card); if (focusKey === key(p)) { card.focus(); focusKey = undefined; }
        }
        if (!grid.children.length) grid.append(state("没有匹配的供应商"));
      }
      input.oninput = () => { query = input.value; filter(); }; filter();
    }
    async function capability(p, refresh = false) {
      if (!alive()) return; const request = ++revision;
      const current = () => alive() && request === revision;
      const back = button("", "provider-back", () => paint(key(p))); back.append(lineIcon("back"), node("span", "", "模型与供应商"));
      const heading = node("div", "provider-detail-heading"), info = node("div", "provider-detail-info"), title = node("div", "provider-detail-title"); title.append(node("h2", "", p.title), badge(p.protocol));
      const status = node("div", "provider-status"); info.append(title, status); heading.append(glyph(p.brand), info);
      const body = node("div", "provider-models"); host.replaceChildren(back, heading, body); heading.tabIndex = -1; heading.focus({preventScroll:true});
      function keyStatus() {
        status.replaceChildren(node("span", "sdot " + (p.key_configured ? "ok" : "off")), node("span", "", p.key_configured ? (p.oauth ? "已登录" : "密钥已配置") : "密钥未配置"));
      }
      keyStatus(); body.append(state("正在拉取模型…"));
      try {
        if (refresh) { data = await api("/api/providers"); if (!current()) return; p = entries().find(row => key(row) === key(p) || (p.name == null && row.brand === p.brand)) || p; title.replaceChildren(node("h2", "", p.title), badge(p.protocol)); keyStatus(); }
        if (!p.key_configured) {
          body.replaceChildren(state(p.api_key_env ? `请先配置 ${p.api_key_env}，再拉取模型。` : "请先通过 xbot provider add 配置供应商和密钥，再拉取模型。", () => capability(p, true))); return;
        }
        const result = await api(`/api/providers/models?provider=${encodeURIComponent(p.name)}`);
        if (!current()) return;
        const models = result.models;
        body.replaceChildren(node("h3", "provider-model-count", `模型 · ${models.length}`));
        if (!models.length) { body.append(state("供应商未返回可用模型。", () => capability(p, true))); return; }
        if (models.some(m => m.hosted_web_search)) status.append(node("span", "provider-search-note", "有 hosted 时本绑定不再挂本地 web_search"));
        const rows = node("div", "provider-model-list");
        for (const m of models) {
          const row = node("div", "provider-model-row"), chips = node("div", "provider-chips");
          for (const [kind, supported, label] of [["search", m.hosted_web_search, "hosted"], ["vision", m.vision, "vision"]]) {
            const chip = node("span", "provider-chip " + (supported ? "supported" : "unsupported")); if (supported) chip.append(lineIcon(kind)); chip.append(node("span", "", (supported ? "" : "无 ") + label)); chips.append(chip);
          }
          const context = m.context >= 1000000 ? `${+(m.context / 1000000).toFixed(2)}M` : `${+(m.context / 1000).toFixed(1)}K`;
          const windowChip = node("span", "provider-chip provider-window", context); windowChip.title = `${m.context.toLocaleString()} tokens`; chips.append(windowChip);
          row.append(node("strong", "provider-model-name", m.model), chips); rows.append(row);
        }
        body.append(rows);
      } catch { if (current()) body.replaceChildren(state("拉取模型失败，请检查供应商配置和网络。", () => capability(p, true))); }
    }
    async function load() {
      host.replaceChildren(state("正在读取供应商…"));
      try { data = await api("/api/providers"); if (!alive()) return; selected = key(entries().find(p => p.key_configured) || entries()[0]); paint(); }
      catch { if (alive()) host.replaceChildren(state("读取供应商失败。", load)); }
    }
    await load();
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
            if (!m.available || !p.key_configured) {b.disabled = true;b.append(node("small", "", !m.available ? "该协议后做" : p.oauth ? "请先重新登录" : "请先配置 " + p.api_key_env));}
            options.push(b);group.append(b);
            if (p.name === (data.provider || "") && m.model === data.model && !b.disabled) choose(p,m,b);
          }
        }
      }
    } catch (err) {error.textContent = err.message || String(err);}
  }
  window.XBotProviders = { open, settings, decorate, citedText, progress };
})();
