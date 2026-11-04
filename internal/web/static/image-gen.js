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
    host.replaceChildren(node("h2", "", "图片生成"), node("p", "ig-scope", "所有 Bot 共用这份设置"));
    const error = node("p", "ws-error"); error.setAttribute("role", "alert"); error.hidden = true; host.append(error);
    try { data = await api("/api/image-gen"); }
    catch { if (current()) { error.textContent = "没能读取图片生成设置，请重新打开试试。"; error.hidden = false; } return; }
    if (!current()) return;
    const box = node("div", "setrows"), row = node("div", "setrow settings-field"), tx = node("div", "tx"), hint = node("div", "hint");
    tx.append(node("span", "settings-field-label", "允许生成图片"), hint);
    const toggle = node("fieldset", "set-segments");
    toggle.setAttribute("aria-label", "允许生成图片");
    for (const [value, text] of [["off", "关闭"], ["on", "开启"]]) {
      const label = node("label", "", text), input = node("input");
      input.type = "radio"; input.name = "image-gen-enabled"; input.value = value;
      label.prepend(input); toggle.append(label);
    }
    toggle.onchange = e => save({enabled:e.target.value === "on"});
    row.append(tx, toggle); box.append(row);
    const modeSec = node("div", "setrow settings-field"), modeTx = node("div", "tx");
    const modeLabel = node("label", "settings-field-label", "生图方式"), modeHint = node("div", "hint");
    modeLabel.htmlFor = "image-gen-mode"; modeHint.id = "image-gen-mode-hint"; modeTx.append(modeLabel, modeHint);
    const modeControl = node("div", "settings-field-control"), modes = node("select", "settings-select");
    modes.id = "image-gen-mode"; modes.setAttribute("aria-label", "生图方式"); modes.setAttribute("aria-describedby", modeHint.id);
    for (const [id, title] of [["auto", "自动"], ["local", "始终使用下方账号"]]) {
      const option = node("option", "", title); option.value = id; modes.append(option);
    }
    modes.onchange = e => save({mode:e.target.value});
    modeControl.append(modes); modeSec.append(modeTx, modeControl); box.append(modeSec); host.append(box);
    const section = node("div", "setsec ig-accounts"); section.append(node("h4", "", "生图账号"));
    const list = node("div", "ws-list"); list.setAttribute("role", "radiogroup"); list.setAttribute("aria-label", "生图账号");
    const rows = [];
    for (const p of data.providers) {
      const select = button("ws-provider ig-provider", "", () => save({provider:p.id}));
      select.dataset.provider = p.id; select.setAttribute("role", "radio");
      const glyph = node("img", "ig-glyph"); glyph.src = `/provider-icons/${p.id}.svg`; glyph.alt = "";
      const meta = node("span", "ws-meta");
      select.append(node("span", "ws-radio"), glyph, node("span", "ws-name", p.name), meta);
      const guide = node("div", "ig-guide");
      guide.append(node("p", "", `还没登录 ${p.id === "openai" ? "OpenAI" : "Grok"} 账号。到「模型与供应商」里点登录，不用在这里再登一遍。`), button("btn ghost", "去模型与供应商", () => manage(p.id)));
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
    section.append(list);
    const footer = node("p", "ig-footnote"), imageNote = node("span", "ws-provider-foot", "图片保存在工作区，由 Bot 挂到回复中。");
    imageNote.append(node("br")); footer.append(imageNote, node("span", "", "修改从下一轮对话生效。"));
    host.append(section, footer);
    function paint() {
      toggle.querySelector(`input[value="${data.enabled ? "on" : "off"}"]`).checked = true; toggle.disabled = saving;
      hint.textContent = data.enabled ? "Bot 可以按需画图" : "对话里不会去画图";
      modeSec.hidden = section.hidden = imageNote.hidden = !data.enabled;
      modes.value = data.mode; modes.disabled = saving;
      modeHint.textContent = data.mode === "local" ? "跳过当前模型自带的生图能力" : "优先使用当前模型的生图能力，否则使用下方账号";
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
        const next = await api("/api/image-gen", {method:"PUT", body:{enabled:data.enabled,mode:data.mode,provider:data.provider,...patch}});
        if (current()) data = next;
      } catch { if (current()) { error.textContent = "没能保存图片生成设置，请再试一次。"; error.hidden = false; } }
      finally { saving = false; if (current()) paint(); }
    }
    paint();
  }
  window.XBotImageGen = {mount, unmount:() => teardown()};
})();
