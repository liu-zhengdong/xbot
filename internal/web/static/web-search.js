(() => {
  let teardown = () => {};
  // Probe outcomes for the stored key, kept across re-opens. A stored key alone is "saved", never "ok".
  const known = new Map();
  const resting = p => p.key_configured ? known.get(p.id) || "saved" : "empty";
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const svg = path => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/>${path}</svg>`;
  const icons = {play:svg('<path d="M10 9.5v5l4.5-2.5z" fill="currentColor" stroke="none"/>'), ok:svg('<path d="m8.5 12.5 2.2 2.2 4.8-5"/>'), bad:svg('<path d="m9 9 6 6"/><path d="m15 9-6 6"/>')};
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  async function mount({api, host, isCurrent}) {
    teardown();
    let alive = true, saving = false, data;
    const requests = new Set(), rows = new Map();
    const current = () => alive && isCurrent();
    teardown = () => { alive = false; for (const r of requests) r.abort(); requests.clear(); host.replaceChildren(); };
    host.className = "web-search-settings";
    host.replaceChildren(node("h2", "", "网页搜索"), node("p", "setlead", "控制所有 Bot 查网页的方式。整台 XBot 共用一份。"));
    const error = node("p", "ws-error"); error.setAttribute("role", "alert"); error.hidden = true; host.append(error);
    try { data = await api("/api/web-search"); }
    catch { if (current()) { error.textContent = "没能读取网页搜索设置，请重新打开试试。"; error.hidden = false; } return; }
    if (!current()) return;
    const switchBox = node("div", "setrows"), switchRow = node("div", "setrow"), tx = node("div", "tx");
    const hint = node("div", "hint"); tx.append(node("b", "", "允许查网页"), hint);
    const toggle = button("tog", "", () => save({enabled:!data.enabled})); toggle.setAttribute("role", "switch"); toggle.setAttribute("aria-label", "允许查网页");
    switchRow.append(tx, toggle); switchBox.append(switchRow); host.append(switchBox);
    const modeSec = node("div", "setsec"); modeSec.append(node("h4", "", "怎么搜"));
    const modes = node("div", "ws-choices"); modes.setAttribute("role", "radiogroup"); modes.setAttribute("aria-label", "怎么搜");
    const modeButtons = [];
    for (const [id, title, desc] of [["auto","自动","模型自带搜索就用厂商的；没有就用本机搜。"],["local","总用本机","一律用本机搜，不用厂商的——即便模型支持。"]]) {
      const b = button("ws-choice", "", () => save({mode:id})); b.dataset.mode = id; b.setAttribute("role", "radio");
      const body = node("span", "ws-body"), ttl = node("span", "ws-title", title);
      if (id === "auto") ttl.append(node("span", "ws-tag", "默认"));
      body.append(ttl, node("span", "ws-desc", desc)); b.append(node("span", "ws-radio"), body); modes.append(b); modeButtons.push(b);
    }
    modeSec.append(modes); host.append(modeSec);
    const providerSec = node("div", "setsec"), providerTitle = node("h4", "ws-provider-title", "本机用哪家");
    const info = button("ws-provider-info", "ⓘ");
    info.setAttribute("aria-label", "说明");
    info.dataset.tip = "本机搜不到网页时，对话里会提示。默认 DuckDuckGo，免密钥。改完后，新开的对话按新设置来。";
    providerTitle.append(info); providerSec.append(providerTitle);
    const list = node("div", "ws-list"); list.setAttribute("role", "radiogroup"); list.setAttribute("aria-label", "本机用哪家");
    for (const p of data.providers) {
      const row = {p, status:resting(p), version:0, lastAttempt:null};
      // A div, not a button: the selected row holds the key input and its status icon.
      row.button = node("div", "ws-provider"); row.button.dataset.provider = p.id; row.button.setAttribute("role", "radio"); row.button.tabIndex = 0;
      row.button.onclick = () => save({provider:p.id});
      row.button.onkeydown = e => { if (e.target === row.button && (e.key === " " || e.key === "Enter")) { e.preventDefault(); save({provider:p.id}); } };
      const name = node("span", "ws-name", p.name); if (p.id === "ddg") name.append(node("span", "ws-tag", "默认"));
      row.button.append(node("span", "ws-radio"), name);
      if (!p.needs_key) row.button.append(node("span", "ws-meta", "免密钥"));
      else {
        row.key = node("span", "ws-key"); row.key.onclick = e => e.stopPropagation();
        row.input = node("input", "ws-input"); row.input.type = "password"; row.input.id = "search-key-" + p.id; row.input.autocomplete = "new-password"; row.input.maxLength = 512;
        row.input.setAttribute("aria-label", p.name + " API 密钥");
        row.try = button("ws-try", "", () => probe(row, true));
        row.input.oninput = () => {
          row.controller?.abort(); ++row.version; row.status = "empty"; row.lastAttempt = null; row.edited = true; paint();
        };
        row.input.onblur = e => { if (e.relatedTarget !== row.try) probe(row, false); };
        row.key.append(row.input, row.try); row.button.append(row.key);
      }
      list.append(row.button); rows.set(p.id, row);
    }
    providerSec.append(list); host.append(providerSec);
    function paint() {
      toggle.classList.toggle("on",data.enabled); toggle.setAttribute("aria-checked",String(data.enabled)); toggle.disabled = saving;
      hint.textContent = data.enabled ? "需要时 Bot 可以查网页。" : "对话里不会去搜网页。";
      modeSec.hidden = providerSec.hidden = !data.enabled;
      for (const b of modeButtons) { const on = b.dataset.mode === data.mode; b.classList.toggle("on",on); b.setAttribute("aria-checked",String(on)); b.disabled = saving; }
      for (const [id,r] of rows) {
        const selected = id === data.provider;
        r.button.classList.toggle("on",selected); r.button.setAttribute("aria-checked",String(selected)); r.button.setAttribute("aria-disabled",String(saving));
        if (!r.key) continue;
        r.key.hidden = !selected;
        const stored = r.p.key_configured && !r.edited;
        r.input.placeholder = stored ? "••••••••••••" : "粘贴密钥"; r.input.classList.toggle("saved", stored);
        r.try.disabled = r.status === "checking" || (!r.input.value.trim() && !stored);
        const state = r.status === "ok" || r.status === "bad" ? r.status : "play";
        r.try.className = "ws-try " + state; r.try.innerHTML = icons[state];
        const label = r.status === "ok" ? "可用 · 点再试" : r.status === "bad" ? "密钥无效 · 点再试" : r.status === "checking" ? "正在试…" : r.try.disabled ? "先粘贴密钥" : "试一下";
        r.try.title = label; r.try.setAttribute("aria-label", label);
      }
    }
    async function save(patch) {
      if (saving || !current()) return;
      const keyVersions = new Map([...rows].map(([id,r]) => [id,r.version]));
      saving = true; error.hidden = true; paint();
      try {
        const next = await api("/api/web-search",{method:"PUT",body:{enabled:data.enabled,mode:data.mode,provider:data.provider,...patch}});
        if (current()) {
          data = next;
          for (const p of next.providers) {
            const r = rows.get(p.id); if (!r || r.version !== keyVersions.get(p.id)) continue;
            const changed = r.p.key_configured !== p.key_configured;
            r.p = p;
            if (changed && r.input && !r.edited && r.status !== "checking") r.status = resting(p);
          }
        }
      } catch { if (current()) { error.textContent = "没能保存网页搜索设置，请再试一次。"; error.hidden = false; } }
      finally { saving = false; if (current()) paint(); }
    }
    async function probe(r, explicit) {
      const key = r.input.value.trim();
      if (!current() || r.status === "checking" || (!key && (!r.p.key_configured || r.edited)) || (!explicit && r.lastAttempt === key)) return;
      const version = ++r.version, controller = new AbortController(); r.controller = controller; requests.add(controller);
      r.status = "checking"; r.lastAttempt = key; error.hidden = true; paint();
      try {
        await api("/api/web-search/key",{method:"POST",body:{provider:r.p.id,key},signal:controller.signal});
        if (current() && version === r.version) {
          ++r.version; r.status = "ok"; known.set(r.p.id, "ok"); r.p.key_configured = true; r.edited = false;
          r.input.value = ""; r.lastAttempt = "";
        }
      } catch { if (current() && version === r.version) { r.status = "bad"; if (!key) known.set(r.p.id, "bad"); } }
      finally { requests.delete(controller); if (current()) paint(); }
    }
    paint();
  }
  window.XBotWebSearch = {mount, unmount:() => teardown()};
})();
