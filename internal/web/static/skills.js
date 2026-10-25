(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const MAX_FILE = 256 << 10;
  const NAME_LINE = /^name:[ \t]*(.+?)[ \t]*$/m;
  // The server already speaks plainly; anything it did not word itself gets the fallback.
  const plain = (e, fallback) => e && e.status >= 400 && e.status < 500 && /[一-鿿]/.test(e.message || "") ? e.message : fallback;
  const nameOf = text => { const head = /^---\r?\n([\s\S]*?)\r?\n---/.exec(text); return head && NAME_LINE.exec(head[1])?.[1].replace(/^["']|["']$/g, "") || ""; };
  const ownText = row => (row.name + " " + row.description).toLowerCase();

  async function mount({api, host, isCurrent}) {
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
      nav++; detail = null; fail("");
      const lead = node("p", "setlead", "技能库整台共用。是否启用，按每个 Bot 单独设。");
      const head = node("div", "sethd"); head.append(node("h2", "", "技能"));
      statusLine = node("p", "sk-status", status); statusLine.setAttribute("role", "status"); statusLine.hidden = !status;
      if (!skills.length) {
        const empty = node("div", "skempty"), art = node("span", "skempty-art"); art.setAttribute("aria-hidden", "true");
        empty.append(art, node("h3", "", "还没有技能"), node("p", "", "从本机导入一份技能，所有 Bot 都能用；要不要开，再按 Bot 设。"), button("btn pri", "导入", ev => openImport(ev.currentTarget)));
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
      const foot = node("p", "setfoot", "点开一条，可给某个 Bot 打开或关掉。改完后下一轮对话生效。");
      function paint() {
        const q = query.trim().toLowerCase(), rows = q ? skills.filter(r => ownText(r).includes(q)) : skills;
        list.replaceChildren(...rows.map(r => {
          const row = button("skrow", "", () => openDetail(r)), tx = node("span", "tx");
          tx.append(node("b", "", r.name), node("span", "hint", r.description));
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
      detail = data; status = "";
      const back = button("setback", "", () => { showList(); }); back.append(node("span", "skback-chev"), document.createTextNode("技能"));
      const bots = node("div", "setrows");
      if (!data.bots.length) bots.append(node("div", "setrow", "还没有 Bot。"));
      for (const b of data.bots) {
        const line = node("div", "setrow"), tx = node("div", "tx"); tx.append(node("b", "", b.name));
        if (b.shared) { tx.append(node("span", "hint", "多人共用的 Bot，暂不使用技能库")); line.append(tx); bots.append(line); continue; }
        const tog = button("tog", "", () => flip(b, tog)); tog.setAttribute("role", "switch"); tog.setAttribute("aria-label", "在 " + b.name + " 里启用「" + data.name + "」");
        const paint = () => { tog.classList.toggle("on", b.enabled); tog.setAttribute("aria-checked", String(b.enabled)); tog.title = b.enabled ? "已启用" : "已停用"; };
        b.paint = paint; paint(); line.append(tx, tog); bots.append(line);
      }
      const sec = node("div", "setsec"); sec.append(node("h4", "", "在这些 Bot 里启用"), bots);
      body.replaceChildren(back, node("div", "skdetail-name", data.name), node("p", "skdetail-desc", data.description), sec,
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
      const {opener} = layer; layer = null; layers.replaceChildren();
      opener?.focus?.();
    }
    function openLayer(opener, title, lead, actions, extra) {
      if (layer) closeLayer();
      const root = node("div", "nestlayer"), card = node("div", "nestcard");
      card.setAttribute("role", "dialog"); card.setAttribute("aria-modal", "true"); card.setAttribute("aria-label", title);
      const buttons = actions.map(([cls, text, act]) => button(cls, text, act));
      const bar = node("div", "nest-actions"); bar.append(...buttons);
      const err = node("p", "sk-error"); err.setAttribute("role", "alert"); err.hidden = true;
      card.append(node("h3", "", title), node("p", "lead", lead), ...(extra || []), err, bar);
      root.append(card);
      root.onkeydown = ev => {
        if (ev.key === "Escape") { ev.preventDefault?.(); ev.stopPropagation(); if (!importing) closeLayer(); }
        else if (ev.key === "Tab") {
          ev.stopPropagation();
          const live = buttons.filter(b => !b.disabled), first = live[0], last = live.at(-1);
          const here = document.activeElement;
          if (live.length && (ev.shiftKey ? here === first || !live.includes(here) : here === last || !live.includes(here))) { ev.preventDefault(); (ev.shiftKey ? last : first).focus?.(); }
        }
      };
      layers.append(root); layer = {opener, err, buttons};
      buttons.at(-1).focus?.();
      return layer;
    }
    const layerError = text => { if (layer) { layer.err.textContent = text; layer.err.hidden = !text; } };

    function openImport(opener) {
      status = ""; if (statusLine) { statusLine.textContent = ""; statusLine.hidden = true; }
      const pick = button("pick", "", () => picker.click());
      const tx = node("span", "tx"); tx.append(node("b", "", "选择技能文件夹…"), node("span", "hint", "文件夹里应有技能说明；导入后出现在列表里。"));
      const icon = node("span", "icwrap"); icon.setAttribute("aria-hidden", "true"); pick.append(icon, tx);
      openLayer(opener, "导入技能", "选择本机上的技能文件夹，拷进你的技能库。导入后所有 Bot 都能看见；是否启用再按 Bot 设。",
        [["btn sec", "取消", () => closeLayer()], ["btn pri", "选择文件夹", () => picker.click()]], [pick]);
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
      send({folder, content, overwrite: false});
    };
    async function send(payload) {
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
      if (problem.status === 409) return conflict(payload);
      layerError(plain(problem, "没能导入，请再试一次。"));
    }
    function conflict(payload) {
      const {opener} = layer, name = nameOf(payload.content);
      closeLayer();
      openLayer(opener, "库里已有同名技能", (name ? "要导入的「" + name + "」和库里已有的重名。" : "要导入的技能和库里已有的重名。") + "覆盖后，用新的这份替换；各 Bot 的启用开关保持不变。",
        [["btn sec", "取消", () => closeLayer()], ["btn pri", "覆盖", () => send({...payload, overwrite: true})]]);
    }

    if (!(await load())) return;
    showList();
  }
  window.XBotSkills = {mount, unmount: () => teardown()};
})();
