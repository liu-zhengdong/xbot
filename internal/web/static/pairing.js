/* G7 添加设备嵌层：上半是本机短码，对方输入后这边再确认（request）；下半收起着
   「输入对方的短码」（join → confirm → 等对方允许），是次要入口，不和「添加设备」同权。
   挂在 devices.js 的 window.xbotDevices 上。校验位和 internal/remote 同一套（Luhn mod 30），打错不连网关。 */
(function () {
  "use strict";
  const D = window.xbotDevices;
  if (!D) return;
  const { S, api, svg, esc, host, left, fmtSeen, modal, render, refresh } = D;
  const closeModal = D.closeModal;
  const ALPHA = "23456789ABCDEFGHJKMNPQRSTVWXYZ";

  // ---- 配对码 ----
  function checkChar(data) {
    let sum = 0, factor = 2; const n = ALPHA.length;
    for (let i = data.length - 1; i >= 0; i--) {
      const v = ALPHA.indexOf(data[i]) * factor;
      sum += Math.floor(v / n) + (v % n);
      factor = 3 - factor;
    }
    return ALPHA[(n - (sum % n)) % n];
  }
  const codeOK = (c) => c.length === 12 && checkChar(c.slice(0, 11)) === c[11];
  // 粘贴整段话时按整词（ASCII 字母数字）拼出第一段合法的 12 位，前后的话不会混进来；否则只留字符表里的字符。
  function cleanCode(raw) {
    const up = String(raw || "").toUpperCase();
    const words = up.match(/[A-Z0-9]+/g) || [];
    for (let i = 0; i < words.length; i++) {
      let c = "";
      for (const w of words.slice(i)) {
        if ([...w].some((ch) => !ALPHA.includes(ch)) || c.length + w.length > 12) break;
        c += w;
        if (c.length === 12 && codeOK(c)) return c;
      }
    }
    return [...up].filter((c) => ALPHA.includes(c)).join("").slice(0, 12);
  }
  // 和 serve 的 429/409 文案一致（devices.go 的 pairErrCopy）。
  const rateLimited = "操作太频繁，等一分钟再试", nameFull = "同名设备已满 9 台，先解除或改名旧的，或选择替换旧的";

  let pairGen = 0, cleanup = Promise.resolve(), joinRun = null, pairClosing = false;
  async function startAdd() {
    const gen = ++pairGen;
    S.fieldErr = "";
    S.view = "add"; S.joinOpen = false; S.joinVal = ""; S.joinErr = "";
    S.pairing.b = { phase: "none" };
    S.busy = true; render();
    await cleanup;
    if (gen !== pairGen) return;
    pairClosing = false;
    try { await api("DELETE", "/api/devices/join"); }
    catch (e) { if (gen === pairGen) { S.busy = false; S.fieldErr = e.message; render(); } return; }
    if (gen === pairGen) await newCode();
  }
  let codeGen = 0, codeRun = null;
  async function newCode() {
    const gen = ++codeGen;
    S.busy = true; render();
    try {
      const p = api("POST", "/api/devices/code", {});
      codeRun = p;
      const out = await p;
      if (gen !== codeGen) return; // 取消后才返回：丢掉这个码
      S.pairing = out.pairing; S.code = S.pairing.a.code || ""; S.view = "add"; S.fieldErr = "";
      await refresh();
    } catch (e) {
      if (gen !== codeGen) return;
      S.fieldErr = e.message;
    } finally {
      if (gen === codeGen) { S.busy = false; render(); }
    }
  }
  // 立刻收起；等在途请求结束后再清理，重开时等待清理完成。
  function closePair() {
    const gen = ++pairGen; ++codeGen; pairClosing = true;
    S.code = ""; S.busy = false;
    S.pairing = { a: { phase: "none" }, b: { phase: "none" } };
    closeModal();
    document.querySelector('#settings-devices [data-act="add"]')?.focus();
    cleanup = Promise.allSettled([codeRun, joinRun]).then(() => Promise.allSettled([
      api("DELETE", "/api/devices/code"), api("DELETE", "/api/devices/join"),
    ])).finally(() => { if (gen === pairGen) pairClosing = false; });
    return cleanup;
  }
  // 输入对方的短码（B 侧）：本机先查校验位，再经 join 接口找对方。
  async function submitJoin() {
    const gen = pairGen;
    S.joinErr = "";
    const code = cleanCode(S.joinVal);
    if (code.length !== 12) { S.joinErr = "请输入完整的 12 位配对码。"; render(); return; }
    if (!codeOK(code)) { S.pairing.b = { phase: "typo" }; render(); return; } // 本机判断，不连网关
    S.pairing.b = { phase: "checking" }; render();
    try {
      joinRun = api("POST", "/api/devices/join", { code });
      const out = await joinRun;
      if (gen !== pairGen) return;
      S.pairing = out.pairing;
    } catch (e) {
      if (gen !== pairGen) return;
      S.pairing.b = e.message === rateLimited ? { phase: "rate_limited" } : { phase: "gateway_unreachable", gateway: S.data && S.data.self && S.data.self.gateway };
    }
    syncFromPairing(); render();
  }

  // 服务器状态 → 弹窗：a 侧是本机码被对方输入，b 侧是本机输入了对方的码。
  function syncFromPairing() {
    const a = S.pairing.a || {}, b = S.pairing.b || {};
    if (a.phase === "request" && S.view !== "request") { S.view = "request"; }
    if (S.view === "request" && a.phase === "done") { S.view = "success-a"; S.peer = { name: a.name }; refresh(); }
    if (S.view === "request" && a.phase === "denied") { S.view = null; }
    if (["add", "confirm", "waiting"].includes(S.view)) {
      if (b.phase === "confirm") { S.view = "confirm"; S.peer = { name: b.name, os: b.os, bots: b.bots || [] }; }
      else if (b.phase === "waiting") { S.view = "waiting"; }
      else if (b.phase === "denied" && S.view === "waiting") { S.view = "denied"; }
      else if (b.phase === "done") { S.view = "success-b"; S.peer = Object.assign({}, S.peer, { name: b.name || (S.peer && S.peer.name) }); refresh(); }
      else if (S.view !== "add") { S.view = "add"; S.joinOpen = true; } // 确认或等待中被打回：回到输入处说原因
    }
  }

  let seen = "", pollSkipped = false;
  async function poll() {
    if (pairClosing) return;
    if (document.hidden) { pollSkipped = true; return; }
    const gen = pairGen;
    const active = ["add", "confirm", "waiting", "request"].includes(S.view);
    if (!active && !(S.data && S.data.self && S.data.self.gateway)) return;
    pollSkipped = false;
    try {
      // Compare with the last server state, not S.pairing: a refresh() that
      // already stored the new state must still move the modal on.
      const out = await api("GET", "/api/devices/pairing");
      if (gen !== pairGen || pairClosing) return;
      const now = JSON.stringify(out.pairing);
      if (now === seen) return;
      const local = S.pairing.b || {};
      seen = now; S.pairing = out.pairing;
      if (local.phase === "typo" && (S.pairing.b || {}).phase === "none") S.pairing.b = local; // checked here only
      syncFromPairing(); render();
    } catch (e) { /* 下次再试 */ }
  }
  // 窗口在后台时轮询停摆：一回到前台就马上查一次，「X 想连到这台电脑」不用等下一个间隔。
  document.addEventListener("visibilitychange", () => {
    if (!document.hidden && pollSkipped) poll();
  });

  const allows = (them, themBots, mineBots) => `<ul class="g7-allows">
      <li>${svg("bot")}<span>这里能用 ${esc(them)} 上的 ${themBots} 个 Bot</span></li>
      <li>${svg("swap")}<span>${esc(them)} 也能用这台电脑上的 ${mineBots} 个 Bot</span></li></ul>`;
  const selfBots = () => (S.data && S.data.self && S.data.self.bots ? S.data.self.bots.length : 0);
  const spin = (t) => `<span class="g7-wait">${svg("spin")}<span>${t}</span></span>`;
  function deviceCard(p, extra) {
    const bots = (p.bots || []);
    return `<div class="g7-device"><span class="g7-dev-ico">${svg("laptop", 20)}</span><div class="g7-dev-main"><div class="g7-dev-name">${esc(p.name)}</div>
      <div class="g7-dev-sub">${esc(p.os || "")}${p.os ? " · " : ""}${bots.length} 个 Bot${bots.length ? "：" + bots.map(esc).join("、") : ""}</div></div>${extra || ""}</div>`;
  }
  // 添加设备（shot 04）：说明为主，本机短码在中间；输入对方的码收起在下面，是次要入口。
  // 只绑到同一账号的电脑不会进侧栏（账号设备还没接到管道），所以说明里写清要配对一次。
  function viewPair() {
    const a = S.pairing.a || {}, code = a.code || S.code;
    const exp = a.phase === "expired";
    const groups = code.replace(/-/g, "").match(/.{1,4}/g) || [];
    return modal(`<h2>添加设备</h2>
      <p class="g7-lead">在另一台电脑上打开 XBot，进入「设置 · 设备」输入下方短码，这边再点一次「允许」。配对后，那台电脑上的 Bot 会出现在左侧，可以直接聊。</p>
      <p class="g7-fine">只把那台电脑绑到同一账号，它会出现在「我的设备」里，但还不会出现在这里的侧栏；要聊天，先配对一次。</p>
      <h3 class="g7-pair-label">让对方输入这个短码：</h3>
      ${code ? `<button id="g7-copy-code" class="g7-code${exp ? " is-expired" : ""}" type="button" data-act="copy" aria-label="复制配对码 ${esc(code)}" ${exp ? "disabled" : ""}>${groups.map(g => `<span class="g7-group">${[...g].map(c => `<i>${esc(c)}</i>`).join("")}</span>`).join("")}</button>`
        : `<p class="g7-fine">${S.busy ? "正在生成配对码…" : "未能生成配对码，点右侧图标重试。"}</p>`}
      <div class="g7-code-meta">
        ${code ? `<span class="g7-timer">${svg("clock", 13)}${exp ? "配对码已过期" : `<span data-left="${esc(a.expires)}">${left(a.expires)}</span><span>后失效</span>`}</span>` : ""}
        <span class="g7-copy-status" role="status"></span><span class="spacer"></span>
        <button id="g7-renew" class="ghost" type="button" data-act="renew" title="换一个" aria-label="换一个" ${S.busy ? "disabled" : ""}>${svg("refresh", 16)}</button>
      </div>
      ${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      ${joinSection()}
      <div class="modal-actions"><span class="spacer"></span><button class="primary" type="button" data-act="close-pair">完成</button></div>`, "g7-pair");
  }
  // 次要：对方给了短码，在这台电脑上输入。收起时只有一行文字按钮。
  function joinSection() {
    if (!S.joinOpen) return `<button class="text-btn g7-join-open" type="button" data-act="join-open" aria-expanded="false">对方给了你短码？在这里输入</button>`;
    const b = S.pairing.b || {}, v = S.joinVal || "";
    let ph = b.phase && b.phase !== "none" ? b.phase : "";
    if (ph === "wrong") ph = "wrong_or_used";
    const gw = b.gateway || (S.data && S.data.self && S.data.self.gateway) || "";
    const msg = {
      checking: `<p class="g7-hint">${spin("正在找这台电脑")}</p>`,
      typo: `<p class="error g7-msg">这个配对码有一位不对。核对另一台电脑上显示的码，再输一次。</p>`,
      wrong_or_used: `<p class="error g7-msg">配对码不对或已用过。核对一下，或在另一台电脑上点「换一个」。</p>`,
      expired: `<p class="error g7-msg">这个配对码已经过期，在另一台电脑上点「换一个」。</p>`,
      already: `<p class="g7-hint g7-info">${svg("check", 14)}<span>这台电脑已经和「${esc(b.name || "另一台电脑")}」配对了，不用再配。</span></p>`,
      gateway_unreachable: `<p class="error g7-msg">连不上网关${gw ? " " + esc(host(gw)) : ""}。检查一下网络，或在「设备」里改网关地址。</p>`,
      rate_limited: `<p class="error g7-msg">${rateLimited}</p>`,
      name_full: `<p class="error g7-msg">${nameFull}</p>`,
      offline: `<p class="error g7-msg">另一台电脑没有回应。确认它开着 XBot，再试一次。</p>`,
      denied: `<p class="error g7-msg">对方没有允许。要再试，请对方换一个配对码。</p>`,
    }[ph] || "";
    const bad = ph === "typo" || ph === "expired" || ph === "wrong_or_used";
    const ok = cleanCode(v).length === 12 && ph !== "checking";
    return `<div class="g7-join"><h3 class="g7-pair-label">输入对方的短码</h3>
      <label class="field g7-pair-field${bad ? " is-bad" : ""}"><span class="g7-sr">对方的配对码</span>
        <input id="g7-code" type="text" inputmode="text" autocomplete="one-time-code" spellcheck="false" autocapitalize="characters" maxlength="200"
          placeholder="12 位配对码" value="${esc(v)}" ${ph === "checking" ? "disabled" : ""}></label>
      ${S.joinErr ? `<p class="error g7-msg">${esc(S.joinErr)}</p>` : ""}${msg}
      <div class="g7-join-acts"><span class="spacer"></span><button class="secondary" type="button" data-act="join" ${ok ? "" : "disabled"}>下一步</button></div></div>`;
  }
  function viewConfirm() {
    const p = S.peer || {};
    return modal(`<h2>连接到 ${esc(p.name)}？</h2>
      <p class="g7-lead">配对码对应的是这台电脑。确认是你自己的电脑，再连接。</p>
      ${deviceCard(p)}
      <h3 class="g7-h3">连接后</h3>${allows(p.name, (p.bots || []).length, selfBots())}
      <p class="g7-fine">${esc(p.name)} 那边还要点一次「允许」。不认识这台电脑就点「取消」。</p>
      ${S.joinErr ? `<p class="error g7-field-err">${esc(S.joinErr)}</p>` : ""}
      <div class="modal-actions"><button class="secondary" type="button" data-act="close-pair">取消</button><button class="primary" type="button" data-act="confirm" ${S.busy ? "disabled" : ""}>连接</button></div>`);
  }
  function viewWaiting() {
    const p = S.peer || {}, b = S.pairing.b || {};
    return modal(`<h2>等 ${esc(p.name)} 允许</h2>
      <p class="g7-lead">请到 ${esc(p.name)} 上点「允许」。</p>
      ${deviceCard(p, `<span class="g7-dev-side">${svg("spin", 18)}</span>`)}
      <div class="modal-actions"><span class="g7-timer">${svg("clock", 13)}<span data-left="${esc(b.expires)}" data-minus="15000">${left(b.expires, 15000)}</span><span>后自动取消</span></span>
        <span class="spacer"></span><button class="secondary" type="button" data-act="close-pair">取消</button></div>`);
  }
  function viewDenied() {
    const p = S.peer || {};
    return modal(`<h2>${esc(p.name)} 没有允许</h2>
      <p class="g7-lead">这次没连上，两边什么都没共享。要再试，请在 ${esc(p.name)} 上点「添加设备」，拿一个新的配对码。</p>
      <div class="modal-actions"><span class="spacer"></span><button class="primary" type="button" data-act="close-pair">知道了</button></div>`);
  }
  function viewRequest() {
    const a = S.pairing.a || {};
    const head = `<div class="approval-head g7-req-head"><span class="approval-ico">${svg("laptop", 16)}</span><div class="approval-main">
        <div class="approval-title">${esc(a.name)} 想连到这台电脑</div>
        <div class="approval-detail">${esc(a.os || "")}${a.os ? " · " : ""}刚刚输入了配对码${S.code ? " " + esc(S.code) : ""}</div></div></div>`;
    const timer = `<span class="approval-timer">${svg("clock", 13)}<span><span data-left="${esc(a.expires)}">${left(a.expires)}</span> 后自动拒绝</span></span>`;
    const c = a.collision;
    if (c) {
      const old = (S.data && S.data.devices || []).find((d) => d.label === c.name);
      const status = old && old.online ? "在线" : "离线" + (c.seen && fmtSeen(c.seen) ? " · 最后在线 " + fmtSeen(c.seen) : "");
      return modal(`${head}
        <div class="g7-device g7-old"><span class="g7-dev-ico">${svg("laptop", 20)}</span><div class="g7-dev-main"><div class="g7-dev-name">已经有一台 ${esc(c.name)}</div>
          <div class="g7-dev-sub"><i class="status-dot ${old && old.online ? "ok" : "off-ring"}"></i>${esc(status)} · ${c.bots} 个 Bot</div></div></div>
        <p class="g7-fine">如果是同一台电脑重装了 XBot，就替换旧的：侧栏里的分组和你起的名字都保留。不是你自己在操作，就点「拒绝」。</p>
        <div class="g7-timer-row">${timer}</div>${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
        <div class="modal-actions g7-replace-actions"><button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button><span class="spacer"></span>
          <button class="secondary" type="button" data-act="decide" data-d="add">作为新设备添加</button>
          <button class="primary" type="button" data-act="decide" data-d="replace">替换旧的 ${esc(c.name)}</button></div>`, "g7-request");
    }
    return modal(`${head}
      <h3 class="g7-h3">允许后</h3>${allows(a.name, (a.bots || []).length, selfBots())}
      <p class="g7-fine">不是你自己在另一台电脑上操作，就点「拒绝」，这个配对码会立刻作废。</p>${S.fieldErr ? `<p class="error g7-field-err">${esc(S.fieldErr)}</p>` : ""}
      <div class="modal-actions approval-actions">${timer}<span class="spacer"></span>
        <button class="secondary" type="button" data-act="decide" data-d="deny">拒绝</button>
        <button class="primary" type="button" data-act="decide" data-d="allow">允许</button></div>`, "g7-request");
  }
  function viewSuccess(side) {
    const p = S.peer || {};
    const dev = (S.data && S.data.devices || []).find((d) => d.label === p.name);
    const n = dev ? dev.bots.length : (p.bots || []).length;
    const lead = n ? `${esc(p.name)} 上的 ${n} 个 Bot 会出现在左侧列表里。` : `${esc(p.name)} 上的 Bot 会出现在左侧列表里。`;
    return modal(`<div class="g7-done"><span class="g7-done-ico">${svg("check", 24)}</span><h2>已和 ${esc(p.name)} 配对</h2><p class="g7-lead">${lead}</p></div>
      <div class="modal-actions g7-center"><button class="secondary" type="button" data-act="to-drawer">查看设备</button><button class="primary" type="button" data-act="${side === "b" ? "close-pair" : "close"}">完成</button></div>`);
  }
  Object.assign(D.views, { add: viewPair, confirm: viewConfirm, waiting: viewWaiting, denied: viewDenied,
    request: viewRequest, "success-a": () => viewSuccess("a"), "success-b": () => viewSuccess("b") });

  async function act(name, el) {
    const data = S.data || {};
    switch (name) {
      case "add": await startAdd(); break;
      case "renew": await newCode(); break;
      case "close-pair": await closePair(); break;
      case "join-open":
        S.joinOpen = true; render();
        { const c = document.getElementById("g7-code"); if (c) c.focus(); } break;
      case "join":
        if (S.pairing.b) S.pairing.b = { phase: "none" };
        await submitJoin(); break;
      case "confirm": {
        const gen = pairGen;
        S.joinErr = ""; S.busy = true; render();
        try { joinRun = api("POST", "/api/devices/join/confirm"); const out = await joinRun; if (gen !== pairGen) return; S.pairing = out.pairing; S.view = "waiting"; }
        catch (e) {
          if (gen !== pairGen) return;
          if (e.message === nameFull || e.message === rateLimited) S.joinErr = e.message; // 回来时显示服务端原话
          else { S.pairing.b = { phase: "none" }; S.view = "add"; S.joinOpen = true; }
        }
        S.busy = false; syncFromPairing(); render(); break;
      }
      case "copy": {
        const code = (S.pairing.a && S.pairing.a.code) || S.code;
        try { await navigator.clipboard.writeText(code); } catch (e) { return; }
        el.classList.add("is-done");
        const status = el.parentElement.querySelector(".g7-copy-status");
        if (status) status.textContent = "已复制";
        setTimeout(() => { el.classList.remove("is-done"); if (status) status.textContent = ""; }, 2000);
        break;
      }
      case "decide": {
        const d = el.dataset.d;
        S.fieldErr = "";
        S.peer = { name: S.pairing.a.name, bots: S.pairing.a.bots || [] };
        el.disabled = true;
        try { const out = await api("POST", "/api/devices/request", { decision: d }); S.pairing = out.pairing; }
        catch (e) { el.disabled = false; S.fieldErr = e.message; render(); return; } // 同名满 9 台等：在卡片上说原因
        const phase = (S.pairing.a || {}).phase;
        // 过期窗口里点「允许」，服务器回 200 + denied：没配上，回去看设备列表。
        if (d === "deny" || phase === "denied") { S.view = null; S.code = ""; if (phase === "denied") refresh(); }
        else if (phase === "done") { S.view = "success-a"; S.code = ""; await refresh(); }
        else syncFromPairing();
        render(); break;
      }
    }
  }
  for (const name of ["add", "renew", "close-pair", "join-open", "join", "confirm", "copy", "decide"]) {
    D.acts[name] = (el) => act(name, el);
  }
  D.closePair = closePair;
  D.onInput = (t) => {
    if (t.id !== "g7-code") return;
    S.joinVal = t.value;
    const b = S.pairing.b || {};
    if (b.phase && b.phase !== "none" && b.phase !== "checking") S.pairing.b = { phase: "none" };
    S.joinErr = "";
    render();
  };
  D.onKey = (ev) => {
    if (ev.key === "Enter" && !ev.isComposing && ev.keyCode !== 229 && ev.target.id === "g7-code" && S.view === "add") {
      const btn = document.querySelector('#devices-modal [data-act="join"]');
      if (btn && !btn.disabled) { ev.preventDefault(); act("join", btn); }
      return true;
    }
    // Escape 等同当前弹窗上可见的取消/关闭按钮。
    if (ev.key === "Escape" && Object.prototype.hasOwnProperty.call(D.views, S.view)) {
      ev.preventDefault();
      if (S.view === "request") act("decide", { dataset: { d: "deny" }, disabled: false });
      else if (["add", "confirm", "waiting", "denied", "success-b"].includes(S.view)) act("close-pair");
      else closeModal(); // success-a: nothing to cancel
      return true;
    }
    return false;
  };
  setInterval(poll, 1500);
})();
