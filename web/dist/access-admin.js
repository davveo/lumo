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
  function providerPanels(module) {
    const s = module.state;
    if (!s.ready || !s.providers.length) return '';
    return `<section class="admin-section"><h2>渠道参数配置</h2><p class="channel-form-note">分别保存每个渠道的参数，再启用对应登录或支付方式。密钥加密保存，已保存的密钥不会回显；留空保留，勾选清除后才会删除。</p><div class="channel-profile-list">${s.providers
      .map((p) => {
        const draft = s.providerDrafts[p.id] || {},
          open = s.editProvider === p.id,
          complete = p.parameters_complete ?? p.configured,
          demoPhone = p.id === 'phone' && p.configured && !p.values?.webhook_url;
        return `<article class="channel-profile"><div class="channel-profile-heading"><div><h3>${esc(p.name)}</h3><p>${demoPhone ? '演示验证码模式 · 尚未配置真实短信通道' : complete ? '参数已配置' : '待补充渠道参数'}${p.reason ? ' · ' + esc(p.reason) : ''}</p></div><button type="button" class="outline" data-access="provider" data-id="${esc(p.id)}" aria-expanded="${open}">${open ? '收起参数' : '配置参数'}</button></div>${
          open
            ? `<form data-provider-form data-id="${esc(p.id)}"><div class="admin-form">${(p.fields || [])
                .map((f) => {
                  const value = draft[f.key] ?? p.values?.[f.key] ?? '',
                    configured = !!p.secret_configured?.[f.key],
                    common = `name="${esc(f.key)}" data-provider-field data-provider="${esc(p.id)}" ${s.busy ? 'disabled' : ''}`;
                  const control =
                    f.type === 'textarea'
                      ? `<textarea ${common} rows="4" maxlength="32768" autocomplete="off" placeholder="${configured ? '已配置，留空保留原值' : esc(f.help || '')}">${esc(value)}</textarea>`
                      : f.type === 'select'
                        ? `<select ${common}><option value="">请选择</option>${(f.options || []).map((v) => `<option value="${esc(v)}" ${v === value ? 'selected' : ''}>${esc(v === 'production' ? '生产环境' : v === 'sandbox' ? '沙箱环境' : v)}</option>`).join('')}</select>`
                        : `<input ${common} type="${f.secret ? 'password' : f.type === 'url' ? 'url' : 'text'}" value="${esc(value)}" maxlength="32768" autocomplete="${f.secret ? 'new-password' : 'off'}" placeholder="${configured ? '已配置，留空保留原值' : ''}">`;
                  return `<label class="field ${f.type === 'textarea' ? 'wide' : ''}">${esc(f.label)}${f.required ? ' · 必需' : ''}${control}<small>${esc(f.help || '')}${f.secret ? ' · ' + (configured ? '已保存密钥' : '尚未保存密钥') : ''}</small></label>${f.secret && configured ? `<label class="channel-secret-clear"><input type="checkbox" name="clear_${esc(f.key)}" data-provider-clear="${esc(f.key)}" data-provider="${esc(p.id)}" ${s.providerClears[p.id]?.[f.key] ? 'checked' : ''}>清除${esc(f.label)}</label>` : ''}`;
                })
                .join(
                  '',
                )}</div><label class="field">修改原因<input name="reason" data-provider-reason data-provider="${esc(p.id)}" maxlength="240" required value="${esc(s.providerReasons[p.id] || '')}" placeholder="如变更应用密钥、更新回调域名"></label><div class="admin-form-footer"><span>参数版本 ${Number(p.version) || 0}${p.source === 'environment' ? ' · 当前读取环境配置' : ''}</span><button class="primary" ${s.busy || p.secure_storage_ready === false ? 'disabled' : ''}>保存${esc(p.name)}参数</button></div></form>`
            : ''
        }</article>`;
      })
      .join('')}</div></section>`;
  }
  function create(key) {
    if (modules.has(key)) return modules.get(key);
    const definition = definitions[key];
    const m = U.controller(
      'access-' + key,
      () => ({
        ready: false,
        busy: false,
        error: '',
        config: {},
        version: 0,
        capabilities: [],
        providers: [],
        providerDrafts: {},
        providerReasons: {},
        providerClears: {},
        editProvider: '',
        reason: '',
      }),
      async (module) => {
        const [data, profiles] = await Promise.all([
          module.adapter.call(definition.service, C.Action.PolicyGet, {}),
          module.adapter.call(definition.service, C.Action.ProviderGet, {}),
        ]);
        return {
          config: data.config || {},
          version: Number(data.version || 0),
          capabilities: data.capabilities || [],
          providers: profiles.items || [],
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
                )}</div>${key === 'loginSettings' ? `<div class="admin-form"><label class="field">验证码有效期（秒）<input type="number" min="60" max="600" step="1" required name="otp_ttl_seconds" value="${esc(s.config.otp_ttl_seconds)}"></label><label class="field">验证码发送间隔（秒）<input type="number" min="30" max="300" step="1" required name="otp_cooldown_seconds" value="${esc(s.config.otp_cooldown_seconds)}"></label></div>` : ''}<label class="field">修改原因<input name="reason" data-access-reason value="${esc(s.reason)}" required maxlength="240" placeholder="如暂停渠道维护、开放手机登录"></label><div class="admin-form-footer"><span>当前版本 ${s.version}</span><button class="primary" ${s.busy ? 'disabled' : ''}>保存配置</button></div></form>`) +
          providerPanels(module)
        );
      },
    );
    modules.set(key, m);
    document.addEventListener('click', async (event) => {
      const el = event.target.closest('[data-access]');
      if (!el || !m.active() || m.state.busy) return;
      if (el.dataset.access === 'refresh') await m.refresh();
      if (el.dataset.access === 'provider') {
        m.state.editProvider = m.state.editProvider === el.dataset.id ? '' : el.dataset.id;
        m.draw();
      }
    });
    document.addEventListener('input', (event) => {
      if (!m.active()) return;
      const s = m.state,
        el = event.target;
      if (el.matches('[data-access-reason]')) s.reason = el.value;
      if (el.matches('[data-provider-field]')) {
        (s.providerDrafts[el.dataset.provider] ||= {})[el.name] = el.value;
      }
      if (el.matches('[data-provider-reason]')) s.providerReasons[el.dataset.provider] = el.value;
      if (el.matches('[data-provider-clear]')) {
        (s.providerClears[el.dataset.provider] ||= {})[el.dataset.providerClear] = el.checked;
      }
      if (el.matches('[data-access-form] input') && el.name !== 'reason')
        s.config[el.name] = el.type === 'checkbox' ? el.checked : Number(el.value);
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active()) return;
      if (event.target.matches('[data-provider-form]')) {
        event.preventDefault();
        if (m.state.busy) return;
        const form = event.target,
          id = form.dataset.id,
          s = m.state,
          p = s.providers.find((row) => row.id === id),
          f = new FormData(form),
          values = {};
        if (!p) return;
        for (const field of p.fields || []) values[field.key] = String(f.get(field.key) || '');
        const clear_secrets = (p.fields || [])
            .filter((field) => field.secret && f.get('clear_' + field.key) === 'on')
            .map((field) => field.key),
          reason = String(f.get('reason') || '').trim();
        if (!reason) {
          s.error = '请填写渠道参数修改原因';
          m.draw();
          return;
        }
        s.providerDrafts[id] = values;
        s.providerReasons[id] = reason;
        if (
          await m.run(
            () =>
              m.adapter.call(definition.service, C.Action.ProviderUpsert, {
                id,
                data: { values, clear_secrets, version: p.version, reason },
              }),
            () => {},
          )
        ) {
          delete s.providerDrafts[id];
          delete s.providerReasons[id];
          delete s.providerClears[id];
          m.adapter.notify?.('渠道参数已保存');
          await m.refresh();
        }
        return;
      }
      if (!event.target.matches('[data-access-form]')) return;
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
