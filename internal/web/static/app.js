(function () {
  if (getComputedStyle(document.documentElement).getPropertyValue("--mygo-titlebar-height").trim()) {
    document.documentElement.classList.add("in-app");
  }
  const $ = (id) => document.getElementById(id);
  const login = $("login");
  const shell = $("shell");
  const sidebar = $("sidebar");
  const backdrop = $("backdrop");
  const thread = $("thread");
  const notesEl = $("notes");
  const input = $("input");
  const titleEl = $("title");
  const subtitle = $("subtitle");
  const bellDot = $("bell-dot");
  const panel = $("panel");
  const panelBody = $("panel-body");
  const modal = $("modal");
  const modalForm = $("modal-form");
  const modalFields = $("modal-fields");
  const modalError = $("modal-error");
  const composer = $("composer");
  const lightbox = $("lightbox");

  const state = {
    bots: [],
    folders: [],
    groups: [],
    others: null,
    notes: [],
    sel: null,
    sig: "",
    streaming: false,
    pendingSends: [],
    replyErrors: [],
    firstRender: true,
    threadLoad: 0,
    panelLoad: 0,
    knownNotes: null,
    owner: "",
    preview: {},
    previewAt: {},
    listSig: "",
    tab: "details",
    panelData: null,
    panelTouched: false,
    editOpen: false,
    messages: [],
    traceOpen: {},
    recentTasksOpen: new Set(),
    traceOut: {},
    peer: null,
    replyTo: null,
    placeholder: "写一条消息",
    modelCatalog: null,
    push: null,
  };

  // Soft blobs: near-circles with an irregular edge. No eyes, no smile.
  const BLOBS = [
    "M40 8C56 5 72 16 73 34C74 53 61 72 42 73C22 74 7 60 8 40C9 20 24 11 40 8Z",
    "M38 7C55 4 71 15 74 33C77 52 62 73 41 74C20 75 6 59 7 39C8 19 21 10 38 7Z",
    "M42 6C59 8 74 20 73 38C72 57 58 74 39 73C19 72 6 57 8 38C10 18 25 4 42 6Z",
    "M36 9C52 5 70 14 73 32C76 51 63 73 43 74C21 75 6 61 7 41C8 21 20 13 36 9Z",
    "M41 7C58 6 73 18 74 36C75 55 60 74 40 73C19 72 5 56 8 37C11 17 24 8 41 7Z",
    "M39 8C54 4 71 17 73 35C75 54 59 75 38 73C17 71 6 55 9 36C12 17 24 12 39 8Z",
  ];

  const THEME_MOON = '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>';
  const THEME_SUN = '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>';

  function applyTheme(theme) {
    const dark = theme === "dark";
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
    const meta = $("theme-color");
    if (meta) meta.setAttribute("content", dark ? "#161618" : "#ffffff");
    const themeIcon = $("theme-icon");
    if (themeIcon) themeIcon.innerHTML = dark ? THEME_SUN : THEME_MOON;
    const accountTheme = $("account-theme");
    if (accountTheme) accountTheme.replaceChildren(icon(dark ? "sun" : "moon"), document.createTextNode("外观"));
    try { localStorage.setItem("xbot-theme", dark ? "dark" : "light"); } catch (e) {}
  }

  function initTheme() {
    let theme = "light";
    try { theme = localStorage.getItem("xbot-theme") || ""; } catch (e) {}
    if (theme !== "light" && theme !== "dark") {
      theme = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    applyTheme(theme);
  }

  let authenticated = false;

  function visibleError(e, fallback) {
    if (e && e.status >= 500) return "XBot 出错了，稍后再试";
    if (e instanceof TypeError) return "连不上 XBot";
    const msg = String((e && e.message) || "");
    return /[㐀-鿿]/.test(msg) ? msg : (fallback || "操作失败，稍后再试");
  }

  async function apiFetch(path, opt) {
    let res;
    try { res = await fetch(path, opt); } catch (e) { throw new Error("连不上 XBot"); }
    if (res.status === 401 && authenticated) showLogin(true);
    if (res.status >= 500) {
      const err = new Error("XBot 出错了，稍后再试");
      err.status = res.status;
      throw err;
    }
    return res;
  }

  async function api(path, opt) {
    opt = opt || {};
    const res = await apiFetch(path, {
      method: opt.method || "GET",
      credentials: "same-origin",
      headers: opt.body ? { "Content-Type": "application/json" } : undefined,
      body: opt.body ? JSON.stringify(opt.body) : undefined,
    });
    const text = await res.text();
    let data = {};
    if (text) {
      try { data = JSON.parse(text); } catch (e) { data = { error: text }; }
    }
    if (res.status === 401) {
      const err = new Error(data.error || "unauthorized");
      err.status = 401;
      throw err;
    }
    if (!res.ok) {
      const err = new Error(data.error || "request failed");
      err.status = res.status;
      throw err;
    }
    return data;
  }

  // ---- Background tasks: header badge + popover (F5) ----
  const BG_LAYERS = '<path d="M12 3l9 5-9 5-9-5 9-5z"/><path d="M3 13l9 5 9-5"/>';
  function bgIcon(paths, size, extra) {
    const s = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    s.setAttribute("viewBox", "0 0 24 24");
    s.setAttribute("width", size || 16);
    s.setAttribute("height", size || 16);
    s.setAttribute("fill", "none");
    s.setAttribute("stroke", "currentColor");
    s.setAttribute("stroke-width", "2");
    s.setAttribute("stroke-linecap", "round");
    s.setAttribute("stroke-linejoin", "round");
    if (extra) s.setAttribute("class", extra);
    s.innerHTML = paths;
    return s;
  }
  const BG_ICONS = {
    spin: '<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>',
    queued: '<circle cx="12" cy="12" r="8.5" stroke-dasharray="3 3.2"/>',
    ok: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    err: '<path d="M6 6l12 12M18 6L6 18"/>',
    pause: '<circle cx="12" cy="12" r="8.5"/><path d="M9.5 9v6M14.5 9v6"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    steer: '<path d="M21 12a8.5 8.5 0 0 1-12.6 7.4L3 21l1.6-5.4A8.5 8.5 0 1 1 21 12z"/><path d="M12 8.5v7M8.5 12h7"/>',
    stop: '<rect x="6.5" y="6.5" width="11" height="11" rx="2.5" fill="currentColor" stroke="none"/>',
    up: '<path d="M12 19V5M5 12l7-7 7 7"/>',
  };
  function bgFrom() {
    const d = el("div", "bg-from");
    d.append(bgIcon(BG_LAYERS, 12), document.createTextNode("后台"));
    return d;
  }
  const bgState = { data: null, open: false, steerOpen: {}, timer: 0 };
  function bgBot() { return state.sel && state.sel.type === "bot" ? state.sel.id : ""; }
  async function bgRefresh() {
    const name = bgBot();
    const badge = document.getElementById("bg-badge");
    if (!badge) return;
    if (!name) { badge.hidden = true; bgClose(); return; }
    let data;
    try {
      data = await api("/api/bots/" + encodeURIComponent(name) + "/backgrounds");
    } catch (e) { return; }
    if (bgBot() !== name) return;
    if (bgState.name !== name) { bgState.steerOpen = {}; bgState.force = true; }
    bgState.data = data;
    bgState.name = name;
    bgRender();
  }
  function bgTick() {
    clearTimeout(bgState.timer);
    bgState.timer = setTimeout(async () => {
      if (!document.hidden) await bgRefresh();
      bgTick();
    }, 2000);
  }
  function bgClose() {
    bgState.open = false;
    const pop = document.getElementById("bg-pop");
    if (pop) pop.hidden = true;
    const dim = document.getElementById("bg-dim");
    if (dim) dim.hidden = true;
    const badge = document.getElementById("bg-badge");
    if (badge) badge.classList.remove("on");
  }
  async function bgAct(path, method, body) {
    bgState.force = true;
    try { await api(path, { method: method, body: body }); } catch (e) { setStatus(visibleError(e), true); }
    await bgRefresh();
  }
  function bgClock(t) {
    const d = new Date(t);
    if (isNaN(d)) return "";
    return String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
  }
  function bgRow(cls, icon, title, sub, acts) {
    const li = el("li", "bg-pr " + cls);
    const rt = el("div", "bg-rt");
    const st = el("span", "bg-st");
    st.append(bgIcon(BG_ICONS[icon], 16, icon === "spin" ? "bg-spin" : ""));
    const tx = el("div", "bg-tx");
    tx.append(el("div", "bg-tt", title), el("div", "bg-sub", sub));
    const a = el("span", "bg-acts");
    (acts || []).forEach((b) => a.append(b));
    rt.append(st, tx, a);
    li.append(rt);
    return li;
  }
  function bgBtn(icon, label, fn) {
    const b = el("button", "bg-ib");
    b.type = "button";
    b.title = label;
    b.setAttribute("aria-label", label);
    b.append(bgIcon(BG_ICONS[icon], 15));
    b.addEventListener("click", (ev) => { ev.stopPropagation(); fn(); });
    return b;
  }
  function bgRender() {
    const d = bgState.data || {};
    const running = d.backgrounds || [];
    const queue = d.queue || [];
    const finished = (d.finished || []).slice(0, 3);
    const controls = d.controls || [];
    const name = bgState.name || "";
    if (name !== bgBot()) { bgClose(); return; }
    const base = "/api/bots/" + encodeURIComponent(name);
    const badge = document.getElementById("bg-badge");
    badge.hidden = !running.length && !queue.length;
    badge.querySelector(".bg-n").textContent = "后台 " + running.length;
    // An open popover stays open after the last task ends, so its result can be read.
    const pop = document.getElementById("bg-pop");
    if (!bgState.open) return;
    // Do not rebuild under the owner's fingers: a poll would wipe a half-typed steer.
    const typing = pop.querySelector(".bg-addin input");
    if (typing && (typing === document.activeElement || typing.value) && !bgState.force) return;
    bgState.force = false;
    const taskOf = {};
    running.forEach((r) => { taskOf[r.id] = r.task; });
    const head = el("div", "bg-ph");
    head.append(el("b", "", "后台任务"), el("span", "bg-cnt", running.length + " 运行 · " + queue.length + " 排队"));
    head.append(bgBtn("err", "关闭", bgClose));
    const list = el("ul", "bg-pl");
    running.forEach((r) => {
      const li = bgRow("run", "spin", r.task || r.id, "第 " + (r.steps || 0) + " 步" + (r.last_tool ? " · " + r.last_tool : ""), [
        bgBtn("steer", "追加指示", () => { bgState.steerOpen[r.id] = !bgState.steerOpen[r.id]; bgState.focusSteer = r.id; bgState.force = true; bgRender(); }),
        bgBtn("stop", "取消", () => bgAct(base + "/agents/" + encodeURIComponent(r.id) + "/cancel", "POST")),
      ]);
      const steers = controls.filter((c) => c.background === r.id && c.kind === "steer");
      if (steers.length) {
        const ins = el("ul", "bg-ins");
        steers.forEach((c) => {
          const on = c.state === "applied";
          const it = el("li", on ? "on" : "");
          it.append(bgIcon(BG_ICONS[on ? "ok" : c.state === "dropped" ? "err" : "clock"], 13), el("span", "x", c.text), el("span", "r", on ? "已生效" : c.state === "dropped" ? "已丢弃" : "待生效"));
          ins.append(it);
        });
        li.append(ins);
      }
      if (bgState.steerOpen[r.id]) {
        const f = el("form", "bg-addin");
        const inp = el("input");
        inp.placeholder = "追加指示，下一步生效…";
        const go = el("button", "bg-send");
        go.type = "submit";
        go.append(bgIcon(BG_ICONS.up, 14));
        f.append(inp, go);
        f.addEventListener("submit", (ev) => {
          ev.preventDefault();
          const t = inp.value.trim();
          if (t) bgAct(base + "/agents/" + encodeURIComponent(r.id) + "/steer", "POST", { text: t });
        });
        li.append(f);
        if (bgState.focusSteer === r.id) { bgState.focusSteer = ""; setTimeout(() => inp.focus(), 0); }
      }
      list.append(li);
    });
    queue.forEach((q) => {
      let sub = "等待空位 · 第 " + q.position + " 位";
      if (q.reason === "conflict") sub = "等「" + (taskOf[q.background] || q.background) + "」释放 " + (q.name || "") + " · 第 " + q.position + " 位";
      if (q.reason === "queue") sub = "排在「" + (taskOf[q.background] || q.background) + "」之后 · 第 " + q.position + " 位";
      list.append(bgRow("q", "queued", q.text || q.id, sub, [
        bgBtn("stop", "取消", () => bgAct(base + "/queue/" + encodeURIComponent(q.id), "DELETE")),
      ]));
    });
    const parts = [head, list];
    if (finished.length) {
      const done = el("ul", "bg-pl");
      finished.forEach((f) => {
        const map = {
          done: ["ok", "ok", "完成 · " + (f.steps || 0) + " 步" + (f.ended ? " · " + bgClock(f.ended) : "")],
          failed: ["err", "err", "失败"],
          cancelled: ["int", "pause", "已取消"],
          interrupted: ["int", "pause", "已中断"],
        }[f.status] || ["int", "pause", f.status || ""];
        done.append(bgRow(map[0], map[1], f.task || f.id, map[2]));
      });
      parts.push(el("p", "bg-sec", "最近结束"), done);
    }
    pop.replaceChildren(...parts);
  }
  function bgSetup() {
    const badge = document.getElementById("bg-badge");
    if (!badge) return;
    badge.prepend(bgIcon(BG_LAYERS, 12));
    const dim = el("div", "bg-dim");
    dim.id = "bg-dim";
    dim.hidden = true;
    const pop = el("div", "bg-pop");
    pop.id = "bg-pop";
    pop.hidden = true;
    document.querySelector(".main").append(dim, pop);
    badge.addEventListener("click", (ev) => {
      ev.stopPropagation();
      bgState.open = !bgState.open;
      pop.hidden = !bgState.open;
      dim.hidden = !bgState.open;
      badge.classList.toggle("on", bgState.open);
      bgState.force = true;
      bgRender();
    });
    dim.addEventListener("click", bgClose);
    document.addEventListener("click", (ev) => { if (bgState.open && !pop.contains(ev.target)) bgClose(); });
    document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && bgState.open) bgClose(); });
    document.addEventListener("visibilitychange", () => { if (!document.hidden) bgRefresh(); });
    bgTick();
  }

  // ---- Bot status: sidebar second line + step-flow popover (bot-status prototype) ----
  const stStatus = { map: {} };
  const stpState = { ready: false, open: false, bot: "", row: null, steps: null };

  function tsOf(v) {
    if (v == null) return 0;
    const n = Number(v);
    if (typeof v !== "string" || /^\d+$/.test(v.trim())) {
      if (!Number.isFinite(n)) return 0;
      return n > 0 && n < 1e12 ? n * 1000 : n;
    }
    const t = new Date(v).getTime();
    return Number.isNaN(t) ? 0 : t;
  }

  function spinIcon(size) {
    const s = rawSvg('<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>', size, 2.8);
    s.classList.add("st-spin");
    return s;
  }

  function stNodePrefix(st) {
    const node = st && st.node ? String(st.node).trim() : "";
    return node && node !== "local" ? node + " · " : "";
  }

  // The second line is owned by the status poll; rows only record a signature so an
  // unchanged line is never rewritten and no row is touched without a change.
  function stDesc(name) {
    const st = stStatus.map[name] || null;
    const kind = st ? String(st.state || "idle") : "idle";
    const pre = stNodePrefix(st);
    let sig = kind + "|" + pre;
    const line = el("span", "preview");
    if (kind === "running") {
      line.className = "preview st run click";
      line.append(spinIcon(11));
      const text = el("span");
      text.append(pre + "在跑 ");
      const tool = st && st.tool ? String(st.tool) : "";
      if (tool) text.append(el("code", "", tool));
      line.append(text);
      line.addEventListener("click", (ev) => { ev.stopPropagation(); toggleStp(name, line); });
      sig += tool;
    } else if (kind === "awaiting") {
      line.className = "preview st wait click";
      line.append(el("i", "wd"));
      line.append(el("span", "", pre + "等你确认"));
      line.addEventListener("click", (ev) => { ev.stopPropagation(); jumpToApproval(name); });
    } else if (kind === "offline") {
      line.className = "preview st off";
      const mins = Math.round(Number(st && st.stale_seconds) / 60) || 1;
      line.append(el("span", "", pre + "离线 · 最后状态 " + mins + " 分钟前"));
      sig += mins;
    } else {
      const bot = (state.bots || []).find((b) => b.name === name);
      const text = realPreview(st && st.last_message) || (bot ? lineForBot(bot) : "") || "没有消息";
      line.className = "preview";
      line.append(el("span", "", pre + text));
      sig += text;
    }
    return { sig: sig, node: line, off: kind === "offline" };
  }

  function otherLabel(o) {
    const s = String((o && o.state) || "");
    if (s === "running" || s === "awaiting" || s === "busy") return "在忙";
    if (s === "away" || s === "offline" || s === "later" || s === "dnd") return "稍后回复";
    return "空闲";
  }

  function othersFolder() {
    const box = el("div", "folder");
    const collapsed = folderCollapsed("别人的");
    const head = el("div", "folder-head");
    const toggle = el("button", "folder-toggle");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    toggle.append(icon(collapsed ? "chevron" : "chevronDown"));
    const name = el("span", "folder-name");
    const lock = rawSvg('<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>', 11, 2.2);
    lock.classList.add("lk");
    name.append(lock, document.createTextNode("别人的"));
    toggle.append(name, el("span", "folder-count", String(state.others.length)));
    toggle.addEventListener("click", () => {
      closeFolderMenus();
      setFolderCollapsed("别人的", !folderCollapsed("别人的"));
      state.listSig = "";
      renderLists();
    });
    head.append(toggle);
    box.append(head);
    const body = el("div", "folder-bots");
    body.hidden = collapsed;
    for (const o of state.others) {
      const slug = (o && o.bot) || "?";
      const row = el("div", "item other");
      row.dataset.obot = slug;
      row.dataset.osig = otherLabel(o);
      row.append(avatar(slug, null));
      const wrap = el("span", "item-body");
      const line = el("span", "name-line");
      line.append(el("span", "name", (o && o.name) || niceName(slug)));
      const pv = el("span", "preview st");
      pv.append(el("span", "", row.dataset.osig));
      wrap.append(line, pv);
      row.append(wrap);
      body.append(row);
    }
    box.append(body);
    return box;
  }


  // Remote same-owner bots only appear in /api/bots/status (with a node name).
  // Keep one row per remote bot under a folder named after the node.
  function ensureRemoteRows() {
    const roster = $("roster");
    if (!roster) return;
    const local = new Set((state.bots || []).map((b) => b.name));
    const remotes = Object.values(stStatus.map).filter((st) => st && st.node && st.bot && !local.has(st.bot));
    // Drop remote folders when empty.
    roster.querySelectorAll(".folder[data-remote-node]").forEach((f) => {
      const node = f.dataset.remoteNode;
      if (!remotes.some((st) => st.node === node)) f.remove();
    });
    const byNode = {};
    remotes.forEach((st) => { (byNode[st.node] || (byNode[st.node] = [])).push(st); });
    for (const [node, list] of Object.entries(byNode)) {
      let folder = roster.querySelector('.folder[data-remote-node="' + CSS.escape(node) + '"]');
      if (!folder) {
        folder = el("div", "folder");
        folder.dataset.remoteNode = node;
        const head = el("div", "folder-head");
        const toggle = el("button", "folder-toggle");
        toggle.type = "button";
        toggle.setAttribute("aria-expanded", "true");
        toggle.append(icon("chevronDown"));
        const name = el("span", "folder-name", node);
        toggle.append(name, el("span", "folder-count", String(list.length)));
        toggle.addEventListener("click", () => {
          const body = folder.querySelector(".folder-bots");
          const open = body.hidden;
          body.hidden = !open;
          toggle.setAttribute("aria-expanded", open ? "true" : "false");
        });
        head.append(toggle);
        folder.append(head, el("div", "folder-bots"));
        // Insert before the locked "别人的" folder if present, else at end.
        const others = [...roster.querySelectorAll(".folder")].find((f) => f.querySelector(".folder-name")?.textContent?.includes("别人的"));
        if (others) roster.insertBefore(folder, others);
        else roster.append(folder);
      } else {
        const count = folder.querySelector(".folder-count");
        if (count) count.textContent = String(list.length);
      }
      const body = folder.querySelector(".folder-bots");
      const have = new Set([...body.querySelectorAll(".item[data-bot]")].map((r) => r.dataset.bot));
      list.forEach((st) => {
        if (have.has(st.bot)) return;
        const row = el("div", "item remote");
        row.dataset.bot = st.bot;
        row.append(avatar(st.bot, null));
        const wrap = el("span", "item-body");
        const line = el("span", "name-line");
        line.append(el("span", "name", niceName(st.bot)));
        wrap.append(line, el("span", "preview"));
        row.append(wrap);
        row.addEventListener("click", (ev) => {
          ev.stopPropagation();
          setTip(row, "在节点 " + node + " 上");
        });
        body.append(row);
      });
      // Remove rows no longer present.
      body.querySelectorAll(".item[data-bot]").forEach((r) => {
        if (!list.some((st) => st.bot === r.dataset.bot)) r.remove();
      });
    }
  }

  function paintStatus() {
    const roster = $("roster");
    roster.querySelectorAll(".item[data-bot]").forEach((row) => {
      const d = stDesc(row.dataset.bot);
      if (row.dataset.stsig === d.sig) return;
      row.dataset.stsig = d.sig;
      row.classList.toggle("off", d.off);
      const old = row.querySelector(".preview");
      if (old) old.replaceWith(d.node);
    });
    const bySlug = {};
    (state.others || []).forEach((o) => { if (o && o.bot) bySlug[o.bot] = o; });
    roster.querySelectorAll(".item[data-obot]").forEach((row) => {
      const label = otherLabel(bySlug[row.dataset.obot]);
      if (row.dataset.osig === label) return;
      row.dataset.osig = label;
      const text = row.querySelector(".preview span");
      if (text) text.textContent = label;
    });
    sortStatusRows();
  }

  // Running/awaiting first, stable otherwise. Only out-of-place rows are moved.
  function sortStatusRows() {
    const hot = (row) => {
      const st = stStatus.map[row.dataset.bot];
      return st && (st.state === "running" || st.state === "awaiting") ? 0 : 1;
    };
    document.querySelectorAll("#roster .folder-bots").forEach((body) => {
      const rows = Array.from(body.children).filter((r) => r.dataset.bot);
      if (rows.length < 2) return;
      const sorted = rows.slice().sort((a, b) => hot(a) - hot(b));
      let ref = body.firstChild;
      for (const row of sorted) {
        if (row !== ref) body.insertBefore(row, ref);
        ref = row.nextSibling;
      }
    });
  }

  async function refreshStatus() {
    const data = await api("/api/bots/status");
    const map = {};
    for (const b of (data && data.bots) || []) if (b && b.bot) map[b.bot] = b;
    stStatus.map = map;
    state.others = data && Array.isArray(data.others) ? data.others : null;
    renderLists();
    ensureRemoteRows();
    paintStatus();
    if (stpState.open) stpRefresh().catch(() => {});
  }

  async function jumpToApproval(name) {
    closeStp();
    try {
      if (state.sel && state.sel.type === "bot" && state.sel.id === name) await loadThread();
      else await selectBot(name);
    } catch (e) {}
    const find = () => thread.querySelector(".approval") || document.querySelector("#thread .approval") || document.querySelector("#node-pending .approval");
    let card = find();
    if (!card) {
      await new Promise((r) => setTimeout(r, 50));
      card = find();
    }
    if (card) {
      card.scrollIntoView({ block: "center", behavior: "smooth" });
      return;
    }
    const lines = thread.querySelectorAll(".approval-line");
    if (lines.length) lines[lines.length - 1].scrollIntoView({ block: "center", behavior: "smooth" });
  }

  function stpSetup() {
    if (stpState.ready) return;
    stpState.ready = true;
    const shade = el("div", "mshade");
    shade.id = "mshade";
    shade.hidden = true;
    const pop = el("div", "stp");
    pop.id = "stp";
    pop.hidden = true;
    document.body.append(shade, pop);
    shade.addEventListener("click", closeStp);
    document.addEventListener("click", (ev) => {
      if (stpState.open && !pop.contains(ev.target) && !(ev.target instanceof Element && ev.target.closest(".preview.st"))) closeStp();
    });
    document.addEventListener("visibilitychange", () => { if (!document.hidden) refreshStatus().catch(() => {}); });
    window.addEventListener("resize", () => { if (stpState.open) placeStp(); });
  }

  function closeStp() {
    if (!stpState.open) return;
    stpState.open = false;
    stpState.bot = "";
    stpState.row = null;
    stpState.steps = null;
    const pop = document.getElementById("stp");
    if (pop) pop.hidden = true;
    const shade = document.getElementById("mshade");
    if (shade) shade.hidden = true;
  }

  function toggleStp(name, line) {
    if (stpState.open && stpState.bot === name) { closeStp(); return; }
    openStp(name, line).catch(() => {});
  }

  async function openStp(name, line) {
    stpSetup();
    stpState.open = true;
    stpState.bot = name;
    stpState.row = line.closest(".item");
    stpState.steps = null;
    const pop = document.getElementById("stp");
    pop.hidden = false;
    stpPaint();
    placeStp();
    const data = await api("/api/bots/" + encodeURIComponent(name) + "/steps?n=20").catch(() => null);
    if (!stpState.open || stpState.bot !== name) return;
    stpState.steps = (data && data.steps) || [];
    stpPaint();
  }

  async function stpRefresh() {
    const name = stpState.bot;
    const st = name && stStatus.map[name];
    // A finished turn keeps its last steps on screen until the popover is closed.
    if (!stpState.open || !st || st.state !== "running") return;
    const data = await api("/api/bots/" + encodeURIComponent(name) + "/steps?n=20").catch(() => null);
    if (!stpState.open || stpState.bot !== name) return;
    stpState.steps = (data && data.steps) || [];
    stpPaint();
  }

  function stClock(sec) {
    const s = Math.max(0, Math.round(sec));
    return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
  }

  function stDu(sec) {
    if (sec < 10) return sec.toFixed(1) + "s";
    if (sec < 60) return Math.round(sec) + "s";
    return stClock(sec);
  }

  function stpPaint() {
    const pop = document.getElementById("stp");
    if (!pop || !stpState.open) return;
    const name = stpState.bot;
    const steps = stpState.steps;
    const head = el("div", "ph");
    const hi = el("span", "si");
    const live = stStatus.map[name] && stStatus.map[name].state === "running";
    hi.append(live ? spinIcon(14) : rawSvg('<path d="M5 12.5l4.5 4.5L19 7.5"/>', 14, 2.4));
    head.append(hi, el("b", "", niceName(name) + " · 本轮"));
    const cnt = el("span", "cnt");
    if (!steps) cnt.textContent = "读取中…";
    else if (!steps.length) cnt.textContent = "本轮还没有步骤";
    else {
      const st = stStatus.map[name];
      const t0 = (st && tsOf(st.since)) || tsOf(steps[0].time) || Date.now();
      cnt.textContent = "第 " + steps.length + " 步 · " + stClock((Date.now() - t0) / 1000);
    }
    head.append(cnt);
    const x = el("button", "x");
    x.type = "button";
    x.setAttribute("aria-label", "关闭");
    x.append(rawSvg('<path d="M6 6l12 12M18 6L6 18"/>', 16, 2.2));
    x.addEventListener("click", closeStp);
    head.append(x);
    const ol = el("ol");
    for (let i = 0; i < (steps || []).length; i++) {
      const step = steps[i];
      const run = /^(run|running|active)/i.test(String(step.state || ""));
      const li = el("li", run ? "run" : "ok");
      const si = el("span", "si");
      si.append(run ? spinIcon(14) : rawSvg('<path d="M5 12.5l4.5 4.5L19 7.5"/>', 14, 2.4));
      const sx = el("div", "sx");
      const sl = el("div", "sl");
      sl.append(el("b", "", String(step.tool || "tool")));
      // A "detail · result" summary splits into the mono chip and the gray line.
      const sum = String(step.summary || "").trim();
      const cut = sum.indexOf(" · ");
      const rest = cut > 0 ? sum.slice(cut + 3) : sum;
      if (cut > 0) sl.append(el("code", "", sum.slice(0, cut)));
      sx.append(sl);
      if (rest) sx.append(el("div", "sr", rest));
      li.append(si, sx);
      const t = tsOf(step.time);
      const tn = i + 1 < steps.length ? tsOf(steps[i + 1].time) : (run ? Date.now() : 0);
      if (t && tn > t) li.append(el("span", "du", stDu((tn - t) / 1000)));
      ol.append(li);
    }
    pop.replaceChildren(head, ol);
  }

  function placeStp() {
    const pop = document.getElementById("stp");
    if (!pop || !stpState.open) return;
    const mob = window.innerWidth <= 767;
    const shade = document.getElementById("mshade");
    if (shade) shade.hidden = !mob;
    pop.style.top = "";
    pop.style.left = "";
    if (mob) return;
    const row = stpState.row && stpState.row.isConnected ? stpState.row : null;
    const side = sidebar.getBoundingClientRect();
    let left = side.right + 4;
    let top = row ? row.getBoundingClientRect().top - 4 : side.top + 12;
    if (left + pop.offsetWidth > window.innerWidth - 8) left = Math.max(8, window.innerWidth - pop.offsetWidth - 8);
    if (top + pop.offsetHeight > window.innerHeight - 8) top = Math.max(8, window.innerHeight - pop.offsetHeight - 8);
    pop.style.left = left + "px";
    pop.style.top = top + "px";
  }


  function showLogin(expired = false) {
    if (expired && state.sel && state.sel.type === "bot") {
      try { sessionStorage.setItem("xbot-selected-bot", state.sel.id); } catch (e) {}
    }
    authenticated = false;
    closeModal();
    closePops();
    closeDrawer();
    closeStp();
    closeShots();
    bgClose();
    panel.classList.remove("open");
    $("panel-scrim").hidden = true;
    $("info").setAttribute("aria-expanded", "false");
    state.sel = null;
    ++state.threadLoad;
    ++state.panelLoad;
    setStatus("");
    const notice = $("login-notice");
    if (expired) notice.textContent = "登录已过期，重新输入登录码";
    notice.hidden = !notice.textContent;
    shell.hidden = true;
    login.hidden = false;
    $("token").value = "";
    $("token").focus();
  }

  function showApp() {
    authenticated = true;
    $("login-notice").textContent = "";
    $("login-notice").hidden = true;
    login.hidden = true;
    shell.hidden = false;
  }

  function el(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text != null) node.textContent = text;
    return node;
  }

  function setTip(node, text, placement) {
    if (!node) return;
    const value = text ? String(text) : "";
    if (value) node.dataset.tip = value;
    else delete node.dataset.tip;
    if (placement) node.dataset.tipPlacement = placement;
    else delete node.dataset.tipPlacement;
    node.removeAttribute("title");
  }

  function clip(s, n) {
    s = String(s || "").replace(/\s+/g, " ").trim();
    const chars = Array.from(s);
    if (chars.length <= n) return s;
    return chars.slice(0, n).join("") + "…";
  }

  function when(t) {
    if (!t) return "";
    const d = new Date(t);
    if (Number.isNaN(d.getTime())) return "";
    const hm = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
    const now = new Date();
    const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const days = Math.round((start(now) - start(d)) / 86400000);
    if (days === 0) return hm;
    if (days === 1) return "昨天 " + hm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日";
  }

  function cronText(cron, enabled) {
    const raw = String(cron || "").trim();
    const parts = raw.split(/\s+/);
    let label = "自定义时间";
    const number = (s, max, min = 0) => /^\d+$/.test(s) && Number(s) >= min && Number(s) <= max;
    if (parts.length === 5) {
      const [min, hour, dom, mon, dow] = parts;
      const daily = dom === "*" && mon === "*" && dow === "*";
      const step = (s, max) => /^\*\/\d+$/.test(s) && number(s.slice(2), max, 1);
      if (daily && hour === "*" && step(min, 59)) label = "每 " + Number(min.slice(2)) + " 分钟";
      else if (daily && min === "0" && step(hour, 23)) label = "每 " + Number(hour.slice(2)) + " 小时";
      else if (number(min, 59)) {
        const hours = hour.split(",");
        const range = hour.split("-");
        if (daily && min === "0" && hours.length > 1 && hours.every((h) => number(h, 23))) {
          label = "每天 " + hours.map(Number).join("、") + " 点";
        } else if (daily && min === "0" && range.length === 2 && range.every((h) => number(h, 23)) && Number(range[0]) <= Number(range[1])) {
          label = "每天 " + Number(range[0]) + "–" + Number(range[1]) + " 点每小时";
        } else if (number(hour, 23)) {
          const hm = Number(hour) + ":" + String(Number(min)).padStart(2, "0");
          const days = dow.split(",");
          const week = ["日", "一", "二", "三", "四", "五", "六", "日"];
          if (daily) label = "每天 " + hm;
          else if (mon === "*" && dom === "*" && (dow === "1-5" || dow === "1,2,3,4,5")) label = "工作日 " + hm;
          else if (mon === "*" && dom === "*" && days.every((d) => number(d, 7))) label = "每周" + days.map((d) => week[Number(d)]).join("、") + " " + hm;
          else if (mon === "*" && dow === "*" && number(dom, 31, 1)) label = "每月 " + Number(dom) + " 日 " + hm;
        }
      }
    }
    if (enabled === false) label += (label ? " · " : "") + "停用";
    return label;
  }

  function niceName(slug) {
    // Names keep their exact case; only surrounding whitespace is trimmed.
    return String(slug || "").trim();
  }

  function hashName(name) {
    let h = 2166136261;
    const s = String(name || "?");
    for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
    return h >>> 0;
  }

  const ICONS = {
    logout: '<path d="M9 4H4v16h5"/><path d="M9 12h12m-5-5 5 5-5 5"/>',
    moon: THEME_MOON,
    sun: THEME_SUN,
    sidebar: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',
    terminal: '<polyline points="4 17 10 11 4 5"/><path d="M12 19h8"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    chevron: '<path d="m9 18 6-6-6-6"/>',
    chevronLeft: '<path d="m15 18-6-6 6-6"/>',
    chevronDown: '<path d="m6 9 6 6 6-6"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    ellipsis: '<circle class="solid" cx="5" cy="12" r="1.5"/><circle class="solid" cx="12" cy="12" r="1.5"/><circle class="solid" cx="19" cy="12" r="1.5"/>',
    wrench: '<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
    pencil: '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>',
    trash: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/>',
    folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    reply: '<polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/>',
    bubble: '<path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  };

  function icon(name, extra) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.classList.add("icon");
    if (extra) svg.classList.add(extra);
    svg.innerHTML = ICONS[name] || ICONS.wrench;
    return svg;
  }

  function rawSvg(body, size, stroke) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("width", String(size));
    svg.setAttribute("height", String(size));
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", String(stroke));
    svg.setAttribute("stroke-linecap", "round");
    svg.setAttribute("stroke-linejoin", "round");
    svg.innerHTML = body;
    return svg;
  }

  const CHECK_PATH = '<path d="M5 12.5l4.5 4.5L19 7.5"/>';
  const X_PATH = '<path d="M6 6l12 12"/><path d="M18 6L6 18"/>';
  const CLOCK_PATH = '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>';
  const PENCIL_PATH = '<path d="M12 20h9"/><path d="M16.5 3.5a2.12 2.12 0 0 1 3 3L7 19l-4 1 1-4Z"/>';
  const SEND_PATH = '<path d="M12 19V5"/><path d="m5 12 7-7 7 7"/>';

  function markInitial(name, paint, size) {
    if (size === "xs") return "";
    const given = paint && typeof paint.initial === "string" ? paint.initial.trim() : "";
    const raw = String(name || "").trim();
    if (!raw) return "";
    const chars = Array.from(raw);
    if (/\p{Script=Han}|\p{Script=Hiragana}|\p{Script=Katakana}|\p{Script=Hangul}/u.test(chars[0])) return chars[0];
    if (/^[A-Za-z]{2}/.test(raw)) return chars[0].toUpperCase() + chars[1].toLowerCase();
    return Array.from(given || raw)[0].toLocaleUpperCase();
  }

  function faceFor(name, paint, size) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 80 80");
    svg.style.setProperty("--avatar-light", "#d3d3d3");
    svg.style.setProperty("--avatar-dark", "#3d3d3d");
    svg.style.setProperty("--avatar-letter-light", "#333333");
    svg.style.setProperty("--avatar-letter-dark", "#e4e4e4");
    svg.setAttribute("aria-hidden", "true");
    const blob = document.createElementNS("http://www.w3.org/2000/svg", "path");
    blob.setAttribute("d", BLOBS[hashName(name) % BLOBS.length]);
    blob.setAttribute("fill", "var(--avatar-fill)");
    svg.append(blob);
    const letter = markInitial(name, paint, size);
    if (letter) {
      const text = document.createElementNS("http://www.w3.org/2000/svg", "text");
      text.setAttribute("x", "40");
      text.setAttribute("y", "43");
      text.setAttribute("text-anchor", "middle");
      text.setAttribute("dominant-baseline", "central");
      text.setAttribute("fill", "var(--avatar-ink)");
      text.setAttribute("font-size", Array.from(letter).length > 1 ? "26" : "30");
      text.setAttribute("font-weight", "560");
      text.textContent = letter;
      svg.append(text);
    }
    return svg;
  }

  function avatar(name, paint, size) {
    paint = paint || {};
    const av = el("span", "avatar" + (size ? " " + size : ""));
    if (name) setTip(av, name, "right");
    av.append(faceFor(name, paint, size));
    if (paint.url) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = paint.url;
      img.addEventListener("load", () => av.classList.add("has-photo"));
      img.addEventListener("error", () => img.remove());
      av.append(img);
    }
    return av;
  }

  function paintOfBot(b) {
    if (!b) return null;
    return { bg: b.avatar_bg, ink: b.avatar_ink, url: b.avatar_url, initial: b.initial };
  }

  function paintOf(name) {
    return paintOfBot((state.bots || []).find((x) => x.name === name));
  }

  function copyText(text, btn) {
    const done = () => {
      if (!btn) return;
      btn.setAttribute("aria-label", "已复制");
      setTimeout(() => btn.setAttribute("aria-label", "复制"), 1200);
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done).catch(() => fallback());
      return;
    }
    fallback();
    function fallback() {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      document.body.append(ta);
      ta.select();
      try { document.execCommand("copy"); done(); } catch (e) {}
      ta.remove();
    }
  }

  function unwrap(content) {
    const text = content || "";
    const m = text.match(/^<untrusted_event\s+([^>]*)>\n([\s\S]*)\n<\/untrusted_event>$/);
    if (!m) return { text: text, untrusted: false, source: "", sender: "" };
    const source = /source="([^"]*)"/.exec(m[1]);
    const sender = /sender="([^"]*)"/.exec(m[1]);
    const type = /type="([^"]*)"/.exec(m[1]);
    return {
      type: type ? type[1] : "",
      text: m[2],
      untrusted: true,
      source: source ? source[1] : "",
      sender: sender ? sender[1] : "",
    };
  }

  function routineOf(m) {
    if (m.role !== "user") return null;
    const parsed = unwrap(m.content || "");
    if (!parsed.untrusted || parsed.source !== "timer") return null;
    const text = parsed.text.trim();
    if (text.startsWith("Missed routine times from")) {
      const items = text.match(/^- /gm) || [];
      const more = /^- … and (\d+) more routines/m.exec(text);
      const count = items.length + (more ? Number(more[1]) - 1 : 0);
      return { label: "错过的定时 · " + count + " 项", prompt: text };
    }
    const routine = /^<routine\b([^>]*)>([\s\S]*?)<\/routine>/.exec(text);
    if (!routine) return null;
    const name = /\bname="([^"]*)"/.exec(routine[1]);
    const id = /\bid="([^"]*)"/.exec(routine[1]);
    return {
      label: (name && name[1]) || (id && id[1]) || "定时",
      prompt: routine[2].trim(),
      time: when(m.time),
    };
  }

  // Drawers the owner opened stay open when a poll redraws the thread.
  const openRoutines = new Set();

  function routineLine(routine, key) {
    const row = el("details", "routine-line");
    if (key && openRoutines.has(key)) row.open = true;
    row.addEventListener("toggle", () => {
      if (!key) return;
      if (row.open) openRoutines.add(key);
      else openRoutines.delete(key);
    });
    const head = el("summary", "sysline");
    head.append(icon("clock"), el("span", "", routine.label + (routine.time ? " · " + routine.time : "")));
    const drawer = el("div", "routine-drawer");
    drawer.append(el("div", "routine-prompt", routine.prompt), el("div", "routine-source", "来源：定时"));
    row.append(head, drawer);
    return row;
  }

  function appendInline(parent, text) {
    const re = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]]+\]\(https?:\/\/[^\s)]+\))/g;
    let last = 0;
    let match;
    while ((match = re.exec(text))) {
      if (match.index > last) parent.append(document.createTextNode(text.slice(last, match.index)));
      const tok = match[0];
      if (tok.charAt(0) === "`") {
        parent.append(el("code", "", tok.slice(1, -1)));
      } else if (tok.startsWith("**")) {
        parent.append(el("strong", "", tok.slice(2, -2)));
      } else if (tok.charAt(0) === "*") {
        parent.append(el("em", "", tok.slice(1, -1)));
      } else {
        const lm = /^\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)$/.exec(tok);
        if (lm) {
          const a = document.createElement("a");
          a.textContent = lm[1];
          a.href = lm[2];
          a.rel = "noopener noreferrer";
          a.target = "_blank";
          parent.append(a);
        } else {
          parent.append(document.createTextNode(tok));
        }
      }
      last = match.index + tok.length;
    }
    if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
  }

  function appendBlocks(parent, text) {
    const lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
    let para = [];
    let list = null;
    const flushPara = () => {
      if (!para.length) return;
      const p = document.createElement("p");
      appendInline(p, para.join("\n"));
      parent.append(p);
      para = [];
    };
    const flushList = () => {
      if (!list) return;
      parent.append(list);
      list = null;
    };
    for (const line of lines) {
      const h = /^(#{1,3})\s+(.*)$/.exec(line);
      if (h) {
        flushPara(); flushList();
        const p = document.createElement("p");
        p.style.fontWeight = "600";
        appendInline(p, h[2]);
        parent.append(p);
        continue;
      }
      const li = /^[-*]\s+(.*)$/.exec(line);
      if (li) {
        flushPara();
        if (!list) list = document.createElement("ul");
        const row = document.createElement("li");
        appendInline(row, li[1]);
        list.append(row);
        continue;
      }
      if (line.trim() === "") { flushPara(); flushList(); continue; }
      flushList();
      para.push(line);
    }
    flushPara();
    flushList();
  }

  function highlight(codeEl, text, lang) {
    const use = /go|js|ts|java|py/.test(lang || "");
    if (!use) { codeEl.textContent = text; return; }
    const kw = new Set(["func", "return", "if", "else", "package", "import", "const", "let", "var", "for", "type", "string", "error"]);
    let i = 0;
    while (i < text.length) {
      const ch = text.charAt(i);
      if (ch === '"' || ch === "'" || ch === "`") {
        let j = i + 1;
        while (j < text.length && text.charAt(j) !== ch) {
          if (text.charAt(j) === "\\") j++;
          j++;
        }
        if (j < text.length) j++;
        codeEl.append(el("span", "str", text.slice(i, j)));
        i = j;
        continue;
      }
      if (text.startsWith("//", i) || (ch === "#" && (lang || "").indexOf("py") >= 0)) {
        let j = text.indexOf("\n", i);
        if (j < 0) j = text.length;
        codeEl.append(el("span", "cm", text.slice(i, j)));
        i = j;
        continue;
      }
      if (/[A-Za-z_]/.test(ch)) {
        let j = i + 1;
        while (j < text.length && /[A-Za-z0-9_]/.test(text.charAt(j))) j++;
        const word = text.slice(i, j);
        if (kw.has(word)) codeEl.append(el("span", "kw", word));
        else codeEl.append(document.createTextNode(word));
        i = j;
        continue;
      }
      codeEl.append(document.createTextNode(ch));
      i++;
    }
  }

  function codeBlock(lang, code) {
    const wrap = el("div", "code-block");
    const head = el("div", "code-head");
    head.append(el("span", "", (lang || "text").trim() || "text"));
    const btn = el("button", "copy", "复制");
    btn.type = "button";
    btn.addEventListener("click", () => copyText(code, btn));
    head.append(btn);
    const pre = document.createElement("pre");
    const codeEl = document.createElement("code");
    highlight(codeEl, code.replace(/\n$/, ""), (lang || "").trim());
    pre.append(codeEl);
    wrap.append(head, pre);
    return wrap;
  }

  function appendMarkdown(parent, text) {
    const src = String(text || "");
    const re = /```([^\n`]*)\n?([\s\S]*?)```/g;
    let last = 0;
    let match;
    while ((match = re.exec(src))) {
      if (match.index > last) appendBlocks(parent, src.slice(last, match.index));
      parent.append(codeBlock(match[1], match[2]));
      last = match.index + match[0].length;
    }
    if (last < src.length) appendBlocks(parent, src.slice(last));
  }

  function safeImage(url) {
    if (/^data:image\/(png|jpeg|jpg|gif|webp);base64,[a-z0-9+/=\s]+$/i.test(url)) return true;
    try {
      const u = new URL(url, location.origin);
      return u.protocol === "https:" || u.protocol === "http:";
    } catch (e) { return false; }
  }

  function pullImages(text) {
    const images = [];
    let rest = String(text || "").replace(/!\[([^\]]*)\]\(([^)\s]+)\)/g, (all, alt, url) => {
      if (safeImage(url)) { images.push(url); return ""; }
      return all;
    });
    rest = rest.replace(/(https?:\/\/[^\s)]+?\.(?:png|jpe?g|gif|webp))(?:\?[^\s)]*)?/gi, (url) => {
      if (safeImage(url)) { images.push(url); return ""; }
      return url;
    });
    return { text: rest.replace(/\n{3,}/g, "\n\n").trim(), images: images };
  }

  function gallery(urls) {
    const box = el("div", "gallery");
    const extra = urls.length > 3 ? urls.length - 2 : 0;
    const show = extra ? urls.slice(0, 3) : urls;
    show.forEach((url, i) => {
      const cell = el("button", "thumb");
      cell.type = "button";
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      img.addEventListener("error", () => { img.remove(); cell.classList.add("broken"); });
      cell.append(img);
      if (extra && i === show.length - 1) {
        cell.classList.add("more");
        const plus = el("span", "plus");
        plus.append(icon("plus"), el("span", "", String(extra)));
        cell.append(plus);
      }
      cell.addEventListener("click", () => openShots(urls));
      box.append(cell);
    });
    return box;
  }

  function openShots(urls) {
    const grid = el("div", "grid");
    for (const url of urls) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      grid.append(img);
    }
    lightbox.replaceChildren(grid);
    lightbox.hidden = false;
  }

  function closeShots() {
    lightbox.hidden = true;
    lightbox.replaceChildren();
  }

  const HEAT = [
    "010010100010010010",
    "021302110231021102",
    "132021302110320211",
    "213021132021103201",
    "102310211320211032",
    "021102130210211021",
    "010010010100010010",
  ];

  function heat() {
    const card = el("div", "heat-card");
    card.append(el("div", "heat-title", "活跃对话"));
    const months = el("div", "heat-months");
    months.append(el("span", "", "8月"), el("span", "", "9月"), el("span", "", "10月"));
    const wrap = el("div", "heat");
    wrap.setAttribute("aria-hidden", "true");
    const levels = ["", "l1", "l2", "l3"];
    for (const row of HEAT) {
      for (const ch of row) {
        const cell = document.createElement("i");
        const level = levels[Number(ch)] || "";
        if (level) cell.className = level;
        wrap.append(cell);
      }
    }
    card.append(months, wrap);
    return card;
  }

  function emptyState(heading, line, who) {
    const box = el("div", "empty");
    if (who) box.append(avatar(who, paintOf(who), "lg"));
    box.append(el("h2", "", heading), el("p", "", line), heat());
    return box;
  }

  function toolSummary(call) {
    let args = {};
    try { args = JSON.parse((call.function && call.function.arguments) || "{}"); } catch (e) {}
    const name = (call.function && call.function.name) || "tool";
    const bits = [];
    for (const key of Object.keys(args)) {
      const v = args[key];
      if (typeof v === "string" && v) bits.push(v.length > 80 ? v.slice(0, 80) + "…" : v);
    }
    return { name: name, args: args, line: bits.join(" · ") };
  }

  function peerPhrase(name, args, who) {
    const text = clip(args.text || args.prompt || "", 72);
    const from = niceName(who) || "Bot";
    if (name === "message_bot") {
      const to = args.bot || args.to || "";
      const line = !to
        ? from + (text ? " · " + text : " 发给了另一个 Bot")
        : from + " 发给 " + niceName(to) + (text ? " · " + text : "");
      return { text: line, peer: to };
    }
    if (name === "post_group") {
      const g = args.name || args.id || "群";
      return { text: from + " 发到 " + niceName(g) + (text ? " · " + text : ""), peer: "" };
    }
    if (name === "create_group") return { text: from + " 建了群" + (args.name ? " · " + niceName(args.name) : ""), peer: "" };
    if (name === "fork_bot") return { text: from + " 复刻出 " + niceName(args.name || "新 Bot"), peer: "" };
    return null;
  }

  function toolIconName(name) {
    return /shell|bash|exec|terminal/i.test(name || "") ? "terminal" : "wrench";
  }

  function closeToolPop() {
    const pop = document.querySelector(".tool-pop");
    if (pop) pop.remove();
    document.querySelectorAll(".card.open").forEach((node) => node.classList.remove("open"));
  }

  function openToolPop(anchor, text) {
    closeToolPop();
    const pop = el("div", "pop tool-pop");
    const pre = document.createElement("pre");
    pre.textContent = text;
    pop.append(pre);
    document.body.append(pop);
    const r = anchor.getBoundingClientRect();
    const width = Math.min(420, window.innerWidth - 24);
    let left = r.left;
    if (left + width > window.innerWidth - 12) left = window.innerWidth - 12 - width;
    if (left < 12) left = 12;
    pop.style.width = width + "px";
    pop.style.left = left + "px";
    let top = r.bottom + 6;
    if (top + 180 > window.innerHeight) top = Math.max(12, r.top - 186);
    pop.style.top = top + "px";
    anchor.classList.add("open");
  }

  function toolShell(name, line, resultText) {
    const card = el("button", "card");
    card.type = "button";
    const head = el("span", "card-head");
    const well = el("span", "card-icon");
    well.append(icon(toolIconName(name)));
    const text = el("span", "card-copy");
    text.append(el("div", "card-title", name || "tool"));
    if (line) text.append(el("div", "card-sub", line));
    head.append(well, text);
    if (resultText) head.append(icon("chevron", "chev"));
    card.append(head);
    if (!resultText) card.disabled = true;
    else {
      card.addEventListener("click", (ev) => {
        ev.stopPropagation();
        if (card.classList.contains("open")) closeToolPop();
        else openToolPop(card, resultText);
      });
    }
    return card;
  }

  function armCopy(row) {
    let timer = 0;
    row.addEventListener("pointerdown", () => {
      timer = window.setTimeout(() => row.classList.add("reveal"), 420);
    });
    const stop = () => window.clearTimeout(timer);
    row.addEventListener("pointerup", stop);
    row.addEventListener("pointercancel", stop);
    row.addEventListener("pointerleave", stop);
  }

  function copyBtn(text) {
    const btn = el("button", "copy");
    btn.type = "button";
    btn.setAttribute("aria-label", "复制");
    setTip(btn, "复制");
    btn.append(icon("copy"));
    btn.addEventListener("click", () => copyText(text || "", btn));
    return btn;
  }

  function systemLine(text, who, peer) {
    const row = el(peer ? "button" : "div", "sysline" + (peer ? " link" : ""));
    setTip(row, text);
    if (peer) {
      row.type = "button";
      row.dataset.peer = peer;
      row.addEventListener("click", () => openPeer(state.sel && state.sel.id, peer));
    }
    if (who) row.append(avatar(who, paintOf(who), "xs"));
    row.append(el("span", "label", text));
    return row;
  }

  function optionBadge(letter, ghost) {
    const badge = el("span", "opt-badge" + (ghost ? " ghost" : ""));
    if (ghost) badge.append(rawSvg(PENCIL_PATH, 14, 2.2));
    else badge.textContent = letter;
    return badge;
  }

  function optionCards(info, picked, question) {
    const card = el("div", "ask" + (picked != null ? " done" : ""));
    card.setAttribute("role", "radiogroup");
    if (question) card.setAttribute("aria-label", question);
    const opts = Array.isArray(info.args.options) ? info.args.options : [];
    const rows = [];
    let chosen = -1;
    opts.forEach((raw, i) => {
      let label = String(raw);
      let recommend = false;
      if (label.startsWith("* ")) { recommend = true; label = label.slice(2); }
      if (picked != null && chosen < 0 && (picked === label || picked === String(raw))) chosen = i;
      rows.push({ label: label, recommend: recommend });
    });
    if (question) card.append(el("p", "ask-q", question));
    if (picked != null) {
      const row = el("div", "ask-chosen");
      if (chosen >= 0) row.append(optionBadge(String.fromCharCode(65 + chosen)));
      else row.append(optionBadge("", true));
      row.append(el("span", "opt-t", chosen >= 0 ? rows[chosen].label : picked));
      const check = el("span", "ask-check");
      check.append(rawSvg(CHECK_PATH, 18, 2.4));
      row.append(check);
      card.append(row);
      return card;
    }
    const list = el("ul", "ask-opts");
    rows.forEach((row, i) => {
      const letter = String.fromCharCode(65 + i);
      const li = el("li", "ask-opt" + (row.recommend ? " recommend" : ""));
      li.tabIndex = 0;
      li.setAttribute("role", "radio");
      li.append(optionBadge(letter), el("span", "opt-t", row.label), el("span", "opt-kbd", letter));
      li.addEventListener("click", () => sendText(row.label));
      list.append(li);
    });
    const other = el("li", "ask-opt other");
    const field = document.createElement("input");
    field.placeholder = "其他…";
    field.setAttribute("aria-label", "其他");
    field.maxLength = 2000;
    const send = el("button", "ask-send");
    send.type = "button";
    send.setAttribute("aria-label", "发送");
    send.append(rawSvg(SEND_PATH, 14, 2.4));
    const submit = () => {
      const text = field.value.trim();
      if (text) sendText(text);
    };
    send.addEventListener("click", submit);
    field.addEventListener("keydown", (ev) => {
      if (ev.key === "Enter") { ev.preventDefault(); submit(); }
    });
    field.addEventListener("input", () => other.classList.toggle("typing", !!field.value.trim()));
    other.append(optionBadge("", true), field, send);
    list.append(other);
    card.addEventListener("keydown", (ev) => {
      if (ev.target.tagName === "INPUT") return;
      const items = Array.from(list.querySelectorAll(".ask-opt:not(.other)"));
      const i = items.indexOf(document.activeElement);
      if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
        ev.preventDefault();
        const n = (i + (ev.key === "ArrowDown" ? 1 : items.length - 1)) % items.length;
        items[n].focus();
      } else if (ev.key === "Enter" || ev.key === " ") {
        if (i >= 0) { ev.preventDefault(); sendText(rows[i].label); }
      } else if (/^[a-zA-Z]$/.test(ev.key)) {
        const hit = items.find((x) => x.querySelector(".opt-badge").textContent === ev.key.toUpperCase());
        if (hit) hit.focus();
      }
    });
    card.append(list);
    return card;
  }

  function laterUser(messages, index) {
    for (let i = index + 1; i < messages.length; i++) {
      if (messages[i].role !== "user") continue;
      const text = unwrap(messages[i].content || "").text.trim();
      // Card records share the user role; only a typed reply answers the card.
      if (text.startsWith("<approval_card") || text.startsWith("<coding_card")) continue;
      return text;
    }
    return null;
  }

  function appendRich(parent, text) {
    const src = String(text || "");
    const re = /```([^\n`]*)\n?([\s\S]*?)```/g;
    let last = 0;
    let match;
    const addText = (chunk) => {
      const pulled = pullImages(chunk);
      if (pulled.text) {
        const bubble = el("div", "bubble");
        const prose = el("div", "prose");
        appendMarkdown(prose, pulled.text);
        bubble.append(prose);
        parent.append(bubble);
      }
      if (pulled.images.length) parent.append(gallery(pulled.images));
    };
    while ((match = re.exec(src))) {
      if (match.index > last) addText(src.slice(last, match.index));
      parent.append(codeBlock(match[1], match[2]));
      last = match.index + match[0].length;
    }
    if (last < src.length) addText(src.slice(last));
  }

  function lineBySeq(messages, seq) {
    const n = Number(seq);
    if (!n) return null;
    for (const m of messages || []) {
      if (Number(m.seq) === n) return m;
    }
    return null;
  }

  function previewText(m) {
    if (!m) return "";
    const raw = m.role === "user" ? unwrap(m.content || "").text : (m.content || "");
    return String(raw).replace(/\r\n/g, "\n").trim();
  }

  function quoteLine(text) {
    return String(text || "")
      .replace(/```[\s\S]*?```/g, " ")
      .replace(/`([^`]*)`/g, "$1")
      .replace(/!\[[^\]]*\]\([^)]*\)/g, " ")
      .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/^\s{0,3}#{1,6}\s+/gm, "")
      .replace(/[*_~>]+/g, "")
      .replace(/^\s*[-*+]\s+/gm, "")
      .replace(/^\s*\d+[.)]\s+/gm, "")
      .replace(/\s+/g, " ")
      .trim();
  }

  function quoteSnippet(text) {
    const line = quoteLine(text);
    if (!line) return "“较早的消息";
    return "“" + line;
  }

  function quoteBlock(messages, seq) {
    const btn = el("button", "quote-block");
    btn.type = "button";
    btn.append(icon("bubble"));
    btn.append(el("span", "", quoteSnippet(previewText(lineBySeq(messages, seq)))));
    btn.addEventListener("click", (ev) => {
      ev.stopPropagation();
      jumpToSeq(seq);
    });
    return btn;
  }

  function jumpToSeq(seq) {
    const row = thread.querySelector('.msg[data-seq="' + Number(seq) + '"]');
    if (!row) return;
    row.scrollIntoView({ block: "center", behavior: "smooth" });
    row.classList.remove("quote-hit");
    void row.offsetWidth;
    row.classList.add("quote-hit");
    window.setTimeout(() => row.classList.remove("quote-hit"), 1500);
  }

  function replyBtn(m) {
    const btn = el("button", "reply");
    btn.type = "button";
    btn.setAttribute("aria-label", "引用");
    setTip(btn, "引用");
    btn.append(icon("reply"));
    btn.addEventListener("click", (ev) => {
      ev.preventDefault();
      ev.stopPropagation();
      armQuote(m.seq, previewText(m));
    });
    return btn;
  }

  function armTouchQuote(row, m) {
    let timer = 0;
    let armed = false;
    row.addEventListener("pointerdown", (ev) => {
      if (ev.pointerType === "mouse") return;
      if (ev.target.closest && ev.target.closest("button, a, input, textarea")) return;
      armed = false;
      timer = window.setTimeout(() => {
        armed = true;
        row.classList.add("reveal");
        armQuote(m.seq, previewText(m));
      }, 480);
    });
    const stop = () => window.clearTimeout(timer);
    row.addEventListener("pointerup", stop);
    row.addEventListener("pointercancel", stop);
    row.addEventListener("pointerleave", stop);
    row.addEventListener("contextmenu", (ev) => {
      if (armed) ev.preventDefault();
    });
  }

  function userBubble(m, messages) {
    const parsed = unwrap(m.content || "");
    const row = el("div", "msg user");
    if (m.seq) row.dataset.seq = String(m.seq);
    const wrap = el("div", "bubble-col");
    if (m.branch) wrap.append(bgFrom());
    if (m.reply_to) wrap.append(quoteBlock(messages, m.reply_to));
    const pulled = pullImages(parsed.text);
    if (pulled.text) {
      const bubble = el("div", "bubble");
      bubble.textContent = pulled.text;
      wrap.append(bubble);
    }
    if (pulled.images.length) wrap.append(gallery(pulled.images));
    if (!pulled.text && !pulled.images.length) {
      const bubble = el("div", "bubble");
      bubble.textContent = parsed.text;
      wrap.append(bubble);
    }
    const metaRow = el("div", "meta-row");
    if (parsed.untrusted && parsed.source !== "timer" && !parsed.source.startsWith("bot:")) {
      metaRow.append(el("span", "tag warn", parsed.source ? "不可信 · " + parsed.source : "不可信"));
    }
    metaRow.append(el("span", "meta", when(m.time)));
    if (m.seq) metaRow.append(replyBtn(m));
    metaRow.append(copyBtn(parsed.text));
    wrap.append(metaRow);
    row.append(wrap);
    armCopy(row);
    if (m.seq) armTouchQuote(row, m);
    return row;
  }

  function botBubble(m, results, messages, index, who, showName) {
    const row = el("div", "msg bot");
    if (m.seq) row.dataset.seq = String(m.seq);
    row.append(avatar(who, paintOf(who)));
    const body = el("div", "body");
    if (m.branch) body.append(bgFrom());
    if (showName) {
      const name = el("div", "who", niceName(who));
      if (who) setTip(name, who);
      body.append(name);
    }
    if (m.reply_to) body.append(quoteBlock(messages, m.reply_to));
    if (m.content) appendRich(body, m.content);
    const calls = m.tool_calls || [];
    const peers = [];
    for (const call of calls) {
      const info = toolSummary(call);
      const phrase = peerPhrase(info.name, info.args, who);
      if (phrase) { peers.push(phrase); continue; }
      if (info.name === "ask_user") {
        const q = info.args && info.args.question ? String(info.args.question) : "";
        const shown = q && !(m.content || "").includes(q) ? q : "";
        body.append(optionCards(info, laterUser(messages, index), shown));
        continue;
      }
    }
    const metaRow = el("div", "meta-row");
    metaRow.append(el("span", "meta", when(m.time)));
    if (m.seq) metaRow.append(replyBtn(m));
    metaRow.append(copyBtn(m.content || ""));
    body.append(metaRow);
    row.append(body);
    armCopy(row);
    if (m.seq) armTouchQuote(row, m);
    return { row: row, peers: peers };
  }

  function skipMessage(m) {
    if (!m || m.role === "tool" || m.role === "system") return true;
    const c = m.content || "";
    if (m.role === "user" && (c.startsWith("<environment_") || c.startsWith("<memory>") || c.startsWith("<background_tasks>"))) return true;
    return false;
  }

  function renderMessages(messages, who) {
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = state.firstRender || thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(niceName(who) || "XBot", "没有消息", who));
      restoreSendFeedback(inner);
      thread.replaceChildren(inner);
      state.firstRender = false;
      if (near) thread.scrollTop = thread.scrollHeight;
      return;
    }
    const results = {};
    for (const m of messages) {
      if (m.role === "tool" && m.tool_call_id) results[m.tool_call_id] = m;
    }
    let any = false;
    let lastKey = "";
    let lastRoutine = null;
    const latestCard = {};
    const latestApproval = {};
    messages.forEach((m, i) => {
      const card = codingCardOf(m && m.content);
      if (card) latestCard[card.id] = i;
      const appr = approvalCardOf(m && m.content);
      if (appr) latestApproval[appr.id] = i;
    });
    messages.forEach((m, i) => {
      if (skipMessage(m)) return;
      // Background turns have no cards in the chat; the header badge lists them.
      if (m.kind === "fork" || m.kind === "fork_end") return;
      const routine = routineOf(m);
      if (routine) {
        lastRoutine = routineLine(routine, (state.sel ? state.sel.id : "") + ":" + (m.seq || m.time || i));
        lastRoutine.dataset.branch = m.branch || "";
        inner.append(lastRoutine);
        any = true;
        lastKey = "";
        return;
      }
      if (m.role === "assistant" && String(m.content || "").trim() === "SILENT") {
        // Pair a quiet reply only with the wake of the same turn (branch).
        if (lastRoutine && lastRoutine.dataset.branch === (m.branch || "")) {
          if (!lastRoutine.querySelector(".routine-silent")) {
            lastRoutine.querySelector("summary").append(el("span", "routine-silent", " · 没什么要说的"));
          }
        } else inner.append(systemLine("没什么要说的"));
        any = true;
        lastKey = "";
        return;
      }
      // Invisible tool-only messages do not break the routine/result pair.
      if (m.role === "user" || String(m.content || "").trim()) lastRoutine = null;
      const appr = approvalCardOf(m.content);
      if (appr) {
        if (latestApproval[appr.id] !== i) return;
        any = true;
        lastKey = "";
        if (m.branch) inner.append(bgFrom());
        inner.append(approvalHistoryCard(appr, m.time));
        return;
      }
      const card = codingCardOf(m.content);
      if (card) {
        if (latestCard[card.id] !== i) return;
        any = true;
        lastKey = "";
        inner.append(codingCard(card));
        return;
      }
      if (m.role === "user") {
        const parsed = unwrap(m.content || "");
        if (parsed.untrusted && parsed.source === "subagent" && parsed.type === "fork_end") return;
        // Steer, queue, and interrupt sent to a background show in the badge popover.
        if (m.branch && /^\[(主人的补充|排队|打断)\]/.test(m.content || "")) return;
        if (parsed.untrusted && (parsed.source.startsWith("bot:") || parsed.source.startsWith("group:"))) {
          any = true;
          lastKey = "";
          const body = clip(pullImages(parsed.text).text, 80) || "一条消息";
          if (parsed.source.startsWith("bot:")) {
            const from = parsed.sender || parsed.source.slice(4);
            inner.append(systemLine(niceName(from) + " 发来 · " + body, from, from));
          } else {
            inner.append(systemLine("群里 · " + body));
          }
          return;
        }
        any = true;
        const row = userBubble(m, messages);
        if (lastKey === "user") row.classList.add("cont");
        lastKey = "user";
        inner.append(row);
        return;
      }
      any = true;
      const key = "bot:" + (who || "");
      const cont = lastKey === key;
      lastKey = key;
      const built = botBubble(m, results, messages, i, who, !cont);
      const asks = (m.tool_calls || []).some((c) => toolSummary(c).name === "ask_user");
      if (m.content || asks) {
        if (cont) built.row.classList.add("cont");
        inner.append(built.row);
      } else lastKey = "";
      for (const line of built.peers) {
        inner.append(systemLine(line.text, who, line.peer));
        lastKey = "";
      }
    });
    if (!any) inner.append(emptyState(niceName(who) || "XBot", "没有消息", who));
    restoreSendFeedback(inner);
    thread.replaceChildren(inner);
    state.firstRender = false;
    thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
  }

  function renderGroup(messages, who) {
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = state.firstRender || thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(niceName(who) || "群", "没有消息", who));
      restoreSendFeedback(inner);
      thread.replaceChildren(inner);
      state.firstRender = false;
      if (near) thread.scrollTop = thread.scrollHeight;
      return;
    }
    let last = "";
    for (const m of messages) {
      const mine = m.kind === "person";
      const text = m.text || "";
      if (!mine && m.kind === "bot" && last && last !== m.sender && mine === false) {
        // Bot speaking after another bot stays a bubble; the action line is the tool call.
      }
      if (!mine && /^\[action\]/i.test(text)) {
        inner.append(systemLine(clip(niceName(m.sender || "Bot") + " · " + text.replace(/^\[action\]\s*/i, ""), 96), m.sender));
        last = "";
        continue;
      }
      const row = el("div", "msg " + (mine ? "user" : "bot"));
      const key = mine ? "user" : ("bot:" + (m.sender || ""));
      if (key === last) row.classList.add("cont");
      if (mine) {
        const wrap = el("div", "bubble-col");
        const pulled = pullImages(text);
        if (pulled.text) {
          const bubble = el("div", "bubble");
          bubble.textContent = pulled.text;
          wrap.append(bubble);
        }
        if (pulled.images.length) wrap.append(gallery(pulled.images));
        if (!pulled.text && !pulled.images.length) wrap.append(el("div", "bubble", text));
        const metaRow = el("div", "meta-row");
        metaRow.append(el("span", "meta", when(m.time)), copyBtn(text));
        wrap.append(metaRow);
        row.append(wrap);
      } else {
        row.append(avatar(m.sender || "B", paintOf(m.sender)));
        const body = el("div", "body");
        if (!row.classList.contains("cont")) {
          const name = el("div", "who", niceName(m.sender || "bot"));
          if (m.sender) setTip(name, m.sender);
          body.append(name);
        }
        appendRich(body, text);
        const metaRow = el("div", "meta-row");
        metaRow.append(el("span", "meta", when(m.time)), copyBtn(text));
        body.append(metaRow);
        row.append(body);
      }
      armCopy(row);
      last = key;
      inner.append(row);
    }
    restoreSendFeedback(inner);
    thread.replaceChildren(inner);
    state.firstRender = false;
    thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
  }

  function codingCardOf(content) {
    const m = String(content || "").match(/^<coding_card id="([^"]*)" status="([^"]*)">\n([\s\S]*)\n<\/coding_card>$/);
    if (!m) return null;
    return { id: m[1], status: m[2], body: m[3] };
  }

  // Reads the attribute list of an opening <approval_card …> tag: key="value"
  // pairs in any order, and a > inside a quoted value does not end the tag.
  // Values are attribute-escaped like every other card attribute.
  function approvalCardTag(s) {
    const attr = (v) => v.replace(/&lt;/g, "<").replace(/&quot;/g, '"').replace(/&amp;/g, "&");
    const attrs = {};
    let i = 0;
    for (;;) {
      while (s[i] === " ") i++;
      if (s[i] === ">") return { attrs: attrs, end: i + 1 };
      const eq = s.indexOf("=", i);
      if (eq < 0) return null;
      const key = s.slice(i, eq).trim();
      if (!key || s[eq + 1] !== '"') return null;
      const q = s.indexOf('"', eq + 2);
      if (q < 0) return null;
      attrs[key] = attr(s.slice(eq + 2, q));
      i = q + 1;
    }
  }

  function approvalCardOf(content) {
    const s = String(content || "");
    const open = "<approval_card ";
    const tail = "\n</approval_card>";
    if (!s.startsWith(open) || !s.endsWith(tail)) return null;
    const tag = approvalCardTag(s.slice(open.length));
    if (!tag) return null;
    // Cards written before title and subtitle existed still parse; those fields
    // come back empty and the renderer falls back to the tool name.
    if (tag.attrs.id == null || tag.attrs.tool == null || tag.attrs.state == null) return null;
    const body = s.slice(open.length + tag.end);
    return {
      id: tag.attrs.id,
      tool: tag.attrs.tool,
      state: tag.attrs.state,
      title: tag.attrs.title || "",
      subtitle: tag.attrs.subtitle || "",
      deadline: tag.attrs.deadline || "",
      summary: body.slice(1, body.length - tail.length),
    };
  }

  // Fallback when a card carries no deadline. Matches Server.approvalWait.
  const APPROVAL_WAIT_MS = 24 * 60 * 60 * 1000;

  // Absolute instant this card stops accepting a decision. The server sends
  // deadline (RFC3339) on the live event and on the persisted card. A card
  // written before that field existed counts 24 hours from its transcript time.
  // wait === 0 means this card has no countdown (a node command).
  function approvalDeadlineMs(opt) {
    if (!opt || opt.wait === 0) return null;
    const raw = opt.deadline || opt.expires || opt.expiry;
    if (raw) {
      const t = typeof raw === "number" ? raw : Date.parse(raw);
      if (!Number.isNaN(t)) return t;
    }
    if (opt.since) {
      const t = Date.parse(opt.since);
      if (!Number.isNaN(t)) return t + APPROVAL_WAIT_MS;
    }
    return Date.now() + APPROVAL_WAIT_MS;
  }

  // Long waits read as hours ("23 小时"), shorter ones as minutes or seconds.
  function approvalRemainText(ms) {
    const secs = Math.max(0, Math.ceil(ms / 1000));
    const hour = 3600;
    const minute = 60;
    if (secs >= hour) return Math.floor(secs / hour) + " 小时";
    if (secs >= minute) return Math.floor(secs / minute) + " 分钟";
    return secs + " 秒";
  }

  function approvalLine(title, summary, state, time) {
    const kind = state === "approved" ? "ok" : state === "declined" ? "no" : "exp";
    const row = el("div", "approval-line " + kind);
    const st = el("span", "st");
    if (state === "approved") st.append(rawSvg(CHECK_PATH, 18, 2.4));
    else if (state === "declined") st.append(rawSvg(X_PATH, 18, 2.4));
    else st.append(rawSvg(CLOCK_PATH, 17, 2));
    const what = el("span", "what");
    what.append(document.createTextNode((title || "tool") + (summary ? " " : "")));
    if (summary) {
      const code = el("code");
      code.textContent = summary;
      what.append(code);
    }
    row.append(st, what);
    if (time) row.append(el("span", "when", when(time)));
    return row;
  }

  function pendingApprovalCard(opt) {
    const title = opt.title || opt.tool || "tool";
    const summary = String(opt.summary || "");
    const card = el("div", "approval");
    const head = el("div", "approval-head");
    const ico = el("span", "approval-ico");
    ico.append(rawSvg(ICONS.terminal, 16, 2));
    const main = el("div", "approval-main");
    main.append(el("div", "approval-title", title));
    if (opt.subtitle) main.append(el("div", "approval-detail", opt.subtitle));
    if (summary) {
      const chip = el("code", "approval-chip");
      chip.textContent = summary;
      main.append(chip);
    }
    head.append(ico, main);
    card.append(head);
    const actions = el("div", "approval-actions");
    let timerText = null;
    let tick = 0;
    let settled = false;
    const settle = (state) => {
      if (settled) return;
      settled = true;
      if (tick) window.clearInterval(tick);
      card.replaceWith(approvalLine(title, summary, state, Date.now()));
    };
    const deadlineMs = approvalDeadlineMs(opt);
    if (deadlineMs != null && deadlineMs <= Date.now()) {
      return approvalLine(title, summary, "expired", opt.since || Date.now());
    }
    if (deadlineMs != null) {
      const timer = el("span", "approval-timer");
      timer.append(rawSvg(CLOCK_PATH, 13, 2));
      timerText = el("span");
      timer.append(timerText);
      const paint = () => {
        const left = deadlineMs - Date.now();
        if (left <= 0) { settle("expired"); return; }
        const label = approvalRemainText(left);
        if (timerText.textContent !== label) timerText.textContent = label;
      };
      paint();
      tick = window.setInterval(paint, 1000);
      actions.append(timer);
    }
    const deny = el("button", "secondary", "拒绝");
    const allow = el("button", "primary", "允许");
    deny.type = "button";
    allow.type = "button";
    const decide = async (ok) => {
      allow.disabled = true;
      deny.disabled = true;
      try {
        await opt.decide(ok);
        settle(ok ? "approved" : "declined");
      } catch (e) {
        if (e && e.status === 410) {
          settle("expired");
          return;
        }
        allow.disabled = false;
        deny.disabled = false;
        let err = card.querySelector(".approval-err");
        if (!err) {
          err = el("div", "approval-err");
          card.append(err);
        }
        err.textContent = visibleError(e, "确认失败");
      }
    };
    allow.addEventListener("click", () => decide(true));
    deny.addEventListener("click", () => decide(false));
    actions.append(deny, allow);
    card.append(actions);
    return card;
  }

  function approvalHistoryCard(card, time) {
    const st = card.state || "pending";
    if (st === "pending") {
      const payload = {
        tool: card.tool || "",
        title: card.title || "",
        subtitle: card.subtitle || "",
        summary: card.summary || "",
        deadline: card.deadline || "",
        since: time || "",
        decide: async (ok) => {
          if (!state.sel || state.sel.type !== "bot") throw new Error("没有选中的 Bot");
          await api("/api/bots/" + encodeURIComponent(state.sel.id) + "/approvals/" + encodeURIComponent(card.id || ""), {
            method: "POST",
            body: { allow: ok },
          });
        },
      };
      return pendingApprovalCard(payload);
    }
    return approvalLine(card.title || card.tool || "tool", card.summary || "", st, time);
  }

  function codingCard(card) {
    const row = el("div", "coding-card");
    const head = el("div", "coding-card-head");
    head.append(el("span", "k", "编码任务"), el("span", "coding-card-id", card.id || ""), el("span", "coding-card-status", card.status || ""));
    row.append(head);
    const pre = el("pre", "");
    pre.textContent = card.body || "";
    row.append(pre);
    return row;
  }

  function hiddenFromPreview(m) {
    if (!m || m.role === "tool" || m.role === "system") return true;
    const raw = String(m.content || "").trim();
    if (m.role === "user" && (raw.startsWith("<environment") || raw.startsWith("<memory>") || raw.startsWith("<background_tasks>") || raw.startsWith("<untrusted") || raw.startsWith("<coding_card ") || raw.startsWith("<approval_card"))) return true;
    return false;
  }

  function summaryLine(text) {
    return /^summary written\b/i.test(String(text || "").trim());
  }

  function plainText(md) {
    return String(md || "")
      .replace(/^\s*(```|~~~)[^\n]*$/gm, " ")
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/^\s*(?:#{1,6}\s+|>\s*|[-*+]\s+|\d+[.)]\s+)/gm, "")
      .replace(/`+/g, "")
      .replace(/(\*\*|__)([\s\S]*?)\1/g, "$2")
      .replace(/\*([^*\n]+)\*/g, "$1")
      .replace(/_([^_\n]+)_/g, "$1")
      .replace(/\s+/g, " ").trim();
  }

  function previewFromMessages(messages) {
    if (!messages) return "";
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (hiddenFromPreview(m)) continue;
      const parsed = unwrap(m.content || "");
      if (parsed.untrusted) continue;
      const text = clip(plainText(pullImages(parsed.text).text), 72);
      if (!text || text === "SILENT" || summaryLine(text)) continue;
      return text;
    }
    return "";
  }

  function previewFromGroup(messages) {
    if (!messages) return "";
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      const raw = String(m.text || "").trim();
      if (!raw || /^\[action\]/i.test(raw)) continue;
      const who = m.kind === "person" ? "你" : niceName(m.sender || "");
      const text = clip(plainText(raw), 64);
      return who ? who + "：" + text : text;
    }
    return "";
  }

  function realPreview(text) {
    const raw = String(text || "").trim();
    return raw && raw !== "SILENT" ? clip(raw, 72) : "";
  }

  function lineForBot(b) {
    const p = state.preview["b:" + b.name];
    if (p && realPreview(p)) return p;
    if (b.description) return plainText(b.description);
    if (b.persona) return plainText(b.persona);
    return "没有消息";
  }

  function lineForGroup(g) {
    const p = state.preview["g:" + g.id];
    if (p) return p;
    return (g.members || []).map(niceName).join(" · ") || "没有消息";
  }

  function primaryMark() {
    const mark = icon("pin", "primary-mark");
    setTip(mark, "主 Bot");
    mark.setAttribute("aria-label", "主 Bot");
    return mark;
  }

  function listButton(name, preview, active, unread, paint, primary, label) {
    const btn = el("button", "item" + (active ? " active" : ""));
    btn.type = "button";
    setTip(btn, name, "right");
    const av = avatar(name, paint);
    if (unread) av.append(el("i", "unread"));
    const body = el("span", "item-body");
    const line = el("span", "name-line");
    line.append(el("span", "name", label || niceName(name)));
    if (primary) {
      line.append(primaryMark());
    }
    body.append(line, preview instanceof Node ? preview : el("span", "preview", preview || "没有消息"));
    btn.append(av, body);
    return btn;
  }

  function sectionLabel(text, action) {
    const row = el("div", "group-label");
    row.append(el("span", "label", text));
    if (action) row.append(action);
    return row;
  }

  function folderStore() {
    try {
      const raw = JSON.parse(localStorage.getItem("xbot-folders") || "{}");
      return raw && typeof raw === "object" ? raw : {};
    } catch (e) {
      return {};
    }
  }

  function folderCollapsed(name) {
    return !!folderStore()[name];
  }

  function setFolderCollapsed(name, on) {
    const all = folderStore();
    if (on) all[name] = true;
    else delete all[name];
    localStorage.setItem("xbot-folders", JSON.stringify(all));
  }

  function renameFolderCollapsed(oldName, next) {
    const all = folderStore();
    if (Object.prototype.hasOwnProperty.call(all, oldName)) {
      all[next] = all[oldName];
      delete all[oldName];
      localStorage.setItem("xbot-folders", JSON.stringify(all));
    }
  }

  function forgetFolderCollapsed(name) {
    const all = folderStore();
    if (!Object.prototype.hasOwnProperty.call(all, name)) return;
    delete all[name];
    localStorage.setItem("xbot-folders", JSON.stringify(all));
  }

  function folderError(e) {
    const msg = (e && e.message) || "";
    if ((e && e.status === 409) || /already exists/i.test(msg)) return "已经有这个分组";
    if ((e && e.status === 404) || msg === "not found") return "没有这个分组";
    return visibleError(e);
  }

  function closeFolderMenus() {
    document.querySelectorAll(".folder-menu").forEach((menu) => {
      menu.hidden = true;
      menu.remove();
    });
    document.querySelectorAll(".folder-more").forEach((btn) => {
      btn.setAttribute("aria-expanded", "false");
    });
  }

  function placeMenu(anchor, menu, side) {
    document.body.append(menu);
    menu.hidden = false;
    menu.style.position = "fixed";
    menu.style.zIndex = "45";
    const width = menu.offsetWidth;
    const height = menu.offsetHeight;
    const r = anchor.getBoundingClientRect();
    let left = side === "left" ? r.left - width - 6 : r.right - width;
    if (left < 8) left = Math.min(window.innerWidth - width - 8, r.right + 6);
    let top = side === "left" ? r.top - 6 : r.bottom + 4;
    if (top + height > window.innerHeight - 8) top = Math.max(8, window.innerHeight - height - 8);
    menu.style.left = left + "px";
    menu.style.top = top + "px";
    menu.style.right = "auto";
    menu.style.bottom = "auto";
  }

  function botRow(b) {
    const d = stDesc(b.name);
    const btn = listButton(b.name, d.node, state.sel && state.sel.type === "bot" && state.sel.id === b.name, !!b.unread, paintOfBot(b), !!b.primary);
    btn.dataset.bot = b.name;
    btn.dataset.stsig = d.sig;
    btn.classList.toggle("off", d.off);
    btn.addEventListener("click", () => selectBot(b.name));
    return btn;
  }

  function folderSection(folder) {
    const box = el("div", "folder");
    const collapsed = folderCollapsed(folder.name);
    const head = el("div", "folder-head");
    const toggle = el("button", "folder-toggle");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
    toggle.append(icon(collapsed ? "chevron" : "chevronDown"));
    toggle.append(el("span", "folder-name", folder.name));
    toggle.append(el("span", "folder-count", String(folder.count)));
    toggle.addEventListener("click", () => {
      closeFolderMenus();
      setFolderCollapsed(folder.name, !folderCollapsed(folder.name));
      state.listSig = "";
      renderLists();
    });
    head.append(toggle);
    if (!folder.ungrouped) {
      const more = el("button", "icon-btn folder-more");
      more.type = "button";
      more.setAttribute("aria-label", "更多");
      more.setAttribute("aria-expanded", "false");
      setTip(more, "更多");
      more.append(icon("ellipsis"));
      const menu = el("div", "pop folder-menu");
      menu.hidden = true;
      const rename = el("button", "menu-row");
      rename.type = "button";
      rename.append(icon("pencil"), el("span", "", "重命名"));
      rename.addEventListener("click", (ev) => {
        ev.stopPropagation();
        closeFolderMenus();
        openRenameFolder(folder.name);
      });
      const drop = el("button", "menu-row secondary");
      drop.type = "button";
      drop.append(icon("trash"), el("span", "", "删除"));
      drop.addEventListener("click", (ev) => {
        ev.stopPropagation();
        closeFolderMenus();
        confirmDeleteFolder(folder.name);
      });
      menu.append(rename, el("div", "menu-rule"), drop);
      more.addEventListener("click", (ev) => {
        ev.stopPropagation();
        const open = menu.hidden;
        closeFolderMenus();
        closePops();
        if (open) {
          more.setAttribute("aria-expanded", "true");
          placeMenu(more, menu, "below");
        }
      });
      head.append(more, menu);
    }
    box.append(head);
    const body = el("div", "folder-bots");
    body.hidden = collapsed;
    const byName = {};
    for (const b of state.bots) byName[b.name] = b;
    for (const name of folder.bots || []) {
      if (byName[name]) body.append(botRow(byName[name]));
    }
    box.append(body);
    return box;
  }

  function renderLists() {
    const collapsed = folderStore();
    const sig = JSON.stringify({
      bots: state.bots.map((b) => [b.name, !!b.pinned, !!b.primary, !!b.unread, b.group || "", b.description, lineForBot(b)]),
      folders: state.folders.map((f) => [f.name, f.count, !!f.ungrouped, (f.bots || []).join("\n"), !!collapsed[f.name]]),
      groups: state.groups.map((g) => [g.id, g.name, lineForGroup(g)]),
      others: (state.others || []).map((o) => [(o && o.bot) || "", (o && o.name) || ""]),
      sel: state.sel,
    });
    if (sig === state.listSig) return;
    state.listSig = sig;
    document.querySelectorAll(".folder-menu").forEach((menu) => menu.remove());
    const pins = $("pin-row");
    const roster = $("roster");
    pins.replaceChildren();
    roster.replaceChildren();
    const pinned = state.bots.filter((b) => b.pinned);
    for (const b of pinned) {
      const btn = el("button", "pin" + (state.sel && state.sel.type === "bot" && state.sel.id === b.name ? " active" : ""));
      btn.type = "button";
      setTip(btn, b.name);
      btn.setAttribute("aria-label", niceName(b.name));
      const av = avatar(b.name, paintOfBot(b), "pin");
      if (b.unread) av.append(el("i", "unread"));
      btn.append(av);
      btn.addEventListener("click", () => selectBot(b.name));
      pins.append(btn);
    }
    const add = el("button", "pin add");
    add.type = "button";
    add.setAttribute("aria-label", "新建 Bot");
    setTip(add, "新建 Bot", "right");
    const face = el("span", "plus-face");
    face.append(icon("plus"));
    add.append(face);
    add.addEventListener("click", openNewBot);
    pins.append(add);

    const folders = state.folders.length
      ? state.folders
      : [{ name: "未分组", count: state.bots.length, bots: state.bots.map((b) => b.name), ungrouped: true }];
    const hasFolders = folders.some((folder) => !folder.ungrouped);
    for (const folder of folders) {
      if (!hasFolders && folder.ungrouped) {
        // No header, but keep the .folder-bots body so sortStatusRows still lifts busy bots.
        const body = el("div", "folder-bots");
        for (const name of folder.bots || []) {
          const b = state.bots.find((bot) => bot.name === name);
          if (b) body.append(botRow(b));
        }
        roster.append(body);
      } else roster.append(folderSection(folder));
    }
    if (state.others && state.others.length) roster.append(othersFolder());

    const newer = el("button", "icon-btn");
    newer.type = "button";
    newer.setAttribute("aria-label", "新建群组");
    setTip(newer, "新建群组", "right");
    newer.append(icon("plus"));
    newer.addEventListener("click", openNewGroup);
    roster.append(sectionLabel("群组", newer));
    for (const g of state.groups) {
      const btn = listButton(g.id, lineForGroup(g), state.sel && state.sel.type === "group" && state.sel.id === g.id, false, null, false, niceName(g.name || g.id));
      btn.addEventListener("click", () => selectGroup(g.id));
      roster.append(btn);
    }
    paintStatus();
  }

  function renderMe() {
    const slot = $("me-avatar");
    slot.replaceChildren(avatar(state.owner || "me", null, "me"));
    const name = $("me-name");
    name.textContent = "我";
    setTip(name, state.owner || "");
  }

  function renderNotes() {
    bellDot.hidden = state.notes.length === 0;
    if (!state.notes.length) {
      notesEl.hidden = true;
      $("bell").setAttribute("aria-expanded", "false");
      notesEl.replaceChildren();
      return;
    }
    const open = !notesEl.hidden;
    notesEl.replaceChildren();
    const header = el("div", "notes-header");
    const readAll = el("button", "secondary notes-read-all", "全部已读");
    readAll.type = "button";
    readAll.addEventListener("click", async () => {
      readAll.disabled = true;
      try { await markNotesRead(state.notes.slice()); }
      catch (e) { setStatus("通知未能标为已读，请重试。", true); }
      finally { readAll.disabled = false; }
    });
    header.append(el("b", "", "通知"), readAll);
    notesEl.append(header);
    for (const n of state.notes) {
      const btn = el("button", "note");
      btn.type = "button";
      const left = document.createElement("span");
      left.append(el("b", "", niceName(n.bot)), document.createTextNode("  " + plainText(n.text)));
      setTip(btn, n.bot || "");
      btn.append(left, el("span", "meta", when(n.time)));
      btn.addEventListener("click", () => openNote(n));
      notesEl.append(btn);
    }
    notesEl.hidden = !open;
    $("bell").setAttribute("aria-expanded", String(open));
  }

  async function markNotesRead(notes) {
    const byBot = new Map();
    for (const n of notes) {
      if (!byBot.has(n.bot)) byBot.set(n.bot, []);
      byBot.get(n.bot).push(n.id);
    }
    try {
      await Promise.all(Array.from(byBot, ([bot, ids]) =>
        api("/api/notes/read", { method: "POST", body: { bot, ids } })));
    } finally {
      await refreshNotes();
    }
  }

  async function readBotNotes(name) {
    const data = await api("/api/notes");
    await markNotesRead((data.notes || []).filter((n) => n.bot === name));
  }

  async function openNote(n) {
    notesEl.hidden = true;
    $("bell").setAttribute("aria-expanded", "false");
    if (n.bot) await selectBot(n.bot);
  }

  function setHeader(slug, label) {
    const shown = label || niceName(slug) || "XBot";
    titleEl.textContent = shown;
    setTip(titleEl, slug && slug !== shown ? slug : "");
    document.title = shown + " · XBot";
  }

  function leavePeer() {
    state.peer = null;
    const main = document.querySelector(".main");
    if (main) main.classList.remove("peer");
    const back = $("back");
    if (back) back.hidden = true;
  }

  function pairSep() {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", "0 0 4 4");
    svg.setAttribute("aria-hidden", "true");
    svg.classList.add("pair-sep");
    const c = document.createElementNS("http://www.w3.org/2000/svg", "circle");
    c.setAttribute("cx", "2");
    c.setAttribute("cy", "2");
    c.setAttribute("r", "1.1");
    svg.append(c);
    return svg;
  }

  function pairNames(a, b) {
    const name = el("span", "pair-name");
    name.append(el("span", "pair-who", niceName(a)), pairSep(), el("span", "pair-who", niceName(b)));
    return name;
  }

  function paintPeerHeader(a, b) {
    const back = $("back");
    if (back) back.hidden = false;
    const main = document.querySelector(".main");
    if (main) main.classList.add("peer");
    titleEl.replaceChildren();
    const row = el("div", "pair-title");
    const faces = el("span", "pair-faces");
    faces.append(avatar(a, paintOf(a)), avatar(b, paintOf(b)));
    row.append(faces, pairNames(a, b));
    titleEl.append(row);
    const slugTip = (a !== niceName(a) || b !== niceName(b)) ? (a + " · " + b) : "";
    setTip(titleEl, slugTip);
    subtitle.textContent = "";
    subtitle.hidden = true;
    document.title = niceName(a) + " · " + niceName(b) + " · XBot";
  }

  const PEER_LIMIT = 50;

  function peerCursor(m) {
    if (!m || m.to == null || m.to === "" || m.seq == null || Number(m.seq) < 1) return "";
    return m.to + ":" + m.seq;
  }

  function comparePeer(x, y) {
    const tx = Date.parse(x.ts || "") || 0;
    const ty = Date.parse(y.ts || "") || 0;
    if (tx !== ty) return tx - ty;
    const to = String(x.to || "").localeCompare(String(y.to || ""));
    if (to) return to;
    const seq = (Number(x.seq) || 0) - (Number(y.seq) || 0);
    if (seq) return seq;
    return String(x.id || "").localeCompare(String(y.id || ""));
  }

  function mockPeerMessages(a, b, before) {
    if (before) return [];
    const pair = [a, b].slice().sort().join("\0");
    if (pair === "ada\0bob") {
      return [
        { id: "mock-1", seq: 1, from: "ada", to: "bob", text: "封面在对话里，下午之前看一眼", ts: "2026-10-14T01:13:08Z" },
        { id: "mock-2", seq: 2, from: "bob", to: "ada", text: "收到，我下午看。", ts: "2026-10-14T01:20:00Z" },
      ];
    }
    return [
      { id: "mock-1", seq: 1, from: a, to: b, text: "这条往来还没接到。", ts: new Date().toISOString() },
    ];
  }

  async function fetchPeerMessages(a, b, before) {
    const q = new URLSearchParams({ limit: String(PEER_LIMIT) });
    if (before) q.set("before", String(before));
    const path = "/api/bots/" + encodeURIComponent(a) + "/peers/" + encodeURIComponent(b) + "/messages?" + q;
    try {
      const data = await api(path);
      const list = data && Array.isArray(data.messages) ? data.messages : null;
      if (!list) {
        const err = new Error("bad peer payload");
        err.status = 502;
        throw err;
      }
      return { list: list, mock: false };
    } catch (e) {
      if (e.status === 401) throw e;
      return { list: null, mock: true };
    }
  }

  function renderPeer(list, peer) {
    const inner = el("div", "thread-inner");
    const rows = (list || []).slice().sort(comparePeer);
    if (!rows.length) {
      const box = emptyState("", "没有消息");
      const heading = box.querySelector("h2");
      if (heading) heading.replaceChildren(pairNames(peer.a, peer.b));
      inner.append(box);
      thread.replaceChildren(inner);
      return;
    }
    let last = "";
    for (const m of rows) {
      const mine = m.from === peer.a;
      const row = el("div", "msg " + (mine ? "user" : "bot"));
      if (last === m.from) row.classList.add("cont");
      if (!mine) row.append(avatar(m.from, paintOf(m.from)));
      const body = el("div", mine ? "bubble-col" : "body");
      if (!mine && last !== m.from) {
        const who = el("div", "who", niceName(m.from));
        if (m.from) setTip(who, m.from);
        body.append(who);
      }
      const bubble = el("div", "bubble");
      bubble.textContent = m.text || "";
      const metaRow = el("div", "meta-row");
      metaRow.append(el("span", "meta", when(m.ts)));
      body.append(bubble, metaRow);
      row.append(body);
      inner.append(row);
      last = m.from || "";
    }
    thread.replaceChildren(inner);
    thread.scrollTop = thread.scrollHeight;
  }

  async function loadPeer(before) {
    const peer = state.peer;
    if (!peer) return;
    const keep = !!before;
    const prevHeight = keep ? thread.scrollHeight : 0;
    const prevTop = keep ? thread.scrollTop : 0;
    let got;
    try {
      got = await fetchPeerMessages(peer.a, peer.b, before);
    } catch (e) {
      if (e.status === 401) throw e;
      got = { list: null, mock: true };
    }
    if (state.peer !== peer) return;
    if (got.mock) {
      if (keep && peer.list.length && !peer.mock) {
        peer.more = false;
        return;
      }
      peer.mock = true;
      peer.list = mockPeerMessages(peer.a, peer.b, before);
      peer.more = false;
      peer.cursor = null;
      renderPeer(peer.list, peer);
      return;
    }
    peer.mock = false;
    const incoming = got.list;
    if (keep) {
      const seen = {};
      for (const m of peer.list) {
        const key = m.id || peerCursor(m);
        if (key) seen[key] = true;
      }
      const older = incoming.filter((m) => {
        const key = m.id || peerCursor(m);
        return !key || !seen[key];
      });
      peer.list = older.concat(peer.list);
    } else {
      peer.list = incoming.slice();
    }
    peer.list.sort(comparePeer);
    peer.more = incoming.length >= PEER_LIMIT;
    peer.cursor = peer.list.length ? peerCursor(peer.list[0]) : null;
    renderPeer(peer.list, peer);
    if (keep) thread.scrollTop = Math.max(0, thread.scrollHeight - prevHeight + prevTop);
  }

  async function openPeer(a, b) {
    if (!a || !b || a === b || state.streaming) return;
    clearQuote();
    state.peer = { a: a, b: b, list: [], more: false, cursor: null, mock: false };
    paintPeerHeader(a, b);
    await loadPeer(null);
  }

  async function closePeer() {
    if (!state.peer) return;
    leavePeer();
    if (state.sel && state.sel.type === "bot") setHeader(state.sel.id);
    else if (state.sel && state.sel.type === "group") {
      const g = state.groups.find((x) => x.id === state.sel.id);
      setHeader(state.sel.id, g ? niceName(g.name || state.sel.id) : "");
    }
    setStatus("");
    state.sig = "";
    await loadThread();
  }

  let subtitleError = false;
  function setStatus(text, isError = false) {
    subtitleError = isError;
    let desc = "";
    if (!text && state.sel && state.sel.type === "bot") {
      const bot = (state.bots || []).find((b) => b.name === state.sel.id);
      desc = (bot && bot.description) || "";
    }
    const show = text || desc;
    subtitle.textContent = show;
    subtitle.hidden = !show;
  }

  function resetConversation() {
    state.streaming = 0;
    state.pendingSends = [];
    state.replyErrors = [];
    state.firstRender = true;
    state.messages = [];
    state.panelData = null;
    state.traceOpen = {};
    state.traceOut = {};
    ++state.threadLoad;
    ++state.panelLoad;
    thread.replaceChildren();
    const skeleton = el("div", "thread-inner thread-skeleton");
    skeleton.setAttribute("aria-label", "正在加载对话");
    for (let i = 0; i < 3; i++) skeleton.append(el("div", "skeleton-bubble"));
    thread.append(skeleton);
    panelBody.replaceChildren(el("div", "muted", "正在加载"));
    closePanelMenu();
  }

  function threadUnavailable() {
    state.sig = "";
    const box = el("div", "thread-unavailable");
    box.setAttribute("role", "status");
    const retry = el("button", "secondary", "重试");
    retry.type = "button";
    retry.addEventListener("click", async () => {
      retry.disabled = true;
      await loadThread();
      retry.disabled = false;
      if (panel.classList.contains("open")) await loadPanel();
    });
    box.append(el("p", "", "连不上 XBot"), retry);
    thread.replaceChildren(box);
  }

  async function selectBot(name) {
    leavePeer();
    clearQuote();
    state.sel = { type: "bot", id: name };
    try { sessionStorage.setItem("xbot-selected-bot", name); } catch (e) {}
    resetConversation();
    try {
      const draft = sessionStorage.getItem("xbot-draft:" + name);
      if (draft) {
        sessionStorage.removeItem("xbot-draft:" + name);
        if (!input.value) input.value = draft;
        syncComposer();
      }
    } catch (e) {}
    state.sig = "";
    state.tab = state.tab || "details";
    state.editOpen = false;
    closeDrawer();
    setHeader(name);
    setStatus("");
    syncMore();
    setComposerPlaceholder("写给 " + niceName(name));
    renderLists();
    const selection = state.sel;
    // Reading notifications runs on its own: it never holds up the panel, and a stale
    // failure never overwrites another bot's status.
    readBotNotes(name).catch(() => {
      if (state.sel === selection) setStatus("通知未能标为已读，请重试。", true);
    });
    await loadThread();
    if (state.sel === selection && panel.classList.contains("open")) await loadPanel();
  }

  async function selectGroup(id) {
    leavePeer();
    clearQuote();
    state.sel = { type: "group", id: id };
    resetConversation();
    state.sig = "";
    state.editOpen = false;
    closeDrawer();
    const g = state.groups.find((x) => x.id === id);
    setHeader(id, g ? niceName(g.name || id) : "");
    setStatus("");
    syncMore();
    setComposerPlaceholder("写到这个群");
    renderLists();
    const selection = state.sel;
    await loadThread();
    if (state.sel === selection && panel.classList.contains("open")) await loadPanel();
  }

  async function loadThread() {
    if (!state.sel || state.streaming || state.peer) return;
    const selection = state.sel;
    const request = ++state.threadLoad;
    try {
      if (selection.type === "bot") {
        const data = await api("/api/bots/" + encodeURIComponent(selection.id) + "/transcript");
        if (state.sel !== selection || request !== state.threadLoad || state.streaming || state.peer) return;
        const messages = data.messages || [];
        const sig = JSON.stringify(messages);
        state.messages = messages;
        state.preview["b:" + selection.id] = previewFromMessages(messages);
        state.previewAt["b:" + selection.id] = Date.now();
        renderLists();
        if (sig === state.sig) {
          if (panel.classList.contains("open") && state.tab === "trace") paintPanel();
          return;
        }
        state.sig = sig;
        if (state.peer) return;
        renderMessages(messages, selection.id);
        if (panel.classList.contains("open") && state.tab === "trace") paintPanel();
        return;
      }
      const data = await api("/api/groups/" + encodeURIComponent(selection.id));
      if (state.sel !== selection || request !== state.threadLoad || state.streaming || state.peer) return;
      const messages = data.messages || [];
      const sig = JSON.stringify(messages);
      state.preview["g:" + selection.id] = previewFromGroup(messages);
      state.previewAt["g:" + selection.id] = Date.now();
      renderLists();
      if (sig === state.sig) return;
      state.sig = sig;
      renderGroup(messages, data.group && data.group.name);
    } catch (e) {
      if (state.sel === selection && request === state.threadLoad && !state.streaming && !state.pendingSends.length) threadUnavailable();
    }
  }

  function section(title) {
    const box = el("div", "section");
    if (title) box.append(el("h3", "", title));
    return box;
  }

  function kv(k, v) {
    const row = el("div", "kv");
    row.append(el("div", "k", k), el("div", "v", v || "—"));
    return row;
  }

  const TABS = [["details", "详情"], ["library", "记忆"], ["computer", "任务"], ["trace", "轨迹"]];

  function tabBar() {
    const bar = el("div", "tabs");
    bar.setAttribute("role", "tablist");
    for (const pair of TABS) {
      const btn = el("button", state.tab === pair[0] ? "on" : "", pair[1]);
      btn.type = "button";
      btn.setAttribute("role", "tab");
      btn.setAttribute("aria-selected", state.tab === pair[0] ? "true" : "false");
      btn.addEventListener("click", () => {
        state.tab = pair[0];
        paintPanel();
      });
      bar.append(btn);
    }
    return bar;
  }

  function hero(name, desc, paint, pin) {
    const box = el("div", "hero");
    box.append(avatar(name, paint, "lg"));
    const title = el("div", "hero-name");
    const heading = el("h2", "", niceName(name) || "Bot");
    if (name && name !== heading.textContent) setTip(heading, name);
    title.append(heading);
    if (pin) {
      const mark = el("button", "pin-mark" + (pin.on ? " on" : ""));
      mark.type = "button";
      mark.setAttribute("aria-label", pin.on ? "取消置顶" : "置顶");
      setTip(mark, pin.on ? "已置顶" : "置顶");
      mark.append(icon("pin"));
      mark.addEventListener("click", pin.toggle);
      title.append(mark);
    }
    box.append(title);
    if (desc) box.append(el("p", "desc", desc));
    return box;
  }

  function menuRow(iconName, label, onClick, danger) {
    const btn = el("button", "menu-row" + (danger ? " danger" : ""));
    btn.type = "button";
    btn.append(icon(iconName), el("span", "", label));
    btn.addEventListener("click", (ev) => {
      ev.stopPropagation();
      closePanelMenu();
      onClick();
    });
    return btn;
  }

  function closePanelMenu() {
    $("panel-menu").hidden = true;
    $("panel-more").setAttribute("aria-expanded", "false");
    document.querySelectorAll(".menu-sub").forEach((sub) => {
      sub.hidden = true;
      sub.remove();
    });
    const move = $("panel-menu").querySelector(".menu-move > .menu-row");
    if (move) move.setAttribute("aria-expanded", "false");
  }

  function syncPanelChrome() {
    const bot = state.sel && state.sel.type === "bot";
    const data = state.panelData || {};
    $("panel-export").hidden = !bot;
    $("panel-more").hidden = !bot;
    const menu = $("panel-menu");
    document.querySelectorAll(".menu-sub").forEach((sub) => sub.remove());
    menu.replaceChildren();
    if (!bot) {
      closePanelMenu();
      return;
    }
    const pinned = !!data.pinned;
    const current = (state.bots.find((b) => b.name === state.sel.id) || {}).group || "";
    menu.append(menuRow("pin", pinned ? "取消置顶" : "置顶", () => togglePin(state.sel.id, !pinned)));
    menu.append(menuRow("copy", "复刻", () => openFork(state.sel.id)));
    menu.append(menuRow("pencil", "编辑资料", () => openProfile(data)));
    menu.append(moveMenu(state.sel.id, current));
    menu.append(el("div", "menu-rule"));
    menu.append(menuRow("trash", "删除 Bot", () => confirmDelete(state.sel.id), true));
  }

  function moveMenu(botName, current) {
    const wrap = el("div", "menu-move");
    const btn = el("button", "menu-row");
    btn.type = "button";
    btn.setAttribute("aria-expanded", "false");
    btn.append(icon("folder"), el("span", "", "移到分组"), icon("chevron", "mark"));
    const sub = el("div", "pop menu-sub");
    sub.hidden = true;
    const folders = state.folders.length ? state.folders : [{ name: "未分组", ungrouped: true }];
    for (const folder of folders) {
      const group = folder.ungrouped ? "" : folder.name;
      const here = group === (current || "");
      const item = el("button", "menu-row" + (here ? " here" : ""));
      item.type = "button";
      item.append(el("span", "", folder.name));
      if (here) item.append(icon("check", "mark"));
      item.addEventListener("click", async (ev) => {
        ev.stopPropagation();
        closePanelMenu();
        if (here) return;
        try {
          await api("/api/bots/" + encodeURIComponent(botName), { method: "PATCH", body: { group: group } });
          await refreshLists();
        } catch (e) {
          setStatus(folderError(e), true);
        }
      });
      sub.append(item);
    }
    sub.append(el("div", "menu-rule"));
    const create = el("button", "menu-row");
    create.type = "button";
    create.append(icon("plus"), el("span", "", "新建分组"));
    create.addEventListener("click", (ev) => {
      ev.stopPropagation();
      closePanelMenu();
      openNewFolder(botName);
    });
    sub.append(create);
    btn.addEventListener("click", (ev) => {
      ev.stopPropagation();
      const open = sub.hidden;
      sub.hidden = true;
      btn.setAttribute("aria-expanded", "false");
      if (open) {
        btn.setAttribute("aria-expanded", "true");
        placeMenu(btn, sub, "left");
      }
    });
    wrap.append(btn, sub);
    return wrap;
  }

  function openNewFolder(botName) {
    openModal("新建分组", [
      { name: "name", label: "名字", placeholder: "工作", maxLength: 32 },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      if (name === "未分组") throw new Error("不能用这个名字");
      try {
        await api("/api/groups", { method: "POST", body: { name: name } });
        if (botName) {
          await api("/api/bots/" + encodeURIComponent(botName), { method: "PATCH", body: { group: name } });
        }
      } catch (e) {
        throw new Error(folderError(e));
      }
      await refreshLists();
    });
  }

  function openRenameFolder(oldName) {
    openModal("重命名", [
      { name: "name", label: "名字", value: oldName, maxLength: 32 },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      if (name === oldName) return;
      try {
        await api("/api/groups/" + encodeURIComponent(oldName), { method: "PATCH", body: { name: name } });
      } catch (e) {
        throw new Error(folderError(e));
      }
      renameFolderCollapsed(oldName, name);
      await refreshLists();
    }, "保存");
  }

  function confirmDeleteFolder(name) {
    openModal("删除分组", [
      { name: "warn", type: "note", label: "「" + name + "」里的 Bot 不会被删除，会回到未分组。" },
    ], async () => {
      try {
        await api("/api/groups/" + encodeURIComponent(name), { method: "DELETE" });
      } catch (e) {
        throw new Error(folderError(e));
      }
      forgetFolderCollapsed(name);
      await refreshLists();
    }, "删除");
  }

  function confirmDelete(name) {
    openModal("删除 Bot", [
      { name: "warn", type: "note", label: "「" + niceName(name) + "」和它的对话会一起删掉，不能恢复。" },
    ], async () => {
      try {
        await api("/api/bots/" + encodeURIComponent(name), { method: "DELETE" });
      } catch (e) {
        const msg = e.message || "";
        if (/primary/i.test(msg)) throw new Error("主 Bot 不能删，先把主 Bot 交给另一个。");
        if (/last bot/i.test(msg)) throw new Error("最后一个 Bot 不能删。");
        throw new Error(msg || "删除失败");
      }
      delete state.preview["b:" + name];
      if (state.sel && state.sel.type === "bot" && state.sel.id === name) state.sel = null;
      await refreshLists();
      const home = state.bots.find((b) => b.primary) || state.bots[0];
      if (home) await selectBot(home.name);
      else paintPanel();
    }, "删除");
  }

  function paintPanel() {
    panelBody.replaceChildren();
    if (!state.sel) {
      syncPanelChrome();
      return;
    }
    if (state.sel.type === "group") {
      const g = state.groups.find((x) => x.id === state.sel.id) || { name: state.sel.id, members: [] };
      panelBody.append(hero(g.name || g.id, (g.members || []).join(" · "), null), tabBar());
      const body = el("div", "tab-body");
      if (state.tab === "library") {
        body.append(el("p", "hint", "还没有记住任何事。对它说「记住……」就会出现在这里"));
      } else if (state.tab === "computer") {
        body.append(el("p", "hint", "没有任务"));
      } else if (state.tab === "trace") {
        body.append(el("p", "hint", "没有轨迹"));
      } else {
        const s = section("成员");
        s.append(el("p", "", (g.members || []).map(niceName).join("、") || "没有成员"));
        body.append(s);
      }
      panelBody.append(body);
      syncPanelChrome();
      return;
    }
    const data = state.panelData || { name: state.sel.id };
    const desc = data.description || "";
    const heroBox = hero(data.name || state.sel.id, desc, paintOfBot(data), {
      on: !!data.pinned,
      toggle: () => togglePin(state.sel.id, !data.pinned),
    });
    if (data.primary) {
      // The hero already has the 置顶 pin toggle; say 主 Bot in words here.
      heroBox.querySelector(".hero-name").append(el("span", "primary-label", "主 Bot"));
    }
    panelBody.append(heroBox, tabBar());
    syncPanelChrome();
    const body = el("div", "tab-body");
    if (state.tab === "library") body.append(libraryView(data));
    else if (state.tab === "computer") body.append(computerView(data));
    else if (state.tab === "trace") body.append(traceView());
    else body.append(detailsView(data));
    panelBody.append(body);
  }

  function durText(ms) {
    const s = Math.max(1, Math.round(ms / 1000));
    if (s < 60) return s + "秒";
    const m = Math.round(s / 60);
    if (m < 60) return m + "分";
    return Math.round(m / 60) + "小时";
  }

  function argText(args) {
    if (!args || typeof args !== "object") return "";
    if (typeof args.command === "string" && args.command) return args.command;
    const keys = Object.keys(args);
    if (!keys.length) return "";
    try { return JSON.stringify(args, null, 2); } catch (e) { return ""; }
  }

  function previewOutput(text) {
    const raw = String(text || "");
    const lines = raw.split("\n");
    let short = lines.slice(0, 4).join("\n");
    if (short.length > 280) short = short.slice(0, 280);
    const truncated = short.length < raw.length;
    return { text: truncated ? short.replace(/\s+$/, "") + "…" : raw, truncated: truncated };
  }

  function traceTurns(messages) {
    const results = {};
    for (const m of messages || []) {
      if (m.role === "tool" && m.tool_call_id) results[m.tool_call_id] = m;
    }
    const turns = [];
    for (const m of messages || []) {
      if (m.role !== "assistant" || !(m.tool_calls && m.tool_calls.length)) continue;
      const calls = [];
      for (const call of m.tool_calls) {
        const info = toolSummary(call);
        if (info.name === "ask_user") continue;
        const result = results[call.id || ""];
        calls.push({
          name: info.name,
          args: info.args,
          output: result ? (result.content || "") : "",
          resultTime: result && result.time,
        });
      }
      if (!calls.length) continue;
      const start = m.time ? new Date(m.time).getTime() : 0;
      let end = start;
      for (const c of calls) {
        if (!c.resultTime) continue;
        const t = new Date(c.resultTime).getTime();
        if (!Number.isNaN(t) && t > end) end = t;
      }
      turns.push({
        key: (state.sel ? state.sel.id : "") + ":" + (m.seq != null ? m.seq : (m.time || turns.length)),
        time: m.time,
        names: calls.map((c) => c.name),
        dur: start && end > start ? end - start : 0,
        calls: calls,
      });
    }
    return turns;
  }

  function traceBody(turn) {
    const box = el("div", "trace-body");
    turn.calls.forEach((call, i) => {
      const item = el("div", "trace-call");
      item.append(el("div", "trace-call-name", call.name));
      const args = argText(call.args);
      if (args) item.append(el("pre", "trace-args", args));
      if (call.output) {
        const outKey = turn.key + ":" + i;
        const open = !!state.traceOut[outKey];
        const preview = previewOutput(call.output);
        item.append(el("pre", "trace-out", open || !preview.truncated ? call.output : preview.text));
        if (preview.truncated) {
          const more = el("button", "text-btn trace-more", open ? "收起" : "展开");
          more.type = "button";
          more.addEventListener("click", (ev) => {
            ev.stopPropagation();
            state.traceOut[outKey] = !state.traceOut[outKey];
            paintPanel();
          });
          item.append(more);
        }
      }
      box.append(item);
    });
    return box;
  }

  function traceView() {
    const wrap = document.createDocumentFragment();
    const turns = traceTurns(state.messages);
    if (!turns.length) {
      wrap.append(el("p", "hint", "没有轨迹"));
      return wrap;
    }
    const list = el("div", "trace");
    for (const turn of turns) {
      const open = !!state.traceOpen[turn.key];
      const row = el("button", "trace-row" + (open ? " open" : ""));
      row.type = "button";
      row.setAttribute("aria-expanded", open ? "true" : "false");
      row.append(
        el("span", "trace-time", when(turn.time) || "—"),
        el("span", "trace-tools", turn.names.join(" · ")),
        el("span", "trace-dur", turn.dur ? durText(turn.dur) : ""),
        icon("chevron", "chev")
      );
      row.addEventListener("click", () => {
        state.traceOpen[turn.key] = !state.traceOpen[turn.key];
        paintPanel();
      });
      list.append(row);
      if (open) list.append(traceBody(turn));
    }
    wrap.append(list);
    return wrap;
  }

  function styleChips(data) {
    const style = data.style || {};
    const bits = [];
    if (style.tone) bits.push(style.tone);
    if (style.length) bits.push(style.length === "short" ? "短" : style.length === "long" ? "长" : style.length === "medium" ? "中" : style.length);
    if (style.language) bits.push(style.language);
    return dedupeChips(bits);
  }

  // Groups of labels that mean the same thing. A longer chip covers a shorter one in its group.
  const CHIP_MEANINGS = [
    ["短", "简短", "简洁", "精简", "简明", "short", "brief", "concise"],
    ["长", "详细", "详尽", "long", "detailed"],
    ["中", "适中", "中等", "medium"],
  ];

  function chipMeanings(text) {
    const n = String(text || "").trim().toLowerCase();
    const hit = [];
    CHIP_MEANINGS.forEach((words, i) => {
      for (const word of words) {
        const key = word.toLowerCase();
        if (n === key || (key.length >= 2 && n.includes(key))) {
          hit.push(i);
          break;
        }
      }
    });
    return hit;
  }

  function dedupeChips(bits) {
    const kept = [];
    const seen = new Set();
    for (const bit of bits) {
      const text = String(bit || "").trim();
      const key = text.toLowerCase();
      if (!text || seen.has(key)) continue;
      seen.add(key);
      kept.push(text);
    }
    return kept.filter((text, i) => {
      const mine = chipMeanings(text);
      if (!mine.length) return true;
      return !kept.some((other, j) => {
        if (i === j) return false;
        const theirs = chipMeanings(other);
        if (!mine.every((g) => theirs.includes(g))) return false;
        if (other.length === text.length) return j < i;
        return other.length > text.length;
      });
    });
  }

  function modelLabel(data) {
    const model = String(data.model || "").trim();
    const provider = String(data.provider || "").trim();
    if (provider && model && provider !== model) return provider + " / " + model;
    return model || provider;
  }

  function modelCertified(data) {
    const model = String((data && data.model) || "").trim();
    const provider = String((data && data.provider) || "").trim();
    if (!model) return false;
    const hits = (state.modelCatalog || []).filter((m) => m.model === model && (!provider || m.provider === provider));
    return hits.some((m) => m.certified);
  }

  function detailsView(data) {
    const wrap = document.createDocumentFragment();
    const bits = styleChips(data);
    const about = section("");
    if (data.persona) {
      const block = el("div", "kv");
      block.append(el("div", "k", "人设"), el("p", "clamp v", data.persona));
      about.append(block);
    }
    if (bits.length) {
      const block = el("div", "kv");
      const chips = el("div", "chips");
      for (const bit of bits) chips.append(el("span", "chip", bit));
      block.append(el("div", "k", "风格"), chips);
      about.append(block);
    }
    const model = modelLabel(data);
    if (model) {
      const row = el("div", "kv");
      const value = el("div", "v");
      value.append(document.createTextNode(model));
      if (modelCertified(data)) {
        const flag = el("span", "cert");
        flag.append(icon("check"), document.createTextNode("已认证"));
        value.append(flag);
      }
      row.append(el("div", "k", "模型"), value);
      about.append(row);
    }
    if (data.quiet_start || data.quiet_end) about.append(kv("免打扰", (data.quiet_start || "") + " – " + (data.quiet_end || "")));
    wrap.append(about);
    return wrap;
  }

  function scopeLabel(scope) {
    if (scope === "person") return "关于你";
    if (scope === "bot") return "关于 Bot";
    return scope || "资料";
  }

  function libraryView(data) {
    const wrap = document.createDocumentFragment();
    const facts = data.memory || [];
    if (!facts.length) wrap.append(el("p", "hint", "还没有记住任何事。对它说「记住……」就会出现在这里"));
    for (const f of facts) {
      const card = el("div", "lib-card");
      card.append(el("div", "k", scopeLabel(f.scope)), el("div", "v", f.text || ""));
      wrap.append(card);
    }
    return wrap;
  }

  function computerView(data) {
    const wrap = document.createDocumentFragment();
    const routines = section("例程");
    const list = data.routines || [];
    if (!list.length) routines.append(el("p", "hint", "没有例程"));
    for (const rt of list) {
      const card = el("div", "routine");
      card.title = String(rt.cron || "");
      const meta = el("span", "meta", cronText(rt.cron, true) + (rt.zone === "UTC" ? " · UTC" : ""));
      if (rt.enabled === false) {
        const dot = el("i", "status-dot off");
        setTip(dot, "停用");
        meta.append(document.createTextNode(" "), dot);
      }
      card.append(el("span", "", rt.name || rt.id), meta);
      routines.append(card);
    }
    const tasks = section("进行中");
    const agents = data.agents || [];
    const running = agents.filter((ag) => ag.status === "running");
    const finished = agents.filter((ag) => ag.status === "done" || ag.status === "failed" || ag.status === "cancelled");
    if (!running.length) tasks.append(el("p", "hint", "没有进行中的任务"));
    for (const ag of running) {
      const card = el("div", "task");
      const top = el("div", "task-top");
      top.append(el("i", "status-dot run"), el("span", "", "进行中"));
      card.append(top, el("p", "task-text", clip(ag.task || ag.id, 140)));
      const row = el("div", "row");
      const field = document.createElement("input");
      field.placeholder = "消息";
      field.maxLength = 4000;
      const go = el("button", "secondary", "引导");
      go.type = "button";
      go.addEventListener("click", () => controlAgent(ag.id, "steer", field.value));
      const stop = el("button", "secondary", "停止");
      stop.type = "button";
      stop.addEventListener("click", () => controlAgent(ag.id, "cancel", ""));
      row.append(field, go, stop);
      card.append(row);
      tasks.append(card);
    }
    wrap.append(routines, tasks);
    if (finished.length) {
      const recent = section("");
      const toggle = el("button", "secondary recent-toggle");
      toggle.type = "button";
      const list = el("div", "recent-tasks");
      const botName = data.name;
      const sync = () => {
        const open = state.recentTasksOpen.has(botName);
        toggle.replaceChildren(icon(open ? "chevronDown" : "chevron"),
          el("span", "", "最近结束 · " + finished.length));
        toggle.setAttribute("aria-expanded", String(open));
        list.hidden = !open;
      };
      toggle.addEventListener("click", () => {
        if (state.recentTasksOpen.has(botName)) state.recentTasksOpen.delete(botName);
        else state.recentTasksOpen.add(botName);
        sync();
      });
      for (const ag of finished) {
        const row = el("div", "recent-task");
        row.append(el("span", "recent-title", clip(ag.task || ag.id, 80)),
          el("span", ag.status === "failed" ? "recent-status failed" : "recent-status",
            ag.status === "failed" ? "失败" : ag.status === "cancelled" ? "已取消" : "完成"));
        list.append(row);
      }
      sync();
      recent.append(toggle, list);
      wrap.append(recent);
    }
    return wrap;
  }

  async function loadModelCatalog() {
    if (state.modelCatalog) return;
    try {
      const data = await api("/api/models?all=1");
      state.modelCatalog = (data && data.models) || [];
    } catch (e) {
      state.modelCatalog = [];
    }
  }

  async function loadPanel() {
    if (!state.sel) { paintPanel(); return; }
    if (state.sel.type === "group") { state.panelData = null; paintPanel(); return; }
    const selection = state.sel;
    const request = ++state.panelLoad;
    try {
      const data = await api("/api/bots/" + encodeURIComponent(selection.id));
      await loadModelCatalog();
      if (state.sel !== selection || request !== state.panelLoad) return;
      state.panelData = data;
      paintPanel();
    } catch (e) {
      if (state.sel === selection && request === state.panelLoad) {
        panelBody.replaceChildren(el("div", "muted", "连不上 XBot"));
      }
    }
  }

  function labeled(label, node) {
    const box = el("label", "field", label);
    box.append(node);
    return box;
  }

  function textInput(value, max) {
    const node = document.createElement("input");
    node.value = value || "";
    if (max) node.maxLength = max;
    return node;
  }

  function choice(options, value) {
    const node = document.createElement("select");
    for (const pair of options) {
      const o = document.createElement("option");
      o.value = pair[0];
      o.textContent = pair[1];
      if (pair[0] === (value || "")) o.selected = true;
      node.append(o);
    }
    return node;
  }

  function profileForm(data) {
    const wrap = document.createElement("div");
    const head = el("div", "profile-head");
    let mark = avatar(data.name, paintOfBot(data), "lg");
    const file = document.createElement("input");
    file.type = "file";
    file.accept = "image/png,image/jpeg,image/gif,image/webp";
    file.hidden = true;
    const pick = el("button", "secondary", "上传头像");
    pick.type = "button";
    const clear = el("button", "secondary", "清除头像");
    clear.type = "button";
    const stack = el("div", "stack");
    stack.append(pick, clear, file);
    head.append(mark, stack);
    const style = data.style || {};
    const name = textInput(data.name, 64);
    const description = textInput(data.description, 160);
    const tone = textInput(style.tone, 80);
    const length = choice([["", "未设置"], ["short", "短"], ["medium", "中"], ["long", "长"]], style.length);
    const emoji = choice([["", "未设置"], ["none", "不用"], ["spare", "少量"], ["often", "常用"]], style.emoji);
    const language = textInput(style.language, 40);
    const quietStart = textInput(data.quiet_start, 5);
    quietStart.type = "time";
    const quietEnd = textInput(data.quiet_end, 5);
    quietEnd.type = "time";
    const persona = document.createElement("textarea");
    persona.value = data.persona || "";
    persona.maxLength = 32000;
    const err = el("p", "error", "");
    err.hidden = true;
    const save = $("modal-ok");
    save.textContent = "保存";
    save.hidden = false;
    save.type = "submit";
    const exp = el("button", "text-btn", "导出模板");
    exp.type = "button";
    const imp = el("button", "text-btn", "导入模板");
    imp.type = "button";
    const fileIn = document.createElement("input");
    fileIn.type = "file";
    fileIn.accept = "application/json,.json";
    fileIn.hidden = true;
    const actions = el("div", "profile-templates");
    actions.append(exp, imp, fileIn);
    modalForm.querySelector(".modal-actions").prepend(actions);
    wrap.append(
      head,
      labeled("标识", name),
      labeled("简介", description),
      labeled("语气", tone),
      labeled("回复长度", length),
      labeled("表情", emoji),
      labeled("语言", language),
      labeled("免打扰开始", quietStart),
      labeled("免打扰结束", quietEnd),
      labeled("人设", persona),
      err,
    );
    const showErr = (e, fallback) => { err.hidden = false; err.textContent = visibleError(e, fallback); };
    pick.addEventListener("click", () => file.click());
    file.addEventListener("change", async () => {
      if (!file.files || !file.files[0]) return;
      try {
        await uploadAvatar(data.name, file.files[0]);
        await refreshLists();
        await loadPanel();
        const fresh = state.panelData || data;
        const next = avatar(fresh.name, paintOfBot(fresh), "lg");
        mark.replaceWith(next);
        mark = next;
      } catch (e) { showErr(e, "头像上传失败"); }
    });
    clear.addEventListener("click", async () => {
      try {
        await api("/api/bots/" + encodeURIComponent(data.name) + "/avatar", { method: "DELETE" });
        await refreshLists();
        await loadPanel();
        const fresh = state.panelData || data;
        const next = avatar(fresh.name, paintOfBot(fresh), "lg");
        mark.replaceWith(next);
        mark = next;
      } catch (e) { showErr(e, "清除失败"); }
    });
    modalForm.onsubmit = async (ev) => {
      ev.preventDefault();
      err.hidden = true;
      const body = {
        name: name.value.trim(),
        description: description.value.trim(),
        tone: tone.value.trim(),
        length: length.value,
        emoji: emoji.value,
        language: language.value.trim(),
        quiet_start: quietStart.value,
        quiet_end: quietEnd.value,
        persona: persona.value.trim(),
      };
      if ((body.quiet_start === "") !== (body.quiet_end === "")) {
        err.hidden = false;
        err.textContent = "免打扰需要开始和结束，或都留空。";
        return;
      }
      try {
        const res = await api("/api/bots/" + encodeURIComponent(data.name), { method: "PATCH", body: body });
        closeModal();
        await refreshLists();
        const next = (res && res.name) || body.name || data.name;
        if (state.sel && next !== state.sel.id) await selectBot(next);
        else await loadPanel();
      } catch (e) { showErr(e, "保存失败"); }
    };
    exp.addEventListener("click", async () => {
      err.hidden = true;
      try { await downloadTemplate(data.name); }
      catch (e) { showErr(e, "导出失败"); }
    });
    imp.addEventListener("click", () => fileIn.click());
    fileIn.addEventListener("change", async () => {
      const f = fileIn.files && fileIn.files[0];
      fileIn.value = "";
      if (!f) return;
      err.hidden = true;
      try {
        const tpl = JSON.parse(await f.text());
        const res = await api("/api/bots/import", { method: "POST", body: tpl });
        closeModal();
        await refreshLists();
        if (res && res.name) await selectBot(res.name);
      } catch (e) { showErr(e, "导入失败"); }
    });
    return wrap;
  }

  function openProfile(data) {
    if (!data || !data.name) return;
    $("modal-title").textContent = "编辑资料";
    modalForm.classList.add("profile-modal");
    $("modal-cancel").textContent = "取消";
    modalFields.replaceChildren(profileForm(data));
    modalError.hidden = true;
    modal.hidden = false;
  }

  async function uploadAvatar(name, file) {
    const body = new FormData();
    body.append("file", file);
    const res = await apiFetch("/api/bots/" + encodeURIComponent(name) + "/avatar", {
      method: "POST", credentials: "same-origin", body: body,
    });
    const text = await res.text();
    let payload = {};
    if (text) { try { payload = JSON.parse(text); } catch (e) { payload = { error: text }; } }
    if (!res.ok) throw new Error(payload.error || "头像上传失败");
    return payload;
  }

  async function downloadTemplate(name) {
    const res = await apiFetch("/api/bots/" + encodeURIComponent(name) + "/template", { credentials: "same-origin" });
    if (!res.ok) {
      let msg = "导出失败";
      try { const data = await res.json(); if (data.error) msg = data.error; } catch (e) {}
      throw new Error(msg);
    }
    const blob = await res.blob();
    const a = document.createElement("a");
    a.href = URL.createObjectURL(blob);
    a.download = name + ".json";
    a.click();
    URL.revokeObjectURL(a.href);
  }

  function openFork(src) {
    openModal("复刻 Bot", [
      { name: "name", label: "标识", placeholder: "reviewer" },
      { name: "role", label: "角色", multiline: true },
      { name: "group", label: "群" },
    ], async (v) => {
      const name = (v.name || "").trim();
      const role = (v.role || "").trim();
      if (!name) throw new Error("需要一个名字");
      if (!role) throw new Error("需要一个角色");
      const body = { name: name, role: role };
      const group = (v.group || "").trim();
      if (group) body.group = group;
      await api("/api/bots/" + encodeURIComponent(src) + "/fork", { method: "POST", body: body });
      await refreshLists();
      await selectBot(name);
    });
  }

  async function togglePin(name, pinned) {
    try {
      await api("/api/bots/" + encodeURIComponent(name) + "/pin", { method: "POST", body: { pinned: pinned } });
      await refreshLists();
      if (state.sel && state.sel.type === "bot" && state.sel.id === name && panel.classList.contains("open")) await loadPanel();
    } catch (e) {
      setStatus(visibleError(e), true);
    }
  }

  async function controlAgent(id, kind, text) {
    if (!state.sel || state.sel.type !== "bot") return;
    const path = "/api/bots/" + encodeURIComponent(state.sel.id) + "/agents/" + encodeURIComponent(id) + "/" + kind;
    try {
      if (kind === "cancel") await api(path, { method: "POST" });
      else await api(path, { method: "POST", body: { text: text } });
      setStatus(kind === "cancel" ? "已取消" : "已记下");
      await loadPanel();
    } catch (e) {
      setStatus(visibleError(e), true);
    }
  }

  function closeDrawer() {
    sidebar.classList.remove("open");
    syncBackdrop();
  }

  function syncBackdrop() {
    const drawer = window.innerWidth < 768 && sidebar.classList.contains("open");
    backdrop.hidden = !drawer;
    const topBottom = document.querySelector(".top").getBoundingClientRect().bottom;
    $("shell").style.setProperty("--panel-overlay-top", topBottom + "px");
    $("panel-scrim").hidden = !(window.innerWidth <= 1100 && panel.classList.contains("open") && !drawer);
  }

  async function refreshLists() {
    const data = await api("/api/bots");
    const groups = await api("/api/groups");
    state.bots = data.bots || [];
    state.folders = groups.folders || [];
    state.groups = groups.groups || [];
    renderLists();
    warmOne().catch(() => {});
  }

  let warming = false;
  async function warmOne() {
    if (warming || state.streaming) return;
    const now = Date.now();
    const jobs = [];
    for (const b of state.bots) jobs.push({ key: "b:" + b.name, kind: "bot", id: b.name });
    for (const g of state.groups) jobs.push({ key: "g:" + g.id, kind: "group", id: g.id });
    const job = jobs.find((j) => state.preview[j.key] == null || now - (state.previewAt[j.key] || 0) > 20000);
    if (!job) return;
    if (state.sel && ((job.kind === "bot" && state.sel.type === "bot" && state.sel.id === job.id) || (job.kind === "group" && state.sel.type === "group" && state.sel.id === job.id))) {
      state.previewAt[job.key] = now;
      return;
    }
    warming = true;
    try {
      if (job.kind === "bot") {
        const data = await api("/api/bots/" + encodeURIComponent(job.id) + "/transcript");
        state.preview[job.key] = previewFromMessages(data.messages || []);
      } else {
        const data = await api("/api/groups/" + encodeURIComponent(job.id));
        state.preview[job.key] = previewFromGroup(data.messages || []);
      }
      state.previewAt[job.key] = Date.now();
      renderLists();
    } catch (e) {
      if (state.preview[job.key] == null) state.preview[job.key] = "";
      state.previewAt[job.key] = Date.now();
    } finally {
      warming = false;
    }
  }

  async function refreshNodePending() {
    const host = $("node-pending");
    if (!host) return;
    let data;
    try { data = await api("/api/node/pending"); } catch (e) { return; }
    const items = (data && data.pending) || [];
    host.replaceChildren();
    if (!items.length) {
      host.hidden = true;
      return;
    }
    host.hidden = false;
    for (const item of items) {
      const detail = [item.caller || "", item.id || ""].filter(Boolean).join(" · ");
      host.append(pendingApprovalCard({
        title: "节点命令",
        subtitle: detail,
        summary: item.command || "",
        wait: 0,
        decide: (ok) => api("/api/node/pending/" + encodeURIComponent(item.id || ""), { method: "POST", body: { allow: ok } }),
      }));
    }
  }

  async function refreshNotes() {
    const data = await api("/api/notes");
    const notes = data.notes || [];
    if (state.knownNotes) {
      const seen = new Set(state.knownNotes);
      for (const n of notes) {
        if (!seen.has(n.id)) pushNote(n.bot || "XBot", n.text || "", n.id);
      }
    }
    state.knownNotes = notes.map((n) => n.id);
    state.notes = notes;
    renderNotes();
  }

  function pushNote(title, body, tag) {
    if (typeof Notification === "undefined" || Notification.permission !== "granted") return;
    try {
      if (navigator.serviceWorker && navigator.serviceWorker.controller) {
        navigator.serviceWorker.controller.postMessage({ type: "notify", title: title, body: body, tag: tag });
        return;
      }
      new Notification(title, { body: body, tag: tag });
    } catch (e) {}
  }

  function setWorking(host, name) {
    let status = host.querySelector(".live-status");
    if (!status) {
      status = el("div", "meta live-status");
      host.append(status);
    }
    status.replaceChildren();
    const dot = el("i", "status-dot run");
    setTip(dot, "正在处理");
    status.append(dot);
    if (name) status.append(el("span", "working-name", name));
  }

  function approvalCard(payload) {
    return pendingApprovalCard({
      tool: payload.tool || payload.name || "",
      title: payload.title || "",
      subtitle: payload.subtitle || "",
      summary: payload.summary || "",
      deadline: payload.deadline || payload.expires || "",
      decide: async (ok) => {
        if (!state.sel || state.sel.type !== "bot") throw new Error("没有选中的 Bot");
        await api("/api/bots/" + encodeURIComponent(state.sel.id) + "/approvals/" + encodeURIComponent(payload.id || ""), {
          method: "POST",
          body: { allow: ok },
        });
      },
    });
  }

  function restoreSendFeedback(inner) {
    state.pendingSends = state.pendingSends.filter((row) => !row.accepted);
    if (state.pendingSends.length) inner.querySelector(".empty")?.remove();
    inner.append(...state.pendingSends, ...state.replyErrors);
  }

  let sendStatus;
  function announceSend(text) {
    if (!sendStatus) {
      sendStatus = el("div", "visually-hidden");
      sendStatus.setAttribute("role", "status");
      sendStatus.setAttribute("aria-live", "polite");
      document.body.append(sendStatus);
    }
    sendStatus.textContent = text;
  }

  function liveBubble() {

    const inner = thread.querySelector(".thread-inner") || thread;
    const empty = inner.querySelector(".empty");
    if (empty) empty.remove();
    const row = el("div", "msg bot live");
    const who = state.sel ? state.sel.id : "Bot";
    row.append(avatar(who, paintOf(who)));
    const body = el("div", "body");
    const shown = niceName(who);
    if (shown) {
      const name = el("div", "who", shown);
      setTip(name, who);
      body.append(name);
    }
    const bubble = el("div", "bubble");
    const prose = el("div", "prose");
    const dots = el("span", "typing-dots");
    dots.setAttribute("aria-label", "正在回复");
    for (let i = 0; i < 3; i++) dots.append(el("i", ""));
    bubble.append(prose, dots);
    const live = el("div", "meta live-status");
    const dot = el("i", "status-dot run");
    setTip(dot, "正在回复");
    live.append(dot);
    body.append(bubble, live);
    row.append(body);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    return prose;
  }

  async function readSSE(res, selection, prose) {
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
    let acc = "";
    // After a switch keep reading without touching the view: closing the
    // request would cancel the turn on the server.
    const drain = async () => { try { while (!(await reader.read()).done) { /* discard */ } } catch (e) { /* gone */ } };
    if (state.sel !== selection) { await drain(); return; }
    while (true) {
      const step = await reader.read();
      if (state.sel !== selection) { await drain(); return; }
      if (step.done) break;
      buf += dec.decode(step.value, { stream: true });
      let cut;
      while ((cut = buf.indexOf("\n\n")) >= 0) {
        const block = buf.slice(0, cut);
        buf = buf.slice(cut + 2);
        let event = "message";
        let data = "";
        for (const line of block.split("\n")) {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:")) data += line.slice(5).trim();
        }
        if (!data) continue;
        let payload = {};
        try { payload = JSON.parse(data); } catch (e) { continue; }
        const host = prose.closest(".body") || prose.parentElement;
        if (event === "delta" && payload.text) {
          const status = host && host.querySelector(".live-status");
          if (status) status.remove();
          host.querySelector(".typing-dots")?.remove();
          acc += payload.text;
          prose.replaceChildren();
          appendMarkdown(prose, acc);
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "tool") {
          const who = state.sel && state.sel.id;
          if (host) {
            setWorking(host, payload.name || "");
            const phrase = peerPhrase(payload.name || "", { text: payload.summary || "" }, who);
            if (phrase && phrase.text && host.parentElement) {
              host.parentElement.insertAdjacentElement("afterend", systemLine(phrase.text, who, phrase.peer));
            }
          }
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "approval" && host) {
          const status = host.querySelector(".live-status");
          if (status) status.remove();
          host.append(approvalCard(payload));
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "done" && payload.reply_to && host) {
          const live = host.closest(".msg");
          if (live && !live.querySelector(".quote-block")) {
            const block = quoteBlock(state.messages, payload.reply_to);
            const bubble = host.querySelector(".bubble");
            if (bubble) host.insertBefore(block, bubble);
            else host.prepend(block);
          }
        } else if (event === "done" && payload.background && host) {
          const status = host.querySelector(".live-status");
          if (status) status.remove();
        } else if (event === "error") {
          throw new Error("发送失败");
        }
      }
    }
  }

  function optimisticMessage(text) {
    let inner = thread.querySelector(".thread-inner:not(.thread-skeleton)");
    if (!inner) {
      inner = el("div", "thread-inner");
      thread.replaceChildren(inner);
    }
    inner.querySelector(".empty")?.remove();
    const row = el("div", "msg user optimistic");
    const body = el("div", "bubble-col");
    body.append(el("div", "bubble", text), el("div", "send-state", "发送中"));
    row.append(body);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    state.pendingSends.push(row);
    return row;
  }

  async function sendText(text, pending) {
    text = (text || "").trim();
    // A message sent while a reply is streaming interrupts it: the server forks the
    // running turn to the background and answers this one at once.
    if (!text || !state.sel) return;
    const selection = state.sel;
    ++state.threadLoad;
    pending = pending || optimisticMessage(text);
    pending.dataset.sendText = text;
    const marker = pending.querySelector(".send-state");
    marker.className = "send-state";
    marker.replaceChildren(document.createTextNode("发送中"));
    announceSend("发送中");
    const prose = selection.type === "bot" ? liveBubble() : null;
    const live = prose?.closest(".msg.live");
    let accepted = false;
    const markSent = () => {
      accepted = true;
      pending.accepted = true;
      if (state.sel === selection) state.pendingSends = state.pendingSends.filter((row) => {
        // Only a failed copy of this same text is resolved by this send.
        if (row !== pending && row.querySelector(".send-failed") && row.dataset.sendText === text) {
          row.remove();
          return false;
        }
        return true;
      });
      pending.classList.remove("optimistic");
      marker.remove();
    };
    state.streaming = (state.streaming || 0) + 1;
    let counted = true;
    const settle = () => {
      if (counted) {
        counted = false;
        if (state.sel === selection) state.streaming = Math.max(0, state.streaming - 1);
      }
    };
    setStatus("正在回复");
    try {
      const payload = { text: text };
      if (state.replyTo && state.replyTo.seq > 0) payload.reply_to = state.replyTo.seq;
      const res = await apiFetch("/api/" + (selection.type === "group" ? "groups/" : "bots/") + encodeURIComponent(selection.id) + "/messages", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });
      if (!res.ok) throw new Error("发送失败");
      markSent();
      const ct = res.headers.get("content-type") || "";
      if (ct.includes("application/json")) {
        const data = await res.json().catch(() => ({}));
        if (state.sel !== selection) return;
        setStatus(data.status === "queued" ? "已排队" : "");
      } else if (prose) {
        await readSSE(res, selection, prose);
      }
      if (state.sel !== selection) return;
      state.sig = "";
      clearQuote();
      settle();
      bgRefresh();
      await loadThread();
      if (state.sel === selection && subtitle.textContent === "正在回复") setStatus("");
    } catch (e) {
      // Session expired mid-send: keep the text so the input gets it back
      // after the owner logs in again (showLogin already dropped the thread).
      if (!accepted && !authenticated && selection.type === "bot") {
        try { sessionStorage.setItem("xbot-draft:" + selection.id, text); } catch (err) {}
      }
      if (state.sel !== selection) return;
      setStatus("");
      if (accepted) {
        const error = el("div", "reply-error", "回复出错了");
        live?.replaceWith(error);
        if (!live) thread.querySelector(".thread-inner")?.append(error);
        state.replyErrors.push(error);
        announceSend("回复出错了");
        settle();
        state.sig = "";
        await loadThread();
        return;
      }
      live?.remove();
      marker.className = "send-state send-failed";
      if (!input.value.trim()) {
        input.value = text;
        marker.replaceChildren(el("span", "", "没发出去，原文还在输入框里"));
        announceSend("没发出去，原文还在输入框里");
      } else {
        // A newer draft is in the composer: keep it, and let the owner bring this text back.
        const back = el("button", "secondary", "放回输入框");
        back.type = "button";
        back.addEventListener("click", () => {
          input.value = input.value.trim() ? input.value.replace(/\s*$/, "\n") + text : text;
          syncComposer();
          input.focus();
        });
        marker.replaceChildren(el("span", "", "没发出去"), back);
        announceSend("没发出去，可以放回输入框");
      }
      syncComposer();
    } finally {
      live?.querySelector(".typing-dots")?.remove();
      live?.querySelector(".live-status")?.remove();
      if (prose && !prose.textContent && !live?.querySelector(".approval")) live?.remove();
      settle();
    }
  }

  function closeModal() {
    modal.hidden = true;
    modalForm.classList.remove("profile-modal");
    modalForm.querySelector(".profile-templates")?.remove();
    modalError.hidden = true;
    modalError.textContent = "";
    modalForm.onsubmit = null;
    $("modal-ok").hidden = false;
    $("modal-ok").className = "primary";
    $("modal-cancel").textContent = "取消";
  }

  function openModal(title, fields, onOk, okLabel) {
    $("modal-title").textContent = title;
    $("modal-ok").hidden = false;
    $("modal-ok").textContent = okLabel || "创建";
    $("modal-ok").className = okLabel === "删除" ? (title === "删除 Bot" ? "text-btn danger" : "secondary") : "primary";
    $("modal-ok").disabled = false;
    $("modal-cancel").textContent = "取消";
    modalFields.replaceChildren();
    modalError.hidden = true;
    const inputs = {};
    for (const f of fields) {
      if (f.type === "note") {
        modalFields.append(el("p", "note", f.label));
        continue;
      }
      if (f.type === "check") {
        const row = el("label", "check-row");
        const cb = document.createElement("input");
        cb.type = "checkbox";
        cb.checked = !!f.checked;
        inputs[f.name] = cb;
        row.append(cb, el("span", "", f.label));
        modalFields.append(row);
        continue;
      }
      const label = el(f.type === "checks" ? "div" : "label", "field");
      label.append(el("span", "", f.label));
      if (f.type === "choice") {
        const box = el("div", "choice");
        box.setAttribute("role", "radiogroup");
        const group = "choice-" + f.name;
        const current = f.value || (f.options && f.options[0] && f.options[0][0]) || "";
        for (const pair of f.options || []) {
          const opt = el("label", "choice-opt");
          const radio = document.createElement("input");
          radio.type = "radio";
          radio.name = group;
          radio.value = pair[0];
          radio.checked = pair[0] === current;
          opt.append(radio, document.createTextNode(pair[1]));
          box.append(opt);
        }
        inputs[f.name] = box;
        label.append(box);
      } else if (f.type === "checks") {
        const box = el("div", "checks");
        inputs[f.name] = [];
        for (const opt of f.options || []) {
          const value = Array.isArray(opt) ? opt[0] : opt;
          const text = Array.isArray(opt) ? opt[1] : opt;
          const row = el("label", "member-option");
          setTip(row, value);
          const cb = document.createElement("input");
          cb.type = "checkbox";
          cb.value = value;
          cb.addEventListener("change", () => {
            inputs[f.name] = Array.from(box.querySelectorAll("input:checked")).map((n) => n.value);
          });
          const bot = state.bots.find((b) => b.name === value);
          row.append(cb, avatar(value, bot ? paintOfBot(bot) : null), el("span", "member-name", text), icon("check"));
          box.append(row);
        }
        label.append(box);
      } else if (f.multiline) {
        const ta = document.createElement("textarea");
        ta.rows = 3;
        ta.placeholder = f.placeholder || "";
        ta.maxLength = 4000;
        inputs[f.name] = ta;
        label.append(ta);
      } else {
        const inp = document.createElement("input");
        inp.placeholder = f.placeholder || "";
        inp.maxLength = f.maxLength || 64;
        if (f.value) inp.value = f.value;
        inp.autocomplete = "off";
        inputs[f.name] = inp;
        label.append(inp);
      }
      modalFields.append(label);
    }
    modal.hidden = false;
    const first = modalFields.querySelector("input, textarea");
    if (first) first.focus();
    modalForm.onsubmit = async (ev) => {
      ev.preventDefault();
      const values = {};
      for (const key of Object.keys(inputs)) {
        const node = inputs[key];
        if (Array.isArray(node)) values[key] = node.slice();
        else if (node instanceof HTMLInputElement && node.type === "checkbox") values[key] = node.checked;
        else if (node instanceof HTMLElement && node.classList.contains("choice")) {
          const picked = node.querySelector("input:checked");
          values[key] = picked ? picked.value : "";
        } else values[key] = node.value;
      }
      const ok = $("modal-ok");
      ok.disabled = true;
      try {
        await onOk(values);
        closeModal();
      } catch (e) {
        modalError.hidden = false;
        modalError.textContent = visibleError(e);
      } finally {
        ok.disabled = false;
      }
    };
  }

  function exportName(res, name, format) {
    const disp = res.headers.get("content-disposition") || "";
    const star = /filename\*=UTF-8''([^;]+)/i.exec(disp);
    if (star) {
      try { return decodeURIComponent(star[1].trim()); } catch (e) {}
    }
    const plain = /filename="([^"]+)"/i.exec(disp) || /filename=([^;]+)/i.exec(disp);
    if (plain) return plain[1].trim();
    return name + (format === "jsonl" ? ".jsonl" : ".md");
  }

  async function downloadExport(name, format, tools) {
    const q = "format=" + encodeURIComponent(format) + "&tools=" + (tools ? "1" : "0");
    const res = await apiFetch("/api/bots/" + encodeURIComponent(name) + "/export?" + q, {
      credentials: "same-origin",
    });
    if (res.status === 401) {
      throw new Error("需要重新登录");
    }
    if (!res.ok) {
      let msg = "";
      const text = await res.text().catch(() => "");
      if (text) {
        try { msg = (JSON.parse(text).error || "").trim(); } catch (e) { msg = text.trim().slice(0, 160); }
      }
      if (res.status === 404 || res.status === 501) throw new Error("导出失败");
      throw new Error(msg || "导出失败");
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = exportName(res, name, format);
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1500);
  }

  function openExport() {
    if (!state.sel || state.sel.type !== "bot") return;
    const name = state.sel.id;
    closePops();
    openModal("导出对话", [
      { name: "format", label: "格式", type: "choice", value: "md", options: [["md", "Markdown"], ["jsonl", "JSONL"]] },
      { name: "tools", label: "包含工具细节", type: "check" },
    ], async (v) => {
      const format = v.format === "jsonl" ? "jsonl" : "md";
      await downloadExport(name, format, !!v.tools);
    }, "下载");
  }

  function syncMore() {
    const bot = !!(state.sel && state.sel.type === "bot");
    $("more").hidden = !bot;
    if (!bot) {
      $("more-menu").hidden = true;
      $("more").setAttribute("aria-expanded", "false");
    }
  }

  function openNewBot() {
    openModal("新建 Bot", [
      { name: "name", label: "标识", placeholder: "ada" },
      { name: "persona", label: "人设", multiline: true },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      await api("/api/bots", { method: "POST", body: { name: name, persona: (v.persona || "").trim() } });
      await refreshLists();
      await selectBot(name);
    });
  }

  function openNewGroup() {
    openModal("新建群组", [
      { name: "name", label: "名字", placeholder: "晨会" },
      { name: "members", label: "成员", type: "checks", options: state.bots.map((b) => [b.name, niceName(b.name)]) },
    ], async (v) => {
      const members = v.members || [];
      if (members.length < 2) throw new Error("至少选两个 Bot");
      const g = await api("/api/groups", { method: "POST", body: { name: (v.name || "").trim(), members: members } });
      await refreshLists();
      if (g.id) await selectGroup(g.id);
    });
  }

  function syncComposer() {
    const hasText = input.value.trim().length > 0;
    composer.classList.toggle("has-text", hasText);
    $("send").disabled = !hasText;
    $("send").classList.toggle("primary", hasText);
    $("send").classList.toggle("secondary", !hasText);
  }

  function closeAccount() {
    $("account-menu").hidden = true;
    $("account").setAttribute("aria-expanded", "false");
  }

  function closePops() {
    closeAccount();
    $("connect-pop").hidden = true;
    $("connect").setAttribute("aria-expanded", "false");
    $("more-menu").hidden = true;
    $("more").setAttribute("aria-expanded", "false");
    closePanelMenu();
    closeFolderMenus();
    closeToolPop();
    notesEl.hidden = true;
    $("bell").setAttribute("aria-expanded", "false");
  }

  document.addEventListener("click", () => { if (authenticated && subtitleError) setStatus(""); }, true);
  document.addEventListener("submit", () => { if (authenticated && subtitleError) setStatus(""); }, true);

  $("login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const err = $("login-error");
    err.hidden = true;
    if (!$("token").value.trim()) {
      err.hidden = false;
      err.textContent = "先填登录码";
      $("token").focus();
      return;
    }
    try {
      const res = await apiFetch("/api/login", {
        method: "POST",
        credentials: "same-origin",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ token: $("token").value.trim() }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(res.status === 401 ? "登录码不对，重新复制终端里 login token 那一行" : "登录失败，稍后再试");
      state.owner = data.owner || "";
      renderMe();
      showApp();
      await boot();
      startPoll();
    } catch (e) {
      err.hidden = false;
      err.textContent = visibleError(e, "登录失败");
    }
  });

  $("logout").addEventListener("click", async () => {
    closeAccount();
    try { await api("/api/logout", { method: "POST" }); } catch (e) {}
    state.sel = null;
    state.knownNotes = null;
    state.listSig = "";
    try { sessionStorage.removeItem("xbot-selected-bot"); } catch (e) {}
    showLogin();
  });

  function toggleTheme() {
    const dark = document.documentElement.getAttribute("data-theme") === "dark";
    applyTheme(dark ? "light" : "dark");
  }
  $("theme").addEventListener("click", toggleTheme);
  $("account-theme").addEventListener("click", () => {
    closeAccount();
    toggleTheme();
  });

  let compactSidebarChoice = null;
  let wideSidebarCollapsed = false;
  function syncSidebar() {
    const compact = window.innerWidth >= 768 && window.innerWidth <= 1100;
    const collapsed = compact ? compactSidebarChoice !== false : wideSidebarCollapsed;
    sidebar.classList.toggle("collapsed", collapsed);
    $("collapse").replaceChildren(icon("sidebar"), document.createTextNode(collapsed ? "展开列表" : "收起列表"));
  }
  $("logout").prepend(icon("logout"));
  $("account").addEventListener("click", () => {
    const open = $("account-menu").hidden;
    closePops();
    $("account-menu").hidden = !open;
    $("account").setAttribute("aria-expanded", open ? "true" : "false");
  });
  $("collapse").addEventListener("click", () => {
    closeAccount();
    if (window.innerWidth < 768) { closeDrawer(); return; }
    const collapsed = !sidebar.classList.contains("collapsed");
    if (window.innerWidth <= 1100) compactSidebarChoice = collapsed;
    else wideSidebarCollapsed = collapsed;
    syncSidebar();
  });

  function rememberPanel(open) {
    state.panelTouched = true;
    try { localStorage.setItem("xbot-panel-open", String(open)); } catch (e) {}
  }
  $("panel-scrim").addEventListener("click", () => $("panel-close").click());

  $("menu").addEventListener("click", () => {
    sidebar.classList.add("open");
    if (window.innerWidth < 768 && panel.classList.contains("open")) {
      rememberPanel(false);
      panel.classList.remove("open");
    }
    $("info").setAttribute("aria-expanded", "false");
    syncBackdrop();
  });

  backdrop.addEventListener("click", () => {
    state.panelTouched = true;
    sidebar.classList.remove("open");
    panel.classList.remove("open");
    $("info").setAttribute("aria-expanded", "false");
    backdrop.hidden = true;
  });

  $("back").addEventListener("click", () => { closePeer(); });

  thread.addEventListener("scroll", () => {
    const peer = state.peer;
    if (!peer || peer.loading || !peer.more || !peer.cursor) return;
    if (thread.scrollTop > 48) return;
    peer.loading = true;
    loadPeer(peer.cursor).finally(() => { if (state.peer) state.peer.loading = false; });
  });

  $("info").addEventListener("click", async () => {
    const open = panel.classList.toggle("open");
    rememberPanel(open);
    $("info").setAttribute("aria-expanded", open ? "true" : "false");
    if (open && window.innerWidth < 768) sidebar.classList.remove("open");
    syncBackdrop();
    if (open) {
      try { await loadPanel(); } catch (e) { setStatus(visibleError(e), true); }
    }
  });

  $("panel-more").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const menu = $("panel-menu");
    menu.hidden = !menu.hidden;
    $("panel-more").setAttribute("aria-expanded", menu.hidden ? "false" : "true");
  });

  $("panel-export").addEventListener("click", () => {
    closePanelMenu();
    openExport();
  });

  $("panel-close").addEventListener("click", () => {
    rememberPanel(false);
    panel.classList.remove("open");
    $("info").setAttribute("aria-expanded", "false");
    syncBackdrop();
  });

  $("bell").addEventListener("click", async (ev) => {
    ev.stopPropagation();
    if (typeof Notification !== "undefined" && Notification.permission === "default") {
      try { await Notification.requestPermission(); } catch (e) {}
    }
    if (!state.notes.length) return;
    hideTip();
    notesEl.hidden = !notesEl.hidden;
    $("bell").setAttribute("aria-expanded", String(!notesEl.hidden));
  });

  $("connect").addEventListener("click", (ev) => {
    ev.stopPropagation();
    closeAccount();
    const pop = $("connect-pop");
    pop.hidden = !pop.hidden;
    $("connect").setAttribute("aria-expanded", pop.hidden ? "false" : "true");
    if (!pop.hidden) loadPush().catch(() => {});
  });

  function urlBase64ToUint8Array(s) {
    const pad = "=".repeat((4 - (s.length % 4)) % 4);
    const b64 = (s + pad).replace(/-/g, "+").replace(/_/g, "/");
    const raw = atob(b64);
    const out = new Uint8Array(raw.length);
    for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
    return out;
  }

  function paintPush(data) {
    state.push = data;
    const btn = $("phone-push");
    const hint = $("phone-push-hint");
    const on = !!(data && data.subscribed);
    if (btn) {
      btn.setAttribute("aria-checked", on ? "true" : "false");
      btn.setAttribute("aria-label", on ? "关闭手机推送" : "开启手机推送");
    }
    if (hint) {
      hint.classList.remove("bad");
      hint.hidden = !on;
      hint.textContent = "免打扰时段不推送";
    }
  }

  function showPushErr(text) {
    const hint = $("phone-push-hint");
    if (!hint) return;
    hint.hidden = false;
    hint.classList.add("bad");
    hint.textContent = text;
  }

  async function loadPush() {
    const data = await api("/api/push");
    paintPush(data);
    return data;
  }

  async function togglePush() {
    try {
      const data = state.push || await loadPush();
      if (data.subscribed) {
        await api("/api/push/subscribe", { method: "DELETE", body: {} });
        await loadPush();
        return;
      }
      if (!("serviceWorker" in navigator) || !("PushManager" in window) || typeof Notification === "undefined") {
        showPushErr("这台浏览器不支持后台推送。");
        return;
      }
      let perm = Notification.permission;
      if (perm === "default") perm = await Notification.requestPermission();
      if (perm !== "granted") {
        showPushErr("需要允许通知，手机才能在后台收到。");
        return;
      }
      const reg = await navigator.serviceWorker.ready;
      const sub = await reg.pushManager.subscribe({
        userVisibleOnly: true,
        applicationServerKey: urlBase64ToUint8Array(data.public_key),
      });
      const json = sub.toJSON();
      await api("/api/push/subscribe", {
        method: "POST",
        body: {
          endpoint: json.endpoint,
          p256dh: json.keys && json.keys.p256dh,
          auth: json.keys && json.keys.auth,
        },
      });
      await loadPush();
    } catch (e) {
      showPushErr(visibleError(e, "开启失败"));
    }
  }

  const phonePush = $("phone-push");
  if (phonePush) phonePush.addEventListener("click", () => { togglePush().catch(() => {}); });

  $("more").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const menu = $("more-menu");
    menu.hidden = !menu.hidden;
    $("more").setAttribute("aria-expanded", menu.hidden ? "false" : "true");
  });

  $("export-chat").addEventListener("click", () => openExport());

  const tipEl = el("div", "tip");
  tipEl.setAttribute("role", "tooltip");
  tipEl.hidden = true;
  document.body.append(tipEl);
  let tipTimer = 0;
  let tipHost = null;

  function hideTip() {
    window.clearTimeout(tipTimer);
    tipHost = null;
    tipEl.hidden = true;
  }

  function placeTip(host) {
    const text = host.getAttribute("data-tip");
    if (!text || host.getAttribute("aria-expanded") === "true") { hideTip(); return; }
    tipEl.textContent = text;
    tipEl.hidden = false;
    const r = host.getBoundingClientRect();
    const tw = tipEl.offsetWidth;
    const th = tipEl.offsetHeight;
    let left = r.left + (r.width / 2) - (tw / 2);
    left = Math.max(8, Math.min(left, window.innerWidth - tw - 8));
    const right = host.dataset.tipPlacement === "right" && host.closest(".sidebar.collapsed");
    if (right) left = Math.max(8, Math.min(r.right + 8, window.innerWidth - tw - 8));
    let top = right ? Math.max(8, Math.min(r.top + (r.height - th) / 2, window.innerHeight - th - 8)) : r.bottom + 6;
    if (top + th > window.innerHeight - 8) top = Math.max(8, r.top - th - 6);
    tipEl.style.left = left + "px";
    tipEl.style.top = top + "px";
  }

  document.addEventListener("pointerover", (ev) => {
    const host = ev.target.closest && ev.target.closest("[data-tip]");
    if (!host || host === tipHost) return;
    window.clearTimeout(tipTimer);
    tipHost = host;
    tipTimer = window.setTimeout(() => { if (tipHost === host) placeTip(host); }, 420);
  });
  document.addEventListener("pointerout", (ev) => {
    if (!tipHost) return;
    const next = ev.relatedTarget && ev.relatedTarget.closest && ev.relatedTarget.closest("[data-tip]");
    if (next === tipHost) return;
    if (tipHost.contains(ev.relatedTarget)) return;
    hideTip();
  });
  document.addEventListener("focusin", (ev) => {
    const host = ev.target.closest && ev.target.closest("[data-tip]");
    if (!host) return;
    window.clearTimeout(tipTimer);
    tipHost = host;
    placeTip(host);
  });
  document.addEventListener("focusout", (ev) => {
    if (tipHost && ev.target.closest && ev.target.closest("[data-tip]") === tipHost) hideTip();
  });
  document.addEventListener("scroll", hideTip, true);
  document.addEventListener("click", hideTip, true);

  document.addEventListener("click", (ev) => {
    if (!ev.target.closest(".account-wrap")) closeAccount();
    if (!ev.target.closest(".connect-wrap")) {
      $("connect-pop").hidden = true;
      $("connect").setAttribute("aria-expanded", "false");
    }
    if (!ev.target.closest(".more-wrap")) {
      $("more-menu").hidden = true;
      $("more").setAttribute("aria-expanded", "false");
    }
    if (!ev.target.closest(".panel-more-wrap") && !ev.target.closest(".menu-sub")) closePanelMenu();
    if (!ev.target.closest(".folder-head") && !ev.target.closest(".folder-menu")) closeFolderMenus();
    if (!ev.target.closest(".tool-pop") && !ev.target.closest(".card")) closeToolPop();
    if (!ev.target.closest("#notes") && !ev.target.closest("#bell")) {
      notesEl.hidden = true;
      $("bell").setAttribute("aria-expanded", "false");
    }
  });

  function setComposerPlaceholder(text) {
    state.placeholder = text;
    if (!state.replyTo) input.placeholder = text;
  }

  function paintQuoteBar() {
    const bar = $("quote-bar");
    const on = !!(state.replyTo && state.sel && state.sel.type === "bot" && !state.peer);
    bar.hidden = !on;
    composer.classList.toggle("quoting", on);
    if (on) {
      $("quote-bar-text").textContent = quoteSnippet(state.replyTo.text);
      input.placeholder = "回复…";
    } else {
      input.placeholder = state.placeholder || "写一条消息";
    }
  }

  function armQuote(seq, text) {
    const n = Number(seq);
    if (!n || !state.sel || state.sel.type !== "bot" || state.peer) return;
    state.replyTo = { seq: n, text: String(text || "").trim() };
    paintQuoteBar();
    input.focus();
  }

  function clearQuote() {
    state.replyTo = null;
    paintQuoteBar();
  }

  $("quote-clear").addEventListener("click", () => {
    clearQuote();
    input.focus();
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    closeAccount();
    hideTip();
    if (!lightbox.hidden) { closeShots(); return; }
    if (!modal.hidden) { closeModal(); return; }
    if (stpState.open) { closeStp(); return; }
    if (state.replyTo) { clearQuote(); return; }
    closePops();
    if (panel.classList.contains("open") && window.innerWidth <= 1100) {
      rememberPanel(false);
      panel.classList.remove("open");
      $("info").setAttribute("aria-expanded", "false");
    }
    sidebar.classList.remove("open");
    syncBackdrop();
  });

  modal.addEventListener("click", (ev) => { if (ev.target === modal) closeModal(); });
  $("modal-cancel").addEventListener("click", closeModal);
  lightbox.addEventListener("click", (ev) => { if (ev.target === lightbox) closeShots(); });

  composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const text = input.value;
    if (!text.trim() || !state.sel) return;
    input.value = "";
    input.style.height = "auto";
    syncComposer();
    sendText(text);
  });

  input.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && !ev.shiftKey) {
      ev.preventDefault();
      composer.requestSubmit();
    }
  });

  input.addEventListener("input", () => {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 140) + "px";
    syncComposer();
  });

  window.addEventListener("resize", () => {
    if (window.innerWidth >= 768) sidebar.classList.remove("open");
    syncSidebar();
    syncPanelDefault();
    syncBackdrop();
  });

  async function boot() {
    if (!bgState.ready) { bgState.ready = true; bgSetup(); }
    stpSetup();
    await refreshLists();
    await refreshStatus().catch(() => {});
    await refreshNotes();
    await refreshNodePending().catch(() => {});
    await loadPush().catch(() => {});
    renderMe();
    let remembered = "";
    try { remembered = sessionStorage.getItem("xbot-selected-bot") || ""; } catch (e) {}
    const home = state.bots.find((b) => b.name === remembered) || state.bots.find((b) => b.primary) || state.bots[0];
    if (!state.sel && home) await selectBot(home.name);
    else if (!state.sel) {
      const inner = el("div", "thread-inner");
      inner.append(emptyState("选一个对话", "左边是你的 Bot。"));
      thread.replaceChildren(inner);
    }
    for (let i = 0; i < state.bots.length + state.groups.length; i++) await warmOne();
    syncPanelDefault();
  }

  function syncPanelDefault() {
    if (state.panelTouched) return;
    let preference = null;
    try { preference = localStorage.getItem("xbot-panel-open"); } catch (e) {}
    // The remembered choice only applies where the panel sits beside the
    // conversation; narrower windows always start with it closed.
    const wide = window.innerWidth > 1100 && (preference === "true" || (preference !== "false" && window.innerWidth >= 1440));
    const was = panel.classList.contains("open");
    panel.classList.toggle("open", wide);
    $("info").setAttribute("aria-expanded", wide ? "true" : "false");
    syncBackdrop();
    if (wide && !was) loadPanel().catch(() => {});
  }

  syncSidebar();
  initTheme();
  syncComposer();
  const hold = el("div", "thread-inner");
  hold.append(emptyState("XBot", "登录之后，对话在这里继续。"));
  thread.replaceChildren(hold);

  if ("serviceWorker" in navigator) {
    navigator.serviceWorker.register("/sw.js").catch(() => {});
  }

  function startPoll() {
    if (startPoll.started) return;
    startPoll.started = true;
    setInterval(() => {
      if (shell.hidden || state.streaming) return;
      if (!document.hidden) refreshStatus().catch(() => {});
      refreshNotes().catch(() => {});
      refreshNodePending().catch(() => {});
      refreshLists().catch(() => {});
      loadThread().catch(() => {});
    }, 3000);
  }

  api("/api/session").then(async (data) => {
    if (!data || !data.authed) { showLogin(); return; }
    state.owner = (data && data.owner) || "";
    renderMe();
    showApp();
    await boot();
    startPoll();
  }).catch(() => showLogin());
})();
