(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
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
    const providerSec = node("div", "setsec"); providerSec.append(node("h4", "", "本机用哪家"));
    const list = node("div", "ws-list"); list.setAttribute("role", "radiogroup"); list.setAttribute("aria-label", "本机用哪家");
    for (const p of data.providers) {
      const row = {p, status:p.key_configured ? "ok" : "empty", version:0, lastAttempt:null};
      row.button = button("ws-provider", "", () => save({provider:p.id})); row.button.dataset.provider = p.id; row.button.setAttribute("role", "radio");
      const name = node("span", "ws-name", p.name); if (p.id === "ddg") name.append(node("span", "ws-tag", "默认"));
      row.meta = node("span", "ws-meta"); row.button.append(node("span", "ws-radio"), name, row.meta); list.append(row.button);
      if (p.needs_key) {
        row.key = node("div", "ws-key");
        const label = node("label", "", "API 密钥"); label.htmlFor = "search-key-" + p.id;
        row.input = node("input", "ws-input"); row.input.type = "password"; row.input.id = label.htmlFor; row.input.autocomplete = "new-password"; row.input.maxLength = 512;
        row.input.placeholder = p.key_configured ? "••••••••••••" : "粘贴 API 密钥";
        row.try = button("ws-try", "试一下", () => probe(row, true));
        row.input.oninput = () => {
          row.controller?.abort(); ++row.version; row.status = "empty"; row.lastAttempt = null; row.edited = true; paint();
        };
        row.input.onblur = e => { if (e.relatedTarget !== row.try) probe(row, false); };
        const line = node("div", "ws-key-row"); line.append(row.input, row.try);
        row.message = node("div", "ws-status"); row.message.setAttribute("role", "status"); row.message.setAttribute("aria-live", "polite");
        row.key.append(label, line, row.message); list.append(row.key);
      }
      rows.set(p.id, row);
    }
    providerSec.append(list, node("p", "setfoot ws-provider-foot", "本机搜不到网页时，对话里会提示。默认 DuckDuckGo，免密钥。"));
    host.append(providerSec, node("p", "setfoot", "改完后，新开的对话按新设置来。"));
    function paint() {
      toggle.classList.toggle("on",data.enabled); toggle.setAttribute("aria-checked",String(data.enabled)); toggle.disabled = saving;
      hint.textContent = data.enabled ? "需要时 Bot 可以查网页。" : "对话里不会去搜网页。";
      modeSec.hidden = providerSec.hidden = !data.enabled;
      for (const b of modeButtons) { const on = b.dataset.mode === data.mode; b.classList.toggle("on",on); b.setAttribute("aria-checked",String(on)); b.disabled = saving; }
      for (const [id,r] of rows) {
        const selected = id === data.provider;
        r.button.classList.toggle("on",selected); r.button.setAttribute("aria-checked",String(selected)); r.button.disabled = saving;
        const ok = selected && r.status === "ok", bad = selected && r.status === "bad";
        r.meta.className = "ws-meta" + (ok ? " ok" : bad ? " bad" : "");
        r.meta.textContent = ok ? "可用" : bad ? "密钥无效" : selected && r.status === "checking" ? "正在试…" : r.p.needs_key ? "要密钥" : "免密钥";
        if (!r.key) continue;
        r.key.hidden = !selected;
        r.try.disabled = r.status === "checking" || (!r.input.value.trim() && (!r.p.key_configured || r.edited));
        r.message.className = "ws-status" + (ok ? " ok" : bad ? " bad" : "");
        r.message.textContent = ok ? "密钥可用" : bad ? "密钥无效或连不上" : r.status === "checking" ? "正在试…" : "只保存在本机，用于本机搜网页。";
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
            if (changed && r.input && !r.edited && r.status !== "checking") {
              r.input.placeholder = p.key_configured ? "••••••••••••" : "粘贴 API 密钥";
              r.status = p.key_configured ? "ok" : "empty";
            }
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
          ++r.version; r.status = "ok"; r.p.key_configured = true; r.edited = false;
          r.input.value = ""; r.input.placeholder = "••••••••••••"; r.lastAttempt = "";
        }
      } catch { if (current() && version === r.version) r.status = "bad"; }
      finally { requests.delete(controller); if (current()) paint(); }
    }
    paint();
  }
  window.XBotWebSearch = {mount, unmount:() => teardown()};
})();
