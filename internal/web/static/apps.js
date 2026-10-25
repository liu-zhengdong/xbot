(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const CHIPS = [["all", "全部"], ["local", "本机"], ["dev", "开发"], ["work", "效率"], ["chat", "通讯"], ["more", "更多"]];
  const FEATURED = ["desktop", "browser", "context7"];
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
      if (a.state === "connected") return [a.category === "local" || a.category === "chat" ? "已连接" : "已添加", "btn ap-done", true];
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
      const b = button(cls, text, () => add(a)); b.disabled = off || busy.has(a.id);
      c.append(tx, b); return c;
    }
    function paint() { return view === "connected" ? showConnected() : showStore(); }

    function showStore() {
      view = "store"; fail("");
      const head = node("div", "sethd ap-head"); head.append(node("h2", "", "连接应用"));
      const open = button("ap-count", "已连接 " + count + " 个", () => { view = "connected"; paint(); }); open.append(node("span", "skchev"));
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
      fail("");
      const back = button("setback", "", () => { view = "store"; paint(); });
      back.append(node("span", "skback-chev"), document.createTextNode("返回商店"));
      const head = node("div", "sethd"); head.append(node("h2", "", "已连接 " + count + " 个"));
      const lead = node("p", "setlead", "这些应用对所有 Bot 都能用。");
      const list = apps.filter(a => a.state === "connected");
      if (!list.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art"); art.setAttribute("aria-hidden", "true");
        empty.append(art, node("h3", "", "还没有连接应用"), node("p", "", "到商店里添加一个，所有 Bot 都能用。"), button("btn pri", "去商店看看", () => { view = "store"; paint(); }));
        body.replaceChildren(back, head, empty); return;
      }
      const rows = node("div", "sklist");
      rows.append(...list.map(a => {
        const row = node("div", "skrow ap-row"), tx = node("span", "tx");
        tx.append(node("b", "", a.title), node("span", "hint", a.summary));
        const b = button("btn", "移除", () => remove(a)); b.disabled = busy.has(a.id);
        row.append(tx, b); return row;
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
