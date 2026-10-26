(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const CHIPS = [["all", "全部"], ["local", "本机"], ["dev", "开发"], ["work", "效率"], ["chat", "通讯"], ["more", "更多"]];
  const FEATURED = ["desktop", "browser", "context7"];
  // Static artwork from the signed-off design; custom apps get the plug.
  const ICONS = {
    desktop: `<svg viewBox="0 0 24 24" fill="none" stroke="#3f3f46" stroke-width="1.7" stroke-linecap="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8"/><path d="M12 16v4"/></svg>`,
    browser: `<svg viewBox="0 0 24 24" fill="none" stroke="#3b6ea5" stroke-width="1.7" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18"/><path d="M12 3a14 14 0 0 1 0 18"/><path d="M12 3a14 14 0 0 0 0 18"/></svg>`,
    fetch: `<svg viewBox="0 0 24 24" fill="none" stroke="#0f766e" stroke-width="1.7" stroke-linecap="round"><path d="M8 6h13"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M3 6h.01"/><path d="M3 12h.01"/><path d="M3 18h.01"/></svg>`,
    playwright: `<svg viewBox="0 0 24 24" fill="none" stroke="#2f9e5b" stroke-width="1.7" stroke-linecap="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="m10 9 5 3-5 3V9Z" fill="#2f9e5b" stroke="none"/></svg>`,
    context7: `<svg viewBox="0 0 24 24" fill="none" stroke="#5b5bd6" stroke-width="1.7" stroke-linecap="round"><path d="M12 3 4 7v10l8 4 8-4V7l-8-4Z"/><path d="M12 12 4 7"/><path d="m12 12 8-5"/><path d="M12 12v10"/></svg>`,
    telegram: `<svg viewBox="0 0 24 24" fill="none"><path d="M21 5 3.5 11.5 9 13.5 18 8l-8 7.5V20l3-3 5.5 3.5L21 5Z" fill="#2AABEE"/></svg>`,
    lark: `<svg viewBox="0 0 24 24" fill="none"><path d="M6 4h8l4 4v12H6V4Z" fill="#3370FF"/><path d="M14 4v4h4" fill="#60A5FA"/><path d="M9 12h6M9 16h4" stroke="#fff" stroke-width="1.5" stroke-linecap="round"/></svg>`,
    gmail: `<svg viewBox="0 0 24 24" fill="none"><path d="M3 7.5 12 13l9-5.5V18a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V7.5Z" fill="#EA4335"/><path d="M3 7.5 12 13l9-5.5L12 4 3 7.5Z" fill="#FBBC05"/><path d="M3 7.5V18l6-4.5V11L3 7.5Z" fill="#4285F4"/><path d="M21 7.5V18l-6-4.5V11l6-3.5Z" fill="#34A853"/></svg>`,
    gdrive: `<svg viewBox="0 0 24 24" fill="none"><path d="m12 3 7.5 13H4.5L12 3Z" fill="#4285F4"/><path d="M4.5 16 8 22h12l-3.5-6H4.5Z" fill="#0F9D58"/><path d="M19.5 16 16 22H8l7.5-13 4 7Z" fill="#F4B400"/></svg>`,
    gcalendar: `<svg viewBox="0 0 24 24" fill="none"><rect x="4" y="5" width="16" height="15" rx="2" fill="#fff" stroke="#1A73E8" stroke-width="1.4"/><path d="M4 9h16" stroke="#1A73E8" stroke-width="1.4"/><path d="M8 3v4M16 3v4" stroke="#1A73E8" stroke-width="1.6" stroke-linecap="round"/><text x="12" y="17" text-anchor="middle" font-size="8" font-weight="700" fill="#1A73E8" font-family="Segoe UI,sans-serif">31</text></svg>`,
  };
  const PLUG = `<svg viewBox="0 0 24 24" fill="none" stroke="#71717a" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3v5"/><path d="M15 3v5"/><path d="M6 8h12v4a6 6 0 0 1-12 0V8Z"/><path d="M12 18v3"/></svg>`;
  const icon = a => { const n = node("span", "ico"); n.setAttribute("aria-hidden", "true"); n.innerHTML = ICONS[a.id] || PLUG; return n; };
  const textOf = a => (a.title + " " + a.summary).toLowerCase();

  // The store installs for the owner, so every call here is free of any Bot.
  async function mount({api, host, isCurrent, connect = () => {}}) {
    teardown();
    let alive = true, apps = [], count = 0, chip = "all", query = "", view = "store", busy = new Set(), layer = null;
    const current = () => alive && isCurrent();
    teardown = () => { alive = false; layer = null; host.replaceChildren(); };
    host.className = "skills-settings apps-settings";
    const body = node("div", "ap-body");
    const errorBox = node("p", "sk-error"); errorBox.setAttribute("role", "alert"); errorBox.hidden = true;
    const layers = node("div", "sk-layers");
    host.replaceChildren(body, errorBox, layers);
    const fail = text => { errorBox.textContent = text || ""; errorBox.hidden = !text; };

    async function load() {
      try { const d = await api("/api/apps"); apps = d.apps || []; count = d.connected || 0; }
      catch { if (current()) fail("没能读取应用，请重新打开试试。"); return false; }
      return current();
    }

    function label(a) {
      if (a.state === "soon") return ["即将支持", "btn ap-soon", true];
      // A connected card can be taken back right here, the same as the row on the connected page.
      if (a.state === "connected") return ["移除", "btn", false];
      return [a.category === "local" || a.category === "chat" ? "连接" : "添加", "btn pri", false];
    }
    async function add(a) {
      if (busy.has(a.id)) return;
      if (a.card) { connect(a); return; }
      busy.add(a.id); fail("");
      try { await api("/api/apps", {method: "POST", body: {id: a.id}}); if (!(await load())) return; }
      catch (e) { if (current()) fail(plain(e, "没能添加，请再试一次。")); }
      finally { busy.delete(a.id); }
      if (current()) paint();
    }
    async function remove(a) {
      if (busy.has(a.id)) return;
      busy.add(a.id); fail("");
      try { await api("/api/apps/" + encodeURIComponent(a.id), {method: "DELETE"}); if (!(await load())) return; }
      catch (e) { if (current()) fail(plain(e, "没能移除，请再试一次。")); }
      finally { busy.delete(a.id); }
      if (current()) paint();
    }

    function card(a) {
      const c = node("div", "ap-card"), tx = node("div", "tx"), [text, cls, off] = label(a);
      tx.append(node("b", "", a.title), node("span", "hint", a.summary));
      if (a.scope) tx.append(node("span", "ap-scope", a.scope));
      const b = button(cls, text, () => a.state === "connected" ? remove(a) : add(a)); b.disabled = off || busy.has(a.id);
      c.append(icon(a), tx, b); return c;
    }
    function paint() { return view === "connected" ? showConnected() : showStore(); }

    function showStore() {
      view = "store";
      const head = node("div", "sethd ap-head"); head.append(node("h2", "", "连接应用"));
      const open = button("ap-count", "已连接 " + count + " 个", () => { view = "connected"; fail(""); paint(); }); open.append(node("span", "skchev"));
      head.append(open);
      const wrap = node("div", "sksearchwrap ap-search"), glyph = node("span", "sksic"); glyph.setAttribute("aria-hidden", "true");
      const search = node("input", "sksearch"); search.type = "search"; search.placeholder = "搜索应用…"; search.setAttribute("aria-label", "搜索应用"); search.autocomplete = "off"; search.value = query;
      wrap.append(glyph, search);
      const chips = node("div", "ap-chips"); chips.setAttribute("role", "group"); chips.setAttribute("aria-label", "分类");
      const grid = node("div", "ap-grid-wrap");
      const store = apps.filter(a => !a.custom);
      function fill() {
        for (const b of chips.children) { const on = b.dataset.chip === chip; b.classList.toggle("on", on); b.setAttribute("aria-pressed", String(on)); }
        const q = query.trim().toLowerCase();
        const rows = store.filter(a => (chip === "all" || a.category === chip) && (!q || textOf(a).includes(q)));
        const parts = [];
        if (!rows.length) {
          const none = node("div", "sknone"); none.append(node("p", "", "没有匹配的应用"), button("clearsrch", "清除搜索", () => { query = ""; chip = "all"; search.value = ""; fill(); }));
          parts.push(none);
        } else if (chip === "all" && !q) {
          const rec = FEATURED.map(id => rows.find(a => a.id === id)).filter(Boolean);
          const section = (title, list) => { const s = node("section", "ap-sec"); s.append(node("h4", "", title)); const g = node("div", "ap-grid"); g.append(...list.map(card)); s.append(g); return s; };
          parts.push(section("为你推荐", rec));
          for (const [id, name] of CHIPS.slice(1, 5)) { const list = rows.filter(a => a.category === id && !rec.includes(a)); if (list.length) parts.push(section(name, list)); }
        } else { const g = node("div", "ap-grid"); g.append(...rows.map(card)); parts.push(g); }
        grid.replaceChildren(...parts);
      }
      for (const [id, name] of CHIPS) { const b = button("ap-chip", name, () => { chip = id; fill(); }); b.dataset.chip = id; chips.append(b); }
      search.oninput = () => { query = search.value; fill(); };
      const foot = node("p", "setfoot ap-foot"); foot.append(button("btn linkish", "自定义应用…", ev => openCustom(ev.currentTarget)));
      fill();
      body.replaceChildren(head, wrap, chips, grid, foot);
    }

    function showConnected() {
      const back = button("setback", "", () => { view = "store"; fail(""); paint(); });
      back.append(node("span", "skback-chev"), document.createTextNode("返回商店"));
      const head = node("div", "sethd"); head.append(node("h2", "", "已连接 " + count + " 个"));
      const list = apps.filter(a => a.state === "connected");
      const lead = node("p", "setlead", list.some(a => a.scope) ? "多数应用对所有 Bot 都能用；带说明的，只有填过密钥的 Bot 能用。" : "这些应用对所有 Bot 都能用。");
      if (!list.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art"); art.setAttribute("aria-hidden", "true");
        empty.append(art, node("h3", "", "还没有连接应用"), node("p", "", "到商店里添加一个，所有 Bot 都能用。"), button("btn pri", "去商店看看", () => { view = "store"; fail(""); paint(); }));
        body.replaceChildren(back, head, empty); return;
      }
      const rows = node("div", "sklist");
      rows.append(...list.map(a => {
        const row = node("div", "skrow ap-row"), tx = node("span", "tx");
        tx.append(node("b", "", a.title), node("span", "hint", a.summary));
        if (a.scope) tx.append(node("span", "ap-scope", a.scope));
        const b = button("btn", "移除", () => remove(a)); b.disabled = busy.has(a.id);
        row.append(icon(a), tx, b); return row;
      }));
      body.replaceChildren(back, head, lead, rows);
    }

    function closeLayer() { if (!layer) return; const {opener} = layer; layer = null; layers.replaceChildren(); opener?.isConnected && opener.focus?.({preventScroll: true}); }
    function openCustom(opener) {
      if (layer) closeLayer();
      const root = node("div", "nestlayer"), cardEl = node("div", "nestcard");
      root.setAttribute("role", "dialog"); root.setAttribute("aria-modal", "true"); root.setAttribute("aria-label", "自定义应用");
      let mode = "program";
      const field = (labelText, ph, multi) => { const w = node("label", "ap-field"); w.append(node("span", "", labelText)); const i = node(multi ? "textarea" : "input"); if (!multi) i.type = "text"; i.placeholder = ph || ""; i.autocomplete = "off"; w.append(i); return [w, i]; };
      const [nameW, name] = field("名称", "比如：我的笔记");
      const [progW, prog] = field("本机程序", "程序的名称或位置");
      const [optW, opts] = field("附加选项（每行一个，可不填）", "", true);
      const [urlW, url] = field("网址", "https://");
      const tabs = node("div", "ap-chips");
      const setMode = m => { mode = m; progW.hidden = optW.hidden = m !== "program"; urlW.hidden = m !== "url"; for (const b of tabs.children) { b.classList.toggle("on", b.dataset.m === m); b.setAttribute("aria-pressed", String(b.dataset.m === m)); } };
      for (const [m, t] of [["program", "本机程序"], ["url", "网址"]]) { const b = button("ap-chip", t, () => setMode(m)); b.dataset.m = m; tabs.append(b); }
      const err = node("p", "sk-error"); err.hidden = true; err.setAttribute("role", "alert");
      const ok = button("btn pri", "添加", async () => {
        err.hidden = true; ok.disabled = true;
        const bodyJSON = mode === "program" ? {name: name.value, program: prog.value, options: opts.value.split("\n").map(s => s.trim()).filter(Boolean)} : {name: name.value, url: url.value};
        try { await api("/api/apps/custom", {method: "POST", body: bodyJSON}); if (!current()) return; closeLayer(); await load(); if (current()) paint(); }
        catch (e) { err.textContent = plain(e, "没能添加，请检查填写的内容。"); err.hidden = false; ok.disabled = false; }
      });
      const actions = node("div", "nest-actions"); actions.append(button("btn", "取消", closeLayer), ok);
      cardEl.append(node("h3", "", "自定义应用"), node("p", "lead", "把你自己的程序或网址接进来，所有 Bot 都能用。"), tabs, nameW, progW, optW, urlW, err, actions);
      root.append(cardEl); layers.replaceChildren(root); layer = {opener};
      root.addEventListener("keydown", ev => { if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); closeLayer(); } });
      setMode("program"); name.focus({preventScroll: true});
    }

    if (await load()) paint();
  }
  window.XBotApps = { mount, unmount: () => teardown() };
})();
