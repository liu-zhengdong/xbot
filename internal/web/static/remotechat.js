/* 远端 Bot 对话：点侧栏设备分组里的 Bot，以主人的身份和另一台电脑上的 Bot 聊天（v2 原型 remote-*、side-full）。
   面板绝对定位盖住 .main，app.js 不参与；请求走 devices.js 的 api，登录过期和别处一样。 */
(function () {
  "use strict";
  const D = window.xbotDevices;
  const $main = document.querySelector(".main");
  if (!D || !$main) return;
  const { S, api, esc, fmtSeen, sideFaces } = D;

  // ---- 图标（和 app.css .icon 同风格：24 网格、1.6 线宽） ----
  const I = {
    back: '<path d="m15 18-6-6 6-6"/>',
    panel: '<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M15 5v14"/>',
    send: '<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    cloudOff: '<path d="m2 2 20 20"/><path d="M5.78 5.78A7 7 0 0 0 9 19h8.5a4.5 4.5 0 0 0 1.31-.2"/><path d="M21.53 16.9A4.5 4.5 0 0 0 17.5 10h-1.79A7 7 0 0 0 9.69 5.8"/>',
  };
  const svg = (k, size) => `<svg class="icon" viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"${size ? ` style="width:${size}px;height:${size}px"` : ""}>${I[k]}</svg>`;

  // ---- 状态：一次只开一个对话 ----
  const V = {
    dev: "", bot: "",
    devInfo: null,          // 消息接口给的 device：{id,label,online,seen,unpaired}
    list: [],               // messages，按 at、id 排
    send: null,             // 正在发的那条（乐观追加的气泡）
    failed: [],             // 没发出去的原文：一直显示「重试」
    loading: false, error: "", info: false, atBottom: true,
  };
  const LIMIT = 200, POLL = 5000, MAX = 16000;

  // 面板：盖住 .main（它是 position: relative），绝对定位；关闭时整块隐藏，后面的本机对话不受影响。
  const root = document.createElement("section");
  root.id = "remote-chat"; root.className = "rc"; root.hidden = true; root.setAttribute("aria-label", "远端 Bot 对话");
  document.querySelector(".main").append(root);
  let el = {}, raf = 0, lastFocus = null;

  const isOpen = () => !root.hidden;
  const same = (d, b) => d === V.dev && b === V.bot;
  const key = () => (isOpen() ? V.dev + "\u0000" + V.bot : "");
  const path = (d, b) => "/api/remote/" + encodeURIComponent(d) + "/bots/" + encodeURIComponent(b);

  // ---- 数据 ----
  // 标签和在线状态以 /api/devices 为准（30 秒刷一次）；还没有时用消息接口给的 device。
  function device() {
    const d = ((S.data && S.data.devices) || []).find((x) => x.id === V.dev);
    return Object.assign({ id: V.dev, label: V.dev }, d || {}, V.devInfo || {});
  }
  const label = () => device().label || V.dev; // 拼进 HTML 时一律 esc
  const online = () => !!device().online && !((S.data || {}).gateway_ok === false);
  const seen = () => fmtSeen(device().seen);
  const lastSeen = () => seen() || "很久以前";
  function merge(m) {
    if (!m) return;
    const byID = m.id != null ? V.list.findIndex((x) => String(x.id) === String(m.id)) : -1;
    if (byID >= 0) V.list[byID] = m;
    else V.list.push(m); // 服务器给的顺序就是记录的顺序，不按时间重排
  }
  // 重新拿到整份记录后，本机记着的「没发出去」只要服务器那条回来了、或者已经显示成失败，就交还给服务器。
  const settle = () => { V.failed = V.failed.filter((t) => !V.list.some((m) => m.role === "me" && m.text === t && m.state !== "sending")); };

  async function load(initial) {
    const d = V.dev, b = V.bot;
    if (initial) { V.loading = true; V.error = ""; draw(); }
    try {
      const r = await api("GET", path(d, b) + "/messages?limit=" + LIMIT);
      if (!same(d, b)) return;
      const lastBefore = V.list.length ? V.list[V.list.length - 1].id : "";
      V.list = (r.messages || []).filter((m) => m && m.text != null);
      V.devInfo = r.device || null;
      settle();
      if (initial) markRead();
      else if (V.list.length && V.list[V.list.length - 1].id !== lastBefore && D.refresh) D.refresh(); // 新回复：侧栏的最新一句跟上
      draw();
      scrollDown(initial);
    } catch (e) {
      if (!same(d, b) || e.status === 401) return; // 401 已经去重新登录
      V.error = "连不上 XBot：" + e.message;
      draw();
    } finally {
      if (same(d, b)) { V.loading = false; draw(); }
    }
  }
  // 打开时标已读（接口顺手标），侧栏的未读点跟着消掉。
  async function markRead() {
    try { await api("POST", path(V.dev, V.bot) + "/read"); } catch (e) { /* 连不上网关就下次再说 */ }
    if (D.refresh) D.refresh();
  }
  // 主人打的字：先显示气泡，拿到服务器那条后替换；没发出去就留原文和「重试」。
  async function sendText(text) {
    const t = String(text || "").trim().slice(0, MAX);
    if (!t) return;
    const d = V.dev, b = V.bot;
    V.failed = V.failed.filter((x) => x !== t);
    showError("");
    V.send = { text: t, role: "me", state: "sending" };
    if (el.input) { el.input.value = ""; grow(); }
    draw();
    scrollDown(true);
    try {
      const r = await api("POST", path(d, b) + "/messages", { text: t });
      if (!same(d, b)) return;
      merge(r && r.message);
    } catch (e) {
      if (!same(d, b)) return;
      // 502 {error, message}：网关没送到，服务端已经把这行存成 failed；其他错误留在本机，给「重试」。
      const msg = e.data && e.data.error;
      if (e.status === 502 && e.data && e.data.message) merge(e.data.message);
      else if (e.status >= 400 && e.status < 500 && msg) {
        // 4xx 带 error（如 409 已解除配对）：没发出去，不进 V.failed；原文放回空着的输入框，上面写一行报错。
        if (el.input && !el.input.value) { el.input.value = t; grow(); el.input.focus(); }
        showError("没能完成：" + msg);
      } else if (!V.failed.includes(t)) V.failed.push(t);
    }
    if (!same(d, b)) return;
    V.send = null;
    draw();
    scrollDown();
    if (D.refresh) D.refresh(); // 侧栏的最新一句和「排队中」
  }

  // 输入框上方的报错：纯文本写入（textContent），空串就收起。
  function showError(text) {
    if (!el.error) return;
    el.error.textContent = text;
    el.error.hidden = !text;
  }

  // ---- 打开、关闭 ----
  function open(devId, bot) {
    if (isOpen() && same(devId, bot)) { const t = el.input; if (t && !t.value) t.focus(); return; }
    const was = isOpen();
    if (!was) lastFocus = document.activeElement;
    V.dev = devId; V.bot = bot; V.devInfo = null; V.list = []; V.send = null; V.failed = [];
    V.error = ""; V.info = false; V.loading = false; V.atBottom = true;
    root.hidden = false;
    document.documentElement.classList.add("rc-open"); // 手机上输入框 16px，聚焦时不自动缩放
    // 本机 Bot 的详情抽屉是盖在卡片上的一层（z-index 30），远端对话只有 12，
    // 抽屉开着会挡住远端对话的标题和发送区。远端对话整块打开时把它收掉。
    window.dispatchEvent(new Event("xbot:remote-open"));
    titleSig = "";
    render(); // 上一个对话的报错随整块重画一起清掉
    draw(true);
    load(true);
    if (was && D.repaint) D.repaint();
    if (el.input && lastFocus && lastFocus.tagName === "TEXTAREA") el.input.focus();
  }
  function close() {
    if (!isOpen()) return;
    root.hidden = true;
    root.innerHTML = "";
    el = {};
    document.documentElement.classList.remove("rc-open");
    V.dev = V.bot = ""; V.list = []; V.send = null; V.failed = [];
    if (D.repaint) D.repaint(); // 侧栏去掉选中行
    if (lastFocus && document.contains(lastFocus) && lastFocus.focus) lastFocus.focus();
    lastFocus = null;
  }

  // ---- 画 ----
  function render() {
    root.innerHTML = `<header class="rc-head"><div class="rc-head-in" id="rc-title"></div></header>
      <div class="rc-body"><div class="rc-thread" id="rc-thread" role="log" aria-label="对话"></div><aside class="rc-info" id="rc-info" aria-label="详情" hidden></aside></div>
      <div class="rc-dock"><div class="rc-note" id="rc-note" role="status"></div>
      <div class="rc-error" id="rc-error" role="alert" hidden></div>
      <form class="rc-composer" id="rc-form" novalidate><textarea id="rc-input" rows="1" maxlength="${MAX}" placeholder="${esc("写给 " + V.bot)}" aria-label="写给 ${esc(V.bot)}"></textarea>
      <button class="rc-send" type="submit" aria-label="发送">${svg("send")}</button></form></div>`;
    el = {
      title: root.querySelector("#rc-title"), thread: root.querySelector("#rc-thread"), info: root.querySelector("#rc-info"),
      note: root.querySelector("#rc-note"), error: root.querySelector("#rc-error"), form: root.querySelector("#rc-form"), input: root.querySelector("#rc-input"),
    };
    // Enter 发送、Shift+Enter 换行；输入法选词时的回车（isComposing 或 229）不算发送。
    el.input.addEventListener("keydown", (ev) => {
      if (ev.key !== "Enter" || ev.shiftKey || ev.isComposing || ev.keyCode === 229) return;
      ev.preventDefault();
      sendText(el.input.value);
    });
    el.input.addEventListener("input", () => { grow(); showError(""); }); // 主人一改就收起上次的报错
    el.form.addEventListener("submit", (ev) => { ev.preventDefault(); sendText(el.input.value); });
    el.thread.addEventListener("scroll", () => {
      const t = el.thread;
      V.atBottom = t.scrollHeight - t.scrollTop - t.clientHeight < 48;
    });
  }
  // 轮询只重画变化的部分：输入框里正在打的字和焦点都不动。
  function draw(force) {
    if (!isOpen() || !el.title) return;
    drawTitle(force);
    drawThread();
    drawNote();
    drawInfo();
  }
  let titleSig = "";
  function drawTitle(force) {
    const on = online();
    root.classList.toggle("is-off", !on); // 离线：副标题前的点是空心圈
    const sig = [V.bot, label(), on, seen(), V.info].join("\u0000"); // 时间按分钟变；没拦住的事由才重画按钮
    if (force || sig !== titleSig) {
      titleSig = sig;
      const status = on ? "在线" : "离线 · 最后联系 " + (seen() || "很久以前");
      el.title.innerHTML = `<button class="icon-btn rc-back" type="button" data-rc="back" aria-label="返回">${svg("back")}</button>
        ${sideFaces(V.bot, V.dev)}<div class="rc-titles"><div class="rc-name">${esc(V.bot)}</div><div class="rc-sub">${esc("在 " + label() + " 上")}<span class="rc-sep" aria-hidden="true">·</span><span class="rc-st">${esc(status)}</span></div></div>
        <button class="icon-btn rc-info-btn${V.info ? " on" : ""}" type="button" data-rc="info" aria-label="详情" aria-expanded="${V.info}" data-tip="详情">${svg("panel")}</button>`;
      if (D.tip) D.tip(); // 重画换掉了带提示的按钮
    }
  }
  // 正文一律当纯文本：先转义，再把换行变成 <br>。
  const lines = (t) => esc(String(t).replace(/\r\n?/g, "\n").replace(/\n+$/, "")).replace(/\n/g, "<br>");
  const clock = (at) => { const t = new Date(at); return isNaN(t.getTime()) ? "" : String(t.getHours()).padStart(2, "0") + ":" + String(t.getMinutes()).padStart(2, "0"); };
  // 主人这条下面的状态：本机还没发出、网关里等离线设备上线、设备在线但没回执、没发出去；其他不写。
  function stateOf(m) {
    if (m.role === "me" && (m.state === "failed" || V.failed.includes(m.text))) return "failed"; // 服务端存成 failed 的（502）重新打开也给「重试」
    if (m.state === "queued_local") return "local";
    if (m.role === "me" && m.no_ack && online()) return "noack"; // 离线仍按「已排队」写
    if (m.state === "sent" && !online()) return "waiting";
    return "";
  }
  const stateHTML = (kind) => kind === "failed" ? `<div class="rc-state"><span>没发出去</span><button class="text-btn" type="button" data-rc="retry">重试</button></div>`
    : kind === "local" ? `<div class="rc-state">${svg("clock", 12)}<span>已排队 · 还在这台电脑上</span></div>`
    : kind === "waiting" ? `<div class="rc-state">${svg("clock", 12)}<span>已排队 · 等 ${esc(label())} 上线</span></div>`
    : kind === "noack" ? `<div class="rc-state">${svg("clock", 12)}<span>没收到 ${esc(label())} 的回执</span><span class="rc-hint">可以在「设备」里看看它的状态</span></div>` : "";
  const owner = (text, kind) => `<div class="rc-u"><div class="bubble">${lines(text)}</div>${stateHTML(kind)}</div>`;
  function drawThread() {
    const items = [];
    if (V.loading && !V.list.length) items.push(`<div class="rc-empty">正在加载</div>`);
    else if (V.error && !V.list.length) items.push(`<div class="rc-empty"><p>${esc(V.error)}</p><button class="secondary" type="button" data-rc="reload">重试</button></div>`);
    else if (!V.list.length && !V.send) items.push(`<div class="rc-empty">还没有消息。写给 ${esc(V.bot)}，${online() ? "它会马上收到" : esc(label() + " 上线后会收到")}。</div>`);
    let prevBot = false;
    for (const m of V.list) {
      const kind = stateOf(m);
      if (m.role === "me") { items.push(owner(m.text, kind)); prevBot = false; continue; }
      // Bot 连着回的几条：第一条有头像、名字和时间，后面的紧跟着写（原型 66/67）。
      if (prevBot) items.push(`<div class="rc-b cont"><div class="rc-bbody"><div class="rc-text">${lines(m.text)}</div></div></div>`);
      else items.push(`<div class="rc-b">${sideFaces(V.bot, V.dev)}<div class="rc-bbody"><div class="who">${esc(V.bot)}<time>${clock(m.at)}</time></div><div class="rc-text">${lines(m.text)}</div></div></div>`);
      prevBot = true;
    }
    // 连不上 XBot 时没进记录的那句：留在原处给「重试」。
    for (const t of V.failed) if (!V.list.some((m) => m.role === "me" && m.text === t)) items.push(owner(t, "failed"));
    if (V.send) items.push(owner(V.send.text, ""));
    el.thread.innerHTML = `<div class="thread-inner">${items.join("")}</div>`;
  }
  function drawNote() {
    el.note.innerHTML = online() ? "" : `${svg("cloudOff", 14)}<span>${esc(label())} 离线，${esc(seen() || "很久以前")}联系过。消息已经交给网关，${esc(label())} 打开 XBot 后 ${esc(V.bot)} 才会收到并回复；最多等 7 天。</span>`;
  }
  function drawInfo() {
    el.info.hidden = !V.info;
    if (!V.info) return;
    const d = device(), where = label();
    el.info.innerHTML = `<div class="rc-iface">${sideFaces(V.bot, V.dev)}<b>${esc(V.bot)}</b></div>
      <dl class="rc-facts">
        <div><dt>设备</dt><dd>${esc(where)}</dd></div>
        <div><dt>地址</dt><dd><code>${esc(V.bot + "@" + (d.name || d.id || V.dev))}</code></dd></div>
        <div><dt>聊天记录</dt><dd>这台电脑和 ${esc(where)} 各存一份。</dd></div>
        <div><dt>记忆</dt><dd>只在 ${esc(where)} 上。</dd></div>
      </dl>
      <a class="rc-link" href="#devices" data-rc="devices">在「设备」里查看</a>`;
  }
  function grow() {
    const t = el.input;
    if (!t) return;
    t.style.height = "auto";
    t.style.height = Math.min(t.scrollHeight, 140) + "px";
  }
  function scrollDown(force) {
    if (!force && !V.atBottom) return;
    cancelAnimationFrame(raf);
    raf = requestAnimationFrame(() => { if (el.thread) el.thread.scrollTop = el.thread.scrollHeight; });
  }

  // ---- 关闭：点本机的 Bot、置顶、新建、收起列表，或手机上的返回 ----
  document.addEventListener("click", (ev) => {
    if (!isOpen()) return;
    if (root.contains(ev.target)) {
      const t = ev.target.closest("[data-rc]");
      if (!t) return;
      const a = t.dataset.rc;
      if (a === "info") { V.info = !V.info; draw(); }
      else if (a === "retry") sendText(t.closest(".rc-u").querySelector(".bubble").textContent);
      else if (a === "reload") load(true);
      else if (a === "devices") { ev.preventDefault(); D.open(); }
      else if (a === "back") close();
      return;
    }
    // 只在 #roster / #pin-row 里点（本机 Bot、群组、新建）才关；账号菜单和设备抽屉照常盖在上面。
    if (ev.target.closest("#roster .item, #roster .folder-toggle, #pin-row button, #back")) close();
  }, true);
  window.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && isOpen() && !S.drawer && !S.view) close(); });

  // ---- 每 5 秒拉一次记录（打开且页面可见时）；切回页面立刻拉 ----
  async function poll() {
    if (!isOpen() || document.hidden || V.loading) return;
    await load(false);
  }
  setInterval(poll, POLL);
  document.addEventListener("visibilitychange", () => { if (!document.hidden) poll(); });
  // devices.js 刷到新的设备数据后重画标题和离线说明。
  D.onPaint = () => { if (isOpen()) draw(); };

  // D.chat is a getter over window.xbotRemoteChat (devices.js), so set the window one.
  window.xbotRemoteChat = { open: open, close: close, isOpen: isOpen, key: key, same: same };
})();
