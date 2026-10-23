(function () {
  'use strict';
  const SESSION_KEY = 'lumo-api-session-v2';
  const RETURN_KEY = 'lumo-api-return-v2';
  const EVENT_KEY = 'lumo-api-auth-event-v2';
  const DATA_KEYS = new Set(['lumo-mall-v1', 'lumo-member-v1', 'lumo-promotions-v1']);
  const memory = new Map();
  const safeRead = (storage, key) => { try { return storage.getItem(key); } catch (_) { return null; } };
  const remove = (storage, key) => { try { storage.removeItem(key); } catch (_) {} };
  const parse = value => { try { return JSON.parse(value); } catch (_) { return null; } };
  const safeHash = (value, fallback = '#/member') => typeof value === 'string' && /^#\/(?:[a-zA-Z0-9_\/:-]|%[0-9a-fA-F]{2})*$/.test(value) && value.length <= 300 && !value.startsWith('#/login') ? value : fallback;
  const privateRoute = route => ['member', 'orders', 'checkout', 'payment', 'success', 'editor'].includes(route[0]) || route[0] === 'promotions' && route[1] === 'mygroups';
  const emit = (name, detail) => window.dispatchEvent(new CustomEvent(name, { detail }));
  const stored = [parse(safeRead(sessionStorage, SESSION_KEY)), parse(safeRead(localStorage, SESSION_KEY))]
    .filter(row => row && typeof row.token === 'string' && /^[a-f0-9]{64}$/.test(row.token) && Number(row.expiresAt) > Date.now())
    .sort((a, b) => b.issuedAt - a.issuedAt);
  let session = stored[0] || null;
  let verified = false, refreshPromise = null;
  const ui = { tab: 'password', returnTo: safeHash(safeRead(sessionStorage, RETURN_KEY)), remember: !!session?.remember, menuOpen: false, error: '', busy: false, checking: !!session, passwordVisible: false };
  if (session) window.LumoAPI.setToken(session.token);
  function getItem(key) {
    // Business caches are request-scoped memory only. Server refresh owns their
    // content; old prototype orders, balances and addresses are never migrated.
    if (DATA_KEYS.has(key)) return memory.get(key) || null;
    return safeRead(localStorage, key);
  }
  function setItem(key, value) {
    if (DATA_KEYS.has(key)) { memory.set(key, String(value)); return; }
    try { localStorage.setItem(key, value); } catch (_) {}
  }
  const isLoggedIn = () => verified && !!session && session.expiresAt > Date.now();
  function user() {
    if (!isLoggedIn()) return null;
    return { id: session.userId, username: session.username || session.userId, nickname: session.userId === 'admin' ? '商城管理员' : 'LUMO 生活家', role: session.role, expiresAt: session.expiresAt, remember: session.remember };
  }
  const labels = { orders: '我的订单', checkout: '确认订单', payment: '订单支付', success: '订单结果', editor: '商城编辑', cart: '购物车', promotions: '活动好物' };
  function model() { return { ...ui, loggedIn: isLoggedIn(), user: user(), returnLabel: labels[ui.returnTo.slice(2).split('/')[0]] || '个人中心', otp: {} }; }
  const redraw = () => { if (typeof renderKeepScroll === 'function') renderKeepScroll(); };
  const message = value => { if (typeof toast === 'function') toast(value); };
  function setReturn(hash) {
    ui.returnTo = safeHash(hash);
    try { sessionStorage.setItem(RETURN_KEY, ui.returnTo); } catch (_) {}
  }
  function clearSession(reason = '', broadcast = false) {
    const previous = session;
    session = null; verified = false; ui.checking = false; ui.menuOpen = false;
    memory.clear(); window.LumoAPI.setToken('');
    remove(localStorage, SESSION_KEY); remove(sessionStorage, SESSION_KEY);
    if (typeof state !== 'undefined') {
      state.cart = []; state.orders = []; state.pending = null; state.favorites = []; state.address = null; state.coupon = 0; state.points = false;
    }
    if (broadcast) { try { localStorage.setItem(EVENT_KEY, JSON.stringify({ type: 'logout', userId: previous?.userId, at: Date.now() })); } catch (_) {} }
    if (reason) ui.error = reason;
    emit('lumo:logout', { userId: previous?.userId, reason });
  }
  function persistSession(next, remember) {
    remove(localStorage, SESSION_KEY); remove(sessionStorage, SESSION_KEY);
    session = { token: next.token, userId: next.user_id, username: next.user_id, role: next.role, expiresAt: Date.parse(next.expires_at), issuedAt: Date.now(), remember: !!remember };
    if (!session.userId || !session.token || !Number.isFinite(session.expiresAt)) throw new Error('身份服务返回的会话不完整');
    try { (remember ? localStorage : sessionStorage).setItem(SESSION_KEY, JSON.stringify(session)); }
    catch (_) { /* The authenticated session still works in this document. */ }
    verified = true; ui.checking = false; window.LumoAPI.setToken(session.token);
  }
  async function refresh() {
    if (!session?.token) { ui.checking = false; return null; }
    if (refreshPromise) return refreshPromise;
    const token = session.token;
    refreshPromise = (async () => {
      try {
        const current = await window.LumoAPI.call('iam', 'current', {}, { token });
        if (!session || session.token !== token) return null;
        session.userId = current.user_id; session.role = current.role; session.expiresAt = Date.parse(current.expires_at);
        if (!current.user_id || !Number.isFinite(session.expiresAt)) throw new Error('身份服务返回的会话不完整');
        verified = true; ui.checking = false; ui.error = '';
        return user();
      } catch (error) {
        if (!session || session.token !== token) return null;
        verified = false; ui.checking = false;
        if (error.status === 401) clearSession('登录状态已失效，请重新登录');
        else ui.error = error.message;
        throw error;
      } finally { refreshPromise = null; }
    })();
    return refreshPromise;
  }
  function ensureSession() {
    if (isLoggedIn()) return true;
    if (verified && session && session.expiresAt <= Date.now()) clearSession('登录状态已过期，请重新登录');
    return false;
  }
  function requireLogin(hash = location.hash) {
    if (ensureSession()) return true;
    setReturn(hash); ui.menuOpen = false;
    if (location.hash !== '#/login') location.hash = '#/login'; else redraw();
    return false;
  }
  function gate(route) {
    if (privateRoute(route) && !ensureSession()) { requireLogin(location.hash); return false; }
    if (route[0] === 'editor' && user()?.role !== 'admin') { location.hash = '#/member'; message('商城编辑需要管理员账号'); return false; }
    if (route[0] === 'login' && ensureSession()) { location.hash = safeHash(ui.returnTo); return false; }
    return true;
  }
  function closeMenu() {
    ui.menuOpen = false;
    const menu = document.querySelector('[data-auth-menu]');
    if (menu) menu.hidden = true;
    document.querySelector('[data-auth-menu-trigger]')?.setAttribute('aria-expanded', 'false');
  }
  async function login(username, password, remember = false) {
    if (ui.busy) return null;
    ui.busy = true; ui.error = ''; ui.remember = remember;
    const destination = safeHash(ui.returnTo);
    try {
      const result = await window.LumoAPI.call('iam', 'login', { data: { username, password } }, { auth: false });
      memory.clear();
      persistSession(result, remember);
      remove(sessionStorage, RETURN_KEY);
      ui.busy = false;
      emit('lumo:login', { user: user() });
      location.hash = destination;
      redraw();
      return user();
    } catch (error) { ui.error = error.message; throw error; }
    finally { ui.busy = false; redraw(); }
  }
  function logout() {
    if (!ensureSession()) { requireLogin('#/member'); return; }
    closeMenu();
    if (typeof modal === 'function') modal('退出登录？', '<p class="modal-desc">退出后仍可浏览商品。订单、地址、优惠券和积分保留在账号中，重新登录即可查看。</p><div class="modal-actions"><button class="outline" data-action="close-modal">继续逛逛</button><button class="primary" data-auth-action="confirm-logout">确认退出</button></div>');
  }
  async function confirmLogout(all = false) {
    if (ui.busy) return;
    ui.busy = true;
    try {
      await window.LumoAPI.call('iam', all ? 'logout_all' : 'logout');
      clearSession('', true); if (typeof closeModal === 'function') closeModal(); location.hash = '#/'; redraw();
    } catch (error) { message(error.message); }
    finally { ui.busy = false; }
  }
  async function renew() {
    if (!ensureSession()) throw new Error('请先登录');
    const result = await window.LumoAPI.call('iam', 'refresh');
    persistSession(result, ui.remember); return user();
  }
  document.addEventListener('click', event => {
    const authButton = event.target.closest('[data-auth-action]');
    if (authButton && !authButton.disabled) {
      const action = authButton.dataset.authAction;
      if (action === 'login') { requireLogin(authButton.dataset.return || location.hash); return; }
      if (action === 'toggle-password') {
        const input = document.getElementById(authButton.dataset.target || 'auth-password');
        if (input) { ui.passwordVisible = input.type === 'password'; input.type = ui.passwordVisible ? 'text' : 'password'; authButton.setAttribute('aria-label', ui.passwordVisible ? '隐藏密码' : '显示密码'); authButton.setAttribute('aria-pressed', String(ui.passwordVisible)); }
        return;
      }
      if (action === 'forgot-password') { if (typeof modal === 'function') modal('账号帮助', '<p class="modal-desc">请联系商城管理员重置账号密码。</p><button class="primary" data-action="close-modal">知道了</button>'); return; }
      if (action === 'toggle-menu') { if (!ensureSession()) { requireLogin('#/member'); return; } ui.menuOpen = !ui.menuOpen; redraw(); return; }
      if (action === 'close-menu') { closeMenu(); return; }
      if (action === 'logout') { logout(); return; }
      if (action === 'confirm-logout') { confirmLogout(); return; }
      if (action === 'logout-all') { confirmLogout(true); return; }
      if (action === 'refresh-session') { renew().then(() => message('登录会话已更新')).catch(error => message(error.message)); return; }
      if (action === 'retry-session') { refresh().then(() => emit('lumo:login', { user: user() })).then(redraw).catch(error => message(error.message)); return; }
    }
    if (ui.menuOpen && !event.target.closest('[data-auth-account]')) closeMenu();
    const serviceButton = event.target.closest('[data-service-action]');
    const memberButton = event.target.closest('[data-member-action]');
    const appButton = event.target.closest('[data-action]');
    const promoButton = event.target.closest('[data-promo-action]');
    const editorButton = event.target.closest('[data-editor-action]');
    const protectedApp = ['pay', 'submit-order', 'order-details', 'favorite', 'points', 'coupon', 'coupons', 'address', 'choose-address', 'quick-add', 'detail-add', 'checkout', 'buy-now'];
    const protectedPromo = ['buy-flash', 'create-group', 'join-group', 'select-exchange', 'remove-exchange'];
    if (serviceButton || memberButton || editorButton || appButton && protectedApp.includes(appButton.dataset.action) || promoButton && protectedPromo.includes(promoButton.dataset.promoAction)) {
      if (!ensureSession()) { event.preventDefault(); event.stopImmediatePropagation(); requireLogin(editorButton ? '#/editor' : location.hash); }
      else if (editorButton && user()?.role !== 'admin') { event.preventDefault(); event.stopImmediatePropagation(); message('商城编辑需要管理员账号'); }
    }
  }, true);
  document.addEventListener('submit', event => {
    if (event.target.id !== 'auth-login-form') {
      const privateForm = /^(?:member-|studio-|service-)/.test(String(event.target.id || '')) || event.target.id === 'address-form';
      if (privateForm && !ensureSession()) { event.preventDefault(); event.stopImmediatePropagation(); requireLogin(String(event.target.id || '').startsWith('studio-') ? '#/editor' : '#/member'); }
      return;
    }
    event.preventDefault(); event.stopImmediatePropagation();
    if (ui.busy) return;
    const form = event.target;
    const data = new FormData(form), username = String(data.get('username') || '').trim(), password = String(data.get('password') || '');
    if (!username || !password) { ui.error = '请输入账号和密码'; redraw(); return; }
    const button = form.querySelector('[type="submit"]');
    if (button) { button.disabled = true; button.textContent = '正在登录…'; }
    login(username, password, data.get('remember') === 'on').catch(() => {});
  }, true);
  document.addEventListener('change', event => { if (event.target.name === 'remember' && event.target.closest('#auth-login-form')) ui.remember = event.target.checked; });
  document.addEventListener('keydown', event => { if (event.key === 'Escape') closeMenu(); });
  window.addEventListener('hashchange', closeMenu);
  window.addEventListener('lumo:session-expired', () => {
    const current = location.hash;
    clearSession('登录状态已失效，请重新登录');
    if (privateRoute(String(current || '#/').slice(2).split('/'))) { setReturn(current); location.hash = '#/login'; }
    redraw();
  });
  window.addEventListener('storage', event => {
    if (event.key === EVENT_KEY && parse(event.newValue)?.type === 'logout' && session && parse(event.newValue)?.userId === session.userId) { clearSession('账号已在另一个页面退出'); location.hash = '#/'; redraw(); }
    if (event.key === SESSION_KEY && event.storageArea === localStorage && event.newValue) {
      // Reload before an account switch so no module can retain another user's
      // transient addresses, balances or orders.
      const next = parse(event.newValue);
      if (next?.token && next.token !== session?.token) window.location.reload();
    }
  });
  window.addEventListener('focus', () => { if (session && session.expiresAt <= Date.now()) { clearSession('登录状态已过期，请重新登录'); redraw(); } });
  window.LumoAuth = { getItem, setItem, isLoggedIn, user, model, requireLogin, gate, ensureSession, afterRender: () => {}, refresh, login, logout, confirmLogout, renew,
    headerUtility: () => window.LumoAuthView?.headerUtility() || '', headerAccount: () => window.LumoAuthView?.headerAccount() || '' };
})();
