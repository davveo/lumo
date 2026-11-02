(function () {
  'use strict';
  const esc = (value) =>
    String(value ?? '').replace(
      /[&<>"']/g,
      (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c],
    );
  const call = (action, data = {}, id, publicRequest = false) =>
    window.LumoAPI.call('iam', action, { id, data }, { auth: !publicRequest });
  const field = (label, name, type = 'text', extra = '') =>
    `<label class="field">${label}<input name="${name}" type="${type}" ${extra} required></label>`;
  const result = '<p data-security-result role="status"></p>';
  const verification = `${field('验证码', 'code', 'text', 'inputmode="numeric" pattern="[0-9]{6}" maxlength="6" autocomplete="one-time-code"')}<p data-security-demo></p>`;
  const channel =
    '<label class="field">验证方式<select name="channel"><option value="phone">手机短信</option><option value="email">电子邮箱</option></select></label>';
  function recover() {
    modal(
      '找回密码',
      `<form id="security-reset-form">${field('账号', 'username', 'text', 'autocomplete="username"')}${channel}<button type="button" class="outline" data-security-action="send-reset">发送验证码</button><p>验证码仅发送至该账号已验证绑定的联系方式。</p>${verification}${field('新密码', 'new_password', 'password', 'minlength="8" maxlength="72" autocomplete="new-password"')}${result}<button class="primary" type="submit">验证并重设密码</button></form>`,
    );
  }
  async function open() {
    const data = await call('security_get');
    modal(
      '账号安全中心',
      `<section class="mem-panel"><h3>修改密码</h3><form id="security-password-form">${field('当前密码', 'current_password', 'password', 'autocomplete="current-password"')}${field('新密码', 'new_password', 'password', 'minlength="8" maxlength="72" autocomplete="new-password"')}${result}<button class="primary">保存并重新登录</button></form></section><section class="mem-panel"><h3>验证联系方式</h3><p>${(data.contacts || []).map((c) => `${c.channel === 'phone' ? '手机' : '邮箱'}：${esc(c.address)}`).join(' · ') || '尚未绑定找回密码的联系方式'}</p><form id="security-bind-form">${channel}${field('手机号码或邮箱', 'address')}${data.has_password ? field('当前密码', 'current_password', 'password', 'autocomplete="current-password"') : ''}<button type="button" class="outline" data-security-action="send-bind">发送验证码</button>${verification}${result}<button class="primary">确认绑定</button></form></section><section class="mem-panel"><h3>登录设备</h3>${(data.sessions || []).map((s) => `<div class="mem-setting-row"><div><strong>${esc(s.device || '登录设备')}${s.current ? '（当前）' : ''}</strong><p>${esc(s.ip || '')} · ${esc(new Date(s.created_at).toLocaleString())}</p></div><button class="outline" data-security-action="revoke" data-id="${esc(s.id)}" data-current="${!!s.current}">退出此设备</button></div>`).join('') || '<p>暂无有效会话</p>'}</section>`,
    );
  }
  async function signOut() {
    closeModal();
    await window.LumoAuth.logout();
    location.hash = '#/login';
  }
  async function perform(form, task) {
    if (form.dataset.busy) return;
    form.dataset.busy = 'true';
    const output = form.querySelector('[data-security-result]');
    output.textContent = '正在处理…';
    try {
      await task();
    } catch (error) {
      output.textContent = error.message;
    } finally {
      delete form.dataset.busy;
    }
  }
  document.addEventListener('click', async (event) => {
    const button = event.target.closest('[data-security-action]');
    if (!button) return;
    event.preventDefault();
    const action = button.dataset.securityAction,
      form = button.closest('form');
    try {
      if (action === 'open') {
        await open();
        return;
      }
      if (action === 'revoke') {
        await call('session_revoke', {}, button.dataset.id);
        if (button.dataset.current === 'true') await signOut();
        else await open();
        return;
      }
      if (action === 'send-bind' || action === 'send-reset') {
        await perform(form, async () => {
          const data = Object.fromEntries(new FormData(form));
          const value = await call(
            action === 'send-bind' ? 'security_send' : 'password_reset_request',
            data,
            undefined,
            action === 'send-reset',
          );
          form.dataset.challenge = value.challenge_id;
          form.querySelector('[data-security-demo]').textContent = value.demo_code
            ? '演示验证码：' + value.demo_code
            : '';
          form.querySelector('[data-security-result]').textContent = value.message || '验证码已发送';
        });
      }
    } catch (error) {
      toast(error.message);
    }
  });
  document.addEventListener(
    'submit',
    (event) => {
      const form = event.target;
      if (!['security-password-form', 'security-bind-form', 'security-reset-form'].includes(form.id)) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      void perform(form, async () => {
        const data = Object.fromEntries(new FormData(form));
        if (form.id === 'security-password-form') {
          await call('password_change', data);
          toast('密码已修改，请重新登录');
          await signOut();
          return;
        }
        if (!form.dataset.challenge) throw Error('请先发送验证码');
        data.challenge_id = form.dataset.challenge;
        if (form.id === 'security-bind-form') {
          await call('security_verify', data);
          await open();
          toast('联系方式已验证绑定');
        } else {
          await call('password_reset', data, undefined, true);
          closeModal();
          toast('密码已重设，请使用新密码登录');
          location.hash = '#/login';
        }
      });
    },
    true,
  );
  window.LumoSecurity = { open, recover };
})();
