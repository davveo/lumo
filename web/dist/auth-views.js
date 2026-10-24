(function () {
  'use strict';

  const auth = () => window.LumoAuth;
  const viewModel = () =>
    auth()?.model() || {
      tab: 'password',
      returnTo: '#/member',
      returnLabel: '个人中心',
      remember: false,
      menuOpen: false,
      loggedIn: false,
      user: null,
      otp: {},
      error: '',
      busy: false,
      passwordVisible: false,
    };
  const displayName = (data) =>
    data.loggedIn ? window.LumoMember?.model()?.profile?.nickname || data.user?.nickname || 'LUMO 生活家' : '';
  const brand = () => ({ brand: 'LUMO', brandSuffix: 'Mall', ...shop() });
  const eye = (open) =>
    `<svg viewBox="0 0 24 24" aria-hidden="true"><path d="${open ? 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Zm13 0a3 3 0 1 1-6 0 3 3 0 0 1 6 0' : 'm3 3 18 18M10.6 5.1 12 5c6.5 0 10 7 10 7a18.5 18.5 0 0 1-3.5 4.5M6.4 6.4C3.5 8.6 2 12 2 12s3.5 7 10 7a11 11 0 0 0 5.6-1.5M10 10a3 3 0 0 0 4 4'}"/></svg>`;
  const lock = () =>
    '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 10h14v11H5V10Zm3 0V7a4 4 0 0 1 8 0v3M12 14v3"/></svg>';
  const backLink = () => `<a class="auth-back-shop" href="#/">${icon('back')}返回商城</a>`;
  const logo = () => {
    const b = brand();
    return `<a class="brand" href="#/" aria-label="${esc(b.brand)} ${esc(b.brandSuffix)} 首页"><b>${esc(b.brand)}</b><span>${esc(b.brandSuffix)}</span><small>CURATED FOR EVERYDAY</small></a>`;
  };

  function headerUtility() {
    const data = viewModel();
    if (!data.loggedIn)
      return `<span>欢迎来到 ${esc(brand().brand)} ${esc(brand().brandSuffix)}</span><button class="auth-utility-login" data-auth-action="login">请登录</button>`;
    return `<span class="auth-utility-greeting" title="${esc(displayName(data))}">你好，${esc(displayName(data))}</span><a href="#/member">个人中心</a><button data-auth-action="logout">退出登录</button>`;
  }

  function headerAccount() {
    const data = viewModel();
    if (!data.loggedIn)
      return `<button class="icon-button auth-login-trigger" data-auth-action="login" aria-label="登录">${icon('user')}<span class="header-label">登录</span></button>`;
    const name = displayName(data),
      initial = name.trim().slice(0, 1).toUpperCase() || 'L';
    return `<div class="auth-user-control" data-auth-account><button data-auth-menu-trigger class="auth-user-trigger ${data.menuOpen ? 'is-open' : ''}" data-auth-action="toggle-menu" aria-label="账号菜单" aria-expanded="${!!data.menuOpen}" aria-controls="auth-account-menu"><span class="auth-avatar">${esc(initial)}</span><span class="auth-user-label">我的</span>${icon('chevron', 'auth-user-chevron')}</button><div id="auth-account-menu" data-auth-menu class="auth-account-menu" ${data.menuOpen ? '' : 'hidden'}><div class="auth-menu-profile"><span class="auth-avatar">${esc(initial)}</span><div><strong>${esc(name)}</strong><small>你的 LUMO 生活会员</small></div></div><nav class="auth-menu-links" aria-label="会员快捷菜单">${[
      ['user', '个人中心', '#/member'],
      ['bag', '我的订单', '#/member/orders'],
      ['bell', '消息中心', '#/member/messages'],
      ['ticket', '我的优惠券', '#/member/coupons'],
      ['shield', '账户设置', '#/member/settings'],
    ]
      .map(([symbol, title, href]) => `<a href="${href}">${icon(symbol)}<span>${title}</span>${icon('chevron')}</a>`)
      .join(
        '',
      )}</nav><button class="auth-menu-logout" data-auth-action="logout">${icon('back')}退出登录</button></div></div>`;
  }

  function passwordFields(data) {
    return `<label class="auth-field"><span>账号</span><span class="auth-input-wrap">${icon('user')}<input id="auth-username" name="username" type="text" maxlength="128" autocomplete="username" placeholder="请输入账号" required></span></label><label class="auth-field"><span>密码</span><span class="auth-input-wrap">${lock()}<input id="auth-password" name="password" type="${data.passwordVisible ? 'text' : 'password'}" maxlength="128" autocomplete="current-password" placeholder="请输入密码" required><button type="button" class="auth-password-toggle" data-auth-action="toggle-password" data-target="auth-password" aria-label="${data.passwordVisible ? '隐藏密码' : '显示密码'}" aria-pressed="${!!data.passwordVisible}">${eye(data.passwordVisible)}</button></span></label>`;
  }

  function page() {
    const data = viewModel(),
      b = brand();
    return `<div class="auth-page"><header class="auth-topbar"><div class="container auth-topbar-inner">${logo()}<div><span class="auth-header-note">美好日常，从这里继续</span>${backLink()}</div></div></header><main class="container auth-main" id="main"><div class="auth-layout"><section class="auth-story" aria-label="LUMO 品质生活会员"><span class="auth-kicker">A LITTLE MORE, FOR YOUR EVERYDAY</span><h1>喜欢的好物，<br>都在等你回家。</h1><p>从每一次心动，到每一份日常。<br>登录 ${esc(b.brand)}，继续你的品质生活。</p><div class="auth-story-image"><img src="${esc(asset('hero.jpg'))}" alt="笔记本、耳机与智能设备组成的品质生活桌面"><span class="auth-image-tag">CURATED FOR YOUR EVERYDAY</span><div class="auth-story-note">${icon('heart')}把心动留住，把日常过好。</div></div><div class="auth-benefits">${[
      ['bag', '订单随时查看'],
      ['ticket', '专属会员礼遇'],
      ['heart', '心动好物收藏'],
    ]
      .map(([symbol, label]) => `<div>${icon(symbol)}<span>${label}</span></div>`)
      .join(
        '',
      )}</div></section><section class="auth-card" aria-label="会员登录"><div class="auth-form-heading"><h2>欢迎回到 ${esc(b.brand)}</h2><p>登录后，让每一份喜欢都有归处。</p></div><div class="auth-tabs"><span class="active">账号密码登录</span></div><div id="auth-login-panel"><form id="auth-login-form">${passwordFields(data)}<div class="auth-form-meta"><label class="auth-remember"><input type="checkbox" name="remember" ${data.remember ? 'checked' : ''}>在此设备记住登录</label><button class="auth-forgot" type="button" data-auth-action="forgot-password">忘记密码？</button></div><div class="auth-error ${data.error ? 'is-visible' : ''}" role="alert" aria-live="polite" ${data.error ? '' : 'hidden'}>${data.error ? icon('info') + esc(data.error) : ''}</div><button class="auth-submit" type="submit" ${data.busy || data.checking ? 'disabled' : ''}>${data.checking ? '正在验证会话…' : data.busy ? '正在登录…' : '登 录'}${data.busy || data.checking ? '<span class="auth-spinner" aria-hidden="true"></span>' : icon('arrow')}</button>${data.error ? '<button type="button" class="text-button" data-auth-action="retry-session" style="display:block;margin:12px auto 0">重新验证已保存的会话</button>' : ''}</form></div></section></div></main><footer class="auth-mini-footer container"><span>© 2026 ${esc(b.brand)} ${esc(b.brandSuffix)} · 品质生活，好物相伴</span><div><button data-action="help">购物帮助</button><button data-action="privacy">隐私说明</button></div></footer></div>`;
  }

  window.LumoAuthView = { page, headerUtility, headerAccount, utility: headerUtility, account: headerAccount };
})();
