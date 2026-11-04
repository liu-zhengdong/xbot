/* Direct children of the selected Bot. Public records stay separate from chat. */
(function () {
  const ACTIVE = ["running", "awaiting", "blocked", "stopping", "queued"];
  const labels = {running:"进行中", awaiting:"等待中", blocked:"等待中", stopping:"正在停止", queued:"已排队", done:"已完成", idle:"空闲", failed:"失败", cancelled:"已取消", canceled:"已取消", interrupted:"已中断"};
  const isChild = task => String(task.id || "").startsWith("sa_");
  const statusText = task => (labels[task.status] || task.status || "") + (task.status === "queued" && task.position ? " · 第 " + task.position + " 位" : "");
  const storage = {
    read(key) { try { return sessionStorage.getItem(key) || ""; } catch (_) { return ""; } },
    write(key, value) { try { if (value) sessionStorage.setItem(key, value); else sessionStorage.removeItem(key); } catch (_) {} },
  };

  // The child API embeds provider.Message, whereas the main trace consumes a
  // flat public message DTO. Normalize once at this boundary, preserving seq.
  function messagesOf(entries) {
    return entries.map(entry => {
      const message = entry.message || {}, parts = Array.isArray(message.content) ? message.content : [];
      return {...entry, ...message, seq:entry.seq, time:entry.time,
        content:typeof message.content === "string" ? message.content : parts.filter(p => p.type !== "tool_call" && p.type !== "reasoning").map(p => p.text || "").filter(Boolean).join("\n"),
        tool_calls:parts.filter(p => p.type === "tool_call" && p.tool_call).map(p => p.tool_call).concat(message.tool_calls || []),
        tool_call_id:parts.find(p => p.type === "tool_result")?.tool_call_id || message.tool_call_id,
        parts, publicEntry:entry};
    });
  }

  function create(options) {
    let bot = options.bot;
    const {api, el, icon, appendMarkdown, taskTitle, taskTypeLabel, taskProgress, taskAvatar, taskValue, taskSelected, updateTaskNotable, sameValue, backParent, current, visibleError} = options;
    const root = el("div", "subagents-view"), overview = el("div", "sa-overview"), childHost = el("div", "sa-child-host");
    const rows = new Map(), groups = new Map();
    let tasks = [], child = null, disposed = false;
    const adapter = options.traceAdapter;
    root.append(overview, childHost);
    const valid = c => !disposed && current() && child === c;
    const route = id => "/api/bots/" + encodeURIComponent(bot) + "/agents/" + encodeURIComponent(id);
    const draftKey = id => "xbot-child-draft:" + bot + ":" + id;
    const chosenKey = () => "xbot-child-selected:" + bot;
    function rename(previous, next) {
      if (disposed || bot !== previous || !next || next === previous) return;
      try {
        const prefix = "xbot-child-draft:" + previous + ":", keys = [];
        for (let i = 0; i < sessionStorage.length; i++) { const key = sessionStorage.key(i); if (key.startsWith(prefix)) keys.push(key); }
        for (const key of keys) { sessionStorage.setItem("xbot-child-draft:" + next + ":" + key.slice(prefix.length), sessionStorage.getItem(key)); sessionStorage.removeItem(key); }
        const chosen = storage.read(chosenKey());
        if (chosen) { storage.write("xbot-child-selected:" + next, chosen); storage.write(chosenKey(), ""); }
      } catch (_) {}
      bot = next;
      if (child) {
        child.recordSource.bot = next;
        write(child.nodes.botName, next);
        storage.write(draftKey(child.id), child.nodes.field.value);
      }
    }
    const button = (label, className = "text-btn", glyph) => {
      const b = el("button", className); b.type = "button";
      if (glyph) b.append(icon(glyph));
      b.append(document.createTextNode(label)); return b;
    };
    const write = (node, text) => taskValue(node, text);
    function error(c, text, retry, kind = "action") {
      write(c.nodes.errorText, text); c.nodes.error.hidden = !text; c.retry = retry; c.errorKind = text ? kind : "";
    }
    function syncHeader(c) {
      const t = c.task, n = c.nodes;
      write(n.title, taskTitle(t)); write(n.type, taskTypeLabel(t));
      n.type.className = "ty type-" + ({dev:"dev",research:"res",general:"gen"}[t.type] || "own");
      write(n.status, statusText(t)); n.status.classList.toggle("failed", t.status === "failed");
      write(n.step, [taskProgress(t), t.steps ? "第 " + t.steps + " 步" : ""].filter(Boolean).join(" · "));
      n.step.hidden = !n.step.textContent;
      n.stop.hidden = !ACTIVE.includes(t.status);
      n.stop.disabled = c.stopping || c.sending || t.status === "stopping";
      write(n.stopLabel, c.stopping ? "正在请求…" : t.status === "queued" ? "取消排队" : t.status === "stopping" ? "正在停止" : "停止当前任务");
      n.stop.setAttribute("aria-label", n.stopLabel.textContent);
      n.form.hidden = t.continuable !== true;
      n.readonly.hidden = t.continuable === true;
      n.delivery.hidden = !ACTIVE.includes(t.status);
      n.target.hidden = ACTIVE.includes(t.status);
      n.field.placeholder = ACTIVE.includes(t.status) ? "给这个子代理补充要求…" : "继续和这个子代理说…";
      n.send.disabled = c.sending || c.stopping || !n.field.value.trim();
      write(n.send, c.sending ? "发送中…" : "发送");
      write(n.result, t.result_path || ""); n.resultRow.hidden = !t.result_path;
      write(n.identityState, statusText(t));
      updateTaskNotable(n.view, ACTIVE.includes(t.status) ? [] : (t.notable || []));
    }
    function update(data) {
      tasks = (data.agents || []).filter(isChild);
      const keep = new Set();
      for (const [kind, label, test] of [["active","进行中",t => ACTIVE.includes(t.status) && t.status !== "queued"],["queued","排队中",t => t.status === "queued"],["recent","最近结束",t => !ACTIVE.includes(t.status)]]) {
        let group = groups.get(kind);
        if (!group) { group = el("section", "task-section"); group.append(el("h4", "", label), el("div", "task-items")); groups.set(kind, group); overview.append(group); }
        const items = tasks.filter(test), list = group.lastChild;
        group.hidden = !items.length;
        let cursor = list.firstChild;
        for (const t of items) {
          keep.add(t.id);
          let row = rows.get(t.id);
          if (!row) {
            row = el("div", "sa-task"); row.dataset.agent = t.id;
            const head = el("div", "sa-task-head"), text = el("div", "sa-task-copy"), title = el("b"), type = el("span", "ty"), status = el("span", "sa-task-status");
            text.append(title, type); head.append(taskAvatar(t), text, status);
            const step = el("p", "sa-task-step"), enter = button("查看对话", "secondary sa-enter");
            enter.onclick = () => open(t.id); row.append(head, step, enter);
            row.nodes = {title, type, status, step, enter}; rows.set(t.id, row);
          }
          const n = row.nodes, compact = !ACTIVE.includes(t.status);
          row.classList.toggle("sa-recent", compact); row.classList.toggle("failed", t.status === "failed");
          row.tabIndex = compact ? 0 : -1; row.setAttribute("role", compact ? "button" : "group");
          row.onclick = e => { if (compact && !e.target.closest("button")) open(t.id); };
          row.onkeydown = e => { if (compact && (e.key === "Enter" || e.key === " ") && e.target === row) { e.preventDefault(); open(t.id); } };
          write(n.title, taskTitle(t)); write(n.type, taskTypeLabel(t)); n.type.className = "ty type-" + ({dev:"dev",research:"res",general:"gen"}[t.type] || "own");
          write(n.status, statusText(t)); write(n.step, [taskProgress(t), t.steps ? "第 " + t.steps + " 步" : ""].filter(Boolean).join(" · ")); n.step.hidden = !n.step.textContent || compact;
          n.enter.hidden = compact;
          if (row !== cursor) list.insertBefore(row, cursor); cursor = row.nextSibling;
        }
      }
      for (const [id, row] of rows) if (!keep.has(id)) { row.remove(); rows.delete(id); }
      let empty = overview.querySelector(".sa-empty");
      if (!tasks.length && !empty) { empty = el("div", "sa-empty"); const back = button("回主 Bot"); back.onclick = backParent; empty.append(el("p", "", "还没有子代理"), back); overview.append(empty); }
      if (tasks.length) empty?.remove();
      if (child) {
        const t = tasks.find(t => t.id === child.id);
        // A transcript response / command receipt is newer than the profile
        // request that may already be in flight. The reader owns live status.
        if (t) { const live = child.authoritative; child.task = {...t, ...live}; syncHeader(child); }
      } else if (!root.restored) {
        root.restored = true; const saved = storage.read(chosenKey());
        if (tasks.some(t => t.id === saved)) open(saved);
      }
    }
    function closeChild() {
      if (child) { storage.write(draftKey(child.id), child.nodes.field.value); child.generation++; }
      child = null; childHost.replaceChildren(); overview.hidden = false; storage.write(chosenKey(), "");
      root.classList.remove("child-open"); rows.values().next().value?.querySelector("button")?.focus({preventScroll:true});
    }
    function open(id) {
      if (child?.id === id) return;
      const task = tasks.find(t => t.id === id); if (!task) return;
      if (child) storage.write(draftKey(child.id), child.nodes.field.value);
      const c = {id, task:{...task}, entries:new Map(), rowNodes:new Map(), events:[], generation:0, query:"", selected:null, detailTab:"summary", readPending:false, sending:false, stopping:false};
      child = c; storage.write(chosenKey(), id); overview.hidden = true; root.classList.add("child-open");
      const view = el("div", "sa-child"), head = el("header", "sa-child-head"), breadcrumb = el("div", "sa-breadcrumb");
      const back = button("子代理", "text-btn"), parent = button("回主 Bot", "text-btn"); back.onclick = closeChild; parent.onclick = backParent; breadcrumb.append(back, parent);
      const titleRow = el("div", "sa-title-row"), titleBox = el("div", "sa-title-copy"), title = el("h2"), meta = el("p"), type = el("span", "ty"), status = el("span"), step = el("p", "sa-child-step");
      meta.append(type, status); titleBox.append(title, meta);
      const stop = button("", "secondary sa-stop"), stopLabel = el("span"); stop.append(stopLabel); stop.onclick = () => stopChild(c);
      titleRow.append(taskAvatar(task), titleBox, stop); head.append(breadcrumb, titleRow, step, el("div", "task-notable"));
      const main = el("div", "sa-record-workspace"), column = el("div", "sa-main"), toolbar = el("div", "sa-record-toolbar"), searchBox = el("label", "sa-search"), search = el("input");
      search.type = "search"; search.placeholder = "搜索已加载记录"; search.setAttribute("aria-label", "搜索已加载的子代理记录"); searchBox.append(icon("search"), search);
      search.oninput = () => { if (!c.composing) { c.query = search.value; syncRecords(c); } };
      search.oncompositionstart = () => { c.composing = true; }; search.oncompositionend = () => { c.composing = false; c.query = search.value; syncRecords(c); };
      const identity = el("details", "sa-identity"), summary = el("summary"); summary.setAttribute("aria-label", "任务信息"); summary.append(icon("ellipsis"));
      const facts = el("div", "sa-identity-pop"), resultRow = el("div"), result = el("code"), identityState = el("span"); resultRow.append(el("span", "", "结果文件"), result);
      const botName = el("b", "", bot);
      facts.append(el("span", "", "任务编号"), el("code", "", id), el("span", "", "所属 Bot"), botName, el("span", "", "状态"), identityState, resultRow, el("div", "task-notable-details")); identity.append(summary, facts);
      toolbar.append(el("span", "sa-caption", "对话与工具"), searchBox, identity);
      const records = el("div", "sa-records"), inner = el("div", "sa-records-inner"), older = button("更早的记录", "text-btn sa-older"), list = el("div", "sa-record-list"), empty = el("p", "sa-no-records", "正在读取…"), range = el("p", "sa-range");
      older.hidden = true; older.onclick = () => load(c, "before"); inner.append(older, list, empty, range); records.append(inner);
      const errorBox = el("div", "sa-error"), errorText = el("span"), retry = button("重试"); errorBox.hidden = true; errorBox.setAttribute("role", "status"); retry.onclick = () => c.retry?.(); errorBox.append(errorText, retry);
      const dock = el("div", "sa-dock"), pending = el("div", "sa-pending"), form = el("form", "sa-composer"), field = el("textarea");
      field.setAttribute("aria-label", "给当前子代理发消息"); field.value = storage.read(draftKey(id)); field.rows = 2; field.maxLength = 16384;
      field.oninput = () => { storage.write(draftKey(id), field.value); syncHeader(c); };
      field.onkeydown = e => { if (e.key === "Enter" && !e.shiftKey && !e.isComposing) { e.preventDefault(); send(c); } };
      const bottom = el("div", "sa-composer-bottom"), delivery = el("select", "sa-delivery"), target = el("span", "sa-caption", "发给这个子代理"); delivery.setAttribute("aria-label", "发送方式");
      for (const [value, text] of [["steer","补充当前任务"],["queue","本轮结束后发送"]]) { const item = el("option", "", text); item.value = value; delivery.append(item); }
      const sendButton = el("button", "primary", "发送"); sendButton.type = "submit"; sendButton.setAttribute("aria-label", "发送给这个子代理"); bottom.append(delivery, target, sendButton); form.append(field, bottom); form.onsubmit = e => { e.preventDefault(); send(c); };
      const readonly = el("p", "hint", "这是旧的只读记录，无法继续发消息。"); dock.append(pending, form, readonly);
      const detail = el("aside", "sa-detail trace-detail"); detail.hidden = true; detail.setAttribute("aria-label", "记录详情");
      column.append(toolbar, errorBox, records, dock); main.append(column, detail); view.append(head, main); childHost.replaceChildren(view);
      c.nodes = {view, head, title, type, status, step, stop, stopLabel, botName, main, search, records, older, list, empty, range, form, field, delivery, target, send:sendButton, pending, readonly, detail, error:errorBox, errorText, resultRow, result, identityState};
      c.traceUI = {selected:null, tab:"summary"}; c.traceThink = {}; c.traceNodes = {detail}; c.closeTraceDetail = () => selectRecord(c, null); c.recordSource = {bot,id};
      syncHeader(c); load(c);
    }
    async function load(c, direction = "refresh") {
      if (!valid(c) || c.readPending) return;
      c.readPending = true; c.nodes.older.disabled = true;
      const generation = c.generation, requestBot = bot;
      const before = direction === "before" ? c.gapBefore || c.nextBefore : null;
      const previousHead = Math.min(...c.entries.keys());
      try {
        const page = await api(route(c.id) + "/transcript?limit=40" + (before ? "&before=" + before : ""));
        if (!valid(c) || generation !== c.generation || requestBot !== bot) return;
        let changed = false;
        for (const entry of page.entries || []) {
          const previous = c.entries.get(entry.seq);
          // A truncated tail preview can become a complete public entry when
          // fetched in a smaller window. Equal records keep their DOM nodes.
          if (!previous || !sameValue(previous, entry)) { c.entries.set(entry.seq, entry); changed = true; }
        }
        const sorted = [...c.entries.values()].sort((a,b) => a.seq - b.seq);
        const first = page.entries?.[0]?.seq;
        if (!c.loaded || first <= previousHead) { c.hasMore = page.has_more; c.nextBefore = page.next_before; }
        c.gapBefore = sorted.find((entry, index) => index && entry.seq > sorted[index - 1].seq + 1)?.seq;
        c.loaded = true;
        c.authoritative = {status:page.status, continuable:page.continuable, turn_seq:page.turn_seq};
        c.task = {...c.task, ...c.authoritative}; syncHeader(c);
        if (c.errorKind === "read") error(c, "", null);
        const scroll = c.nodes.records, height = scroll.scrollHeight, top = scroll.scrollTop, follow = height - top - scroll.clientHeight < 80;
        const anchor = [...c.nodes.list.children].find(row => !row.hidden && row.getBoundingClientRect().bottom > scroll.getBoundingClientRect().top);
        const anchorTop = anchor?.getBoundingClientRect().top;
        if (changed) {
          c.messages = messagesOf(sorted);
          const selected = c.events.find(e => e.key === c.selected);
          c.events = adapter.project(c.messages).filter(e => e.type !== "model" || e.text || e.thinking).map(e => ({...e, key:c.id + ":" + e.key}));
          if (selected?.orphan) {
            const call = c.events.find(e => e.call?.id === selected.message.tool_call_id);
            if (call) c.selected = c.traceUI.selected = call.key;
          }
          syncRecords(c); syncPending(c);
        } else syncRecordsMeta(c);
        if (direction === "before") scroll.scrollTop = anchor ? top + anchor.getBoundingClientRect().top - anchorTop : top + scroll.scrollHeight - height;
        else if (follow && !taskSelected(scroll) && changed) scroll.scrollTop = scroll.scrollHeight;
      } catch (e) {
        if (valid(c) && generation === c.generation && requestBot === bot && c.errorKind !== "action") error(c, "记录读取失败：" + visibleError(e), () => load(c, direction), "read");
      } finally {
        c.readPending = false;
        if (valid(c)) { c.nodes.older.disabled = false; syncRecordsMeta(c); }
      }
    }
    function syncRecordsMeta(c) {
      const n = c.nodes;
      n.older.hidden = !c.hasMore && !c.gapBefore; n.older.disabled = c.readPending;
      write(n.older, c.gapBefore ? "补齐中间记录" : "更早的记录");
      const entries = [...c.entries.keys()].sort((a,b) => a-b);
      write(n.range, entries.length ? "已加载 " + entries.length + " 条记录 · 搜索仅限已加载范围" + (c.gapBefore ? " · 存在未加载区段" : "") + ([...c.entries.values()].some(e => e.truncated) ? " · 含已截断的公开预览" : "") : "");
      n.empty.hidden = c.events.some(e => matches(c, e));
      write(n.empty, !c.loaded ? "正在读取…" : c.events.length ? "没有匹配的记录" : "还没有记录");
    }
    function matches(c, event) {
      const q = c.query.trim().toLocaleLowerCase();
      return !q || [event.title, event.text, event.output, event.name, JSON.stringify(event.args || {})].join("\n").toLocaleLowerCase().includes(q);
    }
    function selectRecord(c, key) {
      if (!valid(c)) return;
      c.traceUI.selected = key; c.selected = key; syncRecords(c);
      if (key && matchMedia("(max-width: 767px)").matches) c.nodes.detail.querySelector('[aria-label="关闭详情"]')?.focus({preventScroll:true});
      if (!key) c.rowNodes.get(c.lastSelected)?.focus({preventScroll:true}); else c.lastSelected = key;
    }
    function syncRecords(c) {
      let cursor = c.nodes.list.firstChild;
      for (const event of c.events) {
        let row = c.rowNodes.get(event.key);
        if (!row) {
          const prose = event.type === "input" || event.type === "model";
          row = el(prose ? "article" : "button", "sa-record sa-record-" + event.type); row.dataset.eventKey = event.key;
          if (prose) { row.tabIndex = 0; row.setAttribute("role", "button"); } else row.type = "button";
          const heading = el("div", "sa-record-heading"), name = el("b"), time = el("time"), content = el("div", prose ? "prose sa-record-text" : "sa-record-summary"), preview = el("span", "sa-truncated", "公开预览已截断");
          const badge = el("span", "sa-record-badge"); badge.dataset.type = event.type;
          badge.append(icon(({input:"message-circle", model:"brain", context:"layers", tool:"terminal"})[event.type]));
          if (!prose) { badge.classList.add("trace-kind-badge"); badge.append(el("span", "", event.type === "context" ? "上下文" : "工具")); }
          heading.append(badge, name, time, icon("chevron")); row.append(heading, content, preview);
          row.nodes = {name, time, content, preview}; row.onclick = e => { if (prose && e.target.closest("a,button")) return; if (!taskSelected(row)) selectRecord(c, event.key); };
          row.onkeydown = e => { if ((e.key === "Enter" || e.key === " ") && e.target === row) { e.preventDefault(); selectRecord(c, event.key); } };
          c.rowNodes.set(event.key, row);
        }
        if (row !== cursor) c.nodes.list.insertBefore(row, cursor); cursor = row.nextSibling;
        row.hidden = !matches(c, event); row.classList.toggle("selected", event.key === c.selected); row.classList.toggle("failed", !!event.error);
        const sender = event.message.sender;
        const title = event.type === "model" ? taskTypeLabel(c.task) + "子代理" : event.type === "input" ? sender === "owner" ? "你" : sender?.startsWith("bot:") ? sender.slice(4) : sender || "主 Bot" : event.type === "tool" && event.title === event.name ? "工具调用" : event.title;
        write(row.nodes.name, title); write(row.nodes.time, event.time ? new Date(event.time).toLocaleTimeString([], {hour:"2-digit",minute:"2-digit"}) : "");
        const text = event.type === "tool" ? (event.orphan ? "调用不在已加载范围" : event.error ? "执行失败" : event.hasResult ? "已返回" : "已加载范围内尚无结果") : event.text || (event.message.status ? labels[event.message.status] || event.message.status : "");
        if (row.renderedText !== text && !taskSelected(row)) { row.renderedText = text; row.nodes.content.replaceChildren(); if (["input","model"].includes(event.type)) appendMarkdown(row.nodes.content, text); else row.nodes.content.textContent = text; }
        row.nodes.preview.hidden = !event.truncated;
      }
      const keys = new Set(c.events.map(e => e.key));
      for (const [key, row] of c.rowNodes) if (!keys.has(key) && !taskSelected(row)) { row.remove(); c.rowNodes.delete(key); }
      syncRecordsMeta(c); syncDetail(c);
    }
    function syncDetail(c) {
      const event = c.events.find(e => e.key === c.selected), n = c.nodes;
      n.main.classList.toggle("with-detail", !!event); n.detail.hidden = !event;
      if (event) adapter.sync(c);
    }
    function syncPending(c) {
      const received = new Map(), settled = new Set();
      for (const entry of [...c.entries.values()].sort((a,b) => a.seq - b.seq)) {
        if (entry.kind === "message_received") received.set(entry.input_seq || entry.seq, entry);
        if (["message_consumed","message_dropped"].includes(entry.kind)) settled.add(entry.input_seq);
      }
      const pending = c.gapBefore ? [] : [...received.entries()].filter(([seq, entry]) => entry.delivery === "queue" && !settled.has(seq) && seq > (c.stoppedThrough || 0));
      const signature = JSON.stringify(pending);
      if (c.nodes.pending.renderKey === signature || taskSelected(c.nodes.pending)) return;
      c.nodes.pending.renderKey = signature; c.nodes.pending.replaceChildren(); c.nodes.pending.hidden = !pending.length;
      if (!pending.length) return;
      c.nodes.pending.append(el("span", "sa-caption", "已加载的待发消息 · " + pending.length + " 条"));
      for (const [seq, entry] of pending) {
        const input = c.entries.get(seq), m = messagesOf([input || entry])[0];
        c.nodes.pending.append(el("p", "", m.content || "已受理的排队消息"));
      }
    }
    async function send(c) {
      if (!valid(c) || c.sending || c.stopping || !c.nodes.field.value.trim()) return;
      const text = c.nodes.field.value, delivery = c.nodes.delivery.value;
      if (new TextEncoder().encode(text.trim()).length > 16384) { error(c, "没有送到：内容超过 16 KiB，请缩短后重试。", () => send(c)); return; }
      c.sending = true; syncHeader(c); error(c, "", null); c.generation++;
      try {
        const receipt = await api(route(c.id) + "/messages", {method:"POST", body:{text, delivery}});
        // Clear only the accepted draft, including its saved copy after a Bot
        // switch. Edits made while sending stay intact.
        if (storage.read(draftKey(c.id)) === text) storage.write(draftKey(c.id), "");
        if (c.nodes.field.value === text) c.nodes.field.value = "";
        if (!valid(c)) return;
        c.authoritative = {...c.authoritative, ...receipt}; c.task = {...c.task, ...receipt};
        write(c.nodes.range, "消息已受理 · " + statusText(receipt));
      } catch (e) {
        if (valid(c)) error(c, "没有送到，原文已保留：" + visibleError(e), () => send(c));
      } finally {
        c.sending = false;
        if (valid(c)) { syncHeader(c); if (!c.nodes.error.hidden) return; load(c); }
      }
    }
    async function stopChild(c) {
      if (!valid(c) || c.stopping || c.sending) return;
      c.stopping = true; c.generation++; syncHeader(c); error(c, "", null);
      try {
        const receipt = await api(route(c.id) + "/stop", {method:"POST", body:{}});
        if (!valid(c)) return;
        c.authoritative = {...c.authoritative, ...receipt}; c.task = {...c.task, ...receipt};
        c.stoppedThrough = Math.max(0, ...c.entries.keys()); syncPending(c);
        // No local terminal transition: running/stopping stays visible until
        // the executor reports exit. The accepted stop drops prior inputs.
      } catch (e) { if (valid(c)) error(c, "停止请求失败：" + visibleError(e), () => stopChild(c)); }
      finally { c.stopping = false; if (valid(c)) { syncHeader(c); if (c.nodes.error.hidden) load(c); } }
    }
    root.addEventListener("keydown", e => { if (e.key === "Escape" && child?.selected) { e.preventDefault(); e.stopPropagation(); selectRecord(child, null); } });
    const selectionChanged = () => { if (child && valid(child) && !taskSelected(child.nodes.detail)) syncDetail(child); };
    document.addEventListener("selectionchange", selectionChanged);
    return {root, update, open, rename, poll:() => child && !child.sending && !child.stopping ? load(child) : Promise.resolve(),
      dispose() { if (child) storage.write(draftKey(child.id), child.nodes.field.value); disposed = true; document.removeEventListener("selectionchange", selectionChanged); }};
  }
  window.XBotSubagents = {create, isChild};
})();
