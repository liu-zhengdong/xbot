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
  // The product way in is the account (account.js). #login-code keeps the old
  // install-code form reachable for debugging; nothing in the product links to it.
  const legacyLogin = location.hash === "#login-code";
  const sidebar = $("sidebar");
  const backdrop = $("backdrop");
  const thread = $("thread");
  const historyControls = $("history-nav");
  const historyButtons = Object.fromEntries(["older", "newer", "latest", "error", "end"].map(key => [key, $("history-" + key)]));
  const input = $("input");
  // The composer's last rendered text and its IME state; see the composer
  // helpers further down for how they keep the draft plain and linkified.
  // composerEditCaret is the caret captured before an edit mutates the draft.
  let composerTextCache = "";
  let composerComposing = false;
  let composerEditCaret = null;
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
  // plaintext-only keeps paste and drop plain in Chrome, Safari and current
  // Firefox; engines without it throw here and fall back to editable
  // everything, where the paste handler and the re-render keep the draft plain.
  try { document.createElement("div").contentEditable = "plaintext-only"; }
  catch (e) { input.setAttribute("contenteditable", "true"); }
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
    inboxData: null,
    panelTouched: false,
    editOpen: false,
    messages: [],
    traceOpen: {},
    traceOut: {},
    traceThink: {},
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
    tip: "",
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

  let themeMode = "system";
  const systemTheme = window.matchMedia("(prefers-color-scheme: dark)");
  function applyTheme(mode) {
    themeMode = ["light", "dark", "system"].includes(mode) ? mode : "system";
    const theme = themeMode === "system" ? (systemTheme.matches ? "dark" : "light") : themeMode;
    document.documentElement.setAttribute("data-theme", theme);
    $("theme-color")?.setAttribute("content", theme === "dark" ? "#161618" : "#ffffff");
    $("settings-theme").value = themeMode;
    try { localStorage.setItem("xbot-theme", themeMode); } catch (e) {}
    if (inApp) api("/api/app/theme", { method: "POST", body: { theme } }).catch(() => {});
  }

  function initTheme() {
    let mode = "system";
    try { mode = localStorage.getItem("xbot-theme") || "system"; } catch (e) {}
    applyTheme(mode);
  }
  systemTheme.addEventListener("change", () => { if (themeMode === "system") applyTheme("system"); });

  let authenticated = false;

  function zhError(msg) {
    msg = String(msg || "").trim();
    const timeout = msg.match(/request timeout after (\d+)m/i);
    if (timeout) return "模型超时（" + timeout[1] + " 分钟）";
    for (const [pattern, text] of [
      [/responses: empty response/i, EMPTY_REPLY],
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
      const err = new Error("服务出了点问题，请稍后再试。");
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
      signal: opt.signal,
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

  // Task cards require a second click before stopping a running task.
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
      const summary = workText(st.tool_summary) || "在处理";
      text.append(summary);
      line.title = summary;
      line.append(text);
      line.addEventListener("click", (ev) => { ev.stopPropagation(); toggleStp(name, line); });
      sig += summary;
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
      const text = (st && st.todo) || realPreview(st && st.last_message) || (bot ? lineForBot(bot) : "") || "没有消息";
      line.className = "preview";
      line.append(el("span", "", text));
      if (st && st.todo) line.title = st.todo;
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
    if (state.sel && state.sel.type === "bot" && !state.peer) {
      const st = stStatus.map[state.sel.id];
      const host = thread.querySelector(".msg.live .body");
      if (st && host && host.querySelector(".live-status")) setWorking(host, workText(st.tool_summary));
    }
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
      if (busy && !row.classList.contains("pin")) {
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
    await refreshWork();
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
      const t0 = tsOf(steps[0].time) || Date.now();
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
      sl.append(el("b", "", step.tool === "todo" ? "待办" : String(step.tool || "tool")));
      // A "detail · result" summary splits into the mono chip and the gray line.
      const sum = step.tool === "todo" ? (step.state === "done" ? "待办已更新" : step.state === "error" ? "待办更新失败" : "正在更新待办") : String(step.summary || "").trim();
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
    closeSettings(false);
    closeModal();
    closePops();
    closeDrawer();
    closeStp();
    closeShots();
    panel.classList.remove("open");
    $("panel-scrim").hidden = true;
    $("head-chip").classList.remove("on");
    $("head-chip").setAttribute("aria-expanded", "false");
    state.sel = null;
    ++state.threadLoad;
    ++state.panelLoad;
    setStatus("");
    if (!legacyLogin) {
      shell.hidden = true;
      login.hidden = true;
      window.XBotAccount.show("login", expired ? { note: "登录已过期，请重新登录" } : {});
      return;
    }
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
          const hm = String(Number(hour)).padStart(2, "0") + ":" + String(Number(min)).padStart(2, "0");
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
    play: '<path d="m8 5 12 7-12 7Z"/>',
    pause: '<path d="M8 5v14M16 5v14"/>',
    plug: '<path d="M12 22v-5M9 8V2M15 8V2M18 8v5a6 6 0 0 1-12 0V8Z"/>',
    globe: '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20M2 12h20"/>',
    monitor: '<rect width="20" height="14" x="2" y="3" rx="2"/><path d="M8 21h8M12 17v4"/>',
    phone: '<rect width="14" height="20" x="5" y="2" rx="2"/><path d="M12 18h.01"/>',
    settings: '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
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
    "trash-2": '<path d="M3 6h18M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M10 11v6M14 11v6"/>',
    package: '<path d="m7.5 4.27 9 5.15M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16ZM3.3 7 12 12l8.7-5M12 22V12"/>',
    "file-pen": '<path d="M12 22H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h8l6 6v4M14 2v6h6M21.378 14.626a1 1 0 0 0-3.004-3.004l-5.01 5.012a2 2 0 0 0-.506.854l-.837 2.87a.5.5 0 0 0 .62.62l2.87-.837a2 2 0 0 0 .854-.506z"/>',
    send: '<path d="m22 2-7 20-4-9-9-4ZM22 2 11 13"/>',
    trash: '<path d="M3 6h18"/><path d="M8 6V4h8v2"/><path d="M19 6l-1 14H6L5 6"/>',
    maximize: '<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3 13.5 10.5"/><path d="M3 21l7.5-7.5"/>',
    download: '<path d="M12 4v12"/><path d="m7 11 5 5 5-5"/><path d="M5 15v4a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4"/>',
    folder: '<path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    "circle-check": '<circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/>',
    "circle-x": '<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6M9 9l6 6"/>',
    ban: '<circle cx="12" cy="12" r="10"/><path d="m4.9 4.9 14.2 14.2"/>',
    square: '<rect x="3" y="3" width="18" height="18" rx="2"/>',
    "corner-down-right": '<path d="M15 10l5 5-5 5"/><path d="M4 4v7a4 4 0 0 0 4 4h12"/>',
    hourglass: '<path d="M5 22h14M5 2h14M17 22v-4.2a2 2 0 0 0-.6-1.4L12 12l-4.4 4.4a2 2 0 0 0-.6 1.4V22M7 2v4.2a2 2 0 0 0 .6 1.4L12 12l4.4-4.4a2 2 0 0 0 .6-1.4V2"/>',
    "circle-slash": '<circle cx="12" cy="12" r="10"/><path d="m4.929 4.929 14.142 14.142"/>',
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
    } else if (seed === "glyph" && !paint.portrait) {
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

  // Both task rows and message bubbles use this priority order. The caller
  // supplies its real font/available width; filenames are never shortened.
  function compactPath(path, fits) {
    if (fits(path)) return path;
    const sep = path.includes("\\") && !path.includes("/") ? "\\" : "/";
    let root = "", rest = path;
    const drive = /^[A-Za-z]:[/\\]/.exec(path);
    if (drive) { root = path.slice(0, 2) + sep; rest = path.slice(3); }
    else if (/^~[/\\]/.test(path)) { root = "~" + sep; rest = path.slice(2); }
    else if (/^[/\\]/.test(path)) { root = sep; rest = path.slice(1); }
    const parts = rest.split(/[/\\]/).filter(Boolean), file = parts.at(-1) || path;
    const candidate = (head, tail) => root + [...parts.slice(0, head), "…", ...parts.slice(-tail)].join(sep);
    const choices = [];
    if (parts.length > 4) choices.push(candidate(2, 2));
    if (parts.length > 3) choices.push(candidate(2, 1));
    if (parts.length > 2) choices.push(candidate(1, 1));
    if (parts.length > 1) choices.push(candidate(0, 1));
    return choices.find(fits) || file;
  }

  const pathLabels = new Set();
  let pathFrame = 0, pathObserver, pathCanvas;
  function queuePathLayout() {
    if (pathFrame) return;
    pathFrame = requestAnimationFrame(() => {
      pathFrame = 0;
      for (const label of pathLabels) {
        if (!label.isConnected) { pathLabels.delete(label); continue; }
        const style = getComputedStyle(label);
        pathCanvas ||= document.createElement("canvas");
        const ctx = pathCanvas.getContext("2d"); ctx.font = style.font;
        const measure = text => ctx.measureText(text).width;
        let width;
        const card = label.closest(".notable-text.has-path");
        if (card) width = card.clientWidth - card.querySelector(".notable-prefix").getBoundingClientRect().width;
        else {
          const row = label.closest(".msg"), bubble = label.closest(".bubble, .bot-bubble");
          if (!row || !bubble) continue;
          const column = bubble.closest(".bubble-col, .body"), max = column ? getComputedStyle(column).maxWidth : "100%";
          const ratio = max.endsWith("%") ? parseFloat(max) / 100 : 1;
          const bs = getComputedStyle(bubble);
          width = row.clientWidth * ratio - parseFloat(bs.paddingLeft) - parseFloat(bs.paddingRight);
          // Short notices should fit as a sentence. Long paragraphs can wrap
          // naturally; do not reduce their paths merely because of other prose.
          const para = label.closest("p");
          if (para) {
            const walker = document.createTreeWalker(para, NodeFilter.SHOW_TEXT);
            let other = "", n;
            while ((n = walker.nextNode())) if (!n.parentElement.closest(".compact-path")) other += n.textContent;
            const spare = width - measure(other), count = para.querySelectorAll(".compact-path").length;
            if (spare > width / 3) width = spare / count;
          }
        }
        if (!(width > 0)) continue;
        const next = compactPath(label.dataset.path, text => measure(text) <= width);
        if (label.textContent !== next && !taskSelected(label)) label.textContent = next;
      }
    });
  }

  function pathLabel(path, tag = "span") {
    const label = el(tag, "compact-path", path); label.dataset.path = path; label.title = path;
    pathLabels.add(label); queuePathLayout();
    if (!pathObserver && typeof ResizeObserver !== "undefined") {
      pathObserver = new ResizeObserver(queuePathLayout);
      for (const root of document.querySelectorAll(".chat, #panel")) pathObserver.observe(root);
      document.fonts?.ready.then(queuePathLayout);
    }
    return label;
  }

  // One math formula as a span/div holding KaTeX's markup. KaTeX escapes the
  // TeX source itself and its escaped output is the library's supported
  // injection contract (no raw user HTML ever enters the tree any other way);
  // on any failure the original source stays visible and the bubble survives.
  // src is the formula's full source text including its delimiters — kept
  // separate from displayMode, because a sentence-middle $$…$$ renders as an
  // inline span but must still fall back as $$…$$, not with rewritten
  // delimiters (review-1 R5).
  function mathNode(tex, display, src) {
    const full = src === undefined ? (display ? "$$" + tex + "$$" : "$" + tex + "$") : src;
    const box = document.createElement(display ? "div" : "span");
    box.className = display ? "math-block" : "math-inline";
    try {
      if (!window.katex || typeof window.katex.renderToString !== "function") throw new Error("katex missing");
      // throwOnError: TeX that cannot parse must reach the catch above and
      // fall back to the source. With false, KaTeX returns .katex-error
      // markup instead and the original words would never come back.
      box.innerHTML = window.katex.renderToString(tex, { throwOnError: true, displayMode: !!display });
    } catch (e) {
      box.classList.add("math-raw");
      box.textContent = full; // fallback: the words the model sent
    }
    return box;
  }

  function appendPathText(parent, text) {
    const paths = /(?<![^\s，。；：（("'])((?:~\/|\/|[A-Za-z]:\\)[^\s，。；、）)<>"'`]+)/g;
    let last = 0;
    for (const match of text.matchAll(paths)) {
      if (match.index > last) parent.append(document.createTextNode(text.slice(last, match.index)));
      parent.append(pathLabel(match[0])); last = match.index + match[0].length;
    }
    if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
  }

  // Text links (bubbles, composer draft, web cites) carry the site's own
  // /favicon.ico inside the shared link pad. Not a favicon service: those
  // answer unknown hosts with a placeholder globe that loads fine, while a
  // site without the file answers a page that fails as an image. The icon
  // takes no room until it loads; each site's outcome is kept, so a re-render
  // (the composer's, on every key) builds the same DOM and a failed site is
  // never re-fetched.
  const linkIcons = new Map(); // origin -> loaded (true) or failed (false)
  function linkIcon(a) {
    const site = a.origin;
    if (!/^https?:/.test(site) || linkIcons.get(site) === false) return a;
    const img = document.createElement("img");
    img.className = linkIcons.get(site) ? "link-icon ok" : "link-icon";
    img.alt = "";
    img.src = site + "/favicon.ico";
    img.onload = () => { linkIcons.set(site, true); img.classList.add("ok"); };
    img.onerror = () => { linkIcons.set(site, false); img.remove(); };
    a.prepend(img);
    return a;
  }
  window.XBotLinkIcon = linkIcon;

  function appendInline(parent, text) {
    // Math needs its neighbors glued on: `$5 and $6` is prose (the closing
    // side is a space), `$x^2$` is a formula. $$ pairs come first so a single
    // $ never eats their opening.
    const re = /(`[^`]+`)|(\$\$[^$]+\$\$)|(\$[^\s$][^$\n]*?[^\s$]\$|\$[^\s$]\$)|(\*\*[^*]+\*\*)|(\*[^*\n]+\*)|(\[[^\]]+\]\(https?:\/\/[^\s)]+\))|(https?:\/\/[A-Za-z0-9\-._~:?#@!$&()*+,;=%/[\]]*[A-Za-z0-9\-_~#@$&(*+=%/[])/g;
    let last = 0;
    let match;
    while ((match = re.exec(text))) {
      if (match.index > last) appendPathText(parent, text.slice(last, match.index));
      const tok = match[0];
      if (tok.charAt(0) === "`") {
        const code = tok.slice(1, -1);
        parent.append(/^(?:~[/\\]|[/\\]|[A-Za-z]:[/\\])/.test(code) && !/\s--?/.test(code) ? pathLabel(code, "code") : el("code", "", code));
      } else if (tok.startsWith("$$")) {
        // The sentence-middle $$…$$ tradeoff: rendered inline, but the
        // fallback keeps the full source including both $$ delimiters.
        parent.append(mathNode(tok.slice(2, -2), false, tok));
      } else if (tok.charAt(0) === "$") {
        parent.append(mathNode(tok.slice(1, -1), false, tok));
      } else if (tok.startsWith("**")) {
        parent.append(el("strong", "", tok.slice(2, -2)));
      } else if (tok.charAt(0) === "*") {
        parent.append(el("em", "", tok.slice(1, -1)));
      } else {
        // [text](url) or a bare URL; trailing punctuation stays prose.
        const lm = /^\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)$/.exec(tok) || (match[7] && [tok, tok, tok]);
        if (lm) {
          const a = document.createElement("a");
          a.textContent = lm[1];
          a.href = lm[2];
          a.rel = "noopener noreferrer";
          a.target = "_blank";
          parent.append(linkIcon(a));
        } else {
          parent.append(document.createTextNode(tok));
        }
      }
      last = match.index + tok.length;
    }
    if (last < text.length) appendPathText(parent, text.slice(last));
  }

  function appendBlocks(parent, text) {
    const lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
    let para = [];
    let list = null;
    let math = null; // lines gathered between $$ fences, or null outside one
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
    const flushMath = () => {
      if (math === null) return;
      parent.append(mathNode(math.join("\n").replace(/^\n+|\n+$/g, ""), true));
      math = null;
    };
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      if (math !== null) {
        // First closing $$ wins: the rest of the line is prose again and may
        // hold further formulas (lastIndexOf would swallow them into the TeX).
        const end = line.indexOf("$$");
        if (end < 0) { math.push(line); continue; }
        math.push(line.slice(0, end));
        flushMath();
        const rest = line.slice(end + 2);
        if (rest.trim()) para.push(rest);
        continue;
      }
      const open = /^\s*\$\$(.*)$/.exec(line);
      if (open) {
        flushPara(); flushList();
        const body = open[1];
        // First closing $$ ends the formula; whatever follows is prose that
        // may itself contain another formula (…$$ one $$ and $$ two $$).
        const end = body.indexOf("$$");
        if (end >= 0) {
          // $$x^2$$ closed on the opening line; trailing prose goes on as text.
          if (body.slice(0, end).trim()) parent.append(mathNode(body.slice(0, end), true));
          const rest = body.slice(end + 2);
          if (rest.trim()) para.push(rest);
        } else {
          math = body.trim() ? [body] : [];
        }
        continue;
      }
      // Quote: consecutive > lines become one block; one > level is peeled
      // off and the rest renders through the same path, so >> nests and
      // lists, inline marks and math work inside. No > reaches the text.
      if (/^\s{0,3}>/.test(line)) {
        flushPara(); flushList();
        const inner = [];
        while (i < lines.length && /^\s{0,3}>/.test(lines[i])) inner.push(lines[i++].replace(/^\s{0,3}> ?/, ""));
        i--;
        const quote = document.createElement("blockquote");
        appendMarkdown(quote, inner.join("\n"));
        parent.append(quote);
        continue;
      }
      // GFM table: a header row with pipes directly over a |---| row. Cells
      // go through appendInline, so links stay http(s) and text stays text.
      if (line.indexOf("|") >= 0 && i + 1 < lines.length && tableAligns(lines[i + 1])) {
        const head = tableCells(line), aligns = tableAligns(lines[i + 1]);
        if (head.length === aligns.length) {
          flushPara(); flushList();
          const rows = [];
          i += 2;
          // A quote line ends the table even when it holds a pipe.
          while (i < lines.length && lines[i].indexOf("|") >= 0 && lines[i].trim() && !/^\s{0,3}>/.test(lines[i])) rows.push(tableCells(lines[i++]));
          i--;
          parent.append(tableNode(head, aligns, rows));
          continue;
        }
      }
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
    flushMath();
  }

  // A row's cells: outer pipes dropped, split on | outside `code` spans,
  // \| kept as a literal pipe.
  function tableCells(line) {
    let row = line.trim();
    if (row.startsWith("|")) row = row.slice(1);
    if (row.endsWith("|") && !row.endsWith("\\|")) row = row.slice(0, -1);
    const cells = [];
    let cur = "", tick = false;
    for (let i = 0; i < row.length; i++) {
      const ch = row.charAt(i);
      if (ch === "\\" && row.charAt(i + 1) === "|") { cur += "|"; i++; continue; }
      if (ch === "`") tick = !tick;
      if (ch === "|" && !tick) { cells.push(cur.trim()); cur = ""; continue; }
      cur += ch;
    }
    cells.push(cur.trim());
    return cells;
  }

  // The |:---|---:| row's alignments, or null when the line is not one.
  function tableAligns(line) {
    if (line.indexOf("-") < 0 || !/^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/.test(line)) return null;
    if (line.indexOf("|") < 0) return null;
    return tableCells(line).map((c) => c.endsWith(":") ? (c.startsWith(":") ? "center" : "right") : (c.startsWith(":") ? "left" : ""));
  }

  function tableNode(head, aligns, rows) {
    const wrap = el("div", "md-table");
    const table = document.createElement("table");
    const thead = document.createElement("thead"), tbody = document.createElement("tbody");
    const row = (cells, tag) => {
      const tr = document.createElement("tr");
      for (let c = 0; c < aligns.length; c++) {
        const cell = document.createElement(tag);
        if (aligns[c]) cell.style.textAlign = aligns[c];
        appendInline(cell, cells[c] || "");
        tr.append(cell);
      }
      return tr;
    };
    thead.append(row(head, "th"));
    for (const cells of rows) tbody.append(row(cells, "td"));
    table.append(thead);
    if (rows.length) table.append(tbody);
    wrap.append(table);
    return wrap;
  }

  // Per-language token rules for the code highlighter. Each profile names
  // its keywords, literal words, line-comment openers, whether /* */ and
  // triple-quoted strings exist, and which quotes may span lines.
  const codeLangs = (() => {
    const words = (s) => new Set(s.split(" "));
    const cLike = { block: true, line: ["//"], multi: "" };
    const p = {
      go: { ...cLike, multi: "`", kw: words("break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var"), lit: words("true false nil iota string int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr float32 float64 bool byte rune error any make new len cap append copy delete panic recover close") },
      js: { ...cLike, multi: "`", kw: words("async await break case catch class const continue debugger default delete do else export extends finally for from function if import in instanceof let new of return static super switch this throw try typeof var void while with yield as type interface enum implements private public protected readonly declare namespace keyof"), lit: words("true false null undefined NaN Infinity string number boolean any unknown never void object") },
      py: { block: false, line: ["#"], triple: true, multi: "", kw: words("and as assert async await break class continue def del elif else except finally for from global if import in is lambda match case nonlocal not or pass raise return try while with yield"), lit: words("True False None self cls print len range int str float bool list dict set tuple") },
      java: { ...cLike, kw: words("abstract assert break case catch class const continue default do else enum extends final finally for goto if implements import instanceof interface native new package private protected public return static strictfp super switch synchronized this throw throws transient try volatile while var record fun val when object override open data companion"), lit: words("true false null void int long short byte char boolean float double String") },
      c: { ...cLike, kw: words("auto break case const constexpr continue default do else enum extern for goto if inline register return sizeof static struct switch typedef union volatile while class namespace using template typename public private protected virtual override new delete this throw try catch include define ifdef ifndef endif pragma"), lit: words("true false NULL nullptr void int long short char float double unsigned signed bool size_t auto std") },
      rust: { ...cLike, kw: words("as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait type unsafe use where while"), lit: words("true false None Some Ok Err i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize f32 f64 bool char str String Vec Option Result Box") },
      sh: { block: false, line: ["#"], hashWord: true, multi: "\"'", kw: words("if then else elif fi for while until do done case esac in function return local export source alias unset set shift exit break continue select"), lit: words("echo cd ls cat grep sed awk git go npm pnpm make curl rm cp mv mkdir sudo printf read test true false") },
      sql: { block: true, line: ["--"], multi: "", kw: words("select from where and or not insert into values update set delete create table drop alter add index primary key foreign references join left right inner outer full on as group by order having limit offset distinct union all case when then else end is in like between exists default unique view with returning begin commit rollback SELECT FROM WHERE AND OR NOT INSERT INTO VALUES UPDATE SET DELETE CREATE TABLE DROP ALTER ADD INDEX PRIMARY KEY FOREIGN REFERENCES JOIN LEFT RIGHT INNER OUTER FULL ON AS GROUP BY ORDER HAVING LIMIT OFFSET DISTINCT UNION ALL CASE WHEN THEN ELSE END IS IN LIKE BETWEEN EXISTS DEFAULT UNIQUE VIEW WITH RETURNING BEGIN COMMIT ROLLBACK"), lit: words("null true false NULL TRUE FALSE int integer text varchar boolean timestamp INT INTEGER TEXT VARCHAR BOOLEAN TIMESTAMP") },
      json: { block: false, line: [], multi: "", kw: words(""), lit: words("true false null") },
      yaml: { block: false, line: ["#"], multi: "", kw: words(""), lit: words("true false null yes no on off True False") },
    };
    const alias = { golang: "go", javascript: "js", jsx: "js", mjs: "js", cjs: "js", ts: "js", typescript: "js", tsx: "js", python: "py", python3: "py", py3: "py", kotlin: "java", kt: "java", scala: "java", cs: "java", csharp: "java", swift: "java", dart: "java", h: "c", cpp: "c", "c++": "c", cc: "c", hpp: "c", objc: "c", rs: "rust", bash: "sh", shell: "sh", zsh: "sh", console: "sh", jsonc: "json", json5: "json", yml: "yaml", toml: "yaml", ini: "yaml", postgres: "sql", postgresql: "sql", mysql: "sql", sqlite: "sql" };
    return (lang) => {
      const key = String(lang || "").trim().toLowerCase().split(/\s+/)[0];
      return p[key] || p[alias[key]] || null;
    };
  })();

  // Tokens become spans with textContent, never markup: the code stays the
  // literal source, only colored. An unknown language stays plain text, and
  // an unterminated quote on a single-line string is plain (streaming-safe).
  function highlight(codeEl, text, lang) {
    const L = codeLangs(lang);
    if (!L) { codeEl.textContent = text; return; }
    let plain = "";
    const flush = () => { if (plain) { codeEl.append(document.createTextNode(plain)); plain = ""; } };
    const span = (cls, v) => { flush(); codeEl.append(el("span", cls, v)); };
    let i = 0;
    while (i < text.length) {
      const ch = text.charAt(i);
      if (L.block && text.startsWith("/*", i)) {
        let j = text.indexOf("*/", i + 2);
        j = j < 0 ? text.length : j + 2;
        span("cm", text.slice(i, j)); i = j; continue;
      }
      const lc = L.line.find((m) => text.startsWith(m, i) && (!L.hashWord || i === 0 || /\s/.test(text.charAt(i - 1))));
      if (lc) {
        let j = text.indexOf("\n", i);
        if (j < 0) j = text.length;
        span("cm", text.slice(i, j)); i = j; continue;
      }
      if (L.triple && (text.startsWith('"""', i) || text.startsWith("'''", i))) {
        const q = text.slice(i, i + 3);
        let j = text.indexOf(q, i + 3);
        j = j < 0 ? text.length : j + 3;
        span("str", text.slice(i, j)); i = j; continue;
      }
      if (ch === '"' || ch === "'" || ch === "`") {
        const multi = L.multi.indexOf(ch) >= 0;
        let j = i + 1;
        while (j < text.length && text.charAt(j) !== ch && (multi || text.charAt(j) !== "\n")) {
          if (text.charAt(j) === "\\") j++;
          j++;
        }
        if (j < text.length && text.charAt(j) === ch) {
          span("str", text.slice(i, j + 1)); i = j + 1; continue;
        }
        plain += ch; i++; continue;
      }
      if (/[0-9]/.test(ch) && !/[A-Za-z0-9_$]/.test(text.charAt(i - 1) || "")) {
        const m = /^(?:0[xXbBoO][0-9a-fA-F_]+|\d[\d_]*(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(text.slice(i, i + 64));
        span("num", m[0]); i += m[0].length; continue;
      }
      if (/[A-Za-z_$]/.test(ch)) {
        let j = i + 1;
        while (j < text.length && /[A-Za-z0-9_$]/.test(text.charAt(j))) j++;
        const word = text.slice(i, j);
        if (L.kw.has(word)) span("kw", word);
        else if (L.lit.has(word)) span("lit", word);
        else if (text.charAt(j) === "(") span("fn", word);
        else plain += word;
        i = j; continue;
      }
      plain += ch; i++;
    }
    flush();
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

  // Quotes are containers: a run of > lines outside a code fence is cut out
  // before fences are split, so a fence written inside a quote is parsed
  // after its > prefix is gone and stays inside the blockquote. A line holding
  // an odd number of ``` opens or closes a fence, matching appendFenced's
  // pairing; > lines inside a fence are code, not quotes. Likewise inside a
  // $$ block (same open/close rules as appendBlocks) a leading > is TeX.
  function appendMarkdown(parent, text) {
    const lines = String(text || "").replace(/\r\n/g, "\n").split("\n");
    let buf = [], fence = false, math = false;
    const flush = () => { if (buf.length) appendFenced(parent, buf.join("\n")); buf = []; };
    for (let i = 0; i < lines.length; i++) {
      // A line that opens, continues or closes a $$ block is TeX.
      let tex = false;
      if (!fence && math) {
        tex = true;
        if (lines[i].indexOf("$$") >= 0) math = false;
      } else if (!fence) {
        const open = /^\s*\$\$(.*)$/.exec(lines[i]);
        if (open && open[1].indexOf("$$") < 0) math = true;
      }
      if (!fence && !tex && /^\s{0,3}>/.test(lines[i])) {
        flush();
        const inner = [];
        while (i < lines.length && /^\s{0,3}>/.test(lines[i])) inner.push(lines[i++].replace(/^\s{0,3}> ?/, ""));
        i--;
        const quote = document.createElement("blockquote");
        appendMarkdown(quote, inner.join("\n"));
        parent.append(quote);
        continue;
      }
      if ((lines[i].split("```").length - 1) % 2) fence = !fence;
      buf.push(lines[i]);
    }
    flush();
  }

  function appendFenced(parent, text) {
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

  function gallery(urls, singleIndex) {
    const box = el("div", "gallery");
    const single = singleIndex !== undefined;
    const extra = !single && urls.length > 3 ? urls.length - 2 : 0;
    const show = single ? [urls[singleIndex]] : extra ? urls.slice(0, 3) : urls;
    show.forEach((url, index) => {
      const i = single ? singleIndex : index;
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
      cell.setAttribute("aria-label", "查看第 " + (i + 1) + " 张图片");
      cell.addEventListener("click", () => openShots(urls, i, cell));
      box.append(cell);
    });
    return box;
  }

  let shots = null;

  function openShots(urls, index = 0, trigger = document.activeElement) {
    if (!urls.length) return;
    closeShots();
    // A snapshot of this gallery only: never consult the rest of the thread.
    const list = urls.slice();
    const events = new AbortController();
    const on = (node, type, fn, options = {}) => node.addEventListener(type, fn, { ...options, signal: events.signal });
    const stage = el("div", "image-stage");
    const status = el("div", "image-status", "正在载入…");
    status.setAttribute("role", "status");
    const count = el("div", "image-count");
    count.setAttribute("aria-live", "polite");
    count.hidden = list.length === 1;
    const control = (name, glyph, cls, action) => {
      const btn = el("button", "image-control " + cls);
      btn.type = "button";
      btn.setAttribute("aria-label", name);
      btn.title = name;
      btn.append(icon(glyph));
      on(btn, "click", action);
      return btn;
    };
    const close = control("关闭", "x", "image-close", closeShots);
    const prev = control("上一张", "chevronLeft", "image-prev", () => select(index - 1));
    const next = control("下一张", "chevron", "image-next", () => select(index + 1));
    prev.hidden = next.hidden = list.length === 1;
    let img, scale = 1, x = 0, y = 0, width = 0, height = 0;
    let gesture = null, lastTap = null, lastTouch = -Infinity, suppressClick = false;
    const pointers = new Map();
    const clamp = (v, bound) => Math.max(-bound, Math.min(bound, v));
    const paint = () => {
      x = clamp(x, Math.max(0, (width * scale - stage.clientWidth) / 2));
      y = clamp(y, Math.max(0, (height * scale - stage.clientHeight) / 2));
      img.style.transform = "translate(" + x + "px," + y + "px) scale(" + scale + ")";
      stage.classList.toggle("zoomed", scale > 1);
    };
    const fit = () => {
      scale = 1; x = y = 0;
      if (!img.naturalWidth) return;
      const ratio = Math.min(1, stage.clientWidth / img.naturalWidth, stage.clientHeight / img.naturalHeight);
      width = img.naturalWidth * ratio; height = img.naturalHeight * ratio;
      img.style.width = width + "px"; img.style.height = height + "px";
      paint();
    };
    const zoom = (value, cx, cy) => {
      if (!width) return;
      const rect = stage.getBoundingClientRect();
      const px = cx - rect.left - rect.width / 2, py = cy - rect.top - rect.height / 2;
      const target = Math.max(1, Math.min(6, value));
      x = px - (px - x) * target / scale;
      y = py - (py - y) * target / scale;
      scale = target; paint();
    };
    function select(to) {
      if (to < 0 || to >= list.length) return;
      index = to;
      pointers.clear(); gesture = lastTap = null;
      scale = 1; x = y = width = height = 0;
      stage.classList.remove("zoomed");
      count.textContent = (index + 1) + " / " + list.length;
      // Keep focused controls in the tab order even at an endpoint.
      prev.setAttribute("aria-disabled", String(index === 0));
      next.setAttribute("aria-disabled", String(index === list.length - 1));
      status.hidden = false; status.textContent = "正在载入…";
      const current = document.createElement("img");
      current.alt = "第 " + (index + 1) + " 张图片";
      current.draggable = false; current.hidden = true;
      current.onload = () => {
        if (img !== current || events.signal.aborted) return;
        status.hidden = true; current.hidden = false; fit();
      };
      current.onerror = () => {
        if (img !== current || events.signal.aborted) return;
        current.hidden = true; status.hidden = false; status.textContent = "图片暂时无法显示";
      };
      img = current; stage.replaceChildren(current, status); current.src = list[index];
    }
    lightbox.replaceChildren(stage, count, prev, next, close);
    const scroll = { left: thread.scrollLeft, top: thread.scrollTop };
    shots = { events, trigger, scroll };
    lightbox.hidden = false;
    lightbox.showModal();
    select(index);
    close.focus({ preventScroll: true });
    on(window, "resize", fit);
    on(lightbox, "cancel", ev => { ev.preventDefault(); closeShots(); });
    on(document, "keydown", ev => {
      if (ev.key === "Escape" || ev.key === "ArrowLeft" || ev.key === "ArrowRight") {
        ev.preventDefault(); ev.stopImmediatePropagation();
        if (ev.key === "Escape") closeShots();
        else select(index + (ev.key === "ArrowLeft" ? -1 : 1));
      } else if (ev.key === "Tab") {
        // Native dialog isolates the background; explicitly wrap focus as well.
        const controls = [prev, next, close].filter(btn => !btn.hidden);
        const i = controls.indexOf(document.activeElement);
        ev.preventDefault();
        controls[(i + (ev.shiftKey ? -1 : 1) + controls.length) % controls.length].focus({ preventScroll: true });
      }
    }, { capture: true });
    on(lightbox, "wheel", ev => {
      ev.preventDefault();
      if (!stage.contains(ev.target)) return;
      const delta = ev.deltaY * (ev.deltaMode === 1 ? 16 : ev.deltaMode === 2 ? stage.clientHeight : 1);
      zoom(scale * Math.exp(-Math.max(-200, Math.min(200, delta)) * 0.003), ev.clientX, ev.clientY);
    }, { passive: false });
    on(stage, "dblclick", ev => {
      // Touch double taps are handled below; browsers may also emit dblclick.
      if (performance.now() - lastTouch < 500) return;
      const r = img.getBoundingClientRect();
      if (img.hidden || ev.clientX < r.left || ev.clientX > r.right || ev.clientY < r.top || ev.clientY > r.bottom) return;
      ev.preventDefault(); zoom(scale > 1 ? 1 : 2.5, ev.clientX, ev.clientY);
    });
    const point = ev => ({ x: ev.clientX, y: ev.clientY });
    const pair = () => {
      const [a, b] = [...pointers.values()];
      return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, distance: Math.hypot(a.x - b.x, a.y - b.y) };
    };
    on(lightbox, "pointerdown", () => { suppressClick = false; }, { capture: true });
    on(stage, "pointerdown", ev => {
      if (ev.button !== 0) return;
      if (ev.pointerType === "touch") lastTouch = performance.now();
      pointers.set(ev.pointerId, point(ev));
      stage.setPointerCapture(ev.pointerId);
      if (pointers.size === 1) gesture = { start: point(ev), last: point(ev), moved: false, multi: false, zoomed: scale > 1, image: ev.target === img };
      else {
        gesture.multi = true; gesture.pair = pair(); lastTap = null;
      }
    });
    on(stage, "pointermove", ev => {
      if (!pointers.has(ev.pointerId) || !gesture) return;
      pointers.set(ev.pointerId, point(ev));
      if (pointers.size > 1) {
        const now = pair(), before = gesture.pair;
        if (before && before.distance > 0) {
          zoom(scale * now.distance / before.distance, before.x, before.y);
          x += now.x - before.x; y += now.y - before.y; paint();
        }
        gesture.pair = now; gesture.moved = true;
      } else {
        if (Math.hypot(ev.clientX - gesture.start.x, ev.clientY - gesture.start.y) > 8) gesture.moved = true;
        if (scale > 1) {
          x += ev.clientX - gesture.last.x; y += ev.clientY - gesture.last.y; paint();
        }
      }
      gesture.last = point(ev);
    });
    const release = ev => {
      if (!pointers.has(ev.pointerId) || !gesture) return;
      pointers.delete(ev.pointerId);
      if (stage.hasPointerCapture(ev.pointerId)) stage.releasePointerCapture(ev.pointerId);
      suppressClick = gesture.moved || gesture.multi || ev.type !== "pointerup";
      if (pointers.size) { gesture.last = [...pointers.values()][0]; return; }
      const dx = ev.clientX - gesture.start.x, dy = ev.clientY - gesture.start.y;
      if (ev.type === "pointerup" && !gesture.multi) {
        if (!gesture.zoomed && ev.pointerType === "touch" && Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.25) {
          select(index + (dx < 0 ? 1 : -1));
        } else if (!gesture.moved && gesture.image && ev.pointerType === "touch") {
          const now = performance.now();
          if (lastTap && now - lastTap.time < 300 && Math.hypot(ev.clientX - lastTap.x, ev.clientY - lastTap.y) < 24) {
            zoom(scale > 1 ? 1 : 2.5, ev.clientX, ev.clientY); lastTap = null;
          } else lastTap = { ...point(ev), time: now };
        }
      }
      gesture = null;
    };
    on(stage, "pointerup", release);
    on(stage, "pointercancel", release);
    on(stage, "lostpointercapture", release);
    on(lightbox, "click", ev => {
      if (suppressClick) { suppressClick = false; return; }
      // Pointer capture retargets image clicks to the stage. Hit-test the image
      // rectangle so a tap on the picture never dismisses the viewer.
      const r = img.getBoundingClientRect();
      const onImage = !img.hidden && ev.clientX >= r.left && ev.clientX <= r.right && ev.clientY >= r.top && ev.clientY <= r.bottom;
      if ((ev.target === lightbox || ev.target === stage) && !onImage) closeShots();
    });
  }

  function closeShots() {
    if (!shots) return;
    const { events, trigger, scroll } = shots;
    shots = null;
    events.abort();
    lightbox.close(); lightbox.hidden = true; lightbox.replaceChildren();
    if (trigger && trigger.isConnected) trigger.focus({ preventScroll: true });
    else input.focus({ preventScroll: true });
    thread.scrollLeft = scroll.left; thread.scrollTop = scroll.top;
  }

  // Empty chat (window review P2 #7): the bot's face, its name, one line and
  // up to three starters made from its routines and persona. A starter only
  // fills the composer; sending still takes Enter or the send button.
  const EMPTY_LINE = "有什么事，直接说就行。";
  const EMPTY_REPLY = "这次没有收到回复，请再试一次。";
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

  // The composer keeps the textarea's contract (plain text only, Enter sends,
  // Shift+Enter breaks a line, drafts and pasted pictures behave the same) but
  // renders as a contenteditable so http(s) URLs in the draft become links.
  const COMPOSER_MAX = 16000;
  // The body is an ASCII whitelist, aligned with the bubble pattern but
  // tighter: prose glued to the address (https://x.y这个) stops the link where
  // the address really ends instead of swallowing the sentence.
  const composerURL = /https?:\/\/[[A-Za-z0-9\-._~:?#@!$&()*+,;=%/\]]+/g;

  // Only http(s) counts as an address: the href comes from parsing the text,
  // so a javascript: or data: URL can never become a link.
  function composerSafeHref(url) {
    try {
      const parsed = new URL(url);
      return parsed.protocol === "http:" || parsed.protocol === "https:" ? parsed.href : "";
    } catch (e) {
      return "";
    }
  }

  // Trailing punctuation stays prose: a URL before ,.)… must not eat it.
  function composerURLStop(url) {
    while (url.length > 2 && /[.,;:!?)\]}]$/.test(url)) url = url.slice(0, -1);
    return url;
  }

  function composerLinkNode(url, href) {
    const a = document.createElement("a");
    a.href = href;
    a.textContent = url; // never innerHTML: the draft is untrusted text
    a.target = "_blank";
    a.rel = "noopener noreferrer";
    return linkIcon(a);
  }

  // A colored span that keeps its source characters: draft Markdown is
  // highlighted, never rendered into final layout (not WYSIWYG).
  function composerSpan(cls, text) {
    const s = document.createElement("span");
    s.className = cls;
    s.textContent = text; // never innerHTML: the draft is untrusted text
    return s;
  }

  // Inline draft tokens, leftmost first: code spans, [text](url) links, bold,
  // italic, then the bare composerURL addresses. Code spans win over URL
  // inside backticks; math is a bubble-side thing and stays plain here.
  const composerMD = new RegExp(
    "(`[^`\\n]+`)" +
    "|(\\[[^\\]\\n]*\\]\\(https?://[^\\s)\\n]+\\))" +
    "|(\\*\\*[^*\\n]+\\*\\*)" +
    "|(\\*[^*\\n]+\\*)" +
    "|" + composerURL.source,
  "g");

  function composerInline(out, text) {
    let last = 0;
    for (const m of text.matchAll(composerMD)) {
      if (m.index > last) out.push(document.createTextNode(text.slice(last, m.index)));
      const tok = m[0];
      if (m[1] != null) { // `code`
        out.push(composerSpan("md-code", tok));
      } else if (m[2] != null) { // [text](url)
        const url = tok.slice(tok.indexOf("](") + 2, -1);
        const href = composerSafeHref(url);
        out.push(href ? composerLinkNode(tok, href) : document.createTextNode(tok));
      } else if (m[3] != null) { // **bold**
        out.push(composerSpan("md-bold", tok));
      } else if (m[4] != null) { // *italic*
        out.push(composerSpan("md-italic", tok));
      } else { // bare http(s) address, same rules as before
        const url = composerURLStop(tok);
        if (!url) continue;
        const href = composerSafeHref(url);
        if (href) {
          out.push(composerLinkNode(url, href));
          // Punctuation trimmed off the address stays prose.
          if (url.length < tok.length) out.push(document.createTextNode(tok.slice(url.length)));
        } else {
          out.push(document.createTextNode(tok));
        }
      }
      last = m.index + tok.length;
    }
    if (last < text.length) out.push(document.createTextNode(text.slice(last)));
  }

  // The draft split into text nodes, links and colored spans. Line starts get
  // their heading/list marker first (so `* item` is a list dot, not italic);
  // anything that is not a usable http(s) address stays plain text.
  function composerNodes(text) {
    const out = [];
    const lines = String(text).split("\n");
    for (let i = 0; i < lines.length; i++) {
      if (i) out.push(document.createTextNode("\n"));
      let rest = lines[i];
      const h = /^#{1,3}(?=\s)/.exec(rest);
      if (h) {
        out.push(composerSpan("md-heading", h[0]));
        rest = rest.slice(h[0].length);
      } else {
        const li = /^[-*](?=\s)/.exec(rest);
        if (li) {
          out.push(composerSpan("md-list", li[0]));
          rest = rest.slice(li[0].length);
        }
      }
      composerInline(out, rest);
    }
    return out;
  }

  // The editor's plain text: <br> and block edges both count as a newline, so
  // typing, Enter and paste all round-trip through the same string.
  function composerText(root = input) {
    let out = "";
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (child.nodeType === 3) { out += child.nodeValue; continue; }
        if (child.nodeType !== 1) continue;
        if (child.tagName === "BR") { out += "\n"; continue; }
        walk(child);
        if (child.tagName === "DIV" || child.tagName === "P") out += "\n";
      }
    };
    walk(root);
    return out;
  }

  function composerOffsetOf(root, node, offset) {
    let total = 0;
    const walk = (cur) => {
      if (cur.nodeType === 3) {
        total += cur === node ? offset : cur.nodeValue.length;
        return cur === node;
      }
      // An element boundary (including root) counts children, not characters.
      // Stop before that child; walking all of root used to mean "draft end".
      for (let i = 0; i < cur.childNodes.length; i++) {
        if (cur === node && i === offset) return true;
        if (walk(cur.childNodes[i])) return true;
      }
      if (cur === node) return true;
      if (cur !== root && (cur.tagName === "BR" || cur.tagName === "DIV" || cur.tagName === "P")) total++;
      return false;
    };
    walk(root);
    return total;
  }

  // The caret as a plain-text offset, or null when it is outside the editor.
  function composerCaret() {
    const sel = getSelection();
    if (!sel || !sel.rangeCount) return null;
    const r = sel.getRangeAt(0);
    if (!input.contains(r.startContainer)) return null;
    return composerOffsetOf(input, r.startContainer, r.startOffset);
  }

  function composerCaretTo(want) {
    const sel = getSelection();
    if (!sel) return;
    let remaining = Math.max(0, want);
    let spot = null;
    let lastText = null;
    const walk = (node) => {
      for (const child of node.childNodes) {
        if (spot) return;
        if (child.nodeType === 3) {
          lastText = child;
          if (remaining <= child.nodeValue.length) { spot = [child, Math.max(0, remaining)]; return; }
          remaining -= child.nodeValue.length;
        } else if (child.nodeType === 1) {
          if (child.tagName === "BR") { remaining -= 1; continue; }
          walk(child);
          if (!spot && (child.tagName === "DIV" || child.tagName === "P")) remaining -= 1;
        }
      }
    };
    walk(input);
    const range = document.createRange();
    if (spot) range.setStart(spot[0], spot[1]);
    else if (lastText) range.setStart(lastText, lastText.nodeValue.length);
    else range.setStart(input, 0);
    range.collapse(true);
    sel.removeAllRanges();
    sel.addRange(range);
  }

  function composerRefreshHeight() {
    input.style.height = "auto";
    input.style.height = Math.min(input.scrollHeight, 140) + "px";
  }

  // Renders the draft. No cap here: truncating after the fact silently eats
  // existing characters whenever the edit landed before the end, so the old
  // textarea's maxlength lives at the edit entry instead (see below).
  function renderComposer(text) {
    const nodes = composerNodes(text);
    const changed = nodes.length !== input.childNodes.length ||
      nodes.some((node, i) => !node.isEqualNode(input.childNodes[i]));
    // Ordinary typing already has the right DOM. Leave its native selection
    // alone; only replace nodes when highlighting or normalization needs it.
    if (changed) input.replaceChildren(...nodes);
    composerTextCache = text;
    return changed;
  }

  // Programmatic draft writes (fill, restore, refill, send): like assigning
  // the old textarea's value they start a fresh undo history.
  function composerSet(text) {
    renderComposer(text);
    composerHistory.stack = [{ text: composerTextCache, caret: Infinity }];
    composerHistory.index = 0;
  }

  // Undo/redo is ours, one step per accepted edit: the native history would
  // try to restore nodes that every re-render has long replaced. Keys are
  // handled in the keydown/beforeinput listeners further down.
  const composerHistory = { stack: [], index: -1, limit: 200 };
  function composerRecord(before, text, caret) {
    const h = composerHistory;
    if (!h.stack.length) { h.stack.push({ text: before, caret: Infinity }); h.index = 0; }
    if (h.stack[h.index].text === text) return;
    h.stack.length = h.index + 1; // a new edit abandons the redo tail
    h.stack.push({ text, caret: caret === null ? Infinity : caret });
    if (h.stack.length > h.limit) h.stack.shift();
    h.index = h.stack.length - 1;
  }
  function composerApply(entry) {
    renderComposer(entry.text);
    composerRefreshHeight();
    composerCaretTo(entry.caret);
    syncComposer();
  }
  function composerUndo() {
    const h = composerHistory;
    if (h.index <= 0) return;
    h.index--;
    composerApply(h.stack[h.index]);
  }
  function composerRedo() {
    const h = composerHistory;
    if (h.index >= h.stack.length - 1) return;
    h.index++;
    composerApply(h.stack[h.index]);
  }

  // Rewrites the draft's DOM only when its text changed, keeping the caret at
  // the same spot, so typing beside a link never jumps the cursor. This is the
  // edit entry, so the maxlength rule lives here: an edit that would push the
  // draft past COMPOSER_MAX is refused whole — the old textarea never accepted
  // the new characters by silently deleting older ones at the end.
  function refreshComposerLinks() {
    if (composerComposing) return;
    const text = composerText(input);
    if (text === composerTextCache) return;
    if (text.length > COMPOSER_MAX) {
      renderComposer(composerTextCache);
      composerRefreshHeight();
      composerCaretTo(composerEditCaret === null ? Infinity : composerEditCaret);
      return;
    }
    const before = composerTextCache;
    const caret = composerCaret();
    if (renderComposer(text) && caret !== null) composerCaretTo(caret);
    composerRecord(before, text, caret);
  }

  function fillComposer(text) {
    composerSet(String(text == null ? "" : text));
    composerRefreshHeight();
    syncComposer();
    input.focus();
    composerCaretTo(Infinity);
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

  // Sample the spring's timing once. Normalize its small overshoot so the
  // requested .92 start and 3% peak can coexist with the shared Bezier curve.
  function popoverSpringFrames(easing, start, overshoot) {
    const [x1, y1, x2, y2] = easing.match(/[\d.]+/g).map(Number);
    const cubic = (a, b, t) => 3 * (1 - t) ** 2 * t * a + 3 * (1 - t) * t ** 2 * b + t ** 3;
    const samples = Array.from({length: 61}, (_, i) => ({offset: cubic(x1, x2, i / 60), progress: cubic(y1, y2, i / 60)}));
    const peak = Math.max(...samples.map(s => s.progress));
    return samples.map(({offset, progress}) => ({offset,
      opacity: Math.min(1, Math.max(0, progress)),
      transform: "scale(" + (progress <= 1 ? start + (1 - start) * progress : 1 + overshoot * (progress - 1) / (peak - 1)) + ")",
    }));
  }

  function animateMessageMenu(menu) {
    const style = getComputedStyle(menu), reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
    const duration = parseFloat(style.getPropertyValue(reduced ? "--dur-motion-reduced" : "--dur-pop"));
    const easing = style.getPropertyValue("--ease-spring").trim();
    const frames = reduced ? [{opacity: 0}, {opacity: 1}] : popoverSpringFrames(easing,
      Number(style.getPropertyValue("--pop-scale-start")), Number(style.getPropertyValue("--pop-scale-overshoot")));
    menu.animate(frames, {duration, fill: "both"});
  }

  const bubbleMotions = new WeakMap();
  function bubbleTargetMotion(style, mobile) {
    const press = mobile ? 0 : parseFloat(style.getPropertyValue("--dur-bubble-press"));
    const spring = parseFloat(style.getPropertyValue("--dur-bubble-spring"));
    const duration = press + spring;
    const from = Number(style.getPropertyValue(mobile ? "--press-scale" : "--bubble-press-scale"));
    const frames = popoverSpringFrames(style.getPropertyValue("--ease-spring"), from,
      Number(style.getPropertyValue("--bubble-spring-overshoot"))).map(f => ({offset: (press + f.offset * spring) / duration, transform: f.transform}));
    if (press) frames.unshift({offset: 0, transform: "scale(1)", easing: "ease-out"});
    return {frames, duration};
  }

  function animateBubbleTarget(bubble, mobile) {
    bubbleMotions.get(bubble)?.cancel();
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const motion = bubbleTargetMotion(getComputedStyle(bubble), mobile);
    bubbleMotions.set(bubble, bubble.animate(motion.frames, {duration: motion.duration}));
  }

  function closeMessageMenu() {
    if (messageMenu) {
      const menu = messageMenu, style = getComputedStyle(menu);
      menu.style.transform = style.transform;
      menu.style.setProperty("--leave-opacity", style.opacity);
      for (const animation of menu.getAnimations()) animation.cancel();
      menu.classList.replace("message-menu", "message-menu-leaving");
      menu.setAttribute("aria-hidden", "true"); menu.inert = true;
      menu.addEventListener("animationend", () => menu.remove(), {once: true});
    }
    messageTarget?.classList.remove("message-target");
    messageTarget = null;
    document.documentElement.classList.remove("message-menu-open");
    messageMenu = null;
    if (messagePress) {
      clearTimeout(messagePress.timer);
      messagePress.bubble.classList.remove("message-press");
    }
    messagePress = null;
  }

  function bindMessageMenu(row, m, text) {
    messageContents.set(row, {message: m, text});
    row.tabIndex = 0;
  }

  function messageAt(target) {
    if (target.closest(".code-block, pre")) return null;
    const bubble = target.closest(".bubble, .bot-bubble");
    const data = bubble && messageContents.get(bubble.closest(".msg"));
    return data ? {bubble, ...data} : null;
  }

  function showMessageMenu(data, x, y, mobile) {
    const pressed = mobile && messagePress?.bubble;
    closePops();
    copyReceipt?.remove();
    const selection = window.getSelection();
    const selected = selection && data.bubble.contains(selection.anchorNode) && data.bubble.contains(selection.focusNode) ? selection.toString() : "";
    if (mobile) selection?.removeAllRanges();
    const menu = el("div", "message-menu-surface message-menu");
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
    // Measure the resting box, then rebound the pressed portrait to it.
    if (pressed) pressed.style.transition = "none";
    const r = data.bubble.getBoundingClientRect();
    if (pressed) pressed.style.removeProperty("transition");
    const headerBottom = document.querySelector(".chat > .top")?.getBoundingClientRect().bottom || 0;
    const position = mobile
      ? mobileMessageMenuPosition(r, menu.offsetWidth, menu.offsetHeight, innerWidth, innerHeight, data.bubble.closest(".msg").classList.contains("user"), headerBottom)
      : messageMenuPosition(x, y, menu.offsetWidth, menu.offsetHeight, innerWidth, innerHeight);
    menu.style.left = position.left + "px";
    menu.style.top = position.top + "px";
    menu.style.transformOrigin = mobile
      ? (data.bubble.closest(".msg").classList.contains("user") ? "100% " : "0% ") + (position.top < r.top ? "100%" : "0%")
      : Math.max(0, Math.min(menu.offsetWidth, x - position.left)) + "px " + Math.max(0, Math.min(menu.offsetHeight, y - position.top)) + "px";
    animateMessageMenu(menu);
    menu.classList.add("placed");
    messageMenu = menu;
    messageTarget = data.bubble;
    messageTarget.classList.add("message-target");
    animateBubbleTarget(messageTarget, mobile);
    document.documentElement.classList.add("message-menu-open");
    menu.querySelector("button:not(:disabled)")?.focus({preventScroll: true});
  }

  thread.addEventListener("keydown", ev => {
    if (ev.key !== "ContextMenu" && !(ev.key === "F10" && ev.shiftKey)) return;
    if (ev.target.closest(".code-block, pre")) return;
    const row = ev.target.closest(".msg"), data = row && messageContents.get(row);
    const bubble = row?.querySelector(".bubble, .bot-bubble");
    if (!data || !bubble) return;
    ev.preventDefault(); ev.stopPropagation();
    const r = bubble.getBoundingClientRect();
    showMessageMenu({bubble, ...data}, r.left + r.width / 2, r.top + r.height / 2, false);
  });
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
    data.bubble.classList.add("message-press");
    messagePress = {bubble: data.bubble, x: ev.clientX, y: ev.clientY, timer: setTimeout(() => showMessageMenu(data, ev.clientX, ev.clientY, true), 500)};
  });
  thread.addEventListener("pointermove", (ev) => {
    if (messagePress && Math.hypot(ev.clientX - messagePress.x, ev.clientY - messagePress.y) > 8) closeMessageMenu();
  });
  for (const event of ["pointerup", "pointercancel"]) thread.addEventListener(event, () => {
    if (messagePress) { clearTimeout(messagePress.timer); messagePress.bubble.classList.remove("message-press"); }
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

  // One assembly seam for clickable source rows. A kind and identity keep
  // unrelated sources apart while consecutive rows can share their own data.
  function addSourceEvent(container, kind, identity, make, update) {
    let row = container.lastElementChild;
    while (row && row._source) {
      if (row._source.kind === kind && row._source.identity === identity) break;
      row = row.previousElementSibling;
    }
    if (!row || !row._source) {
      row = el("div", "source-event-row");
      row._source = { kind: kind, identity: identity };
      const head = el("button", "sysline link peer-event");
      head.type = "button";
      row.append(head);
      make(row, head);
      container.append(row);
    }
    update(row, row.firstElementChild);
    return row;
  }

  function paintSourceEvent(head, picture, name, words, count) {
    head.replaceChildren(el("span", "", words.lead), picture, el("span", "peer-event-name", name));
    if (words.tail) head.append(el("span", "", words.tail));
    if (words.lost) head.append(el("span", "peer-event-dot", " · "), el("span", "", words.lost));
    head.dataset.count = String(count);
  }

  function addPeerEvent(container, kind, peer, ts, device) {
    return addSourceEvent(container, "bot", peer, (row, head) => {
      row._peer = { peer: peer, sent: 0, received: 0, bounced: 0, first: ts || "" };
      head.dataset.peer = peer;
      head.addEventListener("click", () => openPeer(state.sel && state.sel.id, peer, row._peer.first));
      setTip(head, "查看往来");
    }, (row, head) => {
      const g = row._peer;
      if (device) g.device = device;
      if (kind === "sent") g.sent++;
      else if (kind === "received") g.received++;
      else g.bounced++;
      const who = splitPeer(peer), label = g.device || who.device;
      paintSourceEvent(head, peerAvatar({ bot: who.bot, device: label }, "xs"),
        niceName(who.bot) + (label ? " · " + label : ""), peerEventWords(g), g.sent + g.received);
    });
  }

  function addCLIEvent(container, parsed, key) {
    const sender = parsed.sender || "CLI";
    return addSourceEvent(container, "cli", sender, (row, head) => {
      const drawer = el("div", "routine-drawer event-raw");
      row.classList.add("routine-line");
      row._texts = [];
      drawer.hidden = !openRoutines.has(key);
      head.setAttribute("aria-expanded", String(!drawer.hidden));
      head.addEventListener("click", () => {
        drawer.hidden = !drawer.hidden;
        head.setAttribute("aria-expanded", String(!drawer.hidden));
        if (drawer.hidden) openRoutines.delete(key); else openRoutines.add(key);
      });
      row.append(drawer);
    }, (row, head) => {
      row._texts.push(parsed.text);
      row.lastElementChild.textContent = row._texts.join("\n\n");
      const count = row._texts.length;
      paintSourceEvent(head, avatar(sender, { seed: sender, portrait: true }, "xs"), sender,
        { lead: "收到", tail: count > 1 ? "的 " + count + " 条消息" : "的消息" }, count);
    });
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
      if (ev.key !== "Enter" || ev.isComposing || ev.keyCode === 229) return;
      ev.preventDefault();
      submit();
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

  // Assistant image markup is a workspace artifact, not a bubble attachment.
  // Strip before link parsing (which otherwise leaves !alt). Protect source
  // examples and TeX; an incomplete image consumes only its own line.
  function stripBotImages(text) {
    const src = String(text || "");
    const tokens = /```[\s\S]*?(?:```|$)|`[^`\n]*(?:`|$)|\$\$[\s\S]*?(?:\$\$|$)|\$[^\s$][^$\n]*\$|\\[^\n]|\[(?:\\.|[^\]\\\n])*\]\((?:\\.|[^)\\\n])*\)|!\[/g;
    let out = "", last = 0, m;
    while ((m = tokens.exec(src))) {
      out += src.slice(last, m.index);
      if (m[0] !== "![") { out += m[0]; last = tokens.lastIndex; continue; }
      let i = tokens.lastIndex, depth = 1;
      const lineEnd = src.indexOf("\n", i), stop = lineEnd < 0 ? src.length : lineEnd;
      // A completed multiline alt is one image. An unfinished alt removes
      // only the first line, so later ordinary prose cannot disappear.
      for (; i < src.length && !src.startsWith("\n\n", i) && !src.startsWith("![", i) && depth; i++) {
        if (src[i] === "\\" && i + 1 < src.length && src[i + 1] !== "\n") i++;
        else if (src[i] === "[") depth++;
        else if (src[i] === "]") depth--;
      }
      if (depth || (i > stop && src[i] !== "(" && src[i] !== "[")) i = stop;
      // Complete image destinations may contain balanced parentheses. A
      // reference-style image is removed too, without consuming later prose.
      if (!depth && (src[i] === "(" || src[i] === "[")) {
        const open = src[i++], close = open === "(" ? ")" : "]";
        depth = 1;
        let quote = "";
        for (; i < src.length && src[i] !== "\n" && depth; i++) {
          if (src[i] === "\\" && i + 1 < src.length && src[i + 1] !== "\n") i++;
          else if (quote) { if (src[i] === quote) quote = ""; }
          else if (open === "(" && /["']/.test(src[i]) && /\s/.test(src[i - 1])) quote = src[i];
          else if (src[i] === open) depth++;
          else if (src[i] === close) depth--;
        }
      }
      last = tokens.lastIndex = i;
    }
    return out + src.slice(last);
  }

  function appendBotContent(parent, text) {
    const bubble = el("div", "prose bot-bubble");
    appendMarkdown(bubble, stripBotImages(text));
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
    const raw = m.role === "user" ? unwrap(m.content || "").text : sayText(m);
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
    if (!row) { loadThread("around", seq); return; }
    row.scrollIntoView({ block: "center", behavior: "instant" });
    row.classList.remove("quote-hit");
    void row.offsetWidth;
    row.classList.add("quote-hit");
    window.setTimeout(() => row.classList.remove("quote-hit"), 1500);
  }

  let activeVoice = null;
  function stopVoice() { if (activeVoice) activeVoice.pause();activeVoice=null; }
  function voiceTime(seconds) {
    seconds=Number.isFinite(seconds)?Math.max(0,Math.floor(seconds)):0;
    return Math.floor(seconds/60)+":"+String(seconds%60).padStart(2,"0");
  }
  function appendAudios(parent, ids) {
    for (const id of ids || []) {
      const row=el("div","voice"),audio=document.createElement("audio");
      audio.preload="none";
      queueMicrotask(()=>{if(audio.isConnected){audio.preload="metadata";audio.load();}});
      audio.src="/api/bots/"+encodeURIComponent(state.sel.id)+"/voice/"+encodeURIComponent(id);
      const play=el("button","v-play"),wave=el("div","v-wave"),time=el("span","v-time","");
      const failure=el("span","v-fail","语音加载失败"),retry=el("button","v-retry","重试");
      for(const height of [10,18,26,14,22,12,28,16,20,12]){
        const bar=el("i");bar.style.setProperty("--h",height);wave.append(bar);
      }
      play.type=retry.type="button";play.setAttribute("aria-label","播放语音");play.append(icon("play"));
      wave.setAttribute("role","button");wave.tabIndex=0;wave.setAttribute("aria-label","播放语音");
      failure.hidden=retry.hidden=true;row.append(play,wave,time,failure,retry,audio);parent.append(row);
      const failed=()=>{audio.pause();play.hidden=wave.hidden=time.hidden=true;failure.hidden=retry.hidden=false;};
      const start=()=>{if(activeVoice!==audio)stopVoice();activeVoice=audio;audio.play().catch(error=>{if(error.name!=="AbortError"&&activeVoice===audio)failed();});};
      play.onclick=()=>{if(audio.paused)start();else audio.pause();};
      wave.onclick=play.onclick;
      wave.onkeydown=e=>{if(e.key==="Enter"||e.key===" "){e.preventDefault();play.onclick();}};
      retry.onclick=()=>{failure.hidden=retry.hidden=true;play.hidden=wave.hidden=time.hidden=false;audio.load();start();};
      const updatePlayback=()=>{
        const duration=Number.isFinite(audio.duration)&&audio.duration>0?audio.duration:0;
        const playing=!audio.paused&&!audio.ended;
        if(playing!==row.classList.contains("is-playing")){
          row.classList.toggle("is-playing",playing);
          play.replaceChildren(icon(playing?"pause":"play"));
          play.setAttribute("aria-label",playing?"暂停语音":"播放语音");
          wave.setAttribute("aria-label",playing?"暂停语音":"播放语音");
        }
        time.textContent=duration?voiceTime(playing?Math.ceil(Math.max(0,duration-audio.currentTime)):duration):"";
        const count=duration?Math.floor(Math.min(1,audio.currentTime/duration)*wave.children.length):0;
        [...wave.children].forEach((bar,i)=>bar.classList.toggle("is-on",i<count));
      };
      audio.addEventListener("play",()=>{if(activeVoice!==audio)stopVoice();activeVoice=audio;updatePlayback();});
      for(const event of ["pause","ended","timeupdate"])audio.addEventListener(event,updatePlayback);
      audio.addEventListener("ended",()=>{if(activeVoice===audio)activeVoice=null;});
      audio.addEventListener("error",failed);
      audio.addEventListener("loadedmetadata",()=>{
        const duration=Number.isFinite(audio.duration)&&audio.duration>0?audio.duration:0;
        updatePlayback();
        row.style.setProperty("--voice-wave-w","calc(var(--voice-wave-base) + "+Math.min(64,Math.max(0,duration-5)*2)+"px)");
      });
    }
  }

  let recording=null, micPress=null, suppressMicClick=false;
  const mic=$("mic"),recBar=$("rec-bar"),recSend=$("rec-send"),recCancel=$("rec-cancel");
  function recordingUI(on) {
    composer.classList.toggle("recording",on);recBar.hidden=!on;
    recSend.disabled=false;recCancel.disabled=false;
  }
  function cancelRecording() {
    clearTimeout(micPress?.timer);micPress=null;
    const rec=recording;if(!rec)return;recording=null;rec.cancelled=true;
    clearInterval(rec.timer);rec.stream?.getTracks().forEach(t=>t.stop());
    if(rec.recorder?.state==="recording")rec.recorder.stop();recordingUI(false);
  }
  async function startRecording() {
    if(recording||state.sel?.type!=="bot"||state.peer)return;
    const rec={owner:state.sel,chunks:[],size:0,cancelled:false};recording=rec;
    recordingUI(true);$("rec-time").textContent="0:00";
    try {
      if(!navigator.mediaDevices?.getUserMedia||!window.MediaRecorder)throw Error("microphone unavailable");
      rec.stream=await navigator.mediaDevices.getUserMedia({audio:true});
      if(rec.cancelled||recording!==rec||state.sel!==rec.owner){rec.stream.getTracks().forEach(t=>t.stop());return;}
      const mime=["audio/webm;codecs=opus","audio/mp4","audio/ogg;codecs=opus"].find(t=>MediaRecorder.isTypeSupported(t));
      rec.recorder=new MediaRecorder(rec.stream,mime?{mimeType:mime}:undefined);
      rec.recorder.ondataavailable=e=>{
        if(!e.data.size||rec.cancelled)return;
        rec.size+=e.data.size;
        if(rec.size>10*1024*1024){cancelRecording();showToast("语音超过 10 MB，请分段录音");return;}
        rec.chunks.push(e.data);
      };
      rec.recorder.onerror=()=>{cancelRecording();showToast("无法使用麦克风");};
      rec.recorder.onstop=async()=>{
        rec.stream.getTracks().forEach(t=>t.stop());clearInterval(rec.timer);
        if(rec.cancelled||recording!==rec)return;
        rec.blob=new Blob(rec.chunks,{type:rec.recorder.mimeType});
        await uploadRecording(rec);
      };
      rec.recorder.start(1000);rec.started=Date.now();
      rec.timer=setInterval(()=>{$("rec-time").textContent=voiceTime((Date.now()-rec.started)/1000);},250);
      if(rec.finish)finishRecording(true);
    } catch(e) {
      if(recording===rec){cancelRecording();showToast("无法使用麦克风");}
    }
  }
  async function uploadRecording(rec) {
    if(rec.cancelled||recording!==rec||state.sel!==rec.owner)return;
    recSend.disabled=true;
    try {
      if(!rec.blob.size)throw Error("录音是空的，请重新录音");
      const res=await apiFetch("/api/bots/"+encodeURIComponent(rec.owner.id)+"/voice",{method:"POST",headers:{"Content-Type":rec.blob.type},body:rec.blob});
      if(!res.ok)throw Error("语音没传上来，请重试");
      const data=await res.json();if(recording!==rec||state.sel!==rec.owner)return;
      recording=null;recordingUI(false);
      sendText("",null,[],[data.audio]);
    } catch(e) {
      if(recording===rec){showToast(e.message);recSend.disabled=false;}
    }
  }
  function finishRecording(send) {
    if(!send){cancelRecording();return;}
    const rec=recording;if(!rec)return;
    if(rec.blob){uploadRecording(rec);return;}
    if(!rec.recorder){rec.finish=true;return;}
    recSend.disabled=true;clearInterval(rec.timer);
    if(rec.recorder.state==="recording")rec.recorder.stop();
  }
  mic.addEventListener("pointerdown",e=>{
    if(e.button!==0)return;
    const press={id:e.pointerId,held:false};micPress=press;suppressMicClick=false;
    press.timer=setTimeout(()=>{press.held=true;suppressMicClick=true;startRecording();},250);
  });
  document.addEventListener("pointerup",e=>{
    const press=micPress;if(!press||press.id!==e.pointerId)return;
    clearTimeout(press.timer);micPress=null;
    if(press.held)setTimeout(()=>{suppressMicClick=false;},0);
    if(press.held){const box=composer.getBoundingClientRect();const inside=e.clientX>=box.left&&e.clientX<=box.right&&e.clientY>=box.top&&e.clientY<=box.bottom;
      finishRecording(inside&&!recCancel.contains(e.target));}
  });
  document.addEventListener("pointercancel",()=>{if(micPress){clearTimeout(micPress.timer);micPress=null;cancelRecording();}});
  mic.addEventListener("click",()=>{if(suppressMicClick){suppressMicClick=false;return;}startRecording();});
  mic.addEventListener("contextmenu",e=>e.preventDefault());
  recSend.onclick=()=>finishRecording(true);recCancel.onclick=()=>cancelRecording();
  document.addEventListener("keydown",e=>{if(e.key==="Escape"&&recording){e.preventDefault();cancelRecording();}});
  window.addEventListener("pagehide",()=>{cancelRecording();stopVoice();});

  function userBubble(m, messages) {
    const parsed = unwrap(m.content || "");
    const row = el("div", "msg user");
    if (m.seq) row.dataset.seq = String(m.seq);
    if (m.time) row.dataset.time = String(m.time);
    row.dataset.sender = "user";
    const wrap = el("div", "bubble-col");
    if (m.reply_to) wrap.append(quoteBlock(messages, m.reply_to));
    const before = wrap.childElementCount;
    appendMessageBubbles(wrap, parsed.text, pastedURLs(state.sel, m.images), m.audios);
    if (wrap.childElementCount === before) wrap.append(el("div", "bubble", parsed.text));
    if (parsed.untrusted && parsed.source !== "timer" && !parsed.source.startsWith("bot:")) {
      wrap.append(el("span", "tag warn", parsed.source ? "不可信 · " + parsed.source : "不可信"));
    }
    bindMessageMenu(row, m, parsed.text);
    row.append(wrap);
    addBubbleTime(row, m.time);
    return row;
  }

  // One transcript message stays one row. Only its visible content is split;
  // each image keeps the whole message's viewer navigation.
  function appendMessageBubbles(wrap, text, images, audios, bot = false) {
    const pulled = bot ? { text: stripBotImages(text), images: [] } : pullImages(text);
    const cls = bot ? "prose bot-bubble" : "bubble";
    const src = pulled.text.replace(/\r\n/g, "\n");
    // Blank lines inside fenced code or display math belong to that block.
    const boundaries = /```[\s\S]*?(?:```|$)|`[^`\n]*(?:`|$)|\$\$[\s\S]*?(?:\$\$|$)|\n[ \t]*\n(?:[ \t]*\n)*/g;
    let start = 0;
    const paragraphs = [];
    for (const match of src.matchAll(boundaries)) {
      if (!match[0].startsWith("\n")) continue;
      paragraphs.push(src.slice(start, match.index));
      start = match.index + match[0].length;
    }
    paragraphs.push(src.slice(start));
    // Flat attachment ids carry no text offsets. Only an explicit short audio
    // label becomes a caption; ambiguous prose stays with the preceding text.
    let caption = "";
    const tail = paragraphs[paragraphs.length - 1].trim();
    if (audios?.length && /^(?:[\p{L}\p{N} -]{1,12}版|(?:注|备注|说明)[：:][^\n]{1,20})[。！.!]?$/u.test(tail)) {
      caption = tail;
      paragraphs.pop();
    }
    const prose = paragraphs.join("\n\n").trim();
    if (prose) {
      const bubble = el("div", cls);
      if (bot) appendMarkdown(bubble, prose);
      else appendRich(bubble, prose);
      wrap.append(bubble);
    }
    (audios || []).forEach((id, i) => {
      const bubble = el("div", cls);
      appendAudios(bubble, [id]);
      if (caption && i === audios.length - 1) bubble.append(el("div", "v-caption", caption));
      wrap.append(bubble);
    });
    const urls = [...pulled.images, ...(images || [])];
    urls.forEach((url, i) => wrap.append(gallery(urls, i)));
  }

  // What the owner sees from the bot. The bot speaks with
  // send_message_to_user (notify_owner before the rename): one group per call
  // that landed, split into content bubbles. A run of bot lines between two
  // inputs that made no such call shows nothing, unless it predates tool-only chat; then its reply text
  // stays, so old history is not blank. Pictures queue on the run's final line
  // and show after its last delivery's text.
  const SAY_TOOLS = ["send_message_to_user", "notify_owner"];
  const TOOL_CHAT_SINCE = Date.parse("2026-10-27T11:00:00+08:00");

  function sayLanded(result) {
    const out = String((result && result.content) || "");
    return out.startsWith("sent to the owner's chat") || out.includes("notified the owner");
  }

  // spokenLines maps a bot line's index to the bubbles it shows: a list of
  // {text, images, audios, replyTo}, or null for a legacy line drawn from its content.
  // Lines it leaves out are not drawn.
  function spokenLines(messages) {
    const results = {};
    for (const m of messages) {
      if (m.role === "tool" && m.tool_call_id) results[m.tool_call_id] = m;
    }
    const spoken = new Map();
    let run = [];
    const close = () => {
      const says = [];
      const images = [];
      const audios = [];
      let replyTo = 0;
      let imageLine = -1;
      for (const i of run) {
        const m = messages[i];
        if ((m.images || []).length) { images.push(...m.images); imageLine = i; }
        if ((m.audios || []).length) { audios.push(...m.audios); imageLine = i; }
        if (m.reply_to) replyTo = m.reply_to;
        for (const call of m.tool_calls || []) {
          const info = toolSummary(call);
          if (SAY_TOOLS.includes(info.name) && (sayLanded(results[call.id]) || (m.delivered || []).includes(call.id))) says.push({ i: i, text: String(info.args.text || "").trim() });
        }
      }
      if (!says.length && (images.length || audios.length)) says.push({ i: imageLine, text: "" });
      if (says.length) {
        says[0].replyTo = replyTo;
        says[says.length - 1].images = images;
        says[says.length - 1].audios = audios;
        for (const say of says) spoken.set(say.i, [...(spoken.get(say.i) || []), say]);
      } else {
        for (const i of run) {
          if (!(Date.parse(messages[i].time) >= TOOL_CHAT_SINCE)) spoken.set(i, null);
        }
      }
      run = [];
    };
    messages.forEach((m, i) => {
      if (m.role === "assistant") run.push(i);
      else if (m.role === "user" && !m.internal) close();
    });
    close();
    return spoken;
  }

  // sayText is what a bot line said to the owner, for quotes and previews.
  function sayText(m) {
    const said = (m.tool_calls || []).map(toolSummary).filter((info) => SAY_TOOLS.includes(info.name)).map((info) => String(info.args.text || "").trim()).filter(Boolean);
    return said.length ? said.join("\n\n") : (m.content || "");
  }

  // botBubble draws one delivery row: say from spokenLines, or null for a legacy
  // line drawn from its content.
  function botBubble(m, say, messages, who) {
    const row = el("div", "msg bot");
    if (m.seq) row.dataset.seq = String(m.seq);
    if (m.time) row.dataset.time = String(m.time);
    row.dataset.sender = "bot:" + (who || "");
    const body = el("div", "body bubble-col");
    const replyTo = say ? say.replyTo : m.reply_to;
    if (replyTo) body.append(quoteBlock(messages, replyTo));
    // Pictures the bot showed with send_message_to_user, stored in paste/ like the
    // owner's own; the same gallery as userBubble.
    const pasted = pastedURLs(state.sel, say ? say.images : m.images);
    const said = say ? say.text : String(m.content || "").trim();
    const audios = say ? say.audios : m.audios;
    let text = "";
    if (say) {
      if (said && said !== "SILENT") text = said;
    } else if (said && said !== "SILENT") text = window.XBotProviders.citedText(m.content,m.parts);
    else if (!pasted.length && !(audios || []).length && !(m.parts || []).length && !(m.tool_calls || []).length) {
      text = EMPTY_REPLY;
    }
    appendMessageBubbles(body, displayText(text), pasted, audios, true);
    if (!say) window.XBotProviders.decorate(body, m.parts);
    bindMessageMenu(row, m, said);
    row.append(body);
    addBubbleTime(row, m.time);
    return row;
  }

  function skipMessage(m) {
    if (!m || m.role === "tool" || m.role === "system") return true;
    // A kernel note the owner did not say. The transcript row says so; the
    // interface does not recognize the text.
    return !!m.internal;
  }

  // Sole transcript -> main-column assembly seam. Keys are transcript seqs
  // (plus the delivered-message ordinal), never content fingerprints. Compare
  // against the last projection, not live DOM: selection, open drawers and
  // image load state belong to the reader and must survive a poll.
  function applyTranscript(messages, who) {
    if (!state.firstRender && state.sig && sameTranscriptValue(messages, state.messages)) return false;
    state.messages = messages;
    const inner = el("div", "thread-inner");
    const prevTop = thread.scrollTop;
    const near = state.firstRender || thread.scrollHeight - thread.scrollTop - thread.clientHeight < 120;
    if (!messages || !messages.length) {
      inner.append(emptyState(niceName(who) || "XBot", EMPTY_LINE, who, true));
      restoreSendFeedback(inner);
      commitTranscript(inner, who);
      state.firstRender = false;
      if (near) thread.scrollTop = thread.scrollHeight;
      return true;
    }
    const spoken = spokenLines(messages);
    let any = false;
    let lastKey = "";
    let lastMsg = null;
    const relays = backgroundRelays(messages);
    const wakeRuns = routineRuns(messages);
    const latestCard = {};
    const latestApproval = {};
    messages.forEach((m, i) => {
      const card = codingCardOf(m && m.content);
      if (card) latestCard[card.id] = i;
      const appr = approvalCardOf(m && m.content);
      if (appr) latestApproval[appr.id] = i;
    });
    const projectMessage = (m, i) => {
      if (skipMessage(m) || wakeRuns.hidden.has(i)) return;
      // Background turns have no cards in the chat; the header badge lists them.
      if (m.kind === "fork" || m.kind === "fork_end") return;
      if (m.role === "assistant" && String(m.content || "").trim() === "SILENT" && !(m.images || []).length && !(m.audios || []).length) return;
      const appr = approvalCardOf(m.content);
      if (appr) {
        if (latestApproval[appr.id] !== i) return;
        any = true;
        lastKey = "";
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
        if (parsed.untrusted && parsed.source === "shell") return;
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
        if (parsed.untrusted && parsed.source === "cli") {
          addCLIEvent(inner, parsed, (state.sel ? state.sel.id : "") + ":event:" + (m.seq || m.time || i));
          any = true;
          lastKey = "";
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
      if (relays.has(i) || !spoken.has(i)) return;
      any = true;
      const key = "bot:" + (who || "");
      const says = spoken.get(i);
      if (!says) {
        const visibleParts = (m.parts || []).some(p => !p.hosted || p.hosted.name !== "image_generation");
        if (!(m.content || visibleParts || (m.images || []).length || (m.audios || []).length || (!(m.tool_calls || []).length && !(m.parts || []).length))) return;
      }
      for (const say of says || [null]) {
        const row = botBubble(m, say, messages, who);
        if (!groupStart(lastMsg, m, lastKey, key)) markContinuation(row, m.time);
        inner.append(row);
        lastKey = key;
        lastMsg = m;
      }
    };
    messages.forEach((m, i) => {
      const before = inner.lastElementChild;
      projectMessage(m, i);
      if (m.truncated && inner.lastElementChild !== before) inner.lastElementChild.append(el("div", "muted", "长记录仅显示预览，完整内容可导出查看。"));
      let row = before ? before.nextElementSibling : inner.firstElementChild;
      let ordinal = 0;
      for (; row; row = row.nextElementSibling) {
        row.dataset.transcriptKey = transcriptRowKey(m, i) + ":" + ordinal++;
      }
    });
    if (!any) inner.append(emptyState(niceName(who) || "XBot", EMPTY_LINE, who, true));
    addTimeDividers(inner);
    const unaligned = messages.some((m, i) => !Number.isInteger(m.seq) || m.seq <= 0 || (i > 0 && m.seq <= messages[i - 1].seq));
    const changed = commitTranscript(inner, who, unaligned);
    state.firstRender = false;
    if (changed) thread.scrollTop = near ? thread.scrollHeight : Math.min(prevTop, thread.scrollHeight);
    return true;
  }

  function transcriptRowKey(m, i) {
    const approval = approvalCardOf(m.content);
    const card = codingCardOf(m.content);
    return approval ? "approval:" + approval.id : card ? "card:" + card.id : "seq:" + (m.seq || i);
  }

  function commitTranscript(next, who, reset = false) {
    for (const row of next.children) {
      if (!row.dataset.transcriptKey) row.dataset.transcriptKey = row.classList.contains("tdiv")
        ? "time:" + row.nextElementSibling.dataset.transcriptKey : "empty";
    }
    const current = thread.querySelector(".thread-inner");
    if (reset || !current || current.dataset.transcriptBot !== who || state.firstRender) {
      next.dataset.transcriptBot = who;
      for (const row of next.children) row._transcriptView = row.cloneNode(true);
      restoreSendFeedback(next);
      thread.replaceChildren(next);
      return true;
    }
    const old = new Map(Array.from(current.children).filter(row => !row.hasAttribute("data-history-controls")).map(row => [row.dataset.transcriptKey, row]));
    let cursor = current.firstElementChild;
    if (cursor?.hasAttribute("data-history-controls")) cursor = cursor.nextElementSibling;
    let changed = false;
    for (const projected of Array.from(next.children)) {
      const key = projected.dataset.transcriptKey;
      let row = old.get(key);
      old.delete(key);
      // Prepending can make the first existing bubble continue a sender group.
      // Update that class without replacing the reader's bubble/selection.
      if (row?._transcriptView && row._transcriptView.className !== projected.className) {
        const grouped = row._transcriptView.cloneNode(true);
        grouped.className = projected.className;
        if (grouped.isEqualNode(projected)) {
          row.className = projected.className;
          row._transcriptView = grouped;
          changed = true;
        }
      }
      if (!row || !row._transcriptView?.isEqualNode(projected)) {
        changed = true;
        projected._transcriptView = projected.cloneNode(true);
        if (row) {
          if (cursor === row) cursor = projected;
          row.replaceWith(projected);
        }
        row = projected;
      }
      if (row !== cursor) {
        current.insertBefore(row, cursor);
        changed = true;
      }
      cursor = row.nextElementSibling;
    }
    // Pending/failed sends are reconciled by the existing send-feedback path.
    for (const row of old.values()) if (row.dataset.transcriptKey) { row.remove(); changed = true; }
    restoreSendFeedback(current);
    for (const row of Array.from(current.children)) {
      if (!row.dataset.transcriptKey && !row.hasAttribute("data-history-controls") && !state.pendingSends.includes(row) && !state.replyErrors.includes(row)) { row.remove(); changed = true; }
    }
    if (activeVoice && !activeVoice.isConnected) stopVoice();
    return changed;
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
    // A bot line counts only for what the chat draws from it.
    const spoken = spokenLines(messages);
    for (let i = messages.length - 1; i >= 0; i--) {
      const m = messages[i];
      if (hiddenFromPreview(m)) continue;
      if (m.role === "assistant" && !spoken.has(i)) continue;
      const says = m.role === "assistant" && spoken.get(i);
      const parsed = says ? { text: says.map((say) => say.text).join(" ") } : unwrap(m.content || "");
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
    // The name is on the row; only the 72px rail, which hides it, tips it.
    setTip(btn, label || niceName(name), "rail");
    btn.setAttribute("aria-label", label || niceName(name));
    const av = avatar(name, paint);
    setTip(av, "");
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

  function placeMenu(anchor, menu, side, point) {
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
    if (point) {
      // A context menu opens at the pointer and flips inward at the edges.
      ({left, top} = messageMenuPosition(point.x, point.y, width, height, innerWidth, innerHeight));
      menu.style.transformOrigin = Math.max(0, Math.min(width, point.x - left)) + "px " + Math.max(0, Math.min(height, point.y - top)) + "px";
    } else if (side === "panel") {
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
    btn.dataset.botMenu = "true";
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
      bots: state.bots.map((b) => [b.name, !!b.pinned, !!b.primary, !!b.unread, (b.notes || 0) > 0, b.group || "", b.description, b.avatar, b.avatar_seed, b.avatar_url, lineForBot(b)]),
      folders: state.folders.map((f) => [f.name, f.count, !!f.ungrouped, (f.bots || []).join("\n"), !!collapsed[f.name]]),
      groups: state.groups.map((g) => [g.id, g.name, lineForGroup(g)]),
      others: (state.others || []).map((o) => [(o && o.bot) || "", (o && o.name) || ""]),
      sel: state.sel ? [state.sel.type, state.sel.id] : null,
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
      btn.dataset.botMenu = "true";
      setTip(btn, niceName(b.name), "rail");
      btn.setAttribute("aria-label", niceName(b.name));
      const av = avatar(b.name, paintOfBot(b), "pin");
      setTip(av, "");
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
    titleEl.replaceChildren(...(titleAv ? [titleAv] : []), el("span", "title-name", shown));
    setTip(titleEl, slug && slug !== shown ? slug : "");
    $("head-chip").disabled = !state.sel;
    document.title = shown + " · XBot";
  }

  function leavePeer() {
    state.peer = null;
    $("chat-search").hidden = state.sel?.type !== "bot";
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
    $("head-chip").disabled = true;
    titleEl.replaceChildren();
    const row = el("span", "pair-title");
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
    $("chat-search").hidden = true;
    $("history-search-panel").hidden = true;
    state.peer = { a: a, b: b, returnTop: state.peer?.returnTop ?? thread.scrollTop, at: at || "", remote: !!splitPeer(b).device, list: [], more: false, cursor: null, mock: false };
    paintPeerHeader(a, b);
    await loadPeer(null);
  }

  async function closePeer() {
    if (!state.peer) return;
    const returnTop = state.peer.returnTop;
    leavePeer();
    if (state.sel && state.sel.type === "bot") setHeader(state.sel.id);
    else if (state.sel && state.sel.type === "group") {
      const g = state.groups.find((x) => x.id === state.sel.id);
      setHeader(state.sel.id, g ? niceName(g.name || state.sel.id) : "");
    }
    setStatus("");
    state.sig = "";
    if (state.sel?.type === "bot" && state.messages.length) {
      state.firstRender = true;
      applyTranscript(state.messages, state.sel.id);
      paintHistory();
      thread.scrollTop = returnTop;
    }
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
    subtitle.textContent = text;
    subtitle.hidden = !text;
  }

  function resetConversation() {
    cancelRecording();stopVoice();
    $("mic").hidden = state.sel?.type !== "bot";
    closeMessageMenu();
    // Pasted pictures belong to the bot they were uploaded to.
    clearAttach();
    state.streaming = 0;
    state.pendingSends = [];
    paintWorkStatus();
    state.replyErrors = [];
    state.firstRender = true;
    state.messages = [];
    $("chat-search").hidden = state.sel?.type !== "bot";
    $("history-search-panel").hidden = true;
    $("chat-search").setAttribute("aria-expanded", "false");
    $("history-search").value = "";
    $("history-results").replaceChildren();
    historyButtons.error.hidden = true;
    historyButtons.end.hidden = true;
    paintHistory();
    state.panelData = null;
    state.inboxData = null;
    state.traceOpen = {};
    state.traceOut = {};
    state.traceThink = {};
    // The snapshot belongs to one bot's one wake. Keeping it would show A's
    // context under B's name, so it is dropped and refetched on demand.
    state.ctxSnap = null;
    state.ctxStep = -1;
    state.ctxMenu = false;
    state.ctxRaw = "";
    ++state.threadLoad;
    ++state.panelLoad;
    thread.replaceChildren();
    showThreadCover();
    panelBody.replaceChildren(el("div", "muted", "正在加载"));
    closePanelMenu();
  }

  let banterIndex = -1;
  let coverTimer = null;
  const BANTER = ["Bot 正在嬉戏", "正在拆家", "去阳台晒太阳了", "在翻旧账（礼貌版）", "假装很忙", "马上回来，别偷看", "把尾巴卷好了再聊", "在和别的 Bot 打架（输了）"];
  function stopCoverBanter() {
    clearTimeout(coverTimer);
    coverTimer = null;
  }

  function showThreadCover() {
    stopCoverBanter();
    document.querySelector(".thread-loading-cover")?.remove();
    thread.setAttribute("aria-busy", "true");
    const cover = el("div", "thread-loading-cover chat-open-mask");
    cover.setAttribute("role", "status");
    cover.setAttribute("aria-label", "正在加载对话");
    const companion = el("div", "chat-open-companion");
    const face = el("div", "chat-open-avatar");
    face.setAttribute("aria-hidden", "true");
    const selectedBot = state.sel?.type === "bot" && state.bots.find(bot => bot.name === state.sel.id);
    if (selectedBot) face.append(avatar(selectedBot.name, paintOfBot(selectedBot), "lg"));
    const line = el("div", "chat-open-banter");
    companion.append(face, line);
    cover.append(companion);
    thread.after(cover);
    const reduced = matchMedia("(prefers-reduced-motion: reduce)").matches;
    let ticks = 0;
    const change = () => {
      banterIndex = (banterIndex + 1 + Math.floor(Math.random() * (BANTER.length - 1))) % BANTER.length;
      line.replaceChildren(...Array.from(BANTER[banterIndex], ch => el("span", "banter-char", ch)));
      line.classList.remove("changing");
      if (!reduced) line.animate([{opacity:0,transform:"translateY(6px) scale(.98)"},{opacity:1,transform:"translateY(0) scale(1)"}], {duration:280,easing:"ease-out"});
    };
    change();
    if (!reduced) companion.animate([{opacity:0,transform:"translateY(6px) scale(.98)"},{opacity:1,transform:"translateY(0) scale(1)"}],{duration:280,easing:"ease-out"});
    const tick = () => {
      if (!cover.isConnected || cover.classList.contains("leaving")) return;
      ticks++;
      if (ticks % 3 === 0) {
        if (!reduced) line.classList.add("changing");
        coverTimer = setTimeout(() => { change(); coverTimer = setTimeout(tick, 900); }, reduced ? 0 : 160);
        return;
      }
      if (!reduced && ticks % 2 === 0) {
        const chars = line.children;
        const ch = chars[Math.floor(Math.random() * chars.length)];
        ch?.animate([{transform:"translateY(0) rotate(0)"},{transform:"translateY(-1.5px) rotate(2deg)"},{transform:"translateY(0) rotate(0)"}],{duration:180,easing:"ease-out"});
      }
      coverTimer = setTimeout(tick,900);
    };
    coverTimer = setTimeout(tick,900);
  }

  function revealThread() {
    stopCoverBanter();
    thread.removeAttribute("aria-busy");
    const cover = document.querySelector(".thread-loading-cover");
    if (!cover) return;
    if (matchMedia("(prefers-reduced-motion: reduce)").matches) { cover.remove(); return; }
    cover.classList.add("leaving");
    cover.addEventListener("transitionend", ev => { if (ev.target === cover) cover.remove(); });
    window.setTimeout(() => cover.remove(), 360);
  }

  function threadUnavailable() {
    state.sig = "";
    stopCoverBanter();
    const cover = document.querySelector(".thread-loading-cover");
    const box = el("div", "thread-unavailable");
    box.setAttribute("role", "status");
    const retry = el("button", "btn sec");
    retry.append(icon("refresh-cw"), document.createTextNode("重试"));
    retry.type = "button";
    retry.addEventListener("click", async () => {
      showThreadCover();
      await loadThread();
      if (panel.classList.contains("open")) await loadPanel();
    });
    box.append(el("h3", "", "对话加载失败"), el("p", "", "这次没能打开对话记录。可以重试。"), retry);
    if (cover) cover.replaceChildren(box);
    else thread.replaceChildren(box);
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
        if (!composerText(input)) composerSet(draft);
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
        if (!composerText(input)) composerSet(draft);
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

  // Structural equality for responses without validators (legacy/migration).
  // No full-transcript serialization or content fingerprints.
  function sameTranscriptValue(a, b) {
    if (a === b) return true;
    if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
    const keys = Object.keys(a);
    return keys.length === Object.keys(b).length && keys.every(key =>
      Object.prototype.hasOwnProperty.call(b, key) && sameTranscriptValue(a[key], b[key]));
  }

  function paintHistory() {
    const sel = state.sel, meta = sel?.history;
    const inner = thread.querySelector(".thread-inner:not(.thread-skeleton)");
    if (inner && sel?.type === "bot" && !state.peer) {
      if (historyControls.parentNode !== inner) inner.prepend(historyControls);
      historyControls.hidden = false;
    } else historyControls.hidden = true;
    historyButtons.older.hidden = !meta?.has_more_before;
    historyButtons.newer.hidden = !meta?.has_more_after;
    historyButtons.latest.hidden = !meta?.has_more_after;
    for (const key of ["older", "newer", "latest"]) {
      const btn = historyButtons[key];
      btn.disabled = !!sel?.historyLoading || !!state.streaming;
      btn.setAttribute("aria-busy", String(sel?.historyLoading === key));
      const label = {older:"更早的消息",newer:"更新的消息",latest:"回到最新"}[key];
      const text = sel?.historyLoading === key ? "加载中…" : label;
      if (btn.textContent !== text) btn.textContent = text;
    }
  }

  async function loadThread(mode = "poll", target = 0) {
    if (!state.sel || state.streaming || state.peer) return false;
    const selection = state.sel;
    if (mode === "poll" && (selection.historyLoading || selection.history?.has_more_after)) return false;
    if (selection.historyLoading && mode !== "around" && mode !== "latest") return false;
    const request = ++state.threadLoad;
    selection.historyLoading = mode;
    paintHistory();
    try {
      if (selection.type === "bot") {
        let query = "";
        if (mode === "older") query = "?before=" + selection.history.next_before;
        if (mode === "newer") query = "?after=" + selection.history.next_after;
        if (mode === "around") query = "?around=" + Number(target);
        const res = await apiFetch("/api/bots/" + encodeURIComponent(selection.id) + "/transcript" + query, {
          credentials: "same-origin",
          headers: mode === "poll" && state.sig && selection.transcriptETag ? { "If-None-Match": selection.transcriptETag } : {},
        });
        if (state.sel !== selection || request !== state.threadLoad || state.streaming || state.peer) return false;
        if (res.status === 304) return true;
        const data = await res.json();
        if (!res.ok) throw new Error(data.error || "request failed");
        if (state.sel !== selection || request !== state.threadLoad || state.streaming || state.peer) return false;
        selection.transcriptETag = !query ? res.headers.get("ETag") : null;
        let messages = data.messages || [];
        const old = state.messages;
        if (mode === "poll" && data.window && old.length && !state.firstRender &&
            thread.scrollHeight-thread.scrollTop-thread.clientHeight >= 120 && data.window.tail_seq > old.at(-1).seq) {
          selection.history = {...selection.history, total:data.window.total, has_more_after:true};
          return true;
        }
        if (data.window && old.length && ["older", "newer", "poll"].includes(mode)) {
          const head = messages[0]?.seq, tail = messages.at(-1)?.seq;
          if (mode === "poll" && head > old.at(-1).seq + 1) {
            selection.history = { ...selection.history, total: data.window.total, has_more_after: true };
            return true;
          }
          if (head && tail) {
            const bySeq = new Map(old.map(m => [m.seq, m]));
            messages.forEach(m => bySeq.set(m.seq, m));
            messages = [...bySeq.values()].sort((a,b) => a.seq-b.seq);
            messages = mode === "older" ? messages.slice(0,300) : messages.slice(-300);
          }
        }
        const box = thread.getBoundingClientRect();
        const anchor = [...thread.querySelectorAll(".msg[data-transcript-key]")].find(row => row.getBoundingClientRect().bottom > box.top);
        const anchorTop = anchor?.getBoundingClientRect().top;
        if (mode === "around" || mode === "latest") state.firstRender = true;
        const changed = applyTranscript(messages, selection.id);
        revealThread();
        if ((mode === "older" || mode === "newer") && anchor?.isConnected) thread.scrollTop += anchor.getBoundingClientRect().top - anchorTop;
        selection.history = data.window ? { ...data.window,
          head_seq: messages[0]?.seq || 0, tail_seq: messages.at(-1)?.seq || 0,
          next_before: messages[0]?.seq || 0, next_after: messages.at(-1)?.seq || 0,
          has_more_before: (messages[0]?.seq || 0) > 1,
          has_more_after: (messages.at(-1)?.seq || 0) < data.window.total,
        } : null;
        historyButtons.error.hidden = true;
        if (subtitleError && subtitle.textContent === "对话加载失败，请重试。") setStatus("");
        if (mode === "around") {
          const row = thread.querySelector('.msg[data-seq="' + Number(target) + '"]');
          if (row) {
            row.scrollIntoView({block:"center", behavior:"instant"});
            row.classList.add("history-hit-target");
            window.setTimeout(() => row.classList.remove("history-hit-target"), 2500);
          }
          else {
            historyButtons.error.textContent = "已定位到命中附近。该记录不在聊天气泡中显示，可查看搜索预览或轨迹。";
            historyButtons.error.hidden = false;
          }
        }
        if (mode === "older" && !selection.history?.has_more_before) {
          historyButtons.end.hidden = false;
          window.setTimeout(() => { historyButtons.end.hidden = true; }, 1800);
        }
        state.previewAt["b:" + selection.id] = Date.now();
        state.sig = "transcript-loaded";
        if (changed && !selection.history?.has_more_after) state.preview["b:" + selection.id] = previewFromMessages(messages);
        if (changed) renderLists();
        if (changed && panel.classList.contains("open")) await loadPanel();
        return true;
      }
      const data = await api("/api/groups/" + encodeURIComponent(selection.id));
      if (state.sel !== selection || request !== state.threadLoad || state.streaming || state.peer) return false;
      const messages = data.messages || [];
      const sig = JSON.stringify(messages);
      state.previewAt["g:" + selection.id] = Date.now();
      if (sig === state.sig) return true;
      state.sig = sig;
      state.preview["g:" + selection.id] = previewFromGroup(messages);
      renderLists();
      renderGroup(messages, data.group && data.group.name);
      revealThread();
      return true;
    } catch (e) {
      if (state.sel === selection && request === state.threadLoad && !state.streaming && !state.pendingSends.length) {
        if (!state.messages.length) threadUnavailable();
        else {
          historyButtons.error.textContent = "对话加载失败，请重试。";
          historyButtons.error.hidden = false;
          setStatus("对话加载失败，请重试。", true);
        }
      }
      return false;
    } finally {
      if (state.sel === selection && request === state.threadLoad) {
        selection.historyLoading = false;
        paintHistory();
      }
    }
  }

  historyButtons.older.addEventListener("click", () => loadThread("older"));
  historyButtons.newer.addEventListener("click", () => loadThread("newer"));
  historyButtons.latest.addEventListener("click", () => loadThread("latest"));
  $("chat-search").addEventListener("click", () => {
    const panel = $("history-search-panel");
    panel.hidden = !panel.hidden;
    $("chat-search").setAttribute("aria-expanded", String(!panel.hidden));
    if (!panel.hidden) $("history-search").focus();
  });
  function closeHistorySearch() {
    $("history-search-panel").hidden = true;
    $("chat-search").setAttribute("aria-expanded", "false");
  }
  $("history-close").addEventListener("click", () => { closeHistorySearch(); $("chat-search").focus(); });
  document.addEventListener("keydown", ev => {
    if (ev.key === "Escape" && !$("history-search-panel").hidden) { closeHistorySearch(); $("chat-search").focus(); }
  });
  document.addEventListener("pointerdown", ev => {
    if (!$("history-search-panel").hidden && !ev.target.closest("#history-search-panel, #chat-search")) closeHistorySearch();
    thread.querySelectorAll(".history-hit-target").forEach(row => row.classList.remove("history-hit-target"));
  });
  let lastHistoryTop = 0;
  thread.addEventListener("scroll", () => {
    const movingUp = thread.scrollTop < lastHistoryTop;
    lastHistoryTop = thread.scrollTop;
    if (movingUp && thread.scrollTop < 24 && !state.sel?.historyLoading && state.sel?.history?.has_more_before && !state.peer) loadThread("older");
  });
  let searchRequest = 0;
  $("history-search").addEventListener("input", () => { ++searchRequest; });
  $("history-form").addEventListener("submit", async ev => {
    ev.preventDefault();
    const selection = state.sel, request = ++searchRequest;
    if (selection?.type !== "bot" || state.peer) return;
    const results = $("history-results"), q = $("history-search").value.trim();
    results.replaceChildren(el("p", "muted", q ? "正在搜索…" : "输入关键词搜索聊天历史。"));
    if (!q) return;
    try {
      const data = await api("/api/bots/" + encodeURIComponent(selection.id) + "/transcript/search?q=" + encodeURIComponent(q));
      if (state.sel !== selection || request !== searchRequest || state.peer) return;
      results.replaceChildren();
      for (const hit of data.matches || []) {
        const btn = el("button", "history-hit search-row");
        btn.append(el("span", "ex", hit.preview || "查看这条历史记录"));
        if (hit.time) btn.append(el("time", "tm", new Date(hit.time).toLocaleString()));
        btn.type = "button";
        btn.dataset.seq = hit.seq;
        btn.addEventListener("click", () => {
          closeHistorySearch();
          loadThread("around", hit.seq);
        });
        results.append(btn);
      }
      if (!results.childElementCount) results.append(el("p", "muted", "没有找到相关历史。"));
      else if (data.matches.length === 20) results.append(el("p", "muted", "显示最相关的 20 条，可增加关键词缩小范围。"));
    } catch (e) {
      if (state.sel === selection && request === searchRequest && !state.peer) results.replaceChildren(el("p", "muted", "历史搜索失败，请重试。"));
    }
  });

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

  const TABS = [["details", "详情"], ["library", "记忆"], ["computer", "任务"], ["trace", "轨迹"], ["context", "上下文"], ["inbox", "收件箱"]];

  function tabBar() {
    const bar = el("div", "tabs");
    bar.setAttribute("role", "tablist");
    for (const pair of TABS) {
      if (pair[0] === "inbox" && state.sel?.type !== "bot") continue;
      const btn = el("button", state.tab === pair[0] ? "on" : "", pair[1]);
      if (pair[0] === "inbox" && state.sel?.type === "bot" && state.inboxData?.pending_count) btn.append(el("span", "inbox-count", " " + state.inboxData.pending_count));
      btn.type = "button";
      btn.setAttribute("role", "tab");
      btn.setAttribute("aria-selected", state.tab === pair[0] ? "true" : "false");
      btn.addEventListener("click", () => {
        state.tab = pair[0];
        paintPanel();
        if (pair[0] === "inbox" && state.sel?.type === "bot") loadInbox();
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

  // The info header has only edit; other actions belong to sidebar Bot rows.
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
  // open, so it is no longer a descendant of the anchor; selecting only
  // inside the row would leave it on screen, still acting on the old bot.
  let panelMore = null;
  let panelSub = null;
  let chromeBot = "";
  let panelMoreAnchor = null;

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
    const more = panelMoreAnchor;
    panelMoreAnchor = null;
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

  // Polling preserves both the edit button focus and an open sidebar menu.
  function syncPanelChrome() {
    const bot = state.sel && state.sel.type === "bot";
    // A repaint for the same bot (polling, status) must not close an open menu.
    if (bot && chromeBot === state.sel.id) return;
    const row = $("panel-actions");
    closePanelMenu();
    row.replaceChildren();
    row.hidden = !bot;
    chromeBot = bot ? state.sel.id : "";
    if (!bot) return;
    const edit = panelAct("pencil", "编辑资料");
    edit.addEventListener("click", () => openProfile(state.panelData || {}));
    row.append(edit);
  }

  // Delegate to the sidebar so pinned tiles and normal rows share one path,
  // even after polling replaces their DOM. A long press must not select a Bot.
  let botPress = null;
  let suppressBotClick = null;
  const sidebarBot = (target) => target.closest?.("[data-bot-menu]");
  function cancelBotPress() {
    if (botPress) clearTimeout(botPress.timer);
    botPress = null;
  }
  sidebar.addEventListener("contextmenu", (ev) => {
    const row = sidebarBot(ev.target);
    if (!row) return;
    ev.preventDefault();
    if (botPress || ev.pointerType === "touch") suppressBotClick = {name: row.dataset.bot, until: Infinity};
    cancelBotPress();
    // A keyboard-opened menu has no pointer; it falls back to the row.
    openPanelMore(row, row.dataset.bot, ev.clientX || ev.clientY ? {x: ev.clientX, y: ev.clientY} : null);
  });
  sidebar.addEventListener("pointerdown", (ev) => {
    cancelBotPress();
    suppressBotClick = null;
    const row = sidebarBot(ev.target);
    if (!row || ev.pointerType === "mouse" || !ev.isPrimary) return;
    const point = {x: ev.clientX, y: ev.clientY};
    botPress = {...point, timer: setTimeout(() => {
      botPress = null;
      if (!row.isConnected) return;
      suppressBotClick = {name: row.dataset.bot, until: Infinity};
      openPanelMore(row, row.dataset.bot, point);
    }, 500)};
  });
  sidebar.addEventListener("pointermove", (ev) => {
    if (botPress && Math.hypot(ev.clientX - botPress.x, ev.clientY - botPress.y) > 10) cancelBotPress();
  });
  sidebar.addEventListener("pointerup", () => {
    if (suppressBotClick) suppressBotClick.until = Date.now() + 1000;
    cancelBotPress();
  });
  sidebar.addEventListener("pointercancel", cancelBotPress);
  sidebar.addEventListener("scroll", () => { cancelBotPress(); closePanelMenu(); }, true);
  sidebar.addEventListener("click", (ev) => {
    const row = sidebarBot(ev.target);
    if (row && suppressBotClick?.name === row.dataset.bot && Date.now() < suppressBotClick.until) {
      ev.preventDefault();
      ev.stopImmediatePropagation();
      suppressBotClick = null;
    }
  }, true);

  function currentGroup(name) {
    return (state.bots.find((b) => b.name === name) || {}).group || "";
  }

  function openPanelMore(anchor, name, point) {
    const data = state.bots.find((b) => b.name === name);
    if (!data) return;
    closePops();
    hideTip();
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
    item("pin", pinned ? "取消置顶" : "置顶", () => togglePin(name, !pinned));
    menu.append(moveMenu(name, currentGroup(name)));
    item("copy", "复刻", () => openFork(name));
    const copy = item("clipboard", "复制 Bot ID", () => copyBotID(name));
    copy.append(el("span", "v mono", name));
    if (state.app) item("folder-open", "打开工作目录", () => openWorkdir(name));
    item("folder-open", "查看生成的图片", () => openGenerated(name));
    item("download", "导出对话", () => openExport(name));
    menu.append(el("div", "mgap"));
    item("trash", "删除 Bot", () => confirmDelete(name), { danger: true });
    anchor.classList.add("on");
    anchor.setAttribute("aria-expanded", "true");
    placeMenu(anchor, menu, undefined, point);
    panelMoreAnchor = anchor;
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

  async function openGenerated(name) {
    const route = "/api/bots/" + encodeURIComponent(name);
    try {
      const data = await api(route + "/generated");
      openModal("生成的图片", [
        { type: "note", label: "图片保存在运行 XBot 的电脑上。" },
        { name: "path", label: "工作区 generated/", value: data.path },
      ], async () => {
        if (!await copyText(data.path)) throw new Error("复制失败，请手动选择路径。");
        showToast("已复制路径");
      }, "复制路径", inputs => {
        inputs.path.readOnly = true;
        $("modal-cancel").textContent = "关闭";
        if (data.can_open) {
          const open = el("button", "secondary", "打开目录"); open.type = "button";
          open.onclick = async () => {
            open.disabled = true;
            try { await api(route + "/open-workdir?folder=generated", {method:"POST"}); }
            catch (e) { modalError.hidden = false; modalError.textContent = visibleError(e); }
            finally { open.disabled = false; }
          };
          modalFields.append(open);
        }
      });
    } catch (e) { setStatus(visibleError(e), true); }
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
    const who = el("div", "tx");
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
    const active = tabs.querySelector("[aria-selected=true]");
    if (active) tabs.scrollLeft = Math.max(0, active.offsetLeft + active.offsetWidth - tabs.clientWidth + 16);
  }

  function paintPanel() {
    const tasks = panelBody.querySelector(".task-view");
    if (tasks && state.sel?.type === "bot" && state.tab === "computer" && tasks.dataset.bot === state.sel.id) {
      syncTaskView(tasks, state.panelData || {name: state.sel.id});
      paintContextBar(); syncPanelChrome(); return;
    }
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
      } else if (state.tab === "inbox") {
        body.append(el("p", "hint", "群组没有收件箱"));
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
    else if (state.tab === "inbox") body.append(inboxView());
    else body.append(detailsView(data));
    panelBody.append(body);
  }

  async function loadInbox() {
    const selection = state.sel;
    if (!selection || selection.type !== "bot") return;
    try {
      const data = await api("/api/bots/" + encodeURIComponent(selection.id) + "/inbox");
      if (state.sel !== selection) return;
      state.inboxData = data;
    } catch (e) {
      if (state.sel !== selection) return;
      state.inboxData = { error: true };
    }
    paintPanel();
  }

  function inboxView() {
    const view = el("div", "inbox-view");
    view.append(el("p", "hint", "叫醒这个 Bot 的外部消息"));
    const data = state.inboxData;
    if (!data) { view.append(el("p", "hint", "正在加载…")); return view; }
    if (data.error) { view.append(el("p", "hint", "收件箱加载失败")); return view; }
    if (!data.items.length) {
      view.append(el("p", "inbox-empty-title", "还没有收到外部消息"), el("p", "hint", "Webhook、定时、其他 Bot 等叫醒时会出现在这里"));
      return view;
    }
    const names = { webhook: "Webhook", timer: "定时", peers: "其他 Bot", telegram: "Telegram", lark: "飞书", watch: "文件监听", "coding-agent": "编码任务" };
    for (const item of data.items) {
      const row = el("details", "inbox-row");
      const head = el("summary", "inbox-row-head");
      const date = new Date(item.time);
      const source = item.sender_kind === "bot" || item.source.startsWith("bot:") ? "其他 Bot" : (names[item.source] || (item.source.startsWith("webhook:") ? "Webhook" : item.source.startsWith("watch:") ? "文件监听" : "其他"));
      head.append(el("time", "inbox-time", isNaN(date) ? "" : date.toLocaleString()), el("span", "inbox-source", source), el("span", "inbox-preview", item.summary.replace(/\s+/g, " ")));
      row.append(head, el("p", "inbox-content", item.summary));
      view.append(row);
    }
    return view;
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
      if (m.role !== "assistant") continue;
      const calls = [];
      for (const call of m.tool_calls || []) {
        const info = toolSummary(call);
        if (info.name === "ask_user") continue;
        const result = results[call.id || ""];
        calls.push({
          name: info.name,
          args: info.args,
          output: result ? (result.content || "") : "",
          hasResult: !!result,
          resultTime: result && result.time,
        });
      }
      // Hosted generation is projected onto the same trace calls as local
      // tools; it stays a hosted protocol item and is never re-executed.
      for (const part of m.parts || []) {
        const h = part.hosted;
        if (!h || h.name !== "image_generation") continue;
        calls.push({ name: h.name, args: h.query ? { prompt: h.query } : {}, output: part.text || "", hasResult: ["completed", "failed"].includes(h.status), resultTime: m.time });
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
        names: calls.map((c) => c.name === "todo" ? todoTrace(c).title : c.name),
        dur: start && end > start ? end - start : 0,
        thinking: (m.thinking || "").trim(),
        calls: calls,
      });
    }
    return turns;
  }

  // A receipt confirms the write; arguments alone are only a proposed list.
  // Keep the original call/result in the trace, never in the conversation.
  function todoTrace(call) {
    const output = String(call.output || "").trim();
    const receipt = /^todo: (\d+) items \((\d+) in_progress, (\d+) pending, (\d+) done, (\d+) dropped\)/.exec(output);
    let title = "待办 · 正在更新";
    if (call.hasResult) title = /^error:/.test(output) ? "待办 · 更新失败" : "待办 · 结果待核对";
    if (!receipt) return { title, lines: [] };
    const total = Number(receipt[1]), done = Number(receipt[4]), dropped = Number(receipt[5]);
    title = total ? "待办 · " + (done === total ? "已完成 " : "进度 ") + done + "/" + total + " 项" + (dropped ? " · 已放弃 " + dropped + " 项" : "") : "待办 · 已清空";
    const labels = { pending: "待办", in_progress: "进行中", done: "已完成", dropped: "已放弃" };
    const items = Array.isArray(call.args && call.args.items) ? call.args.items.slice(0, 20) : [];
    const lines = items.filter(it => it && typeof it.content === "string").map(it => {
      const status = String(it.status || "").trim().toLowerCase();
      const reason = status === "dropped" && typeof it.reason === "string" ? it.reason.trim() : "";
      return (labels[status] || "状态待核对") + " · " + it.content.trim() + (reason ? "（" + reason + "）" : "");
    });
    return { title, lines };
  }

  function traceBody(turn) {
    const box = el("div", "trace-body");
    // Thinking stays folded until the owner opens it; repaints keep the choice.
    if (turn.thinking) {
      const think = el("details", "trace-think");
      think.open = !!state.traceThink[turn.key];
      think.append(el("summary", "text-btn", "思考"), el("pre", "trace-out", turn.thinking));
      think.addEventListener("toggle", () => { state.traceThink[turn.key] = think.open; });
      box.append(think);
    }
    turn.calls.forEach((call, i) => {
      const item = el("div", "trace-call");
      if (call.name === "todo") {
        const view = todoTrace(call);
        item.append(el("div", "trace-call-name", view.title));
        if (view.lines.length) item.append(el("pre", "trace-args", view.lines.join("\n")));
        const raw = el("details");
        raw.append(el("summary", "text-btn", "调用原文"));
        const args = argText(call.args);
        if (args) raw.append(el("pre", "trace-args", args));
        if (call.output) raw.append(el("pre", "trace-out", call.output));
        item.append(raw);
        box.append(item);
        return;
      }
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

  function modelHits(data) {
    const model = String((data && data.model) || "").trim();
    const provider = String((data && data.provider) || "").trim();
    if (!model) return [];
    return (state.modelCatalog || []).filter((m) => m.model === model && (!provider || m.provider === provider));
  }

  function modelTrigger(data, aside) {
    return window.XBotProviders.trigger({api, data, info: modelHits(data)[0], aside, manage: () => { if (!modal.hidden) closeModal(); openSettings("providers"); }, onBound: async () => {state.modelCatalog = null; await refreshLists(); await loadPanel();}});
  }

  function detailsView(data) {
    const wrap = document.createDocumentFragment();
    if (data.persona) {
      const duty = el("div", "psec");
      duty.append(el("h4", "", "职责"), el("p", "", data.persona));
      wrap.append(duty);
    }
    // Only the card's name and first line: the body lives in 设置 · 人设卡, and rebinding is in the drawer.
    const card = data.persona_card;
    if (card) {
      const sec = el("div", "psec");
      sec.append(el("h4", "", "人设卡"), el("p", "", card.name));
      if (card.preview) sec.append(el("p", "psec-weak", card.preview));
      wrap.append(sec);
    }
    const section = el("div", "psec");
    const snap = state.ctxSnap;
    const meta = [];
    if (snap?.ceiling > 0) meta.push("窗口 " + ctxK(snap.ceiling));
    if (snap?.target > 0) meta.push("后台整理线 " + ctxK(snap.target));
    let aside = null;
    if (meta.length) aside = el("span", "v", meta.join(" · "));
    else if (modelHits(data).some((m) => m.certified)) {
      aside = el("span", "v cert");
      aside.append(icon("check"), document.createTextNode("已认证"));
    }
    section.append(el("h4", "", "模型"), modelTrigger(data, aside));
    wrap.append(section);
    return wrap;
  }

  function libraryView(data) {
    const view = el("div", "memory-view");
    const memory = data.memory;
    if (!memory) { view.append(el("p", "hint", "正在加载…")); return view; }
    view.append(el("h3", "memory-heading", "常驻"));
    for (const [kind, label] of [["person", "关于你"], ["bot", "关于本 Bot"]]) {
      const doc = memory[kind];
      const section = el("section", "memory-document");
      const head = el("div", "memory-document-head");
      head.append(el("h4", "", label));
      if (doc && !doc.empty) head.append(memoryFileLink(data.name, kind, "打开文件", doc.path));
      section.append(head);
      if (!doc || doc.empty) section.append(el("p", "hint memory-empty", "还没有常驻记忆"));
      else { const body = el("div", "prose memory-markdown"); appendMarkdown(body, doc.markdown); section.append(body); }
      view.append(section);
    }
    const experiences = el("section", "memory-experiences");
    experiences.append(el("h3", "memory-heading", "经历"));
    const rows = el("div", "memory-experience-list");
    const append = (items) => {
      for (const item of items) {
        const row = el("details", "memory-experience");
        const head = el("summary");
        const at = new Date(item.created_at);
        const time = el("time", "", Number.isNaN(at.getTime()) ? item.created_at : at.toLocaleString([], {year:"numeric", month:"2-digit", day:"2-digit", hour:"2-digit", minute:"2-digit", hour12:false}));
        time.dateTime = item.created_at;
        head.append(time, el("span", "memory-summary", item.summary || ""), icon("chevron"));
        row.append(head);
        if (item.focus) row.append(el("p", "memory-focus", item.focus));
        if (item.segment) row.append(el("p", "memory-segment", item.segment));
        rows.append(row);
      }
    };
    const first = memory.experiences || {items:[]};
    append(first.items);
    if (!first.items.length) rows.append(el("p", "hint memory-empty", "还没有经历"));
    experiences.append(rows);
    let before = first.next_before;
    if (before) {
      const earlier = el("button", "memory-earlier", "更早的经历");
      earlier.type = "button";
      const error = el("p", "memory-page-error"); error.hidden = true;
      earlier.addEventListener("click", async () => {
        const selection = state.sel;
        earlier.disabled = true; error.hidden = true;
        try {
          const page = await api("/api/bots/" + encodeURIComponent(data.name) + "/experiences?before=" + before);
          if (state.sel !== selection || !view.isConnected) return;
          append(page.items); before = page.next_before;
          if (!before) earlier.remove();
        } catch (e) {
          if (state.sel !== selection || !view.isConnected) return;
          error.textContent = "经历加载失败，请重试"; error.hidden = false;
        } finally { earlier.disabled = false; }
      });
      experiences.append(earlier, error);
    }
    view.append(experiences);
    const footer = el("footer", "memory-footer");
    if (memory.month_log && !memory.month_log.empty) {
      const link = memoryFileLink(data.name, "month-log", "打开本月流水", memory.month_log.path);
      link.classList.add("memory-log"); footer.append(link);
    } else footer.append(el("span", "", "本月还没有流水"));
    view.append(footer);
    return view;
  }

  function memoryFileLink(name, kind, label, path) {
    const url = "/api/bots/" + encodeURIComponent(name) + "/memory-files/" + kind;
    const link = el("a", "memory-file", label);
    link.href = url; link.target = "_blank"; link.rel = "noopener"; link.title = path;
    if (state.app) link.addEventListener("click", async (event) => {
      event.preventDefault();
      try { await api(url + "/open", {method:"POST"}); }
      catch (e) { setStatus(visibleError(e), true); }
    });
    return link;
  }

  function elapsedWork(started, now = Date.now()) {
    const at = Date.parse(started);
    if (!Number.isFinite(at) || at <= 0) return "";
    const seconds = Math.max(0, Math.floor((now - at) / 1000));
    return seconds < 60 ? seconds + " 秒" : Math.floor(seconds / 60) + " 分" + (seconds % 60 ? " " + seconds % 60 + " 秒" : "");
  }

  // A routine run is a record, not speech: its wake (missed summaries too)
  // and a text-only SILENT reply never reach the user. Only what the bot says
  // to the user stays in the chat, and the task tab lists no runs.
  // Transcript snapshots are replaced, never edited in place.
  const routineCache = new WeakMap();
  function routineRuns(messages) {
    if (routineCache.has(messages)) return routineCache.get(messages);
    const pending = new Set(), hidden = new Set();
    messages.forEach((m, i) => {
      const branch = m.branch || "";
      if (m.role === "user") {
        if (routineOf(m)) { hidden.add(i); pending.add(branch); }
        else if (!unwrap(m.content || "").untrusted && !m.internal) pending.delete(branch);
      }
      if (m.role !== "assistant" || !m.content || (m.tool_calls || []).length || !pending.has(branch)) return;
      if (m.content.trim() === "SILENT" && !(m.images || []).length && !(m.audios || []).length) hidden.add(i);
      pending.delete(branch);
    });
    const result = {hidden}; routineCache.set(messages, result); return result;
  }

  function taskTypeLabel(ag) {
    const type = String(ag.type || "").trim();
    const labels = {research: "调研", dev: "开发", general: "通用", routine: "例程"};
    return Object.hasOwn(labels, type) ? labels[type] : (type && type !== ag.id ? clip(type, 24) : "子代理");
  }

  function taskTitle(ag) {
    const first = String(ag.task || "").trim().split(/[\r\n]/, 1)[0];
    const title = plainText(first);
    return title && title !== ag.id ? clip(title, 24) : taskTypeLabel(ag);
  }

  function taskSelected(host) {
    const selection = document.getSelection?.();
    return selection && !selection.isCollapsed && selection.rangeCount && selection.getRangeAt(0).intersectsNode(host);
  }

  function taskValue(host, text) {
    text = String(text || "");
    if (host.textContent !== text && !taskSelected(host)) host.textContent = text;
  }

  function taskProgress(ag) {
    const text = String(ag.current_step || "").trim();
    const title = taskTitle(ag).replace(/…$/, "");
    return title && plainText(text).replace(/^在\s*/, "").startsWith(title) ? "" : text;
  }

  function taskText(host, text) {
    text = String(text || "");
    if (host.dataset.text === text || taskSelected(host)) return;
    host.dataset.text = text;
    const parts = [...text.matchAll(/`([^`]+)`|((?<![^\s，。；：（(])(?:~\/|\/|[A-Za-z]:\\)[^\s，。·`]+)/g)];
    if (!parts.length) { host.textContent = text; return; }
    host.classList.add("task-text"); host.replaceChildren();
    let at = 0;
    for (const m of parts) { host.append(document.createTextNode(text.slice(at, m.index)), el("code", "", m[1] || m[2])); at = m.index + m[0].length; }
    host.append(document.createTextNode(text.slice(at)));
  }

  function taskStatusIcon(ag) {
    if (ag.status === "failed") return "x";
    if (["cancelled", "canceled", "interrupted"].includes(ag.status)) return "ban";
    return ag.status === "queued" ? "clock" : "circle-check";
  }

  function taskAvatar(ag, size = "") { return ag.type === "routine" ? icon("alarm-clock") : avatar(ag.id, null, size); }

  function paintGreeting() {
    const host = $("greeting");
    if (!host) return;
    const statuses = Object.values(stStatus.map);
    const busy = statuses.filter(st => st.state === "running" || st.state === "awaiting");
    const hour = new Date().getHours();
    const hello = hour < 6 ? "夜深了" : hour < 12 ? "上午好" : hour < 18 ? "下午好" : "晚上好";
    const sig = JSON.stringify([hello, state.ownerName, busy.map(s => s.bot)]);
    if (host.dataset.sig === sig) return;
    host.dataset.sig = sig;
    host.replaceChildren(document.createTextNode(hello + (state.ownerName ? "，" + state.ownerName : "") + "。"));
    // 状态只写「有 N 个 Bot 在忙」，有忙才出；不写已完成类状态。
    if (busy.length) {
      host.append(el("br"), el("span", "g", "有 "));
      const stack = el("span", "greet-stack");
      for (const st of busy.slice(0, 3)) stack.append(avatar(st.bot, paintOf(st.bot), "xs"));
      if (busy.length > 3) stack.append(el("span", "greet-more", "+" + (busy.length - 3)));
      host.append(stack, document.createTextNode(" " + busy.length + " 个 Bot"), el("span", "g", " 在忙。"));
    }
  }

  function paintWorkStatus() {
    const host = $("work-status");
    const selected = state.sel?.type === "bot" && !state.peer;
    const agents = selected && state.panelData?.name === state.sel.id ? (state.panelData.agents || []).filter(a => ["running", "awaiting"].includes(a.status)) : [];
    const signature = JSON.stringify(agents.map(a => [a.id, a.type, a.task, a.steps, a.current_step, a.started_at]));
    if (host.dataset.sig === signature) return;
    host.dataset.sig = signature;
    host.replaceChildren();
    if (!agents.length) return;
    const row = el("button", "wrow"); row.type = "button"; row.title = "打开任务";
    row.addEventListener("click", () => { state.tab = "computer"; rememberPanel(true); panelOpen(true); paintPanel(); });
    const stack = el("span", "work-stack");
    for (const ag of agents.slice(0, 3)) stack.append(taskAvatar(ag, "xs"));
    if (agents.length > 3) stack.append(el("span", "", "+" + (agents.length - 3)));
    row.append(stack);
    const started = agents.map(a => a.started_at).filter(t => Date.parse(t) > 0).sort()[0];
    if (agents.length === 1) {
      const ag = agents[0];
      const name = el("b"), step = el("span", "work-step"); taskText(name, taskTitle(ag)); taskText(step, taskProgress(ag)); step.hidden = !taskProgress(ag);
      row.append(name, step, el("span", "sep", "·"), el("span", "", "第 " + Math.max(1, ag.steps || 0) + " 步"));
    } else { const many = el("span", "work-many"); many.append(el("b", "", agents.length + " 个子代理"), el("span", "", "在跑")); row.append(many); }
    if (started) { const duration = el("span", "el"); duration.dataset.workSince = started; duration.dataset.longest = agents.length > 1 ? "true" : ""; row.append(el("span", "sep", "·"), duration); }
    row.append(icon("chevron")); host.append(row); updateWorkTimes();
  }

  function slowToolNotice(current, messages, pending, now = Date.now()) {
    const since = Date.parse(current?.since);
    if (current?.state !== "running" || !current.tool || !current.tool_summary || !Number.isFinite(since) || now - since < 3000) return "";
    const interjected = pending.some(row => !row.keepFailed && Date.parse(row.dataset.time) > since) ||
      messages.some(m => m.role === "user" && !m.internal && !unwrap(m.content || "").untrusted && Date.parse(m.time) > since);
    if (!interjected) return "";
    if (current.tool === "todo") return "正在更新待办，已 " + elapsedWork(current.since, now) + "。你的话会接在它后面，返回后马上回你。";
    return "在等 `" + String(current.tool_summary).replace(/`/g, "") + "` 返回，已 " + elapsedWork(current.since, now) + "。你的话会接在它后面，返回后马上回你。";
  }

  function updateWorkTimes() {
    for (const n of document.querySelectorAll("[data-work-since]")) taskText(n, (n.dataset.workPrefix || (n.dataset.longest ? "最久 " : "")) + elapsedWork(n.dataset.workSince));
    const inner = thread.querySelector(".thread-inner");
    const current = state.sel?.type === "bot" && !state.peer && stStatus.map[state.sel.id];
    const text = slowToolNotice(current, state.messages || [], state.pendingSends);
    let slow = inner?.querySelector(".slow-tool");
    if (!text) { slow?.remove(); return; }
    if (!inner) return;
    if (!slow) { slow = el("div", "slow-tool"); slow.append(icon("hourglass"), el("span")); }
    taskText(slow.querySelector("span"), niceName(state.sel.id) + " " + text);
    if (inner.lastElementChild !== slow) inner.append(slow);
  }

  async function refreshWork() {
    paintGreeting();
    const selection = state.sel;
    if (!selection || selection.type !== "bot" || state.peer) { paintWorkStatus(); return; }
    const data = await fetchBotProfile(selection.id);
    if (state.sel !== selection) return;
    const prior = state.panelData;
    state.panelData = data;
    paintWorkStatus();
    const root = panelBody.querySelector(".task-view");
    if (root && state.tab === "computer") syncTaskView(root, data);
    else if (prior && state.tab === "details" && JSON.stringify(prior.routines) !== JSON.stringify(data.routines)) paintPanel();
    updateWorkTimes();
  }

  function updateTaskNotable(card, items) {
    const host = card.querySelector(".task-notable"), details = card.querySelector(".task-notable-details");
    const sig = JSON.stringify(items);
    if (host.dataset.notable === sig || taskSelected(host) || taskSelected(details)) return;
    host.dataset.notable = sig; host.replaceChildren(); details.replaceChildren();
    host.hidden = details.hidden = !items.length;
    if (!items.length) return;
    host.append(el("div", "notable-heading", "值得留意"));
    const icons = {delete:"trash-2",external_message:"send",write_outside:"file-pen",global_install:"package"};
    const labels = {delete:"删除",external_message:"外发消息",write_outside:"工作区外写入",global_install:"全局安装"};
    for (const item of items) {
      const row = el("div", "notable-row"), text = el("span", "notable-text", item.text);
      text.title = item.text;
      if (["write_outside", "delete"].includes(item.kind) && item.objects?.length === 1 && /[/\\]/.test(item.objects[0])) {
        const prefix = item.kind === "write_outside" ? "写入了 " : "删除了 ";
        if (item.text.startsWith(prefix)) {
          text.classList.add("has-path"); text.replaceChildren(el("span", "notable-prefix", prefix), pathLabel(item.objects[0]));
        }
      }
      row.append(icon(icons[item.kind]), text); host.append(row);
      const group = el("div", "notable-detail"); group.append(el("div", "", labels[item.kind]));
      const list = el("ul");
      for (const object of item.objects || []) list.append(el("li", "", object));
      if (item.incomplete) list.append(el("li", "", "部分调用未提供对象，或对象明细超过记录上限"));
      group.append(list); details.append(group);
    }
  }

  function taskCard(ag) {
    const card = el("div", "task-record"); card.dataset.agent = ag.id;
    const compact = el("button", "recent-task"); compact.type = "button";
    const name = el("b", "task-name recent-name");
    compact.append(icon("circle-check"), name, el("span", "recent-status"), icon("chevron"));
    const detail = el("div", "card tk");
    compact.addEventListener("click", () => { card.classList.toggle("expanded"); compact.setAttribute("aria-expanded", String(card.classList.contains("expanded"))); queuePathLayout(); });
    card.append(compact, detail);
    const head = el("div", "ch"), ct = el("div", "ct");
    const title = el("div", "task-heading"); title.append(el("span", "task-type ty")); ct.append(title);
    head.append(taskAvatar(ag, "xs"), ct, el("span", "task-state"));
    detail.append(head, el("div", "now"), el("div", "res"), el("div", "task-notable"), el("div", "task-error"));
    const info = el("details", "task-details"); info.hidden = ag.type === "routine";
    const infoTitle = el("summary", "", "任务详情"); infoTitle.append(icon("chevron"));
    info.append(infoTitle, el("p", "task-request"), el("code", "task-id"), el("div", "task-notable-details"));
    detail.append(info);
    const controls = el("div", "ca");
    const steer = el("button", "secondary task-guide"); steer.type = "button"; steer.append(icon("corner-down-right"), document.createTextNode("引导"));
    const stop = el("button", "secondary task-stop"); stop.type = "button";
    const cancel = el("button", "secondary task-cancel"); cancel.type = "button"; cancel.append(icon("x"), document.createTextNode("取消"));
    stop.append(icon("square"), document.createTextNode("停止"));
    confirmTaskStop(stop, () => controlAgent(ag.id, "cancel", ""));
    cancel.addEventListener("click", () => controlAgent(ag.id, "cancel", ""));
    controls.append(steer, stop, cancel);
    const editor = el("form", "steer-editor"); editor.hidden = true;
    const field = el("input"); field.placeholder = "追加指示"; field.maxLength = 4000;
    const send = el("button", "secondary", "发送"); send.type = "submit";
    editor.append(field, send); detail.append(controls, editor);
    bindTaskEditor(card, ag.id);
    updateTaskCard(card, ag); return card;
  }

  function bindTaskEditor(card, id) {
    const editor = card.querySelector(".steer-editor"), guide = card.querySelector(".task-guide"), field = editor.querySelector("input"), send = editor.querySelector("button");
    const close = () => { editor.hidden = true; guide.setAttribute("aria-expanded", "false"); };
    guide.setAttribute("aria-expanded", "false");
    guide.addEventListener("click", () => { editor.hidden = !editor.hidden; guide.setAttribute("aria-expanded", String(!editor.hidden)); if (!editor.hidden) field.focus(); });
    editor.addEventListener("keydown", ev => { if (ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); close(); guide.focus(); } });
    editor.addEventListener("submit", async ev => {
      ev.preventDefault(); if (!field.value.trim() || send.disabled) return;
      send.disabled = true;
      try { if (await controlAgent(id, "steer", field.value)) { field.value = ""; close(); guide.focus(); } }
      finally { send.disabled = false; }
    });
  }

  function updateTaskCard(card, ag) {
    const hot = ["running", "awaiting", "queued"].includes(ag.status);
    card.classList.toggle("finished", !hot);
    const title = taskTitle(ag);
    const compact = card.querySelector(".recent-task");
    const name = card.querySelector(".task-name"), heading = card.querySelector(".task-heading");
    if (hot && name.parentElement !== heading) heading.prepend(name);
    else if (!hot && name.parentElement !== compact) compact.insertBefore(name, compact.querySelector(".recent-status"));
    if (compact.dataset.state !== ag.status) { compact.dataset.state = ag.status; compact.firstElementChild.replaceWith(icon(ag.type === "routine" ? "alarm-clock" : taskStatusIcon(ag))); }
    taskValue(card.querySelector(".recent-status"), ag.finished_at ? when(ag.finished_at) : taskStatus(ag.status));
    card.querySelector(".recent-task").classList.toggle("failed", ag.status === "failed");
    card.querySelector(".recent-task").setAttribute("aria-expanded", String(card.classList.contains("expanded")));
    taskText(card.querySelector(".task-name"), title);
    const type = card.querySelector(".task-type");
    taskValue(type, taskTypeLabel(ag)); type.hidden = !ag.type || ag.type === "routine" || type.textContent === title;
    type.className = "task-type ty type-" + ({__proto__:null,research:"res",dev:"dev",general:"gen"}[ag.type] || "own");
    taskValue(card.querySelector(".task-request"), ag.task);
    taskValue(card.querySelector(".task-id"), ag.id);
    const stateLabel = card.querySelector(".task-state");
    const running = ["running", "awaiting"].includes(ag.status);
    const text = running ? "在跑" : taskStatus(ag.status);
    if (stateLabel.dataset.status !== ag.status && !taskSelected(stateLabel)) {
      stateLabel.dataset.status = ag.status; stateLabel.replaceChildren();
      if (["running", "awaiting"].includes(ag.status)) stateLabel.append(spinIcon(13));
      else stateLabel.append(icon(taskStatusIcon(ag)));
    }
    let label = stateLabel.querySelector(".state-text");
    if (!label) { label = el("span", "state-text"); stateLabel.append(label); }
    taskValue(label, text + (!hot && ag.finished_at ? " · " + when(ag.finished_at) : ""));
    stateLabel.classList.toggle("failed", ag.status === "failed");
    const now = card.querySelector(".now");
    let stepText = hot ? [taskProgress(ag), ag.steps ? "第 " + ag.steps + " 步" : ""].filter(Boolean).join(" · ") : "";
    if (running && Date.parse(ag.started_at) > 0) {
      now.dataset.workSince = ag.started_at; now.dataset.workPrefix = stepText ? stepText + " · " : "";
      stepText = now.dataset.workPrefix + elapsedWork(ag.started_at);
    } else { delete now.dataset.workSince; delete now.dataset.workPrefix; }
    taskText(now, stepText); now.hidden = !stepText;
    const result = card.querySelector(".res");
    taskValue(result, hot ? "" : ag.summary === "SILENT" ? "没什么要说的" : (ag.status === "failed" ? zhError(ag.summary || "") : plainText(displayText(ag.summary || "")))); result.hidden = !result.textContent;
    result.classList.toggle("failed", ag.status === "failed");
    updateTaskNotable(card, hot ? [] : (ag.notable || []));
    const rawError = card.querySelector(".task-error");
    const raw = ag.status === "failed" ? ag.summary || "" : "";
    if (rawError.dataset.raw !== raw) {
      rawError.dataset.raw = raw; rawError.replaceChildren();
      errorDetails(rawError, raw, result.textContent);
      const toggle = rawError.querySelector("button");
      if (toggle) { toggle.replaceChildren(document.createTextNode("原始错误"), icon("chevron")); toggle.classList.add("raw-error-toggle"); }
    }
    rawError.hidden = !rawError.childElementCount;
    card.querySelector(".ca").hidden = !hot;
    card.querySelector(".task-guide").hidden = !running; card.querySelector(".task-stop").hidden = !running; card.querySelector(".task-cancel").hidden = ag.status !== "queued";
    if (!running) { card.querySelector(".steer-editor").hidden = true; card.querySelector(".task-guide").setAttribute("aria-expanded", "false"); }
  }

  const routineChanges = new Set();
  function routineCard(data, rt) {
    const card = el("div", "routine task-routine"); card.dataset.routine = rt.id;
    const copy = el("div", "ct"); copy.append(el("b", "routine-name"), el("span", "s2 routine-time"));
    const toggle = el("button", "tog"); toggle.type = "button"; toggle.setAttribute("role", "switch");
    const notice = el("span", "routine-error"); notice.hidden = true; notice.setAttribute("role", "status");
    const skills = el("div", "routine-skills");
    const summary = el("button", "routine-skills-summary"); summary.type = "button"; summary.setAttribute("aria-expanded", "false");
    const editor = el("form", "routine-skills-editor"); editor.hidden = true;
    const label = el("label", "", "运行时加载的技能"), input = el("input"); input.type = "text"; input.autocomplete = "off";
    label.append(input);
    const help = el("p", "routine-skills-help", "填技能名或 ID，用逗号分隔，按顺序加载。留空不加载；找不到或已停用的技能会在运行时说明。");
    const error = el("p", "routine-skills-error"); error.hidden = true; error.setAttribute("role", "status");
    const actions = el("div", "routine-skills-actions");
    const cancel = el("button", "", "取消"); cancel.type = "button";
    const save = el("button", "primary", "保存"); save.type = "submit";
    actions.append(cancel, save); editor.append(label, help, error, actions); skills.append(summary, editor);
    const closeEditor = () => {
      editor.hidden = true; summary.hidden = false; summary.setAttribute("aria-expanded", "false"); summary.focus();
    };
    summary.onclick = () => {
      input.value = (card.routine.skills || []).join(", "); error.hidden = true;
      summary.hidden = true; summary.setAttribute("aria-expanded", "true"); editor.hidden = false; input.focus();
    };
    cancel.onclick = closeEditor;
    editor.addEventListener("keydown", e => {
      if (e.key === "Escape") { e.stopPropagation(); e.preventDefault(); if (!save.disabled) closeEditor(); }
    });
    editor.onsubmit = async e => {
      e.preventDefault(); if (save.disabled) return;
      const refs = input.value.split(/[,，]/).map(s => s.trim()).filter(Boolean);
      input.disabled = cancel.disabled = save.disabled = true; error.hidden = true;
      try {
        const changed = await api("/api/bots/" + encodeURIComponent(data.name) + "/routines/" + encodeURIComponent(rt.id), {method: "PATCH", body: {skills: refs}});
        card.routine.skills = changed.skills; updateRoutineCard(card, card.routine); closeEditor();
      } catch (e) { error.textContent = "没保存成功：" + zhError(e.message); error.hidden = false; }
      finally { input.disabled = cancel.disabled = save.disabled = false; }
    };
    toggle.addEventListener("click", async () => {
      const before = card.routine.enabled !== false; const desired = !before;
      clearTimeout(card.failureTimer); notice.getAnimations().forEach(a => a.cancel()); notice.hidden = true;
      routineChanges.add(rt.id); toggle.disabled = true; toggle.classList.toggle("on", desired); toggle.setAttribute("aria-checked", String(desired));
      try {
        const changed = await api("/api/bots/" + encodeURIComponent(data.name) + "/routines/" + encodeURIComponent(rt.id), {method: "PATCH", body: {enabled: desired}});
        card.routine.enabled = changed.enabled;
      } catch (e) {
        card.routine.enabled = before;
        notice.style.opacity = "1"; notice.hidden = false;
        notice.textContent = "没改成，已恢复为" + (before ? "开启。" : "关闭。");
        card.failureTimer = setTimeout(() => { if (matchMedia("(prefers-reduced-motion: reduce)").matches) { notice.hidden = true; return; } const a = notice.animate([{opacity:1},{opacity:0}], {duration:120,fill:"forwards"}); a.finished.then(() => { notice.hidden = true; }).catch(() => {}); }, 4000);
      }
      finally { routineChanges.delete(rt.id); toggle.disabled = false; updateRoutineCard(card, card.routine); }
    });
    card.append(icon("alarm-clock"), copy, toggle, notice, skills); updateRoutineCard(card, rt); return card;
  }

  function updateRoutineCard(card, rt) {
    card.routine = rt;
    taskValue(card.querySelector(".routine-name"), rt.name || "例程");
    taskValue(card.querySelector(".routine-time"), rt.at ? when(rt.at) : cronText(rt.cron, true) + (rt.zone === "UTC" ? " · UTC" : ""));
    taskValue(card.querySelector(".routine-skills-summary"), rt.skills?.length ? "技能：" + rt.skills.join(" → ") : "添加技能");
    if (routineChanges.has(rt.id)) return;
    const toggle = card.querySelector(".tog"), enabled = rt.enabled !== false;
    toggle.classList.toggle("on", enabled); toggle.setAttribute("aria-checked", String(enabled)); toggle.setAttribute("aria-label", (enabled ? "停用" : "启用") + "「" + (rt.name || "例程") + "」");
  }

  function syncTaskView(root, data) {
    const agents = data.agents || [];
    const sections = [
      ["进行中", agents.filter(a => ["running", "awaiting"].includes(a.status)), "agent"],
      ["排队中", agents.filter(a => a.status === "queued"), "agent"],
      ["最近结束", agents.filter(a => !["running", "awaiting", "queued"].includes(a.status)).sort((a,b) => Date.parse(b.finished_at || 0) - Date.parse(a.finished_at || 0)).slice(0,20), "agent"],
      ["例程", data.routines || [], "routine"],
    ];
    const hooks = webhookSection(root); syncWebhookBase(root, data);
    if (!agents.length && !sections[3][1].length) { if (!root.querySelector(".panel-empty")) root.replaceChildren(panelEmpty("tasks", "没有任务", "让它「每天 8 点提醒我」，这里就会有一个例程。"), hooks); return; }
    root.querySelector(".panel-empty")?.remove();
    const cards = new Map([...root.querySelectorAll("[data-agent], [data-routine]")].map(n => [n.dataset.agent ? "agent:" + n.dataset.agent : "routine:" + n.dataset.routine, n]));
    const emptyActive = !sections[0][1].length && !sections[1][1].length;
    let idle = root.querySelector(".task-idle");
    if (emptyActive && !idle) { idle = el("div", "task-idle", "现在没有在跑或排队的任务。"); root.prepend(idle); }
    else if (!emptyActive) idle?.remove();
    const keep = new Set();
    for (const [label, items, kind] of sections) {
      let section = [...root.children].find(n => n.dataset.section === label);
      if (!section) { section = el("section", "task-section"); section.dataset.section = label; section.append(el("h4"), el("div", "task-items")); root.append(section); }
      section.hidden = emptyActive && ["进行中", "排队中"].includes(label);
      taskValue(section.querySelector("h4"), label + " · " + items.length);
      const list = section.querySelector(".task-items");
      let hint = list.querySelector(".pnone");
      if (items.length) hint?.remove();
      else if (!hint) list.append(el("div", "pnone", label === "进行中" ? "现在没有在跑的任务。" : label === "排队中" ? "现在没有排队的任务。" : label === "例程" ? "还没有例程。" : "还没有结束的任务。"));
      let next = list.firstElementChild;
      for (const item of items) {
        const key = kind + ":" + item.id; keep.add(key);
        const card = cards.get(key) || (kind === "agent" ? taskCard(item) : routineCard(data, item));
        if (kind === "agent") updateTaskCard(card, item); else updateRoutineCard(card, item);
        if (card !== next) list.insertBefore(card, next);
        next = card.nextElementSibling;
      }
    }
    for (const [key, card] of cards) if (!keep.has(key)) card.remove();
    if (root.lastElementChild !== hooks) root.append(hooks);
  }

  function computerView(data) {
    const root = el("div", "task-view"); root.dataset.bot = data.name; syncTaskView(root, data); return root;
  }

  // The task tab's Webhook section. It loads once per task view and repaints
  // on its own actions, or when the poll says the gateway came or went.
  function webhookSection(root) {
    let sec = root.querySelector(':scope > [data-section="webhook"]');
    if (sec) return sec;
    sec = el("section", "task-section hook-section"); sec.dataset.section = "webhook";
    const head = el("h4", "hook-head"); const add = el("button", "text-btn hook-new", "新建"); add.type = "button";
    add.onclick = () => newWebhook(root);
    head.append(el("span", "", "Webhook"), add);
    sec.append(head, el("p", "hook-lead", "外网请求可叫醒这个 Bot"), el("div", "hook-body"));
    root.hookDetail = null; loadWebhooks(root);
    paintWebhooks(root, sec); return sec;
  }

  // The gateway's public base follows its standby; when the poll's differs
  // from the one the list or the open detail was read with, reload it so
  // neither keeps handing out a dead address. Only a successful read records
  // the base, so a failed refresh is retried on the next poll.
  function syncWebhookBase(root, data) {
    if (!("webhook_public" in data) || root.hookBase === undefined) return;
    const base = data.webhook_public, d = root.hookDetail;
    if (root.hookBase !== base) loadWebhooks(root);
    if (d && (base ? !d.public_url?.startsWith(base + "/") : d.public_url)) openWebhook(root, d.name, true);
  }

  const hookRoute = (bot, name) => "/api/bots/" + encodeURIComponent(bot) + "/webhooks" + (name ? "/" + encodeURIComponent(name) : "");
  const hookAuth = a => a === "bearer" ? "令牌" : "签名校验";

  async function loadWebhooks(root) {
    const bot = root.dataset.bot;
    try { const data = await api(hookRoute(bot)); if (root.dataset.bot === bot) { root.hooks = data.hooks || []; root.hookBase = data.public || ""; } }
    catch (e) { root.hooks = root.hooks || []; root.hookError = visibleError(e, "读不到 Webhook"); }
    paintWebhooks(root);
  }

  function paintWebhooks(root, sec = root.querySelector(':scope > [data-section="webhook"]')) {
    if (!sec) return;
    const body = sec.querySelector(".hook-body"), detail = root.hookDetail;
    sec.querySelector(".hook-new").hidden = !!detail || !root.hooks?.length;
    sec.querySelector(".hook-lead").hidden = !!detail;
    if (detail) { body.replaceChildren(webhookDetail(root, detail)); return; }
    if (!root.hooks) { body.replaceChildren(el("div", "pnone", "正在读取…")); return; }
    const nodes = [];
    if (root.hookError) { nodes.push(el("div", "hook-error", root.hookError)); root.hookError = ""; }
    if (!root.hooks.length) {
      const empty = el("div", "hook-empty"); const add = el("button", "primary", "新建"); add.type = "button";
      add.onclick = () => newWebhook(root);
      empty.append(el("b", "", "还没有 Webhook"), el("span", "", "建一条后把地址给外网服务"), add);
      nodes.push(empty);
    }
    for (const h of root.hooks) {
      const row = el("button", "hook-row"); row.type = "button"; row.dataset.hook = h.name;
      const ct = el("span", "ct"); ct.append(el("b", "", h.name), el("span", "s2", h.url.replace(/^https?:\/\//, "")));
      row.append(ct, el("span", "s2 hook-auth", hookAuth(h.auth)), icon("chevron"));
      row.onclick = () => openWebhook(root, h.name);
      nodes.push(row);
    }
    body.replaceChildren(...nodes);
  }

  async function openWebhook(root, name, keep = false) {
    try {
      const h = await api(hookRoute(root.dataset.bot, name));
      if (keep && root.hookDetail?.name !== name) return;
      root.hookDetail = h; if (!keep) root.hookReveal = false;
    }
    catch (e) { if (keep) return; root.hookError = visibleError(e, "读不到这条 Webhook"); }
    paintWebhooks(root);
  }

  async function copyHookText(text) { showToast(await copyText(text) ? "已复制" : "复制失败，请手动选择"); }

  function hookCopy(label, text, shown = text) {
    const box = el("button", "hook-copy"); box.type = "button";
    box.append(el("span", "k", label), el("span", "v", shown));
    box.onclick = () => copyHookText(text);
    return box;
  }

  // The address to hand out: the gateway's public one when there is one,
  // the local one tucked below it; without a gateway, a way to get one.
  function hookAddress(h) {
    const box = el("div", "hook-addr");
    box.append(hookCopy(h.public_url ? "公网地址" : "地址", h.url));
    if (h.public_url) {
      const local = el("details", "hook-local"); local.append(el("summary", "", "本机"), hookCopy("本机地址", h.local_url));
      box.append(local);
    } else {
      const go = el("button", "hook-gw"); go.type = "button"; go.append(el("span", "", "登录网关后可得到公网地址"), icon("chevron"));
      go.onclick = () => { if (!modal.hidden) closeModal(); openSettings("devices"); };
      box.append(go);
    }
    return box;
  }

  function webhookDetail(root, h) {
    const box = el("div", "hook-detail");
    const back = el("button", "text-btn hook-back", "← Webhook"); back.type = "button";
    back.onclick = () => { root.hookDetail = null; paintWebhooks(root); };
    const name = el("div", "hook-block"); name.append(el("div", "hook-label", "名称"), el("b", "hook-name", h.name));
    const addr = el("div", "hook-block"); addr.append(el("div", "hook-label", "地址 · 点击复制"), hookAddress(h));
    const auth = el("div", "hook-block"); auth.append(el("div", "hook-label", "校验"), el("span", "", hookAuth(h.auth)));
    const secret = el("div", "hook-block");
    const toggle = el("button", "text-btn hook-reveal", root.hookReveal ? "隐藏" : "显示"); toggle.type = "button";
    toggle.onclick = () => { root.hookReveal = !root.hookReveal; paintWebhooks(root); };
    secret.append(el("div", "hook-label", "密钥 · 点击复制"), hookCopy("密钥", h.secret, root.hookReveal ? h.secret : "•".repeat(12) + h.secret.slice(-4)), toggle);
    const remove = el("button", "text-btn danger hook-remove", "撤掉"); remove.type = "button";
    remove.onclick = () => removeWebhook(root, h);
    box.append(back, name, addr, auth, secret, remove);
    return box;
  }

  function newWebhook(root) {
    openModal("新建 Webhook", [
      { type: "note", label: "名称会出现在地址里，创建后不可改。" },
      { name: "name", label: "名称", placeholder: "例如 github" },
      { name: "auth", type: "choice", label: "校验方式", options: [["hmac", "签名校验"], ["bearer", "令牌"]] },
    ], async (v) => {
      const name = (v.name || "").trim();
      if (!/^[a-z0-9][a-z0-9-]{0,63}$/.test(name)) throw new Error("用小写字母、数字和短横线，不要大写或下划线");
      let h;
      try { h = await api(hookRoute(root.dataset.bot), { method: "POST", body: { name, auth: v.auth } }); }
      catch (e) { throw new Error(e.message); }
      root.hooks = [...(root.hooks || []).filter(x => x.name !== h.name), h];
      paintWebhooks(root);
      // openModal closes its card once this returns; show the result after.
      setTimeout(() => webhookCreated(h));
    }, "创建");
  }

  function webhookCreated(h) {
    openModal("已创建", [{ type: "note", label: "请先复制保存。关掉后仍可在详情里再看。" }], async () => {}, "完成", () => {
      $("modal-cancel").hidden = true;
      modalFields.append(hookAddress(h), hookCopy("密钥", h.secret));
    });
  }

  function removeWebhook(root, h) {
    openModal("撤掉「" + h.name + "」？", [
      { type: "note", label: "之后用这条地址的请求将不再叫醒本 Bot。需要时可以再新建。" },
    ], async () => {
      try { await api(hookRoute(root.dataset.bot, h.name), { method: "DELETE" }); }
      catch (e) { throw new Error(e.message); }
      root.hookDetail = null;
      root.hooks = (root.hooks || []).filter(x => x.name !== h.name);
      paintWebhooks(root);
    }, "撤掉", () => { $("modal-ok").className = "text-btn danger"; });
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
      const [data, snap, inbox] = await Promise.all([
        fetchBotProfile(selection.id),
        api("/api/bots/" + encodeURIComponent(selection.id) + "/context").catch(() => ({ requests: [] })),
        api("/api/bots/" + encodeURIComponent(selection.id) + "/inbox").catch(() => ({ error: true })),
      ]);
      await loadModelCatalog();
      if (state.sel !== selection || request !== state.panelLoad) return;
      state.panelData = data;
      state.inboxData = inbox;
      state.ctxSnap = snap;
      paintPanel();
      paintWorkStatus();
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
  // Bumped whenever the modal closes, so work an old profile drawer started can tell it is gone.
  let profileDrawerGen = 0;

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
    const drawerGen = profileDrawerGen;
    const wrap = document.createElement("div");
    const head = avatarSection(data.name);
    const name = textInput(data.name, 64);
    name.dataset.profileFocus = "true";
    const description = textInput(data.description, 160);
    const duty = document.createElement("textarea");
    duty.value = data.persona || "";
    duty.rows = 8;
    duty.maxLength = 32768;
    duty.setAttribute("aria-label", "职责");
    const dutyBox = el("label", "field", "");
    dutyBox.append(duty);
    const dutyHint = el("p", "pc-pickhint", "这个 Bot 负责什么、边界在哪。口吻交给下面的人设卡。");
    // The card is bound the moment the menu changes; the drawer's save only covers the other fields.
    const cardMenu = document.createElement("select");
    cardMenu.setAttribute("aria-label", "人设卡");
    const cardOption = (card) => {
      const o = document.createElement("option");
      o.value = card.id;
      o.textContent = card.name + (card.default ? "（默认）" : "");
      return o;
    };
    const current = data.persona_card;
    if (current) cardMenu.append(cardOption(current));
    cardMenu.value = current ? current.id : "";
    cardMenu.disabled = true;
    api("/api/persona-cards").then((res) => {
      if (drawerGen !== profileDrawerGen) return;
      const cards = (res && res.cards) || [];
      cardMenu.replaceChildren(...cards.map(cardOption));
      const id = data.persona_card ? data.persona_card.id : "";
      if (id && !cards.some((c) => c.id === id)) cardMenu.prepend(cardOption(data.persona_card));
      cardMenu.value = id || (cards.find((c) => c.default) || {}).id || "";
      cardMenu.disabled = false;
    }).catch((e) => showErr(e, "没能读取人设卡"));
    cardMenu.addEventListener("change", async () => {
      err.hidden = true;
      const prev = data.persona_card ? data.persona_card.id : "";
      cardMenu.disabled = true;
      try {
        const res = await api("/api/bots/" + encodeURIComponent(data.name) + "/persona-card", { method: "PUT", body: { id: cardMenu.value } });
        data.persona_card = res && res.persona_card;
        loadPanel().catch(() => {});
      } catch (e) {
        cardMenu.value = prev;
        showErr(e, "没能选用这张人设卡");
      } finally {
        if (drawerGen === profileDrawerGen) cardMenu.disabled = false;
      }
    });
    const cardBox = el("label", "field", "");
    cardBox.append(cardMenu);
    const manage = el("button", "text-btn pc-manage", "管理人设卡");
    manage.type = "button";
    manage.addEventListener("click", () => { closeModal(); openSettings("persona"); });
    const pickHint = el("p", "pc-pickhint", "人设卡管口吻和分寸，叠在职责之上，选中马上生效。");
    pickHint.append(" ", manage);
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
      section("基本", head, labeled("名字", name), labeled("简介", description)),
      section("职责", dutyBox, dutyHint),
      section("人设卡", cardBox, pickHint),
      section("模型", modelTrigger(data)),
      err,
    );
    const showErr = (e, fallback) => { err.hidden = false; err.textContent = visibleError(e, fallback); };
    modalForm.onsubmit = async (ev) => {
      ev.preventDefault();
      err.hidden = true;
      const body = {
        name: name.value.trim(),
        description: description.value.trim(),
      };
      const nextDuty = duty.value.trim();
      if (!nextDuty) { showErr(null, "职责不能为空"); duty.focus(); return; }
      if (nextDuty !== (data.persona || "").trim()) body.persona = nextDuty;
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
    profileDrawerGen++;
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
      await loadPanel(); return true;
    } catch (e) {
      setStatus(visibleError(e), true); return false;
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
    // covers part of the conversation. The exposed conversation closes it.
    $("panel-scrim").hidden = !(panel.classList.contains("open") && !drawer);
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
    if (!state.sel || warming || state.streaming) return;
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

  // Progress shows the purpose: the first summary line (a shell's description),
  // never the tool name or the command. Empty means the dot stands alone.
  function workText(summary) {
    return String(summary || "").split("\n")[0].trim();
  }

  function setWorking(host, text) {
    let status = host.querySelector(".live-status");
    if (!status) {
      status = el("div", "meta live-status");
      const dots = el("span", "typing-dots");
      dots.setAttribute("aria-label", "正在回复");
      for (let i = 0; i < 3; i++) dots.append(el("i", ""));
      status.append(dots);
      host.append(status);
    }
    const name = status.querySelector(".working-name");
    if ((name?.textContent || "") === text) return;
    if (name) name.remove();
    if (text) status.append(el("span", "working-name", text));
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
    updateWorkTimes();
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
    body.append(prose);
    setWorking(body, "");
    row.append(body);
    markLiveGroup(inner, row);
    addBubbleTime(row, now);
    addTimeDivider(inner, row);
    inner.append(row);
    thread.scrollTop = thread.scrollHeight;
    return prose;
  }

  async function readSSE(res, selection, prose, onQueued, onLive) {
    const reader = res.body.getReader();
    const dec = new TextDecoder();
    let buf = "";
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
        if (event === "tool") {
          // Tool steps show only as the busy line; the trace has the detail.
          setWorking(prose.closest(".body"), workText(payload.summary));
        } else if (event === "result" && payload.name === "send_message_to_user" && payload.text) {
          // A message that landed is this bubble; the next one streams into a
          // fresh live row, as the transcript keeps one bubble per call.
          host.querySelector(".live-status")?.remove();
          appendMarkdown(prose, stripBotImages(displayText(payload.text)));
          host.parentElement.classList.remove("live");
          prose = liveBubble();
          onLive(prose);
          thread.scrollTop = thread.scrollHeight;
        } else if (event === "result" && payload.name === "bot_update_profile") {
          // The result arrives after persistence. Refresh faces even while the
          // streaming poll is paused, without holding up incoming text. The
          // selection guard keeps a delayed fetch from restoring an old header.
          refreshLists().then(() => {
            if (state.sel === selection && selection.type === "bot" && !state.peer) setHeader(selection.id);
          }).catch(() => {});
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

  function optimisticMessage(text, images, audios) {
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
    // The picture is already on the server by the time a send starts, so the
    // optimistic row points at it directly. Nothing local to revoke, and the
    // row after the repaint looks exactly the same.
    appendMessageBubbles(body, text, pastedURLs(state.sel, images), audios);
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

  async function sendText(text, pending, images, audios) {
    text = (text || "").trim();
    images = images || [];
    audios = audios || [];
    // A message sent while a reply is streaming interrupts it: the server forks the
    // running turn to the background and answers this one at once.
    if ((!text && !images.length && !audios.length) || !state.sel) return;
    const selection = state.sel;
    if (selection.type === "bot" && selection.history?.has_more_after && !state.streaming) {
      await loadThread("latest");
      if (state.sel !== selection) return;
      // Sending still works if reading history fails. Reconcile at the tail
      // when the turn settles, instead of losing the already submitted draft.
      selection.history = null;
      selection.transcriptETag = null;
    }
    ++state.threadLoad;
    selection.historyLoading = false;
    if (!pending) {
      // Resending a failed text from the composer replaces its failed row.
      state.pendingSends = state.pendingSends.filter((row) => {
        const stale = !row.accepted && row.querySelector(".send-failed") && row.dataset.sendText === text;
        if (stale) row.remove();
        return !stale;
      });
    }
    pending = pending || optimisticMessage(text, images, audios);
    pending.dataset.sendText = text;
    pending.baseline = ownerSends(text);
    const marker = pending.querySelector(".send-state");
    marker.className = "send-state";
    marker.replaceChildren(document.createTextNode("发送中"));
    announceSend("发送中");
    let prose = selection.type === "bot" ? liveBubble() : null;
    let live = prose?.closest(".msg.live");
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
    try {
      const payload = { text: text };
      if (images.length) payload.images = images;
      if (audios.length) payload.audios = audios;
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
        await readSSE(res, selection, prose, markQueued, (p) => { prose = p; live = p.closest(".msg.live"); });
      }
      if (state.sel !== selection) return;
      state.sig = "";
      clearQuote();
      settle();
      await loadThread();
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
      if (!composerText(input).trim() && text) {
        composerSet(text);
        composerRefreshHeight();
        marker.replaceChildren(el("span", "", (accepted ? "没送到，原文还在输入框里" : "没发出去，原文还在输入框里") + pictureNote));
        announceSend("没发出去，原文还在输入框里" + (images.length ? "，图片要重新贴一次" : ""));
      } else if (audios.length) {
        const retry=el("button","secondary","重试");retry.type="button";retry.onclick=()=>sendText(text,pending,images,audios);
        marker.replaceChildren(el("span","","语音没发出去"),retry);announceSend("语音没发出去，可以重试");
      } else if (!text && images.length) {
        marker.replaceChildren(el("span", "", accepted ? "没送到；图片要重新贴一次" : "没发出去；图片要重新贴一次"));
        announceSend("没发出去，图片要重新贴一次");
      } else {
        // A newer draft is in the composer: keep it, and let the owner bring this text back.
        const back = el("button", "secondary", "放回输入框");
        back.type = "button";
        back.addEventListener("click", () => {
          const cur = composerText(input);
          composerSet(cur.trim() ? cur.replace(/\s*$/, "\n") + text : text);
          composerRefreshHeight();
          syncComposer();
          input.focus();
          composerCaretTo(Infinity);
        });
        marker.replaceChildren(el("span", "", (accepted ? "没送到" : "没发出去") + pictureNote), back);
        announceSend("没发出去，可以放回输入框" + (images.length ? "，图片要重新贴一次" : ""));
      }
      syncComposer();
    } finally {
      live?.querySelector(".live-status")?.remove();
      if (prose && !prose.textContent && !live?.querySelector(".approval")) live?.remove();
      settle();
    }
  }

  function closeModal() {
    profileDrawerGen++;
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
        ? acts.querySelector(".panel-act:not(.danger)") || $("head-chip") : profileReturnFocus;
      profileReturnFocus = null;
      target.focus({ preventScroll: true });
    }
    modalForm.querySelector(".profile-templates")?.remove();
    modalError.hidden = true;
    modalError.textContent = "";
    modalForm.onsubmit = null;
    if (!$("setpage").hidden) $("setpage").querySelector(`[data-set="${settingsSection}"]`).focus();
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
    $("modal-cancel").hidden = false;
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

  function openExport(name) {
    if (!name) return;
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
      { name: "persona", label: "职责", multiline: true },
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
    const hasText = composerText(input).trim().length > 0;
    input.classList.toggle("is-empty", !hasText);
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
  }

  setInterval(updateWorkTimes, 1000);

  function closePops() {
    closeMessageMenu();
    closeAccount();
    closeNewMenu();
    closePanelMenu();
    closeFolderMenus();
    closeToolPop();
  }

  document.addEventListener("click", (ev) => {
    const inSub = panelSub && panelSub.contains(ev.target);
    if (panelMore && !inSub && !panelMore.contains(ev.target) && !panelMoreAnchor?.contains(ev.target)) {
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
      const session = await api("/api/session");
      state.version = session.version || "";
      state.tip = session.tip || "";
      state.app = !!session.app;
      syncDevicesSession(session);
      renderMe();
      showApp();
      await boot();
      startPoll();
      offerClaimOnce();
    } catch (e) {
      err.hidden = false;
      err.textContent = visibleError(e, "登录失败");
    }
  });

  function forgetOwner() {
    state.sel = null;
    state.knownNotes = null;
    state.listSig = "";
    try { sessionStorage.removeItem("xbot-selected-bot"); } catch (e) {}
  }
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
  $("account").addEventListener("click", () => {
    const open = $("account-menu").hidden;
    closePops();
    $("account-menu").hidden = !open;
    $("account").setAttribute("aria-expanded", open ? "true" : "false");
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
      $("head-chip").classList.remove("on");
      $("head-chip").setAttribute("aria-expanded", "false");
    }
    syncBackdrop();
  });

  backdrop.addEventListener("click", () => {
    state.panelTouched = true;
    sidebar.classList.remove("open");
    panel.classList.remove("open");
    $("head-chip").classList.remove("on");
    $("head-chip").setAttribute("aria-expanded", "false");
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
    $("head-chip").classList.toggle("on", open);
    $("head-chip").setAttribute("aria-expanded", open ? "true" : "false");
    $("head-chip").setAttribute("aria-label", open ? "收起信息" : "打开信息");
    setTip($("head-chip"), (open ? "收起信息" : "打开信息") + (isMac ? "  ⌘\\" : "  Ctrl+\\"));
    syncBackdrop();
  }

  $("head-chip").addEventListener("click", async () => {
    const open = !panel.classList.contains("open");
    rememberPanel(open);
    panelOpen(open);
    if (open && window.innerWidth < 768) sidebar.classList.remove("open");
    if (open) {
      try { await loadPanel(); } catch (e) { setStatus(visibleError(e), true); }
    }
  });

  document.addEventListener("keydown", (ev) => {
    if (!$("setpage").hidden) return;
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

  let settingsOpener = null;
  let settingsSection = "general";
  let settingsRequest = 0;
  let settingsLevel = null;
  const settingsNarrow = window.matchMedia("(max-width: 640px)");
  function paintSettingsBar() {
    const page = $("setpage"), home = page.dataset.nav === "home";
    const tab = page.querySelector(`[data-set="${settingsSection}"]`);
    $("settings-back").hidden = home;
    $("settings-back-label").textContent = settingsLevel ? settingsLevel.backLabel : "设置";
    $("settings-bar-title").textContent = home ? "设置" : settingsLevel ? settingsLevel.title : (tab?.textContent.trim() || "设置");
  }
  function showSettingsHome() {
    const page = $("setpage");
    ++settingsRequest;
    window.XBotAccount?.unmountSettings();
    window.XBotWebSearch?.unmount();
    window.XBotImageGen?.unmount();
    window.XBotSkills?.unmount();
    window.XBotPersonaCards?.unmount();
    window.XBotApps?.unmount();
    window.xbotDevices?.unmount();
    settingsLevel = null;
    settingsError(null);
    page.dataset.nav = "home";
    paintSettingsBar();
    page.querySelector(`[data-set="${settingsSection}"]`)?.focus({ preventScroll: true });
  }
  function settingsError(err) {
    const node = $("settings-error");
    node.textContent = err ? visibleError(err) : "";
    node.hidden = !err;
  }
  function closeSettings(restore = true) {
    ++settingsRequest;
    window.XBotAccount?.unmountSettings();
    window.XBotWebSearch?.unmount();
    window.XBotImageGen?.unmount();
    window.XBotSkills?.unmount();
    window.XBotPersonaCards?.unmount();
    $("setpage").hidden = $("setscrim").hidden = true;
    settingsLevel = null;
    window.xbotDevices?.unmount();
    shell.inert = false;
    if (restore) (settingsOpener?.isConnected && !settingsOpener.hidden ? settingsOpener : $("account")).focus({ preventScroll: true });
    settingsOpener = null;
  }
  async function openSettings(section = "general", { home = false, provider = "" } = {}) {
    const page = $("setpage");
    const opening = page.hidden;
    if (opening) settingsOpener = document.activeElement?.closest("#account-menu") ? $("account") : document.activeElement;
    closePops();
    hideTip();
    page.hidden = $("setscrim").hidden = false;
    shell.inert = true;
    settingsSection = section;
    const request = ++settingsRequest;
    settingsLevel = null;
    page.dataset.nav = home ? "home" : "pane";
    paintSettingsBar();
    settingsError(null);
    for (const tab of page.querySelectorAll("[data-set]")) {
      const on = tab.dataset.set === section;
      tab.classList.toggle("on", on);
      tab.setAttribute("aria-current", on ? "page" : "false");
    }
    for (const pane of page.querySelectorAll("[data-spane]")) pane.hidden = pane.dataset.spane !== section;
    if (section !== "devices") window.xbotDevices?.unmount();
    window.XBotAccount?.unmountSettings();
    window.XBotWebSearch?.unmount();
    window.XBotImageGen?.unmount();
    window.XBotSkills?.unmount();
    window.XBotPersonaCards?.unmount();
    window.XBotApps?.unmount();
    $("settings-theme").value = themeMode;
    for (const node of page.querySelectorAll("[data-settings-version]")) node.textContent = aboutVersion();
    if (opening) page.focus({ preventScroll: true });
    else if (settingsNarrow.matches && !home) $("settings-back").focus({ preventScroll: true });
    if (home) return;
    try {
      if (section === "account") {
        await window.XBotAccount.mountSettings($("settings-account"), () => request === settingsRequest);
      } else if (section === "perm") {
        $("settings-permissions").disabled = true;
        const data = await api("/api/permissions");
        if (request === settingsRequest) { paintPermissions(data); $("settings-permissions").disabled = false; }
      } else if (section === "search") {
        await window.XBotWebSearch.mount({api, host:$("settings-search"), isCurrent:()=>request===settingsRequest});
      } else if (section === "image-gen") {
        await window.XBotImageGen.mount({api, host:$("settings-image-gen"), isCurrent:()=>request===settingsRequest, manage:id=>openSettings("providers", {provider:id})});
      } else if (section === "skills") {
        await window.XBotSkills.mount({api, host:$("settings-skills"), isCurrent:()=>request===settingsRequest, onLevel:level=>{ if (request===settingsRequest) { settingsLevel = level; paintSettingsBar(); } }});
      } else if (section === "persona") {
        await window.XBotPersonaCards.mount({api, host:$("settings-persona"), isCurrent:()=>request===settingsRequest, onLevel:level=>{ if (request===settingsRequest) { settingsLevel = level; paintSettingsBar(); } }});
      } else if (section === "providers") {
 await window.XBotProviders.settings({api,host:$("settings-providers"),initialProvider:provider,isCurrent:()=>request===settingsRequest});
 } else if (section === "devices") {
        await window.xbotDevices.mount();
      } else if (section === "apps") {
        await window.XBotApps.mount({api, host:$("settings-apps"), isCurrent:()=>request===settingsRequest});
      } else if (section === "general") {
        await loadPush();
      }
    } catch (err) { if (request === settingsRequest) settingsError(err); }
  }
  window.XBotSettings = { open: openSettings, close: closeSettings, isOpen: () => !$("setpage").hidden };
  function paintPermissions(data) {
    const on = data.confirm === "table";
    const btn = $("settings-permissions");
    btn.classList.toggle("on", on);
    btn.setAttribute("aria-checked", String(on));
    document.querySelector("[data-perm-on]").hidden = !on;
    document.querySelector("[data-perm-hint]").textContent = on
      ? "下面几类会先弹出确认卡，你点了才继续。"
      : "Bot 删除、对外发消息等会直接做，做完在对话里告诉你。";
  }
  $("settings-permissions").addEventListener("click", async () => {
    const btn = $("settings-permissions");
    const confirm = btn.getAttribute("aria-checked") === "true" ? "off" : "table";
    btn.disabled = true;
    settingsError(null);
    try { paintPermissions(await api("/api/permissions", { method: "PUT", body: { confirm } })); }
    catch (err) { settingsError(err); }
    finally { btn.disabled = false; }
  });
  $("settings-theme").addEventListener("change", (ev) => applyTheme(ev.target.value));
  $("account-settings").prepend(icon("settings"));
  $("account-settings").addEventListener("click", () => openSettings("general", { home: settingsNarrow.matches }));
  $("settings-close").addEventListener("click", () => closeSettings());
  $("settings-close-m").addEventListener("click", () => closeSettings());
  $("settings-back").addEventListener("click", () => { if (settingsLevel) settingsLevel.back(); else showSettingsHome(); });
  // Widening the window from the phone home leaves no list to pick from: land on the section the desktop layout shows.
  settingsNarrow.addEventListener("change", (ev) => {
    if (!ev.matches && !$("setpage").hidden && $("setpage").dataset.nav === "home") openSettings(settingsSection);
  });
  $("setscrim").addEventListener("click", () => { if (!deviceLayerOpen() && modal.hidden) closeSettings(); });
  $("setpage").addEventListener("click", (ev) => {
    const tab = ev.target.closest("[data-set]");
    if (tab) openSettings(tab.dataset.set);
  });
  document.addEventListener("keydown", (ev) => {
    if ($("setpage").hidden || deviceLayerOpen() || !modal.hidden) return;
    if (ev.key === "Escape") { ev.preventDefault(); ev.stopImmediatePropagation(); closeSettings(); }
    else if (ev.key === "Tab") {
      const items = [...$("setpage").querySelectorAll('button:not(:disabled),select,a[href],input')].filter(n => n.getClientRects().length);
      const first = items[0], last = items.at(-1);
      if (ev.shiftKey && document.activeElement === first) { ev.preventDefault(); last.focus(); }
      else if (!ev.shiftKey && document.activeElement === last) { ev.preventDefault(); first.focus(); }
    }
  });
  function aboutVersion() { return (state.version || "dev") + (state.tip ? " · " + state.tip : ""); }

  $("settings-version").addEventListener("click", async (ev) => {
    if (await copyText(ev.currentTarget.textContent)) {
      const status = $("settings-version-status");
      status.textContent = "已复制";
      setTimeout(() => { status.textContent = ""; }, 2000);
    }
  });
  $("push-icon").append(icon("phone"));

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
      btn.classList.toggle("on", on);
      btn.setAttribute("aria-checked", on ? "true" : "false");
      btn.setAttribute("aria-label", on ? "关闭手机推送" : "开启手机推送");
    }
    if (hint) {
      hint.classList.remove("bad");
      hint.hidden = false;
      hint.textContent = "Bot 有话找你时，推送到这台设备";
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
    if (!text || (host.getAttribute("aria-expanded") === "true" && host !== $("head-chip"))) { hideTip(); return; }
    const rail = host.closest(".sidebar.rail");
    if (host.dataset.tipPlacement === "rail" && !rail) { hideTip(); return; }
    tipEl.textContent = text;
    tipEl.hidden = false;
    const r = host.getBoundingClientRect();
    const tw = tipEl.offsetWidth;
    const th = tipEl.offsetHeight;
    let left = r.left + (r.width / 2) - (tw / 2);
    left = Math.max(8, Math.min(left, window.innerWidth - tw - 8));
    const right = (host.dataset.tipPlacement === "right" || host.dataset.tipPlacement === "rail") && rail;
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
    if (!ev.target.closest(".new-menu")) closeNewMenu();
    if (!panelMoreAnchor?.contains(ev.target) && !ev.target.closest(".panel-more, .menu-sub")) closePanelMenu();
    if (!ev.target.closest(".folder-head") && !ev.target.closest(".folder-menu")) closeFolderMenus();
    if (!ev.target.closest(".tool-pop") && !ev.target.closest(".card")) closeToolPop();
    if (!ev.target.closest("#notes") && !ev.target.closest("#bell")) {
    }
  });

  function setComposerPlaceholder(text) {
    state.placeholder = text;
    if (!state.replyTo) input.dataset.placeholder = text;
  }

  function paintQuoteBar() {
    const bar = $("quote-bar");
    const on = !!(state.replyTo && state.sel && state.sel.type === "bot" && !state.peer);
    bar.hidden = !on;
    composer.classList.toggle("quoting", on);
    if (on) {
      $("quote-bar-text").textContent = quoteSnippet(state.replyTo.text);
      input.dataset.placeholder = "回复…";
    } else {
      input.dataset.placeholder = state.placeholder || "写一条消息";
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
    for (const id of ["devices-modal"]) {
      const layer = $(id);
      if (layer && layer.firstChild) return true;
    }
    return false;
  }

  document.addEventListener("keydown", (ev) => {
    if (ev.key !== "Escape") return;
    if (deviceLayerOpen()) return;
    if (!$("setpage").hidden && modal.hidden) return;
    hideTip();
    if (panelSub && !panelSub.hidden) { closePanelSub(); return; }
    if (panelMore) { closePanelMore(); return; }
    if (!lightbox.hidden) { closeShots(); return; }
    if (!modal.hidden) { closeModal(); return; }
    if (stpState.open) { closeStp(); return; }
    if (state.replyTo) { clearQuote(); return; }
    if (!$("account-menu").hidden) { closePops(); return; }
    if (panel.classList.contains("open")) {
      rememberPanel(false);
      closePops();
      panelOpen(false);
    }
    sidebar.classList.remove("open");
    syncBackdrop();
  });

  modal.addEventListener("keydown", (ev) => {
    if (ev.key !== "Tab") return;
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

  composer.addEventListener("submit", (ev) => {
    ev.preventDefault();
    if (recording) { finishRecording(true);return; }
    const text = composerText(input);
    const images = attachReady().map((row) => row.id);
    if (attachBusy()) { attachNote("图片还在上传，等一下再发。"); return; }
    if ((!text.trim() && !images.length) || !state.sel) return;
    composerSet("");
    composerRefreshHeight();
    clearAttach();
    syncComposer();
    sendText(text, null, images);
  });

  // A screenshot pasted from the clipboard arrives here, not as text. The
  // browser gives the picture in clipboardData; anything else falls through to
  // the plain-text handler below.
  input.addEventListener("paste", (ev) => {
    const files = imagesFrom(ev.clipboardData);
    if (!files.length) return;
    ev.preventDefault();
    uploadAttach(files);
  });

  // Rich pastes stay plain: the draft is text, and the cap still applies at
  // the edit entry (an over-cap paste is refused whole, like maxlength).
  // The insert that follows lands in the app's undo history like any edit.
  input.addEventListener("paste", (ev) => {
    const text = ev.clipboardData ? ev.clipboardData.getData("text/plain") : "";
    if (!text) return;
    ev.preventDefault();
    document.execCommand("insertText", false, text);
  });

  // A click on a highlighted URL opens the system browser through the same
  // off-origin path as the bubble links (target=_blank + noopener; the desktop
  // shell turns it into OpenExternal). The anchor sits inside the composer
  // form, so the click is consumed here and can never become a submit.
  // Links inside contenteditable never take focus, so the "caret in link" pad
  // is a class following the selection.
  document.addEventListener("selectionchange", () => {
    const sel = getSelection();
    const node = sel && sel.rangeCount ? sel.getRangeAt(0).startContainer : null;
    const el = node && (node.nodeType === 1 ? node : node.parentElement);
    const link = el && input.contains(el) ? el.closest("a") : null;
    for (const a of input.querySelectorAll("a.caret-in")) if (a !== link) a.classList.remove("caret-in");
    if (link) link.classList.add("caret-in");
  });
  input.addEventListener("click", (ev) => {
    const link = ev.target && ev.target.closest ? ev.target.closest("a") : null;
    if (!link || !input.contains(link)) return;
    ev.preventDefault();
    const href = link.getAttribute("href") || "";
    if (!composerSafeHref(href)) return;
    window.open(href, "_blank", "noopener");
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
    // Undo/redo is ours (see composerHistory): the browser's own history
    // would restore nodes that every re-render replaced.
    if ((ev.ctrlKey || ev.metaKey) && !ev.altKey && (ev.key === "z" || ev.key === "Z" || ev.key === "y" || ev.key === "Y")) {
      ev.preventDefault();
      if (!composerComposing) { // mid-composition the IME owns undo
        if (ev.shiftKey || ev.key === "y" || ev.key === "Y") composerRedo();
        else composerUndo();
      }
      return;
    }
    if (ev.key !== "Enter" || ev.shiftKey || ev.isComposing || ev.keyCode === 229) return;
    ev.preventDefault();
    composer.requestSubmit();
  });

  // Intercept the menu-driven undo/redo too, and capture the caret before the
  // browser mutates the draft: a refused over-cap edit restores it.
  input.addEventListener("beforeinput", (ev) => {
    if (ev.inputType === "historyUndo") { ev.preventDefault(); composerUndo(); return; }
    if (ev.inputType === "historyRedo") { ev.preventDefault(); composerRedo(); return; }
    composerEditCaret = composerCaret();
  });

  input.addEventListener("compositionstart", () => {
    composerComposing = true;
    composerEditCaret = composerCaret();
  });
  input.addEventListener("compositionend", () => {
    composerComposing = false;
    refreshComposerLinks();
  });

  input.addEventListener("input", (ev) => {
    // A native undo/redo command (execCommand, some menus) skips beforeinput
    // and arrives here already applied against nodes the re-renders retired:
    // its result is wrong, so drop it and replay our own history instead.
    if (!composerComposing && (ev.inputType === "historyUndo" || ev.inputType === "historyRedo")) {
      const h = composerHistory, at = h.index;
      if (ev.inputType === "historyUndo") composerUndo();
      else composerRedo();
      if (h.index === at) { // nothing to step to: just undo the native damage
        renderComposer(composerTextCache);
        composerRefreshHeight();
        composerCaretTo(at >= 0 ? h.stack[at].caret : Infinity);
        syncComposer();
      }
      return;
    }
    composerRefreshHeight();
    refreshComposerLinks();
    syncComposer();
  });

  window.addEventListener("resize", () => {
    if (window.innerWidth >= 768) sidebar.classList.remove("open");
    syncSidebar();
    syncPanelDefault();
    syncBackdrop();
  });

  async function boot() {
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

  if (!window.XBotPipe && "serviceWorker" in navigator) {
    navigator.serviceWorker.register("/sw.js").catch(() => {});
  }

  function startPoll() {
    if (startPoll.started) return;
    startPoll.started = true;
    setInterval(() => {
      if (shell.hidden) return;
      if (!document.hidden) refreshStatus().catch(() => {});
      if (state.streaming) return;
      if (!document.hidden) refreshUpdate().catch(() => {});
      refreshNotes().catch(() => {});
      refreshLists().catch(() => {});
      loadThread().catch(() => {});
    }, 3000);
  }

  // First run of the desktop app: ask for the model key, test it, save it.
  function showSetup(host) {
    login.hidden = true;
    closeSettings(false);
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

  // enterOwner opens the main interface for the owner: after an account sign-in,
  // a tap on a Bot in 我的设备 (name), or coming back from an account screen (null).
  let booted = false;
  async function enterOwner(name) {
    if (booted && authenticated) {
      login.hidden = true;
      shell.hidden = false;
      if (name) await selectBot(name);
      return;
    }
    if (name) { try { sessionStorage.setItem("xbot-selected-bot", name); } catch (e) {} }
    await ownerSession(await api("/api/session"));
  }
  api("/api/session").then(async data => {
    if (data.authed) await ownerSession(data);
    else if (legacyLogin) showLogin();
    else await window.XBotAccount.boot({owner: name => enterOwner(name).catch(() => showLogin()), signedOut: () => { forgetOwner(); authenticated = false; booted = false; shell.hidden = true; }});
  }).catch(() => showLogin());
  // 把 session 里的入口语境交给设备模块：官网入口（隧道/代理）底栏写账号设备数。
  function syncDevicesSession(data) { if (window.xbotDevices) window.xbotDevices.setSession({ remote: !!data.remote }); }
  // 本机还没绑到登录着的账号：开机后给一次「把这台电脑绑到账号」确认（shot 04b）。
  function offerClaimOnce() {
    if (legacyLogin) return;
    const acct = window.XBotAccount && window.XBotAccount.state ? window.XBotAccount.state() : null;
    if (!acct || !acct.unbound || !window.xbotDevices) return;
    let said = false;
    try { said = sessionStorage.getItem("xbot-claim-dismissed") === "1"; } catch (e) {}
    if (!said) window.xbotDevices.offerClaim();
  }
  async function ownerSession(data) {
    if (!data || !data.authed) { showLogin(); return; }
    booted = true;
    state.owner = (data && data.owner) || "";
    state.ownerName = (data && data.owner_name) || "";
    state.version = (data && data.version) || "";
    state.tip = (data && data.tip) || "";
    state.app = !!data.app;
    syncDevicesSession(data);
    if (data.needs_key) await showSetup(data.key_host || "");
    renderMe();
    showApp();
    await boot();
    startPoll();
    if (new URLSearchParams(location.search).get("s") === "account") openSettings("account");
  }
})();
