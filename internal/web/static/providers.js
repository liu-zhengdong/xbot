// Provider surfaces read the same runtime capability table used by bindings.
(() => {
  const labels = { chat_completions: "Completions", responses: "Responses", anthropic_messages: "Messages" };
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (text, cls, click) => { const n = node("button", cls, text); n.type = "button"; n.onclick = click; return n; };
  const badge = (protocol) => node("span", "provider-badge " + protocol, labels[protocol] || protocol);
  const pname = p => p.name;
  const ctxShort = c => c >= 1000000 ? `${+(c / 1000000).toFixed(2)}M` : `${+(c / 1000).toFixed(1)}K`;
  function safeURL(value) { try { const u = new URL(value); return ["http:", "https:"].includes(u.protocol) ? u.href : null; } catch { return null; } }
  function step(part) {
    const h = part.hosted;
    const running = !["completed", "failed"].includes(h.status);
    const n = node("div", "hs-step" + (running ? " running" : ""));
    n.dataset.hostedId = h.id;
    n.setAttribute("role", "status");
    const verb = running ? "正在搜索" : h.status === "failed" ? "搜索失败" : "已搜索";
    const query = String(h.query || "");
    const label = node("span", "", verb + (query ? "「" + query + "」" : ""));
    label.title = query;
    n.append(node("span", "hs-dot"), label, node("small", "", "厂商搜索 · 不进任务"));
    return n;
  }
  function progress(host, part) {
    if (!host || !part.hosted || part.hosted.name === "image_generation") return;
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
    const hs = (parts || []).filter(p => p.hosted && p.hosted.name !== "image_generation");
    for (const p of hs.slice().reverse()) host.prepend(step(p));
    const cs = citations(parts); if (!cs.length) return;
    for (let i=0;i<cs.length;i++) {
      const url=safeURL(cs[i].citation.url);
      const a=[...host.querySelectorAll("a")].find(a=>a.href===url && a.textContent===String(i+1));
      if (a) {a.textContent=`[${i+1}]`;a.classList.add("hs-cite-mark");a.setAttribute("aria-label",`来源 ${i+1}`);}
    }
    const list = node("ol", "hs-cites");
    for (const p of cs) { const c = p.citation, url = safeURL(c.url), li = node("li"), a = node("a", "", c.title || url); a.href = url; a.target = "_blank"; a.rel = "noopener noreferrer"; a.append(node("small", "", new URL(url).hostname)); li.append(window.XBotLinkIcon(a)); list.append(li); }
    host.append(list);
  }
  const brands = Object.assign(Object.create(null), {
    deepseek: "DeepSeek", openai: "OpenAI", anthropic: "Anthropic", openrouter: "OpenRouter",
    gemini: "Google Gemini", vertex: "Vertex AI", xai: "xAI", kimi: "Kimi", "kimi-code": "Kimi Code", zhipu: "Zhipu GLM", zai: "Z.ai",
    minimax: "MiniMax", stepfun: "StepFun", mimo: "Xiaomi MiMo", qianfan: "Baidu Qianfan", tencent: "Tencent Cloud"
  });
  // The picker's group title: the vendor's brand name, ChatGPT for the
  // subscription, and the configured name only when the brand is unknown.
  const displayName = p => p.oauth === "chatgpt" ? "ChatGPT" : brands[p.brand || String(p.name).toLowerCase()] || p.name;
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
    const paths = { back: "m15 18-6-6 6-6", search: "M21 21l-4.4-4.4M19 11a8 8 0 1 1-16 0 8 8 0 0 1 16 0", image: "M4 4h16v16H4zM4 16l5-5 4 4 2-2 5 5M15 8h.01", vision: "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Zm13 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0", logout: "M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4M16 17l5-5-5-5M21 12H9" };
    const path = document.createElementNS("http://www.w3.org/2000/svg", "path"); path.setAttribute("d", paths[kind]); svg.append(path); return svg;
  }
  async function settings({ api, host, isCurrent = () => true, initialProvider = "" }) {
    const mount = {}; mounts.set(host, mount);
    const alive = () => mounts.get(host) === mount && isCurrent();
    let data, query = "", selected = "", revision = 0, loginRequest = null;
    const key = p => p.name ?? p.brand;
    const vendorOf = brand => ((data && data.vendors) || []).find(v => v.brand === brand);
    function entries() {
      const rows = data.providers.filter(p => p.oauth !== "chatgpt").map(p => {
        const brand = p.brand || (brands[p.name.toLowerCase()] ? p.name.toLowerCase() : "");
        return { ...p, brand, title: p.name && p.name.toLowerCase() !== brand ? p.name : brands[brand] || pname(p) };
      });
      for (const brand of Object.keys(brands)) if (!rows.some(p => p.brand === brand)) rows.push({ brand, title: brands[brand], key_configured: false, protocol: {openai:"responses",anthropic:"anthropic_messages"}[brand] || vendorOf(brand)?.protocol || "chat_completions" });
      const order = Object.keys(brands);
      return rows.sort((a, b) => (order.indexOf(a.brand) < 0 ? 99 : order.indexOf(a.brand)) - (order.indexOf(b.brand) < 0 ? 99 : order.indexOf(b.brand)));
    }
    function subscriptionEntry(kind = "chatgpt") {
      const p = data.providers.find(p => p.oauth === kind);
      return { ...p, name: p?.name || kind, oauth: kind, brand: kind === "chatgpt" ? "openai" : "xai", title: kind === "chatgpt" ? "ChatGPT" : "xAI", protocol: "responses" };
    }
    async function login(kind, render, done) {
      if (!alive()) return;
      const native = document.documentElement.classList.contains("in-app");
      const view = revision, current = () => alive() && view === revision;
      const paint = (state, message) => { if (current()) render(state, message); };
      function openAuthorization(attempt) {
        const popup = window.open(attempt.url, "_blank");
        if (!native && !popup) throw Error("popup blocked");
        if (popup) popup.opener = null;
        attempt.popup = popup;
      }
      if (loginRequest && !loginRequest.stopped && loginRequest.current() && loginRequest.kind === kind) {
        if (loginRequest.url) {
          try { openAuthorization(loginRequest); paint("pending", ""); }
          catch { paint("pending", "请允许弹窗后重试"); }
        }
        return;
      }
      const attempt = { kind, current }; loginRequest = attempt;
      paint("starting", "");
      try {
        // Browsers need the click gesture; the native shell opens HTTPS externally.
        if (!native) {
          attempt.popup = window.open("about:blank", "_blank");
          if (!attempt.popup) throw Error("popup blocked");
          attempt.popup.opener = null;
        }
        const start = await api(`/api/providers/oauth/${kind}/start`, { method: "POST" });
        if (!current() || attempt.stopped) { attempt.popup?.close(); return; }
        attempt.url = safeURL(start.url); if (!attempt.url) throw Error("invalid login URL");
        if (native) openAuthorization(attempt);
        else attempt.popup.location.replace(attempt.url);
        paint("pending", "");
        const expires = Date.now() + 10 * 60 * 1000;
        while (current() && Date.now() < expires) {
          if (attempt.stopped) return;
          const status = await api(`/api/providers/oauth/${kind}/status`);
          if (!current() || attempt.stopped) return;
          if (status.state === "succeeded") {
            const fresh = await api("/api/providers");
            if (!current()) return;
            data = fresh;
            const entry = kind === "chatgpt" ? subscriptionEntry() : (data.providers.find(row => row.oauth === kind) || {});
            if (!entry.key_configured) throw Error("login unavailable");
            done(); return;
          }
          if (status.state === "pending") { paint("pending", ""); await new Promise(resolve => setTimeout(resolve, 1000)); continue; }
          if (attempt.popup && !attempt.popup.closed) attempt.popup.close();
          if (status.state === "cancelled") { paint("cancelled", ""); return; }
          paint("failed", status.error || "");
          return;
        }
        if (current()) paint("failed", "登录已过期，请重试");
      } catch (err) {
        if (attempt.popup && !attempt.popup.closed) attempt.popup.close();
        // Only server-written messages are shown; upstream bodies stay hidden.
        paint("error", err && err.status ? err.message : "");
      } finally {
        if (loginRequest === attempt) loginRequest = null;
      }
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
      const subscriptions = ["chatgpt", "xai"].map(kind => {
        const subnote = node("span", "provider-login-note"); subnote.setAttribute("role", "status");
        function paintSubscription(state, message) {
          card.disabled = state === "starting";
          if (state === "starting") subnote.textContent = "正在登录…";
          else if (state === "pending") subnote.textContent = message || "等待登录，再点重开";
          else subnote.textContent = "登录未完成，请重试";
        }
        const card = button("", "provider-card provider-subscription", () => {
          const p = subscriptionEntry(kind); selected = key(p);
          return p.key_configured ? capability(p) : login(kind, paintSubscription, updateSubscription);
        });
        card.setAttribute("aria-current", String(selected === "subscription:" + kind));
        function updateSubscription() {
          const p = subscriptionEntry(kind);
          card.replaceChildren(kind === "chatgpt" ? node("span", "provider-glyph provider-chatgpt-glyph") : glyph("xai"), node("strong", "", p.title));
          if (p.key_configured) {
            const dot = node("span", "sdot ok"); dot.setAttribute("role", "img"); dot.setAttribute("aria-label", "已登录"); card.append(dot);
          } else { subnote.textContent = "登录"; card.append(subnote); }
        }
        updateSubscription(); subgrid.append(card);
        return { kind, card };
      });
      subscription.append(subgrid);
      const vendors = section("供应商", "模型厂商的 API"), grid = node("div", "provider-grid"); vendors.append(grid); host.append(subscription, vendors);
      function filter() {
        const term = query.trim().toLowerCase();
        for (const {kind, card} of subscriptions) card.hidden = !subscriptionEntry(kind).title.toLowerCase().includes(term);
        subscription.hidden = subscriptions.every(({card}) => card.hidden); grid.replaceChildren();
        for (const p of entries().filter(p => p.title.toLowerCase().includes(term))) {
          const card = button("", "provider-card", () => { selected = key(p); return capability(p); }); card.setAttribute("aria-current", String(selected === key(p))); card.append(glyph(p.brand), node("strong", "", p.title));
          if (p.key_configured) { const dot = node("span", "sdot ok"); dot.setAttribute("role", "img"); dot.setAttribute("aria-label", "已配置"); card.append(dot); }
          grid.append(card); if (focusKey === key(p)) { card.focus(); focusKey = undefined; }
        }
        if (!grid.children.length) grid.append(state("没有匹配的供应商"));
      }
      input.oninput = () => { query = input.value; filter(); }; filter();
      const focusSubscription = subscriptions.find(({kind, card}) => !card.hidden && (focusKey === "subscription:" + kind || focusKey === key(subscriptionEntry(kind))));
      if (focusSubscription) focusSubscription.card.focus();
    }
    // `opts` lets the xAI fold label its input and show the saved-key state
    // inside the fold instead of on the header row.
    function keyForm(p, vendor, saved, current, lead, opts = {}) {
      const form = node("form", "provider-key"), input = node("input"), note = node("p", "provider-login-note");
      input.type = "password"; input.autocomplete = "off"; input.spellcheck = false; input.placeholder = "粘贴密钥"; input.setAttribute("aria-label", opts.label || `${vendor.title} 密钥`);
      const save = button("保存密钥", "btn primary", () => {}); save.type = "submit"; note.setAttribute("role", "status");
      form.append(opts.label ? node("label", "provider-key-label", opts.label) : node("p", "", lead || "请先配置密钥，再拉取模型。"), input, save, note);
      if (opts.configured) { const ok = node("p", "provider-key-state"); ok.append(node("span", "sdot ok"), node("span", "", "密钥已配置 · 可用")); form.append(ok); }
      let saving = false;
      form.onsubmit = async ev => {
        ev.preventDefault(); const value = input.value.trim(); if (!value || saving) return;
        saving = true; save.disabled = input.disabled = true; note.textContent = "正在保存…";
        try { await api(`/api/providers/${encodeURIComponent(vendor.brand)}/key`, { method: "PUT", body: { key: value } }); if (!current()) return; input.value = ""; saved(); }
        catch (err) { if (!current()) return; note.textContent = err.message || "保存失败，请重试"; save.disabled = input.disabled = false; saving = false; }
      };
      return form;
    }
    // xAI account login controls. The failure line, the primary button and the
    // secondary cancel are separate blocks so their text never runs together;
    // `status(state, go, cancel)` mirrors the OAuth state into the header row.
    // A null label means the binding is logged in: idle shows nothing here and
    // re-login lives in the header row.
    function xaiAuthControls(area, label, status, done, current) {
      const fail = node("p", "provider-auth-fail"); fail.setAttribute("role", "alert");
      const actions = node("div", "provider-login-actions");
      const note = node("p", "provider-login-note"); note.setAttribute("role", "status");
      const go = () => login("xai", paint, done);
      const start = button(label || "重新登录", "provider-btn-pri", go);
      const reopen = button("再点重开", "provider-btn-ghost", go);
      const cancel = async () => {
        if (loginRequest && loginRequest.current() && loginRequest.kind === "xai") {
          loginRequest.stopped = true;
          if (loginRequest.popup && !loginRequest.popup.closed) loginRequest.popup.close();
        }
        // A repeated cancel error must repaint, or 取消 stays disabled.
        stop.disabled = true; shown = "";
        try { await api("/api/providers/oauth/xai/cancel", { method: "POST" }); }
        catch (err) { paint("error", err && err.status ? err.message : ""); return; }
        paint("cancelled", "");
      };
      const stop = button("取消", "provider-btn-sec", cancel);
      let shown = "";
      function paint(state, message) {
        // Polling repaints every second; keep the same nodes so focus stays put.
        if (!current() || shown === state + "\n" + message) return; shown = state + "\n" + message;
        start.disabled = stop.disabled = reopen.disabled = false;
        fail.textContent = ""; actions.replaceChildren(); note.textContent = "";
        if (state === "starting" || state === "pending") {
          start.textContent = "等待登录…"; start.disabled = true;
          actions.append(start); if (state === "pending") actions.append(stop, reopen);
          note.textContent = message; status("pending", go, cancel);
        } else if (state === "failed" || state === "error") {
          // Flat: one short red line; a server reason goes on the quiet note below.
          fail.textContent = "登录未完成，请重试。"; note.textContent = message;
          start.textContent = "重新登录"; actions.append(start, stop); status("failed", go, cancel);
        } else {
          start.textContent = label || "重新登录"; if (label) actions.append(start);
          // Logged in, cancel simply returns to the header row; no note.
          if (state === "cancelled" && label) note.textContent = "已取消登录，可重新开始";
          status(label ? "idle" : "logged", go, cancel);
        }
      }
      paint("idle", "");
      area.append(fail, actions, note);
      return {
        // A login may be pending or failed from an earlier view; show it once.
        sync: async () => {
          try {
            const state = await api("/api/providers/oauth/xai/status");
            if (!current() || (loginRequest && loginRequest.current() && loginRequest.kind === "xai")) return;
            if (state.state === "pending") paint("pending", "");
            else if (state.state === "failed") paint("failed", state.error || "");
          } catch {}
        }
      };
    }
    function goToXaiSubscription() {
      query = ""; selected = "subscription:xai"; paint(selected);
    }
    const xaiOccupied = () => data.providers.some(p => p.name === "xai" && p.oauth === "xai");
    function xaiVendorBody(p, vendor, current) {
      const box = node("div", "provider-xai");
      if (!xaiOccupied() && !subscriptionEntry("xai").key_configured) box.append(node("p", "provider-xai-lead", "账号登录请到上方订阅"), button("去订阅", "provider-btn-sec", goToXaiSubscription));
      const alt = node("details", "provider-key-alt");
      const fold = node("summary"); fold.append(lineIcon("back"), node("span", "", "用 API 密钥"));
      alt.append(fold, xaiOccupied()
        ? node("p", "provider-login-note", "xAI 已被账号登录占用，暂时不能保存 API 密钥。想修改密钥，请先退出登录。")
        : keyForm(p, vendor, () => capability(p, true), current, "", { label: "API 密钥", configured: p.key_configured }));
      box.append(alt);
      return box;
    }
    async function capability(p, refresh = false) {
      if (!alive()) return; const request = ++revision;
      const current = () => alive() && request === revision;
      const back = button("", "provider-back", () => paint(key(p))); back.append(lineIcon("back"), node("span", "", "模型与供应商"));
      const heading = node("div", "provider-detail-heading"), info = node("div", "provider-detail-info"), title = node("div", "provider-detail-title"); title.append(node("h2", "", p.title), badge(p.protocol));
      // xAI puts its login state on the right of the header so the column fills.
      const status = node("div", "provider-status"); heading.append(glyph(p.brand), info); info.append(title);
      if (p.brand === "xai") { heading.className += " provider-detail-heading-aside"; heading.append(status); } else info.append(status);
      const auth = node("div", "provider-auth-slot"), body = node("div", "provider-models"); host.replaceChildren(back, heading, auth, body); heading.tabIndex = -1; heading.focus({preventScroll:true});
      // xAI's header row reflects account login only; a saved API key shows
      // inside the folded key form, never here. Logged in, the list already
      // says so: the header carries only the 退出登录 icon.
      function xaiStatus(state, go, cancel) {
        if (state === "pending") status.replaceChildren(node("span", "sdot wait"), node("span", "provider-status-wait", "等待登录…"), button("取消", "provider-relink", cancel));
        else if (state === "failed") status.replaceChildren(node("span", "provider-status-fail", "登录未完成"));
        else if (state === "logged") {
          const out = button("", "provider-logout", () => xaiLogout(out)); out.title = "退出登录"; out.setAttribute("aria-label", "退出登录"); out.append(lineIcon("logout"));
          status.replaceChildren(out);
        } else status.replaceChildren(node("span", "sdot idle"), node("span", "provider-status-idle", "未登录"));
      }
      // Logging out removes the account and its binding on the server, then
      // redraws this card as the not-logged-in xAI vendor.
      async function xaiLogout(out) {
        out.disabled = true;
        try {
          await api("/api/providers/oauth/xai/logout", { method: "POST" });
          if (!current()) return;
          data = await api("/api/providers");
        } catch (err) {
          if (!current()) return;
          out.disabled = false;
          status.append(node("span", "provider-status-fail", err && err.status ? err.message : "退出失败，请重试"));
          return;
        }
        if (!current()) return;
        capability(entries().find(row => row.brand === "xai" && !row.oauth) || { brand: "xai", title: brands.xai, key_configured: false, protocol: "chat_completions" });
      }
      function keyStatus() {
        if (p.brand === "xai") {
          auth.replaceChildren();
          if (p.oauth !== "xai" && xaiOccupied()) status.replaceChildren(node("span", "provider-status-idle", "账号登录占用"));
          else xaiStatus("idle");
          if (p.oauth === "xai" && p.key_configured) {
            const done = () => capability(entries().find(row => key(row) === key(p)) || p, true);
            xaiAuthControls(auth, p.key_configured ? null : "登录", xaiStatus, done, current).sync();
          }
          return;
        }
        status.replaceChildren(node("span", "sdot " + (p.key_configured ? "ok" : "off")), node("span", "", p.key_configured ? (p.oauth ? "已登录" : "密钥已配置") : p.oauth ? "未登录" : "密钥未配置"));
        if (p.oauth === "chatgpt") {
          const note = node("span", "provider-login-note"); note.setAttribute("role", "status");
          const again = button(p.key_configured ? "重新登录" : "登录", "provider-back", () => login("chatgpt", paint, () => capability(subscriptionEntry())));
          function paint(state, message) {
            again.disabled = state === "starting";
            if (state === "starting") note.textContent = "正在登录…";
            else if (state === "pending") note.textContent = message || "等待登录，再点重开";
            else note.textContent = "登录未完成，请重试";
          }
          status.append(again, note);
        }
      }
      keyStatus(); body.append(state("正在拉取模型…"));
      try {
        if (refresh) { data = await api("/api/providers"); if (!current()) return; p = p.oauth === "chatgpt" ? subscriptionEntry() : entries().find(row => key(row) === key(p) || (p.name == null && row.brand === p.brand)) || p; title.replaceChildren(node("h2", "", p.title), badge(p.protocol)); keyStatus(); }
        // An xAI vendor card leads with account login even when an old API key
        // is configured; OAuth is the primary path and the key folds away.
        // The default endpoint has an empty name, so match on brand alone.
        const vendorEarly = p.brand === "xai" && p.oauth !== "xai" && vendorOf("xai");
        if (vendorEarly) { body.replaceChildren(xaiVendorBody(p, vendorEarly, current)); return; }
        if (!p.key_configured) {
          if (p.oauth === "chatgpt") { body.replaceChildren(state("请重新登录 ChatGPT，再拉取模型。")); return; }
          if (p.oauth === "xai") { body.replaceChildren(node("p", "provider-xai-lead", "账号登录请到上方订阅"), button("去订阅", "provider-btn-sec", goToXaiSubscription)); return; }
          const vendor = (p.name == null || p.name === p.brand) && vendorOf(p.brand);
          if (vendor && vendor.key_login) {
            body.replaceChildren(state("这家供应商需要先登录 Google Cloud，暂时不能直接填密钥使用。"));
            if (vendor.models.length) { body.append(node("h3", "provider-model-count", `默认模型 · ${vendor.models.length}`)); const rows = node("div", "provider-model-list"); for (const name of vendor.models) { const row = node("div", "provider-model-row"); row.append(node("strong", "provider-model-name", name)); rows.append(row); } body.append(rows); }
            return;
          }
          if (vendor) { body.replaceChildren(keyForm(p, vendor, () => capability(p, true), current)); return; }
          body.replaceChildren(state("请先配置密钥，再拉取模型。", () => capability(p, true))); return;
        }
        // A vendor card's models are registered as they are fetched, so what the list shows can be chosen. OAuth bindings pull their catalog read-only.
        const own = !p.oauth && p.name === p.brand && vendorOf(p.brand) && !vendorOf(p.brand).key_login;
        const result = own ? await api(`/api/providers/${encodeURIComponent(p.brand)}/sync`, { method: "POST" }) : await api(`/api/providers/models?provider=${encodeURIComponent(p.name)}`);
        if (!current()) return;
        const models = result.models;
        // xAI keeps its header to the logout icon, so the hosted note sits above the models.
        const hosted = models.some(m => m.hosted_web_search);
        body.replaceChildren();
        if (hosted && p.brand === "xai") body.append(node("p", "provider-hosted-hint", "已用厂商搜索，本绑定不再使用本地搜索"));
        body.append(node("h3", "provider-model-count", `模型 · ${models.length}`));
        if (!models.length) { body.append(state("供应商未返回可用模型。", () => capability(p, true))); return; }
        if (hosted && p.brand !== "xai") status.append(node("span", "provider-search-note", "已用厂商搜索，本绑定不再使用本地搜索"));
        const rows = node("div", "provider-model-list");
        for (const m of models) {
          const row = node("div", "provider-model-row"), chips = node("div", "provider-chips");
          for (const [kind, supported, label, none] of [["search", m.hosted_web_search, "厂商搜索", "无厂商搜索"], ["vision", m.vision, "看图", "不能看图"]]) {
            const chip = node("span", "provider-chip " + (supported ? "supported" : "unsupported")); if (supported) chip.append(lineIcon(kind)); chip.append(node("span", "", supported ? label : none)); chips.append(chip);
          }
          const windowChip = node("span", "provider-chip provider-window", ctxShort(m.context)); windowChip.title = `${m.context.toLocaleString()} tokens`; chips.append(windowChip);
          if (m.protocol === "responses" && m.registered) chips.append(imageGenControl(api, p, m));
          row.append(node("strong", "provider-model-name", m.model), chips); rows.append(row);
        }
        body.append(rows);
        if (p.oauth === "xai") {
          const alt = node("details", "provider-key-alt"), fold = node("summary");
          fold.append(lineIcon("back"), node("span", "", "用 API 密钥"));
          const keyEntry = entries().find(row => row.brand === "xai" && !row.oauth && row.name);
          alt.append(fold, node("p", "provider-login-note", keyEntry ? "已配置 API 密钥，可在 Bot 的模型设置中选择。" : "登录账号后，如需配置 API 密钥，请先退出登录。"));
          body.append(alt);
        }
      } catch (err) {
        if (!current()) return;
        // A rejected refresh is a dead credential: say so instead of a network
        // error, and point at the re-login button.
        const message = err && err.message || "";
        if (p.oauth === "xai" && message.includes("重新登录")) {
          // No header link when logged in: offer the re-login button in its place.
          const done = () => capability(entries().find(row => key(row) === key(p)) || p, true);
          auth.replaceChildren(); xaiAuthControls(auth, "重新登录", xaiStatus, done, current);
          body.replaceChildren(state("登录凭证已失效，请重新登录。"));
          return;
        }
        if (p.oauth && message.includes("重新登录")) {
          body.replaceChildren(state(p.oauth === "chatgpt" ? "ChatGPT 登录凭证已失效，请重新登录。" : "登录凭证已失效，请点「重新登录」。", () => capability(p, true)));
          return;
        }
        // A key-only xAI vendor card that still fails points back at account
        // login; an OAuth binding keeps the re-login message above.
        const vendor = p.brand === "xai" && p.oauth !== "xai" && vendorOf("xai");
        if (vendor) {
          body.replaceChildren(state("拉取模型失败，可以登录 xAI 账号后再试。", () => capability(p, true)), xaiVendorBody(p, vendor, current, xaiStatus));
          return;
        }
        body.replaceChildren(state(p.oauth === "chatgpt" ? "拉取模型失败，请重试或重新登录 ChatGPT。" : "拉取模型失败，请检查供应商配置和网络。", () => capability(p, true)));
      }
    }
    async function load() {
      host.replaceChildren(state("正在读取供应商…"));
      try {
        data = await api("/api/providers");
        if (!alive()) return;
        if (initialProvider === "xai") { selected = "subscription:xai"; paint(selected); return; }
        const target = initialProvider === "openai" ? subscriptionEntry() : null;
        selected = key(target || entries().find(p => p.key_configured) || entries()[0]);
        if (target) await capability(target); else paint();
      }
      catch { if (alive()) host.replaceChildren(state("读取供应商失败。", load)); }
    }
    await load();
  }
  // Hosted image generation is a per-model switch, apart from hosted search.
  // OpenAI takes a tool-level image model; xAI picks its own.
  function imageGenControl(api, p, m) {
    const xai = p.oauth === "xai" || p.brand === "xai";
    const wrap = node("span", "provider-chip " + (m.hosted_image_generation ? "supported" : "unsupported"));
    const box = node("input"); box.type = "checkbox"; box.checked = !!m.hosted_image_generation; box.setAttribute("aria-label", `${m.model} 厂商生图`);
    const model = node("input", "provider-image-model"); model.type = "text"; model.placeholder = "生图模型（默认）"; model.value = m.image_generation_model || ""; model.maxLength = 128; model.hidden = xai || !box.checked; model.setAttribute("aria-label", `${m.model} 生图模型`);
    const save = async () => {
      box.disabled = model.disabled = true; wrap.title = "";
      try {
        const out = await api("/api/providers/models/image-generation", { method: "PUT", body: { provider: p.name, model: m.model, enabled: box.checked, image_model: xai ? "" : model.value.trim() } });
        m.hosted_image_generation = out.hosted_image_generation; m.image_generation_model = out.image_generation_model;
      } catch (err) { wrap.title = err.message || String(err); }
      box.checked = !!m.hosted_image_generation; model.value = m.image_generation_model || ""; model.hidden = xai || !box.checked;
      wrap.className = "provider-chip " + (box.checked ? "supported" : "unsupported");
      box.disabled = model.disabled = false;
    };
    box.onchange = save; model.onchange = save;
    const label = node("label", "provider-image-toggle"); label.append(box, node("span", "", "厂商生图"));
    wrap.append(label, model);
    return wrap;
  }
  let activeDialog = null;
  // The picker says "which model does this Bot use": groups carry the vendor's
  // display name, rows a model id alongside quiet capability icons. A row click
  // only stages a choice; 使用此模型 commits it.
  async function open({ api, data, onBound, manage, opener }) {
    if (activeDialog) return;
    const dialog = node("dialog", "binding-dialog");activeDialog = dialog;dialog.tabIndex = -1;
    const close = () => { dialog.close();dialog.remove();activeDialog = null;opener?.isConnected && opener.focus(); };
    dialog.addEventListener("cancel", ev => { ev.preventDefault();if (!saving) close(); });
    dialog.addEventListener("keydown", ev => { if (ev.key === "Escape") ev.stopPropagation(); });
    dialog.addEventListener("click", ev => {
      const r = dialog.getBoundingClientRect();
      if (ev.target === dialog && !saving && (ev.clientX < r.left || ev.clientX > r.right || ev.clientY < r.top || ev.clientY > r.bottom)) close();
    });
    const head = node("div", "binding-head"), titles = node("div", "binding-titles"), sub = node("p", "binding-sub");
    const subtitle = shown => { sub.textContent = data.model ? `${data.name} · 当前 ${shown || displayName({name: data.provider})} · ${data.model}` : `${data.name} · 未绑定模型`; };
    subtitle();titles.append(node("h2", "", "更换模型"), sub);
    const search = node("input", "binding-search");search.type = "search";search.placeholder = "查找模型…";search.setAttribute("aria-label", "查找模型");search.hidden = true;
    const x = button("×", "binding-close", () => { if (!saving) close(); });x.setAttribute("aria-label", "关闭");
    head.append(titles, search, x);
    const body = node("div", "binding-body"), note = node("p", "binding-note"), error = node("p", "bad");error.setAttribute("role", "alert");
    const footer = node("div", "binding-footer");let pending = null, current = null, saving = false;
    const goManage = () => { if (!saving) {close();manage();} };
    const cancel = button("取消", "secondary", () => { if (!saving) close(); });
    const use = button("使用此模型", "primary", async () => {
      if (!pending || pending === current || saving) return; saving = true;use.disabled = cancel.disabled = true;error.textContent = "";
      const {p, m} = pending;
      try { await api(`/api/bots/${encodeURIComponent(data.name)}/binding`, {method:"PUT",body:{provider:p.name,model:m.model}});saving = false;close();await onBound({provider:p.name,display:displayName(p),model:m.model,protocol:p.protocol,hosted_web_search:!!m.hosted_web_search}); }
      catch (err) {error.textContent = err.message || String(err);}
      finally {saving=false;cancel.disabled = false;use.disabled = !pending || pending === current;}
    });use.disabled = true;
    footer.append(button("管理供应商…", "binding-manage", goManage), cancel, use);
    dialog.append(head,body,note,error,footer);document.body.append(dialog);dialog.showModal();dialog.focus();
    const flat = (text, action) => { const n = node("div", "binding-flat"); n.append(node("p", "", text)); if (action) n.append(action); return n; };
    const options = [];
    const stage = o => {
      pending = o;for (const b of options) b.setAttribute("aria-checked", String(b._o === o));
      use.disabled = o === current;
      note.textContent = o === current ? "" : current && !!current.m.hosted_web_search !== !!o.m.hosted_web_search ? (o.m.hosted_web_search ? "下一轮改用厂商搜索。旧对话还能看。" : "下一轮改用本机搜索。旧对话还能看。") : "确认后下一轮生效。";
    };
    const filter = () => {
      const term = search.value.trim().toLowerCase();let hits = 0;
      for (const g of body.querySelectorAll(".binding-section")) {
        const byName = g._title.toLowerCase().includes(term);let any = byName && !g._rows.length;
        for (const b of g._rows) { b.hidden = !!term && !byName && !b._o.m.model.toLowerCase().includes(term); if (!b.hidden) any = true; }
        g.hidden = !any;if (any) hits++;
      }
      body.querySelector(".binding-nomatch")?.remove();
      if (!hits) body.append(Object.assign(flat("没有匹配的模型"), {className: "binding-flat binding-nomatch"}));
    };
    search.oninput = filter;
    async function load() {
      body.replaceChildren(flat("正在拉模型…"));search.hidden = true;options.length = 0;
      let providers;
      try { ({providers} = await api("/api/providers")); }
      catch { if (dialog.isConnected) body.replaceChildren(flat("模型名单暂时拉不下来", button("重试", "secondary", load))); return; }
      if (!dialog.isConnected) return;
      body.replaceChildren();
      if (!providers.length) { body.append(flat("还没有可用的供应商", button("管理供应商", "primary", goManage))); return; }
      search.hidden = false;
      const order = Object.keys(labels), rank = p => order.indexOf(p.protocol) < 0 ? 99 : order.indexOf(p.protocol), lists = [];
      for (const p of providers.slice().sort((a, b) => rank(a) - rank(b))) {
        const title = displayName(p), section = node("section", "binding-section");section._title = title;section._rows = [];
        section.append(node("h3", "binding-heading", title + " · " + (labels[p.protocol] || p.protocol)));
        if (p.name === data.provider) subtitle(title);
        if (!p.key_configured || p.auth_error) section.append(button("配好密钥后再选模型", "binding-empty", goManage));
        else lists.push(models(p, section));
        body.append(section);
      }
      await Promise.all(lists);
    }
    // Rows come from the vendor's live list, not whatever providers.json
    // happens to hold; a failed list retries only that vendor.
    async function models(p, section) {
      const heading = section.firstChild;
      section.replaceChildren(heading, flat("正在拉模型…"));
      let list;
      try { ({models: list} = await api(`/api/providers/models?provider=${encodeURIComponent(p.name)}`)); }
      catch { if (dialog.isConnected) section.replaceChildren(heading, flat("模型名单暂时拉不下来", button("重试", "secondary", () => models(p, section)))); return; }
      if (!dialog.isConnected) return;
      if (!list.length) { section.replaceChildren(heading, flat("这家暂时没有可用模型")); return; }
      const group = node("div", "binding-group");group.setAttribute("role","radiogroup");group.setAttribute("aria-label",section._title);section.replaceChildren(heading, group);
      for (const m of list) {
        const o = {p, m}, b = button("", "binding-option", () => stage(o));b._o = o;b.setAttribute("role", "radio");b.setAttribute("aria-checked","false");
        const caps = node("span", "binding-cap");
        if (m.available) {
          for (const [kind, label, dim] of [["image", m.vision ? "可看图" : "不看图", !m.vision], ["search", m.hosted_web_search ? "厂商搜索" : "本机搜索", false]]) {
            const icon = node("span", "binding-cap-icon" + (dim ? " dim" : ""));
            icon.title = label;icon.setAttribute("role", "img");icon.setAttribute("aria-label", label);icon.append(lineIcon(kind));caps.append(icon);
          }
          if (m.context) { const context = node("span", "", ctxShort(m.context));context.title = `上下文窗口 ${m.context.toLocaleString()}`;context.setAttribute("aria-label", context.title);caps.append(context); }
        } else caps.textContent = "暂时用不了";
        b.append(node("strong", "", m.model), caps);
        b.disabled = !m.available;options.push(b);section._rows.push(b);group.append(b);
        if (p.name === (data.provider || "") && m.model === data.model) { current = o; if (!pending) stage(o); }
      }
      if (search.value) filter();
    }
    await load();
  }
  // The Bot's model face, shared by the details tab and the profile drawer:
  // model id over "provider display name · protocol", the whole row opens the picker.
  // info carries protocol/hosted_web_search when the catalog knows the pair.
  function trigger({ api, data, info, aside, manage, onBound }) {
    const b = button("", "binding-trigger", () => open({ api, data, manage, opener: b, onBound: async bound => {
      data.provider = bound.provider; data.model = bound.model; info = bound; paint(); await onBound?.(bound);
    } }));
    b.setAttribute("aria-haspopup", "dialog");
    const paint = () => {
      const tx = node("span", "bt-tx"), sub = [data.provider && (info?.display || displayName({name: data.provider, brand: info?.brand, oauth: info?.oauth})), labels[info?.protocol] || info?.protocol].filter(Boolean);
      if (info?.hosted_web_search) sub.push("厂商搜索");
      tx.append(node("strong", data.model ? "bt-model" : "bt-model unset", data.model || "未绑定模型"));
      if (sub.length) tx.append(node("span", "bt-sub", sub.join(" · ")));
      const chev = node("span", "skchev"); chev.setAttribute("aria-hidden", "true");
      b.replaceChildren(tx, ...(aside ? [aside] : []), chev);
      b.setAttribute("aria-label", `模型：${data.model || "未绑定"}${data.provider ? "，供应商 " + data.provider : ""}，点按更换`);
    };
    paint();
    return b;
  }
  window.XBotProviders = { open, trigger, settings, decorate, citedText, progress };
})();
