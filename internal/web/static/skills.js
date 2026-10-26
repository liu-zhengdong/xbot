(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const MAX_FILE = 256 << 10;
  const NAME_LINE = /^name:[ \t]*(.+?)[ \t]*$/m;
  // The server already speaks plainly; anything it did not word itself gets the fallback.
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const nameOf = text => { const head = /^---\r?\n([\s\S]*?)\r?\n---/.exec(text); return head && NAME_LINE.exec(head[1])?.[1].replace(/^["']|["']$/g, "") || ""; };
  const ownText = row => [row.title, row.name, row.description, row.slug].join(" ").toLowerCase();
  const displayText = row => {
    const slug = (row.slug || "").trim(), name = (row.name || "").trim(), description = (row.description || "").trim();
    const title = (row.title || "").trim() || name || slug;
    const hint = [description, slug].find(text => text && text !== title) || "";
    return {title, hint};
  };

  async function mount({api, host, isCurrent, onLevel = () => {}}) {
    teardown();
    let alive = true, skills = [], query = "", detail = null, status = "", layer = null, importing = false, statusLine = null, nav = 0;
    const current = () => alive && isCurrent();
    teardown = () => { alive = false; nav++; layer = null; host.replaceChildren(); };
    host.className = "skills-settings";
    const errorBox = node("p", "sk-error"); errorBox.setAttribute("role", "alert"); errorBox.hidden = true;
    const body = node("div", "sk-body");
    host.replaceChildren(body, errorBox);
    const fail = text => { errorBox.textContent = text || ""; errorBox.hidden = !text; };
    const picker = node("input", "sk-picker"); picker.type = "file"; picker.hidden = true; picker.setAttribute("webkitdirectory", ""); picker.setAttribute("aria-hidden", "true"); picker.tabIndex = -1;
    const layers = node("div", "sk-layers");
    host.append(picker, layers);

    async function load() {
      try { skills = (await api("/api/skills")).skills || []; }
      catch { if (current()) { fail("没能读取技能，请重新打开试试。"); } return false; }
      return current();
    }

    // ---- 一层：列表 ----
    function showList() {
      nav++; detail = null; fail(""); onLevel(null);
      const lead = node("p", "setlead", "本机 Bot 共享。是否启用，按每个 Bot 单独设。");
      const head = node("div", "sethd"); head.append(node("h2", "", "技能"));
      statusLine = node("p", "sk-status", status); statusLine.setAttribute("role", "status"); statusLine.hidden = !status;
      if (!skills.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art"); art.setAttribute("aria-hidden", "true");
        empty.append(art, node("h3", "", "还没有技能"), node("p", "", "从本机导入一份技能，也可在对话里让 Bot 帮你装。"), button("btn pri", "导入", ev => openImport(ev.currentTarget)));
        body.replaceChildren(head, lead, statusLine, empty);
        return;
      }
      const toolbar = node("div", "sktoolbar"); toolbar.setAttribute("role", "toolbar"); toolbar.setAttribute("aria-label", "技能列表工具");
      const wrap = node("div", "sksearchwrap"), glyph = node("span", "sksic"); glyph.setAttribute("aria-hidden", "true");
      const search = node("input", "sksearch"); search.type = "search"; search.placeholder = "搜索技能"; search.setAttribute("aria-label", "搜索技能"); search.autocomplete = "off"; search.value = query;
      wrap.append(glyph, search);
      toolbar.append(wrap, button("btn pri", "导入", ev => openImport(ev.currentTarget)));
      const list = node("div", "sklist");
      const none = node("div", "sknone"); none.setAttribute("aria-live", "polite");
      const clear = button("clearsrch", "清除搜索", () => { query = ""; search.value = ""; paint(); search.focus?.(); });
      none.append(node("p", "", "没有匹配的技能"), clear);
      const foot = node("p", "setfoot", "点开一条，可给某个 Bot 打开或关掉。");
      function paint() {
        const q = query.trim().toLowerCase(), rows = q ? skills.filter(r => ownText(r).includes(q)) : skills;
        list.replaceChildren(...rows.map(r => {
          const row = button("skrow", "", () => openDetail(r)), tx = node("span", "tx");
          const {title, hint} = displayText(r);
          tx.append(node("b", "", title));
          if (hint) tx.append(node("span", "hint", hint));
          const chev = node("span", "skchev"); chev.setAttribute("aria-hidden", "true");
          row.append(tx, chev); return row;
        }));
        list.hidden = foot.hidden = rows.length === 0;
        none.hidden = rows.length > 0;
      }
      search.oninput = () => { query = search.value; paint(); };
      paint();
      body.replaceChildren(head, lead, statusLine, toolbar, list, none, foot);
    }

    // ---- 二层：单条技能 ----
    async function openDetail(row) {
      fail("");
      // Only the latest pick may change the page: a newer pick, the back button and closing all bump nav.
      const mine = ++nav, latest = () => current() && mine === nav;
      let data;
      try { data = await api("/api/skills/" + encodeURIComponent(row.slug)); }
      catch (e) { if (latest()) fail(plain(e, "没能读取这条技能，请再试一次。")); return; }
      if (!latest()) return;
      const {title, hint} = displayText(data);
      detail = data; status = ""; onLevel({title, backLabel: "技能", back: () => showList()});
      const back = button("setback", "", () => { showList(); }); back.append(node("span", "skback-chev"), document.createTextNode("技能"));
      const bots = node("div", "setrows");
      if (!data.bots.length) bots.append(node("div", "setrow", "还没有 Bot。"));
      for (const b of data.bots) {
        const line = node("div", "setrow"), tx = node("div", "tx"); tx.append(node("b", "", b.name));
        if (b.shared) { tx.append(node("span", "hint", "多人共用的 Bot，暂不使用技能库")); line.append(tx); bots.append(line); continue; }
        const tog = button("tog", "", () => flip(b, tog)); tog.setAttribute("role", "switch"); tog.setAttribute("aria-label", "在 " + b.name + " 里启用「" + title + "」");
        const paint = () => { tog.classList.toggle("on", b.enabled); tog.setAttribute("aria-checked", String(b.enabled)); tog.title = b.enabled ? "已启用" : "已停用"; };
        b.paint = paint; paint(); line.append(tx, tog); bots.append(line);
      }
      const sec = node("div", "setsec"); sec.append(node("h4", "", "在这些 Bot 里启用"), bots);
      body.replaceChildren(back, node("div", "skdetail-name", title), ...(hint ? [node("p", "skdetail-desc", hint)] : []), sec,
        node("p", "setfoot", "关掉只影响这一条技能、这一个 Bot。库里还在，别的 Bot 不受影响。"));
    }
    async function flip(b, tog) {
      if (tog.disabled || !current()) return;
      const want = !b.enabled, slug = detail.slug;
      b.enabled = want; b.paint(); tog.disabled = true; fail("");
      try { await api("/api/bots/" + encodeURIComponent(b.name) + "/skills/" + encodeURIComponent(slug) + (want ? "/enable" : "/disable"), {method: "POST"}); }
      catch { if (current()) { b.enabled = !want; b.paint(); fail("没能保存，请再试一次。"); } }
      finally { tog.disabled = false; }
    }

    // ---- 导入 / 同名覆盖 ----
    function closeLayer() {
      if (!layer) return;
      const {opener, onClose} = layer; layer = null; sourceSeq++; layers.replaceChildren();
      opener?.focus?.(); onClose?.();
    }
    function openLayer(opener, title, lead, actions, extra) {
      if (layer) closeLayer();
      const root = node("div", "nestlayer"), card = node("div", "nestcard");
      card.setAttribute("role", "dialog"); card.setAttribute("aria-modal", "true"); card.setAttribute("aria-label", title);
      const buttons = actions.map(([cls, text, act]) => button(cls, text, act));
      const within = n => Array.from(n.children || []).flatMap(c => c.type === "button" ? [c] : within(c));
      const rowButtons = (extra || []).flatMap(within);
      const bar = node("div", "nest-actions"); bar.append(...buttons);
      const err = node("p", "sk-error"); err.setAttribute("role", "alert"); err.hidden = true;
      card.append(node("h3", "", title), node("p", "lead", lead), ...(extra || []), err, bar);
      root.append(card);
      root.onkeydown = ev => {
        if (ev.key === "Escape") { ev.preventDefault?.(); ev.stopPropagation(); if (!importing) closeLayer(); }
        else if (ev.key === "Tab") {
          ev.stopPropagation();
          const live = [...rowButtons, ...buttons].filter(b => !b.disabled), first = live[0], last = live.at(-1);
          const here = document.activeElement;
          if (live.length && (ev.shiftKey ? here === first || !live.includes(here) : here === last || !live.includes(here))) { ev.preventDefault(); (ev.shiftKey ? last : first).focus?.(); }
        }
      };
      layers.append(root); layer = {opener, err, buttons, onClose: null};
      (buttons.at(-1).disabled ? rowButtons[0] || buttons[0] : buttons.at(-1)).focus?.();
      return layer;
    }
    const layerError = text => { if (layer) { layer.err.textContent = text; layer.err.hidden = !text; } };

    const SOURCES = [
      ["agent", "Agent", "本机 Agent 技能库里的技能", "ag"],
      ["codex", "Codex", "Codex 里的技能", "oa"],
      ["claude", "Claude", "Claude 里的技能", "cl"],
    ];
    const chev = () => { const c = node("span", "skchev"); c.setAttribute("aria-hidden", "true"); return c; };
    // 来源 glyph：透明底单色，品牌标沿用 provider-icons，由 CSS 遮罩着色
    const sourceRow = (title, hint, action, kind) => {
      const row = button("srcrow", "", action), tx = node("span", "tx"), ico = node("span", "bico sm " + kind);
      ico.setAttribute("aria-hidden", "true");
      tx.append(node("b", "", title), node("span", "hint", hint));
      row.append(ico, tx, chev()); return row;
    };
    // 嵌层 A：四行来源，底栏只有取消
    function openImport(opener) {
      status = ""; if (statusLine) { statusLine.textContent = ""; statusLine.hidden = true; }
      const rows = SOURCES.map(([id, label, hint, kind]) => sourceRow(label, hint, () => openSource(layer.opener, id, label), kind));
      rows.push(sourceRow("本机文件夹", "自己挑一个文件夹导入", () => picker.click(), "fo"));
      const box = node("div", "srcrows"); box.setAttribute("role", "list"); box.append(...rows);
      openLayer(opener, "从哪里导入", "把别处已有的技能拷进你的库。本机 Bot 共享。是否启用，按每个 Bot 单独设。",
        [["btn sec", "取消", () => closeLayer()]], [box]);
    }
    // 嵌层 B：该来源下的技能，多选后「导入所选」；空源给「换个来源」
    let sourceSeq = 0; // only the latest source pick may draw; closing or replacing a layer bumps it too
    async function openSource(opener, id, label) {
      const mine = layer, seq = ++sourceSeq;
      const stale = () => !current() || layer !== mine || seq !== sourceSeq;
      let res;
      try { res = await api("/api/skills/sources/" + encodeURIComponent(id)); }
      catch (e) { if (!stale()) layerError(plain(e, "没能读取" + label + "的技能，请再试一次。")); return; }
      if (stale()) return;
      const items = (res.skills || []).map(r => ({...r, on: false}));
      const box = node("div", "cklist");
      if (!items.length) {
        const empty = node("div", "srcempty");
        empty.append(node("p", "", "这边还没有可导入的技能。"), button("btn linkish", "换个来源", () => openImport(opener)));
        box.className = ""; box.append(empty);
        openLayer(opener, "从 " + label + " 导入", "", [["btn sec", "取消", () => closeLayer()]], [box]);
        return;
      }
      box.setAttribute("role", "list");
      let go;
      const sync = () => { go.disabled = !items.some(r => r.on); };
      for (const r of items) {
        const row = button("ckrow", "", () => { r.on = !r.on; ck.classList.toggle("on", r.on); row.setAttribute("aria-pressed", String(r.on)); sync(); });
        const ck = node("span", "ck"), tx = node("span", "tx");
        ck.setAttribute("aria-hidden", "true"); row.setAttribute("aria-pressed", "false");
        const {title, hint} = displayText({...r, slug: r.id});
        tx.append(node("b", "", title), node("span", "hint", hint));
        row.append(ck, tx); box.append(row);
      }
      const L = openLayer(opener, "从 " + label + " 导入", "勾选要拷进库的技能。",
        [["btn sec", "取消", () => closeLayer()], ["btn pri", "导入所选", () => importPicked(id, items.filter(r => r.on))]], [box]);
      go = L.buttons[1]; go.disabled = true;
      box.children[0].focus?.();
    }
    // 逐条导入；遇到同名就问一下，取消只跳过这一条
    function askOverwrite(opener, name) {
      return new Promise(resolve => {
        openLayer(opener, "库里已有同名技能", "要导入的「" + name + "」和库里已有的重名。覆盖后，用新的这份替换；各 Bot 的启用开关保持不变。",
          [["btn sec", "取消", () => closeLayer()], ["btn pri", "覆盖", () => { resolve(true); closeLayer(); }]]);
        layer.onClose = () => resolve(false);
      });
    }
    async function importPicked(source, picked) {
      if (importing || !layer || !current() || !picked.length) return;
      const {opener} = layer, done = [];
      let problem = null;
      importing = true; layer.buttons.forEach(b => b.disabled = true); layerError("");
      for (const r of picked) {
        const body = {source, id: r.id, overwrite: false};
        for (;;) {
          try { await api("/api/skills/import", {method: "POST", body}); done.push(r.name); break; }
          catch (e) {
            if (!current()) { importing = false; return; }
            if (e.status !== 409 || body.overwrite) { problem = e; break; }
            importing = false;
            const yes = await askOverwrite(opener, r.name);
            if (!current()) return;
            importing = true;
            if (!yes) break;
            body.overwrite = true;
          }
        }
        if (problem) break;
      }
      importing = false;
      if (!current()) return;
      const msg = problem ? plain(problem, "没能导入，请再试一次。") : "";
      if (!done.length && layer) { layer.buttons.forEach(b => b.disabled = false); if (problem) layerError(msg); return; }
      if (layer) closeLayer();
      if (done.length) status = done.length === 1 ? "已导入「" + done[0] + "」。" : "已导入 " + done.length + " 条技能。";
      opener?.focus?.();
      if (await load()) { showList(); if (msg) fail(msg); }
    }
    picker.onclick = () => { picker.value = ""; };
    picker.onchange = async () => {
      const files = [...(picker.files || [])]; picker.value = "";
      if (!files.length || !layer) return;
      const mine = layer; // a read that outlives this import layer must not send into a later one
      const folder = (files[0].webkitRelativePath || "").split("/")[0];
      const file = files.find(f => f.webkitRelativePath === folder + "/SKILL.md");
      if (!file) return layerError("这个文件夹里没找到技能说明，请选择一个技能文件夹。");
      if (file.size > MAX_FILE) return layerError("技能说明太大了，导入不了。");
      let content;
      try { content = await file.text(); } catch { return layerError("没能读取这个文件夹，请换一个再试。"); }
      if (!current() || layer !== mine) return;
      send({folder, content, overwrite: false}, nameOf(content));
    };
    async function send(payload, name) {
      if (importing || !layer || !current()) return;
      importing = true; layer.buttons.forEach(b => b.disabled = true); layerError("");
      let res = null, problem = null;
      try { res = await api("/api/skills/import", {method: "POST", body: payload}); } catch (e) { problem = e; }
      importing = false;
      if (!current() || !layer) return;
      layer.buttons.forEach(b => b.disabled = false);
      if (res) {
        const {opener} = layer; closeLayer();
        status = "已导入「" + res.name + "」。"; opener?.focus?.();
        if (await load()) showList();
        return;
      }
      if (problem.status === 409) return conflict(payload, name);
      layerError(plain(problem, "没能导入，请再试一次。"));
    }
    function conflict(payload, name) {
      const {opener} = layer;
      closeLayer();
      openLayer(opener, "库里已有同名技能", (name ? "要导入的「" + name + "」和库里已有的重名。" : "要导入的技能和库里已有的重名。") + "覆盖后，用新的这份替换；各 Bot 的启用开关保持不变。",
        [["btn sec", "取消", () => closeLayer()], ["btn pri", "覆盖", () => send({...payload, overwrite: true}, name)]]);
    }

    if (!(await load())) return;
    showList();
  }
  window.XBotSkills = {mount, unmount: () => teardown()};
})();
