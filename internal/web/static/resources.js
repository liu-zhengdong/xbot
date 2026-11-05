// Resource reading lives outside the keyed chat projection.
(() => {
  function target(value, explicit = false) {
    let path = String(value).trim(), fileURL = false;
    if (/^file:/i.test(path)) {
      try {
        const url = new URL(path);
        if (url.hostname && url.hostname !== "localhost") return null;
        path = url.pathname + (/^#L\d+(?:-L?\d+)?$/.test(url.hash) ? url.hash : "");
        if (/^\/[A-Za-z]:\//.test(path)) path = path.slice(1);
        fileURL = true;
      } catch { return null; }
    }
    const position = /^(.*?)(?::(\d+)(?::\d+)?|#L(\d+)(?:-L?(\d+))?)$/.exec(path);
    if (position) path = position[1];
    if (fileURL || explicit && path.includes("%")) {
      try { path = decodeURIComponent(path); } catch { return null; }
    }
    if (!/^(?:~[/\\]|\/(?!\/)|[A-Za-z]:[/\\])/.test(path)) return null;
    // These are this application's HTTP namespaces, not implicit file links.
    if (!fileURL && /^\/(?:api|v1)(?:\/|$)/.test(path)) return null;
    if (/[\n\r\0]|\s--?\w|\s[|;&<>]|\$\(/.test(path)) return null;
    const line = position ? Number(position[2] || position[3]) : 1;
    if (!Number.isSafeInteger(line) || line < 1) return null;
    return { path, line, endLine: Number(position?.[4]) || line };
  }

  function referenceAddress(value, path = value.path) {
    return value.line > 1 ? path + (value.endLine > value.line ? "#L" + value.line + "-L" + value.endLine : ":" + value.line) : path;
  }

  function create({ api, el, icon, copyText, read, errorText }) {
    const layer = el("section", "resource-layer");
    layer.id = "resource-layer"; layer.hidden = true; layer.tabIndex = -1;
    layer.setAttribute("role", "dialog"); layer.setAttribute("aria-labelledby", "resource-title");
    const menu = el("div", "resource-menu"); menu.id = "resource-menu"; menu.hidden = true;
    document.body.append(layer, menu);
    let current = null, origin = null, history = [], request = 0, controller = null;

    const button = (text, id, action, cls = "secondary") => {
      const node = el("button", cls, text); node.type = "button"; if (id) node.id = id;
      node.addEventListener("click", action); return node;
    };
    const control = (label, id, glyph, action) => {
      const node = button("", id, action, "icon-btn"); node.setAttribute("aria-label", label); node.title = label; node.append(icon(glyph)); return node;
    };
    function cancel() { request++; controller?.abort(); controller = null; }
    function close(focus = false) {
      cancel(); layer.hidden = true; menu.hidden = true; history = []; current = null;
      if (origin) {
        origin.setAttribute("aria-expanded", "false");
        if (focus && origin.isConnected) origin.focus({ preventScroll: true });
      }
      origin = null;
    }
    function position() {
      if (layer.hidden || !origin) return;
      const gutter = parseFloat(getComputedStyle(layer).getPropertyValue("--resource-edge"));
      if (matchMedia("(max-width:767px)").matches) { layer.style.left = ""; layer.style.top = ""; return; }
      const r = origin.getBoundingClientRect(), box = layer.getBoundingClientRect();
      layer.style.left = Math.max(gutter, Math.min(r.left, innerWidth - box.width - gutter)) + "px";
      layer.style.top = Math.max(gutter, Math.min(r.bottom + gutter, innerHeight - box.height - gutter)) + "px";
    }
    async function copyAddress(text, node, status) {
      const copied = await copyText(text, node);
      status.textContent = copied ? "已复制" : "复制失败，请选择地址后复制";
    }
    function render() {
      const focused = layer.contains(document.activeElement) ? document.activeElement : null;
      const view = current, page = view.page, source = page?.kind === "source";
      const head = el("header", "resource-header"), heading = el("h2", "resource-title"); heading.id = "resource-title";
      if (history.length) head.append(control("返回上一级", "resource-back", "chevronLeft", () => {
        cancel(); current = history.pop(); render(); current.body.scrollTop = current.scrollTop; layer.focus({ preventScroll: true });
      }));
      heading.append(icon(source ? "globe" : page?.kind === "directory" ? "folder" : "file-text"), el("span", "", page?.name || view.target.path.split(/[/\\]/).filter(Boolean).at(-1) || view.target.path));
      head.append(heading, control("关闭", "resource-close", "x", () => close(true)));
      const subtitle = el("div", "resource-subtitle", source ? "网页来源" : (page?.device || "当前连接设备") + (view.target.line > 1 ? " · 第 " + view.target.line + " 行" : ""));
      const address = el("div", "resource-address", page?.path || view.target.path);
      const footer = el("footer", "resource-footer"), status = el("span", "resource-copy-status");
      status.id = "resource-copy-status"; status.setAttribute("role", "status");
      const copyLabel = source ? "复制链接" : view.target.line > 1 ? "复制引用" : "复制路径";
      footer.append(button(copyLabel, "resource-copy", ev => copyAddress(source ? page.path : referenceAddress(view.target, page?.path || view.target.path), ev.currentTarget, status)), status);
      if (source) {
        const link = el("a", "resource-original", "打开原网页"); link.href = page.path; link.target = "_blank"; link.rel = "noopener noreferrer"; footer.append(link);
      } else if (page?.can_open && ["directory", "file"].includes(page.kind)) {
        const label = page.kind === "directory" ? (page.platform === "darwin" ? "在访达中打开" : "打开文件夹") : "用默认应用打开";
        footer.append(button(label, "resource-open", async ev => {
          const node = ev.currentTarget; node.disabled = true;
          try { await api("/api/resources/open", { method: "POST", body: { path: page.path } }); }
          catch (error) { if (current === view) status.textContent = errorText(error, "无法打开资源"); }
          finally { node.disabled = false; }
        }));
      }
      const next = page?.kind === "directory" ? page.next_offset : page?.next_line;
      if (next != null && next > 0) footer.append(button("继续读取", "resource-more", () => load(view, true)));
      layer.replaceChildren(head, subtitle, address, view.body, footer);
      layer.hidden = false; position();
      if (focused && focused !== layer) {
        const replacement = focused.isConnected ? focused : focused.id ? layer.querySelector("#" + CSS.escape(focused.id)) : null;
        (replacement || layer).focus({ preventScroll: true });
      }
    }
    function appendPage(view, page) {
      if (page.kind === "directory") {
        for (const entry of page.entries) {
          const row = button("", "", () => {
            view.scrollTop = view.body.scrollTop; history.push(view);
            const separator = page.path.includes("\\") && !page.path.includes("/") ? "\\" : "/";
            const path = page.path.replace(/[/\\]$/, "") + separator + entry.name;
            show({ path, line: 1 });
          }, "resource-entry");
          row.append(icon(entry.kind === "directory" ? "folder" : entry.kind === "link" ? "link" : "file-text"), el("span", "resource-entry-name", entry.name), icon("chevron"));
          view.body.append(row);
        }
        if (!page.entries.length && !view.body.childNodes.length) view.body.append(el("p", "resource-empty", "这个目录是空的"));
      } else if (page.kind === "file" && !page.preview_error) {
        let file = view.body.querySelector(".resource-file");
        if (!file) { file = el("div", "resource-file"); view.body.append(file); }
        const lines = page.text.split(/\r?\n/); if (page.text.endsWith("\n")) lines.pop();
        lines.forEach((text, index) => {
          const line = page.start_line + index, node = el("div", "resource-line"); node.dataset.line = String(line);
          if (line >= view.target.line && line <= view.target.endLine) node.classList.add("is-highlight");
          node.append(el("span", "resource-line-number", String(line)), el("span", "resource-line-text", text || " ")); file.append(node);
        });
        if (!page.text && !file.childNodes.length) view.body.append(el("p", "resource-empty", "这个文件是空的"));
      } else if (page.preview_error || page.kind === "unsupported") view.body.append(el("p", "resource-empty", page.preview_error || "这种资源无法预览"));
    }
    async function load(view, more = false) {
      const returnFocus = more && document.activeElement === layer.querySelector("#resource-more");
      if (returnFocus) layer.focus({ preventScroll: true });
      cancel(); const id = request; controller = new AbortController();
      const query = new URLSearchParams({ path: view.target.path, line: String(more ? view.page.next_line || 1 : view.target.line) });
      if (more && view.page.kind === "directory") query.set("offset", String(view.page.next_offset));
      if (!more) { view.body.replaceChildren(el("p", "resource-empty", "正在读取…")); render(); }
      else layer.querySelector("#resource-more").disabled = true;
      try {
        const page = await api("/api/resources?" + query, { signal: controller.signal });
        if (request !== id || current !== view || layer.hidden) return;
        const top = view.body.scrollTop;
        if (!more) view.body.replaceChildren();
        view.page = page; appendPage(view, page); render(); view.body.scrollTop = top;
        if (returnFocus && document.activeElement === layer) (layer.querySelector("#resource-more") || view.body).focus({ preventScroll: true });
        if (!more && page.kind === "file") {
          const row = view.body.querySelector('.resource-line[data-line="' + view.target.line + '"]');
          if (row) view.body.scrollTop = Math.max(0, row.offsetTop - view.body.offsetTop);
        }
      } catch (error) {
        if (request !== id || current !== view || layer.hidden) return;
        if (!more) view.body.replaceChildren();
        view.body.append(el("p", "resource-error", errorText(error, "无法读取资源")), button("重试", "resource-retry", () => load(view, more)));
        if (more) layer.querySelector("#resource-more").disabled = false;
        if (returnFocus && document.activeElement === layer) view.body.querySelector("#resource-retry").focus({ preventScroll: true });
        position();
      }
    }
    function show(target) {
      cancel(); current = { target, page: null, body: el("div", "resource-body"), scrollTop: 0 };
      current.body.tabIndex = 0; load(current); layer.focus({ preventScroll: true });
    }
    function open(target, trigger) {
      close(); origin = trigger; origin.setAttribute("aria-expanded", "true"); read(); show(target);
    }
    function source(value, trigger) {
      close(); origin = trigger; origin.setAttribute("aria-expanded", "true"); read();
      current = { target: { path: value.url, line: 1 }, page: { kind: "source", path: value.url, name: value.title || new URL(value.url).hostname }, body: el("div", "resource-body") };
      render(); layer.focus({ preventScroll: true });
    }
    layer.addEventListener("click", ev => ev.stopPropagation());
    menu.addEventListener("click", ev => ev.stopPropagation());
    document.addEventListener("click", ev => {
      if (!layer.hidden && !ev.composedPath().includes(layer) && !ev.composedPath().includes(origin)) close();
      if (!ev.composedPath().includes(menu)) menu.hidden = true;
    });
    document.addEventListener("keydown", ev => {
      if (ev.key !== "Escape") return;
      if (!menu.hidden) { menu.hidden = true; ev.preventDefault(); ev.stopImmediatePropagation(); }
      else if (!layer.hidden) { close(true); ev.preventDefault(); ev.stopImmediatePropagation(); }
    }, true);
    document.addEventListener("contextmenu", ev => {
      const trigger = ev.target.closest(".resource-link[data-path]"); if (!trigger) return;
      ev.preventDefault(); ev.stopImmediatePropagation();
      const destination = trigger._resourceTarget, status = el("span", "resource-copy-status");
      menu.replaceChildren(button("查看", "", () => { menu.hidden = true; open(destination, trigger); }), button(destination.line > 1 ? "复制引用" : "复制路径", "", ev => copyAddress(referenceAddress(destination), ev.currentTarget, status)), status);
      menu.hidden = false;
      menu.style.left = Math.min(ev.clientX, innerWidth - menu.offsetWidth) + "px"; menu.style.top = Math.min(ev.clientY, innerHeight - menu.offsetHeight) + "px";
    }, true);
    addEventListener("resize", position);
    addEventListener("scroll", position, true);
    return { open, source, close };
  }
  window.XBotResources = { target, create };
})();
