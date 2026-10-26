// Account screens: the way in on desktop and phone. Sign in, sign up, check
// mail (magic link, code as the fallback), my devices, account/gateway.
// Wide windows sign straight into the main interface; phones land on 我的设备.
(() => {
  const ICON = {
    github: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 .5C5.73.5.5 5.78.5 12.1c0 5.13 3.29 9.47 7.86 11.01.58.11.79-.25.79-.56 0-.28-.01-1.02-.02-2-3.2.7-3.88-1.56-3.88-1.56-.53-1.36-1.3-1.72-1.3-1.72-1.06-.74.08-.72.08-.72 1.17.08 1.79 1.22 1.79 1.22 1.04 1.81 2.73 1.29 3.4.99.1-.77.41-1.29.74-1.59-2.55-.3-5.23-1.3-5.23-5.78 0-1.28.45-2.32 1.2-3.14-.12-.3-.52-1.52.11-3.16 0 0 .98-.32 3.2 1.2a10.9 10.9 0 0 1 2.92-.4c.99 0 1.99.14 2.92.4 2.22-1.52 3.2-1.2 3.2-1.2.63 1.64.23 2.86.11 3.16.75.82 1.2 1.86 1.2 3.14 0 4.49-2.69 5.48-5.25 5.77.42.37.79 1.1.79 2.22 0 1.6-.01 2.89-.01 3.28 0 .31.21.68.8.56A11.6 11.6 0 0 0 23.5 12.1C23.5 5.78 18.27.5 12 .5Z"/></svg>',
    google: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M22.56 12.25c0-.78-.07-1.53-.2-2.25H12v4.26h5.92c-.26 1.37-1.04 2.53-2.21 3.31v2.77h3.57c2.08-1.92 3.28-4.74 3.28-8.09z"/><path d="M12 23c2.97 0 5.46-.98 7.28-2.66l-3.57-2.77c-.98.66-2.23 1.06-3.71 1.06-2.86 0-5.29-1.93-6.16-4.53H2.18v2.84C3.99 20.53 7.7 23 12 23z"/><path d="M5.84 14.09c-.22-.66-.35-1.36-.35-2.09s.13-1.43.35-2.09V7.07H2.18C1.43 8.55 1 10.22 1 12s.43 3.45 1.18 4.93l2.85-2.22.81-.62z"/><path d="M12 5.38c1.62 0 3.06.56 4.21 1.64l3.15-3.15C17.45 2.09 14.97 1 12 1 7.7 1 3.99 3.47 2.18 7.07l3.66 2.84c.87-2.6 3.3-4.53 6.16-4.53z"/></svg>',
    mail: '<svg class="ac-ic" width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect width="20" height="16" x="2" y="4" rx="2"/><path d="m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7"/></svg>',
    out: '<svg class="ac-ic" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" x2="9" y1="12" y2="12"/></svg>',
    back: '<svg class="ac-ic" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="m15 18-6-6 6-6"/></svg>',
    screen: (n) => `<svg class="ac-ic" width="${n}" height="${n}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/></svg>`,
    chev: '<svg class="ac-ic ac-chev" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m9 18 6-6-6-6"/></svg>',
  };
  const LEAD = {
    phone: "登录后，就能在手机上使用自己电脑上的 Bot。",
    desk: "登录后即可使用本机上的 Bot；同一账号也能从手机连过来。",
  };
  const ERR = {
    credentials: "邮箱或密码不对",
    bad_input: "邮箱格式不对，或密码少于 8 位",
    busy: "试得太多了，过一会儿再试",
    expired: "链接或验证码已失效，再发一次邮件",
    unreachable: "连不上账号服务，稍后再试",
  };
  const wide = () => window.matchMedia("(min-width:641px)").matches;
  const el = (tag, cls, html) => { const n = document.createElement(tag); if (cls) n.className = cls; if (html != null) n.innerHTML = html; return n; };

  let root = null, hooks = {}, me = null, pending = null, view = "", pollTimer = 0, polling = false, back = "devices";

  async function call(method, path, body) {
    const opt = { method, credentials: "same-origin", headers: {} };
    if (body) { opt.headers["Content-Type"] = "application/json"; opt.body = JSON.stringify(body); }
    let res;
    try { res = await fetch(path, opt); } catch (e) { return { ok: false, status: 0, data: { error: "unreachable" } }; }
    const data = await res.json().catch(() => ({}));
    return { ok: res.ok, status: res.status, data };
  }
  const say = (data, fallback) => ERR[data && data.error] || fallback;

  function stopPoll() { clearTimeout(pollTimer); pollTimer = 0; }
  function mount(node, cardClass) {
    stopPoll();
    const stage = el("div", "ac-stage"), card = el("div", "ac-card" + (cardClass ? " " + cardClass : ""));
    card.append(node);
    stage.append(card);
    root.replaceChildren(stage);
    root.hidden = false;
  }
  function field(id, label, type, auto, ph) {
    const f = el("div", "ac-field");
    f.append(Object.assign(el("label"), { htmlFor: id, textContent: label }));
    const i = Object.assign(el("input"), { id, type, autocomplete: auto, placeholder: ph || "" });
    if (type === "email") { i.inputMode = "email"; i.autocapitalize = "off"; i.spellcheck = false; }
    f.append(i);
    return [f, i];
  }
  function oauth() {
    const wrap = el("div");
    wrap.append(el("div", "ac-divider", "或"));
    const stack = el("div", "ac-oauth-stack");
    for (const [name, ico] of [["GitHub", ICON.github], ["Google", ICON.google]]) {
      // Shown so people know it is coming; it signs nobody in.
      const b = el("button", "ac-btn ac-oauth", `<span class="ac-bico" aria-hidden="true">${ico}</span>${name}<span class="ac-soon">即将支持</span>`);
      b.type = "button"; b.disabled = true; b.setAttribute("aria-disabled", "true");
      stack.append(b);
    }
    wrap.append(stack);
    return wrap;
  }
  function errLine() { const p = el("p", "ac-err"); p.setAttribute("role", "alert"); p.hidden = true; return p; }
  function showErr(p, text) { p.textContent = text || ""; p.hidden = !text; }
  function foot(text, label, go) {
    const p = el("p", "ac-foot");
    p.append(document.createTextNode(text));
    const b = el("button"); b.type = "button"; b.textContent = label; b.onclick = go;
    p.append(b);
    return p;
  }
  function busy(btn, on) { btn.disabled = on; }

  function showLogin(opts = {}) {
    view = "login";
    const form = el("form", "ac-auth"); form.noValidate = true;
    form.append(el("h1", "", "登录"), Object.assign(el("p", "ac-lead"), { textContent: wide() ? LEAD.desk : LEAD.phone }));
    const [fe, email] = field("acct-login-email", "邮箱", "email", "username", "you@example.com");
    const [fp, pass] = field("acct-login-pass", "密码", "password", "current-password", "密码");
    const err = errLine(), note = el("p", "ac-note");
    note.hidden = !opts.note; note.textContent = opts.note || "";
    const go = el("button", "ac-btn ac-pri", "登录"); go.type = "submit";
    const alt = el("p", "ac-altlink"), link = el("button", "ac-link", "用邮件里的链接登录");
    link.type = "button"; alt.append(link);
    form.append(fe, fp, err, go, alt, note, oauth(), foot("没有账号？", "注册", () => showRegister()));
    if (opts.email) email.value = opts.email;
    form.onsubmit = async (ev) => {
      ev.preventDefault();
      showErr(err, "");
      if (!email.value.trim() || !pass.value) { showErr(err, "先填邮箱和密码"); return; }
      busy(go, true);
      const r = await call("POST", "/api/account/login", { email: email.value.trim(), password: pass.value });
      busy(go, false);
      if (r.ok) { signedIn(r.data.account); return; }
      if (r.status === 403 && r.data.challenge) { showVerify({ challenge: r.data.challenge, email: r.data.email }); return; }
      showErr(err, say(r.data, "没能登录，稍后再试"));
    };
    link.onclick = async () => {
      showErr(err, "");
      if (!email.value.trim()) { showErr(err, "先填邮箱"); email.focus(); return; }
      busy(link, true);
      const r = await call("POST", "/api/account/magic", { email: email.value.trim() });
      busy(link, false);
      if (r.ok) showVerify(r.data);
      else showErr(err, say(r.data, "没能发出邮件，稍后再试"));
    };
    mount(form);
    email.focus({ preventScroll: true });
  }

  function showRegister() {
    view = "register";
    const form = el("form", "ac-auth"); form.noValidate = true;
    form.append(el("h1", "", "创建账号"), el("p", "ac-lead", "用邮箱注册官方网关账号。之后可在手机上找到自己设备上的 Bot。"));
    const [fe, email] = field("acct-reg-email", "邮箱", "email", "email", "you@example.com");
    const [fp, pass] = field("acct-reg-pass", "密码", "password", "new-password", "至少 8 位");
    const [fp2, pass2] = field("acct-reg-pass2", "确认密码", "password", "new-password", "再输入一次");
    const err = errLine();
    const go = el("button", "ac-btn ac-pri", "创建账号"); go.type = "submit";
    form.append(fe, fp, fp2, err, go, oauth(), foot("已有账号？", "登录", () => showLogin()));
    form.onsubmit = async (ev) => {
      ev.preventDefault();
      showErr(err, "");
      if (!email.value.trim()) { showErr(err, "先填邮箱"); return; }
      if (pass.value.length < 8) { showErr(err, "密码至少 8 位"); return; }
      if (pass.value !== pass2.value) { showErr(err, "两次输入的密码不一样"); return; }
      busy(go, true);
      const r = await call("POST", "/api/account/register", { email: email.value.trim(), password: pass.value });
      busy(go, false);
      if (r.ok) { signedIn(r.data.account); return; }
      showErr(err, say(r.data, "没能创建账号，稍后再试"));
    };
    mount(form);
    email.focus({ preventScroll: true });
  }

  function showVerify(p) {
    view = "verify";
    pending = p;
    const box = el("div", "ac-auth ac-verify");
    const lead = el("p", "ac-lead");
    lead.append("我们向 ", Object.assign(el("span", "ac-email"), { textContent: p.email || "" }), " 发了一封带登录链接的邮件。点开链接即可完成。");
    const open = el("button", "ac-btn ac-sec", "打开邮箱应用"); open.type = "button";
    open.onclick = () => { window.location.href = "mailto:"; };
    const again = el("button", "ac-btn ac-ghost", "没收到？再发一次"); again.type = "button";
    const note = el("p", "ac-note"); note.setAttribute("role", "status"); note.hidden = true;
    const actions = el("div", "ac-actions"); actions.append(open, again);
    const fine = el("button", "ac-fine", "也可改用验证码"); fine.type = "button";
    const codebox = el("form", "ac-codebox"); codebox.hidden = true; codebox.noValidate = true;
    const [fc, code] = field("acct-code", "验证码", "text", "one-time-code", "6 位数字");
    code.inputMode = "numeric"; code.maxLength = 6;
    const err = errLine();
    const ok = el("button", "ac-btn ac-sec", "确认"); ok.type = "submit";
    codebox.append(fc, err, ok);
    const back = foot("", "返回登录", () => showLogin());
    box.append(el("div", "ac-mail", ICON.mail), el("h1", "", "查收邮件"), lead, actions, note, fine, codebox, back);
    again.onclick = async () => {
      busy(again, true);
      const r = await call("POST", "/api/account/resend", { challenge: p.challenge });
      busy(again, false);
      note.hidden = false;
      note.textContent = r.ok ? "已重新发送" : say(r.data, "没能发出邮件，稍后再试");
    };
    fine.onclick = () => { fine.hidden = true; codebox.hidden = false; code.focus(); };
    codebox.onsubmit = async (ev) => {
      ev.preventDefault();
      showErr(err, "");
      if (!/^\d{6}$/.test(code.value.trim())) { showErr(err, "验证码是 6 位数字"); return; }
      busy(ok, true);
      const r = await call("POST", "/api/account/code", { challenge: p.challenge, code: code.value.trim() });
      busy(ok, false);
      if (r.ok) signedIn(r.data.account);
      else showErr(err, r.data.error === "credentials" ? "验证码不对" : say(r.data, "没能确认，稍后再试"));
    };
    mount(box);
    poll();
  }

  // Wait for the mail link to be confirmed, possibly on another device.
  function poll() {
    stopPoll();
    pollTimer = setTimeout(async () => {
      if (view !== "verify" || !pending || polling) return;
      polling = true;
      const r = await call("POST", "/api/account/poll", { challenge: pending.challenge });
      polling = false;
      if (view !== "verify") return;
      if (r.ok && r.data.state === "done") { signedIn(r.data.account); return; }
      poll();
    }, document.hidden ? 5000 : 2000);
  }

  function signedIn(account) {
    pending = null;
    me = { account };
    route();
  }

  function route() {
    const a = me && me.account;
    if (!a) { showLogin(); return; }
    // Wide windows are this computer's own window: straight to the main interface.
    if (a.owner && wide()) { leave(); hooks.owner && hooks.owner(""); return; }
    showDevices();
  }

  function leave() { stopPoll(); view = ""; root.hidden = true; root.replaceChildren(); }

  function topbar(title, withBack) {
    const bar = el("header", "ac-topbar");
    if (withBack) {
      const b = el("button", "ac-ib", ICON.back); b.type = "button"; b.title = "返回"; b.setAttribute("aria-label", "返回"); b.style.marginLeft = "-6px";
      b.onclick = () => (back === "app" ? (leave(), hooks.owner && hooks.owner(null)) : showDevices());
      bar.append(b);
    }
    bar.append(el("h1", "", title));
    return bar;
  }

  async function showDevices() {
    view = "devices";
    back = "devices";
    const a = (me && me.account) || {};
    const box = el("div");
    const bar = topbar("我的设备");
    const who = el("button", "ac-acctmail"); who.type = "button"; who.textContent = a.email || ""; who.title = "账号 / 网关";
    who.onclick = () => showGateway();
    const out = el("button", "ac-ib", ICON.out); out.type = "button"; out.title = "退出登录"; out.setAttribute("aria-label", "退出登录");
    out.onclick = () => signOut();
    bar.append(who, out);
    const cnt = el("div", "ac-devcnt");
    box.append(bar, cnt);
    mount(box, "ac-wide");
    // 这台电脑还没绑到任何账号（登录从不静默绑定）：先问一次，
    // POST /api/account/claim 在本机窗口真绑（bindIfFree），拒绝时说原因。
    // 卡片跟着下面的列表一起渲染，列表到了也不会把它刷掉。
    let claim = null;
    if (a.unbound) {
      claim = el("div", "ac-claim");
      const tx = el("div", "ac-claim-tx");
      tx.append(el("b", "", "把这台电脑绑到账号？"), el("p", "", "绑定后，手机和其它入口也能看到这台电脑上的 Bot。"));
      const err = errLine();
      const go = el("button", "ac-btn ac-pri", "绑定这台电脑"); go.type = "button";
      const skip = el("button", "ac-btn ac-ghost", "先不绑"); skip.type = "button";
      const acts = el("div", "ac-actions"); acts.append(skip, go);
      claim.append(tx, err, acts);
      go.onclick = async () => {
        showErr(err, ""); busy(go, true);
        const r = await call("POST", "/api/account/claim");
        busy(go, false);
        if (r.ok && r.data.account && r.data.account.owner) {
          me = { account: r.data.account };
          // 绑上了但账号那边还没登记：如实说，下一次打开会再报。
          if (r.data.reported === false) { claim.replaceChildren(el("p", "", "已绑定这台电脑。设备列表暂时没更新，稍后会自动补上。")); setTimeout(route, 1600); return; }
          route(); return;
        }
        showErr(err, r.ok ? "还没绑上，回这台电脑再试" : say(r.data, "没能绑定，稍后再试"));
      };
      skip.onclick = () => { claim.remove(); claim = null; };
      cnt.append(claim);
    }
    const lead = () => (claim ? [claim] : []);
    const r = await call("GET", "/api/account/devices");
    if (view !== "devices") return;
    if (r.status === 401) { me = null; showLogin({ note: "登录已过期，请重新登录" }); return; }
    const list = (r.ok && r.data.devices) || [];
    if (!list.length) {
      const empty = el("div", "ac-empty");
      empty.append(el("div", "ac-icbox", ICON.screen(24)), el("h2", "", "还没有设备"), el("p", "", "到电脑上打开 XBot，用本账号登录并把那台电脑绑到账号，保持在线后它会出现在这里。"));
      cnt.replaceChildren(...lead(), empty);
      return;
    }
    const cards = list.map((d) => {
      const card = el("div", "ac-dcard");
      const h = el("div", "ac-h");
      h.append(el("span", "ac-dic", ICON.screen(18)), Object.assign(el("b"), { textContent: d.name }));
      const meta = el("div", "ac-meta");
      meta.append(el("i", "ac-dot" + (d.online ? " ac-on" : "")), document.createTextNode(d.online ? "在线" : "离线"), el("span", "ac-sep", "·"), document.createTextNode(d.bots.length + " 个 Bot"));
      card.append(h, meta);
      if (d.bots.length) {
        const bots = el("div", "ac-bots"); bots.setAttribute("role", "list");
        for (const b of d.bots) bots.append(botRow(b, d.self));
        card.append(bots);
      }
      return card;
    });
    cnt.replaceChildren(...lead(), ...cards, el("p", "ac-devfoot", "手机只是入口。重活仍在电脑上的 Bot 里做。"));
  }

  function botRow(b, open) {
    const row = el(open ? "button" : "div", "ac-brow"); row.setAttribute("role", "listitem");
    if (open) { row.type = "button"; row.onclick = () => { leave(); hooks.owner && hooks.owner(b.name); }; }
    const av = el("span", "ac-av"); av.setAttribute("aria-hidden", "true");
    if (b.avatar_url) av.style.backgroundImage = `url("${b.avatar_url}")`;
    else {
      av.textContent = b.initial || [...b.name][0] || "";
      if (b.avatar_bg) av.style.background = b.avatar_bg;
      if (b.avatar_ink) av.style.color = b.avatar_ink;
    }
    const tx = el("span", "ac-tx");
    tx.append(Object.assign(el("b"), { textContent: b.name }));
    const hint = b.hint || (b.unread ? "有新消息" : "");
    if (hint) tx.append(Object.assign(el("span", "ac-hint"), { textContent: hint }));
    row.append(av, tx);
    if (open) row.insertAdjacentHTML("beforeend", ICON.chev);
    return row;
  }

  function showGateway(from) {
    view = "gateway";
    if (from) back = from;
    const a = (me && me.account) || {};
    const box = el("div");
    const cnt = el("div", "ac-devcnt");
    const status = el("div", "ac-statusline");
    status.append(el("i", "ac-dot" + (a.gateway_ok ? " ac-on" : "")), document.createTextNode(a.official ? "当前：经官方网关" : "当前：经自建网关"));
    const rows = el("div", "ac-grows");
    const row = (title, hint, val) => {
      const g = el("div", "ac-grow"), tx = el("div", "ac-tx");
      tx.append(Object.assign(el("b"), { textContent: title }), Object.assign(el("div", "ac-hint"), { textContent: hint }));
      g.append(tx);
      if (val) g.append(Object.assign(el("span", "ac-val"), { textContent: val }));
      return g;
    };
    rows.append(row("账号", a.email || ""), row("网关", (a.official ? "官方默认" : "自建") + " · " + (a.gateway || ""), a.gateway_ok ? "已连接" : "未连接"));
    const out = el("button", "ac-btn ac-sec", "退出登录"); out.type = "button"; out.onclick = () => signOut();
    const wrap = el("div"); wrap.style.marginTop = "18px"; wrap.append(out);
    cnt.append(status, rows, wrap, el("p", "ac-gfoot", "改网关地址将在高级选项中提供。"));
    const bar = topbar("账号 / 网关", true);
    bar.querySelector("h1").style.fontSize = "18px";
    box.append(bar, cnt);
    mount(box, "ac-wide");
  }

  async function signOut() {
    await call("POST", "/api/account/logout");
    me = null;
    pending = null;
    hooks.signedOut && hooks.signedOut();
    showLogin();
  }

  async function boot(h) {
    root = document.getElementById("acct");
    hooks = h || {};
    const r = await call("GET", "/api/account/me");
    me = r.ok ? r.data : { account: null };
    route();
  }

  // Called by the main interface: open a screen over it.
  async function show(name, opts = {}) {
    root = root || document.getElementById("acct");
    if (name === "login") { me = null; showLogin(opts); return; }
    if (!me || !me.account) {
      const r = await call("GET", "/api/account/me");
      me = r.ok ? r.data : { account: null };
    }
    if (!me.account) { showLogin(opts); return; }
    if (name === "gateway") showGateway(opts.from || "devices");
    else showDevices();
  }

  window.XBotAccount = { boot, show, signOut, active: () => !!root && !root.hidden, state: () => (me && me.account) || null };
})();
