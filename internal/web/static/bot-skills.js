(() => {
  function create({botName, api, el, icon, appendMarkdown, current, manage}) {
    let alive = true, rows = [], loaded = false, selected = null, listRequest = 0, detailRequest = 0;
    let listScroll = 0, detailKey = "";
    const root = el("div", "bot-skills-view");
    const toolbar = el("div", "bot-skills-toolbar");
    const count = el("span", "bot-skills-count");
    const tools = el("div", "bot-skills-tools"), searchBox = el("label", "bot-skills-search");
    const field = el("input"); field.type = "search"; field.placeholder = "搜索技能";
    field.setAttribute("aria-label", "搜索技能"); field.autocomplete = "off";
    const button = (cls, text, action) => {
      const node = el("button", cls, text); node.type = "button"; node.addEventListener("click", action); return node;
    };
    const clear = button("icon-btn bot-skills-clear", "", () => { field.value = ""; filter(); field.focus(); });
    clear.append(icon("x")); clear.setAttribute("aria-label", "清除搜索"); clear.hidden = true;
    const manager = button("btn ghost bot-skills-manage", "管理技能", manage);
    searchBox.append(icon("search"), field, clear); tools.append(searchBox, manager); toolbar.append(count, tools);
    const notice = el("div", "bot-skills-notice"); notice.setAttribute("role", "status"); notice.hidden = true;
    const layout = el("div", "bot-skills-layout"), list = el("div", "bot-skills-list");
    const detail = el("article", "bot-skill-detail"); detail.hidden = true;
    layout.append(list, detail); root.append(toolbar, notice, layout);
    const valid = name => alive && current() && botName() === name;
    const scrollHost = () => root.closest(".panel-body");
    const display = row => window.XBotSkills.displayText(row);

    function message(host, title, text, action, actionLabel = "重试") {
      const block = el("div", "bot-skills-empty");
      block.append(el("h3", "", title));
      if (text) block.append(el("p", "", text));
      if (action) block.append(button("btn sec bot-skills-retry", actionLabel, action));
      host.replaceChildren(block);
    }
    function paintSelection() {
      root.classList.toggle("has-detail", !!selected);
      layout.classList.toggle("has-detail", !!selected); detail.hidden = !selected;
      for (const row of list.querySelectorAll(".bot-skill-row")) row.setAttribute("aria-current", String(row.dataset.slug === selected));
    }
    function closeDetail() {
      if (!selected) return false;
      const slug = selected; selected = null; detailKey = ""; ++detailRequest;
      detail.replaceChildren(); paintSelection();
      const opener = [...list.querySelectorAll(".bot-skill-row")].find(row => row.dataset.slug === slug);
      requestAnimationFrame(() => {
        if (!alive || selected) return;
        if (scrollHost()) scrollHost().scrollTop = listScroll;
        opener?.focus({preventScroll:true});
      });
      return true;
    }
    function filter() {
      const query = field.value.trim().toLowerCase(); clear.hidden = !field.value;
      const visible = rows.filter(row => [row.title, row.name, row.slug, row.description].join(" ").toLowerCase().includes(query));
      if (selected && !visible.some(row => row.slug === selected)) closeDetail();
      if (!rows.length) {
        toolbar.hidden = true;
        message(list, "这个 Bot 尚未启用技能", "到技能库启用一项技能。", manage, "管理技能"); return;
      }
      toolbar.hidden = false;
      if (!visible.length) {
        message(list, "没有匹配的技能", "换一个名称或关键词试试。", () => { field.value = ""; filter(); field.focus(); }, "清除搜索"); return;
      }
      list.replaceChildren(...visible.map(row => {
        const {title, hint} = display(row), node = button("bot-skill-row", "", () => openDetail(row));
        node.dataset.slug = row.slug; node.setAttribute("aria-label", "查看" + title);
        const copy = el("span", "bot-skill-copy"); copy.append(el("span", "bot-skill-name", title));
        if (hint) copy.append(el("span", "bot-skill-description", hint));
        node.append(copy, icon("chevron")); return node;
      }));
      paintSelection();
    }
    field.addEventListener("input", ev => { if (!ev.isComposing) filter(); });
    field.addEventListener("compositionend", filter);

    function detailHeader(row) {
      const back = button("btn ghost bot-skill-back", "返回技能列表", closeDetail);
      const head = el("div", "bot-skill-heading"), copy = el("div");
      const title = el("h2", "", display(row).title); title.tabIndex = -1;
      copy.append(title, el("p", "bot-skill-slug", row.slug));
      const close = button("icon-btn bot-skill-close", "", closeDetail); close.append(icon("x")); close.setAttribute("aria-label", "关闭技能详情");
      head.append(copy, close);
      const content = el("div", "bot-skill-content");
      detail.replaceChildren(back, head, content);
      return {content, title};
    }
    async function openDetail(row, refresh = false) {
      const name = botName(), request = ++detailRequest;
      if (!refresh) {
        if (!selected) listScroll = scrollHost()?.scrollTop || 0;
        selected = row.slug; detailKey = ""; paintSelection();
        const {content, title} = detailHeader(row); content.append(el("p", "muted", "正在读取技能…"));
        if (scrollHost()) scrollHost().scrollTop = 0;
        title.focus({preventScroll:true});
      }
      const latest = () => valid(name) && request === detailRequest && selected === row.slug;
      try {
        const data = await api("/api/bots/" + encodeURIComponent(name) + "/skills/" + encodeURIComponent(row.slug));
        if (!latest()) return;
        const key = JSON.stringify(data);
        detail.querySelector(".bot-skill-error")?.remove();
        if (detailKey === key) return;
        const {title, hint} = display(data), content = detail.querySelector(".bot-skill-content");
        detail.querySelector("h2").textContent = title;
        detail.querySelector(".bot-skill-slug").textContent = data.slug;
        content.replaceChildren();
        if (hint) content.append(el("p", "bot-skill-lead", hint));
        const markdown = el("div", "prose bot-skill-markdown"); appendMarkdown(markdown, data.body || "");
        if (!data.body?.trim()) markdown.append(el("p", "muted", "这项技能还没有正文。"));
        const raw = el("details", "bot-skill-raw");
        raw.append(el("summary", "", "查看 Markdown 原文"), el("pre", "", data.body || ""));
        content.append(markdown, raw); detailKey = key;
      } catch (e) {
        if (!latest()) return;
        detail.querySelector(".bot-skill-error")?.remove();
        const error = el("div", "bot-skill-error"); error.setAttribute("role", "status");
        message(error, "技能读取失败", "请重试。", () => openDetail(row, true));
        if (!detailKey) detail.querySelector(".bot-skill-content").replaceChildren();
        detail.append(error);
      }
    }
    async function refresh() {
      const name = botName(), request = ++listRequest;
      notice.hidden = true; layout.setAttribute("aria-busy", "true");
      if (!loaded) { toolbar.hidden = true; message(list, "正在读取技能…", ""); }
      try {
        const data = await api("/api/bots/" + encodeURIComponent(name) + "/skills");
        if (!valid(name) || request !== listRequest) return;
        const next = data.skills.filter(row => !row.disabled), changed = JSON.stringify(next) !== JSON.stringify(rows);
        rows = next; count.textContent = "已启用 " + rows.length;
        if (selected && !rows.some(row => row.slug === selected)) closeDetail();
        if (!loaded || changed) filter();
        loaded = true;
        if (selected) openDetail(rows.find(row => row.slug === selected), true);
      } catch (e) {
        if (!valid(name) || request !== listRequest) return;
        notice.hidden = false;
        message(notice, "技能加载失败", loaded ? "保留上次读取的内容。" : "暂时无法读取这个 Bot 的技能。", refresh);
        if (!loaded) list.replaceChildren();
      } finally {
        if (valid(name) && request === listRequest) layout.setAttribute("aria-busy", "false");
      }
    }
    return {root, refresh, closeDetail, dispose() { alive = false; ++listRequest; ++detailRequest; root.remove(); }};
  }
  window.XBotBotSkills = {create};
})();
