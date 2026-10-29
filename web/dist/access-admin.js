(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc } = U;
  const modules = new Map();
  const definitions = {
    paymentSettings: {
      service: C.Service.Payment,
      title: '支付方式',
      note: '通过开关控制商城可选择的支付渠道。关闭后会拦截新的支付请求，已成功的支付记录保留。',
      switches: [
        ['demo_enabled', '演示支付', 'demo'],
        ['wechat_enabled', '微信支付', 'wechat'],
        ['alipay_enabled', '支付宝', 'alipay'],
        ['card_enabled', '银行卡', 'card'],
      ],
    },
    loginSettings: {
      service: C.Service.IAM,
      title: '登录方式',
      note: '配置会员登录入口，商城按已启用且配置完整的渠道展示。管理员后台保留独立的账号密码登录。',
      switches: [
        ['password_enabled', '账号密码', 'password'],
        ['phone_enabled', '手机号 + 验证码', 'phone'],
        ['wechat_enabled', '微信登录', 'wechat'],
        ['qq_enabled', 'QQ登录', 'qq'],
        ['allow_registration', '允许已验证的新用户注册', 'registration'],
      ],
    },
  };
  function create(key) {
    if (modules.has(key)) return modules.get(key);
    const definition = definitions[key];
    const m = U.controller(
      'access-' + key,
      () => ({ ready: false, busy: false, error: '', config: {}, version: 0, capabilities: [], reason: '' }),
      async (module) => {
        const data = await module.adapter.call(definition.service, C.Action.PolicyGet, {});
        return {
          config: data.config || {},
          version: Number(data.version || 0),
          capabilities: data.capabilities || [],
          ready: true,
        };
      },
      (module) => {
        const s = module.state;
        return (
          U.heading(
            definition.title,
            definition.note,
            `<button class="outline" data-access="refresh" ${s.busy ? 'disabled' : ''}>刷新</button>`,
          ) +
          (!s.ready
            ? window.LumoSkeleton.render('form', { count: 5 })
            : `<form data-access-form class="admin-section"><div class="access-switches">${definition.switches
                .map(([name, label, id]) => {
                  const capability = s.capabilities.find((row) => row.id === id),
                    missing = capability && capability.configured === false;
                  return `<label class="access-setting"><span><strong>${esc(label)}</strong><small>${missing ? esc(capability.reason || '当前渠道尚未完成配置') : name === 'allow_registration' ? '首次通过验证码或第三方验证的用户可创建会员账号。' : '保存后用于新的登录或支付请求。'}</small></span><input type="checkbox" role="switch" name="${name}" ${s.config[name] ? 'checked' : ''} ${s.busy || missing ? 'disabled' : ''} aria-label="${esc(label)}"></label>`;
                })
                .join(
                  '',
                )}</div>${key === 'loginSettings' ? `<div class="admin-form"><label class="field">验证码有效期（秒）<input type="number" min="60" max="600" step="1" required name="otp_ttl_seconds" value="${esc(s.config.otp_ttl_seconds)}"></label><label class="field">验证码发送间隔（秒）<input type="number" min="30" max="300" step="1" required name="otp_cooldown_seconds" value="${esc(s.config.otp_cooldown_seconds)}"></label></div>` : ''}<label class="field">修改原因<input name="reason" data-access-reason value="${esc(s.reason)}" required maxlength="240" placeholder="如暂停渠道维护、开放手机登录"></label><div class="admin-form-footer"><span>当前版本 ${s.version}</span><button class="primary" ${s.busy ? 'disabled' : ''}>保存配置</button></div></form>`)
        );
      },
    );
    modules.set(key, m);
    document.addEventListener('click', async (event) => {
      const el = event.target.closest('[data-access]');
      if (!el || !m.active() || m.state.busy) return;
      if (el.dataset.access === 'refresh') await m.refresh();
    });
    document.addEventListener('input', (event) => {
      if (m.active() && event.target.matches('[data-access-reason]')) m.state.reason = event.target.value;
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active() || !event.target.matches('[data-access-form]')) return;
      event.preventDefault();
      if (m.state.busy) return;
      const data = new FormData(event.target),
        s = m.state,
        config = { ...s.config };
      for (const [name] of definition.switches) config[name] = data.get(name) === 'on';
      if (key === 'loginSettings') {
        config.otp_ttl_seconds = Number(data.get('otp_ttl_seconds'));
        config.otp_cooldown_seconds = Number(data.get('otp_cooldown_seconds'));
      }
      const reason = String(data.get('reason') || '').trim();
      if (!reason) {
        s.error = '请填写修改原因';
        m.draw();
        return;
      }
      s.config = config;
      s.reason = reason;
      if (
        await m.run(
          () =>
            m.adapter.call(definition.service, C.Action.PolicyUpsert, { data: { config, version: s.version, reason } }),
          () => {},
        )
      ) {
        m.adapter.notify?.('配置已生效');
        s.reason = '';
        await m.refresh();
      }
    });
    return m;
  }
  window.LumoAccessAdmin = { create };
})();
