(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  const icon = kind => {
    const n = node("span", "vault-icon"); n.setAttribute("aria-hidden", "true");
    const paths = {key:'<circle cx="7.5" cy="15.5" r="5.5"/><path d="m21 2-9.6 9.6m4.1-4.1 3 3L22 7l-3-3"/>', terminal:'<path d="m4 17 6-6-6-6m8 14h8"/>', search:'<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>', down:'<path d="m6 9 6 6 6-6"/>', lock:'<rect width="18" height="11" x="3" y="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>'};
    n.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">' + paths[kind] + '</svg>';
    return n;
  };
  const domainsOf = input => input.value.split(/[,，]/).map(s => s.trim()).filter(Boolean);
  async function mount({api, host, bot, displayName, isCurrent}) {
    teardown();
    let alive = true, layer = null, secrets = [], category = "all", query = "", closeMenu = () => {};
    const current = () => alive && isCurrent();
    const body = node("div"), layers = node("div", "sk-layers"), error = node("p", "sk-error");
    error.hidden = true; error.setAttribute("role", "alert");
    host.className = "skills-settings vault-settings"; host.replaceChildren(body, error, layers);
    const closeLayer = () => { if (!layer) return; const old = layer; layer = null; old.inputs.forEach(i => i.value = ""); layers.replaceChildren(); if (old.opener?.isConnected) old.opener.focus({preventScroll:true}); };
    const outsideMenu = ev => { if (!ev.target.closest(".vault-add-wrap")) closeMenu(); };
    document.addEventListener("click", outsideMenu);
    teardown = () => { alive = false; closeLayer(); document.removeEventListener("click", outsideMenu); host.replaceChildren(); };
    const path = "/api/bots/" + encodeURIComponent(bot) + "/secrets";
    async function load() {
      try { const data = await api(path); if (current()) { secrets = data.secrets || []; error.hidden = true; paint(); } }
      catch { if (current()) { error.textContent = "没能读取保险库，请重新打开试试。"; error.hidden = false; } }
    }
    function dialog(title, lead, cancelAction = closeLayer) {
      closeMenu(); closeLayer();
      const root = node("div", "nestlayer"), card = node("div", "nestcard"), err = node("p", "sk-error"), actions = node("div", "nest-actions");
      root.setAttribute("role", "dialog"); root.setAttribute("aria-modal", "true"); root.setAttribute("aria-label", title);
      err.hidden = true; err.setAttribute("role", "alert");
      card.append(node("h3", "", title), node("p", "lead", lead)); root.append(card);
      layer = {root, opener:document.activeElement, inputs:[]}; layers.replaceChildren(root);
      root.addEventListener("keydown", ev => {
        if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); cancelAction(); }
        if (ev.key === "Tab") {
          ev.stopPropagation();
          const items = [...root.querySelectorAll("button:not(:disabled),input:not(:disabled)")].filter(n => n.getClientRects().length);
          const first = items[0], last = items.at(-1);
          if (ev.shiftKey && document.activeElement === first) { ev.preventDefault(); last?.focus(); }
          else if (!ev.shiftKey && document.activeElement === last) { ev.preventDefault(); first?.focus(); }
        }
      });
      return {root, card, err, actions};
    }
    function field(label, type, placeholder) {
      const wrap = node("label", "ap-field"), input = node("input"); input.type = type; input.autocomplete = "off"; input.spellcheck = false; input.placeholder = placeholder;
      wrap.append(node("span", "", label), input); layer.inputs.push(input); return [wrap, input];
    }
    async function save(d, active, submit, payload) {
      submit.disabled = true; d.err.hidden = true;
      try { await api(path, {method:"POST", body:payload}); if (!current() || layer !== active) return; closeLayer(); await load(); }
      catch { if (current() && layer === active) { d.err.textContent = "没能保存，请检查后重试。"; d.err.hidden = false; submit.disabled = false; } }
    }
    function add(sudo = false) {
      const d = dialog("添加密钥", "写入当前 Bot 的保险库。提交后界面不再保留明文。"), active = layer;
      const [nw, name] = field("名称", "text", "例如 GH_TOKEN"), [vw, value] = field("值", "password", "粘贴密钥"), [dw, domains] = field("域名", "text", "可空；多个用逗号，如 api.github.com, github.com");
      const shortcut = button("sudo-bar", "", () => { name.value = "LOCAL_SUDO"; domains.value = ""; shortcut.classList.add("on"); shortcut.setAttribute("aria-pressed", "true"); value.focus(); });
      shortcut.setAttribute("aria-pressed", "false");
      const tx = node("span", "tx"); tx.append(node("b", "", "本机 sudo"), node("span", "hint", "给 shell 提权用，模型看不见")); shortcut.append(icon("terminal"), tx);
      const cancel = button("btn", "取消", closeLayer), submit = button("btn pri", "添加", () => {
        if (submit.disabled) return;
        if (!name.value.trim() || !value.value) { d.err.textContent = "请填写名称和值。"; d.err.hidden = false; (!name.value.trim() ? name : value).focus(); return; }
        save(d, active, submit, {name:name.value.trim(), value:value.value, domains:domainsOf(domains)});
      });
      d.actions.append(cancel, submit); d.card.append(shortcut, nw, vw, dw, d.err, d.actions);
      if (sudo) shortcut.click(); else name.focus({preventScroll:true});
    }
    function manage(secret) {
      const d = dialog("管理", "可改绑定域名；要换值就重新设置。界面永不显示旧值。"), active = layer;
      const ro = node("div", "ap-field"); ro.append(node("span", "", "名称"), node("div", "vault-readonly", secret.name));
      const [dw, domains] = field("域名", "text", "可空；多个用逗号分隔"); domains.value = (secret.domains || []).join(", ");
      const [vw, value] = field("新值", "password", "粘贴新密钥"); vw.hidden = true;
      const reset = button("btn linkish", "重新设置值", () => { vw.hidden = false; reset.hidden = true; value.focus(); });
      const removeButton = button("btn vault-danger-ghost", "删除", () => remove(secret));
      const right = node("div", "vault-actions"), submit = button("btn pri", "保存", () => {
        if (submit.disabled) return;
        if (!vw.hidden && !value.value) { d.err.textContent = "请填写新值。"; d.err.hidden = false; value.focus(); return; }
        const payload = {name:secret.name, domains:domainsOf(domains)};
        if (!vw.hidden) payload.value = value.value;
        save(d, active, submit, payload);
      });
      right.append(button("btn", "取消", closeLayer), submit); d.actions.classList.add("vault-spread"); d.actions.append(removeButton, right);
      d.card.append(ro, dw, reset, vw, d.err, d.actions); domains.focus({preventScroll:true});
    }
    function remove(secret) {
      const back = () => manage(secret);
      const d = dialog("删除 " + secret.name + "？", "删掉后，" + displayName + " 再也用不到这个密钥；出站请求也注入不到这个名字。不可恢复，要再用只能重新添加。", back), active = layer;
      const cancel = button("btn", "取消", back), submit = button("btn danger", "删除", async () => {
        if (submit.disabled) return;
        submit.disabled = cancel.disabled = true;
        try { await api(path + "/" + encodeURIComponent(secret.name), {method:"DELETE"}); if (!current() || layer !== active) return; closeLayer(); await load(); }
        catch { if (current() && layer === active) { d.err.textContent = "没能删除，请再试一次。"; d.err.hidden = false; submit.disabled = cancel.disabled = false; } }
      });
      d.actions.append(cancel, submit); d.card.append(d.err, d.actions); cancel.focus({preventScroll:true});
    }
    function paint() {
      closeMenu();
      const head = node("div", "sethd vault-head"); head.append(node("h2", "", "保险库"), node("span", "vault-current", "当前 · " + displayName));
      const foot = node("p", "setfoot", "模型只看见密钥名字，看不见值；出站时代理才注入，且只发往绑定域名。连接应用装密钥时也会写进这里。");
      if (!secrets.length) {
        const empty = node("div", "skempty vault-empty"), art = icon("lock"), acts = node("div", "vault-actions"); art.classList.add("vault-lock");
        acts.append(button("btn pri", "添加", () => add()), button("btn", "本机 sudo", () => add(true)));
        empty.append(art, node("h3", "", "还没有密钥"), node("p", "", "添加后，只有这个 Bot 能在出站请求里用名字引用它们。"), acts);
        body.replaceChildren(head, empty, foot); return;
      }
      const addWrap = node("div", "vault-add-wrap"), menu = node("div", "vault-add-menu"), trigger = button("btn pri", "添加", () => { const open = menu.hidden; menu.hidden = !open; trigger.setAttribute("aria-expanded", String(open)); if (open) menu.querySelector("button").focus(); });
      trigger.append(icon("down")); trigger.setAttribute("aria-haspopup", "menu"); trigger.setAttribute("aria-expanded", "false"); trigger.setAttribute("aria-label", "添加"); menu.hidden = true; menu.setAttribute("role", "menu");
      for (const [sudo, label, hint, glyph] of [[false,"密钥","出站注入 · 可绑域名","key"],[true,"本机 sudo","给 shell 提权 · 模型看不见","terminal"]]) {
        const item = button("vault-menu-item", "", () => add(sudo)), tx = node("span"); tx.append(node("b", "", label), node("span", "hint", hint)); item.append(icon(glyph), tx); item.setAttribute("role", "menuitem"); menu.append(item);
      }
      closeMenu = () => { menu.hidden = true; trigger.setAttribute("aria-expanded", "false"); };
      addWrap.addEventListener("keydown", ev => { if (ev.key === "Escape" && !menu.hidden) { ev.preventDefault(); ev.stopPropagation(); closeMenu(); trigger.focus(); } });
      addWrap.append(trigger, menu); head.append(addWrap);
      const toolbar = node("div", "vault-toolbar"), chips = node("div", "vault-chips"), searchWrap = node("div", "vault-search-wrap"), search = node("input", "vault-search"), list = node("div", "vault-list");
      chips.setAttribute("role", "group"); chips.setAttribute("aria-label", "品类"); list.setAttribute("role", "list");
      search.type = "search"; search.autocomplete = "off"; search.placeholder = "搜索保险库"; search.setAttribute("aria-label", "搜索保险库"); search.value = query;
      const counts = {all:secrets.length, out:secrets.filter(s => s.domains?.length).length}; counts.loc = counts.all - counts.out;
      const isLocal = s => !s.domains?.length;
      function fill() {
        for (const b of chips.children) { const on = b.dataset.category === category; b.classList.toggle("on", on); b.setAttribute("aria-pressed", String(on)); }
        const q = query.trim().toLowerCase();
        const visible = secrets.filter(s => (category === "all" || (category === "loc") === isLocal(s)) && (!q || (s.name + " " + (s.domains || []).join(" ")).toLowerCase().includes(q)));
        if (!visible.length) {
          const empty = node("div", "vault-no-results"); empty.append(node("h3", "", q ? "没有匹配的密钥" : category === "out" ? "没有出站密钥" : "没有本机密钥"), node("p", "", q ? "换个名称或域名试试。" : "换个筛选，或添加一条密钥。")); list.replaceChildren(empty); return;
        }
        list.replaceChildren(...visible.map(secret => {
          const local = isLocal(secret), row = node("div", "vault-card"), glyph = node("span", "vault-glyph " + (local ? "local" : "outbound")), tx = node("span", "tx"), sub = node("span", "vault-sub" + (local ? " local" : ""), local ? "本机" : secret.domains.join(" · "));
          row.setAttribute("role", "listitem"); glyph.append(icon(local ? "terminal" : "key")); sub.title = sub.textContent;
          tx.append(node("span", "vault-name", secret.name), sub); row.append(glyph, tx, button("btn vault-manage", "管理", () => manage(secret))); return row;
        }));
      }
      for (const [id, text] of [["all","全部"],["out","出站"],["loc","本机"]]) { const b = button("vault-chip", text + " " + counts[id], () => { category = id; fill(); }); b.dataset.category = id; chips.append(b); }
      search.oninput = () => { query = search.value; fill(); };
      searchWrap.append(icon("search"), search); toolbar.append(chips, searchWrap); fill(); body.replaceChildren(head, toolbar, list, foot);
    }
    if (!bot) { body.append(node("h2", "", "保险库"), node("p", "setlead", "请先在侧栏选择一个 Bot。")); return; }
    await load();
  }
  window.XBotVault = {mount, unmount:() => teardown()};
})();
