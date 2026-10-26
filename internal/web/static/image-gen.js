(() => {
  let teardown = () => {};
  const node = (tag, cls, text) => { const n = document.createElement(tag); n.className = cls || ""; if (text != null) n.textContent = text; return n; };
  const button = (cls, text, action) => { const b = node("button", cls, text); b.type = "button"; b.onclick = action; return b; };
  async function mount({api, host, isCurrent, manage}) {
    teardown();
    let alive = true, saving = false, data;
    const current = () => alive && isCurrent();
    teardown = () => { alive = false; host.replaceChildren(); };
    host.className = "web-search-settings image-gen-settings";
    host.replaceChildren(node("h2", "", "图片生成"), node("p", "setlead", "决定 Bot 用哪家画图。整台 XBot 共用一份。"));
    const error = node("p", "ws-error"); error.setAttribute("role", "alert"); error.hidden = true; host.append(error);
    try { data = await api("/api/image-gen"); }
    catch { if (current()) { error.textContent = "没能读取图片生成设置，请重新打开试试。"; error.hidden = false; } return; }
    if (!current()) return;
    const box = node("div", "setrows"), row = node("div", "setrow"), tx = node("div", "tx"), hint = node("div", "hint");
    tx.append(node("b", "", "允许生成图片"), hint);
    const toggle = button("tog", "", () => save({enabled:!data.enabled}));
    toggle.setAttribute("role", "switch"); toggle.setAttribute("aria-label", "允许生成图片");
    row.append(tx, toggle); box.append(row); host.append(box);
    const section = node("div", "setsec"); section.append(node("h4", "", "用哪家"));
    const list = node("div", "ws-list"); list.setAttribute("role", "radiogroup"); list.setAttribute("aria-label", "用哪家");
    const rows = [];
    for (const p of data.providers) {
      const select = button("ws-provider ig-provider", "", () => save({provider:p.id}));
      select.dataset.provider = p.id; select.setAttribute("role", "radio");
      const glyph = node("img", "ig-glyph"); glyph.src = `/provider-icons/${p.id}.svg`; glyph.alt = "";
      const meta = node("span", "ws-meta");
      select.append(node("span", "ws-radio"), glyph, node("span", "ws-name", p.name), meta);
      const guide = node("div", "ig-guide");
      guide.append(node("p", "", `还没登录 ${p.id === "openai" ? "OpenAI" : "Grok"} 账号。到「模型与供应商」里点登录，不用在这里再登一遍。`), button("ws-try", "去模型与供应商", () => manage(p.id)));
      list.append(select, guide); rows.push({id:p.id, select, meta, guide});
    }
    // Arrow keys follow the radio-group convention and keep focus on the choice.
    list.onkeydown = e => {
      if (!["ArrowDown","ArrowRight","ArrowUp","ArrowLeft"].includes(e.key)) return;
      const i = rows.findIndex(r => r.select === e.target); if (i < 0) return;
      e.preventDefault(); if (saving) return;
      const next = rows[(i + (["ArrowDown","ArrowRight"].includes(e.key) ? 1 : rows.length - 1)) % rows.length];
      next.select.focus(); save({provider:next.id});
    };
    section.append(list, node("p", "setfoot ig-binding-guide", "要和 Bot 当前使用的账号、模型对上。"), node("p", "setfoot ws-provider-foot", "画好的图进工作区，不会出现在气泡里。"));
    host.append(section, node("p", "setfoot", "改完后，新开的对话按新设置来。"));
    function paint() {
      toggle.classList.toggle("on", data.enabled); toggle.setAttribute("aria-checked", String(data.enabled)); toggle.disabled = saving;
      hint.textContent = data.enabled ? "需要时 Bot 可以画图。" : "对话里不会去画图。";
      section.hidden = !data.enabled;
      for (const r of rows) {
        const selected = data.provider === r.id, loggedIn = data.providers.find(p => p.id === r.id)?.logged_in;
        r.select.classList.toggle("on", selected); r.select.setAttribute("aria-checked", String(selected)); r.select.disabled = saving; r.select.tabIndex = selected ? 0 : -1;
        r.meta.className = "ws-meta" + (loggedIn ? " ok" : ""); r.meta.textContent = loggedIn ? "已登录" : "未登录";
        r.guide.hidden = !selected || loggedIn;
      }
    }
    async function save(patch) {
      if (saving || !current()) return;
      saving = true; error.hidden = true; paint();
      try {
        const next = await api("/api/image-gen", {method:"PUT", body:{enabled:data.enabled,provider:data.provider,...patch}});
        if (current()) data = next;
      } catch { if (current()) { error.textContent = "没能保存图片生成设置，请再试一次。"; error.hidden = false; } }
      finally { saving = false; if (current()) paint(); }
    }
    paint();
  }
  window.XBotImageGen = {mount, unmount:() => teardown()};
})();
