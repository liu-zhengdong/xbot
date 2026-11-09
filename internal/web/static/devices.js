/* Account devices own shared execution and next-message targets.
   Legacy pairing owns Bot messaging. Both use the existing settings shell. */
(function () {
  "use strict";
  // Platform marks: Simple Icons 11.15.0; sources and terms in vendor/platform-icons.SOURCE.md.
  const I = {
    macos: '<path class="solid" d="M12.152 6.896c-.948 0-2.415-1.078-3.96-1.04-2.04.027-3.91 1.183-4.961 3.014-2.117 3.675-.546 9.103 1.519 12.09 1.013 1.454 2.208 3.09 3.792 3.039 1.52-.065 2.09-.987 3.935-.987 1.831 0 2.35.987 3.96.948 1.637-.026 2.676-1.48 3.676-2.948 1.156-1.688 1.636-3.325 1.662-3.415-.039-.013-3.182-1.221-3.22-4.857-.026-3.04 2.48-4.494 2.597-4.559-1.429-2.09-3.623-2.324-4.39-2.376-2-.156-3.675 1.09-4.61 1.09zM15.53 3.83c.843-1.012 1.4-2.427 1.245-3.83-1.207.052-2.662.805-3.532 1.818-.78.896-1.454 2.338-1.273 3.714 1.338.104 2.715-.688 3.559-1.701"/>',
    windows: '<path class="solid" d="M0,0H11.377V11.372H0ZM12.623,0H24V11.372H12.623ZM0,12.623H11.377V24H0Zm12.623,0H24V24H12.623"/>',
    linux: '<path class="solid" d="M12.504 0c-.155 0-.315.008-.48.021-4.226.333-3.105 4.807-3.17 6.298-.076 1.092-.3 1.953-1.05 3.02-.885 1.051-2.127 2.75-2.716 4.521-.278.832-.41 1.684-.287 2.489a.424.424 0 00-.11.135c-.26.268-.45.6-.663.839-.199.199-.485.267-.797.4-.313.136-.658.269-.864.68-.09.189-.136.394-.132.602 0 .199.027.4.055.536.058.399.116.728.04.97-.249.68-.28 1.145-.106 1.484.174.334.535.47.94.601.81.2 1.91.135 2.774.6.926.466 1.866.67 2.616.47.526-.116.97-.464 1.208-.946.587-.003 1.23-.269 2.26-.334.699-.058 1.574.267 2.577.2.025.134.063.198.114.333l.003.003c.391.778 1.113 1.132 1.884 1.071.771-.06 1.592-.536 2.257-1.306.631-.765 1.683-1.084 2.378-1.503.348-.199.629-.469.649-.853.023-.4-.2-.811-.714-1.376v-.097l-.003-.003c-.17-.2-.25-.535-.338-.926-.085-.401-.182-.786-.492-1.046h-.003c-.059-.054-.123-.067-.188-.135a.357.357 0 00-.19-.064c.431-1.278.264-2.55-.173-3.694-.533-1.41-1.465-2.638-2.175-3.483-.796-1.005-1.576-1.957-1.56-3.368.026-2.152.236-6.133-3.544-6.139zm.529 3.405h.013c.213 0 .396.062.584.198.19.135.33.332.438.533.105.259.158.459.166.724 0-.02.006-.04.006-.06v.105a.086.086 0 01-.004-.021l-.004-.024a1.807 1.807 0 01-.15.706.953.953 0 01-.213.335.71.71 0 00-.088-.042c-.104-.045-.198-.064-.284-.133a1.312 1.312 0 00-.22-.066c.05-.06.146-.133.183-.198.053-.128.082-.264.088-.402v-.02a1.21 1.21 0 00-.061-.4c-.045-.134-.101-.2-.183-.333-.084-.066-.167-.132-.267-.132h-.016c-.093 0-.176.03-.262.132a.8.8 0 00-.205.334 1.18 1.18 0 00-.09.4v.019c.002.089.008.179.02.267-.193-.067-.438-.135-.607-.202a1.635 1.635 0 01-.018-.2v-.02a1.772 1.772 0 01.15-.768c.082-.22.232-.406.43-.533a.985.985 0 01.594-.2zm-2.962.059h.036c.142 0 .27.048.399.135.146.129.264.288.344.465.09.199.14.4.153.667v.004c.007.134.006.2-.002.266v.08c-.03.007-.056.018-.083.024-.152.055-.274.135-.393.2.012-.09.013-.18.003-.267v-.015c-.012-.133-.04-.2-.082-.333a.613.613 0 00-.166-.267.248.248 0 00-.183-.064h-.021c-.071.006-.13.04-.186.132a.552.552 0 00-.12.27.944.944 0 00-.023.33v.015c.012.135.037.2.08.334.046.134.098.2.166.268.01.009.02.018.034.024-.07.057-.117.07-.176.136a.304.304 0 01-.131.068 2.62 2.62 0 01-.275-.402 1.772 1.772 0 01-.155-.667 1.759 1.759 0 01.08-.668 1.43 1.43 0 01.283-.535c.128-.133.26-.2.418-.2zm1.37 1.706c.332 0 .733.065 1.216.399.293.2.523.269 1.052.468h.003c.255.136.405.266.478.399v-.131a.571.571 0 01.016.47c-.123.31-.516.643-1.063.842v.002c-.268.135-.501.333-.775.465-.276.135-.588.292-1.012.267a1.139 1.139 0 01-.448-.067 3.566 3.566 0 01-.322-.198c-.195-.135-.363-.332-.612-.465v-.005h-.005c-.4-.246-.616-.512-.686-.71-.07-.268-.005-.47.193-.6.224-.135.38-.271.483-.336.104-.074.143-.102.176-.131h.002v-.003c.169-.202.436-.47.839-.601.139-.036.294-.065.466-.065zm2.8 2.142c.358 1.417 1.196 3.475 1.735 4.473.286.534.855 1.659 1.102 3.024.156-.005.33.018.513.064.646-1.671-.546-3.467-1.089-3.966-.22-.2-.232-.335-.123-.335.59.534 1.365 1.572 1.646 2.757.13.535.16 1.104.021 1.67.067.028.135.06.205.067 1.032.534 1.413.938 1.23 1.537v-.043c-.06-.003-.12 0-.18 0h-.016c.151-.467-.182-.825-1.065-1.224-.915-.4-1.646-.336-1.77.465-.008.043-.013.066-.018.135-.068.023-.139.053-.209.064-.43.268-.662.669-.793 1.187-.13.533-.17 1.156-.205 1.869v.003c-.02.334-.17.838-.319 1.35-1.5 1.072-3.58 1.538-5.348.334a2.645 2.645 0 00-.402-.533 1.45 1.45 0 00-.275-.333c.182 0 .338-.03.465-.067a.615.615 0 00.314-.334c.108-.267 0-.697-.345-1.163-.345-.467-.931-.995-1.788-1.521-.63-.4-.986-.87-1.15-1.396-.165-.534-.143-1.085-.015-1.645.245-1.07.873-2.11 1.274-2.763.107-.065.037.135-.408.974-.396.751-1.14 2.497-.122 3.854a8.123 8.123 0 01.647-2.876c.564-1.278 1.743-3.504 1.836-5.268.048.036.217.135.289.202.218.133.38.333.59.465.21.201.477.335.876.335.039.003.075.006.11.006.412 0 .73-.134.997-.268.29-.134.52-.334.74-.4h.005c.467-.135.835-.402 1.044-.7zm2.185 8.958c.037.6.343 1.245.882 1.377.588.134 1.434-.333 1.791-.765l.211-.01c.315-.007.577.01.847.268l.003.003c.208.199.305.53.391.876.085.4.154.78.409 1.066.486.527.645.906.636 1.14l.003-.007v.018l-.003-.012c-.015.262-.185.396-.498.595-.63.401-1.746.712-2.457 1.57-.618.737-1.37 1.14-2.036 1.191-.664.053-1.237-.2-1.574-.898l-.005-.003c-.21-.4-.12-1.025.056-1.69.176-.668.428-1.344.463-1.897.037-.714.076-1.335.195-1.814.12-.465.308-.797.641-.984l.045-.022zm-10.814.049h.01c.053 0 .105.005.157.014.376.055.706.333 1.023.752l.91 1.664.003.003c.243.533.754 1.064 1.189 1.637.434.598.77 1.131.729 1.57v.006c-.057.744-.48 1.148-1.125 1.294-.645.135-1.52.002-2.395-.464-.968-.536-2.118-.469-2.857-.602-.369-.066-.61-.2-.723-.4-.11-.2-.113-.602.123-1.23v-.004l.002-.003c.117-.334.03-.752-.027-1.118-.055-.401-.083-.71.043-.94.16-.334.396-.4.69-.533.294-.135.64-.202.915-.47h.002v-.002c.256-.268.445-.601.668-.838.19-.201.38-.336.663-.336zm7.159-9.074c-.435.201-.945.535-1.488.535-.542 0-.97-.267-1.28-.466-.154-.134-.28-.268-.373-.335-.164-.134-.144-.333-.074-.333.109.016.129.134.199.2.096.066.215.2.36.333.292.2.68.467 1.167.467.485 0 1.053-.267 1.398-.466.195-.135.445-.334.648-.467.156-.136.149-.267.279-.267.128.016.034.134-.147.332a8.097 8.097 0 01-.69.468zm-1.082-1.583V5.64c-.006-.02.013-.042.029-.05.074-.043.18-.027.26.004.063 0 .16.067.15.135-.006.049-.085.066-.135.066-.055 0-.092-.043-.141-.068-.052-.018-.146-.008-.163-.065zm-.551 0c-.02.058-.113.049-.166.066-.047.025-.086.068-.14.068-.05 0-.13-.02-.136-.068-.01-.066.088-.133.15-.133.08-.031.184-.047.259-.005.019.009.036.03.03.05v.02h.003z"/>',
    laptop: '<path d="M18 5a2 2 0 0 1 2 2v8.5a2 2 0 0 0 .2.9l1.1 2.1a1 1 0 0 1-.9 1.5H3.6a1 1 0 0 1-.9-1.5l1.1-2.1a2 2 0 0 0 .2-.9V7a2 2 0 0 1 2-2z"/><path d="M20 16H4"/>',
    devices: '<path d="M18 8V6a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h8"/><path d="M10 19v-4"/><path d="M7 19h5"/><rect width="6" height="10" x="16" y="11" rx="2"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    check: '<path d="M5 12.5l4.5 4.5L19 7.5"/>',
    refresh: '<path d="M21 12a9 9 0 0 1-15.5 6.2L3 16"/><path d="M3 21v-5h5"/><path d="M3 12a9 9 0 0 1 15.5-6.2L21 8"/><path d="M21 3v5h-5"/>',
    clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    ellipsis: '<circle cx="5" cy="12" r="1.5" fill="currentColor"/><circle cx="12" cy="12" r="1.5" fill="currentColor"/><circle cx="19" cy="12" r="1.5" fill="currentColor"/>',
    pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/>',
    unlink: '<path d="m18.84 12.25 1.72-1.71a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="m5.17 11.75-1.71 1.71a5 5 0 0 0 7.07 7.07l1.71-1.71"/><path d="M8 2v3"/><path d="M2 8h3"/><path d="M16 22v-3"/><path d="M22 16h-3"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    keyboard: '<rect width="20" height="14" x="2" y="5" rx="2"/><path d="M6 9h.01M10 9h.01M14 9h.01M18 9h.01M6 13h.01M18 13h.01M9 15.5h6"/>',
    square: '<rect x="3" y="3" width="18" height="18" rx="2"/>',
    spin: '<circle cx="12" cy="12" r="8.5" opacity=".22"/><path d="M12 3.5a8.5 8.5 0 0 1 8.5 8.5"/>',
    bot: '<rect x="4" y="8" width="16" height="12" rx="3"/><path d="M12 4v4"/><path d="M9 13h.01M15 13h.01"/>',
    swap: '<path d="M16 3l4 4-4 4"/><path d="M20 7H4"/><path d="M8 21l-4-4 4-4"/><path d="M4 17h16"/>',
    shield: '<path d="M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z"/>',
    cloudOff: '<path d="m2 2 20 20"/><path d="M5.78 5.78A7 7 0 0 0 9 19h8.5a4.5 4.5 0 0 0 1.31-.2"/><path d="M21.53 16.9A4.5 4.5 0 0 0 17.5 10h-1.79A7 7 0 0 0 9.69 5.8"/>',
  };
  const svg = (k, size) => `<svg viewBox="0 0 24 24" aria-hidden="true" class="icon"${size ? ` style="width:${size}px;height:${size}px"` : ""}>${I[k]}</svg>`;
  const esc = (s) => String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
  const host = (u) => { try { return new URL(u).host || u; } catch (e) { return u || ""; } };

  async function api(method, path, body) {
    const res = await fetch(path, {
      method, credentials: "same-origin",
      headers: body === undefined ? {} : { "Content-Type": "application/json" },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (res.status === 401 && !path.startsWith("/api/account/")) { sessionExpired(); }
    authed = true;
    if (res.status === 204) return null;
    let data = null;
    try { data = await res.json(); } catch (e) { data = null; }
    if (!res.ok) { const err = new Error((data && data.error) || "请求失败"); err.status = res.status; err.data = data; throw err; } // 远端消息的 502 还带 {message}
    return data;
  }
  // 登录过期：收起设备抽屉和弹窗、清掉状态和挂到 body 上的层，重新加载让 app.js 走登录流程
  let authed = false; // Before login (the login page) a 401 is normal: never reload then.
  function sessionExpired() {
    if (!authed) { const err = new Error("未登录"); err.status = 401; throw err; }
    S.embedded = false; S.view = null; S.menuFor = ""; S.target = null; S.fieldErr = ""; S.busy = false;
    if (devicesEl) devicesEl.replaceChildren();
    if (modalEl) { try { modalEl.remove(); } catch (e) { modalEl.innerHTML = ""; } }
    devicesEl = modalEl = null;
    location.reload();
    const err = new Error("登录已过期");
    err.status = 401;
    throw err;
  }

  function left(expires, minus) {
    const t = Math.max(0, Math.round((new Date(expires).getTime() - (minus || 0) - Date.now()) / 1000));
    return Math.floor(t / 60) + ":" + String(t % 60).padStart(2, "0");
  }
  function fmtSeen(iso) {
    if (!iso) return "";
    const d = new Date(iso), now = new Date();
    if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
    const hm = String(d.getHours()).padStart(2, "0") + ":" + String(d.getMinutes()).padStart(2, "0");
    const day = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
    const diff = Math.round((day(now) - day(d)) / 86400000);
    if (diff === 0) return "今天 " + hm;
    if (diff === 1) return "昨天 " + hm;
    return (d.getMonth() + 1) + "月" + d.getDate() + "日";
  }

  // ---- 状态 ----
  const S = {
    desktop: null,
    data: null,            // GET /api/devices
    pairing: { a: {}, b: {} },
    view: null,            // drawer 之上的弹窗
    embedded: false,
    code: "",              // A 最近一次显示的码，用于确认框副行
    peer: null,            // 对方（请求方）{name, os, bots}
    remote: false,         // 官网入口（session.remote）：底栏写账号设备数，不写本机名
    acct: null,            // GET /api/account/me 的 account，用来出「把这台电脑绑到账号」
    acctDevs: null,        // 官网入口底栏「已绑定 N 台设备」的 N
    roster: null,          // GET /api/account/devices 的设备行：同一账号里的其他电脑，并进侧栏
    claimBusy: false, claimErr: "",
    joinOpen: false, joinVal: "", joinErr: "", // 添加设备下半：输入对方的短码（次要）
    fieldErr: "",
    menuFor: "", target: null, busy: false,
  };
  let devicesEl = null, modalEl = null, lastView = "";
  let desktopGeneration = 0;
  async function checkDesktop() {
    const generation = ++desktopGeneration;
    const button = devicesEl.querySelector('[data-act="desktop-check"]');
    if (button) button.disabled = true;
    let result;
    try { result = await api("GET", "/api/devices/desktop"); }
    catch (e) { result = { error: "连接失败" }; }
    if (generation !== desktopGeneration || !S.embedded) return;
    const data = result.status && result.status.data || {};
    const platform = result.platform || data.platform;
    const state = data.permissions && data.permissions.accessibility && data.permissions.accessibility.state;
    let label = "暂时查不到状态", guide = "请确认电脑操作组件已安装，再重新检查。";
    if (result.error) {
      guide = String(result.error).includes("not installed") ? "先在这台电脑的终端运行 npm install -g agent-desktop，再重新检查。" : "检查失败，请稍后重试。";
    } else if (platform === "darwin" || platform === "macos") {
      label = state === "granted" ? "无障碍已授权" : state === "denied" ? "无障碍未授权" : "无障碍状态未知";
      guide = state === "granted" ? "Bot 可以读取和操作 App；截图另需屏幕录制授权。" : "在这台 Mac 的「系统设置 → 隐私与安全性 → 辅助功能」中，允许启动 XBot 的应用；若列表另有 agent-desktop，也请打开。然后重新检查。";
    } else if (platform === "windows") {
      label = "使用 Windows 系统授权";
      guide = "请在已登录、未锁屏的桌面运行 XBot。不能操作权限比 XBot 更高的应用或系统安全提示；需要时让两者以相同权限运行。";
    } else if (result.legacy) {
      label = "使用屏幕操作";
      guide = "这台电脑保留原有的截图、点击和输入方式。";
    }
    S.desktop = { label, guide };
    const status = devicesEl.querySelector('#g7-desktop-status'), help = devicesEl.querySelector('#g7-desktop-help');
    if (status) status.textContent = label;
    if (help) help.textContent = guide;
    if (button) button.disabled = false;
  }
  // 挂给 pairing.js 的句柄（最后填上 views/acts 等）。
  const D = window.xbotDevices = {};

  function ensureHosts() {
    devicesEl = document.getElementById("settings-devices");
    if (modalEl) return;
    modalEl = document.createElement("div"); modalEl.className = "g7-layer"; modalEl.id = "devices-modal";
    document.body.append(modalEl);
  }

  async function refresh() {
    try { S.data = await api("GET", "/api/devices"); S.pairing = S.data.pairing; window.dispatchEvent(new Event("xbot:devices")); } catch (e) { /* 离线时保留旧数据 */ }
    if (S.remote) {
      // 官网入口：底栏写账号名下有几台设备（真实账号列表，不是本机配对表）。
      try { S.acctDevs = ((await api("GET", "/api/account/devices")) || { devices: [] }).devices.length; } catch (e) { /* 下次再试 */ }
    }
    paintFoot();
    readAcctRoster();
    paintSidebar();
  }
  // app.js 在拿到 /api/session 后调一次：这条入口是隧道/代理那头的官网。
  function setSession(s) {
    const was = S.remote;
    S.remote = !!(s && s.remote);
    if (S.remote && !was) refresh(); // 底栏要账号设备数，立即拉一次
    else paintFoot();
    reloadSharedAccount();
  }
  function paintFoot() {
    // 底栏「我」下面那行：本机写这台电脑的名字；官网入口写「已绑定 N 台设备」。
    const me = document.getElementById("me-device");
    if (!me) return;
    if (S.remote) {
      const n = S.acctDevs;
      me.textContent = n == null ? "" : n ? "已绑定 " + n + " 台设备" : "还没有设备";
    } else {
      const self = (S.data && S.data.self) || {};
      me.textContent = self.name || self.label || "";
    }
    me.hidden = !me.textContent;
  }
  // 每次打开设备页刷新账号状态。
  async function loadAcct() {
    try { const out = await api("GET", "/api/account/me"); S.acct = (out && out.account) || null; } catch (e) { S.acct = null; }
    return S.acct;
  }
  // 同一账号里的其他电脑（GET /api/account/devices）：登录后才有；本机那台不算，它就在左边的本机列表里。
  // 侧栏要用它的设备名、在线和 Bot 名单，所以每次刷新顺手读一次，最多一分钟一次（账号那边的心跳也是一分钟一报）。
  let rosterAt = 0, acctAt = 0;
  async function readAcctRoster(force) {
    if (!S.acct) {
      // 开机时没人替这里读过账号状态（那一步只在打开设备页时做），全靠这句补上：
      // 否则刷新过的页面里 S.acct 一直是 null，侧栏那组永远空着。
      if (force || Date.now() - acctAt > 55000) { acctAt = Date.now(); await loadAcct(); }
      if (!S.acct) { if (S.roster) { S.roster = null; paintSidebar(); } return; }
    }
    if (!force && Date.now() - rosterAt < 55000) return;
    rosterAt = Date.now();
    try {
      const out = await api("GET", "/api/account/devices");
      S.roster = Array.isArray(out && out.devices) ? out.devices : [];
    } catch (e) { if (!S.roster) S.roster = []; } // 读不到就留着上一次的，下一轮再试
    paintSidebar();
  }

  // ---- 打开、关闭 ----
  function openDevices() { return window.XBotSettings.open("devices"); }
  async function mount() {
    ensureHosts();
    S.embedded = true; S.menuFor = "";
    const generation = ++shared.mountGeneration;
    shared.legacy = false; shared.detailID = "";
    await Promise.all([refresh(), loadAcct()]);
    if (!S.embedded || generation !== shared.mountGeneration) return;
    if (S.acct) await readSharedDevices();
    if (!S.embedded || generation !== shared.mountGeneration) return;
    render();
    if (!S.acct) checkDesktop();
  }
  // 「把这台电脑绑到账号？」（04b）：本机还没绑到登录着的账号时，app.js 开机后请一次。
  async function offerClaim() {
    const acct = await loadAcct();
    if (!acct || !acct.unbound) return;
    let said = false;
    try { said = sessionStorage.getItem("xbot-claim-dismissed") === "1"; } catch (e) {}
    if (said) return;
    if (!S.data) await refresh();
    S.claimErr = ""; S.view = "claim"; render();
  }
  function unmount() {
    ++desktopGeneration; ++shared.mountGeneration; ++shared.detailGeneration;
    closeSharedModal(false);
    S.embedded = false;
    if (["add", "join", "confirm", "waiting"].includes(S.view) && D.closePair) D.closePair();
    else { S.view = null; render(); }
  }
  function closeModal() { S.view = null; S.fieldErr = ""; render(); }

  const FOCUSABLE = 'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])';
  // 配对弹窗管理自己的焦点与 Escape；设备内容由设置浮层承载。
  function trap(ev) {
    const top = modalEl && modalEl.firstChild ? "modal" : "";
    if (!top) return false;
    if (ev.key === "Escape") {
      if (top === "modal" && !["rename", "unpair", "claim"].includes(S.view)) return false; // pairing.js owns its modals' Escape (it cancels codes)
      if (S.view === "claim") { try { sessionStorage.setItem("xbot-claim-dismissed", "1"); } catch (e) {} }
      closeModal();
      return true;
    }
    if (ev.key !== "Tab") return false;
    const layer = modalEl;
    const items = Array.from(layer.querySelectorAll(FOCUSABLE));
    if (!items.length) return false;
    const i = items.indexOf(ev.target);
    ev.preventDefault();
    const next = ev.shiftKey ? (i <= 0 ? items.length - 1 : i - 1) : (i < 0 || i === items.length - 1 ? 0 : i + 1);
    items[next].focus();
    return true;
  }

  function modal(inner, cls) { return `<div class="modal${cls ? " " + cls : ""}" data-g7-modal><div class="modal-card g7-card" role="dialog" aria-modal="true">${inner}</div></div>`; }

  // 中文与西文、数字之间加空格（原型里是脚本统一加的，这里写进文案）。
  const sp = (s) => String(s).replace(/([\u4e00-\u9fff])([A-Za-z0-9])/g, "$1 $2").replace(/([A-Za-z0-9%])([\u4e00-\u9fff])/g, "$1 $2");
  // 改名弹窗里的地址举例：拉丁名字的小写，中文名字用「名字」（和原型一致）。
  const addrName = (label, name) => (/^[A-Za-z]/.test(label || "") && name && name !== "device" ? name : "名字");
  // 相对时间只用于「3 分钟前联系过」，一天以内；更早显示日期。
  function ago(iso) {
    const t = new Date(iso).getTime();
    if (!iso || isNaN(t) || new Date(iso).getFullYear() < 2000) return "";
    const m = Math.round((Date.now() - t) / 60000);
    if (m < 1) return "刚刚联系过";
    if (m < 60) return m + " 分钟前联系过";
    if (m < 24 * 60) return Math.round(m / 60) + " 小时前联系过";
    return "";
  }

  function viewRename() {
    const t = S.target || {};
    const self = t.self;
    if (self) {
      const others = ((S.data && S.data.devices) || []).filter((d) => !d.unpaired);
      const paired = others.length ? `已经配对的${others.map((d) => " " + esc(d.label)).join("、")} 上，还显示原来的名字。` : "";
      return modal(`<h2>这台电脑的名字</h2>
      <label class="field"><span>名字</span><input id="g7-name" type="text" maxlength="40" placeholder="这台电脑的名字" value="${esc(t.label)}"></label>
      <p class="g7-fine">只对以后配对的设备生效。${paired}最多 40 个字。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="save-name">保存</button></div>`, "g7-over");
    }
    return modal(`<h2>重命名 ${esc(t.label)}</h2>
      <label class="field"><span>名字</span><input id="g7-name" type="text" maxlength="40" value="${esc(t.label)}"></label>
      <p class="g7-fine">只改这台电脑上的叫法，${esc(t.label)} 那边不变。Bot 的地址也不变，比如 <code class="g7-addr">${esc(addrName(t.label, t.name))}@${esc(t.name)}</code>。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="save-name">保存</button></div>`, "g7-over");
  }
  function viewUnpair() {
    const t = S.target || {};
    const n = (t.bots || []).length;
    return modal(`<h2>解除和 ${esc(t.label)} 的配对？</h2>
      <p class="g7-lead">侧栏里「${esc(t.label)}」会消失，这里不能用它的 ${n} 个 Bot，两边的 Bot 也不能互发消息。</p>
      <p class="g7-lead">会尽量通知 ${esc(t.label)}，它那边会显示「已解除配对」。通知没送到，这边也照样解除。想再连，重新配对就行。</p>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close">取消</button><button class="primary" type="button" data-act="unpair">解除配对</button></div>`, "g7-over");
  }
  // 和侧栏同一个头像：灰阶圆 + 首字，拉丁名两个字母，中文单字（导出的 D.sideFaces，所以下面不改侧栏的 face）。
  const faceX = (n, id) => (window.XBotFaces ? D.sideFaces(n, id, "xs") : `<span class="avatar xs">${D.sideFaces(n, id)}</span>`);
  // 在线时写多久前联系过（「3 分钟前联系过」）；隔天或对方一整天都显得在线时，写日期。
  const statusLine = (d, up) => {
    if (d.unpaired) return `<span class="g7-status">${svg("unlink", 13)}已解除配对 · 对方解除的</span>`;
    const seen = fmtSeen(d.seen);
    if (!seen) return `<span class="g7-status"><i class="status-dot off-ring"></i>还没收到它的 Bot 列表 · 几秒后会更新</span>`;
    if (!up) return `<span class="g7-status"><i class="status-dot off-ring"></i>连不上网关 · 最后联系 ${esc(seen)}</span>`;
    if (d.online) return `<span class="g7-status"><i class="status-dot ok"></i>在线${ago(d.seen) ? " · " + ago(d.seen) : " · " + esc(seen) + "联系过"}</span>`;
    return `<span class="g7-status"><i class="status-dot off-ring"></i>离线 · 最后联系 ${esc(seen)}</span>`;
  };
  function capability(d) {
    const on = d.bot_messaging !== false, err = S.bmErrMsg && S.bmErr === d.id ? S.bmErrMsg : ""; // 关掉时行也变淡，错误照常显示
    return `<div class="g7-caps${on ? "" : " is-off"}"><div class="g7-cap"><span class="g7-cap-t"><b>Bot 互发消息</b></span>
      <button class="g7-switch" id="g7-bm-${esc(d.id)}" type="button" role="switch" aria-checked="${on}" aria-label="Bot 互发消息" data-act="bm-toggle" data-id="${esc(d.id)}"></button></div>
      ${err ? `<p class="error g7-inline-err">${esc(err)}</p>` : ""}</div>`;
  }
  function devCard(d, up) {
    const open = S.menuFor === d.id;
    if (d.unpaired) {
      // 对方解除了：不显示 Bot，也不显示能力；「移除」直接删本机记录。
      return `<div class="g7-dev-card is-gone">
        <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
          <div class="g7-dev-main"><div class="g7-dev-name">${esc(d.label)}</div>${statusLine(d, up)}</div>
          <button class="secondary g7-sm" type="button" data-act="remove" data-id="${esc(d.id)}">${svg("x", 14)}<span>移除</span></button></div>
        <div class="g7-meta">${esc(d.label)} 那边已经解除配对，这里不能再用它的 Bot，两边的 Bot 也不能互发消息。移除后，想再连就重新配对。</div></div>`;
    }
    const menu = open ? `<div class="pop g7-row-menu">
        <button type="button" class="menu-row" data-act="rename" data-id="${esc(d.id)}">${svg("pencil")}<span>重命名</span></button>
        <button type="button" class="menu-row" data-act="ask-unpair" data-id="${esc(d.id)}">${svg("unlink")}<span>解除配对</span></button></div>` : "";
    const bots = (d.bots || []).length ? `<div class="g7-bots">${d.bots.map((b) => `<span class="g7-bot">${faceX(b, d.id)}<span>${esc(b)}</span></span>`).join("")}</div>` : "";
    const added = fmtSeen(d.added); // 今天 09:12 / 昨天 18:20 / 10月12日
    const date = !added ? "" : /^今天/.test(added) ? "今天" : added;
    const meta = `<div class="g7-meta"><span>Bot 地址</span><code class="g7-addr">名字@${esc(d.name)}</code>${date ? `<span>·</span><span>${esc(date)}添加</span>` : ""}</div>`;
    const caps = (d.bots || []).length ? capability(d) : ""; // 还没收到 Bot 列表时不给开关
    return `<div class="g7-dev-card${up && !d.online ? " is-off" : ""}">
      <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(d.label)}</div>${statusLine(d, up)}</div>
        <div class="g7-more-wrap"><button class="ghost${open ? " is-on" : ""}" type="button" aria-label="更多" aria-expanded="${open}" data-act="row-menu" data-id="${esc(d.id)}">${svg("ellipsis")}</button>${menu}</div></div>
      ${bots}<div class="g7-details">${meta}${caps}</div></div>`;
  }
  function viewDevices() {
    const data = S.data || { self: {}, devices: [] };
    const self = data.self || {}, devs = data.devices || [], gw = self.gateway, up = !!data.gateway_ok;
    // 本机绑到当前账号时只在这台电脑卡里弱标，不另列账号设备。
    const bound = !!(S.acct && S.acct.owner && !S.acct.unbound);
    const selfCard = `<section class="profile-section"><h3>这台电脑</h3>
      <div class="g7-device g7-self"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(self.name)}${bound ? `<span class="g7-bound">已绑到账号</span>` : ""}</div><div class="g7-dev-sub">${esc([self.os, (self.bots || []).length + " 个 Bot"].filter(Boolean).join(" · "))}</div></div>
        <button class="ghost" type="button" aria-label="改名" data-act="rename-self">${svg("pencil")}</button></div>
      <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">电脑操作</div><div class="g7-dev-sub" id="g7-desktop-status">${esc(S.desktop ? S.desktop.label : "正在检查授权")}</div></div>
        <button class="secondary g7-sm" type="button" data-act="desktop-check">重新检查</button></div>
      <p class="g7-fine" id="g7-desktop-help">${esc(S.desktop ? S.desktop.guide : "")}</p></section>`;
    // 只在已登录且本机未绑时出现；已绑、未登都整节不渲染。
    const claim = S.acct && S.acct.unbound ? `<section class="profile-section g7-claim" id="g7-claim"><b>通过账号关联设备</b>
      <small>把这台电脑绑到账号，手机和其它入口也能看到这台上的 Bot。</small>
      ${S.claimErr ? `<p class="error g7-inline-err">${esc(S.claimErr)}</p>` : ""}
      <button class="secondary g7-sm" type="button" data-act="claim" ${S.claimBusy ? "disabled" : ""}>${S.claimBusy ? "绑定中" : "绑定这台电脑"}</button></section>` : "";
    const banner = gw && !up ? `<div class="g7-banner"><span class="g7-banner-ico">${svg("cloudOff", 18)}</span>
        <span>${sp("连不上网关" + host(gw))}<small>所有设备暂时显示离线，消息会先排队。</small></span>
        <button class="secondary g7-sm" type="button" data-act="gw-retry">${svg("refresh", 14)}<span>重试</span></button></div>` : "";
    let list;
    if (!devs.length) {
      list = `<section class="profile-section"><h3>已匹配设备</h3><div class="g7-empty"><svg class="doodle" width="220" height="110" viewBox="0 0 220 110" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><use href="#doodle-devices"/></svg><h3>还没有其它设备</h3>
          <p>点「配对设备」，在另一台电脑上输入短码配对。配对后它的 Bot 会出现在左侧，那台电脑保持在线就能聊。</p></div>
        </section>`;
    } else {
      list = `<section class="profile-section"><h3>已匹配设备 · ${devs.length}</h3>${devs.map((d) => devCard(d, up)).join("")}
          <p class="g7-fine">${sp("这里列的是配对过的电脑。「在线」表示最近能连上该设备上的 XBot。官网入口不是设备，不会出现在此列表。")}</p></section>`;
    }
    return `${S.acct ? '<button class="text-btn account-device-back" type="button" data-shared="manage">账号设备</button>' : ''}<div class="sethd"><h2>${S.acct ? '短码配对' : '设备'}</h2><button class="primary" type="button" data-act="add">${svg("plus", 14)}<span>配对设备</span></button></div>
      <div class="g7-lead">${sp("配对后，可在同一界面与各台电脑上的 Bot 聊天。记忆和文件仍在各自设备上。")}</div>${banner}
      <div class="g7-body">${selfCard}${claim}${list}</div>`;
  }
  // 认领确认（shot 04b）：POST /api/account/claim 是真绑定（bindIfFree + 上报账号）。
  function viewClaim() {
    const self = (S.data && S.data.self) || {};
    return modal(`<h2>把这台电脑绑到账号？</h2>
      <p class="g7-lead">绑定后，手机和其它入口也能看到这台电脑上的 Bot。</p>
      <div class="g7-device g7-flat"><span class="g7-dev-ico">${svg("laptop", 20)}</span>
        <div class="g7-dev-main"><div class="g7-dev-name">${esc(self.name || "这台电脑")}</div>
        <div class="g7-dev-sub">${esc([self.os, "本机", "未绑定"].filter(Boolean).join(" · "))}</div></div></div>
      ${S.claimErr ? `<p class="error g7-field-err">${esc(S.claimErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="claim-dismiss">先不绑</button><button class="primary" type="button" data-act="claim" ${S.claimBusy ? "disabled" : ""}>${S.claimBusy ? "绑定中" : "绑定这台电脑"}</button></div>
      <p class="g7-fine">账号内所有 Bot 均可使用绑定的设备。短码配对只用于与其他设备上的 Bot 互发消息。</p>`, "g7-claim-modal");
  }

  function render() {
    ensureHosts();
    const focusId = document.activeElement && document.activeElement.id;
    const sel = document.activeElement && "selectionStart" in document.activeElement ? document.activeElement.selectionStart : null;
    const selEnd = sel != null ? document.activeElement.selectionEnd : null;
    const selDir = sel != null ? document.activeElement.selectionDirection : null;
    const wasView = lastView;
    if (S.embedded && S.acct && !shared.legacy) paintSharedPage();
    else { delete devicesEl.dataset.sharedPage; devicesEl.innerHTML = S.embedded ? viewDevices() : ""; }
    const V = Object.assign({ rename: viewRename, unpair: viewUnpair, claim: viewClaim }, D.views)[S.view];
    modalEl.innerHTML = V ? V() : "";
    if (V && lastView === S.view) { const card = modalEl.querySelector(".modal-card"); if (card) card.style.animation = "none"; }
    lastView = S.view || "";
    const f = focusId && document.getElementById(focusId) || (S.menuFor && devicesEl.querySelector(`[data-act="row-menu"][data-id="${CSS.escape(S.menuFor)}"]`));
    const inside = document.activeElement === document.body || (devicesEl.contains(document.activeElement) || modalEl.contains(document.activeElement));
    if (f && !f.disabled) { f.focus(); if (sel != null && f.setSelectionRange) try { f.setSelectionRange(sel, selEnd, selDir); } catch (e) {} }
    else if (V && (S.view !== wasView || !inside)) { const first = modalEl.querySelector("input:not([disabled])") || modalEl.querySelector(FOCUSABLE); if (first) first.focus(); }
  }

  // ---- 事件 ----
  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "desktop-check": await checkDesktop(); break;
      case "close": closeModal(); break;
      case "to-drawer": if (D.closePair) await D.closePair(); else closeModal(); await openDevices(); break;
      case "row-menu": S.menuFor = S.menuFor === el.dataset.id ? "" : el.dataset.id; render(); break;
      case "rename": case "ask-unpair": {
        const d = (data.devices || []).find((x) => x.id === el.dataset.id);
        if (!d) return;
        S.menuFor = ""; S.target = d; S.fieldErr = ""; S.view = name === "rename" ? "rename" : "unpair"; render(); break;
      }
      case "rename-self": S.target = { self: true, label: data.self && data.self.name, os: data.self && data.self.os }; S.fieldErr = ""; S.view = "rename"; render(); break;
      case "save-name": {
        const v = (document.getElementById("g7-name").value || "").trim();
        if (!v) { S.fieldErr = "名字不能为空。"; render(); return; } // 空名字服务端也拒：本地先说
        try {
          if (S.target.self) await api("PATCH", "/api/devices/self", { name: v });
          else await api("PATCH", "/api/devices/" + encodeURIComponent(S.target.id), { label: v });
        } catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break;
      }
      case "unpair":
        try { await api("DELETE", "/api/devices/" + encodeURIComponent(S.target.id)); }
        catch (e) { S.fieldErr = e.message; render(); return; }
        S.view = null; await refresh(); render(); break; // 返回只有 ok，不知道通知送到没有：不弹 crop 21 的提示
      case "remove":
        try { await api("DELETE", "/api/devices/" + encodeURIComponent(el.dataset.id)); }
        catch (e) { S.bmErr = el.dataset.id; S.bmErrMsg = "没能移除：" + e.message; render(); return; }
        await refresh(); render(); break;
      case "bm-toggle": {
        // 后端在另一个 PR 里加 bot_messaging；没这个字段时按开着算。
        const d = (data.devices || []).find((x) => x.id === el.dataset.id);
        if (!d) return;
        const on = d.bot_messaging === false, old = d.bot_messaging; S.bmErrMsg = ""; // undefined 当开着
        d.bot_messaging = on; S.bmErr = ""; render();
        try { await api("PATCH", "/api/devices/" + encodeURIComponent(d.id), { bot_messaging: on }); }
        catch (e) { d.bot_messaging = old; S.bmErr = d.id; S.bmErrMsg = "没改成功：" + e.message + "。开关已经改回去了。"; }
        render(); break;
      }
      case "gw-retry":
        try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
        paintSidebar(); render(); break;
      case "claim": {
        // 真绑定：服务端 bindIfFree + 上报账号（account.go 的 accountClaim）。
        S.claimErr = ""; S.claimBusy = true; render();
        try {
          const out = await api("POST", "/api/account/claim");
          S.acct = (out && out.account) || S.acct;
          S.view = null;
          await refresh();
          await readSharedDevices();
        } catch (e) { S.claimErr = e.message; }
        S.claimBusy = false; render(); break;
      }
      case "claim-dismiss":
        S.view = null;
        try { sessionStorage.setItem("xbot-claim-dismissed", "1"); } catch (e) {}
        render(); break;
      default: if (D.acts[name]) await D.acts[name](el);
    }
  }

  document.addEventListener("click", (ev) => {
    const t = ev.target.closest("[data-act]");
    if (t && (devicesEl && devicesEl.contains(t) || modalEl && modalEl.contains(t))) { ev.preventDefault(); act(t.dataset.act, t); return; }
    if (S.menuFor && devicesEl && devicesEl.contains(ev.target) && !ev.target.closest(".g7-row-menu")) { S.menuFor = ""; render(); }
  });
  document.addEventListener("input", (ev) => {
    if (D.onInput) D.onInput(ev.target);
  });
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Enter" && ev.target.id === "g7-name") { ev.preventDefault(); act("save-name", ev.target); }
    if (D.onKey && D.onKey(ev)) return;
    if (devicesEl && (devicesEl.firstChild || modalEl.firstChild)) trap(ev);
  });

  // 倒计时只改文字，不重画
  setInterval(() => {
    if (!modalEl || !modalEl.firstChild) return;
    modalEl.querySelectorAll("[data-left]").forEach((el) => { el.textContent = left(el.dataset.left, Number(el.dataset.minus || 0)); });
  }, 1000);

  // Account execution uses account membership. Legacy pairing below only owns Bot messaging.
  const shared = {
    devices: null, error: "", bot: "", targets: new Map(), tasks: [], tasksError: "", unavailable: new Map(), tasksTruncated: false,
    detailID: "", detail: null, detailError: "", detailGeneration: 0, mountGeneration: 0,
    legacy: false, modal: null, modalGeneration: 0, opener: null, listRequest: null, taskRequest: null, scroll: new Map(),
  };
  const accountPath = id => "/api/account/devices/" + encodeURIComponent(id);
  const deviceOS = os => ({darwin:"macOS", windows:"Windows", linux:"Linux"}[os] || os || "");
  const platformMark = os => `<span class="account-device-glyph" role="img" aria-label="${esc(deviceOS(os) || "未报告系统")}" title="${esc(deviceOS(os) || "未报告系统")}">${svg(({darwin:"macos", windows:"windows", linux:"linux"})[os] || "devices")}</span>`;
  const taskPath = task => accountPath(task.device_id) + "/tasks/" + encodeURIComponent(task.ref);
  const chevron = '<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="m9 5 7 7-7 7"/></svg>';
  const monitor = '<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><rect x="3" y="3" width="18" height="13" rx="2"/><path d="M8 21h8m-4-5v5"/></svg>';
  const backIcon = '<svg class="icon" viewBox="0 0 24 24" aria-hidden="true"><path d="m15 18-6-6 6-6"/></svg>';
  const taskStatus = t => shared.unavailable.has(t.device_id) ? "设备暂不可达，状态待确认" : ({running:"正在执行", completed:"已完成", failed:"失败", cancelled:"已停止"}[t.status] || t.status);
  const selectedText = node => {
    const selection = window.getSelection();
    return selection && !selection.isCollapsed && selection.rangeCount && selection.getRangeAt(0).intersectsNode(node);
  };
  function textAt(root, selector, text) {
    const node = root.querySelector(selector), value = String(text || "");
    if (node && node.textContent !== value && !selectedText(node)) node.textContent = value;
  }
  function patchRows(host, rows, key, markup) {
    const keep = new Set();
    rows.forEach((item, index) => {
      const id = String(key(item)); keep.add(id);
      let row = [...host.children].find(node => node.dataset.key === id);
      const html = markup(item);
      if (!row) { row = document.createElement("div"); row.dataset.key = id; }
      if (row._markup !== html && !selectedText(row)) {
        const focused = row.contains(document.activeElement);
        row.innerHTML = html; row._markup = html;
        if (focused) row.querySelector("button:not(:disabled)")?.focus({preventScroll:true});
      }
      if (host.children[index] !== row) host.insertBefore(row, host.children[index] || null);
    });
    [...host.children].forEach(row => { if (!keep.has(row.dataset.key)) row.remove(); });
  }
  async function readSharedDevices() {
    if (shared.listRequest) return shared.listRequest;
    shared.listRequest = (async () => {
      try {
        const result = await api("GET", "/api/account/devices");
        if (!Array.isArray(result.devices)) throw new Error("设备列表读取失败");
        shared.devices = result.devices; shared.error = "";
        S.acctDevs = result.devices.length; paintFoot();
        for (const [bot, id] of shared.targets) {
          if (!result.devices.some(d => d.id === id)) setTarget(bot, "");
        }
      } catch (e) { shared.error = e.message; }
      paintTarget();
      if (S.embedded && S.acct && !shared.legacy) paintSharedPage();
      if (shared.modal === "picker") paintPicker();
    })().finally(() => { shared.listRequest = null; });
    return shared.listRequest;
  }
  function deviceRow(d, pick) {
    const chosen = pick && targetFor(shared.bot) === d.id;
    const description = pick && !d.online ? "离线 · 暂时无法使用" : [d.self ? "这台电脑" : "", !pick && !d.online && d.last_seen ? "上次在线 " + fmtSeen(d.last_seen) : ""].filter(Boolean).join(" · ");
    return `<button class="account-device-row${chosen ? " is-selected" : ""}" type="button" data-shared="${pick ? "choose" : "detail"}" data-id="${esc(d.id)}" ${pick && !d.online ? "disabled" : ""} ${pick ? `aria-pressed="${chosen}"` : ""}>
      ${platformMark(d.os)}<span class="account-device-main"><b>${esc(d.name)}</b><span>${esc(description)}</span></span>
      <span class="account-device-presence"><span><i class="status-dot ${d.online ? "ok" : ""}"></i>${pick ? "" : d.online ? "在线" : "离线"}</span>${!pick && d.running_count > 0 ? `<span>${d.running_count} 个任务</span>` : ""}</span>${pick ? chosen ? svg("check") : "" : chevron}</button>`;
  }
  function paintSharedPage() {
    if (!devicesEl || !S.embedded || shared.legacy) return;
    const mode = shared.detailID ? "detail:" + shared.detailID : "list";
    if (devicesEl.dataset.sharedPage !== mode) {
      const scroller = devicesEl.closest(".setcnt");
      if (devicesEl.dataset.sharedPage) shared.scroll.set(devicesEl.dataset.sharedPage, scroller.scrollTop);
      devicesEl.dataset.sharedPage = mode;
      shared.restoreScroll = shared.scroll.get(mode) || 0;
      devicesEl.innerHTML = shared.detailID
        ? `<button class="icon-btn account-device-back" type="button" data-shared="back" aria-label="返回设备" title="返回设备">${backIcon}</button><div class="account-device-heading"><span data-device-platform></span><div><h2><button class="account-device-name" type="button" data-device-name data-shared="rename" aria-label="修改设备名称" title="修改设备名称"></button></h2><p class="account-device-note" data-device-presence></p></div></div><p class="error" data-device-error role="status"></p><div data-device-detail></div>`
        : `<div class="sethd"><h2>设备</h2><button class="icon-btn" type="button" data-shared="add" aria-label="添加设备" title="添加设备">${svg("plus")}</button></div><p class="account-device-note">账号内所有 Bot 均可使用这些设备。</p><div data-account-claim></div><div data-account-list></div><p class="account-device-note account-device-footer">设备在线时，Bot 可使用设备上的文件、工具与应用。</p><button class="text-btn" type="button" data-shared="legacy">短码配对与其他设备的 Bot</button>`;
    }
    if (shared.detailID) { paintDeviceDetail(); return; }
    const claim = devicesEl.querySelector('[data-account-claim]');
    const claimHTML = S.acct.unbound ? `<div class="account-device-section"><p class="account-device-note">这台电脑尚未绑定。绑定后，账号内所有 Bot 均可使用。</p><p class="error" role="status">${esc(S.claimErr)}</p><button class="secondary" type="button" data-act="claim" ${S.claimBusy ? "disabled" : ""}>绑定这台电脑</button></div>` : "";
    if (claim._html !== claimHTML) { claim.innerHTML = claimHTML; claim._html = claimHTML; }
    const host = devicesEl.querySelector('[data-account-list]');
    const notice = shared.error ? `<p class="error" role="status">${esc(shared.error)}</p><button class="secondary" type="button" data-shared="retry">重试</button>`
      : shared.devices === null ? '<p class="account-device-note">正在读取设备…</p>'
      : !shared.devices.length ? '<div class="account-device-empty"><h3>还没有设备</h3><p>在新电脑登录同一账号，并绑定这台电脑。</p><button class="primary" type="button" data-shared="add">添加设备</button></div>' : "";
    let status = host.querySelector('[data-list-status]');
    if (!status) { status = document.createElement("div"); status.dataset.listStatus = ""; host.prepend(status); }
    if (status._html !== notice) { status.innerHTML = notice; status._html = notice; }
    let rows = host.querySelector('[data-list-rows]');
    if (!rows) { rows = document.createElement("div"); rows.dataset.listRows = ""; rows.className = "account-device-list"; host.append(rows); }
    patchRows(rows, shared.devices || [], d => d.id, d => deviceRow(d, false));
    restoreDeviceScroll();
  }
  function restoreDeviceScroll() {
    if (shared.restoreScroll == null) return;
    devicesEl.closest(".setcnt").scrollTop = shared.restoreScroll; shared.restoreScroll = null;
  }
  async function openDevice(id) {
    shared.lastDeviceID = id;
    shared.detailID = id; shared.detail = null; shared.detailError = "";
    paintSharedPage();
    devicesEl.querySelector('[data-shared="back"]')?.focus({preventScroll:true});
    await readDeviceDetail();
  }
  async function readDeviceDetail() {
    const id = shared.detailID, generation = ++shared.detailGeneration;
    if (!id) return;
    try {
      const result = await api("GET", accountPath(id));
      if (generation !== shared.detailGeneration || id !== shared.detailID || !S.embedded) return;
      shared.detail = result; shared.detailError = "";
    } catch (e) { if (generation === shared.detailGeneration) shared.detailError = e.message; }
    if (generation === shared.detailGeneration && S.embedded) paintDeviceDetail();
  }
  function paintDeviceDetail() {
    const root = devicesEl, detail = shared.detail;
    const d = detail?.device || (shared.devices || []).find(d => d.id === shared.detailID);
    if (!d) return;
    textAt(root, '[data-device-name]', d.name);
    root.querySelector('[data-device-name]').setAttribute('aria-label', '修改设备名称：' + d.name);
    root.querySelector('[data-device-name]').disabled = !detail;
    const platform = root.querySelector('[data-device-platform]');
    if (platform.dataset.os !== (d.os || "")) { platform.innerHTML = platformMark(d.os); platform.dataset.os = d.os || ""; }
    textAt(root, '[data-device-presence]', d.online ? "在线" : "离线" + (d.last_seen ? " · 上次在线 " + fmtSeen(d.last_seen) : ""));
    textAt(root, '[data-device-error]', shared.detailError);
    const body = root.querySelector('[data-device-detail]');
    if (!detail) {
      const html = shared.detailError ? '<button class="secondary" type="button" data-shared="retry-detail">重试</button>' : '<p class="account-device-note">正在读取详情…</p>';
      if (body._loading !== html) { body.innerHTML = html; body._loading = html; }
      return;
    }
    if (!body.querySelector('[data-detail-config]')) {
      body.innerHTML = `<section class="account-device-section" data-detail-config><h3>能力</h3><div class="account-device-caps" data-detail-caps></div></section><section class="account-device-section"><h3 data-detail-tasks-title></h3><p class="account-device-note" data-detail-task-note hidden></p><div data-detail-tasks></div></section><div data-detail-desktop></div><button class="icon-btn account-device-unbind" type="button" data-shared="unbind" aria-label="解除绑定" title="解除绑定">${svg("unlink")}</button>`;
    }
    const capNames = {read:"读取文件", write:"写入文件", exec:"终端", browser:"浏览器", desktop:"电脑操作"};
    const capabilities = [...new Set((d.capabilities || []).map(c => c.startsWith("exec:") ? "exec" : c))];
    const caps = body.querySelector('[data-detail-caps]');
    caps.closest("section").hidden = !capabilities.length;
    patchRows(caps, capabilities, c => c, c => `<span class="account-device-cap">${esc(capNames[c] || c)}</span>`);
    const tasks = detail.tasks || [];
    textAt(body, '[data-detail-tasks-title]', tasks.length ? "当前任务 · " + tasks.length : "当前没有任务");
    textAt(body, '[data-detail-task-note]', detail.tasks_truncated ? "仅显示最近的任务记录" : "");
    body.querySelector('[data-detail-task-note]').hidden = !detail.tasks_truncated;
    patchRows(body.querySelector('[data-detail-tasks]'), tasks, t => t.ref, taskRow);
    const desktop = body.querySelector('[data-detail-desktop]');
    if (d.self && !desktop.firstChild) {
      desktop.innerHTML = `<section class="account-device-section"><h3>这台电脑的电脑操作</h3><div class="setrow"><span id="g7-desktop-status">正在检查授权</span><button class="icon-btn" type="button" data-act="desktop-check" aria-label="重新检查" title="重新检查">${svg("refresh")}</button></div><p class="account-device-note" id="g7-desktop-help"></p></section>`;
      checkDesktop();
    }
    restoreDeviceScroll();
  }
  function taskRow(t) {
    return `<button type="button" class="account-device-task account-device-row" data-shared="task" data-device="${esc(t.device_id)}" data-ref="${esc(t.ref)}"><span class="account-device-main"><b>${esc(t.description || "设备任务")}</b><span>${esc([t.bot, taskStatus(t)].filter(Boolean).join(" · "))}</span></span>${chevron}</button>`;
  }
  let sharedLayer;
  function closeSharedModal(restore = true) {
    if (!sharedLayer?.firstChild) return;
    ++shared.modalGeneration;
    sharedLayer.querySelector("dialog").close(); sharedLayer.replaceChildren();
    shared.modal = null;
    document.getElementById("account-device-trigger").setAttribute("aria-expanded", "false");
    const opener = shared.opener?.isConnected ? shared.opener : shared.openerKey ? document.querySelector(shared.openerKey) : null;
    if (restore) (opener || document.getElementById("account-device-trigger")).focus({preventScroll:true});
    shared.opener = null; shared.openerKey = "";
  }
  function sharedModal(kind, title, body, actions = "") {
    closeSharedModal(false);
    shared.opener = document.activeElement;
    shared.openerKey = shared.opener?.matches('[data-shared]') ? ["shared", "id", "device", "ref"].filter(k => k in shared.opener.dataset).map(k => `[data-${k}="${CSS.escape(shared.opener.dataset[k])}"]`).join("") : "";
    shared.modal = kind;
    if (!sharedLayer) { sharedLayer = document.createElement("div"); sharedLayer.id = "account-devices-modal"; document.body.append(sharedLayer); }
    sharedLayer.innerHTML = `<dialog class="account-device-dialog${kind === "picker" ? " account-device-picker" : ""}" aria-label="${esc(title)}"><header><h2>${esc(title)}</h2><button class="icon-btn" type="button" data-shared="close" aria-label="关闭">${svg("x")}</button></header><div class="account-device-modal-body">${body}</div>${actions ? `<footer>${actions}</footer>` : ""}</dialog>`;
    const dialog = sharedLayer.firstChild;
    dialog.addEventListener("cancel", ev => { ev.preventDefault(); closeSharedModal(); });
    dialog.addEventListener("click", ev => { if (ev.target === dialog) { const r = dialog.getBoundingClientRect(); if (ev.clientX < r.left || ev.clientX > r.right || ev.clientY < r.top || ev.clientY > r.bottom) closeSharedModal(); } });
    dialog.showModal();
    (dialog.querySelector("input") || dialog.querySelector('[data-shared="close"]')).focus();
    return dialog;
  }
  function setTarget(bot, id) {
    if (id) shared.targets.set(bot, id); else shared.targets.delete(bot);
    try { if (id) sessionStorage.setItem("xbot-device-target:" + bot, id); else sessionStorage.removeItem("xbot-device-target:" + bot); } catch (e) {}
    paintTarget();
  }
  function targetFor(bot) {
    if (!S.acct || !bot) return "";
    if (!shared.targets.has(bot)) {
      try { const id = sessionStorage.getItem("xbot-device-target:" + bot); if (id) shared.targets.set(bot, id); } catch (e) {}
    }
    const id = shared.targets.get(bot) || "";
    if (id && shared.devices && !shared.devices.some(d => d.id === id)) {
      shared.targets.delete(bot);
      try { sessionStorage.removeItem("xbot-device-target:" + bot); } catch (e) {}
      return "";
    }
    return id;
  }
  function paintTarget() {
    const foot = document.getElementById("account-device-foot"), button = document.getElementById("account-device-trigger");
    foot.hidden = !shared.bot || !S.acct;
    const id = targetFor(shared.bot), device = (shared.devices || []).find(d => d.id === id);
    const label = "本次使用：" + (id ? (device?.name || "所选设备") + (device && !device.online ? "（离线）" : "") : "自动");
    if (button.dataset.label !== label) { button.innerHTML = monitor + `<span>${esc(label)}</span>` + chevron; button.dataset.label = label; }
  }
  function openPicker() {
    sharedModal("picker", "本次使用的设备", `<div class="account-device-search"><input class="ui-input" type="search" aria-label="搜索设备" placeholder="搜索设备" id="account-device-search"></div><div data-picker-status role="status"></div><div class="account-device-picker-list" data-picker-rows></div>`, `<p class="account-device-note">只用于 ${esc(shared.bot)} 的下一条消息，发送后恢复自动。</p><button class="text-btn" type="button" data-shared="manage">管理账号设备</button>`);
    document.getElementById("account-device-trigger").setAttribute("aria-expanded", "true");
    positionPicker(); paintPicker(); readSharedDevices();
  }
  function positionPicker() {
    if (shared.modal !== "picker") return;
    const dialog = sharedLayer.querySelector("dialog"), rect = document.getElementById("account-device-trigger").getBoundingClientRect();
    const inset = parseFloat(getComputedStyle(dialog).getPropertyValue("--x-s6"));
    dialog.style.setProperty("--account-picker-left", Math.max(inset, Math.min(rect.left, innerWidth - dialog.offsetWidth - inset)) + "px");
    dialog.style.setProperty("--account-picker-bottom", innerHeight - rect.top + inset + "px");
  }
  window.addEventListener("resize", positionPicker);
  function paintPicker() {
    if (shared.modal !== "picker") return;
    const search = sharedLayer.querySelector("input"), query = search.value.trim().toLowerCase();
    const rows = (shared.devices || []).filter(d => [d.name,d.os,deviceOS(d.os)].join(" ").toLowerCase().includes(query));
    const host = sharedLayer.querySelector('[data-picker-rows]');
    const items = query ? rows : [{id:"",name:"自动"}, ...rows];
    patchRows(host, items, d => d.id, d => d.id ? deviceRow(d, true) : `<button class="account-device-row${!targetFor(shared.bot) ? " is-selected" : ""}" type="button" data-shared="choose" data-id=""><span class="account-device-glyph">${svg("swap")}</span><span class="account-device-main"><b>自动</b><span>由 Bot 选择合适的在线设备</span></span>${!targetFor(shared.bot) ? svg("check") : ""}</button>`);
    const status = shared.error ? `<p class="error">${esc(shared.error)}</p><button class="secondary" type="button" data-shared="retry">重试</button>` : shared.devices === null ? '<p class="account-device-note">正在读取设备…</p>' : query && !rows.length ? '<p class="account-device-note">没有匹配的设备</p><button class="text-btn" type="button" data-shared="clear-search">清除搜索</button>' : !shared.devices.length ? '<p class="account-device-note">还没有设备，请先添加设备。</p>' : "";
    const statusNode = sharedLayer.querySelector('[data-picker-status]');
    if (statusNode._html !== status) { statusNode.innerHTML = status; statusNode._html = status; }
  }
  function addDevice() {
    sharedModal("add", "添加设备", `<p>在新电脑打开 XBot。</p><ol class="account-device-steps"><li>登录同一账号${S.acct?.email ? "：" + esc(S.acct.email) : ""}。</li><li>在新电脑主动选择「绑定这台电脑」。</li><li>保持 XBot 在线，再回到这里检查。</li></ol><p class="account-device-note" data-add-result role="status"></p>`, `<div class="modal-actions"><button class="icon-btn" type="button" data-shared="check-new" aria-label="检查设备" title="检查设备">${svg("refresh")}</button></div>`);
  }
  async function editDevice(remove) {
    const d = shared.detail?.device; if (!d) return;
    const dialog = sharedModal(remove ? "unbind" : "rename", remove ? "解除和「" + d.name + "」的绑定？" : "设备名称", remove ? '<p>账号内所有 Bot 将不能再使用这台设备。设备上的文件和 Bot 历史会保留。</p>' : `<label class="field"><span>名称</span><input class="ui-input" type="text" maxlength="40" value="${esc(d.name)}" aria-label="设备名称"></label>`, `<p class="error" role="status" data-shared-error></p><div class="modal-actions"><button class="secondary" type="button" data-shared="close">取消</button><button class="${remove ? "account-device-danger" : "primary"}" type="button" data-shared="save-device">${remove ? "解除绑定" : "保存"}</button></div>`);
    if (remove) dialog.querySelector('footer [data-shared="close"]').focus();
    dialog.querySelector('[data-shared="save-device"]').onclick = async () => {
      const value = dialog.querySelector("input")?.value.trim();
      if (!remove && !value) { textAt(dialog, '[data-shared-error]', "名称不能为空"); return; }
      const generation = shared.modalGeneration;
      dialog.querySelectorAll('footer button').forEach(b => b.disabled = true);
      try {
        const result = await api(remove ? "DELETE" : "PATCH", accountPath(d.id), remove ? undefined : {name:value});
        if (generation !== shared.modalGeneration) return;
        if (remove) { shared.detailID = ""; shared.detail = null; devicesEl.dataset.sharedPage = ""; }
        else shared.detail.device = result.device;
        closeSharedModal(); await readSharedDevices(); paintSharedPage();
        if (remove) devicesEl.querySelector('[data-shared="add"]')?.focus();
      } catch (e) { if (generation === shared.modalGeneration) textAt(dialog, '[data-shared-error]', e.message); }
      finally { if (generation === shared.modalGeneration) dialog.querySelectorAll('footer button').forEach(b => b.disabled = false); }
    };
    dialog.querySelector("input")?.addEventListener("keydown", ev => { if (ev.key === "Enter" && !ev.isComposing) { ev.preventDefault(); dialog.querySelector('[data-shared="save-device"]').click(); } });
  }
  async function readBotDeviceTasks() {
    if (!shared.bot || !S.acct || shared.taskRequest) return;
    const bot = shared.bot;
    shared.taskRequest = (async () => {
      try {
        const result = await api("GET", "/api/bots/" + encodeURIComponent(bot) + "/device-tasks");
        if (bot !== shared.bot) return;
        if (!Array.isArray(result.tasks)) throw new Error("任务状态读取失败");
        shared.tasksTruncated = !!result.tasks_truncated;
        shared.unavailable = new Map((result.unavailable_devices || []).map(device => [device.id, device]));
        const known = shared.tasks.filter(task => shared.unavailable.has(task.device_id) && !result.tasks.some(fresh => fresh.device_id === task.device_id && fresh.ref === task.ref));
        shared.tasks = [...result.tasks, ...known]; shared.tasksError = "";
      } catch (e) { if (bot === shared.bot) shared.tasksError = e.message; }
      if (bot === shared.bot) paintDeviceTaskStatus();
    })().finally(() => { shared.taskRequest = null; });
    return shared.taskRequest;
  }
  function paintDeviceTaskStatus() {
    let row = document.getElementById("account-device-task-status");
    if (!row) { row = document.createElement("button"); row.type = "button"; row.id = "account-device-task-status"; row.className = "account-device-task-status"; document.getElementById("composer").before(row); }
    const active = shared.tasks.filter(t => t.status === "running" || shared.unavailable.has(t.device_id));
    const task = active[0] || shared.tasks[0];
    row.hidden = !shared.bot || !S.acct || (!task && !shared.tasksError);
    const label = active.length > 1 ? active.length + " 个设备任务" + (shared.unavailable.size ? " · 部分设备状态待确认" : "") : task ? [task.device_name, task.description || "设备任务", taskStatus(task), shared.tasksError ? "状态暂未更新" : ""].filter(Boolean).join(" · ") : "任务状态读取失败，点击重试";
    if (row.textContent !== label && !selectedText(row)) row.textContent = label;
    row.onclick = () => {
      if (active.length > 1) {
        const dialog = sharedModal("tasks", "设备任务", '<p class="account-device-note" data-current-task-note hidden></p><div data-current-device-tasks></div>');
        textAt(dialog, '[data-current-task-note]', shared.tasksTruncated ? "仅显示最近的任务记录" : "");
        dialog.querySelector('[data-current-task-note]').hidden = !shared.tasksTruncated;
        patchRows(dialog.querySelector('[data-current-device-tasks]'), active, t => t.device_id + ":" + t.ref, taskRow);
      } else if (task) openTask(task);
      else readBotDeviceTasks();
    };
    row.title = shared.tasksError ? "状态读取失败，点击重试" : "查看执行详情";
    if (shared.modal === "tasks") {
      textAt(sharedLayer, '[data-current-task-note]', shared.tasksTruncated ? "仅显示最近的任务记录" : "");
      sharedLayer.querySelector('[data-current-task-note]').hidden = !shared.tasksTruncated;
      patchRows(sharedLayer.querySelector('[data-current-device-tasks]'), active, t => t.device_id + ":" + t.ref, taskRow);
    }
  }
  async function openTask(task) {
    const fromTaskList = shared.modal === "tasks";
    const dialog = sharedModal("task", "执行详情", `<h3 data-task-title></h3><p class="account-device-note" data-task-meta></p><p class="error" data-task-error role="status"></p><pre class="account-device-output" data-task-output></pre><button class="text-btn" type="button" data-shared="more-output" hidden>读取更多</button><div data-task-artifacts></div>`, `<p class="error" role="status" data-shared-error></p><div class="modal-actions"><button class="icon-btn" type="button" data-shared="refresh-task" aria-label="重新读取" title="重新读取">${svg("refresh")}</button><button class="icon-btn account-device-stop" type="button" data-shared="stop-task" aria-label="停止任务" title="停止任务">${svg("square")}</button></div>`);
    if (fromTaskList) shared.opener = document.getElementById("account-device-task-status");
    dialog._task = task; dialog._offset = 0; dialog._output = "";
    paintTask(dialog, task); await readTaskOutput(dialog);
  }
  function paintTask(dialog, task) {
    dialog._task = task;
    textAt(dialog, '[data-task-title]', task.description || "设备任务");
    textAt(dialog, '[data-task-meta]', [task.device_name, task.bot, taskStatus(task)].filter(Boolean).join(" · "));
    textAt(dialog, '[data-task-error]', task.error || "");
    dialog.querySelector('[data-shared="stop-task"]').hidden = task.status !== "running";
    patchRows(dialog.querySelector('[data-task-artifacts]'), task.artifacts || [], a => a.id, a => `<button class="text-btn" type="button" data-shared="artifact" data-id="${esc(a.id)}">${esc(a.name)}</button>`);
  }
  async function readTaskOutput(dialog) {
    if (dialog._reading) return;
    dialog._reading = true;
    const generation = shared.modalGeneration;
    try {
      const result = await api("GET", taskPath(dialog._task) + "?offset=" + dialog._offset + "&limit=16384");
      if (generation !== shared.modalGeneration) return;
      dialog._output += result.output; dialog._offset = result.next_offset;
      paintTask(dialog, result.task); textAt(dialog, '[data-task-output]', dialog._output);
      dialog.querySelector('[data-shared="more-output"]').hidden = !result.output;
      textAt(dialog, '[data-shared-error]', "");
    } catch (e) { if (generation === shared.modalGeneration) textAt(dialog, '[data-shared-error]', e.message); }
    finally { dialog._reading = false; }
  }
  async function sharedAction(action, node) {
    switch (action) {
      case "close": closeSharedModal(); break;
      case "add": addDevice(); break;
      case "detail": await openDevice(node.dataset.id); break;
      case "back": shared.detailID = ""; ++shared.detailGeneration; paintSharedPage(); devicesEl.querySelector(`[data-shared="detail"][data-id="${CSS.escape(shared.lastDeviceID)}"]`)?.focus({preventScroll:true}); break;
      case "retry": await readSharedDevices(); break;
      case "retry-detail": await readDeviceDetail(); break;
      case "rename": await editDevice(false); break;
      case "unbind": await editDevice(true); break;
      case "choose": setTarget(shared.bot, node.dataset.id); closeSharedModal(); break;
      case "clear-search": sharedLayer.querySelector("input").value = ""; paintPicker(); sharedLayer.querySelector("input").focus(); break;
      case "manage": closeSharedModal(); await openDevices(); break;
      case "legacy": shared.legacy = true; delete devicesEl.dataset.sharedPage; render(); checkDesktop(); break;
      case "check-new": {
        node.disabled = true;
        const generation = shared.modalGeneration;
        await readSharedDevices();
        if (generation !== shared.modalGeneration) return;
        textAt(sharedLayer, '[data-add-result]', shared.error || "已重新读取账号设备。列表显示当前已绑定的设备。");
        node.disabled = false; break;
      }
      case "task": {
        const task = [...shared.tasks, ...(shared.detail?.tasks || [])].find(t => t.device_id === node.dataset.device && t.ref === node.dataset.ref);
        await openTask(task || {device_id:node.dataset.device, ref:node.dataset.ref}); break;
      }
      case "more-output": case "refresh-task": await readTaskOutput(sharedLayer.querySelector("dialog")); break;
      case "stop-task": {
        const dialog = sharedLayer.querySelector("dialog"), generation = shared.modalGeneration;
        node.disabled = true;
        try {
          const result = await api("POST", taskPath(dialog._task) + "/stop", {});
          if (generation !== shared.modalGeneration) return;
          paintTask(dialog, result.task); textAt(dialog, '[data-shared-error]', "");
          readBotDeviceTasks(); if (shared.detailID) readDeviceDetail();
        } catch (e) { if (generation === shared.modalGeneration) textAt(dialog, '[data-shared-error]', e.message); }
        finally { node.disabled = false; }
        break;
      }
      case "artifact": {
        const dialog = sharedLayer.querySelector("dialog"), task = dialog._task, generation = shared.modalGeneration;
        const artifact = task.artifacts.find(a => a.id === node.dataset.id), chunks = [];
        node.disabled = true;
        try {
          for (let offset = 0; offset < artifact.size;) {
            const response = await fetch(taskPath(task) + "/artifacts/" + encodeURIComponent(artifact.id) + "?offset=" + offset + "&limit=32768", {credentials:"same-origin"});
            if (!response.ok) throw new Error("产物读取失败");
            const bytes = await response.arrayBuffer();
            if (!bytes.byteLength) throw new Error("产物尚未完整读取，请重试");
            chunks.push(bytes); offset += bytes.byteLength;
            if (generation !== shared.modalGeneration) return;
          }
          const url = URL.createObjectURL(new Blob(chunks)), link = document.createElement("a");
          link.href = url; link.download = artifact.name; link.click(); URL.revokeObjectURL(url);
        } catch (e) { if (generation === shared.modalGeneration) textAt(dialog, '[data-shared-error]', e.message); }
        finally { node.disabled = false; }
        break;
      }
    }
  }
  document.getElementById("account-device-trigger").addEventListener("click", openPicker);
  document.addEventListener("input", ev => { if (ev.target.id === "account-device-search" && !ev.isComposing) paintPicker(); });
  document.addEventListener("compositionend", ev => { if (ev.target.id === "account-device-search") paintPicker(); });
  document.addEventListener("click", ev => { const node = ev.target.closest('[data-shared]'); if (node && !node.disabled) sharedAction(node.dataset.shared, node); });
  document.addEventListener("keydown", ev => {
    if (sharedLayer?.firstChild && ev.key === "Escape") { ev.preventDefault(); ev.stopImmediatePropagation(); closeSharedModal(); }
  }, true);
  async function reloadSharedAccount() {
    await loadAcct(); paintTarget();
    if (S.acct) { await readSharedDevices(); readBotDeviceTasks(); }
    else { shared.tasks = []; paintDeviceTaskStatus(); }
    readAcctRoster(true); // 侧栏那组也要跟着登录状态变
  }
  Object.assign(D, {
    targetFor, reloadAccount: reloadSharedAccount,
    signedOut() {
      S.acct = null; shared.devices = null; shared.tasks = []; shared.targets.clear();
      S.roster = null; rosterAt = 0;
      try { Object.keys(sessionStorage).filter(key => key.startsWith("xbot-device-target:")).forEach(key => sessionStorage.removeItem(key)); } catch (e) {}
      D.conversation(""); paintTarget(); paintDeviceTaskStatus();
    },
    conversation(bot) {
      if (bot === shared.bot) return;
      closeSharedModal(false); shared.bot = bot; shared.tasks = []; shared.unavailable.clear(); paintTarget(); paintDeviceTaskStatus();
      readBotDeviceTasks();
    },
    accepted(send) {
      if (send._deviceAccepted) return;
      send._deviceAccepted = true;
      if (targetFor(send.selection.id) === send.deviceID) setTarget(send.selection.id, "");
      readBotDeviceTasks();
    },
    renameBot(previous, name) {
      const id = targetFor(previous); setTarget(previous, ""); if (id) setTarget(name, id);
      if (shared.bot === previous) { shared.bot = name; paintTarget(); readBotDeviceTasks(); }
    },
  });
  setInterval(() => {
    if (document.hidden) return;
    if (S.acct && (S.embedded || shared.modal === "picker")) readSharedDevices();
    if (S.embedded && shared.detailID && !shared.legacy) readDeviceDetail();
    readBotDeviceTasks();
    if (shared.modal === "task") readTaskOutput(sharedLayer.querySelector("dialog"));
  }, 3000);
  reloadSharedAccount();

  // ---- 侧栏：其他电脑上的 Bot，按设备分组（v2 原型 side-full、side-gw-down、rail-devices）----
  // 组头（label）也是一行按钮：不做折叠，所以设备和 Bot 行都放进 #roster（本机行下面），app.js 重画本机行时不会冲掉；
  // 网关不通时只在分组上方放一条提示、整体变淡，不给每台标离线；离线的设备变淡，但行照样能点开远端对话。
  // 设备有两路来源，画在同一组里：短码配对的（GET /api/devices，对话走加密信箱）和登录后同一账号里的其他电脑
  // （GET /api/account/devices，本机那台不算）。账号设备的对话通道还没打通，点开只在面板里说明一句，不发请求。
  const initials = (n) => { const c = Array.from(String(n || "?")); return /[A-Za-z]/.test(c[0]) ? (c[0].toUpperCase() + (c[1] || "").toLowerCase()) : c[0]; };
  // 远端 Bot 的人像按那台设备给的 bot_avatars 画：上传过图片的画几何图形（图片不过网关），旧版本的设备没给 seed 就按名字。
  const seedOf = (n, id) => {
    const d = ((S.data || {}).devices || []).find((x) => x.id === id);
    const a = (d && d.bot_avatars && d.bot_avatars[n]) || {};
    return !a.custom && a.avatar_seed ? a.avatar_seed : "glyph"; // 没给 seed 或上传过图片：几何图形，不借别的 Bot 的脸
  };
  const face = (n, id, size) => (window.XBotFaces ? window.XBotFaces.html(n, seedOf(n, id), size) : `<span class="avatar"><svg viewBox="0 0 36 36" aria-hidden="true" style="--avatar-light:#d3d3d3;--avatar-dark:#3d3d3d;--avatar-letter-light:#333333;--avatar-letter-dark:#e4e4e4"><circle cx="18" cy="18" r="18" fill="var(--avatar-fill)"/><text x="18" y="23" text-anchor="middle" font-size="${initials(n).length > 1 ? 13 : 15}" fill="var(--avatar-ink)">${esc(initials(n))}</text></svg></span>`);
  // 没有 Bot 列表的刚配对设备也列出来（「等它的 Bot 列表」）；对方解除配对的隐藏。点组头折叠，和本机分组一样。
  const sideFolds = () => { try { return JSON.parse(localStorage.getItem("xbot-device-folders") || "{}") || {}; } catch (e) { return {}; } };
  // 账号里同一账号的其他电脑（本机那台不算）；配对过的那台按 id 去重，一台设备只画一行。
  const acctOthers = () => (S.roster || []).filter((d) => d && d.id && !d.self);
  // 账号设备的 bots 是对象（{name, …}），配对设备的是名字；两种都认。
  const botNames = (d) => (d.bots || []).map((b) => (b && typeof b === "object" ? b.name : b)).filter(Boolean);
  function sideGroups() {
    const out = [], done = new Set();
    for (const d of ((S.data || {}).devices || [])) {
      if (d.unpaired || done.has(d.id)) continue;
      done.add(d.id);
      out.push({ id: d.id, label: d.label, bots: d.bots || [], chats: d.chats || {}, online: !!d.online, seen: d.seen, paired: true, chat: true });
    }
    for (const d of acctOthers()) {
      if (done.has(d.id)) continue;
      done.add(d.id);
      out.push({ id: d.id, label: d.name || d.id, bots: botNames(d), chats: {}, online: !!d.online, seen: d.last_seen, paired: false, chat: !!d.chat });
    }
    return out;
  }
  // 侧栏和远端对话标题认的一台设备：配对那路优先，账号那路补上。
  const sideDevice = (id) => sideGroups().find((d) => d.id === id)
    || { id, label: id, bots: [], chats: {}, online: false, seen: 0, paired: false, chat: false };
  // 点开一个 Bot 之前先问一句：这台设备有对话通道吗？账号通道关着（`chat` 为假）时面板里只写一句话，不发请求。
  const blockedReason = (id) => sideDevice(id).chat ? "" : "这台设备的对话通道还没打通，现在只能在这里看到它的 Bot。";
  let sideSig = "";
  function paintSidebar() {
    const nav = document.getElementById("remote-roster");
    if (!nav) return;
    const data = S.data || {};
    const groups = sideGroups(), pairs = groups.some((d) => d.paired);
    const up = !!data.gateway_ok, folds = sideFolds(), sel = D.chat && D.chat.isOpen() ? D.chat.key() : "";
    const sig = JSON.stringify([groups, up, folds, sel]);
    if (D.onPaint) D.onPaint(); // 打开着的远端对话跟着新的在线状态
    if (sig === sideSig) return;
    sideSig = sig;
    const had = document.activeElement && nav.contains(document.activeElement) ? document.activeElement.closest("[data-side]") : null;
    const key = had ? `[data-side="${had.dataset.side}"]` + (had.dataset.id ? `[data-id="${CSS.escape(had.dataset.id)}"]` : "") + (had.dataset.bot ? `[data-bot="${CSS.escape(had.dataset.bot)}"]` : "") : "";
    const status = (d) => {
      if (!d.bots.length) return `<span class="g7-side-st">等它的 Bot 列表</span>`;
      if (d.paired && !up) return ""; // 网关不通：上面一条提示，不逐台标离线（账号设备不看这条网关）
      if (d.online) return `<span class="g7-side-st"><i class="status-dot ok"></i>在线</span>`;
      const seen = fmtSeen(d.seen) || "很久以前";
      if (d.paired) return `<span class="g7-side-st" title="离线 · 最后联系 ${esc(seen)}"><i class="status-dot off-ring"></i>${esc(seen)}</span>`;
      return `<span class="g7-side-st" title="离线 · 上次在线 ${esc(seen)}"><i class="status-dot off-ring"></i>${esc(seen)}</span>`;
    };
    const row = (d, b) => {
      const on = sel === d.id + "\u0000" + b;
      // 账号设备的 Bot：这台还没有对话通道，只画名字；点开在面板里说明，不发请求。
      if (!d.paired) return `<button class="item g7-remote-bot${on ? " active" : ""}" type="button" data-side="open" data-id="${esc(d.id)}" data-bot="${esc(b)}" data-tip="${esc(b)}" data-tip-placement="right">
        ${face(b, d.id)}<span class="item-body"><span class="name-line"><span class="name">${esc(b)}</span></span></span></button>`;
      const c = (d.chats || {})[b] || {};
      const body = String(c.last || "").trim(), me = c.last_role === "me" ? "你：" : "";
      const pv = c.queued ? `<span class="preview g7-side-q"><b class="g7-side-qb">◷ 排队中</b> · 你：${esc(c.queued)}</span>`
        : `<span class="preview">${body ? esc(me + body) : "还没有消息"}</span>`;
      return `<button class="item g7-remote-bot${on ? " active" : ""}" type="button" data-side="open" data-id="${esc(d.id)}" data-bot="${esc(b)}" data-tip="${esc(b)}" data-tip-placement="right">
        ${c.unread > 0 ? face(b, d.id).replace(/<\/span>$/, '<i class="unread"></i></span>') : face(b, d.id)}<span class="item-body"><span class="name-line"><span class="name">${esc(b)}</span></span>${pv}</span></button>`;
    };
    nav.hidden = !groups.length;
    nav.innerHTML = !groups.length ? "" : (up || !pairs ? "" : `<div class="g7-side-down"><span>连不上网关，其他电脑的 Bot 暂时用不了。</span><button class="ghost" type="button" aria-label="重试" data-side="retry">${svg("refresh")}</button></div>`)
      + groups.map((d) => {
        const closed = !!folds[d.id];
        const empty = d.paired ? "刚配对，几秒后会列出它的 Bot。" : "它还没把 Bot 名单报给账号，等它下次上线。";
        const rows = d.bots.length ? d.bots.map((b) => row(d, b)).join("") : `<p class="g7-side-line">${empty}</p>`;
        return `<div class="g7-side-dev${(d.paired && !up) || (d.bots.length && !d.online) ? " is-off" : ""}"><button class="g7-side-head" type="button" data-side="fold" data-id="${esc(d.id)}" aria-expanded="${closed ? "false" : "true"}">
          <span class="g7-side-name">${svg("laptop", 13)}<span>${esc(d.label)}</span></span>${status(d)}</button><div class="g7-side-bots"${closed ? " hidden" : ""}>${rows}</div></div>`;
      }).join("");
    // 重画会换掉按钮，键盘焦点留在原来那一个（重试、组头或 Bot 行）。
    const back = key && nav.querySelector(key);
    if (back) back.focus();
  }
  // 远端对话打开或关闭后，侧栏的选中行要跟着变。
  D.repaint = () => { sideSig = ""; paintSidebar(); };
  // 主界面开机时（app.js 拿到 /api/session 之后）先跑一次，别等那个 30 秒的定时器。
  D.boot = () => refresh();
  document.addEventListener("click", async (ev) => {
    const t = ev.target.closest && ev.target.closest("[data-side]");
    if (!t) return;
    const side = t.dataset.side;
    if (side === "open") { if (D.chat) D.chat.open(t.dataset.id, t.dataset.bot); return; }
    if (side === "fold") {
      const f = sideFolds(); f[t.dataset.id] = !f[t.dataset.id];
      try { localStorage.setItem("xbot-device-folders", JSON.stringify(f)); } catch (e) {}
    } else {
      try { S.data = await api("GET", "/api/devices?retry=1"); } catch (e) {}
    }
    paintSidebar();
  });
  // 远端对话（remotechat.js，在其后加载）不是三个状态槽之一，单独挂，别被下面那两行清掉。
  Object.defineProperty(D, "chat", { get: () => window.xbotRemoteChat || null, set: () => false });
  setInterval(() => { if (!document.hidden) refresh(); }, 30000);

  Object.assign(D, { S, api, svg, esc, host, left, fmtSeen, modal, render, refresh, open: openDevices, mount, unmount, closeModal, sideFaces: face, sideDevice, blockedReason, setSession, offerClaim });
  D.views = {}; D.acts = {}; D.onInput = null; D.onKey = null; // pairing.js 接着往这三个上面挂
  refresh();
})();
