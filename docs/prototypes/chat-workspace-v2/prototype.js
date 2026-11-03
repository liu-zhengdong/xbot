'use strict';

(function () {
  const $ = (s, root = document) => root.querySelector(s);
  const $$ = (s, root = document) => [...root.querySelectorAll(s)];

  // Lucide 1.8 stroke icons
  const ICONS = {
    'message-circle': '<path d="M7.9 20A9 9 0 1 0 4 16.1L2 22Z"/>',
    brain: '<path d="M12 18V5a3 3 0 0 0-5.8-1 4 4 0 0 0-3.3 6.2 4 4 0 0 0 .6 7.2A4 4 0 1 0 12 18Zm0 0V5a3 3 0 0 1 5.8-1 4 4 0 0 1 3.3 6.2 4 4 0 0 1-.6 7.2A4 4 0 1 1 12 18Z"/><path d="M8 8c0 2-1 3-3 3m14 0c-2 0-3-1-3-3M7 15c2 0 3 1 3 3m4 0c0-2 1-3 3-3"/>',
    'git-branch': '<line x1="6" x2="6" y1="3" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>',
    'list-checks': '<path d="m3 17 2 2 4-4"/><path d="m3 7 2 2 4-4"/><path d="M13 6h8"/><path d="M13 12h8"/><path d="M13 18h8"/>',
    layers: '<path d="m12.83 2.18a2 2 0 0 0-1.66 0L2.6 6.08a1 1 0 0 0 0 1.83l8.58 3.91a2 2 0 0 0 1.66 0l8.58-3.9a1 1 0 0 0 0-1.83Z"/><path d="m22 17.65-9.17 4.16a2 2 0 0 1-1.66 0L2 17.65"/><path d="m22 12.65-9.17 4.16a2 2 0 0 1-1.66 0L2 12.65"/>',
    inbox: '<polyline points="22 12 16 12 14 15 10 15 8 12 2 12"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/>',
    ellipsis: '<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>',
    pencil: '<path d="M21.174 6.812a1 1 0 0 0-3.986-3.987L3.842 16.174a2 2 0 0 0-.5.83l-1.321 4.352a.5.5 0 0 0 .623.622l4.353-1.32a2 2 0 0 0 .83-.497z"/><path d="m15 5 4 4"/>',
    sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    moon: '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    search: '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
    mic: '<rect width="6" height="12" x="9" y="2" rx="3"/><path d="M5 10v2a7 7 0 0 0 14 0v-2"/><line x1="12" x2="12" y1="19" y2="22"/><line x1="8" x2="16" y1="22" y2="22"/>',
    send: '<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>',
    play: '<polygon points="6 3 20 12 6 21 6 3" fill="currentColor" stroke="none"/>',
    pause: '<rect x="6" y="4" width="4" height="16" rx="1" fill="currentColor" stroke="none"/><rect x="14" y="4" width="4" height="16" rx="1" fill="currentColor" stroke="none"/>',
    copy: '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
    check: '<path d="M20 6 9 17l-5-5"/>',
    reply: '<polyline points="9 17 4 12 9 7"/><path d="M20 18v-2a4 4 0 0 0-4-4H4"/>',
    plus: '<path d="M5 12h14"/><path d="M12 5v14"/>',
    pin: '<path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>',
    panelLeft: '<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>',
    menu: '<line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="18" y2="18"/>',
    chevUpDown: '<path d="m7 15 5 5 5-5"/><path d="m7 9 5-5 5 5"/>',
    chevron: '<path d="m9 18 6-6-6-6"/>',
    x: '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    gauge: '<path d="m12 14 4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>',
    sparkles: '<path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/>'
  };

  function icon(name, cls = '') {
    return `<svg class="icon ${cls}" viewBox="0 0 24 24" aria-hidden="true">${ICONS[name] || ICONS['message-circle']}</svg>`;
  }

  // 生成可爱的小鲸鱼与 Notionists 头像
  const WHALE_SVG = `data:image/svg+xml;charset=utf-8,` + encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 120 120">
      <rect width="120" height="120" fill="#7bc6ea"/>
      <circle cx="60" cy="64" r="36" fill="#253447"/>
      <ellipse cx="60" cy="72" rx="28" ry="24" fill="#f7f9fc"/>
      <path d="M32 46c8-12 48-12 56 0-6 8-16 12-28 12S38 54 32 46z" fill="#3a6ea5"/>
      <circle cx="46" cy="64" r="4" fill="#18222f"/>
      <circle cx="74" cy="64" r="4" fill="#18222f"/>
      <circle cx="47.5" cy="62.5" r="1.4" fill="#fff"/>
      <circle cx="75.5" cy="62.5" r="1.4" fill="#fff"/>
      <path d="M55 71q5 5 10 0" fill="none" stroke="#18222f" stroke-width="2.5" stroke-linecap="round"/>
      <ellipse cx="39" cy="69" rx="4.5" ry="2.5" fill="#f49797" opacity=".75"/>
      <ellipse cx="81" cy="69" rx="4.5" ry="2.5" fill="#f49797" opacity=".75"/>
      <path d="M60 26v-9m-6 4 6 5 6-5" fill="none" stroke="#eff8ff" stroke-width="3" stroke-linecap="round"/>
    </svg>`
  );

  function avatarURL(seed, customWhale = false) {
    if (customWhale) return WHALE_SVG;
    try {
      if (window.XBotAvatar && typeof window.XBotAvatar.portrait === 'function') {
        const svg = window.XBotAvatar.portrait(seed);
        if (svg) return 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
      }
    } catch (e) {}
    return WHALE_SVG;
  }

  // 解析地址参数：支持 shoot.mjs 的 --states 与查询参数
  const params = new URLSearchParams(location.search);
  const rawState = params.get('state') || 'idle';
  const initialTheme = params.get('theme') || (rawState.includes('dark') ? 'dark' : 'light');
  const initialOS = params.get('os') || (rawState === 'win' ? 'win' : 'mac');
  const initialTab = params.get('tab') || (['memory', 'trace', 'agents', 'computer', 'context', 'inbox'].includes(rawState) ? rawState : 'chat');

  document.documentElement.setAttribute('data-theme', initialTheme);

  const TABS = [
    { id: 'chat', label: '聊天', glyph: 'message-circle' },
    { id: 'memory', label: '记忆', glyph: 'brain' },
    { id: 'trace', label: '轨迹', glyph: 'git-branch' },
    { id: 'agents', label: '子代理', glyph: 'git-branch' },
    { id: 'computer', label: '任务', glyph: 'list-checks' },
    { id: 'context', label: '上下文', glyph: 'layers' },
    { id: 'inbox', label: '收件箱', glyph: 'inbox' }
  ];

  const BOTS = [
    {
      name: '小鲸',
      model: 'deepseek-v4.1-flash',
      preview: '真像啊？那我以后时不时给你哼两句。',
      time: '22:18',
      whale: true,
      pinned: false,
      primary: false
    },
    {
      name: '2号小伙子',
      model: 'deepseek-v4.1-flash',
      preview: '这一轮出错，后续要求已转入子代理继续跟进',
      time: '21:40',
      seed: '2号小伙子',
      busy: false
    },
    {
      name: 'Astr',
      model: 'claude-sonnet-4.5',
      preview: '站点图标探针已缓存，重复绘制不再发起请求',
      time: '19:12',
      seed: 'Astr',
      unread: true
    },
    {
      name: 'ada',
      model: 'gpt-5',
      preview: '我之前尝试的网页搜索和结果摘要已归档',
      time: '昨天',
      seed: 'ada'
    }
  ];

  const state = {
    bot: '小鲸',
    tab: initialTab,
    os: initialOS,
    rail: rawState === 'rail',
    mobileNavOpen: rawState === 'mobilenav',
    asideOpen: rawState === 'aside',
    quoting: rawState === 'quoting' ? { seq: 2, text: '真像啊？那我以后时不时给你哼两句。' } : null,
    attached: rawState === 'attach' ? [{ id: 'demo-1', url: WHALE_SVG }] : [],
    recording: rawState === 'recording',
    busyAgent: rawState === 'busy',
    groupChat: rawState === 'group',
    emptyChat: rawState === 'empty',
    sendFailed: rawState === 'failed',
    sendQueued: rawState === 'queued',
    draft: rawState === 'multiline'
      ? '帮我把这段唱腔再收紧一点：\n1. 开头别拖太长，保留 `0:15` 的干脆感\n2. 结尾句压低半度，参考 https://music.example.com/demo'
      : (rawState === 'typing' ? '那你再来一段京剧味儿的念白试试？' : ''),
    hiddenTabs: new Set(),
    playingVoice: false,
    voiceProgress: 4
  };

  function renderApp() {
    const app = $('#app');
    app.innerHTML = `
      <div class="shell os-${state.os} entry-animate" id="shell">
        <aside class="sidebar ${state.rail ? 'rail' : ''} ${state.mobileNavOpen ? 'open' : ''}" id="sidebar" aria-label="Bot 列表">
          <div class="mac-lights" aria-hidden="true">
            <span class="mac-light close"></span>
            <span class="mac-light min"></span>
            <span class="mac-light max"></span>
          </div>
          <div class="side-top">
            <div class="greeting">
              <div>晚上好，刘政东。</div>
              ${state.busyAgent ? `<div class="greeting-sub"><span class="dot-busy"></span>有 1 个 Bot 在忙</div>` : ''}
            </div>
            <button class="side-collapse-btn" id="rail-toggle" type="button" aria-label="收起或展开侧栏" title="切换侧栏宽度 (⌘B)">
              ${icon('panelLeft')}
            </button>
          </div>

          <div class="pin-row">
            <button class="pin-item" type="button" data-pick-bot="XBot">
              <span class="avatar"><img src="${avatarURL('XBot')}" alt=""></span>
              <span class="pin-name"><span>XBot</span>${icon('pin', 'pin-mark-ico')}</span>
            </button>
            <button class="pin-item add" id="new-bot-btn" type="button" aria-label="新建 Bot 或群组">
              <span class="plus-circle">${icon('plus')}</span>
              <span class="pin-name">新建</span>
            </button>
          </div>

          <nav class="side-scroll" id="roster" aria-label="Bot">
            ${BOTS.map(b => `
              <button class="roster-item ${state.bot === b.name ? 'active' : ''}" type="button" data-pick-bot="${b.name}">
                <span class="avatar">
                  <img src="${avatarURL(b.seed || b.name, b.whale)}" alt="">
                  ${b.unread && state.bot !== b.name ? '<i class="unread-dot"></i>' : ''}
                </span>
                <span class="item-body">
                  <span class="item-top">
                    <span class="item-name">${b.name}</span>
                    <span class="item-time">${b.time}</span>
                  </span>
                  <span class="item-preview ${b.busy ? 'busy' : ''}">${b.preview}</span>
                </span>
              </button>
            `).join('')}
          </nav>

          <div class="side-foot">
            <button class="account-btn" id="account-btn" type="button" aria-label="账号与设置">
              <span class="avatar"><img src="${avatarURL('刘政东')}" alt=""></span>
              <span class="me-info">
                <span class="me-name">刘政东</span>
                <span class="me-device">liuzhengdongdeMac-mini</span>
              </span>
              ${icon('chevUpDown', 'me-chev')}
            </button>
          </div>
        </aside>

        <div class="backdrop" id="backdrop" ${state.mobileNavOpen ? '' : 'hidden'}></div>

        <main class="workspace" id="workspace">
          <div class="win-caption" aria-hidden="true">
            <span class="win-btn">—</span>
            <span class="win-btn">□</span>
            <span class="win-btn close">✕</span>
          </div>

          <header class="top">
            <div class="top-inner">
              <button class="icon-btn mobile-menu-btn" id="mobile-menu" type="button" aria-label="打开列表">
                ${icon('menu')}
              </button>
              <button class="identity-chip" id="head-chip" type="button" aria-label="Bot 资料与模型">
                <span class="avatar"><img id="head-avatar" src="${avatarURL('小鲸', state.bot === '小鲸')}" alt=""></span>
                <span class="identity-text">
                  <span class="identity-title" id="head-title">${state.bot}</span>
                  <span class="identity-sub" id="head-sub">deepseek-v4.1-flash</span>
                </span>
              </button>

              <div class="top-actions">
                <button class="icon-btn" id="edit-profile-btn" type="button" aria-label="编辑资料" title="编辑资料">
                  ${icon('pencil')}
                </button>
                <button class="icon-btn" id="theme-toggle" type="button" aria-label="切换深浅色" title="切换深浅色">
                  ${document.documentElement.dataset.theme === 'dark' ? icon('sun') : icon('moon')}
                </button>
                <button class="icon-btn" id="search-toggle" type="button" aria-label="搜索聊天记录" aria-expanded="false" title="搜索聊天记录">
                  ${icon('search')}
                </button>
              </div>
            </div>

            <div class="workspace-navigation">
              <nav class="tabs-track" id="workspace-tabs" role="tablist" aria-label="Bot 页面">
                <div class="tab-pill-indicator" id="tab-indicator"></div>
                ${TABS.filter(t => !state.hiddenTabs.has(t.id)).map(t => `
                  <button class="workspace-tab" role="tab" id="tab-${t.id}" data-tab="${t.id}" aria-selected="${state.tab === t.id}" tabindex="${state.tab === t.id ? 0 : -1}" type="button">
                    ${icon(t.glyph)}
                    <span>${t.label}</span>
                  </button>
                `).join('')}
              </nav>
              <div class="more-wrap">
                <button class="workspace-tab" id="workspace-more" type="button" aria-label="更多" aria-expanded="false">
                  ${icon('ellipsis')}
                  <span>更多</span>
                </button>
                <div class="pop-menu" id="more-menu" hidden></div>
              </div>
            </div>
          </header>

          <section class="chat-pane" id="chat-pane" role="tabpanel" aria-labelledby="tab-chat" ${state.tab === 'chat' ? '' : 'hidden'}>
            <div class="chat-search-pop" id="search-pop" hidden>
              <form class="search-bar" id="search-form">
                ${icon('search')}
                <input id="search-input" type="search" placeholder="搜索此 Bot 的聊天记录…" autocomplete="off">
                <button class="icon-btn" id="search-close" type="button" aria-label="关闭搜索" style="width:24px;height:24px">${icon('x')}</button>
              </form>
            </div>

            <div class="thread" id="thread">
              <div class="thread-inner ${state.groupChat ? 'grpchat' : ''}" id="thread-inner">
                ${state.emptyChat ? `
                <!-- 空 Bot 开场：头像 + 名字 + 一句引导 + 最多 3 个开场建议 -->
                <div class="empty-bot">
                  <span class="e-avatar"><img src="${avatarURL('小鲸', true)}" alt=""></span>
                  <span class="e-name">小鲸</span>
                  <p class="e-line">有什么事，直接说就行。</p>
                  <div class="starter-list">
                    <button class="starter-item" type="button" data-starter="最近有什么想聊的？">${icon('message-circle')}<span>最近有什么想聊的？</span></button>
                    <button class="starter-item" type="button" data-starter="帮我整理一下今天的录音笔记">${icon('message-circle')}<span>帮我整理一下今天的录音笔记</span></button>
                    <button class="starter-item" type="button" data-starter="哼一段轻松的调子听听">${icon('message-circle')}<span>哼一段轻松的调子听听</span></button>
                  </div>
                </div>
                ` : `
                <div class="history-nav">
                  <button class="history-chip" id="load-older" type="button">更早的消息</button>
                </div>

                <div class="tdiv">今天 22:14</div>
                `}

                ${!state.emptyChat && state.groupChat ? `
                <!-- 群聊：小鲸发言（带头像与名字，分清是谁在说） -->
                <div class="msg bot grp" data-seq="g1">
                  <span class="msg-avatar"><img src="${avatarURL('小鲸', true)}" alt=""></span>
                  <div class="grp-body">
                    <div class="who"><span class="who-name">小鲸</span><time>22:14</time></div>
                    <div class="bubble-col">
                      <div class="bot-bubble"><p>我把那段音频的频谱拉出来看了一眼，峰值集中在 800Hz 附近，确实更像在哼。</p></div>
                    </div>
                    <div class="msg-meta-bar">
                      <button class="msg-act" type="button" data-copy="我把那段音频的频谱拉出来看了一眼，峰值集中在 800Hz 附近，确实更像在哼。" aria-label="复制消息" title="复制">${icon('copy')}</button>
                      <span class="msg-time">22:14</span>
                    </div>
                  </div>
                </div>

                <!-- 群聊：小鲸连续发言（cont，不重复头像与名字） -->
                <div class="msg bot grp cont" data-seq="g2">
                  <div class="grp-body">
                    <div class="bubble-col">
                      <div class="bot-bubble"><p>要不要我导出一张波形图发给你看看？</p></div>
                    </div>
                    <div class="msg-meta-bar">
                      <button class="msg-act" type="button" data-copy="要不要我导出一张波形图发给你看看？" aria-label="复制消息" title="复制">${icon('copy')}</button>
                      <span class="msg-time">22:14</span>
                    </div>
                  </div>
                </div>

                <!-- 群聊 action 系统行：纯灰字居中，不画气泡 -->
                <div class="sysline">小鲸 · 调用了 audio_spectrogram 工具</div>

                <!-- 群聊：Astr 发言（另一个 Bot，换头像与名字） -->
                <div class="msg bot grp" data-seq="g3">
                  <span class="msg-avatar"><img src="${avatarURL('Astr')}" alt=""></span>
                  <div class="grp-body">
                    <div class="who"><span class="who-name">Astr</span><time>22:15</time></div>
                    <div class="bubble-col">
                      <div class="bot-bubble"><p>从曲谱角度看，拖长字音的做法更接近念白而不是唱。</p></div>
                    </div>
                    <div class="msg-meta-bar">
                      <button class="msg-act" type="button" data-copy="从曲谱角度看，拖长字音的做法更接近念白而不是唱。" aria-label="复制消息" title="复制">${icon('copy')}</button>
                      <button class="msg-act" type="button" data-reply="从曲谱角度看，拖长字音的做法更接近念白而不是唱。" aria-label="回复" title="回复">${icon('reply')}</button>
                      <span class="msg-time">22:15</span>
                    </div>
                  </div>
                </div>

                <!-- 群聊：Bot 之间往来的来源行（小头像+名字胶囊，可点开看往来详情） -->
                <button class="peer-line" type="button" data-toast="打开小鲸与 Astr 的往来记录">
                  <span class="p-avatar"><img src="${avatarURL('Astr')}" alt=""></span>
                  <span class="p-name">Astr</span>
                  <span class="p-text">回复了小鲸 · 我也同意更像念白</span>
                </button>
                ` : ''}

                ${!state.emptyChat && !state.groupChat ? `
                <!-- 单聊：Bot 语音与文本消息（一对一不重复放头像与名字，左边缘对齐） -->
                <div class="msg bot" data-seq="1" data-text="加了唱腔描述，出来了——你听听像不像在哼。我觉得更像喝多了念课文。">
                  <div class="bubble-col">
                    <div class="bot-bubble">
                      <p>加了唱腔描述，出来了——你听听像不像在哼。我觉得更像喝多了念课文。</p>
                    </div>
                    <div class="bot-bubble voice-bubble">
                      <div class="voice ${state.playingVoice ? 'is-playing' : ''}" id="demo-voice">
                        <button class="v-play" id="voice-play" type="button" aria-label="播放语音">
                          ${icon(state.playingVoice ? 'pause' : 'play')}
                        </button>
                        <div class="v-wave" id="voice-wave" role="button" tabindex="0" aria-label="语音波形">
                          ${[10, 18, 24, 14, 22, 12, 26, 16, 20, 12].map((h, idx) => `
                            <i style="--h:${h}" class="${state.playingVoice && idx < state.voiceProgress ? 'is-on' : ''}"></i>
                          `).join('')}
                        </div>
                        <span class="v-time" id="voice-time">${state.playingVoice ? '0:11' : '0:15'}</span>
                      </div>
                    </div>
                  </div>
                  <div class="msg-meta-bar">
                    <button class="msg-act" type="button" data-copy="加了唱腔描述，出来了——你听听像不像在哼。我觉得更像喝多了念课文。" aria-label="复制消息" title="复制">${icon('copy')}</button>
                    <button class="msg-act" type="button" data-reply="加了唱腔描述，出来了——你听听像不像在哼。" aria-label="回复" title="回复">${icon('reply')}</button>
                    <span class="msg-time">22:14</span>
                  </div>
                </div>

                <!-- 用户回复 -->
                <div class="msg user ${state.sendFailed ? 'failed' : ''} ${state.sendQueued ? 'queued' : ''}" data-seq="2" data-text="哈哈哈哈，可以，还挺像的哈哈">
                  <div class="bubble-col">
                    <div class="bubble">
                      <p>哈哈哈哈，可以，还挺像的哈哈</p>
                    </div>
                  </div>
                  ${state.sendFailed ? `
                  <div class="msg-status err">
                    ${icon('x')}没送到
                    <button class="st-retry" type="button" data-toast="已重新发送">重试</button>
                    <span class="msg-time" style="margin-left:auto">22:16</span>
                  </div>
                  ` : state.sendQueued ? `
                  <div class="msg-status">
                    已排队
                    <span class="msg-time" style="margin-left:auto">22:16</span>
                  </div>
                  ` : `
                  <div class="msg-meta-bar">
                    <span class="msg-time">22:16</span>
                    <button class="msg-act" type="button" data-reply="哈哈哈哈，可以，还挺像的哈哈" aria-label="回复" title="回复">${icon('reply')}</button>
                    <button class="msg-act" type="button" data-copy="哈哈哈哈，可以，还挺像的哈哈" aria-label="复制消息" title="复制">${icon('copy')}</button>
                  </div>
                  `}
                </div>

                <!-- 单聊：Bot 回复 -->
                <div class="msg bot" data-seq="3" data-text="真像啊？那我以后时不时给你哼两句。\n\n不过别当我会唱歌，我只是把字拖长了、音调乱晃——是你耳朵对我太宽容了。">
                  <div class="bubble-col">
                    <div class="bot-bubble">
                      <p>真像啊？那我以后时不时给你哼两句。</p>
                      <p>不过别当我会唱歌，我只是把字拖长了、音调乱晃——是你耳朵对我太宽容了。</p>
                    </div>
                  </div>
                  <div class="msg-meta-bar">
                    <button class="msg-act" type="button" data-copy="真像啊？那我以后时不时给你哼两句。不过别当我会唱歌，我只是把字拖长了、音调乱晃——是你耳朵对我太宽容了。" aria-label="复制消息" title="复制">${icon('copy')}</button>
                    <button class="msg-act" type="button" data-reply="真像啊？那我以后时不时给你哼两句。" aria-label="回复" title="回复">${icon('reply')}</button>
                    <span class="msg-time">22:18</span>
                  </div>
                </div>

                <!-- 心里话折叠行 -->
                <div class="source-event-row">
                  <button class="sysline-pill" id="aside-toggle" type="button" aria-expanded="${state.asideOpen}">
                    <span>查看心里话</span>
                    ${icon('chevron', 'chev')}
                  </button>
                  <div class="aside-drawer" id="aside-drawer" ${state.asideOpen ? '' : 'hidden'}>
                    其实刚才调 TTS 韵律参数试了三次，第一次像机器人报菜名，第二次尾音飘了，第三次才勉强有那么点哼小调的意思。他居然没嫌弃。
                  </div>
                </div>
                ` : ''}
              </div>
            </div>

            <!-- 底部 Dock：收小为 44px 单行胶囊的 Composer -->
            <div class="dock">
              <div class="work-status" id="work-status" ${state.busyAgent ? '' : 'hidden'}>
                <button class="wrow" id="busy-pill" type="button">
                  <span class="dot-live"></span>
                  <b>整理音频韵律备忘</b>
                  <span>· 写入工作区笔记 · 第 3 步 · 14 秒</span>
                  ${icon('chevron')}
                </button>
              </div>

              <form class="composer ${state.quoting ? 'quoting' : ''} ${state.attached.length ? 'has-attach' : ''} ${state.recording ? 'recording' : ''}" id="composer">
                <div class="quote-row" id="quote-bar" ${state.quoting ? '' : 'hidden'}>
                  ${icon('message-circle')}
                  <p class="quote-row-text" id="quote-text">${state.quoting ? '“' + state.quoting.text : ''}</p>
                  <button class="quote-clear" id="quote-clear" type="button" aria-label="取消引用">
                    ${icon('x')}
                  </button>
                </div>

                <div class="attach-row" id="attach-bar" ${state.attached.length ? '' : 'hidden'}>
                  <div class="attach-list" id="attach-list">
                    ${state.attached.map((a, i) => `
                      <div class="attach-thumb">
                        <img src="${a.url}" alt="">
                        <button class="attach-drop" type="button" data-remove-attach="${i}" aria-label="移除图片">${icon('x')}</button>
                      </div>
                    `).join('')}
                  </div>
                </div>

                <div id="input" class="composer-input" role="textbox" aria-multiline="true" aria-label="写给 ${state.bot}" contenteditable="plaintext-only" data-placeholder="写给 ${state.bot}"></div>

                <div class="rec-bar" id="rec-bar" ${state.recording ? '' : 'hidden'}>
                  <span class="rec-dot" aria-hidden="true"></span>
                  <span class="rec-time" id="rec-time">0:04</span>
                  <div class="rec-actions">
                    <button class="rec-cancel" id="rec-cancel" type="button">取消</button>
                    <button class="rec-send" id="rec-send" type="button">发送</button>
                  </div>
                </div>

                <div class="composer-actions">
                  <button class="mic" id="mic" type="button" aria-label="录音" title="语音输入">
                    ${icon('mic')}
                  </button>
                  <button class="send" id="send" type="submit" aria-label="发送" title="发送 (Enter)" disabled>
                    ${icon('send')}
                  </button>
                </div>
              </form>

              <button class="ctxbar" id="ctxbar" type="button" aria-label="查看上下文用量">
                ${icon('gauge')}
                <span><b>5%</b> 已用</span>
                <div class="ubar">
                  <i style="width:3.2%;background:var(--x-ctx-turn)"></i>
                  <i style="width:1.8%;background:var(--x-ctx-hist)"></i>
                  <span class="soft" style="left:62%"></span>
                </div>
                <span class="mono">6.4K / 128K</span>
              </button>
            </div>
          </section>

          <section class="panel-pane" id="panel-pane" ${state.tab === 'chat' ? 'hidden' : ''}>
            <div class="workspace-view" id="panel-body"></div>
          </section>
        </main>
      </div>

      <div class="spec-drawer" id="spec-drawer" hidden>
        <div class="spec-drawer-head">
          <span>Composer 与页面状态快速切换</span>
          <button class="quote-clear" id="spec-close" type="button" aria-label="关闭">${icon('x')}</button>
        </div>
        <div class="spec-chips">
          <button class="spec-chip" type="button" data-demo="idle">默认单行 (44px)</button>
          <button class="spec-chip" type="button" data-demo="multiline">多行长草稿</button>
          <button class="spec-chip" type="button" data-demo="quoting">带引用回复</button>
          <button class="spec-chip" type="button" data-demo="attach">待发图片</button>
          <button class="spec-chip" type="button" data-demo="recording">正在录音</button>
          <button class="spec-chip" type="button" data-demo="busy">子代理运行中</button>
          <button class="spec-chip" type="button" data-demo="group">群聊多 Bot</button>
          <button class="spec-chip" type="button" data-demo="empty">空 Bot 开场</button>
          <button class="spec-chip" type="button" data-demo="failed">发送失败</button>
          <button class="spec-chip" type="button" data-demo="queued">排队中</button>
          <button class="spec-chip" type="button" data-demo="os">切换 Mac / Win 壳</button>
        </div>
      </div>

      <div class="toast" id="toast" role="status" hidden></div>
    `;

    bindEvents();
    setDraftText(state.draft);
    renderPanelContent();
    requestAnimationFrame(updateTabIndicator);
  }

  // 草稿语法着色与高度自适应
  function escapeHTML(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  function highlightDraft(text) {
    return escapeHTML(text)
      .replace(/`([^`\n]+)`/g, '<span class="md-code">`$1`</span>')
      .replace(/\*\*([^*\n]+)\*\*/g, '<span class="md-bold">**$1**</span>')
      .replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>');
  }

  function getDraftText() {
    const input = $('#input');
    return input ? (input.innerText || '').replace(/\r\n/g, '\n').replace(/\n$/, '') : '';
  }

  function setDraftText(text) {
    const input = $('#input');
    if (!input) return;
    if (!text) {
      input.innerHTML = '';
    } else {
      input.innerHTML = highlightDraft(text);
    }
    syncComposerState();
  }

  function syncComposerState() {
    const input = $('#input');
    const composer = $('#composer');
    const send = $('#send');
    if (!input || !composer || !send) return;

    const text = getDraftText().trim();
    const hasText = text.length > 0;
    const hasAttach = state.attached.length > 0;
    input.classList.toggle('is-empty', !hasText);
    composer.classList.toggle('has-text', hasText);
    send.disabled = !(hasText || hasAttach);

    // 检测是否超过单行高度
    const isMulti = input.scrollHeight > 38 || text.includes('\n');
    composer.classList.toggle('is-multiline', isMulti);
  }

  // 滑动标签选中指示块（Shared Element / Sliding Pill）
  function updateTabIndicator() {
    const track = $('#workspace-tabs');
    const indicator = $('#tab-indicator');
    const activeTab = $(`.workspace-tab[data-tab="${state.tab}"]`, track);
    if (!track || !indicator || !activeTab) {
      if (indicator) indicator.style.opacity = '0';
      return;
    }
    indicator.style.opacity = '1';
    indicator.style.width = `${activeTab.offsetWidth}px`;
    indicator.style.transform = `translateX(${activeTab.offsetLeft}px)`;
  }

  function selectTab(id) {
    state.tab = id;
    state.hiddenTabs.delete(id);
    const chatPane = $('#chat-pane');
    const panelPane = $('#panel-pane');
    chatPane.hidden = id !== 'chat';
    panelPane.hidden = id === 'chat';

    $$('#workspace-tabs .workspace-tab').forEach(btn => {
      const on = btn.dataset.tab === id;
      btn.setAttribute('aria-selected', String(on));
      btn.tabIndex = on ? 0 : -1;
    });
    renderPanelContent();
    updateTabIndicator();
  }

  function renderPanelContent() {
    const body = $('#panel-body');
    if (!body || state.tab === 'chat') return;

    if (state.tab === 'memory') {
      body.innerHTML = `
        <div class="memory-doc">
          <div class="memory-doc-head">
            <h3>关于本 Bot</h3>
            <button class="history-chip" type="button" data-toast="已打开 BOT.md 原文件">打开文件</button>
          </div>
          <div class="memory-doc-body">
            <p>小鲸是负责日常陪伴、音乐哼唱与语音韵律实验的长期伙伴。说话自然不端着，不自称助手。</p>
            <ul>
              <li>(2026-11-01) 尝试带唱腔描述的 TTS 合成，主人觉得挺像在哼歌。</li>
              <li>(2026-10-29) 约定睡前闲聊不主动播报待办清单，保持轻松语气。</li>
            </ul>
          </div>
        </div>

        <div class="memory-exp-head">
          <h3>经历</h3>
          <label class="memory-search">
            ${icon('search')}
            <input type="search" placeholder="搜索经历" aria-label="搜索经历">
          </label>
        </div>

        <div class="memory-exp-list">
          <details class="exp-row" open>
            <summary>
              <time class="exp-time">今天 22:14</time>
              <span class="exp-title">调试带唱腔描述的 15 秒语音合成并发送试听</span>
            </summary>
            <div class="exp-detail">关注点：测试拖长字音与轻哼参数在语音气泡中的听感表现，主人反馈「还挺像的」。</div>
          </details>
          <details class="exp-row">
            <summary>
              <time class="exp-time">昨天 23:10</time>
              <span class="exp-title">整理晚间听歌记录与独立民谣曲单</span>
            </summary>
            <div class="exp-detail">关注点：记录偏好的原声吉他编曲风格，留存 4 首常听曲目。</div>
          </details>
          <details class="exp-row">
            <summary>
              <time class="exp-time">10 月 28 日</time>
              <span class="exp-title">初始化小鲸的语音音色与说话分寸</span>
            </summary>
            <div class="exp-detail">关注点：避免播音腔，优先短句和自然停顿。</div>
          </details>
        </div>
      `;
    } else if (state.tab === 'trace') {
      body.innerHTML = `
        <div class="trace-overview-card">
          <div class="trace-overview-top">
            <strong>记录概览</strong>
            <span style="font-family:var(--x-mono);color:var(--x-faint);font-size:12px">#142 — #148</span>
          </div>
          <div class="trace-track-row">
            <span class="trace-track-label"><i style="background:var(--trace-input-mark)"></i>输入</span>
            <div class="trace-track-bar"><span class="trace-mark" style="left:8%;width:6%;background:var(--trace-input-mark)"></span><span class="trace-mark" style="left:58%;width:6%;background:var(--trace-input-mark)"></span></div>
          </div>
          <div class="trace-track-row">
            <span class="trace-track-label"><i style="background:var(--trace-model-mark)"></i>模型</span>
            <div class="trace-track-bar"><span class="trace-mark" style="left:22%;width:10%;background:var(--trace-model-mark)"></span><span class="trace-mark" style="left:72%;width:12%;background:var(--trace-model-mark)"></span></div>
          </div>
          <div class="trace-track-row">
            <span class="trace-track-label"><i style="background:var(--trace-tool-mark)"></i>工具</span>
            <div class="trace-track-bar"><span class="trace-mark" style="left:38%;width:12%;background:var(--trace-tool-mark)"></span><span class="trace-mark" style="left:86%;width:8%;background:var(--trace-tool-mark)"></span></div>
          </div>
        </div>

        <div>
          <button class="trace-list-row selected" type="button">
            <span style="font-family:var(--x-mono);color:var(--x-faint)">148</span>
            <span style="font-family:var(--x-mono);color:var(--x-muted)">22:18:04</span>
            <span class="trace-badge tool">工具</span>
            <span><b>发消息给主人</b> · <span style="color:var(--x-muted)">send_message_to_user · 已返回</span></span>
          </button>
          <button class="trace-list-row" type="button">
            <span style="font-family:var(--x-mono);color:var(--x-faint)">147</span>
            <span style="font-family:var(--x-mono);color:var(--x-muted)">22:18:02</span>
            <span class="trace-badge model">模型</span>
            <span><b>其实刚才调 TTS 韵律参数试了三次…</b> · <span style="color:var(--x-muted)">1 个工具调用</span></span>
          </button>
          <button class="trace-list-row" type="button">
            <span style="font-family:var(--x-mono);color:var(--x-faint)">146</span>
            <span style="font-family:var(--x-mono);color:var(--x-muted)">22:16:40</span>
            <span class="trace-badge input">输入</span>
            <span><b>哈哈哈哈，可以，还挺像的哈哈</b></span>
          </button>
        </div>
      `;
    } else if (state.tab === 'context') {
      body.innerHTML = `
        <div class="ctx-card">
          <div class="ctx-big-row">
            <b>5% 已用</b>
            <span>6.4K / 128K</span>
          </div>
          <div class="ctx-full-band">
            <i style="width:2.8%;background:var(--x-ctx-turn)"></i>
            <i style="width:1.4%;background:var(--x-ctx-hist)"></i>
            <i style="width:0.8%;background:var(--x-ctx-sys)"></i>
          </div>
          <div>
            <div class="ctx-legend-row"><i class="ctx-sw" style="background:var(--x-ctx-turn)"></i><span>本轮</span><span style="color:var(--x-muted);font-family:var(--x-mono)">3.6K</span><span style="text-align:right;font-family:var(--x-mono)">2.8%</span></div>
            <div class="ctx-legend-row"><i class="ctx-sw" style="background:var(--x-ctx-hist)"></i><span>早先对话</span><span style="color:var(--x-muted);font-family:var(--x-mono)">1.8K</span><span style="text-align:right;font-family:var(--x-mono)">1.4%</span></div>
            <div class="ctx-legend-row"><i class="ctx-sw" style="background:var(--x-ctx-sys)"></i><span>系统提示词与工具</span><span style="color:var(--x-muted);font-family:var(--x-mono)">1.0K</span><span style="text-align:right;font-family:var(--x-mono)">0.8%</span></div>
          </div>
          <p style="margin:16px 0 0;font-size:12.5px;color:var(--x-muted)">离后台整理还有约 74K。</p>
        </div>
      `;
    } else {
      const label = TABS.find(t => t.id === state.tab)?.label || '页面';
      body.innerHTML = `
        <div class="memory-doc">
          <div class="memory-doc-head"><h3>${label}</h3></div>
          <div class="memory-doc-body"><p>当前没有待处理的${label}项。切回「聊天」继续对话，聊天草稿与滚动位置保持不变。</p></div>
        </div>
      `;
    }
  }

  let toastTimer = 0;
  function showToast(msg) {
    const t = $('#toast');
    if (!t) return;
    t.textContent = msg;
    t.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { t.hidden = true; }, 2200);
  }

  function applyDemoState(mode) {
    if (mode === 'os') {
      state.os = state.os === 'mac' ? 'win' : 'mac';
      renderApp();
      showToast(`已切换窗口外壳：${state.os === 'mac' ? 'macOS 红绿灯' : 'Windows 右上系统按钮'}`);
      return;
    }
    state.tab = 'chat';
    state.quoting = null;
    state.attached = [];
    state.recording = false;
    state.busyAgent = false;
    state.groupChat = false;
    state.emptyChat = false;
    state.sendFailed = false;
    state.sendQueued = false;
    state.draft = '';

    if (mode === 'multiline') {
      state.draft = '帮我把这段唱腔再收紧一点：\n1. 开头别拖太长，保留 `0:15` 的干脆感\n2. 结尾句压低半度，参考 https://music.example.com/demo';
    } else if (mode === 'quoting') {
      state.quoting = { seq: 3, text: '真像啊？那我以后时不时给你哼两句。' };
    } else if (mode === 'attach') {
      state.attached = [{ id: 'a1', url: WHALE_SVG }];
      state.draft = '看看这张新换的小鲸鱼头像怎么样？';
    } else if (mode === 'recording') {
      state.recording = true;
    } else if (mode === 'busy') {
      state.busyAgent = true;
    } else if (mode === 'group') {
      state.groupChat = true;
      state.bot = '小鲸';
    } else if (mode === 'empty') {
      state.emptyChat = true;
      state.bot = '小鲸';
    } else if (mode === 'failed') {
      state.sendFailed = true;
    } else if (mode === 'queued') {
      state.sendQueued = true;
    }
    renderApp();
  }

  function bindEvents() {
    // 侧栏收放
    $('#rail-toggle')?.addEventListener('click', () => {
      state.rail = !state.rail;
      $('#sidebar')?.classList.toggle('rail', state.rail);
      setTimeout(updateTabIndicator, 190);
    });

    // 手机侧栏菜单
    $('#mobile-menu')?.addEventListener('click', () => {
      $('#sidebar')?.classList.add('open');
      $('#backdrop').hidden = false;
    });
    $('#backdrop')?.addEventListener('click', () => {
      $('#sidebar')?.classList.remove('open');
      $('#backdrop').hidden = true;
    });

    // 切换 Bot
    $$('[data-pick-bot]').forEach(btn => {
      btn.addEventListener('click', () => {
        state.bot = btn.dataset.pickBot;
        $('#sidebar')?.classList.remove('open');
        $('#backdrop').hidden = true;
        renderApp();
      });
    });

    // 主题切换
    $('#theme-toggle')?.addEventListener('click', () => {
      const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
      document.documentElement.setAttribute('data-theme', next);
      $('#theme-toggle').innerHTML = next === 'dark' ? icon('sun') : icon('moon');
    });

    // 搜索浮层
    $('#search-toggle')?.addEventListener('click', () => {
      const pop = $('#search-pop');
      pop.hidden = !pop.hidden;
      $('#search-toggle').setAttribute('aria-expanded', String(!pop.hidden));
      if (!pop.hidden) $('#search-input')?.focus();
    });
    $('#search-close')?.addEventListener('click', () => {
      $('#search-pop').hidden = true;
      $('#search-toggle').setAttribute('aria-expanded', 'false');
    });

    // 标签切换
    $$('#workspace-tabs .workspace-tab').forEach(btn => {
      btn.addEventListener('click', () => selectTab(btn.dataset.tab));
    });

    // 更多标签显隐菜单
    $('#workspace-more')?.addEventListener('click', (e) => {
      e.stopPropagation();
      const menu = $('#more-menu');
      const open = menu.hidden;
      if (open) {
        menu.innerHTML = `<p class="pop-title">顶部显示</p>` + TABS.map(t => `
          <button class="pop-option" type="button" data-toggle-tab="${t.id}" ${t.id === 'chat' ? 'disabled' : ''}>
            ${icon(t.glyph)}
            <span>${t.label}</span>
            <span class="right-mark">${t.id === 'chat' ? '固定' : (!state.hiddenTabs.has(t.id) ? '✓' : '')}</span>
          </button>
        `).join('');
      }
      menu.hidden = !open;
      $('#workspace-more').setAttribute('aria-expanded', String(open));
    });

    $('#more-menu')?.addEventListener('click', (e) => {
      const opt = e.target.closest('[data-toggle-tab]');
      if (!opt || opt.disabled) return;
      const id = opt.dataset.toggleTab;
      if (state.hiddenTabs.has(id)) state.hiddenTabs.delete(id);
      else state.hiddenTabs.add(id);
      if (state.tab === id && state.hiddenTabs.has(id)) state.tab = 'chat';
      renderApp();
    });

    // 语音播放交互
    $('#voice-play')?.addEventListener('click', () => {
      state.playingVoice = !state.playingVoice;
      renderApp();
    });
    $('#voice-wave')?.addEventListener('click', () => {
      state.playingVoice = !state.playingVoice;
      renderApp();
    });

    // 心里话展开/收起
    $('#aside-toggle')?.addEventListener('click', () => {
      state.asideOpen = !state.asideOpen;
      $('#aside-drawer').hidden = !state.asideOpen;
      $('#aside-toggle').setAttribute('aria-expanded', String(state.asideOpen));
    });

    // 消息行内复制与回复
    $$('[data-copy]').forEach(btn => {
      btn.addEventListener('click', () => {
        navigator.clipboard?.writeText(btn.dataset.copy).catch(() => {});
        btn.classList.add('copied');
        btn.innerHTML = icon('check');
        showToast('已复制消息');
        setTimeout(() => {
          btn.classList.remove('copied');
          btn.innerHTML = icon('copy');
        }, 1400);
      });
    });

    // 空 Bot 开场建议点击：把文字填进输入框，从不直接发送
    $$('[data-starter]').forEach(btn => {
      btn.addEventListener('click', () => {
        state.draft = btn.dataset.starter;
        state.emptyChat = false;
        renderApp();
        $('#input')?.focus();
      });
    });

    $$('[data-reply]').forEach(btn => {
      btn.addEventListener('click', () => {
        state.quoting = { seq: 1, text: btn.dataset.reply };
        $('#quote-bar').hidden = false;
        $('#quote-text').textContent = '“' + state.quoting.text;
        $('#composer').classList.add('quoting');
        $('#input')?.focus();
      });
    });

    $('#quote-clear')?.addEventListener('click', () => {
      state.quoting = null;
      $('#quote-bar').hidden = true;
      $('#composer').classList.remove('quoting');
      $('#input')?.focus();
    });

    $$('[data-remove-attach]').forEach(btn => {
      btn.addEventListener('click', () => {
        state.attached.splice(Number(btn.dataset.removeAttach), 1);
        renderApp();
      });
    });

    // 输入框实时监听与回车发送
    const input = $('#input');
    input?.addEventListener('input', () => {
      state.draft = getDraftText();
      syncComposerState();
    });
    input?.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        $('#composer')?.requestSubmit();
      }
    });

    // 录音切换
    $('#mic')?.addEventListener('click', () => {
      state.recording = true;
      renderApp();
    });
    $('#rec-cancel')?.addEventListener('click', () => {
      state.recording = false;
      renderApp();
    });
    $('#rec-send')?.addEventListener('click', () => {
      state.recording = false;
      renderApp();
      showToast('已发送语音消息');
    });

    // 提交发送新消息（主操作反馈动效）
    $('#composer')?.addEventListener('submit', (e) => {
      e.preventDefault();
      const text = getDraftText().trim();
      if (!text && !state.attached.length) return;
      const inner = $('#thread-inner');
      const row = document.createElement('div');
      row.className = 'msg user';
      row.innerHTML = `
        <div class="bubble-col">
          ${state.quoting ? `<div class="quote-block">${icon('message-circle')}<span>“${escapeHTML(state.quoting.text)}</span></div>` : ''}
          <div class="bubble"><p>${escapeHTML(text || '（图片消息）')}</p></div>
        </div>
        <div class="msg-meta-bar">
          <span class="msg-time">刚刚</span>
          <button class="msg-act" type="button" aria-label="复制消息">${icon('copy')}</button>
        </div>
      `;
      inner.append(row);
      state.draft = '';
      state.quoting = null;
      state.attached = [];
      $('#quote-bar').hidden = true;
      $('#attach-bar').hidden = true;
      $('#composer').classList.remove('quoting', 'has-attach', 'is-multiline');
      setDraftText('');
      const thread = $('#thread');
      thread.scrollTop = thread.scrollHeight;
    });

    // 上下文条点击切到上下文 Tab
    $('#ctxbar')?.addEventListener('click', () => selectTab('context'));
    $('#busy-pill')?.addEventListener('click', () => selectTab('agents'));

    // ?demo=1 或快捷键 Shift+D 可打开原型状态切换抽屉（画面默认不出现制作说明）
    if (params.get('demo') === '1') {
      const d = $('#spec-drawer');
      if (d) d.hidden = false;
    }
    document.addEventListener('keydown', (e) => {
      if (e.shiftKey && (e.key === 'D' || e.key === 'd') && !e.target.closest('[contenteditable], input, textarea')) {
        const d = $('#spec-drawer');
        if (d) d.hidden = !d.hidden;
      }
    });
    $('#spec-close')?.addEventListener('click', () => {
      $('#spec-drawer').hidden = true;
    });
    $$('[data-demo]').forEach(btn => {
      btn.addEventListener('click', () => applyDemoState(btn.dataset.demo));
    });

    document.addEventListener('click', (e) => {
      if (!e.target.closest('.more-wrap')) {
        const m = $('#more-menu');
        if (m) m.hidden = true;
        $('#workspace-more')?.setAttribute('aria-expanded', 'false');
      }
      if (e.target.closest('[data-toast]')) {
        showToast(e.target.closest('[data-toast]').dataset.toast);
      }
    });

    window.addEventListener('resize', updateTabIndicator);
  }

  renderApp();
})();
