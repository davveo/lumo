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
    return `<a class="brand" href="#/" aria-label="${esc(b.brand)} ${esc(b.brandSuffix)} 首页"><b>${esc(b.brand)}</b><span>${esc(b.brandSuffix)}</span></a>`;
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
    return `<label class="auth-field"><span>账号</span><span class="auth-input-wrap">${icon('user')}<input id="auth-username" name="username" type="text" maxlength="128" autocomplete="username" placeholder="请输入账号" value="${esc(data.registrationName || '')}" required></span></label><label class="auth-field"><span>密码</span><span class="auth-input-wrap">${lock()}<input id="auth-password" name="password" type="${data.passwordVisible ? 'text' : 'password'}" maxlength="128" autocomplete="current-password" placeholder="请输入密码" required><button type="button" class="auth-password-toggle" data-auth-action="toggle-password" data-target="auth-password" aria-label="${data.passwordVisible ? '隐藏密码' : '显示密码'}" aria-pressed="${!!data.passwordVisible}">${eye(data.passwordVisible)}</button></span></label>`;
  }

  function phoneFields(data) {
    return `<label class="auth-field"><span>手机号</span><span class="auth-input-wrap"><input id="auth-phone" name="phone" type="tel" inputmode="tel" autocomplete="tel" maxlength="11" placeholder="请输入手机号" required value="${esc(data.phone || '')}"></span></label><label class="auth-field"><span>短信验证码</span><span class="auth-input-wrap"><input id="auth-code" name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" maxlength="6" placeholder="请输入6位验证码" required value="${esc(data.code || '')}"><button type="button" class="auth-otp-send" data-auth-action="send-otp" ${data.otp?.sending || data.otp?.remaining ? 'disabled' : ''}>${data.otp?.sending ? '正在发送…' : data.otp?.remaining ? data.otp.remaining + '秒后重试' : '获取验证码'}</button></span></label>${data.otp?.demoCode ? `<p class="auth-channel-note">演示验证码：<strong>${esc(data.otp.demoCode)}</strong>（未发送短信）</p>` : ''}<p class="auth-channel-note">${data.allowRegistration ? '未注册的手机号验证后将创建会员账号。' : '仅支持已注册会员登录。'}</p>`;
  }
  function loginTabs(data) {
    return `<div class="auth-tabs">${(data.methods || [{ id: 'password', name: '账号密码' }])
      .filter((m) => ['password', 'phone'].includes(m.id))
      .map(
        (m) =>
          `<button type="button" class="${data.tab === m.id ? 'active' : ''}" data-auth-action="login-tab" data-id="${esc(m.id)}">${esc(m.name)}登录</button>`,
      )
      .join('')}</div>`;
  }
  function socialLogins(data) {
    const rows = (data.methods || []).filter((m) => ['wechat', 'qq'].includes(m.id));
    return rows.length
      ? `<div class="auth-social-methods"><p>其他登录方式</p>${rows.map((m) => `<button type="button" class="outline" data-auth-action="oauth" data-id="${esc(m.id)}" ${data.busy ? 'disabled' : ''}>${esc(m.name)}登录</button>`).join('')}</div>`
      : '';
  }
  function registration(data) {
    const password = data.methods?.some((m) => m.id === 'password');
    if (!data.methodsLoaded) return '<p role="status">正在读取注册方式…</p>';
    if (!data.allowRegistration)
      return '<p class="auth-channel-note">商城暂未开放新会员注册。</p><a class="text-button" href="#/login">返回登录</a>';
    if (!password)
      return `<p class="auth-channel-note">请选择已开放的注册方式。</p>${data.methods?.some((m) => m.id === 'phone') ? '<button type="button" class="auth-submit" data-auth-action="register-phone">手机号注册</button>' : ''}${socialLogins(data)}<a class="text-button" href="#/login">已有账号，去登录</a>`;
    return `<form id="auth-register-form"><label class="auth-field"><span>账号</span><span class="auth-input-wrap">${icon('user')}<input name="username" autocomplete="username" minlength="4" maxlength="32" pattern="[a-zA-Z][a-zA-Z0-9_.-]{3,31}" placeholder="4至32位，以字母开头" value="${esc(data.registrationName || '')}" required></span></label><label class="auth-field"><span>密码</span><span class="auth-input-wrap">${lock()}<input name="password" type="password" autocomplete="new-password" minlength="8" maxlength="72" placeholder="至少8位，包含字母和数字" required></span></label><label class="auth-field"><span>确认密码</span><span class="auth-input-wrap">${lock()}<input name="confirm_password" type="password" autocomplete="new-password" minlength="8" maxlength="72" placeholder="再次输入密码" required></span></label><div class="auth-error ${data.error ? 'is-visible' : ''}" role="alert" ${data.error ? '' : 'hidden'}>${esc(data.error)}</div><button type="submit" class="auth-submit" ${data.busy ? 'disabled' : ''}>${data.busy ? '正在注册…' : '注 册'}${icon('arrow')}</button></form><div class="auth-registration-entry">已有账号？<a href="#/login">立即登录</a></div>`;
  }
  function loginPanel(data) {
    return `${loginTabs(data)}<div id="auth-login-panel">${data.methodsLoaded && !data.methods?.some((m) => ['password', 'phone'].includes(m.id)) ? '<p class="auth-channel-note">请选择下方第三方登录方式，或联系商城客服。</p>' : ''}<form id="auth-login-form" ${data.methodsLoaded === false || (data.methodsLoaded && !data.methods?.some((m) => ['password', 'phone'].includes(m.id))) ? 'hidden' : ''}>${data.tab === 'phone' ? phoneFields(data) : passwordFields(data)}<div class="auth-form-meta"><label class="auth-remember"><input type="checkbox" name="remember" ${data.remember ? 'checked' : ''}>在此设备记住登录</label><button class="auth-forgot" type="button" data-auth-action="forgot-password">忘记密码？</button></div><div class="auth-error ${data.error ? 'is-visible' : ''}" role="alert" aria-live="polite" ${data.error ? '' : 'hidden'}>${data.error ? icon('info') + esc(data.error) : ''}</div><button class="auth-submit" type="submit" ${data.busy || data.checking ? 'disabled' : ''}>${data.checking ? '正在验证会话…' : data.busy ? '正在登录…' : '登 录'}${data.busy || data.checking ? '<span class="auth-spinner" aria-hidden="true"></span>' : icon('arrow')}</button>${data.error ? '<button type="button" class="text-button" data-auth-action="retry-session" style="display:block;margin:12px auto 0">重新验证已保存的会话</button>' : ''}</form>${(data.methodsLoaded === false || !data.methods?.some((m) => ['password', 'phone'].includes(m.id))) && data.error ? `<div class="auth-error is-visible" role="alert">${esc(data.error)}</div>` : ''}${data.methodsFailed ? '<button type="button" class="text-button" data-auth-action="reload-methods">重新读取登录方式</button>' : ''}${socialLogins(data)}${data.methodsLoading ? '<p class="auth-channel-note">正在读取登录方式…</p>' : ''}${data.methodsLoaded && data.allowRegistration ? '<div class="auth-registration-entry">还没有账号？<a href="#/register">免费注册</a></div>' : ''}</div>`;
  }
  function page() {
    const data = viewModel(),
      b = brand(),
      registering = location.hash === '#/register';
    return `<div class="auth-page"><header class="auth-topbar"><div class="container auth-topbar-inner">${logo()}<div>${backLink()}</div></div></header><main class="container auth-main" id="main"><div class="auth-layout"><section class="auth-story" aria-label="商城商品展示"><div class="auth-story-image"><img src="${esc(asset('hero.jpg'))}" alt="笔记本、耳机与智能设备"></div></section><section class="auth-card" aria-label="${registering ? '会员注册' : '会员登录'}"><div class="auth-form-heading"><h1>${registering ? '注册账号' : '登录 ' + esc(b.brand)}</h1></div>${registering ? registration(data) : loginPanel(data)}${registering && data.methodsFailed ? '<p class="auth-error is-visible" role="alert">注册方式加载失败</p><button type="button" class="text-button" data-auth-action="reload-methods">重新加载</button>' : ''}</section></div></main><footer class="auth-mini-footer container"><span>© 2026 ${esc(b.brand)} ${esc(b.brandSuffix)}</span><div><button data-action="help">购物帮助</button><button data-action="privacy">隐私说明</button></div></footer></div>`;
  }

  window.LumoAuthView = { page, headerUtility, headerAccount, utility: headerUtility, account: headerAccount };
})();
