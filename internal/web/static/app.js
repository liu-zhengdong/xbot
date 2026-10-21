(function () {
  // The desktop app: MyGo sets --mygo-titlebar-height and window.mygo.platform.
  const inApp = !!getComputedStyle(document.documentElement).getPropertyValue("--mygo-titlebar-height").trim();
  const shell = document.getElementById("shell");
  if (inApp) {
    const root = document.documentElement;
    const platform = (window.mygo && window.mygo.platform) || "";
    root.classList.add("in-app");
    root.classList.add(platform === "darwin" ? "os-mac" : platform === "linux" ? "os-linux" : "os-win");
  }
  const $ = (id) => document.getElementById(id);
  const login = $("login");
  const sidebar = $("sidebar");
  const backdrop = $("backdrop");
  const thread = $("thread");
  const input = $("input");
  const titleEl = $("title");
  const subtitle = $("subtitle");
  const updateBtn = $("update");
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
    ownerName: "",
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
    // The context panel's own UI state: which request is shown, which band or
    // message is open, and the snapshot itself (fetched per bot, never cached
    // across a switch so a stale turn cannot be misread as the current one).
    ctxSnap: null,
    ctxStep: -1,
    ctxMenu: false,
    ctxOpen: { turn: true },
    ctxRaw: "",
    peer: null,
    replyTo: null,
    // Pictures pasted or dropped into the composer, waiting for send. Each
    // entry is {id, uploading, url}; id is empty until the upload lands.
    attach: [],
    // Bumped whenever the list is abandoned, so an upload still in flight can
    // tell that the list it was filling is gone.
    attachBatch: 0,
    placeholder: "写一条消息",
    modelCatalog: null,
    push: null,
    version: "",
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
    const accountTheme = $("account-theme");
    if (accountTheme) accountTheme.replaceChildren(icon(dark ? "sun" : "moon"), document.createTextNode("外观"), el("span", "menu-count", dark ? "深色" : "浅色"));
    try { localStorage.setItem("xbot-theme", dark ? "dark" : "light"); } catch (e) {}
    // The desktop window's native buttons follow XBot's own theme, not the OS's.
    if (inApp) {
      api("/api/app/theme", { method: "POST", body: { theme: dark ? "dark" : "light" } }).catch(() => {});
    }
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

  function zhError(msg) {
    msg = String(msg || "").trim();
    const timeout = msg.match(/request timeout after (\d+)m/i);
    if (timeout) return "模型超时（" + timeout[1] + " 分钟）";
    for (const [pattern, text] of [
      [/text is empty/i, "内容是空的"], [/unanswered tool calls/i, "有一张确认卡片还没处理"],
      [/cancelled|canceled/i, "已取消"], [/interrupted/i, "被中断"],
      [/context deadline exceeded|timeout/i, "超时"], [/connection refused|no such host/i, "连不上"],
    ]) if (pattern.test(msg)) return text;
    return /[㐀-鿿]/.test(msg) ? msg : "出错了";
  }

  function taskStatus(status) {
    return ({ done: "完成", failed: "失败", cancelled: "已取消", canceled: "已取消",
      interrupted: "已中断", denied: "已拒绝", declined: "已拒绝",
      running: "进行中", queued: "已排队" })[status] || zhError(status);
  }

  function errorDetails(host, raw, shown) {
    if (!raw || raw === shown) return;
    const toggle = el("button", "text-btn", "详情");
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", "false");
    const detail = el("pre", "error-raw", raw);
    detail.hidden = true;
    toggle.addEventListener("click", () => {
      detail.hidden = !detail.hidden;
      toggle.setAttribute("aria-expanded", String(!detail.hidden));
    });
    host.append(toggle, detail);
  }

  function visibleError(e, fallback) {
    if (e instanceof TypeError) return "连不上 XBot";
    const raw = String((e && e.message) || "");
    const msg = zhError(raw);
    if (e && e.status >= 400 && e.status < 500 && e.status !== 401 && raw && raw !== "request failed") return "没能完成：" + msg;
    return raw ? msg : fallback || "操作失败，稍后再试";
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
  const bgState = { data: null, open: false, steerOpen: {}, steerDraft: {}, awaitingIDs: new Set(), timer: 0 };
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
    if (bgState.name !== name) { bgState.steerOpen = {}; bgState.steerDraft = {}; bgState.awaitingIDs = new Set(); bgState.force = true; }
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
    tx.append(el("div", "bg-tt", plainText(title)), el("div", "bg-sub", sub));
    const a = el("span", "bg-acts");
    (acts || []).forEach((b) => a.append(b));
    rt.append(st, tx, a);
    li.append(rt);
    return li;
  }
  function bgBtn(icon, label, fn) {
    const b = el("button", "bg-ib");
    b.type = "button";
    setTip(b, label);
    b.setAttribute("aria-label", label);
    b.append(bgIcon(BG_ICONS[icon], 15));
    b.addEventListener("click", (ev) => { ev.stopPropagation(); fn(); });
    return b;
  }
  // Match the pending card's branch; another task of this Bot may still run.
  function taskAwaiting(task, name) {
    if (task.status === "awaiting") return true;
    if (!state.sel || state.sel.type !== "bot" || state.sel.id !== name) return false;
    const latest = new Map();
    for (const m of state.messages || []) {
      const card = approvalCardOf(m.content);
      if (card) latest.set(card.id, { card, branch: m.branch });
    }
    return Array.from(latest.values()).some(({ card, branch }) =>
      card.state === "pending" && branch === task.id);
  }

  function approvalTaskButton(name, id) {
    const button = el("button", "secondary task-awaiting", "等你确认");
    button.type = "button";
    button.addEventListener("click", () => { bgClose(); jumpToApproval(name, id); });
    return button;
  }

  function confirmTaskStop(button, action) {
    let until = 0;
    let timer = 0;
    const original = Array.from(button.childNodes, (node) => node.cloneNode(true));
    button.addEventListener("click", (ev) => {
      ev.stopPropagation();
      if (Date.now() < until) {
        clearTimeout(timer);
        until = 0;
        button.replaceChildren(...original);
        delete button.dataset.stopUntil;
        action();
        return;
      }
      until = Date.now() + 3000;
      button.dataset.stopUntil = String(until);
      button.textContent = "再点一次停止";
      timer = setTimeout(() => { until = 0; delete button.dataset.stopUntil; button.replaceChildren(...original); }, 3000);
    });
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
    const awaitingIDs = new Set(running.filter((r) => taskAwaiting(r, name)).map((r) => r.id));
    const awaitingChanged = awaitingIDs.size !== bgState.awaitingIDs.size ||
      Array.from(awaitingIDs).some((id) => !bgState.awaitingIDs.has(id));
    const waiting = awaitingIDs.size;
    badge.querySelector(".bg-n").textContent = "后台 " + running.length + (waiting ? " · 等你确认" : "");
    // An open popover stays open after the last task ends, so its result can be read.
    const pop = document.getElementById("bg-pop");
    if (!bgState.open) return;
    // Do not rebuild under the owner's fingers: a poll would wipe a half-typed steer.
    if (!awaitingChanged && Array.from(pop.querySelectorAll("[data-stop-until]")).some((b) => Number(b.dataset.stopUntil) > Date.now())) return;
    const typing = pop.querySelector(".bg-addin input");
    if (typing && (typing === document.activeElement || typing.value) && !bgState.force && !awaitingChanged) return;
    pop.querySelectorAll(".bg-addin input").forEach((inp) => {
      if (inp.dataset.bot === name) bgState.steerDraft[inp.dataset.task] = inp.value;
    });
    bgState.awaitingIDs = awaitingIDs;
    bgState.force = false;
    const taskOf = {};
    running.forEach((r) => { taskOf[r.id] = r.task; });
    const head = el("div", "bg-ph");
    head.append(el("b", "", "后台任务"), el("span", "bg-cnt", running.length + " 运行 · " + queue.length + " 排队"));
    head.append(bgBtn("err", "关闭", bgClose));
    const list = el("ul", "bg-pl");
    running.forEach((r) => {
      const awaiting = awaitingIDs.has(r.id);
      const stop = bgBtn("stop", "停止", () => {});
      confirmTaskStop(stop, () => bgAct(base + "/agents/" + encodeURIComponent(r.id) + "/cancel", "POST"));
      const acts = [stop];
      if (!awaiting) acts.unshift(bgBtn("steer", "追加指示", () => { bgState.steerOpen[r.id] = !bgState.steerOpen[r.id]; bgState.focusSteer = r.id; bgState.force = true; bgRender(); }));
      const li = bgRow("run", awaiting ? "pause" : "spin", plainText(displayText(r.task || r.id)) || "后台任务",
        awaiting ? "" : "第 " + (r.steps || 0) + " 步" + (r.last_tool ? " · " + r.last_tool : ""), acts);
      if (awaiting) li.querySelector(".bg-sub").replaceWith(approvalTaskButton(name, r.id));
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
      if (!awaiting && bgState.steerOpen[r.id]) {
        const f = el("form", "bg-addin");
        const inp = el("input");
        inp.placeholder = "追加指示，下一步生效…";
        inp.dataset.task = r.id;
        inp.dataset.bot = name;
        inp.value = bgState.steerDraft[r.id] || "";
        inp.addEventListener("input", () => { bgState.steerDraft[r.id] = inp.value; });
        const go = el("button", "bg-send");
        go.type = "submit";
        go.disabled = !inp.value.trim();
        inp.addEventListener("input", () => { go.disabled = !inp.value.trim(); });
        go.append(bgIcon(BG_ICONS.up, 14));
        f.append(inp, go);
        f.addEventListener("submit", (ev) => {
          ev.preventDefault();
          const t = inp.value.trim();
          if (!t) return;
          // A sent instruction is no longer a draft: clear it so polls resume.
          delete bgState.steerDraft[r.id];
          inp.value = "";
          go.disabled = true;
          bgAct(base + "/agents/" + encodeURIComponent(r.id) + "/steer", "POST", { text: t });
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
        }[f.status] || ["int", "pause", taskStatus(f.status)];
        done.append(bgRow(map[0], map[1], plainText(displayText(f.task || f.id)) || "后台任务", map[2]));
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

  // The second line is owned by the status poll; rows only record a signature so an
  // unchanged line is never rewritten and no row is touched without a change.
  function stDesc(name) {
    const st = stStatus.map[name] || null;
    const kind = st ? String(st.state || "idle") : "idle";
    let sig = kind;
    const line = el("span", "preview");
    if (kind === "running") {
      line.className = "preview st run click";
      line.append(spinIcon(11));
      const text = el("span");
      text.append("在跑 ");
      const tool = st && st.tool ? String(st.tool) : "";
      if (tool) text.append(el("code", "", tool));
      line.append(text);
      line.addEventListener("click", (ev) => { ev.stopPropagation(); toggleStp(name, line); });
      sig += tool;
    } else if (kind === "awaiting") {
      line.className = "preview st wait click";
      line.append(el("i", "wd"));
      line.append(el("span", "", "等你确认"));
      line.addEventListener("click", (ev) => { ev.stopPropagation(); jumpToApproval(name); });
    } else if (kind === "offline") {
      line.className = "preview st off";
      const mins = Math.round(Number(st && st.stale_seconds) / 60) || 1;
      line.append(el("span", "", "离线 · 最后状态 " + mins + " 分钟前"));
      sig += mins;
    } else {
      const bot = (state.bots || []).find((b) => b.name === name);
      const text = realPreview(st && st.last_message) || (bot ? lineForBot(bot) : "") || "没有消息";
      line.className = "preview";
      line.append(el("span", "", text));
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
    paintRailStatus();
    sortStatusRows();
  }

  function paintRailStatus() {
    document.querySelectorAll("#roster .item[data-bot], #pin-row .pin[data-bot]").forEach((row) => {
      const name = row.dataset.bot;
      const kind = (stStatus.map[name] || {}).state || "idle";
      if (row.dataset.railState === kind) return;
      row.dataset.railState = kind;
      const av = row.querySelector(".avatar");
      if (!av) return;
      const old = row.querySelector(".rail-status");
      if (old) old.remove();
      const busy = kind === "running" || kind === "awaiting";
      setTip(av, niceName(name), "right");
      if (row.classList.contains("pin")) setTip(row, niceName(name), "right");
      if (busy) {
        const dot = el("span", "rail-status " + kind);
        dot.setAttribute("aria-hidden", "true");
        dot.append(kind === "running" ? spinIcon(10) : icon("awaiting"));
        av.append(dot);
      }
    });
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
    paintStatus();
    if (stpState.open) stpRefresh().catch(() => {});
  }

  async function jumpToApproval(name, branch) {
    closeStp();
    try {
      if (state.sel && state.sel.type === "bot" && state.sel.id === name) await loadThread();
      else await selectBot(name);
    } catch (e) {}
    const find = () => Array.from(document.querySelectorAll("#thread .approval"))
      .find((card) => !branch || card.dataset.branch === branch);
    let card = find();
    if (!card) {
      await new Promise((r) => setTimeout(r, 50));
      card = find();
    }
    if (card) {
      card.scrollIntoView({ block: "center", behavior: "instant" });
      const primary = card.querySelector("button.primary:not(:disabled)");
      if (primary) primary.focus({ preventScroll: true });
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
      const rest = cut > 0 ? sum.slice(cut + 3) : "";
      if (sum) sl.append(el("code", "", cut > 0 ? sum.slice(0, cut) : sum));
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
    $("account-version").textContent = shortVersion(state.version);
  }

  // A newer version, downloaded in the background: one click installs it and
  // starts XBot again. The button is the whole interface on purpose — while it
  // downloads nothing shows, and a failure only appears when someone asks.
  const updateState = { state: "", latest: "" };

  function paintUpdate(st) {
    updateState.state = (st && st.state) || "";
    updateState.latest = (st && st.latest) || "";
    const busy = updateBtn.classList.contains("busy");
    if (busy) return;
    // "ready" is the only state worth interrupting for. A platform that
    // installs with its own installer offers the file instead.
    const offer = updateState.state === "ready" || updateState.state === "manual";
    updateBtn.hidden = !offer;
    if (!offer) return;
    const label = updateBtn.querySelector(".update-label");
    const ready = updateState.state === "ready";
    label.textContent = ready ? "更新到 " + updateState.latest.replace(/^v/, "") : "下载新版";
    const tip = ready
      ? "新版本已下载，点一下重启并更新"
      : "这台电脑用安装包更新，点一下下载";
    updateBtn.dataset.tip = tip;
    updateBtn.setAttribute("aria-label", tip);
  }

  async function refreshUpdate() {
    const st = await api("/api/update").catch(() => null);
    paintUpdate(st);
  }

  updateBtn.addEventListener("click", async () => {
    if (!updateState.latest) return;
    if (updateState.state === "manual") {
      // Windows: hand the installer to the browser, which downloads it.
      const st = await api("/api/update").catch(() => null);
      if (st && st.download) window.open(st.download, "_blank", "noopener");
      return;
    }
    const label = updateBtn.querySelector(".update-label");
    updateBtn.classList.add("busy");
    updateBtn.disabled = true;
    label.textContent = "正在更新";
    try {
      await api("/api/update/install", { method: "POST" });
      // The process restarts and the page reconnects to the new version.
      label.textContent = "正在重启";
      waitForRestart(updateState.latest);
    } catch (e) {
      updateBtn.classList.remove("busy");
      updateBtn.disabled = false;
      label.textContent = "更新失败";
      updateBtn.dataset.tip = (e && e.message) || "更新失败，稍后再试";
      setTimeout(() => { updateBtn.classList.remove("busy"); refreshUpdate().catch(() => {}); }, 4000);
    }
  });

  // The account menu and restart check use the API's version display format.
  function shortVersion(v) {
    return String(v || "").trim().replace(/^v/, "").split("+")[0];
  }

  // waitForRestart reloads once the server reports the version we installed.
  // The old process keeps answering while it drains in-flight turns, so a 200
  // alone would reload the old page and give up waiting.
  function waitForRestart(want) {
    want = shortVersion(want);
    const started = Date.now();
    const tick = async () => {
      try {
        const res = await fetch("/api/session", { credentials: "same-origin" });
        if (res.ok) {
          const data = await res.json().catch(() => null);
          const now = (data && data.version) || "";
          if (now && (!want || now === want)) { location.reload(); return; }
        }
      } catch (e) {}
      // serve drains in-flight turns for up to two minutes before it
      // re-execs, and answers as the old version throughout. Wait past that
      // rather than reload a page that is still the old one: the new process
      // swaps in underneath it.
      if (Date.now() - started > 150000) { location.reload(); return; }
      setTimeout(tick, 700);
    };
    setTimeout(tick, 1200);
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
    const hm = clockTime(t);
    const now = new Date();
    const start = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const days = Math.round((start(now) - start(d)) / 86400000);
    if (days === 0) return hm;
    if (days === 1) return "昨天 " + hm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日";
  }

  function clockTime(t) {
    const d = new Date(t);
    return !t || Number.isNaN(d.getTime()) ? "" : String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
  }

  // One sender writing again within five minutes (including the boundary)
  // continues the group above it. Pure, so the same rule decides the name/time
  // header and the spacing for history, new pages and live appends.
  const GROUP_GAP_MS = 5 * 60 * 1000;

  function groupStart(prev, cur, prevKey, key) {
    if (!prev || prevKey !== key) return true;
    const a = Date.parse(prev.time || prev.ts || prev.at || "");
    const b = Date.parse(cur.time || cur.ts || cur.at || "");
    return !Number.isFinite(a) || !Number.isFinite(b) || b < a || b - a > GROUP_GAP_MS;
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
    gauge: '<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>',
    spinner: '<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>',
    "users-plus": '<circle cx="9" cy="7" r="3"/><path d="M3 21v-2a6 6 0 0 1 12 0v2M16 4a3 3 0 0 1 0 6M19 13v6M16 16h6"/>',
    person: '<circle cx="12" cy="8" r="4"/><path d="M4 22v-2a8 8 0 0 1 16 0v2"/>',
    awaiting: '<path d="M12 5v9"/><circle cx="12" cy="19" r="1" fill="currentColor" stroke="none"/>',
    logout: '<path d="M9 4H4v16h5"/><path d="M9 12h12m-5-5 5 5-5 5"/>',
    moon: THEME_MOON,
    sun: THEME_SUN,
    sidebar: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M9 4v16"/>',
    terminal: '<polyline points="4 17 10 11 4 5"/><path d="M12 19h8"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    clipboard: '<rect x="8" y="2" width="8" height="4" rx="1"/><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/>',
    "folder-open": '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/><path d="m6 14 1.5-3h13l-1.5 3"/>',
    chevron: '<path d="m9 18 6-6-6-6"/>',
    link: '<path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/>',
    chevronLeft: '<path d="m15 18-6-6 6-6"/>',
    chevronDown: '<path d="m6 9 6 6 6-6"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    ellipsis: '<circle class="solid" cx="5" cy="12" r="1.5"/><circle class="solid" cx="12" cy="12" r="1.5"/><circle class="solid" cx="19" cy="12" r="1.5"/>',
    wrench: '<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
    pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>',
    trash: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/>',
    maximize: '<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3 13.5 10.5"/><path d="M3 21l7.5-7.5"/>',
    download: '<path d="M12 4v12"/><path d="m7 11 5 5 5-5"/><path d="M5 15v4a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4"/>',
    folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    reply: '<polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/>',
    bubble: '<path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    "alarm-clock": '<circle cx="12" cy="13" r="8"/><path d="M12 9v4l2 2"/><path d="M5 3 2 6"/><path d="m22 6-3-3"/>',
    device: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><circle class="solid" cx="12" cy="8" r="1"/>',
    swap: '<path d="m16 3 4 4-4 4"/><path d="M20 7H4"/><path d="m8 21-4-4 4-4"/><path d="M4 17h16"/>',
    arrowRight: '<path d="M5 12h14"/><path d="m13 6 6 6-6 6"/>',
    imageUp: '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><path d="m7 9 5-5 5 5"/><path d="M12 4v12"/>',
    "refresh-cw": '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/>',
    "book-open": '<path d="M12 7v14"/><path d="M3 18a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h3a6 6 0 0 1 6 4 6 6 0 0 1 6-4h3a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1h-4a5 5 0 0 0-5 3 5 5 0 0 0-5-3z"/>',
    "message-circle-question": '<path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z"/><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>',
    "list-checks": '<path d="m3 17 2 2 4-4M3 7l2 2 4-4M13 6h8M13 12h8M13 18h8"/>',
    "file-text": '<path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7Z"/><path d="M14 2v6h6M8 13h8M8 17h6"/>',
    chart: '<path d="M3 3v18h18M8 16v-4M13 16V8M18 16V5"/>',
    refresh: '<path d="M21 12a9 9 0 1 1-2.64-6.36"/><path d="M21 3v6h-6"/>',
    undo: '<path d="M3 7v6h6"/><path d="M21 17a9 9 0 0 0-9-9 9 9 0 0 0-6 2.3L3 13"/>',
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

  // A bot's fallback mark is a geometric tile derived from its name: four of
  // seven shapes, each in one of four turns. Only numbers go into the markup.
  function fnv(s) {
    let h = 0x811c9dc5;
    for (const ch of new TextEncoder().encode(String(s))) { h ^= ch; h = Math.imul(h, 0x01000193) >>> 0; }
    return h >>> 0;
  }

  function glyph(name) {
    let h = fnv(name);
    const take = (n) => { const v = h % n; h = Math.floor(h / n) || fnv(name + h); return v; };
    const S = 40;
    const shapes = [
      (x, y) => `<path d="M${x} ${y}h${S}v${S}h-${S}z"/>`,
      (x, y) => `<path d="M${x} ${y}h${S}A${S} ${S} 0 0 1 ${x} ${y + S}z"/>`,
      (x, y) => `<path d="M${x} ${y + S}h${S}A${S / 2} ${S / 2} 0 0 0 ${x} ${y + S}z"/>`,
      (x, y) => `<circle cx="${x + S / 2}" cy="${y + S / 2}" r="${S / 2 - 3}"/>`,
      (x, y) => `<path d="M${x} ${y}h${S}L${x} ${y + S}z"/>`,
      (x, y) => `<circle cx="${x + S / 2}" cy="${y + S / 2}" r="${S / 5}"/>`,
      (x, y) => `<path d="M${x} ${y + S / 2}h${S}v${S / 2}h-${S}z"/>`,
      () => "",
    ];
    let g = "";
    for (let i = 0; i < 4; i++) {
      const x = 20 + (i % 2) * S, y = 20 + Math.floor(i / 2) * S;
      const k = take(8), r = take(4) * 90;
      g += `<g transform="rotate(${r} ${x + S / 2} ${y + S / 2})">${shapes[k](x, y)}</g>`;
    }
    return `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" viewBox="0 0 120 120" data-kind="glyph"><rect width="120" height="120" style="fill:var(--av-bg,#dcdce0)"/><g style="fill:var(--av-ink,#1a1a1a)">${g}</g></svg>`;
  }

  // The glyph markup is numbers only, so parsing it is safe. Parsing keeps
  // the <svg> in the SVG namespace; two on one page are told apart by index.
  let glyphCount = 0;
  function glyphSVG(name) {
    const doc = new DOMParser().parseFromString(glyph(name), "image/svg+xml");
    if (!doc.documentElement || doc.documentElement.nodeName !== "svg") return null;
    const svg = document.importNode(doc.documentElement, true);
    svg.setAttribute("id", "xbot-glyph-" + ++glyphCount);
    svg.setAttribute("aria-hidden", "true");
    return svg;
  }

  // Portraits are drawn here and handed over as a data URL: the server never
  // stores generated SVG, and a src never lets the markup run as script.
  const PORTRAITS = new Map();
  function portraitURL(seed, small) {
    const key = (small ? "f:" : "p:") + seed;
    if (PORTRAITS.has(key)) return PORTRAITS.get(key);
    let url = "";
    try {
      const lib = window.XBotAvatar;
      if (lib) url = "data:image/svg+xml;charset=utf-8," + encodeURIComponent((small ? lib.face(seed) : lib.portrait(seed)) || "");
    } catch (e) { url = ""; }
    if (PORTRAITS.size >= 256) PORTRAITS.delete(PORTRAITS.keys().next().value);
    PORTRAITS.set(key, url);
    return url;
  }

  // The fallback for a broken upload, a "glyph" seed or a missing bundle.
  function drawGlyph(av, name) {
    const svg = glyphSVG(name);
    if (!svg) return;
    av.replaceChildren(svg);
    av.classList.add("glyph");
  }

  function avatar(name, paint, size) {
    paint = paint || {};
    const av = el("span", "avatar" + (size ? " " + size : ""));
    if (name) setTip(av, name, "right");
    const seed = String(paint.seed || name || "");
    if (size === "me" && !paint.url && !portraitURL(seed, false)) {
      // 我 is a line-art portrait from the owner's name like the bots; the soft
      // blob and its initial are only the fallback when the bundle didn't load.
      av.classList.add("blob");
      av.append(faceFor(name, paint, size));
      return av;
    }
    if (paint.url) {
      const img = document.createElement("img");
      img.alt = "";
      img.src = paint.url;
      img.addEventListener("error", () => drawGlyph(av, name || seed));
      av.append(img);
    } else if (seed === "glyph") {
      drawGlyph(av, name || seed);
    } else {
      const src = portraitURL(seed, size === "xs");
      if (!src) { drawGlyph(av, name || seed); return av; }
      const img = document.createElement("img");
      img.alt = "";
      img.decoding = "async";
      img.draggable = false;
      img.src = src;
      av.append(img);
    }
    return av;
  }

  function paintOfBot(b) {
    if (!b) return null;
    return {
      bg: b.avatar_bg, ink: b.avatar_ink, url: b.avatar_url, initial: b.initial,
      avatar: b.avatar, seed: b.avatar_seed || b.name, remote: !!b.remote,
    };
  }

  // Device views share the same grouping rule and face renderer.
  window.XBotMessages = { groupStart, clockTime, timeDivider };
  window.XBotFaces = {
    html: (name, seed, size) => avatar(name, { seed: seed || name }, size || "").outerHTML,
  };

  function paintOf(name) {
    return paintOfBot((state.bots || []).find((x) => x.name === name));
  }

  // A bot on another computer is drawn from that device's bot_avatars seed. With
  // no seed (old peer, devices not loaded yet, unknown device) or an upload it is
  // the geometric glyph, never a local bot's face; "xbot:devices" repaints it.
  function paintOfPeer(who) {
    if (!who.device) return paintOf(who.bot);
    const data = (window.xbotDevices && window.xbotDevices.S && window.xbotDevices.S.data) || {};
    const d = (data.devices || []).find((x) => x.label === who.device || x.name === who.device || x.id === who.device);
    const a = (d && d.bot_avatars && d.bot_avatars[who.bot]) || {};
    return { seed: !a.custom && a.avatar_seed ? a.avatar_seed : "glyph" };
  }

  function peerAvatar(who, size) {
    const av = avatar(who.bot, paintOfPeer(who), size);
    if (who.device) { av.dataset.peerBot = who.bot; av.dataset.peerDevice = who.device; }
    return av;
  }

  window.addEventListener("xbot:devices", () => {
    for (const old of document.querySelectorAll(".avatar[data-peer-device]")) {
      const size = ["xs", "lg"].find((c) => old.classList.contains(c)) || "";
      old.replaceWith(peerAvatar({ bot: old.dataset.peerBot, device: old.dataset.peerDevice }, size));
    }
  });

  async function copyText(text, btn) {
    let copied = false;
    try {
      if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
      await navigator.clipboard.writeText(text);
      copied = true;
    } catch (e) {
      const ta = el("textarea");
      ta.value = text;
      ta.style.position = "fixed";
      ta.style.left = "-9999px";
      document.body.append(ta);
      ta.select();
      try { copied = document.execCommand("copy"); } catch (e) {}
      ta.remove();
    }
    if (copied && btn) {
      btn.setAttribute("aria-label", "已复制");
      setTimeout(() => btn.setAttribute("aria-label", "复制"), 1200);
    }
    return copied;
  }

  function unwrap(content) {
    const text = content || "";
    const m = text.match(/^<untrusted_event\s+([^>]*)>\n([\s\S]*)\n<\/untrusted_event>$/);
    if (!m) return { text: text, untrusted: false, source: "", sender: "" };
    const source = /source="([^"]*)"/.exec(m[1]);
    const sender = /sender="([^"]*)"/.exec(m[1]);
    const type = /type="([^"]*)"/.exec(m[1]);
    const attrs = Object.fromEntries(Array.from(m[1].matchAll(/([\w-]+)="([^"]*)"/g), a => [a[1], a[2]]));
    return {
      attributes: attrs,
      type: type ? type[1] : "",
      text: m[2],
      untrusted: true,
      source: source ? source[1] : "",
      sender: sender ? sender[1] : "",
    };
  }

  // Display only: transcript content and routing identifiers stay intact.
  function displayText(text) {
    return String(text || "")
      .replace(/已转到后台\s*bg_[0-9a-f]{4,}\b\s*继续做/g, "已转到后台继续做")
      .replace(/\bbg_[0-9a-f]{4,}\b/g, "")
      .replace(/\bsa_[0-9a-f]{4,}\b/g, "")
      .replace(/后台命令\s*sh[0-9a-f]{6,}\s*/g, "后台命令").replace(/\bsh[0-9a-f]{6,}\b/g, "").trim();
  }

  function eventLine(parsed, key) {
    const attrs = parsed.attributes || {};
    const first = clip(plainText(displayText(parsed.text.split(/\r?\n/)[0]))
      .replace(/（(done|failed|cancelled|interrupted)）/g, (_, w) => "（" + taskStatus(w) + "）"), 80);
    const task = plainText(displayText(attrs.task_title || attrs.task || attrs.title || attrs.name));
    const failed = /fail|error/i.test(parsed.type + " " + (attrs.status || ""));
    const timeout = failed && /request timeout after (\d+)m/i.exec(parsed.text);
    const reason = timeout ? "模型超时（" + timeout[1] + " 分钟）" : "出错了";
    const label = parsed.source === "subagent"
      ? (failed ? "子任务失败：" + reason : (task ? "子任务「" + task + "」：" : "子任务：") + first)
      : ({ shell: "后台命令", webhook: "Webhook", watch: "监视" }[parsed.source] || "来自 " + (displayText(parsed.source) || "外部事件")) + "：" + first;
    const row = el("div", "routine-line event-line");
    const head = el("button", "secondary sysline event-toggle");
    head.type = "button";
    head.append(icon("bubble"), el("span", "", label));
    const drawer = el("div", "routine-drawer event-raw", parsed.text);
    drawer.hidden = !openRoutines.has(key);
    head.setAttribute("aria-expanded", String(!drawer.hidden));
    head.addEventListener("click", () => {
      drawer.hidden = !drawer.hidden;
      head.setAttribute("aria-expanded", String(!drawer.hidden));
      if (drawer.hidden) openRoutines.delete(key);
      else openRoutines.add(key);
    });
    row.append(head, drawer);
    return row;
  }

  function backgroundRelays(messages) {
    const normalize = text => String(text || "").replace(/\s+/g, " ").trim();
    const finalByBranch = new Map();
    messages.forEach((m, i) => {
      if (m.role === "assistant" && m.branch) finalByBranch.set(m.branch, i);
    });
    const suppressed = new Set();
    const pending = new Set();
    messages.forEach((m, i) => {
      const parsed = unwrap(m.content || "");
      if (m.role === "user" && parsed.untrusted && parsed.source === "subagent" && parsed.type === "fork_end") {
        if (normalize(parsed.text)) pending.add(normalize(parsed.text));
      }
      if (m.role !== "assistant") return;
      if (m.branch) {
        if (finalByBranch.get(m.branch) === i && !(m.tool_calls || []).length && normalize(m.content)) {
          pending.add(normalize(m.content));
        }
        return;
      }
      if (!(m.tool_calls || []).length && pending.has(normalize(m.content))) suppressed.add(i);
      // Even a different or tool-calling first reply consumes the opportunity.
      pending.clear();
    });
    return suppressed;
  }

  function missedTimes(text, now = new Date()) {
    const yesterday = new Date(now);
    yesterday.setDate(yesterday.getDate() - 1);
    let previousDay = "";
    return text.split(/,\s*/).map(value => {
      const match = /^(\d{4})-(\d{2})-(\d{2}) (\d{2}:\d{2})$/.exec(value);
      if (!match) return value;
      const day = match.slice(1, 4).join("-");
      const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
      const label = date.toDateString() === now.toDateString() ? "今天" : date.toDateString() === yesterday.toDateString() ? "昨天" : match[2] + "-" + match[3];
      const result = (day === previousDay ? "" : label + " ") + match[4];
      previousDay = day;
      return result;
    }).join("、");
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
      const lines = text.split("\n").filter(line => /^- /.test(line)).map(line => {
        const m = /^- "[^"]*" "(.*)": (\d+) missed \((.*)\)$/.exec(line);
        if (m) return m[1] + " · 错过 " + m[2] + " 次（" + missedTimes(m[3]) + "）";
        const more = /^- … and (\d+) more routines/.exec(line);
        return more ? "另有 " + more[1] + " 项定时" : "其他定时";
      });
      return { label: "错过的定时 · " + count + " 项", prompt: "电脑关机或休眠期间，这些定时没有运行。\n" + lines.join("\n") };
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
    head.append(icon("alarm-clock"), el("span", "", routine.time ? "定时 · " + routine.label + " " + routine.time : routine.label));
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

  // pastedURLs turns stored image ids into the URLs that serve them.
  function pastedURLs(sel, ids) {
    if (!sel || sel.type !== "bot" || !ids || !ids.length) return [];
    return ids.map((id) => "/api/bots/" + encodeURIComponent(sel.id) + "/paste/" + encodeURIComponent(id));
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

  // Empty chat (window review P2 #7): the bot's face, its name, one line and
  // up to three starters made from its routines and persona. A starter only
  // fills the composer; sending still takes Enter or the send button.
  const EMPTY_LINE = "有什么事，直接说就行。";
  const GENERIC_STARTERS = [
    {text: "你能帮我做些什么？", icon: "message-circle-question"},
    {text: "帮我整理今天要做的事", icon: "list-checks"},
    {text: "把这段话总结成三点", icon: "book-open"},
  ];

  function starterPrompts(profile) {
    const out = [];
    const push = (text, icon) => {
      text = String(text || "").replace(/\s+/g, " ").trim();
      if (text && text.length <= 30 && !out.some(item => item.text === text) && out.length < 3) out.push({text, icon});
    };
    for (const rt of (profile && profile.routines) || []) {
      if (rt && rt.enabled !== false && rt.name) push("现在跑一次「" + rt.name + "」", "alarm-clock");
    }
    // What the persona or description says the bot does, asked as a request:
    // 「负责整理日记」 becomes 「帮我整理日记」.
    const said = [profile && profile.persona, profile && profile.description].join("\n");
    const re = /(?:擅长|负责|专门|帮(?:主人|你|我|用户)?)([^，。,.；;：:！!？?\n「」]{2,14})/g;
    for (let m; (m = re.exec(said)) && out.length < 3;) push("帮我" + m[1].trim(), /读|阅读|文章/.test(m[1]) ? "book-open" : "list-checks");
    for (const item of GENERIC_STARTERS) push(item.text, item.icon);
    return out;
  }

  function fillComposer(text) {
    input.value = text;
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 140) + "px";
    syncComposer();
    input.focus();
    input.setSelectionRange(text.length, text.length);
  }

  // The profile behind the starters: the panel's when it is this bot's, else
  // fetched once per bot and kept for the page's life.
  // A Map, so names like "constructor" are not mistaken for cached entries.
  // An entry still in flight is shared with loadPanel, which would otherwise
  // fetch the same profile a second time when the panel is open.
  const starterProfiles = new Map();
  function fetchBotProfile(name) {
    const live = starterProfiles.get(name);
    if (live && !live.done) return live.request;
    const entry = { done: false };
    entry.request = api("/api/bots/" + encodeURIComponent(name)).finally(() => { entry.done = true; });
    starterProfiles.set(name, entry);
    return entry.request;
  }
  async function starterProfile(name) {
    if (state.panelData && state.panelData.name === name) return state.panelData;
    const cached = starterProfiles.get(name);
    return (cached ? cached.request : fetchBotProfile(name)).catch(() => ({}));
  }

  function starterList(who) {
    const box = el("div", "starters");
    starterProfile(who).then((profile) => {
      if (!box.isConnected) return;
      for (const {text, icon: glyph} of starterPrompts(profile)) {
        const btn = el("button", "starter");
        btn.type = "button"; // never a submit: it fills the composer only
        btn.append(icon(glyph), el("span", "", text), icon("chevron"));
        btn.addEventListener("click", () => fillComposer(text));
        box.append(btn);
      }
    });
    return box;
  }

  function doodle(name) {
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    const wide = name === "devices" || name === "offline";
    const height = name === "devices" ? 110 : 120;
    for (const [k, v] of Object.entries({class: "doodle", width: wide ? 220 : 200, height, viewBox: "0 0 " + (wide ? 220 : 200) + " " + height, fill: "none", stroke: "currentColor", "stroke-width": 1.5, "stroke-linecap": "round", "stroke-linejoin": "round", "aria-hidden": "true"})) svg.setAttribute(k, v);
    const use = document.createElementNS(svg.namespaceURI, "use");
    use.setAttribute("href", "#doodle-" + name);
    svg.append(use);
    return svg;
  }

  function panelEmpty(name, heading, text) {
    const box = el("div", "panel-empty");
    if (name) box.append(doodle(name));
    box.append(el("h3", "", heading), el("p", "", text));
    return box;
  }

  function emptyState(heading, line, who, starters) {
    const box = el("div", "empty");
    if (starters) box.append(doodle("chat"));
    if (who) box.append(avatar(who, paintOf(who), "lg"));
    box.append(el("h2", "", heading), el("p", "", line));
    if (starters && who) box.append(starterList(who));
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
      // The other bot's name, never the text: the exchange view has it.
      if (to) return { text: "", peer: to, sent: true };
      return { text: from + " 发给了另一个 Bot", peer: "" };
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

  const messageContents = new WeakMap();
  let messageMenu = null;
  let messageTarget = null;
  let messagePress = null;
  let copyReceipt = null;

  function messageMenuPosition(x, y, width, height, viewportWidth, viewportHeight) {
    return {
      left: Math.max(8, Math.min(x + width > viewportWidth - 8 ? x - width : x, viewportWidth - width - 8)),
      top: Math.max(8, Math.min(y + height > viewportHeight - 8 ? y - height : y, viewportHeight - height - 8)),
    };
  }

  function mobileMessageMenuPosition(rect, width, height, viewportWidth, viewportHeight, right, headerBottom) {
    const left = Math.max(8, Math.min(right ? rect.right - width : rect.left, viewportWidth - width - 8));
    const above = rect.top - height - 8;
    return {left, top: above >= headerBottom + 8 ? above : rect.bottom + 8};
  }

  function closeMessageMenu() {
    messageMenu?.remove();
    messageTarget?.classList.remove("message-target");
    messageTarget = null;
    document.documentElement.classList.remove("message-menu-open");
    messageMenu = null;
    if (messagePress) clearTimeout(messagePress.timer);
    messagePress = null;
  }

  function bindMessageMenu(row, m, text) {
    messageContents.set(row, {message: m, text});
  }

  function messageAt(target) {
    if (target.closest(".code-block, pre")) return null;
    const bubble = target.closest(".bubble, .bot-bubble");
    const data = bubble && messageContents.get(bubble.closest(".msg"));
    return data ? {bubble, ...data} : null;
  }

  function showMessageMenu(data, x, y, mobile) {
    closePops();
    copyReceipt?.remove();
    const selection = window.getSelection();
    const selected = selection && data.bubble.contains(selection.anchorNode) && data.bubble.contains(selection.focusNode) ? selection.toString() : "";
    if (mobile) selection?.removeAllRanges();
    const menu = el("div", "message-menu");
    menu.setAttribute("role", "menu");
    menu.setAttribute("aria-label", "消息操作");
    const items = [
      {label: "回复", glyph: "reply", run: () => armQuote(data.message.seq, previewText(data.message))},
      {label: "复制", glyph: "copy", run: async () => {
        if (!await copyText(selected || data.text)) { setStatus("复制失败，请再试一次"); return; }
        if (!mobile) return;
        const receipt = el("div", "message-copy-receipt", "已复制");
        receipt.setAttribute("role", "status");
        document.body.append(receipt);
        const r = data.bubble.getBoundingClientRect();
        receipt.style.left = Math.max(8, Math.min(r.left + (r.width - receipt.offsetWidth) / 2, innerWidth - receipt.offsetWidth - 8)) + "px";
        receipt.style.top = Math.max(8, r.top - receipt.offsetHeight - 8) + "px";
        copyReceipt = receipt;
        setTimeout(() => { receipt.remove(); if (copyReceipt === receipt) copyReceipt = null; }, 1500);
      }},
    ];
    for (const item of items) {
      if (item.label === "回复" && !data.message.seq) continue;
      const button = el("button", "menu-row");
      button.type = "button";
      button.setAttribute("role", "menuitem");
      button.append(icon(item.glyph), el("span", "", item.label));
      button.addEventListener("click", () => { closeMessageMenu(); item.run(); });
      menu.append(button);
    }
    document.body.append(menu);
    const r = data.bubble.getBoundingClientRect();
    const headerBottom = document.querySelector(".chat > .top")?.getBoundingClientRect().bottom || 0;
    const position = mobile
      ? mobileMessageMenuPosition(r, menu.offsetWidth, menu.offsetHeight, innerWidth, innerHeight, data.bubble.closest(".msg").classList.contains("user"), headerBottom)
      : messageMenuPosition(x, y, menu.offsetWidth, menu.offsetHeight, innerWidth, innerHeight);
    menu.style.left = position.left + "px";
    menu.style.top = position.top + "px";
    messageMenu = menu;
    messageTarget = data.bubble;
    messageTarget.classList.add("message-target");
    document.documentElement.classList.add("message-menu-open");
    menu.querySelector("button:not(:disabled)")?.focus({preventScroll: true});
  }

  thread.addEventListener("contextmenu", (ev) => {
    const data = messageAt(ev.target);
    if (!data) return; // Code blocks keep the browser's own menu.
    ev.preventDefault();
    if (!matchMedia("(hover: none)").matches) showMessageMenu(data, ev.clientX, ev.clientY, false);
  });
  thread.addEventListener("pointerdown", (ev) => {
    if (ev.pointerType !== "touch" || !matchMedia("(hover: none)").matches) return;
    const data = messageAt(ev.target);
    if (!data) return;
    closeMessageMenu();
    messagePress = {x: ev.clientX, y: ev.clientY, timer: setTimeout(() => showMessageMenu(data, ev.clientX, ev.clientY, true), 500)};
  });
  thread.addEventListener("pointermove", (ev) => {
    if (messagePress && Math.hypot(ev.clientX - messagePress.x, ev.clientY - messagePress.y) > 8) closeMessageMenu();
  });
  for (const event of ["pointerup", "pointercancel"]) thread.addEventListener(event, () => {
    if (messagePress) clearTimeout(messagePress.timer);
    messagePress = null;
  });
  document.addEventListener("pointerdown", ev => {
    if (messageMenu && !messageMenu.contains(ev.target)) closeMessageMenu();
  }, true);
  document.addEventListener("scroll", closeMessageMenu, true);
  window.addEventListener("resize", closeMessageMenu);
  document.addEventListener("keydown", ev => {
    if (!messageMenu) return;
    if (!["Escape", "ArrowDown", "ArrowUp", "Enter"].includes(ev.key)) return;
    ev.preventDefault();
    ev.stopImmediatePropagation();
    if (ev.key === "Escape") { closeMessageMenu(); return; }
    const buttons = [...messageMenu.querySelectorAll("button:not(:disabled)")];
    const i = buttons.indexOf(document.activeElement);
    if (ev.key === "Enter") { buttons[Math.max(0, i)]?.click(); return; }
    buttons[(i + (ev.key === "ArrowDown" ? 1 : buttons.length - 1)) % buttons.length]?.focus();
  }, true);

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

  // "writer@MacBook" is the bot writer on the paired device MacBook; a bot
  // on this computer has no device. Bot names never contain "@".
  function splitPeer(name) {
    const raw = String(name || "");
    const cut = raw.indexOf("@");
    return cut < 0 ? { bot: raw, device: "" } : { bot: raw.slice(0, cut), device: raw.slice(cut + 1) };
  }

  // Bot-to-bot traffic in the owner's chat: one centered gray line per run of
  // events with the same peer, never the other bot's text. A run ends at the
  // owner's message or a bot reply to the owner (any other row), so only the
  // trailing event lines can take another event.
  function peerMergeIndex(tail, peer) {
    for (let i = tail.length - 1; i >= 0; i--) if (tail[i] === peer) return i;
    return -1;
  }

  // The words around the peer's name for a run: sent, received, and bounced
  // counts. One message keeps its own wording; more say how many.
  function peerEventWords(g) {
    const total = g.sent + g.received;
    const lost = g.bounced ? g.bounced + " 条没送到" : "";
    if (total > 1) return { lead: "和", tail: "往来了 " + total + " 条消息", lost: lost };
    if (g.sent) return { lead: "已发消息给", tail: "", lost: lost };
    if (g.received) return { lead: "收到", tail: "的消息", lost: lost };
    return { lead: "发给", tail: "的消息没送到", lost: "" };
  }

  function paintPeerEvent(row) {
    const g = row._peer;
    const who = splitPeer(g.peer);
    const words = peerEventWords(g);
    const device = g.device || who.device;
    const name = el("span", "peer-event-name", niceName(who.bot) + (device ? " · " + device : ""));
    row.replaceChildren(el("span", "", words.lead), peerAvatar({ bot: who.bot, device: device }, "xs"), name);
    if (words.tail) row.append(el("span", "", words.tail));
    // The dot is its own flex item, so the row's gap puts an even space on both
    // sides; a space typed after it came out too thin next to the CJK text.
    if (words.lost) row.append(el("span", "peer-event-dot", " · "), el("span", "", words.lost));
    row.dataset.count = String(g.sent + g.received);
  }

  // Adds one event (kind sent, received or bounce) to the run it belongs to,
  // or starts a line at the end of container. device is the paired device's
  // display label when the transcript row has one (m.relay.device).
  function addPeerEvent(container, kind, peer, ts, device) {
    const tail = [];
    for (let n = container.lastElementChild; n && n._peer; n = n.previousElementSibling) tail.unshift(n);
    const at = peerMergeIndex(tail.map((n) => n._peer.peer), peer);
    let row = at < 0 ? null : tail[at];
    if (!row) {
      row = el("button", "sysline link peer-event");
      row.type = "button";
      row.dataset.peer = peer;
      row._peer = { peer: peer, sent: 0, received: 0, bounced: 0, first: ts || "" };
      row.addEventListener("click", () => openPeer(state.sel && state.sel.id, peer, row._peer.first));
      setTip(row, "查看往来");
      container.append(row);
    }
    if (device) row._peer.device = device;
    if (kind === "sent") row._peer.sent++;
    else if (kind === "received") row._peer.received++;
    else row._peer.bounced++;
    paintPeerEvent(row);
    return row;
  }

  // A remote bounce the outbox or the peer wrote back into the sender's inbox.
  function bounceTarget(parsed) {
    if (parsed.source !== "remote") return "";
    const m = /^message to (\S+@\S+?) was not delivered\b/.exec(parsed.text || "");
    return m ? m[1] : "";
  }

  function optionBadge(letter, ghost) {
    const badge = el("span", "opt-badge" + (ghost ? " ghost" : ""));
    if (ghost) badge.append(rawSvg(PENCIL_PATH, 14, 2.2));
    else badge.textContent = letter;
    return badge;
  }

  function optionCards(info, picked, question, choose = sendText, selected = null) {
    const card = el("div", "ask" + (picked != null ? " done" : ""));
    card.setAttribute("role", "radiogroup");
    if (question) card.setAttribute("aria-label", question);
    const opts = Array.isArray(info.args.options) ? info.args.options : [];
    const rows = [];
    let chosen = -1;
    opts.forEach((raw, i) => {
      let label = String(raw);
      let recommend = label === info.args.recommended;
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
      li.setAttribute("aria-checked", String(row.label === selected));
      li.append(optionBadge(letter), el("span", "opt-t", row.label));
      if (row.recommend) li.append(el("span", "opt-recommend", "推荐"));
      if (row.label === selected) li.append(rawSvg(CHECK_PATH, 16, 2.4));
      li.append(el("span", "opt-kbd", letter));
      li.addEventListener("click", () => choose(row.label));
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
        if (i >= 0) { ev.preventDefault(); choose(rows[i].label); }
      } else if (/^[a-zA-Z]$/.test(ev.key)) {
        const hit = items.find((x) => x.querySelector(".opt-badge").textContent === ev.key.toUpperCase());
        if (hit) hit.focus();
      }
    });
    card.append(list);
    return card;
  }

  function appendBotContent(parent, text) {
    const bubble = el("div", "prose bot-bubble");
    appendRich(bubble, text);
    parent.append(bubble);
  }

  function addBubbleTime(row, time) {
    const body = row.querySelector(".bubble-col, .body");
    if (body && time) {
      body.append(el("span", "ht", clockTime(time)));
    }
  }

  function timeDivider(previous, time, now = new Date()) {
    const current = new Date(time);
    if (!Number.isFinite(current.getTime())) return "";
    const prev = new Date(previous);
    if (Number.isFinite(prev.getTime()) && prev.toDateString() === current.toDateString() && current - prev < 30 * 60000) return "";
    const yesterday = new Date(now);
    yesterday.setDate(yesterday.getDate() - 1);
    const day = current.toDateString() === now.toDateString() ? "今天" : current.toDateString() === yesterday.toDateString() ? "昨天" : current.getFullYear() + "/" + (current.getMonth() + 1) + "/" + current.getDate();
    return day + " " + clockTime(time);
  }

  function addTimeDivider(inner, row, previous) {
    if (inner.classList.contains("grpchat")) return;
    const last = previous || Array.from(inner.querySelectorAll(".msg[data-time]")).at(-1)?.dataset.time;
    const label = timeDivider(last, row.dataset.time);
    if (label) inner.append(el("div", "tdiv", label));
  }

  function addTimeDividers(inner) {
    if (inner.classList.contains("grpchat")) return;
    let previous;
    for (const row of inner.querySelectorAll(":scope > .msg[data-time]")) {
      const label = timeDivider(previous, row.dataset.time);
      if (label) row.before(el("div", "tdiv", label));
      previous = row.dataset.time;
    }
  }

  function appendRich(parent, text) {
    const src = String(text || "");
    const re = /```([^\n`]*)\n?([\s\S]*?)```/g;
    let last = 0;
    let match;
    const addText = (chunk) => {
      const pulled = pullImages(chunk);
      if (pulled.text) {
        const prose = el("div", "prose");
        appendMarkdown(prose, pulled.text);
        parent.append(prose);
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
    return String(m.role === "assistant" ? displayText(raw) : raw).replace(/\r\n/g, "\n").trim();
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

  function userBubble(m, messages) {
    const parsed = unwrap(m.content || "");
    const row = el("div", "msg user");
    if (m.seq) row.dataset.seq = String(m.seq);
    if (m.time) row.dataset.time = String(m.time);
    row.dataset.sender = "user";
    const wrap = el("div", "bubble-col");
    if (m.branch) wrap.append(bgFrom());
    if (m.reply_to) wrap.append(quoteBlock(messages, m.reply_to));
    const pulled = pullImages(parsed.text);
    const pasted = pastedURLs(state.sel, m.images);
    if (pulled.text) {
      const bubble = el("div", "bubble");
      appendRich(bubble, pulled.text);
      wrap.append(bubble);
    }
    if (pulled.images.length) wrap.append(gallery(pulled.images));
    if (pasted.length) wrap.append(gallery(pasted));
    if (!pulled.text && !pulled.images.length && !pasted.length) {
      const bubble = el("div", "bubble");
      bubble.textContent = parsed.text;
      wrap.append(bubble);
    }
    if (parsed.untrusted && parsed.source !== "timer" && !parsed.source.startsWith("bot:")) {
      wrap.append(el("span", "tag warn", parsed.source ? "不可信 · " + parsed.source : "不可信"));
    }
    bindMessageMenu(row, m, parsed.text);
    row.append(wrap);
    addBubbleTime(row, m.time);
    return row;
  }

  function botBubble(m, results, messages, index, who) {
    const row = el("div", "msg bot");
    if (m.seq) row.dataset.seq = String(m.seq);
    if (m.time) row.dataset.time = String(m.time);
    row.dataset.sender = "bot:" + (who || "");
    const body = el("div", "body");
    if (m.branch) body.append(bgFrom());
    if (m.reply_to) body.append(quoteBlock(messages, m.reply_to));
    if (m.content) appendBotContent(body, displayText(m.content));
    const calls = m.tool_calls || [];
    const peers = [];
    for (const call of calls) {
      const info = toolSummary(call);
      const phrase = peerPhrase(info.name, info.args, who);
      if (phrase) { peers.push(phrase); continue; }
      if (info.name === "ask_user") {
        // ask_user now persists its own decision card, rendered below.

        continue;
      }
    }
    bindMessageMenu(row, m, m.content || "");
    row.append(body);
    addBubbleTime(row, m.time);
    return { row: row, peers: peers };
  }

  function skipMessage(m) {
    if (!m || m.role === "tool" || m.role === "system") return true;
    // A kernel note the owner did not say. The transcript row says so; the
    // interface does not recognize the text.
    return !!m.internal;
  }

  function renderMessages(messages, who) {
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = state.firstRender || thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(niceName(who) || "XBot", EMPTY_LINE, who, true));
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
    let lastMsg = null;
    let lastRoutine = null;
    const relays = backgroundRelays(messages);
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
        const approval = approvalHistoryCard(appr, m.time);
        approval.dataset.branch = m.branch || "";
        inner.append(approval);
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
        if (parsed.untrusted && (parsed.source.startsWith("bot:") || parsed.source.startsWith("group:"))) {
          any = true;
          lastKey = "";
          if (parsed.source.startsWith("bot:")) {
            addPeerEvent(inner, "received", parsed.sender || parsed.source.slice(4), m.time, m.relay && m.relay.device);
          } else {
            inner.append(systemLine("群里 · " + (clip(pullImages(parsed.text).text, 80) || "一条消息")));
          }
          return;
        }
        if (bounceTarget(parsed)) {
          any = true;
          lastKey = "";
          addPeerEvent(inner, "bounce", bounceTarget(parsed), m.time);
          return;
        }
        if (parsed.untrusted) {
          inner.append(eventLine(parsed, (state.sel ? state.sel.id : "") + ":event:" + (m.seq || m.time || i)));
          any = true;
          lastKey = "";
          return;
        }
        any = true;
        const row = userBubble(m, messages);
        const cont = !groupStart(lastMsg, m, lastKey, "user");
        if (cont) row.classList.add("cont");
        lastMsg = m;
        lastKey = "user";
        inner.append(row);
        return;
      }
      if (relays.has(i)) return;
      any = true;
      const key = "bot:" + (who || "");
      const cont = !groupStart(lastMsg, m, lastKey, key);
      const built = botBubble(m, results, messages, i, who);
      if (m.content) {
        if (cont) markContinuation(built.row, m.time);
        inner.append(built.row);
        lastKey = key;
        lastMsg = m;
      }
      for (const line of built.peers) {
        if (line.sent) addPeerEvent(inner, "sent", line.peer, m.time);
        else inner.append(systemLine(line.text, who, line.peer));
        lastKey = "";
      }
    });
    if (!any) inner.append(emptyState(niceName(who) || "XBot", EMPTY_LINE, who, true));
    addTimeDividers(inner);
    restoreSendFeedback(inner);
    thread.replaceChildren(inner);
    state.firstRender = false;
    thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
  }

  function renderGroup(messages, who) {
    const inner = el("div", "thread-inner grpchat");
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
    let lastMsg = null;
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
      row.dataset.sender = key;
      if (m.time) row.dataset.time = String(m.time);
      if (!groupStart(lastMsg, m, last, key)) row.classList.add("cont");
      lastMsg = m;
      if (mine) {
        const wrap = el("div", "bubble-col");
        const pulled = pullImages(text);
        if (pulled.text) {
          const bubble = el("div", "bubble");
          appendRich(bubble, pulled.text);
          wrap.append(bubble);
        }
        if (pulled.images.length) wrap.append(gallery(pulled.images));
        if (!pulled.text && !pulled.images.length) wrap.append(el("div", "bubble", text));
        bindMessageMenu(row, m, text);
        row.append(wrap);
      } else {
        if (!row.classList.contains("cont")) row.append(avatar(m.sender || "B", paintOf(m.sender)));
        const body = el("div", "body");
        if (!row.classList.contains("cont")) {
          const name = el("div", "who");
          name.append(el("span", "", niceName(m.sender || "bot")));
          if (m.time) name.append(el("time", "", when(m.time)));
          if (m.sender) setTip(name, m.sender);
          body.append(name);
        }
        appendBotContent(body, text);
        bindMessageMenu(row, m, text);
        row.append(body);
        if (row.classList.contains("cont")) markContinuation(row, m.time);
      }
      addBubbleTime(row, m.time);
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
      notice: tag.attrs.notice || "",
      summary: body.slice(1, body.length - tail.length),
    };
  }

  // Only the server defines the decision window. Without a stamped
  // deadline there is no client countdown (for example a node command).
  function approvalDeadlineMs(opt) {
    if (!opt || opt.wait === 0) return null;
    const raw = opt.deadline || opt.expires || opt.expiry;
    if (raw) {
      const t = typeof raw === "number" ? raw : Date.parse(raw);
      if (!Number.isNaN(t)) return t;
    }
    return null;
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

  function approvalPresentation(opt) {
    const summary = String(opt.summary || "");
    const command = String(opt.command || summary);
    const send = opt.title === "对外发送" || opt.kind === "outbound" || opt.kind === "send";
    let title = opt.title || opt.tool || "操作";
    let body = summary;
    if (send) {
      const url = (command + " " + summary).match(/https?:\/\/[^\s'"<>]+/i);
      if (url) {
        try { title = "发到 " + new URL(url[0]).hostname; } catch (e) {}
      }
      // Read shell words without executing shell syntax.
      const words = command.replace(/\\\r?\n/g, "").match(/(?:'[^']*'|"(?:\\.|[^"\\])*"|[^\s'"])+/g) || [];
      const unquote = (word) => {
        if (word.startsWith("'") && word.endsWith("'")) return word.slice(1, -1);
        if (word.startsWith('"') && word.endsWith('"')) {
          return word.slice(1, -1).replace(/\\(["\\$`])/g, "$1");
        }
        return word;
      };
      let payload = "";
      for (let i = 0; i < words.length; i++) {
        const word = words[i];
        if (/^(?:-d|--data|--data-raw|--data-binary|--json)$/.test(word)) {
          payload = unquote(words[i + 1] || "");
          break;
        }
        const attached = word.match(/^(?:--(?:data|data-raw|data-binary|json)=|-d)(.+)$/);
        if (attached) { payload = unquote(attached[1]); break; }
      }
      const candidate = payload || command;
      try {
        const json = JSON.parse(candidate);
        body = ["text", "content", "message"].map((key) => json[key]).find((v) => typeof v === "string") || payload || summary;
      } catch (e) { body = payload || summary; }
      // A file upload names a source, rather than revealing the message.
      if (payload.startsWith("@")) body = summary;
    }
    return { send, title, body, command };
  }

  function approvalLine(title, summary, state, time, send = false) {
    const kind = state === "approved" ? "ok" : state === "declined" ? "no" : "exp";
    const row = el("div", "approval-line " + kind);
    const st = el("span", "st");
    if (state === "approved") st.append(rawSvg(CHECK_PATH, 18, 2.4));
    else if (state === "declined") st.append(rawSvg(X_PATH, 18, 2.4));
    else st.append(rawSvg(CLOCK_PATH, 17, 2));
    const what = el("span", "what");
    const decision = state === "approved" ? (send ? "已发送" : "已允许") :
      state === "declined" ? (send ? "没发" : "已拒绝") :
      state === "no_decision:restart" ? "已取消，没有执行" : "已过期，没有执行";
    what.append(document.createTextNode(decision + " · " + (title || "操作") + (summary ? " " : "")));
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
    const view = approvalPresentation(opt);
    const { title, send } = view;
    const summary = send ? view.body : String(opt.summary || "");
    const card = el("div", "approval");
    const head = el("div", "approval-head");
    const ico = el("span", "approval-ico");
    ico.append(rawSvg(ICONS.terminal, 16, 2));
    const main = el("div", "approval-main");
    main.append(el("div", "approval-title", title));
    if (opt.subtitle && !(send && opt.subtitle === "在本机上执行")) main.append(el("div", "approval-detail", opt.subtitle));
    if (send && summary) main.append(el("blockquote", "quote", summary));
    if (view.command && (send || view.command.length > 100 || view.command.includes("\n"))) {
      const toggle = el("button", "text-btn", "查看命令");
      toggle.type = "button";
      toggle.setAttribute("aria-expanded", "false");
      const command = el("code", "approval-chip", view.command);
      command.hidden = true;
      toggle.addEventListener("click", () => {
        command.hidden = !command.hidden;
        toggle.setAttribute("aria-expanded", String(!command.hidden));
      });
      main.append(toggle, command);
    } else if (!send && summary) main.append(el("code", "approval-chip", summary));
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
      card.replaceWith(state === "auto_approved" ? systemLine("已到等待时限，正在按允许继续") : approvalLine(title, summary, state, Date.now(), send));
    };
    const deadlineMs = approvalDeadlineMs(opt);
    if (deadlineMs != null && deadlineMs <= Date.now()) {
      return systemLine("已到等待时限，正在按允许继续");
    }
    if (deadlineMs != null) {
      const timer = el("span", "approval-timer");
      timer.append(rawSvg(CLOCK_PATH, 13, 2));
      timerText = el("span");
      timer.append(timerText);
      const paint = () => {
        const left = deadlineMs - Date.now();
        if (left <= 0) { settle("auto_approved"); return; }
        const label = approvalRemainText(left) + (send ? "后照发" : "后按允许继续");
        if (timerText.textContent !== label) timerText.textContent = label;
      };
      paint();
      tick = window.setInterval(paint, 1000);
      actions.append(timer);
    }
    const deny = el("button", "secondary", send ? "不发" : "拒绝");
    const allow = el("button", "primary", send ? "发送" : "允许");
    deny.type = "button";
    allow.type = "button";
    const decide = async (ok) => {
      if (settled || allow.disabled) return;
      const clicked = ok ? allow : deny;
      const original = clicked.textContent;
      const spinner = rawSvg(ICONS.spinner, 13, 2);
      spinner.classList.add("approval-spinner");
      clicked.replaceChildren(spinner, document.createTextNode(send && ok ? "正在发送…" : "正在处理…"));
      clicked.setAttribute("aria-busy", "true");
      allow.disabled = true;
      deny.disabled = true;
      try {
        await opt.decide(ok);
        settle(ok ? "approved" : "declined");
      } catch (e) {
        if (e && e.status === 410) {
          settle("auto_approved");
          return;
        }
        clicked.textContent = original;
        clicked.removeAttribute("aria-busy");
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
    if (card.tool === "ask_user") {
      if (!["pending", "approved", "auto_approved"].includes(st)) return systemLine("提问已结束（服务已重启）");
      const args = JSON.parse(card.summary);
      const automatic = st === "auto_approved";
      const box = el("div", "question-card");
      const botName = state.sel && state.sel.id;
      const choose = async (choice) => {
        if (automatic) { sendText(choice); return; }
        try {
          await api("/api/bots/" + encodeURIComponent(botName) + "/questions/" + encodeURIComponent(card.id), {method:"POST",body:{choice}});
          await loadThread();
        } catch (err) { setStatus(visibleError(err), true); }
      };
      box.append(optionCards({args}, automatic ? null : (args.choice || null), args.question, choose, automatic ? args.choice : null));
      if (card.notice) box.append(systemLine(card.notice));
      return box;
    }
    if (st === "auto_approved") return systemLine(card.notice);
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
    const view = approvalPresentation(card);
    return approvalLine(view.title, view.send ? view.body : card.summary || "", st, time, view.send);
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
    return displayText(md)
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
    const raw = plainText(displayText(text));
    return raw && raw !== "SILENT" ? clip(raw, 72) : "";
  }

  function lineForBot(b) {
    const p = state.preview["b:" + b.name];
    if (p && realPreview(p)) return realPreview(p);
    if (b.description) return plainText(b.description);
    if (b.persona) return plainText(b.persona);
    return "没有消息";
  }

  function lineForGroup(g) {
    const p = state.preview["g:" + g.id];
    if (p) return plainText(p);
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
    menu.style.right = "auto";
    menu.style.bottom = "auto";
    menu.style.left = "0";
    menu.style.top = "0";
    menu.style.zIndex = "45";
    // Measure only after the menu is out of the base .pop box, which spans
    // left:0/right:0 and would otherwise report the page width.
    const width = menu.offsetWidth;
    const height = menu.offsetHeight;
    const r = anchor.getBoundingClientRect();
    let left, top;
    if (side === "panel") {
      // Menus opened from the panel stay inside it: left edge on the action
      // row's line, 220 wide, dropping from the button.
      const p = $("panel").getBoundingClientRect();
      left = p.left + 12;
      top = r.bottom + 6;
      if (left + width > p.right - 12) left = Math.max(8, p.right - 12 - width);
    } else {
      left = side === "left" ? r.left - width - 6 : r.right - width;
      if (left < 8) left = Math.min(window.innerWidth - width - 8, r.right + 6);
      top = side === "left" ? r.top - 6 : r.bottom + 4;
    }
    if (top + height > window.innerHeight - 8) top = Math.max(8, window.innerHeight - height - 8);
    menu.style.left = left + "px";
    menu.style.top = top + "px";
    menu.style.right = "auto";
    menu.style.bottom = "auto";
  }

  function botRow(b) {
    const d = stDesc(b.name);
    const active = state.sel && state.sel.type === "bot" && state.sel.id === b.name;
    // A dot for a new message or an unread note; the open bot never shows one.
    const unread = !active && (!!b.unread || (b.notes || 0) > 0);
    const btn = listButton(b.name, d.node, active, unread, paintOfBot(b), !!b.primary);
    btn.dataset.bot = b.name;
    btn.dataset.stsig = d.sig;
    btn.classList.toggle("off", d.off);
    btn.addEventListener("click", () => pickBot(b.name));
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
    // Pinned bots sit in the pin row only, so the count leaves them out.
    const pinnedHere = (folder.bots || []).filter((n) => (state.bots.find((b) => b.name === n) || {}).pinned).length;
    toggle.append(el("span", "folder-count", String(Math.max(0, folder.count - pinnedHere))));
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
      if (byName[name] && !byName[name].pinned) body.append(botRow(byName[name]));
    }
    box.append(body);
    return box;
  }

  function renderLists() {
    const collapsed = folderStore();
    const sig = JSON.stringify({
      bots: state.bots.map((b) => [b.name, !!b.pinned, !!b.primary, !!b.unread, (b.notes || 0) > 0, b.group || "", b.description, lineForBot(b)]),
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
      btn.dataset.bot = b.name;
      setTip(btn, niceName(b.name), "right");
      btn.setAttribute("aria-label", niceName(b.name));
      const av = avatar(b.name, paintOfBot(b), "pin");
      const pinActive = state.sel && state.sel.type === "bot" && state.sel.id === b.name;
      if (!pinActive && (!!b.unread || (b.notes || 0) > 0)) av.append(el("i", "unread"));
      // A pinned bot shows its name under the face; the primary bot keeps its pin.
      const label = el("span", "pin-name");
      label.append(el("span", "pin-label", niceName(b.name)));
      if (b.primary) label.append(primaryMark());
      btn.append(av, label);
      btn.addEventListener("click", () => pickBot(b.name));
      pins.append(btn);
    }
    // One 新建 for the sidebar: 新建 Bot or 新建群组 (the 群组 "+" merged in).
    const add = el("button", "pin add");
    add.type = "button";
    add.setAttribute("aria-label", "新建 Bot 或群组");
    add.setAttribute("aria-haspopup", "menu");
    add.setAttribute("aria-expanded", "false");
    setTip(add, "新建 Bot 或群组", "right");
    const face = el("span", "plus-face");
    face.append(icon("plus"));
    add.append(face, el("span", "pin-name pin-label", "新建"));
    add.addEventListener("click", (ev) => {
      ev.stopPropagation();
      toggleNewMenu(add);
    });
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
          if (b && !b.pinned) body.append(botRow(b));
        }
        roster.append(body);
      } else roster.append(folderSection(folder));
    }
    if (state.others && state.others.length) roster.append(othersFolder());

    // No groups, no header: 新建 already offers 新建群组.
    if (state.groups.length) roster.append(sectionLabel("群组"));
    for (const g of state.groups) {
      const btn = listButton(g.id, lineForGroup(g), state.sel && state.sel.type === "group" && state.sel.id === g.id, false, null, false, niceName(g.name || g.id));
      btn.addEventListener("click", () => {
        if (state.sel && state.sel.type === "group" && state.sel.id === g.id && !state.peer) return;
        selectGroup(g.id);
      });
      roster.append(btn);
    }
    paintStatus();
  }

  function renderMe() {
    const slot = $("me-avatar");
    // owner_name, never the raw id: on Windows the id carries the account SID (#210).
    const name = state.ownerName.trim();
    if (name) slot.replaceChildren(avatar(name, null, "me"));
    else {
      const face = el("span", "avatar me blob");
      face.append(icon("person"));
      slot.replaceChildren(face);
    }
    $("me-name").textContent = name || "我";
  }

  // The 新建 menu under the pin row's last button.
  function closeNewMenu() {
    const menu = document.querySelector(".new-menu");
    if (!menu) return;
    const hadFocus = menu.contains(document.activeElement);
    menu.remove();
    const add = document.querySelector(".pin.add");
    if (add) {
      add.setAttribute("aria-expanded", "false");
      if (hadFocus) add.focus(); // Escape returns to 新建, not to the page
    }
  }

  function toggleNewMenu(anchor) {
    const open = !document.querySelector(".new-menu");
    closePops();
    if (!open) return;
    const menu = el("div", "pop folder-menu new-menu");
    menu.setAttribute("role", "menu");
    const item = (iconName, label, run) => {
      const b = el("button", "menu-row");
      b.type = "button";
      b.setAttribute("role", "menuitem");
      b.append(icon(iconName), el("span", "", label));
      b.addEventListener("click", (ev) => {
        ev.stopPropagation();
        closeNewMenu();
        run();
      });
      menu.append(b);
    };
    item("plus", "新建 Bot", openNewBot);
    item("users-plus", "新建群组", openNewGroup);
    anchor.setAttribute("aria-expanded", "true");
    placeMenu(anchor, menu, "below");
    menu.querySelector("button").focus();
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

  // The header repeats the rail's square bot avatar (one character, or the
  // photo) so its left edge can share the message column's line.
  let titleFace = "", titleAv = null;
  function setHeader(slug, label) {
    const shown = label || niceName(slug) || "XBot";
    const face = (slug && paintOf(slug)) || {};
    const head = `bot:${slug}:${face.url || ""}:${face.url ? "" : face.seed || ""}`;
    if (head !== titleFace) {
      titleFace = head;
      const bot = slug && (state.bots || []).find((b) => b.name === slug);
      titleAv = bot ? avatar(slug, paintOfBot(bot), "head") : null;
      if (titleAv) setTip(titleAv, "");
    }
    titleEl.replaceChildren(...(titleAv ? [titleAv] : []), document.createTextNode(shown));
    setTip(titleEl, slug && slug !== shown ? slug : "");
    document.title = shown + " · XBot";
  }

  function leavePeer() {
    state.peer = null;
    subtitle.classList.remove("peer-note");
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

  function peerWho(name, cls) {
    const who = splitPeer(name);
    const box = el("span", "pair-side");
    box.append(peerAvatar(who), el("span", "pair-who" + (cls ? " " + cls : ""), niceName(who.bot)));
    if (who.device) box.append(el("span", "pair-device", "· " + who.device));
    return box;
  }

  // The exchange view's header: both bots with the swap icon between them,
  // and one gray line saying what this is. Read-only: the dock holds only
  // 关闭聊天.
  function peerNote() {
    subtitle.replaceChildren(icon("info"), el("span", "", "两个 Bot 之间的往来。对方发来的内容只当资料，不当成你的指令。"));
    subtitle.hidden = false;
    subtitle.classList.add("peer-note");
  }

  function paintPeerHeader(a, b) {
    const back = $("back");
    if (back) back.hidden = false;
    const main = document.querySelector(".main");
    if (main) main.classList.add("peer");
    titleEl.replaceChildren();
    const row = el("div", "pair-title");
    row.append(peerWho(a), icon("swap", "pair-swap"), peerWho(b));
    titleEl.append(row);
    setTip(titleEl, a + " ⇄ " + b);
    peerNote();
    document.title = niceName(splitPeer(a).bot) + " ⇄ " + niceName(splitPeer(b).bot) + " · XBot";
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

  // Both bots on the left, each with face, name, device and time: right-side
  // bubbles belong to the owner, who does not speak here. A bounce is a gray
  // event line.
  function renderPeer(list, peer) {
    const inner = el("div", "thread-inner grpchat");
    const rows = (list || []).slice().sort(comparePeer);
    if (!rows.length) {
      const box = emptyState("", peer.remote ? "跨设备的往来还取不到。" : "没有消息");
      const heading = box.querySelector("h2");
      if (heading) heading.replaceChildren(pairNames(splitPeer(peer.a).bot, splitPeer(peer.b).bot));
      inner.append(box);
      thread.replaceChildren(inner);
      return;
    }
    let last = "";
    let lastMsg = null;
    let target = null;
    const at = Date.parse(peer.at || "") || 0;
    for (const m of rows) {
      if (m.kind === "bounce") {
        const to = m.from_bot ? { bot: m.from_bot, device: m.from_device || "" } : splitPeer(peer.b);
        const line = systemLine("上一条没送到 " + niceName(to.bot) + (to.device ? " · " + to.device : ""));
        setTip(line, m.text || "");
        inner.append(line);
        last = "";
        lastMsg = null;
        continue;
      }
      // The API splits bot@device (from_device is the device's label).
      const from = m.from_bot ? { bot: m.from_bot, device: m.from_device || "" } : splitPeer(m.from);
      const row = el("div", "msg bot peer-msg");
      const key = "bot:" + from.bot + "@" + from.device;
      row.dataset.sender = key;
      row.dataset.time = String(m.ts || "");
      if (m.id) row.dataset.id = String(m.id);
      const cont = !groupStart(lastMsg, m, last, key);
      lastMsg = m;
      if (!cont) row.append(peerAvatar(from));
      const body = el("div", "body");
      if (!cont) {
        const who = el("div", "who");
        who.append(el("span", "", niceName(from.bot)));
        if (from.device) {
          const dev = el("span", "who-device");
          dev.append(icon("device"), el("span", "", from.device));
          who.append(dev);
        }
        if (m.ts) who.append(el("time", "", when(m.ts)));
        if (m.from) setTip(who, m.from);
        body.append(who);
      }
      const text = el("div", "peer-text prose bot-bubble");
      text.textContent = m.text || "";
      body.append(text);
      row.append(body);
      if (cont) markContinuation(row, m.ts);
      addBubbleTime(row, m.ts);
      inner.append(row);
      if (!target && at && (Date.parse(m.ts || "") || 0) >= at) target = row;
      last = key;
    }
    thread.replaceChildren(inner);
    // The clicked run may predate the loaded page: loadPeer fetches older
    // pages first (peerNeedsOlder), so only scroll once the run is loaded.
    if (target && !peer.scrolled && !peerNeedsOlder(peer)) {
      peer.scrolled = true;
      target.scrollIntoView({ block: "start" });
    } else if (!peerNeedsOlder(peer)) {
      if (peer.at) peer.scrolled = true;
      thread.scrollTop = thread.scrollHeight;
    }
  }

  // True while the run the owner clicked is older than everything loaded.
  function peerNeedsOlder(peer) {
    const at = Date.parse(peer.at || "") || 0;
    if (!at || peer.scrolled || !peer.more || !peer.list.length) return false;
    return (Date.parse(peer.list[0].ts || "") || 0) > at;
  }

  async function loadPeer(before) {
    const peer = state.peer;
    if (!peer) return;
    const keep = !!before;
    // Paging back by scrolling keeps the view; paging back to reach a clicked
    // run lets renderPeer place it.
    const settled = !peer.at || !!peer.scrolled;
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
    if (got.mock && peer.remote) {
      // Cross-device history has no endpoint yet (prototype v2 item 21).
      if (!keep) renderPeer([], peer);
      peer.more = false;
      return;
    }
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
    if (keep && settled) thread.scrollTop = Math.max(0, thread.scrollHeight - prevHeight + prevTop);
    // Up to 10 more pages to reach the clicked run.
    if (peerNeedsOlder(peer) && (peer.hops = (peer.hops || 0) + 1) <= 10) await loadPeer(peer.cursor);
  }

  // at: the first event's time, so the view opens at the run that was clicked.
  async function openPeer(a, b, at) {
    if (!a || !b || a === b || state.streaming) return;
    clearQuote();
    state.peer = { a: a, b: b, at: at || "", remote: !!splitPeer(b).device, list: [], more: false, cursor: null, mock: false };
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
    // The exchange view's subtitle is its read-only note: an error shows in
    // its place until the next status, which brings the note back.
    if (state.peer && !(isError && text)) {
      subtitleError = false;
      peerNote();
      return;
    }
    if (state.peer) subtitle.classList.remove("peer-note");
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
    closeMessageMenu();
    // Pasted pictures belong to the bot they were uploaded to.
    clearAttach();
    state.streaming = 0;
    state.pendingSends = [];
    state.replyErrors = [];
    state.firstRender = true;
    state.messages = [];
    state.panelData = null;
    state.traceOpen = {};
    state.traceOut = {};
    // The snapshot belongs to one bot's one wake. Keeping it would show A's
    // context under B's name, so it is dropped and refetched on demand.
    state.ctxSnap = null;
    state.ctxStep = -1;
    state.ctxMenu = false;
    state.ctxRaw = "";
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
    const retry = el("button", "btn sec");
    retry.append(icon("refresh-cw"), document.createTextNode("重试"));
    retry.type = "button";
    retry.addEventListener("click", async () => {
      retry.disabled = true;
      await loadThread();
      retry.disabled = false;
      if (panel.classList.contains("open")) await loadPanel();
    });
    box.append(doodle("offline"), el("h3", "", "连不上 XBot"), el("p", "", "本机的 XBot 服务没有响应。它可能刚升级完还在重启，等几秒再试一次。"), retry);
    thread.replaceChildren(box);
  }

  // Clicking the conversation that is already open keeps its live reply and
  // pending or failed sends instead of resetting it.
  function pickBot(name) {
    if (state.sel && state.sel.type === "bot" && state.sel.id === name && !state.peer) return;
    return selectBot(name);
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
    setComposerPlaceholder("写给 " + niceName(name));
    renderLists();
    const selection = state.sel;
    // Reading notifications runs on its own: it never holds up the panel, and a stale
    // failure never overwrites another bot's status.
    readBotNotes(name).catch(() => {
      if (state.sel === selection) setStatus("通知未能标为已读，请重试。", true);
    });
    await loadThread();
    if (state.sel === selection && panel.classList.contains("open") && !state.panelData) await loadPanel();
  }

  async function selectGroup(id) {
    leavePeer();
    clearQuote();
    state.sel = { type: "group", id: id };
    resetConversation();
    state.sig = "";
    state.editOpen = false;
    closeDrawer();
    try {
      const draft = sessionStorage.getItem("xbot-draft:group:" + id);
      if (draft) {
        sessionStorage.removeItem("xbot-draft:group:" + id);
        if (!input.value) input.value = draft;
        syncComposer();
      }
    } catch (e) {}
    const g = state.groups.find((x) => x.id === id);
    setHeader(id, g ? niceName(g.name || id) : "");
    setStatus("");
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
        if (panel.classList.contains("open")) await loadPanel();
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

  const TABS = [["details", "详情"], ["library", "记忆"], ["computer", "任务"], ["trace", "轨迹"], ["context", "上下文"]];

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

  // The bot's actions are icon buttons in the info panel's top bar, left of
  // its close button; the header keeps a single control that opens the panel.
  function panelAct(iconName, label, danger) {
    const btn = el("button", "ghost panel-act" + (danger ? " danger" : ""));
    btn.type = "button";
    btn.setAttribute("aria-label", label);
    setTip(btn, label);
    btn.append(icon(iconName));
    return btn;
  }

  function menuRow(iconName, label, onClick, danger) {
    const btn = panelAct(iconName, label, danger);
    btn.addEventListener("click", (ev) => {
      ev.stopPropagation();
      closePanelMenu();
      onClick();
    });
    return btn;
  }

  // closePanelMenu removes the open submenu. placeMenu moves it to <body> while
  // open, so it is no longer a descendant of #panel-actions; selecting only
  // inside the row would leave it on screen, still acting on the old bot.
  let panelMore = null;
  let panelSub = null;
  let chromeBot = "";

  function closePanelSub() {
    if (!panelSub) return;
    panelSub.hidden = true;
    panelSub.remove();
    panelSub = null;
    document.querySelectorAll(".menu-move > .menu-row").forEach((btn) => {
      btn.setAttribute("aria-expanded", "false");
    });
  }

  function closePanelMore() {
    closePanelSub();
    if (panelMore) {
      panelMore.remove();
      panelMore = null;
    }
    const more = $("panel-more");
    if (more) {
      more.classList.remove("on");
      more.setAttribute("aria-expanded", "false");
    }
  }

  function closePanelMenu() {
    closePanelMore();
    document.querySelectorAll(".menu-sub").forEach((sub) => {
      sub.hidden = true;
      sub.remove();
    });
    document.querySelectorAll(".menu-move > .menu-row").forEach((btn) => {
      btn.setAttribute("aria-expanded", "false");
    });
  }

  // The panel top row: edit and more. Everything else moved into the menu.
  function syncPanelChrome() {
    const bot = state.sel && state.sel.type === "bot";
    // A repaint for the same bot (polling, status) must not close an open menu.
    if (bot && panelMore && chromeBot === state.sel.id) return;
    const row = $("panel-actions");
    closePanelMenu();
    row.replaceChildren();
    row.hidden = !bot;
    chromeBot = bot ? state.sel.id : "";
    if (!bot) return;
    const edit = panelAct("pencil", "编辑资料");
    edit.addEventListener("click", () => openProfile(state.panelData || {}));
    row.append(edit);
    const more = panelAct("ellipsis", "更多");
    more.addEventListener("click", (ev) => {
      ev.stopPropagation();
      openPanelMore(more);
    });
    more.id = "panel-more";
    more.setAttribute("aria-haspopup", "menu");
    more.setAttribute("aria-expanded", "false");
    row.append(more);
  }

  function currentGroup(name) {
    return (state.bots.find((b) => b.name === name) || {}).group || "";
  }

  function openPanelMore(anchor) {
    const data = state.panelData || {};
    if (!state.sel) return;
    // A second click on 更多 closes the menu instead of stacking another one.
    if (panelMore && anchor.classList.contains("on")) { closePanelMore(); return; }
    closePanelMore();
    const pinned = !!data.pinned;
    const menu = el("div", "pop panel-more");
    const item = (ic, text, fn, opts = {}) => {
      const row = el("button", "menu-row" + (opts.danger ? " danger" : ""));
      row.type = "button";
      row.append(icon(ic), el("span", "", text));
      row.addEventListener("click", (ev) => {
        ev.stopPropagation();
        closePops();
        fn();
      });
      menu.append(row);
      return row;
    };
    item("pin", pinned ? "取消置顶" : "置顶", () => togglePin(state.sel.id, !pinned));
    menu.append(moveMenu(state.sel.id, currentGroup(state.sel.id)));
    item("copy", "复刻", () => openFork(state.sel.id));
    const copy = item("clipboard", "复制 Bot ID", () => copyBotID(state.sel.id));
    copy.append(el("span", "v mono", state.sel.id));
    if (state.app) item("folder-open", "打开工作目录", () => openWorkdir(state.sel.id));
    item("download", "导出对话", () => openExport());
    menu.append(el("div", "mgap"));
    item("trash", "删除 Bot", () => confirmDelete(state.sel.id), { danger: true });
    anchor.classList.add("on");
    anchor.setAttribute("aria-expanded", "true");
    placeMenu(anchor, menu, "panel");
    panelMore = menu;
  }

  // The Clipboard API can be missing or refused (older engines, no
  // permission); the selection fallback still copies on a click.
  function legacyCopy(text) {
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.append(ta);
      ta.select();
      const ok = document.execCommand("copy");
      ta.remove();
      return ok;
    } catch (e) {
      return false;
    }
  }

  function copyBotID(name) {
    const done = () => showToast("已复制 " + name);
    const failed = () => setStatus("复制失败，请手动选择名字。", true);
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(name).then(done, () => (legacyCopy(name) ? done() : failed()));
      return;
    }
    legacyCopy(name) ? done() : failed();
  }

  async function openWorkdir(name) {
    try {
      await api("/api/bots/" + encodeURIComponent(name) + "/open-workdir", { method: "POST" });
    } catch (e) {
      setStatus(visibleError(e), true);
    }
  }

  let toastTimer = 0;
  function showToast(text) {
    const box = $("toast");
    box.replaceChildren(icon("check"), el("span", "", text));
    box.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { box.hidden = true; }, 3000);
  }

  // 移到分组 is a full menu row: icon, label, the current group on the right,
  // and the existing sub menu.
  function moveMenu(botName, current) {
    const wrap = el("div", "menu-move");
    const btn = el("button", "menu-row");
    btn.type = "button";
    btn.setAttribute("aria-haspopup", "menu");
    btn.setAttribute("aria-expanded", "false");
    const now = el("span", "v");
    now.append(el("span", "", current || "未分组"), icon("chevron"));
    btn.append(icon("folder"), el("span", "", "移到分组"), now);
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
        closePops();
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
      closePops();
      openNewFolder(botName);
    });
    sub.append(create);
    btn.addEventListener("click", (ev) => {
      ev.stopPropagation();
      const open = sub.hidden;
      if (open) {
        btn.setAttribute("aria-expanded", "true");
        placeMenu(btn, sub, "below");
        panelSub = sub;
      } else {
        sub.hidden = true;
        btn.setAttribute("aria-expanded", "false");
        if (panelSub === sub) panelSub = null;
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

  // The fixed head has a name above its identity line. Only the body scrolls.
  function renderPanelHead(name, paint, sub, pinned = false) {
    const ident = $("panel-ident");
    ident.replaceChildren();
    ident.hidden = false;
    ident.append(avatar(name, paint, "lg"));
    const who = el("div", "identity");
    who.append(el("b", "", niceName(name) || "Bot"));
    if (sub) {
      const line = el("div", "sub");
      if (pinned) line.append(icon("pin"));
      line.append(el("span", "model", sub));
      who.append(line);
    }
    ident.append(who);
    const tabs = $("panel-tabs");
    tabs.replaceChildren(tabBar());
    tabs.hidden = false;
  }

  function paintPanel() {
    panelBody.replaceChildren();
    paintContextBar();
    if (!state.sel) {
      $("panel-ident").hidden = true;
      $("panel-tabs").replaceChildren();
      syncPanelChrome();
      return;
    }
    if (state.sel.type === "group") {
      const g = state.groups.find((x) => x.id === state.sel.id) || { name: state.sel.id, members: [] };
      renderPanelHead(g.name || g.id, null, (g.members || []).length + " 个成员");
      const body = el("div", "tab-body");
      if (state.tab === "library") {
        body.append(el("p", "hint", "还没有记住任何事。对它说「记住……」就会出现在这里"));
      } else if (state.tab === "computer") {
        body.append(el("p", "hint", "没有任务"));
      } else if (state.tab === "trace") {
        body.append(el("p", "hint", "没有轨迹"));
      } else if (state.tab === "context") {
        body.append(el("p", "hint", "群组没有上下文"));
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
    const sub = (data.primary ? "主 Bot · " : "") + (data.model || "未绑定模型");
    renderPanelHead(data.name || state.sel.id, paintOfBot(data), sub, data.pinned);
    syncPanelChrome();
    const body = el("div", "tab-body");
    if (state.tab === "library") body.append(libraryView(data));
    else if (state.tab === "computer") body.append(computerView(data));
    else if (state.tab === "trace") body.append(traceView());
    else if (state.tab === "context") body.append(contextView(data));
    else body.append(detailsView(data));
    panelBody.append(body);
  }

  function durText(ms) {
    const s = Math.max(1, Math.round(ms / 1000));
    if (s < 60) return s + " 秒";
    const m = Math.round(s / 60);
    if (m < 60) return m + " 分";
    return Math.round(m / 60) + " 小时";
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
      wrap.append(panelEmpty("trace", "没有轨迹", "它每做一件事，都会在这里留下一行。"));
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


  // ---- 上下文检视 ------------------------------------------------------
  // One card: how full the working set is, what fills it, and the rows behind
  // the bands the owner is likely to open. It reads the kernel's in-memory
  // snapshot of the current wake and writes nothing. A bot driven by another
  // process, or one that has not woken yet, has no snapshot and says so
  // instead of showing zeros.

  // CTX_BANDS fixes the legend order. The messages come first and the tool
  // definitions last: the soft target is measured on the messages, so its tick
  // on the band lines up with where the messages end.
  const CTX_BANDS = [
    ["turn", "本轮"], ["hist", "早先对话"], ["block", "摘要块"],
    ["sys", "系统提示词"], ["mem", "记忆"], ["env", "环境更新"], ["tools", "工具定义"],
  ];

  function ctxK(n) {
    if (!n) return "0";
    if (n < 1000) return String(n);
    return (n / 1000).toFixed(1).replace(/\.0$/, "") + "K";
  }

  // ctxSums folds a request's layers into the legend's bands. Summary blocks
  // come one layer per block and are summed here.
  function ctxSums(req) {
    const sums = {};
    for (const l of req.layers || []) sums[l.key] = (sums[l.key] || 0) + l.tokens;
    if (req.tools > 0) sums.tools = req.tools;
    return sums;
  }

  function ctxReq(snap) {
    const reqs = snap.requests || [];
    if (state.ctxStep < 0 || state.ctxStep >= reqs.length) return reqs[reqs.length - 1];
    return reqs[state.ctxStep];
  }

  function contextView(data) {
    const wrap = el("div", "ctx");
    const snap = state.ctxSnap;
    if (snap === null) {
      wrap.append(el("p", "hint", "正在读取……"));
      return wrap;
    }
    const reqs = snap.requests || [];
    if (!reqs.length) {
      wrap.append(panelEmpty("", "还没有快照", "发一句话，这里就会显示它这一轮读了哪些内容。"));
      return wrap;
    }
    const req = ctxReq(snap);
    const latest = req === reqs[reqs.length - 1];
    const sums = ctxSums(req);
    // Two lines (ADR 0015). The ceiling is the request's hard limit, tools
    // included, so the band and the percentage are against it. The target is
    // the soft line the background compacts at, measured on the messages.
    const used = Math.max(0, req.prompt || 0);
    const msgs = Math.max(0, used - (req.tools || 0));
    const ceiling = snap.ceiling || 0;
    const target = snap.target || 0;
    const denom = ceiling || used || 1;
    const free = ceiling ? Math.max(0, ceiling - used) : 0;
    const share = (n) => (n * 100 / denom).toFixed(1) + "%";

    wrap.append(ctxPicker(reqs, req, latest));

    const head = el("div", "ctx-used");
    head.append(el("b", "num", ceiling ? Math.round(used * 100 / ceiling) + "% 已用" : ctxK(used) + " 已用"));
    if (ceiling) head.append(el("span", "num", ctxK(used) + " / " + ctxK(ceiling)));
    wrap.append(head);

    const band = el("div", "ctx-band");
    for (const [key, label] of CTX_BANDS) {
      if (!sums[key]) continue;
      const seg = el("i", "ctx-c-" + key);
      seg.style.width = Math.min(100, sums[key] * 100 / denom).toFixed(3) + "%";
      setTip(seg, label + " " + ctxK(sums[key]));
      band.append(seg);
    }
    const bandWrap = el("div", "ctx-bandwrap");
    bandWrap.append(band);
    if (ceiling && target && target < ceiling) {
      const mark = el("i", "ctx-mark");
      mark.style.left = (target * 100 / ceiling).toFixed(3) + "%";
      setTip(mark, "后台整理线 " + ctxK(target));
      bandWrap.append(mark);
    }
    wrap.append(bandWrap);

    const legend = el("div", "ctx-legend");
    const line = (cls, label, value, pct) => {
      const row = el("div", "ctx-lg" + (cls ? " " + cls : ""));
      row.append(el("span", "ctx-sw"), el("span", "ctx-lg-k", label), el("span", "ctx-lg-v num", value), el("span", "ctx-lg-p num", pct));
      legend.append(row);
    };
    for (const [key, label] of CTX_BANDS) {
      if (sums[key]) line("ctx-c-" + key, label, ctxK(sums[key]), share(sums[key]));
    }
    if (ceiling) line("ctx-free", "空余", ctxK(free), share(free));
    wrap.append(legend);

    const detail = snap.detail;
    if (detail) wrap.append(ctxDetails(detail, req));

    if (ceiling && target) {
      wrap.append(el("p", "ctx-foot num", msgs < target
        ? "离后台整理还有约 " + ctxK(target - msgs)
        : "后台在整理 · 离上限还有约 " + ctxK(free)));
    }
    return wrap;
  }

  // ctxPicker is the title with the request menu: the last ten model calls of
  // this wake, newest first. The newest is the default and is called 最新.
  function ctxPicker(reqs, req, latest) {
    const head = el("div", "ctx-hd");
    const pick = el("button", "ctx-pick");
    pick.type = "button";
    pick.setAttribute("aria-haspopup", "menu");
    pick.setAttribute("aria-expanded", state.ctxMenu ? "true" : "false");
    pick.append(el("span", "", "上下文"), el("small", "num", latest ? "最新" : "第 " + req.step + " 次"), icon("chevronDown", "ctx-pick-i"));
    pick.addEventListener("click", (ev) => {
      ev.stopPropagation();
      state.ctxMenu = !state.ctxMenu;
      paintPanel();
    });
    head.append(pick);
    if (state.ctxMenu) {
      const menu = el("div", "ctx-menu");
      menu.setAttribute("role", "menu");
      for (let i = reqs.length - 1; i >= 0; i--) {
        const r = reqs[i];
        const item = el("button", "ctx-menu-i" + (r === req ? " on" : ""));
        item.type = "button";
        item.setAttribute("role", "menuitem");
        item.append(el("span", "num", "第 " + r.step + " 次" + (i === reqs.length - 1 ? " · 最新" : "")), el("span", "num", ctxK(Math.max(0, r.prompt - r.tools))));
        item.addEventListener("click", (ev) => {
          ev.stopPropagation();
          state.ctxStep = i === reqs.length - 1 ? -1 : i;
          state.ctxMenu = false;
          state.ctxRaw = "";
          paintPanel();
        });
        menu.append(item);
      }
      head.append(menu);
    }
    return head;
  }

  // ctxDetails is the four sections behind the legend. Steps of an older
  // request are the first turn_steps steps of the newest one: within one wake
  // a turn only grows. The other sections always describe the newest request.
  function ctxDetails(detail, req) {
    const box = el("div", "ctx-dt");
    const skip = detail.skipped_steps || 0;
    const upto = Math.min(req.turn_steps || 0, skip + (detail.steps || []).length);
    const steps = (detail.steps || []).slice(0, Math.max(0, upto - skip)).map((s, i) => ({
      id: String(skip + i), code: s.tool, text: s.what || (s.tool ? "" : "回复"), n: s.tokens, bad: s.fail, raw: s.preview,
    })).reverse();
    ctxSection(box, "turn", "本轮", upto + " 步", steps, skip > 0 && upto > skip ? "前 " + skip + " 步没有留" : "");
    const turns = (detail.turns || []).map((t, i) => ({ id: String(i), text: t.ask || "（没有开头的话）", n: t.tokens })).reverse();
    const earlier = detail.earlier_turns || 0;
    if (turns.length + earlier) ctxSection(box, "hist", "早先对话", (turns.length + earlier) + " 轮", turns, earlier ? "更早的 " + earlier + " 轮共 " + ctxK(detail.earlier_tokens) : "");
    const blocks = (detail.blocks || []).map((b, i) => ({ id: String(i), text: "seq " + (b.from || "?") + "–" + (b.to || "?"), n: b.tokens, raw: b.preview }));
    if (blocks.length) ctxSection(box, "block", "摘要块", String(blocks.length), blocks, "");
    const tools = (detail.tool_defs || []).map((t, i) => ({ id: String(i), code: t.name, text: "", n: t.tokens }));
    if (tools.length) ctxSection(box, "tools", "工具", String(tools.length), tools, "");
    return box;
  }

  function ctxSection(box, key, label, count, rows, note) {
    const open = !!state.ctxOpen[key];
    const head = el("button", "ctx-ds" + (open ? " open" : ""));
    head.type = "button";
    head.setAttribute("aria-expanded", open ? "true" : "false");
    head.append(icon("chevron", "ctx-ds-i"), el("span", "", label), el("span", "ctx-ds-v num", count));
    head.addEventListener("click", () => {
      state.ctxOpen[key] = !open;
      paintPanel();
    });
    box.append(head);
    if (!open) return;
    const list = el("div", "ctx-rows");
    for (const r of rows) {
      const id = key + ":" + r.id;
      const row = el("button", "ctx-r" + (r.n >= 1000 ? " big" : ""));
      row.type = "button";
      const name = el("span", "ctx-r-k");
      if (r.code) name.append(el("code", "", r.code));
      if (r.text) name.append(document.createTextNode(r.text));
      if (r.bad) name.append(el("span", "ctx-bad", "失败"));
      row.append(name, el("span", "ctx-r-v num", ctxK(r.n)));
      if (r.raw) {
        row.addEventListener("click", () => {
          state.ctxRaw = state.ctxRaw === id ? "" : id;
          paintPanel();
        });
      } else {
        row.classList.add("flat");
      }
      list.append(row);
      if (state.ctxRaw === id && r.raw) list.append(el("pre", "ctx-raw", r.raw));
    }
    if (!rows.length) list.append(el("div", "ctx-note", "还没有"));
    if (note) list.append(el("div", "ctx-note", note));
    box.append(list);
  }

  // A click anywhere else closes the request menu, the way the other menus close.
  document.addEventListener("click", (ev) => {
    if (!state.ctxMenu || ev.target.closest(".ctx-hd")) return;
    state.ctxMenu = false;
    if (state.tab === "context") paintPanel();
  });

  function paintContextBar() {
    const host = $("ctxbar");
    host.replaceChildren();
    const snap = state.ctxSnap;
    const req = snap?.requests?.at(-1);
    host.hidden = !state.sel || state.sel.type !== "bot" || state.tab === "context" || !req || !(snap.ceiling > 0);
    if (host.hidden) return;
    const used = Math.max(0, req.prompt || 0), hard = snap.ceiling;
    const bar = el("button", "ctxbar");
    bar.type = "button";
    bar.setAttribute("aria-label", "查看上下文");
    setTip(bar, "上下文");
    bar.append(icon("gauge"));
    const label = el("span");
    label.append(el("b", "", Math.round(used * 100 / hard) + "%"), document.createTextNode(" 已用"));
    const band = el("div", "ubar");
    const sums = ctxSums(req);
    let room = 100;
    for (const [key] of CTX_BANDS) {
      if (!sums[key]) continue;
      const seg = el("i", "ctx-c-" + key);
      const pct = Math.min(room, Math.max(0, sums[key] * 100 / hard));
      seg.style.width = pct + "%";
      if (key === "tools") seg.style.marginLeft = "auto";
      room -= pct;
      band.append(seg);
    }
    if (snap.target > 0 && snap.target < hard) {
      const tick = el("span", "soft");
      tick.style.left = (snap.target * 100 / hard) + "%";
      band.append(tick);
    }
    bar.append(label, band, el("span", "mono", ctxK(used) + " / " + ctxK(hard)));
    bar.addEventListener("click", () => { state.tab = "context"; paintPanel(); });
    host.append(bar);
  }

  function styleChips(data) {
    const style = data.style || {};
    const bits = [];
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
    if (!facts.length) wrap.append(panelEmpty("memory", "还没有记忆", "聊着聊着，它会记下关于你的事和你们的约定。"));
    for (const f of facts) {
      const card = el("div", "lib-card");
      card.append(el("div", "k", scopeLabel(f.scope)), el("div", "v", f.text || ""));
      wrap.append(card);
    }
    return wrap;
  }

  function computerView(data) {
    const wrap = document.createDocumentFragment();
    if (!(data.routines || []).length && !(data.agents || []).length) {
      wrap.append(panelEmpty("tasks", "没有任务", "让它「每天 8 点提醒我」，这里就会有一个例程。"));
      return wrap;
    }
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
    const running = agents.filter((ag) => ["running", "awaiting", "queued"].includes(ag.status));
    const finished = agents.filter((ag) => ["done", "failed", "cancelled", "canceled", "interrupted", "denied", "declined"].includes(ag.status));
    if (!running.length) tasks.append(el("p", "hint", "没有进行中的任务"));
    for (const ag of running) {
      const card = el("div", "task");
      const top = el("div", "task-top");
      const awaiting = taskAwaiting(ag, data.name);
      if (awaiting) top.append(approvalTaskButton(data.name, ag.id));
      else if (ag.status === "queued") top.append(el("span", "", "已排队"));
      if (top.childNodes.length) card.append(top);
      card.append(el("p", "task-text", clip(plainText(displayText(ag.task || ag.id)) || "子任务", 140)));
      const row = el("div", "row");
      const field = document.createElement("input");
      field.placeholder = "消息";
      field.maxLength = 4000;
      const go = el("button", "secondary", "引导");
      go.type = "button";
      go.disabled = true;
      field.addEventListener("input", () => { go.disabled = !field.value.trim(); });
      go.addEventListener("click", () => controlAgent(ag.id, "steer", field.value));
      const stop = el("button", "secondary", "停止");
      stop.type = "button";
      confirmTaskStop(stop, () => controlAgent(ag.id, "cancel", ""));
      if (!awaiting) row.append(field, go);
      row.append(stop);
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
      let openDetail = null;
      for (const ag of finished) {
        const row = el("button", "recent-task");
        row.type = "button";
        row.setAttribute("aria-expanded", "false");
        row.append(el("span", "recent-title", clip(plainText(displayText(ag.task || ag.id)) || "子任务", 80)),
          el("span", ag.status === "failed" ? "recent-status failed" : "recent-status",
            taskStatus(ag.status)));
        row.addEventListener("click", () => {
          const wasOpen = openDetail && openDetail.row === row;
          if (openDetail) {
            openDetail.row.setAttribute("aria-expanded", "false");
            openDetail.drawer.remove();
            openDetail = null;
          }
          if (wasOpen) return;
          const drawer = el("div", "routine-drawer recent-detail");
          drawer.append(el("div", "routine-prompt", plainText(displayText(ag.task || ag.id)) || "子任务"),
            el("div", "routine-source", "状态：" + (taskStatus(ag.status))));
          if (ag.finished_at) drawer.append(el("div", "routine-source", "结束时间：" + new Date(ag.finished_at).toLocaleString("zh-CN")));
          // Red only for an actual error text; a missing summary is just gray.
          if (ag.summary) {
            const shown = ag.status === "done" ? plainText(ag.summary) : zhError(ag.summary);
            drawer.append(el("div", ag.status === "failed" ? "routine-prompt failed" : "routine-prompt", shown));
            errorDetails(drawer, ag.summary, shown);
          } else drawer.append(el("div", "routine-source", ag.status === "failed" ? "没有留下失败原因" : "没有留下结果摘要。"));
          row.after(drawer);
          row.setAttribute("aria-expanded", "true");
          openDetail = { row, drawer };
        });
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
      const [data, snap] = await Promise.all([
        fetchBotProfile(selection.id),
        api("/api/bots/" + encodeURIComponent(selection.id) + "/context").catch(() => ({ requests: [] })),
      ]);
      await loadModelCatalog();
      if (state.sel !== selection || request !== state.panelLoad) return;
      state.panelData = data;
      state.ctxSnap = snap;
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

  let profileReturnFocus = null;

  const AVATAR_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
  const AVATAR_MAX = 1024 * 1024;
  const AVATAR_BIG = "没换成：图片超过 1 MB。换一张小一点的，或者先裁一下。";
  const AVATAR_KIND = "没换成：只能用 PNG、JPEG、GIF、WebP。";
  const avatarURLOf = (name) => "/api/bots/" + encodeURIComponent(name) + "/avatar";

  // The same limits the server keeps, checked before sending so a wrong file
  // never costs a round trip. null means "fine".
  function avatarFileProblem(file) {
    if (file.size > AVATAR_MAX) return AVATAR_BIG;
    const type = String(file.type || "").toLowerCase();
    if (AVATAR_TYPES.indexOf(type) < 0 && !/\.(png|jpe?g|gif|webp)$/i.test(file.name || "")) return AVATAR_KIND;
    return null;
  }

  // After sending: the server's own words tell size from type. The type branch
  // stays rare on purpose: the server sniffs bytes, so nearly any file passes
  // as some image, and a plain refusal there is almost always the size.
  function avatarUploadProblem(e) {
    const status = e && e.status, msg = String((e && e.message) || "");
    if (status === 413 || /\btoo large\b|1 ?MB|1048576/i.test(msg)) return AVATAR_BIG;
    if (/\btype\b|unsupported/i.test(msg)) return AVATAR_KIND;
    // "bad avatar" is the multipart reader giving up past its 2 MiB cap.
    if (status === 400 && /bad avatar/i.test(msg)) return AVATAR_BIG;
    return "没换成：" + visibleError(e);
  }

  // 换一张: the current portrait, five fresh seeds for this batch, the glyph
  // and an upload. A pick applies at once; nothing waits for 保存.
  function avatarSection(name) {
    const pickOf = (b) => ({ avatar: (b && b.avatar) || "generated", seed: (b && b.avatar_seed) || name });
    let picked = pickOf(state.panelData || (state.bots || []).find((b) => b.name === name));
    let batch = 0, busy = false;
    const root = el("div", "avatar-block");
    // A failed change leaves the avatar as it was and says why, right under it.
    const problem = el("p", "avatar-note err", "");
    problem.setAttribute("role", "alert");
    const fail = (text) => { problem.textContent = text; paint(); };
    const row = el("div", "avatar-row");
    const grid = el("div", "avatar-grid");
    const file = document.createElement("input");
    file.type = "file";
    file.accept = AVATAR_TYPES.join(",");
    file.hidden = true;
    const clear = el("button", "icon-btn");
    clear.type = "button";
    clear.setAttribute("aria-label", "清除上传的图，用回生成的");
    setTip(clear, "清除上传的图，用回生成的");
    clear.append(icon("undo"));
    const again = el("button", "icon-btn");
    again.type = "button";
    again.setAttribute("aria-label", "换一批");
    setTip(again, "换一批");
    again.append(icon("refresh"));

    const big = () => (picked.avatar === "custom"
      ? avatar(name, { url: avatarURLOf(name) + "?v=" + Date.now(), seed: picked.seed }, "xl")
      : avatar(name, { seed: picked.seed }, "xl"));
    const pick = (label, make, on, onPick) => {
      const btn = el("button", "avatar-pick");
      btn.type = "button";
      btn.setAttribute("aria-label", label);
      btn.setAttribute("aria-pressed", String(!!on));
      btn.append(make());
      btn.addEventListener("click", onPick);
      return btn;
    };

    function paint() {
      const custom = picked.avatar === "custom";
      root.replaceChildren(big(), el("span", "avatar-name", name));
      if (problem.textContent) root.append(problem);
      row.replaceChildren(el("span", "muted", "换一张"));
      if (custom) row.append(clear);
      row.append(again);
      root.append(row, grid, file);
      grid.replaceChildren();
      const isGlyph = !custom && picked.seed === "glyph";
      const seeds = [isGlyph ? name : picked.seed];
      for (let i = 1; i <= 5; i++) seeds.push(name + "#" + (5 * batch + i));
      for (const seed of seeds) {
        grid.append(pick(!custom && seed === picked.seed ? "当前头像" : "换一张",
          () => avatar(name, { seed: seed }, "pick"), !custom && seed === picked.seed, () => apply(seed)));
      }
      const mark = el("span", "avatar pick glyph");
      const svg = glyphSVG(name);
      if (svg) mark.append(svg);
      grid.append(pick("几何图形", () => mark, isGlyph, () => apply("glyph")));
      const up = el("button", "avatar-pick up");
      up.type = "button";
      up.setAttribute("aria-label", "上传图片");
      setTip(up, "上传图片");
      up.append(icon("imageUp"));
      up.addEventListener("click", () => file.click());
      grid.append(up);
    }

    // After the server took it, every list and the panel show the new face.
    async function settled(fallback) {
      await refreshLists();
      await loadPanel();
      picked = pickOf(state.panelData || (state.bots || []).find((b) => b.name === name) || fallback);
      paint();
    }

    async function apply(seed) {
      if (busy) return;
      busy = true;
      problem.textContent = "";
      try {
        await api("/api/bots/" + encodeURIComponent(name), { method: "PATCH", body: { avatar_seed: seed } });
        picked = { avatar: "generated", seed: seed };
        await settled(picked);
      } catch (e) {
        fail("没换成：" + visibleError(e));
      } finally { busy = false; }
    }

    again.addEventListener("click", () => { batch += 1; paint(); });
    clear.addEventListener("click", async () => {
      if (busy) return;
      busy = true;
      problem.textContent = "";
      try {
        const res = await api(avatarURLOf(name), { method: "DELETE" });
        picked = { avatar: "generated", seed: (res && res.avatar_seed) || name };
        await settled(picked);
      } catch (e) {
        fail("没换成：" + visibleError(e));
      } finally { busy = false; }
    });
    file.addEventListener("change", async () => {
      const f = file.files && file.files[0];
      file.value = "";
      if (!f || busy) return;
      const bad = avatarFileProblem(f);
      if (bad) { fail(bad); return; }
      problem.textContent = "";
      busy = true;
      const url = URL.createObjectURL(f);
      const note = el("p", "avatar-note", "正在上传 " + f.name);
      const overlay = el("span", "avatar-spin");
      overlay.setAttribute("aria-hidden", "true");
      overlay.append(icon("spinner"));
      const preview = avatar(name, null, "xl");
      const img = document.createElement("img");
      img.alt = "";
      img.src = url;
      preview.replaceChildren(img, overlay);
      root.replaceChildren(preview, el("span", "avatar-name", name), row, note, grid, file);
      try {
        await uploadAvatar(name, f);
        picked = { avatar: "custom", seed: picked.seed };
        await settled(picked);
      } catch (e) {
        fail(avatarUploadProblem(e));
      } finally {
        URL.revokeObjectURL(url);
        busy = false;
      }
    });
    paint();
    return root;
  }

  function profileForm(data) {
    const wrap = document.createElement("div");
    const head = avatarSection(data.name);
    const style = data.style || {};
    const name = textInput(data.name, 64);
    name.dataset.profileFocus = "true";
    const description = textInput(data.description, 160);
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
    const section = (title, ...fields) => {
      const group = el("section", "profile-section");
      group.append(el("h3", "", title), ...fields);
      return group;
    };
    wrap.append(
      section("基本", head, labeled("标识", name), labeled("简介", description)),
      section("说话方式", labeled("语言", language)),
      section("免打扰", labeled("开始", quietStart), labeled("结束", quietEnd)),
      section("人设", labeled("人设", persona)),
      err,
    );
    const showErr = (e, fallback) => { err.hidden = false; err.textContent = visibleError(e, fallback); };
    modalForm.onsubmit = async (ev) => {
      ev.preventDefault();
      err.hidden = true;
      const body = {
        name: name.value.trim(),
        description: description.value.trim(),
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
    profileReturnFocus = document.activeElement;
    hideTip();
    $("modal-title").textContent = "编辑资料";
    modal.classList.add("profile-drawer");
    modalForm.classList.add("profile-modal");
    modalForm.setAttribute("role", "dialog");
    modalForm.setAttribute("aria-modal", "true");
    modalForm.setAttribute("aria-labelledby", "modal-title");
    $("modal-ok").className = "primary";
    $("modal-ok").disabled = false;
    $("modal-cancel").textContent = "取消";
    modalFields.replaceChildren(profileForm(data));
    modalError.hidden = true;
    modal.hidden = false;
    modalFields.scrollTop = 0;
    modalFields.querySelector("[data-profile-focus]").focus();
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
    if (!res.ok) { const e = new Error(payload.error || "头像上传失败"); e.status = res.status; throw e; }
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
    if (kind === "steer" && !String(text || "").trim()) return;
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
    // The panel is a drawer over the card at every width (#243), so it always
    // covers part of the conversation: narrow windows get a scrim to click
    // away, wide ones keep the thread readable and close with 详情 or Escape.
    $("panel-scrim").hidden = !(window.innerWidth < 960 && panel.classList.contains("open") && !drawer);
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
    const card = pendingApprovalCard({
      tool: payload.tool || payload.name || "",
      title: payload.title || "",
      subtitle: payload.subtitle || "",
      summary: payload.summary || "",
      command: payload.command || "",
      kind: payload.kind || "",
      deadline: payload.deadline || payload.expires || "",
      decide: async (ok) => {
        if (!state.sel || state.sel.type !== "bot") throw new Error("没有选中的 Bot");
        await api("/api/bots/" + encodeURIComponent(state.sel.id) + "/approvals/" + encodeURIComponent(payload.id || ""), {
          method: "POST",
          body: { allow: ok },
        });
      },
    });
    card.dataset.branch = payload.branch || payload.background || "";
    return card;
  }

  // How many owner messages in the loaded transcript carry exactly this text
  // (raw content, so image links and blank lines do not change the match).
  function ownerSends(text) {
    // Owner messages only: timer, webhook and other untrusted events never count.
    return (state.messages || []).filter((m) => m && m.role === "user" && !unwrap(m.content || "").untrusted &&
      String(m.content || "").replace(/\r\n/g, "\n").trim() === text).length;
  }

  function restoreSendFeedback(inner) {
    // A queued send stays until the transcript gains one more copy of its text
    // than it had when the send was queued; each copy resolves one queued row.
    const queuedRecorded = (row) => {
      if (!(state.sel && state.sel.type === "bot") || ownerSends(row.dataset.sendText) <= row.baseline) return false;
      for (const other of state.pendingSends) {
        if (other !== row && (other.queued || other.keepFailed) && other.dataset.sendText === row.dataset.sendText) other.baseline++;
      }
      return true;
    };
    state.pendingSends = state.pendingSends.filter((row) => !row.accepted || ((row.queued || row.keepFailed) && !queuedRecorded(row)));
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

  // The last row on screen, so a live reply or send groups against it the same
  // way the next full render will (groupStart).
  function lastRow(inner) {
    // The last thing on screen, not the last message: an event line or card
    // after it ends the group, as it does in renderMessages.
    const last = inner.lastElementChild;
    return last && last.classList.contains("msg") ? last : null;
  }

  // Marks a row appended live as a group continuation of the row above it,
  // using the same helper as the renderers.
  function markLiveGroup(inner, row) {
    const last = lastRow(inner);
    // Tool and event lines between turns break a group, as they do on render.
    if (!last || !last.dataset.sender || groupStart({ time: last.dataset.time }, { time: row.dataset.time }, last.dataset.sender, row.dataset.sender)) return;
    markContinuation(row, row.dataset.time);
  }

  function markContinuation(row, time) {
    row.classList.add("cont");
    row.querySelector(".who")?.remove();
  }

  function liveBubble() {

    const inner = thread.querySelector(".thread-inner") || thread;
    const empty = inner.querySelector(".empty");
    if (empty) empty.remove();
    const who = state.sel ? state.sel.id : "Bot";
    const now = new Date().toISOString();
    const row = el("div", "msg bot live");
    row.dataset.sender = "bot:" + who;
    row.dataset.time = now;
    const body = el("div", "body");
    const prose = el("div", "prose bot-bubble");
    const dots = el("span", "typing-dots");
    dots.setAttribute("aria-label", "正在回复");
    for (let i = 0; i < 3; i++) dots.append(el("i", ""));
    body.append(prose, dots);
    row.append(body);
    markLiveGroup(inner, row);
    addBubbleTime(row, now);
    addTimeDivider(inner, row);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    return prose;
  }

  async function readSSE(res, selection, prose, onQueued) {
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
        if (event === "queued") { onQueued(); continue; }
        if (!data) continue;
        let payload = {};
        try { payload = JSON.parse(data); } catch (e) { continue; }
        if (payload.status === "queued") { onQueued(); continue; }
        const host = prose.closest(".body") || prose.parentElement;
        if (event === "delta" && payload.text) {
          const status = host && host.querySelector(".live-status");
          if (status) status.remove();
          host.querySelector(".typing-dots")?.remove();
          acc += payload.text;
          prose.replaceChildren();
          appendMarkdown(prose, displayText(acc));
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "tool") {
          const who = state.sel && state.sel.id;
          if (host) {
            setWorking(host, payload.name || "");
            // The live summary has no recipient for message_bot; the transcript
            // re-render after the turn draws its grouped event line.
            const phrase = payload.name === "message_bot" ? null : peerPhrase(payload.name || "", { text: payload.summary || "" }, who);
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
          throw new Error(payload.error || payload.message || "回复出错了");
        }
      }
    }
  }

  function optimisticMessage(text, images) {
    let inner = thread.querySelector(".thread-inner:not(.thread-skeleton)");
    if (!inner) {
      inner = el("div", "thread-inner");
      thread.replaceChildren(inner);
    }
    inner.querySelector(".empty")?.remove();
    const row = el("div", "msg user optimistic");
    const body = el("div", "bubble-col");
    row.dataset.sender = "user";
    row.dataset.time = new Date().toISOString();
    if (text) { const bubble = el("div", "bubble"); appendRich(bubble, text); body.append(bubble); }
    // The picture is already on the server by the time a send starts, so the
    // optimistic row points at it directly. Nothing local to revoke, and the
    // row after the repaint looks exactly the same.
    if (images && images.length) body.append(gallery(pastedURLs(state.sel, images)));
    body.append(el("div", "send-state", "发送中"));
    row.append(body);
    markLiveGroup(inner, row);
    addBubbleTime(row, row.dataset.time);
    addTimeDivider(inner, row);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    state.pendingSends.push(row);
    return row;
  }

  async function sendText(text, pending, images) {
    text = (text || "").trim();
    images = images || [];
    // A message sent while a reply is streaming interrupts it: the server forks the
    // running turn to the background and answers this one at once.
    if ((!text && !images.length) || !state.sel) return;
    const selection = state.sel;
    ++state.threadLoad;
    if (!pending) {
      // Resending a failed text from the composer replaces its failed row.
      state.pendingSends = state.pendingSends.filter((row) => {
        const stale = !row.accepted && row.querySelector(".send-failed") && row.dataset.sendText === text;
        if (stale) row.remove();
        return !stale;
      });
    }
    pending = pending || optimisticMessage(text, images);
    pending.dataset.sendText = text;
    pending.baseline = ownerSends(text);
    const marker = pending.querySelector(".send-state");
    marker.className = "send-state";
    marker.replaceChildren(document.createTextNode("发送中"));
    announceSend("发送中");
    const prose = selection.type === "bot" ? liveBubble() : null;
    const live = prose?.closest(".msg.live");
    const markQueued = () => {
      if (!state.pendingSends.includes(pending)) state.pendingSends.push(pending);
      pending.queued = true;
      pending.keepFailed = false;
      pending.querySelector(".send-state")?.remove();
      pending.querySelector(".bubble-col")?.append(el("div", "send-state", "已排队"));
      announceSend("已排队");
    };
    let accepted = false;
    const markSent = () => {
      accepted = true;
      pending.accepted = true;
      if (state.sel === selection) state.pendingSends = state.pendingSends.filter((row) => {
        // Only a failed copy of this same text is resolved by this send.
        if (row !== pending && !row.keepFailed && row.querySelector(".send-failed") && row.dataset.sendText === text) {
          row.remove();
          return false;
        }
        return true;
      });
      pending.classList.remove("optimistic");
      marker.remove();
      // An older 回复出错了 line is resolved by the next accepted send.
      if (state.sel === selection) {
        state.replyErrors.forEach((row) => row.remove());
        state.replyErrors = [];
      }
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
      if (images.length) payload.images = images;
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
        if (data.status === "queued") {
          markQueued();
        }
        setStatus(data.status === "queued" ? "已排队" : "");
      } else if (prose) {
        await readSSE(res, selection, prose, markQueued);
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
      if (!accepted && !authenticated) {
        const key = "xbot-draft:" + (selection.type === "group" ? "group:" : "") + selection.id;
        try { sessionStorage.setItem(key, text); } catch (err) {}
      }
      if (state.sel !== selection) return;
      setStatus("");
      if (accepted) {
        if (pending.queued) {
          live?.remove();
          settle();
          state.sig = "";
          await loadThread();
          return;
        }
        if (!state.pendingSends.includes(pending)) state.pendingSends.push(pending);
        pending.keepFailed = true;
        const error = el("div", "msg bot");
        const errorBody = el("div", "body");
        const errorText = el("div", "prose bot-bubble reply-error", "回复出错了：" + zhError(e.message));
        errorDetails(errorText, e.message, zhError(e.message));
        errorBody.append(errorText);
        error.append(errorBody);
        live?.replaceWith(error);
        if (!live) thread.querySelector(".thread-inner")?.append(error);
        state.replyErrors.push(error);
        announceSend("回复出错了");
        settle();
        state.sig = "";
        await loadThread();
        if (state.sel !== selection) return;
        // The transcript already holds this message: it did arrive, only the reply failed.
        if (!state.pendingSends.includes(pending)) return;
      }
      live?.remove();
      if (!marker.isConnected) pending.querySelector(".bubble-col")?.append(marker);
      marker.className = "send-state send-failed";
      // A picture cannot go back into the textarea, but its words can: the
      // send cleared the input, so the text is restored whether or not a
      // picture came with it, and the row says what the picture needs.
      const pictureNote = images.length ? "；图片要重新贴一次" : "";
      if (!input.value.trim() && text) {
        input.value = text;
        marker.replaceChildren(el("span", "", (accepted ? "没送到，原文还在输入框里" : "没发出去，原文还在输入框里") + pictureNote));
        announceSend("没发出去，原文还在输入框里" + (images.length ? "，图片要重新贴一次" : ""));
      } else if (!text && images.length) {
        marker.replaceChildren(el("span", "", accepted ? "没送到；图片要重新贴一次" : "没发出去；图片要重新贴一次"));
        announceSend("没发出去，图片要重新贴一次");
      } else {
        // A newer draft is in the composer: keep it, and let the owner bring this text back.
        const back = el("button", "secondary", "放回输入框");
        back.type = "button";
        back.addEventListener("click", () => {
          input.value = input.value.trim() ? input.value.replace(/\s*$/, "\n") + text : text;
          syncComposer();
          input.focus();
        });
        marker.replaceChildren(el("span", "", (accepted ? "没送到" : "没发出去") + pictureNote), back);
        announceSend("没发出去，可以放回输入框" + (images.length ? "，图片要重新贴一次" : ""));
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
    modal.classList.remove("profile-drawer");
    modalForm.classList.remove("profile-modal");
    modalForm.removeAttribute("role");
    modalForm.removeAttribute("aria-modal");
    modalForm.removeAttribute("aria-labelledby");
    if (profileReturnFocus) {
      // The action row is redrawn; return to its live 编辑资料 button instead.
      const acts = $("panel-actions");
      const target = profileReturnFocus.closest("#panel-actions") || !profileReturnFocus.isConnected
        ? acts.querySelector(".panel-act:not(.danger)") || $("info") : profileReturnFocus;
      profileReturnFocus = null;
      target.focus({ preventScroll: true });
    }
    modalForm.querySelector(".profile-templates")?.remove();
    modalError.hidden = true;
    modalError.textContent = "";
    modalForm.onsubmit = null;
    $("modal-ok").hidden = false;
    $("modal-ok").className = "primary";
    $("modal-cancel").textContent = "取消";
  }

  function openModal(title, fields, onOk, okLabel, after) {
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
    if (after) after(inputs);
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

  function openNewBot() {
    // The portrait follows the typed name until 「换一个」 or an upload pins a seed.
    const av = { seed: "", follow: true, file: null };
    openModal("新建 Bot", [
      { name: "name", label: "标识", placeholder: "ada" },
      { name: "persona", label: "人设", multiline: true },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!name) throw new Error("需要一个名字");
      const body = { name: name, persona: (v.persona || "").trim() };
      if (!av.follow) body.avatar_seed = av.seed;
      await api("/api/bots", { method: "POST", body: body });
      await refreshLists();
      await selectBot(name);
      if (av.file) {
        try { await uploadAvatar(name, av.file); await refreshLists(); await loadPanel(); }
        catch (e) { setStatus(avatarUploadProblem(e), true); }
      }
    }, null, (inputs) => {
      const name = inputs.name;
      const file = document.createElement("input");
      file.type = "file";
      file.accept = AVATAR_TYPES.join(",");
      file.hidden = true;
      const row = el("div", "newbot-avatar");
      let shot = "";
      const show = () => {
        const seed = name.value.trim() || "XBot";
        const face = av.file ? avatar(seed, { url: shot }, "xl") : avatar(seed, { seed: av.follow ? seed : av.seed }, "xl");
        const old = row.querySelector(".avatar");
        if (old) old.replaceWith(face);
        else row.prepend(face);
      };
      const again = el("button", "icon-btn");
      again.type = "button";
      again.setAttribute("aria-label", "换一个");
      setTip(again, "换一个");
      again.append(icon("refresh"));
      again.addEventListener("click", () => {
        const base = name.value.trim() || "XBot";
        const k = /#(\d+)$/.exec(av.seed);
        const n = av.seed.startsWith(base + "#") && k ? Number(k[1]) + 1 : 1;
        if (shot) URL.revokeObjectURL(shot);
        shot = "";
        av.file = null;
        av.follow = false;
        av.seed = base + "#" + n;
        show();
      });
      const up = el("button", "icon-btn");
      up.type = "button";
      up.setAttribute("aria-label", "上传图片");
      setTip(up, "上传图片");
      up.append(icon("imageUp"));
      up.addEventListener("click", () => file.click());
      file.addEventListener("change", () => {
        const f = file.files && file.files[0];
        file.value = "";
        if (!f) return;
        const bad = avatarFileProblem(f);
        if (bad) { modalError.hidden = false; modalError.textContent = bad; return; }
        modalError.hidden = true;
        if (shot) URL.revokeObjectURL(shot);
        shot = URL.createObjectURL(f);
        av.file = f;
        av.follow = false;
        show();
      });
      row.append(again, up, file);
      const note = el("p", "avatar-note");
      note.append(icon("info"), el("span", "", "头像跟着名字生成，改名字就换一张；点「换一个」就不再跟着名字变。"));
      modalFields.prepend(row, note);
      name.addEventListener("input", () => {
        if (!av.follow) return;
        av.seed = "";
        show();
      });
      show();
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

  // Pictures pasted or dropped into the composer. Each one is uploaded the
  // moment it arrives, so sending a message is the same request as before plus
  // a list of ids; a picture the server refused never gets an id, and the note
  // above the text says why in the server's words.
  const ATTACH_MAX = 4;
  const ATTACH_TYPES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
  let attachSeq = 0;

  function attachState() {
    if (!state.attach) state.attach = [];
    return state.attach;
  }

  function attachNote(text) {
    const note = $("attach-note");
    note.textContent = text || "";
    note.hidden = !text;
    // A refusal can arrive with no thumbnail to sit under, so the bar follows
    // the note as well as the list. It is revealed here, not only in
    // paintAttach, because the caller may not repaint.
    const has = attachState().length > 0 || !!text;
    $("attach-bar").hidden = !has;
    composer.classList.toggle("has-attach", has);
  }

  function attachKey(sel) {
    return sel ? (sel.type === "group" ? "group:" : "") + sel.id : "";
  }

  function paintAttach() {
    const list = $("attach-list");
    const rows = attachState();
    list.replaceChildren();
    for (const row of rows) {
      const cell = el("div", "attach-thumb");
      cell.dataset.id = row.id || "";
      if (row.uploading) cell.classList.add("sending");
      const img = document.createElement("img");
      img.alt = "";
      if (row.url) img.src = row.url;
      cell.append(img);
      const drop = el("button", "attach-drop");
      drop.type = "button";
      drop.setAttribute("aria-label", "去掉这张图");
      drop.append(icon("x"));
      drop.addEventListener("click", (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        removeAttach(row);
      });
      cell.append(drop);
      list.append(cell);
    }
    // The bar stays visible while it has something to say: a refusal has no
    // thumbnail to sit under, and hiding the whole bar would hide the reason.
    const has = rows.length > 0 || !$("attach-note").hidden;
    $("attach-bar").hidden = !has;
    composer.classList.toggle("has-attach", has);
    syncComposer();
  }

  function removeAttach(row) {
    const rows = attachState();
    const at = rows.indexOf(row);
    if (at >= 0) rows.splice(at, 1);
    if (row.url) URL.revokeObjectURL(row.url);
    // With nothing left to explain, an older refusal no longer applies. A
    // caller that has a fresh reason sets it right after this call.
    if (!rows.length) attachNote("");
    paintAttach();
  }

  // clearAttach drops every pending picture. It runs after a send and when the
  // conversation changes, because an id belongs to the bot it was uploaded to.
  function clearAttach() {
    for (const row of attachState()) if (row.url) URL.revokeObjectURL(row.url);
    state.attach = [];
    // Anything still uploading belongs to the run that just ended.
    state.attachBatch = (state.attachBatch || 0) + 1;
    attachNote("");
    paintAttach();
  }

  function attachReady() {
    return attachState().filter((row) => row.id);
  }

  function attachBusy() {
    return attachState().some((row) => row.uploading);
  }

  async function uploadAttach(files) {
    if (!state.sel || state.sel.type !== "bot") {
      attachNote("图片先发到一个 Bot 的对话里，群组还不支持。");
      return;
    }
    // One batch belongs to the conversation it was started in, and to the run
    // of that conversation. The owner may switch bots while the first upload is
    // still in flight, and switching away and back must not let the rest of the
    // batch land in a list that was cleared in between, so a batch number is
    // taken here and the list is abandoned the moment it changes.
    const owner = state.sel;
    const batch = state.attachBatch;
    for (const file of files) {
      if (state.attachBatch !== batch || attachKey(state.sel) !== attachKey(owner)) return;
      const rows = attachState();
      if (rows.length >= ATTACH_MAX) {
        attachNote("一次最多 " + ATTACH_MAX + " 张图。");
        return;
      }
      const type = String(file.type || "").toLowerCase();
      if (ATTACH_TYPES.indexOf(type) < 0 && !/\.(png|jpe?g|gif|webp)$/i.test(file.name || "")) {
        attachNote("只认 PNG、JPEG、GIF、WebP。");
        continue;
      }
      if (file.size > 10 * 1024 * 1024) {
        attachNote("图片超过 10 MB。");
        continue;
      }
      // The server caps the pictures of one message together as well, so a
      // batch that would be refused is stopped here, before any upload.
      const total = rows.reduce((n, r) => n + (r.bytes || 0), 0) + file.size;
      if (total > 10 * 1024 * 1024) {
        attachNote("这几张图加起来超过 10 MB，去掉几张再发。");
        return;
      }
      attachNote("");
      const row = { seq: ++attachSeq, id: "", bytes: file.size, uploading: true, url: URL.createObjectURL(file) };
      rows.push(row);
      paintAttach();
      // stillWanted is the one question every later step asks: is this row
      // still on screen in the conversation the batch started in? A picture
      // the owner removed, a switch to another bot, and a list cleared by a
      // send all make it false, and none of them may be answered by the upload
      // that was already in flight.
      const stillWanted = () =>
        state.attachBatch === batch && attachKey(state.sel) === attachKey(owner) && attachState().indexOf(row) >= 0;
      try {
        const res = await apiFetch("/api/bots/" + encodeURIComponent(owner.id) + "/paste", {
          method: "POST",
          credentials: "same-origin",
          headers: { "Content-Type": file.type || "application/octet-stream" },
          body: file,
        });
        const data = await res.json().catch(() => ({}));
        if (!res.ok) throw new Error(data.error || "这图片没传上来");
        if (!data.image) throw new Error("这图片没传上来");
        if (stillWanted()) {
          row.id = data.image;
          row.uploading = false;
          paintAttach();
        }
      } catch (e) {
        if (stillWanted()) {
          removeAttach(row);
          attachNote(visibleError(e, "这图片没传上来"));
        }
        continue;
      }
    }
  }

  // imagesFrom pulls the pictures out of a paste or a drop, ignoring everything
  // else, so a pasted screenshot never arrives as a broken text paste.
  function imagesFrom(data) {
    if (!data) return [];
    const out = [];
    for (const item of data.items || []) {
      if (item.kind !== "file") continue;
      const file = item.getAsFile();
      if (file && /^image\//i.test(file.type)) out.push(file);
    }
    if (!out.length) {
      for (const file of data.files || []) {
        if (/^image\//i.test(file.type)) out.push(file);
      }
    }
    return out;
  }

  function syncComposer() {
    const hasText = input.value.trim().length > 0;
    const ready = attachReady().length > 0;
    const canSend = hasText || ready;
    composer.classList.toggle("has-text", hasText);
    $("send").disabled = !canSend || attachBusy();
    $("send").classList.toggle("primary", canSend);
    $("send").classList.toggle("secondary", !canSend);
  }

  function closeAccount() {
    $("account-menu").hidden = true;
    $("account").setAttribute("aria-expanded", "false");
    closeConnect(); // its list never outlives the 我 menu
  }

  function closeConnect() {
    $("connect-pop").hidden = true;
    $("account-connect").setAttribute("aria-expanded", "false");
  }

  function closePops() {
    closeMessageMenu();
    closeAccount();
    closeConnect();
    closeNewMenu();
    closePanelMenu();
    closeFolderMenus();
    closeToolPop();
  }

  document.addEventListener("click", (ev) => {
    const inSub = panelSub && panelSub.contains(ev.target);
    if (panelMore && !inSub && !panelMore.contains(ev.target) && !(ev.target.closest && ev.target.closest("#panel-more"))) {
      closePanelMore();
    }
    if (authenticated && subtitleError) setStatus("");
  }, true);
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
      if (!res.ok) throw new Error(res.status === 401 ? "登录码不对，重新复制终端里登录码那一行" : "登录失败，稍后再试");
      state.owner = data.owner || "";
      state.ownerName = data.owner_name || "";
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
  $("account-theme").addEventListener("click", () => {
    closeAccount();
    toggleTheme();
  });

  const SIDE = { min: 240, max: 360, def: 280, snap: 200, rail: 72 };
  function readSide() {
    try {
      const saved = JSON.parse(localStorage.getItem("xbot.side") || "{}");
      return { w: Math.max(SIDE.min, Math.min(SIDE.max, Number(saved.w) || SIDE.def)), rail: !!saved.rail };
    } catch (e) { return { w: SIDE.def, rail: false }; }
  }
  let side = readSide();
  function syncSidebar() {
    const rail = window.innerWidth >= 768 && side.rail;
    sidebar.classList.toggle("rail", rail);
    document.documentElement.classList.toggle("has-rail", rail);
    sidebar.style.setProperty("--side-w", (rail ? SIDE.rail : side.w) + "px");
    $("collapse").replaceChildren(icon("sidebar"), document.createTextNode(rail ? "展开列表" : "收起列表"));
  }
  function saveSide() { localStorage.setItem("xbot.side", JSON.stringify(side)); }
  const sideGrip = $("side-grip");
  let sidePointer = null;
  sideGrip.addEventListener("pointerdown", (ev) => {
    if (ev.button !== 0) return;
    sidePointer = ev.pointerId;
    sideGrip.setPointerCapture(ev.pointerId);
    document.body.classList.add("resizing");
    hideTip();
    ev.preventDefault();
  });
  sideGrip.addEventListener("pointermove", (ev) => {
    if (sidePointer !== ev.pointerId) return;
    const x = ev.clientX - sidebar.getBoundingClientRect().left;
    side = x < SIDE.snap ? { w: side.w, rail: true } : { w: Math.max(SIDE.min, Math.min(SIDE.max, x)), rail: false };
    syncSidebar();
  });
  const endSideResize = (ev) => {
    if (sidePointer !== ev.pointerId) return;
    sidePointer = null;
    document.body.classList.remove("resizing");
    if (sideGrip.hasPointerCapture(ev.pointerId)) sideGrip.releasePointerCapture(ev.pointerId);
    saveSide();
  };
  sideGrip.addEventListener("pointerup", endSideResize);
  sideGrip.addEventListener("pointercancel", endSideResize);
  sideGrip.addEventListener("lostpointercapture", endSideResize);
  sideGrip.addEventListener("dblclick", () => { side = { w: SIDE.def, rail: false }; syncSidebar(); saveSide(); });
  $("rail-new").append(icon("plus"));
  $("rail-new").addEventListener("click", (ev) => { ev.stopPropagation(); toggleNewMenu($("rail-new")); });
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
    side.rail = !side.rail;
    saveSide();
    syncSidebar();
  });

  function rememberPanel(open) {
    state.panelTouched = true;
    try { localStorage.setItem("xbot-panel-open", String(open)); } catch (e) {}
  }
  $("panel-scrim").addEventListener("click", () => {
    closePops();
    rememberPanel(false);
    panelOpen(false);
  });

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
  $("peer-close").addEventListener("click", () => { closePeer(); });

  thread.addEventListener("scroll", () => {
    const peer = state.peer;
    if (!peer || peer.loading || !peer.more || !peer.cursor) return;
    if (thread.scrollTop > 48) return;
    peer.loading = true;
    loadPeer(peer.cursor).finally(() => { if (state.peer) state.peer.loading = false; });
  });

  const isMac = /Mac|iPhone|iPad/.test(navigator.platform || "");
  function panelOpen(open) {
    panel.classList.toggle("open", open);
    $("info").classList.toggle("on", open);
    $("info").setAttribute("aria-expanded", open ? "true" : "false");
    setTip($("info"), (open ? "收起面板" : "展开面板") + (isMac ? "  ⌘\\" : "  Ctrl+\\"));
    syncBackdrop();
  }

  $("info").addEventListener("click", async () => {
    const open = !panel.classList.contains("open");
    rememberPanel(open);
    panelOpen(open);
    if (open && window.innerWidth < 768) sidebar.classList.remove("open");
    if (open) {
      try { await loadPanel(); } catch (e) { setStatus(visibleError(e), true); }
    }
  });

  document.addEventListener("keydown", (ev) => {
    if (ev.key !== "\\" || !(ev.metaKey || ev.ctrlKey) || ev.altKey || ev.shiftKey) return;
    ev.preventDefault();
    const open = !panel.classList.contains("open");
    rememberPanel(open);
    panelOpen(open);
    if (open) loadPanel().catch(() => {});
  });

  // The remote conversation is a full panel at z-index 12; this drawer sits at
  // 30, so it would cover its header and composer. Close it when one opens.
  window.addEventListener("xbot:remote-open", () => {
    if (!panel.classList.contains("open")) return;
    rememberPanel(false);
    panelOpen(false);
  });

  // 连接应用 is a row of the 我 menu; its list opens beside the menu.
  $("account-connect").prepend(icon("link"));
  $("account-connect").append(icon("chevron", "mark"));
  $("account-connect").addEventListener("click", (ev) => {
    ev.stopPropagation();
    const pop = $("connect-pop");
    const open = pop.hidden;
    pop.hidden = !open;
    $("account-connect").setAttribute("aria-expanded", open ? "true" : "false");
    if (!open) return;
    // Beside the menu, bottoms aligned; on a phone, above it.
    const m = $("account-menu").getBoundingClientRect();
    const side = window.innerWidth >= 768 && m.right + 8 + pop.offsetWidth <= window.innerWidth - 8;
    pop.style.left = (side ? m.right + 8 : m.left) + "px";
    pop.style.bottom = (side ? window.innerHeight - m.bottom : window.innerHeight - m.top + 8) + "px";
    pop.style.top = "auto";
    pop.style.maxHeight = Math.max(120, (side ? m.bottom : m.top - 8) - 8) + "px"; // scrolls on short screens
    loadPush().catch(() => {});
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
    if (!text || (host.getAttribute("aria-expanded") === "true" && host !== $("info"))) { hideTip(); return; }
    tipEl.textContent = text;
    tipEl.hidden = false;
    const r = host.getBoundingClientRect();
    const tw = tipEl.offsetWidth;
    const th = tipEl.offsetHeight;
    let left = host === $("info") ? r.right - tw : r.left + (r.width / 2) - (tw / 2);
    left = Math.max(8, Math.min(left, window.innerWidth - tw - 8));
    const right = host.dataset.tipPlacement === "right" && host.closest(".sidebar.rail");
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
    if (!ev.target.closest(".account-wrap")) closeConnect();
    if (!ev.target.closest(".new-menu")) closeNewMenu();
    if (!ev.target.closest(".panel-actions") && !ev.target.closest(".menu-sub")) closePanelMenu();
    if (!ev.target.closest(".folder-head") && !ev.target.closest(".folder-menu")) closeFolderMenus();
    if (!ev.target.closest(".tool-pop") && !ev.target.closest(".card")) closeToolPop();
    if (!ev.target.closest("#notes") && !ev.target.closest("#bell")) {
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

  // Escape consumes one layer per press, topmost first. Device and pairing
  // layers have their own handlers and must see the key before this one.
  function deviceLayerOpen() {
    for (const id of ["devices-modal", "devices-drawer"]) {
      const layer = $(id);
      if (layer && layer.firstChild) return true;
    }
    return false;
  }

  document.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    if (deviceLayerOpen()) return;
    hideTip();
    if (panelSub && !panelSub.hidden) { closePanelSub(); return; }
    if (panelMore) { closePanelMore(); return; }
    if (!lightbox.hidden) { closeShots(); return; }
    if (!modal.hidden) { closeModal(); return; }
    if (stpState.open) { closeStp(); return; }
    if (state.replyTo) { clearQuote(); return; }
    if (!$("account-menu").hidden || !$("connect-pop").hidden) { closePops(); return; }
    if (panel.classList.contains("open")) {
      rememberPanel(false);
      closePops();
      panelOpen(false);
    }
    sidebar.classList.remove("open");
    syncBackdrop();
  });

  modal.addEventListener("keydown", (ev) => {
    if (ev.key !== "Tab" || !modal.classList.contains("profile-drawer")) return;
    const controls = [...modalForm.querySelectorAll("button, input, select, textarea")]
      .filter((node) => !node.disabled && node.getClientRects().length);
    const first = controls[0];
    const last = controls[controls.length - 1];
    if (ev.shiftKey && document.activeElement === first) {
      ev.preventDefault();
      last.focus();
    } else if (!ev.shiftKey && document.activeElement === last) {
      ev.preventDefault();
      first.focus();
    }
  });

  modal.addEventListener("click", (ev) => { if (ev.target === modal) closeModal(); });
  $("modal-cancel").addEventListener("click", closeModal);
  lightbox.addEventListener("click", (ev) => { if (ev.target === lightbox) closeShots(); });

  composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    const text = input.value;
    const images = attachReady().map((row) => row.id);
    if (attachBusy()) { attachNote("图片还在上传，等一下再发。"); return; }
    if ((!text.trim() && !images.length) || !state.sel) return;
    input.value = "";
    input.style.height = "auto";
    clearAttach();
    syncComposer();
    sendText(text, null, images);
  });

  // A screenshot pasted from the clipboard arrives here, not as text. The
  // browser gives the picture in clipboardData; anything else falls through to
  // the textarea's own paste.
  input.addEventListener("paste", (ev) => {
    const files = imagesFrom(ev.clipboardData);
    if (!files.length) return;
    ev.preventDefault();
    uploadAttach(files);
  });

  // Dragging a file anywhere over the chat would otherwise make the browser
  // open it and leave XBot.
  const dropZone = thread.closest("main") || document;
  // During dragenter and dragover the browser hides the dragged files: only
  // the item types are readable. Accepting the drag must happen here anyway,
  // or the browser navigates to the dropped file instead of firing drop.
  const carriesImage = (data) => {
    if (!data) return false;
    const items = [...(data.items || [])];
    if (items.length) return items.some((it) => it.kind === "file" && /^image\//i.test(it.type));
    return [...(data.files || [])].some((f) => /^image\//i.test(f.type));
  };
  for (const type of ["dragenter", "dragover"]) {
    dropZone.addEventListener(type, (ev) => {
      if (!carriesImage(ev.dataTransfer)) return;
      ev.preventDefault();
      composer.classList.add("dropping");
    });
  }
  dropZone.addEventListener("dragleave", (ev) => {
    if (ev.target !== dropZone && dropZone.contains(ev.relatedTarget)) return;
    composer.classList.remove("dropping");
  });
  dropZone.addEventListener("drop", (ev) => {
    if (!ev.dataTransfer) return;
    const files = imagesFrom(ev.dataTransfer);
    composer.classList.remove("dropping");
    if (!files.length) return;
    ev.preventDefault();
    uploadAttach(files);
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
    await refreshUpdate().catch(() => {});
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
    // The panel is a drawer over the conversation now (#243), so it starts
    // closed everywhere: nothing is hidden until the owner asks for it. Only
    // an explicit "true" from a previous session opens it again.
    const wide = preference === "true";
    const was = panel.classList.contains("open");
    panelOpen(wide);
    if (wide && !was) loadPanel().catch(() => {});
  }

  syncSidebar();
  initTheme(); // in the app this also tells the window which theme to use
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
      if (!document.hidden) refreshUpdate().catch(() => {});
      refreshNotes().catch(() => {});
      refreshLists().catch(() => {});
      loadThread().catch(() => {});
    }, 3000);
  }

  // First run of the desktop app: ask for the model key, test it, save it.
  function showSetup(host) {
    login.hidden = true;
    shell.hidden = true;
    $("setup").hidden = false;
    if (host) $("setup-host").textContent = host;
    $("setup-key").focus();
    return new Promise((resolve) => {
      const form = $("setup-form"), input = $("setup-key"), save = $("setup-save"), err = $("setup-error");
      let busy = false;
      input.addEventListener("input", () => { save.disabled = busy || !input.value.trim(); err.hidden = true; });
      form.addEventListener("submit", async (ev) => {
        ev.preventDefault();
        if (busy || !input.value.trim()) return;
        busy = true;
        save.disabled = true;
        save.textContent = "正在测试…";
        try {
          await api("/api/setup/key", { method: "POST", body: { key: input.value.trim() } });
          input.value = "";
          $("setup").hidden = true;
          resolve();
        } catch (e) {
          err.textContent = (e && e.message) || "没保存成功，请重试";
          err.hidden = false;
        } finally {
          busy = false;
          save.disabled = !input.value.trim();
          save.textContent = "测试并保存";
        }
      });
    });
  }

  api("/api/session").then(async (data) => {
    if (!data || !data.authed) { showLogin(); return; }
    state.owner = (data && data.owner) || "";
    state.ownerName = (data && data.owner_name) || "";
    state.version = (data && data.version) || "";
    state.app = !!data.app;
    // The desktop app has no login page to come back to: no 退出 there.
    $("logout").hidden = !!data.app;
    if (data.needs_key) await showSetup(data.key_host || "");
    renderMe();
    showApp();
    await boot();
    startPoll();
  }).catch(() => showLogin());
})();
