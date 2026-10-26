(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const MAX_FILE = 256 << 10;
  const MAX_BODY = 32 << 10;
  // The server already speaks plainly; anything it did not word itself gets the fallback.
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const ownText = row => (row.name + " " + (row.search || row.preview || "")).toLowerCase();
  // The name the server will give an import: the first short non-empty line, else the file name.
  const nameOf = (filename, text) => {
    for (const raw of String(text || "").split("\n")) {
      const line = raw.trim().replace(/^#+/, "").trim();
      if (!line || line === "---") continue;
      if ([...line].length <= 60) return line.replace(/\s+/g, " ");
      break;
    }
    return String(filename || "").replace(/^.*[\\/]/, "").replace(/\.[^.]*$/, "");
  };
  const chev = () => { const c = node("span", "skchev"); c.setAttribute("aria-hidden", "true"); return c; };

  // A nested card over the page, with Escape to close and Tab kept inside. Card text is always textContent.
  function nest(parent, title, lead, actions, extra, onClose) {
    const root = node("div", "nestlayer"), card = node("div", "nestcard");
    card.setAttribute("role", "dialog"); card.setAttribute("aria-modal", "true"); card.setAttribute("aria-label", title);
    const buttons = actions.map(([cls, text, act]) => button(cls, text, act));
    const bar = node("div", "nest-actions"); bar.append(...buttons);
    const err = node("p", "sk-error"); err.setAttribute("role", "alert"); err.hidden = true;
    card.append(node("h3", "", title));
    if (lead) card.append(node("p", "lead", lead));
    card.append(...(extra || []), err, bar);
    root.append(card);
    const L = {root, card, err, buttons, closed: false, busy: false,
      fail: text => { err.textContent = text || ""; err.hidden = !text; },
      close: () => { if (L.closed) return; L.closed = true; if (root.remove) root.remove(); else parent.replaceChildren(...parent.children.filter(c => c !== root)); onClose?.(); }};
    root.onkeydown = ev => {
      if (ev.key === "Escape") { ev.preventDefault?.(); ev.stopPropagation?.(); if (!L.busy) L.close(); }
      else if (ev.key === "Tab") {
        ev.stopPropagation?.();
        const live = Array.from(card.querySelectorAll?.("button,input,textarea") || []).filter(b => !b.disabled && !b.hidden);
        const first = live[0], last = live.at(-1), here = document.activeElement;
        if (live.length && (ev.shiftKey ? here === first || !live.includes(here) : here === last || !live.includes(here))) { ev.preventDefault(); (ev.shiftKey ? last : first).focus?.(); }
      }
    };
    parent.append(root);
    buttons.at(-1)?.focus?.();
    return L;
  }

  async function mount({api, host, isCurrent, onLevel = () => {}}) {
    teardown();
    let alive = true, cards = [], query = "", status = "", layer = null, nav = 0;
    const current = () => alive && isCurrent();
    teardown = () => { alive = false; nav++; layer?.close(); layer = null; host.replaceChildren(); };
    host.className = "skills-settings persona-settings";
    const errorBox = node("p", "sk-error"); errorBox.setAttribute("role", "alert"); errorBox.hidden = true;
    const body = node("div", "sk-body");
    const layers = node("div", "sk-layers");
    const picker = node("input", "sk-picker"); picker.type = "file"; picker.hidden = true; picker.accept = ".md,.markdown,.txt,text/markdown,text/plain"; picker.setAttribute("aria-hidden", "true"); picker.tabIndex = -1;
    host.replaceChildren(body, errorBox, picker, layers);
    const fail = text => { errorBox.textContent = text || ""; errorBox.hidden = !text; };
    const openLayer = (title, lead, actions, extra) => { layer?.close(); const L = nest(layers, title, lead, actions, extra, () => { if (layer === L) layer = null; }); layer = L; return L; };

    async function load() {
      try { cards = (await api("/api/persona-cards")).cards || []; }
      catch { if (current()) fail("没能读取人设卡，请重新打开试试。"); return false; }
      return current();
    }
    const say = text => { status = text; };

    // ---- 一层：卡库 ----
    function showList() {
      nav++; fail(""); onLevel(null);
      const head = node("div", "sethd"); head.append(node("h2", "", "人设卡"));
      const lead = node("p", "setlead", "人设卡写明 Bot 是谁、怎么说话。整台共用；哪个 Bot 用哪张，在它的「编辑资料」里选。");
      const statusLine = node("p", "sk-status", status); statusLine.setAttribute("role", "status"); statusLine.hidden = !status;
      status = "";
      if (!cards.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art pc-art"); art.setAttribute("aria-hidden", "true");
        const acts = node("div", "pc-emptyacts");
        acts.append(button("btn", "写一张", () => openDetail(null)), button("btn pri", "导入", ev => openImport(ev.currentTarget)));
        empty.append(art, node("h3", "", "还没有人设卡"), node("p", "", "自己写一张，或导入一份 Markdown。导入总是新建一张，不会覆盖已有的。"), acts);
        body.replaceChildren(head, lead, statusLine, empty);
        return;
      }
      const toolbar = node("div", "sktoolbar"); toolbar.setAttribute("role", "toolbar"); toolbar.setAttribute("aria-label", "人设卡列表工具");
      const wrap = node("div", "sksearchwrap"), glyph = node("span", "sksic"); glyph.setAttribute("aria-hidden", "true");
      const search = node("input", "sksearch"); search.type = "search"; search.placeholder = "搜索人设卡"; search.setAttribute("aria-label", "搜索人设卡"); search.autocomplete = "off"; search.value = query;
      wrap.append(glyph, search);
      const add = button("btn pc-add", "", () => openDetail(null)); add.setAttribute("aria-label", "新建人设卡"); add.title = "新建人设卡";
      const plus = node("span", "pc-plus"); plus.setAttribute("aria-hidden", "true"); add.append(plus);
      toolbar.append(wrap, add, button("btn pri", "导入", ev => openImport(ev.currentTarget)));
      const list = node("div", "sklist");
      const none = node("div", "sknone"); none.setAttribute("aria-live", "polite");
      none.append(node("p", "", "没有匹配的人设卡"), button("clearsrch", "清除搜索", () => { query = ""; search.value = ""; paint(); search.focus?.(); }));
      function paint() {
        const q = query.trim().toLowerCase(), rows = q ? cards.filter(r => ownText(r).includes(q)) : cards;
        list.replaceChildren(...rows.map(r => {
          const row = button("skrow", "", () => openDetail(r)), tx = node("span", "tx");
          tx.append(node("b", "", r.name), node("span", "hint", r.preview || ""));
          row.append(tx, chev()); return row;
        }));
        list.hidden = rows.length === 0;
        none.hidden = rows.length > 0;
      }
      search.oninput = () => { query = search.value; paint(); };
      paint();
      body.replaceChildren(head, lead, statusLine, toolbar, list, none);
    }

    // ---- 二层：一张卡（新建时 row 为 null）----
    async function openDetail(row) {
      fail("");
      const mine = ++nav, latest = () => current() && mine === nav;
      let data = {id: "", name: "", body: "", bots: 0};
      if (row) {
        try { data = await api("/api/persona-cards/" + encodeURIComponent(row.id)); }
        catch (e) { if (latest()) fail(plain(e, "没能读取这张人设卡，请再试一次。")); return; }
        if (!latest()) return;
      }
      const title = () => name.value.trim() || (data.id ? data.name : "新人设卡");
      const back = button("setback", "", () => showList()); back.append(node("span", "skback-chev"), document.createTextNode("人设卡"));
      const name = node("input", "pc-name"); name.type = "text"; name.maxLength = 60; name.value = data.name; name.placeholder = "卡名"; name.setAttribute("aria-label", "卡名");
      const text = node("textarea", "pc-body"); text.value = data.body; text.maxLength = MAX_BODY; text.setAttribute("aria-label", "人设卡正文");
      text.placeholder = "写下这个 Bot 是谁、替你做什么、说话的口吻。比如：你是我的写作助手，回答先给结论，语气直接，少用客套。";
      const foot = node("div", "pc-foot");
      const save = button("btn pri", "保存", () => submit());
      if (data.id) foot.append(button("btn pc-del", "删除", ev => askDelete(ev.currentTarget)));
      foot.append(node("span", "pc-gap"), save);
      name.oninput = () => onLevel({title: title(), backLabel: "人设卡", back: () => showList()});
      name.oninput();
      body.replaceChildren(back, name, text, foot);
      (data.id ? text : name).focus?.();

      async function submit() {
        if (save.disabled || !latest()) return;
        if (!text.value.trim()) { fail("人设卡正文不能为空。"); text.focus?.(); return; }
        save.disabled = true; fail("");
        const payload = {name: name.value.trim(), body: text.value};
        try {
          const res = data.id
            ? await api("/api/persona-cards/" + encodeURIComponent(data.id), {method: "PUT", body: payload})
            : await api("/api/persona-cards", {method: "POST", body: payload});
          if (!latest()) return;
          say("已保存「" + res.name + "」。用它的 Bot 下一轮对话就按新的来。");
          if (await load()) showList();
        } catch (e) { if (latest()) fail(plain(e, "没能保存，请再试一次。")); }
        finally { save.disabled = false; }
      }
      function askDelete(opener) {
        const n = data.bots || 0;
        const L = openLayer("删除这张人设卡？", n ? n + " 个 Bot 还在用它。删除后，它们改回未选用。" : "删除后找不回来。",
          [["btn", "取消", () => L.close()], ["btn pri", "删除", () => remove(L, n > 0)]]);
        L.opener = opener;
      }
      async function remove(L, force) {
        if (L.busy || !current()) return;
        L.busy = true; L.buttons.forEach(b => b.disabled = true); L.fail("");
        try {
          await api("/api/persona-cards/" + encodeURIComponent(data.id) + (force ? "?force=1" : ""), {method: "DELETE"});
          if (!current()) return;
          L.busy = false; L.close();
          say("已删除「" + data.name + "」。");
          if (await load()) showList();
        } catch (e) {
          if (!current()) return;
          L.busy = false; L.buttons.forEach(b => b.disabled = false);
          // Someone picked it meanwhile: ask again with the new count.
          if (e.status === 409 && !force) { data.bots = Number(/(\d+)/.exec(e.message || "")?.[1]) || 1; L.close(); askDelete(L.opener); return; }
          L.fail(plain(e, "没能删除，请再试一次。"));
        }
      }
    }

    // ---- 导入 Markdown：总是新建 ----
    let importLayer = null;
    function openImport(opener) {
      importLayer = openLayer("导入 Markdown", "选一份 Markdown 文件。导入后新建一张人设卡，第一行作卡名，全文作正文；不会覆盖已有的卡。",
        [["btn", "取消", () => importLayer.close()], ["btn pri", "选择文件", () => { picker.value = ""; picker.click(); }]]);
      importLayer.opener = opener;
    }
    picker.onchange = async () => {
      const f = picker.files && picker.files[0]; picker.value = "";
      const L = importLayer;
      if (!f || !L || L.closed) return;
      if (f.size > MAX_FILE) return L.fail("这个文件太大了，导入不了。");
      let content;
      try { content = await f.text(); } catch { return L.fail("没能读取这个文件，请换一个再试。"); }
      if (!current() || L.closed) return;
      send(L, {filename: f.name, content, confirm: false});
    };
    async function send(L, payload) {
      if (L.busy || !current()) return;
      L.busy = true; L.buttons.forEach(b => b.disabled = true); L.fail("");
      let res = null, problem = null;
      try { res = await api("/api/persona-cards/import", {method: "POST", body: payload}); } catch (e) { problem = e; }
      L.busy = false;
      if (!current() || L.closed) return;
      L.buttons.forEach(b => b.disabled = false);
      if (res) {
        L.close();
        say("已导入「" + res.name + "」。");
        if (await load()) showList();
        return;
      }
      if (problem.status === 409 && !payload.confirm) return sameName(payload, nameOf(payload.filename, payload.content));
      L.fail(plain(problem, "没能导入，请再试一次。"));
    }
    function sameName(payload, name) {
      const label = name ? "「" + name + "」" : "这个名字";
      const L = openLayer("库里已有同名人设卡", label + "已经有一张了。仍然导入的话，新的这张会在名字后加上编号，比如「" + (name || "卡名") + " (2)」；原来那张不变。",
        [["btn", "取消", () => L.close()], ["btn pri", "仍然导入", () => send(L, {...payload, confirm: true})]]);
      importLayer = L;
    }

    if (!(await load())) return;
    showList();
  }

  // The profile drawer's picker: one card or none, bound as soon as it is confirmed.
  async function pick({api, botName, current, onPicked, manage}) {
    const parent = document.body;
    let cards;
    try { cards = (await api("/api/persona-cards")).cards || []; }
    catch (e) { throw new Error(plain(e, "没能读取人设卡，请再试一次。")); }
    let chosen = current || "", query = "";
    const search = node("input", "sksearch pc-picksearch"); search.type = "search"; search.placeholder = "搜索人设卡"; search.setAttribute("aria-label", "搜索人设卡"); search.autocomplete = "off";
    const wrap = node("div", "sksearchwrap pc-pickwrap"), glyph = node("span", "sksic"); glyph.setAttribute("aria-hidden", "true"); wrap.append(glyph, search);
    const list = node("div", "pc-picklist"); list.setAttribute("role", "radiogroup"); list.setAttribute("aria-label", "人设卡");
    const none = node("p", "pc-picknone", "没有匹配的人设卡"); none.hidden = true;
    const manageBtn = button("btn linkish pc-manage", "管理卡库", () => { L.close(); manage?.(); });
    const rows = [{id: "", name: "未选用", preview: "用创建 Bot 时写的人设"}, ...cards];
    function paint() {
      const q = query.trim().toLowerCase();
      const shown = rows.filter(r => !r.id || !q || ownText(r).includes(q));
      list.replaceChildren(...shown.map(r => {
        const on = r.id === chosen;
        const row = button("pc-pickrow" + (on ? " on" : ""), "", () => { chosen = r.id; paint(); });
        row.setAttribute("role", "radio"); row.setAttribute("aria-checked", String(on));
        const dot = node("span", "pc-radio"); dot.setAttribute("aria-hidden", "true");
        const tx = node("span", "tx"); tx.append(node("b", "", r.name)); if (r.preview) tx.append(node("span", "hint", r.preview));
        row.append(dot, tx); return row;
      }));
      none.hidden = !(q && shown.length === 1);
    }
    search.oninput = () => { query = search.value; paint(); };
    paint();
    const extra = cards.length ? [wrap, list, none, manageBtn] : [list, node("p", "pc-picknone", "卡库还是空的。"), manageBtn];
    const L = nest(parent, "选用人设卡", "选中后马上生效，这个 Bot 下一轮对话就按这张卡说话。", [
      ["btn", "取消", () => L.close()],
      ["btn pri", "选用", async () => {
        if (L.busy) return;
        L.busy = true; L.buttons.forEach(b => b.disabled = true); L.fail("");
        try {
          const res = await api("/api/bots/" + encodeURIComponent(botName) + "/persona-card", {method: "PUT", body: {id: chosen}});
          L.busy = false; L.close();
          onPicked?.(res && res.persona_card || null);
        } catch (e) {
          L.busy = false; L.buttons.forEach(b => b.disabled = false);
          L.fail(plain(e, "没能选用，请再试一次。"));
        }
      }],
    ], extra);
    L.card.classList?.add?.("pc-pickcard");
    L.root.classList?.add?.("pc-picklayer");
    (cards.length ? search : L.buttons.at(-1)).focus?.();
    return L;
  }

  window.XBotPersonaCards = {mount, unmount: () => teardown(), pick};
})();
