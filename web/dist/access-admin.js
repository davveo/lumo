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
  function providerForm(p, s) {
    const draft = s.providerDrafts[p.id] || {};
    return `<form data-provider-form data-id="${esc(p.id)}" class="admin-form-stack"><div class="admin-form">${(
      p.fields || []
    )
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
      )}</div><label class="field">修改原因<input name="reason" data-provider-reason data-provider="${esc(p.id)}" maxlength="240" required value="${esc(s.providerReasons[p.id] || '')}" placeholder="如变更应用密钥、更新回调域名"></label><div class="admin-form-footer"><span>参数版本 ${Number(p.version) || 0}${p.source === 'environment' ? ' · 当前读取环境配置' : ''}</span><button class="primary" ${s.busy || p.secure_storage_ready === false ? 'disabled' : ''}>保存${esc(p.name)}参数</button></div></form>`;
  }
  function providerPanels(module, key) {
    const s = module.state;
    if (!s.providers.length) return U.empty('暂无可配置的渠道参数', '当前方式不需要额外参数，或渠道尚未提供配置。');
    return `<section class="admin-section access-provider-section"><div class="access-provider-tabs" role="tablist" aria-label="${esc(definitions[key].title)}渠道">${s.providers
      .map((p) => {
        const selected = s.editProvider === p.id;
        return `<button type="button" class="access-provider-tab" role="tab" id="${key}-provider-tab-${esc(p.id)}" aria-controls="${key}-provider-panel-${esc(p.id)}" aria-selected="${selected}" tabindex="${selected ? 0 : -1}" data-access="provider" data-id="${esc(p.id)}" ${s.busy ? 'disabled' : ''}><span class="access-provider-dot ${(p.parameters_complete ?? p.configured) ? 'is-complete' : ''}" aria-hidden="true"></span>${esc(p.name)}</button>`;
      })
      .join('')}</div>${s.providers
      .map((p) => {
        const selected = s.editProvider === p.id,
          complete = p.parameters_complete ?? p.configured,
          demoPhone = p.id === 'phone' && p.configured && !p.values?.webhook_url;
        return `<div role="tabpanel" id="${key}-provider-panel-${esc(p.id)}" aria-labelledby="${key}-provider-tab-${esc(p.id)}" class="access-parameter-panel" ${selected ? '' : 'hidden'}>${selected ? `<div class="access-provider-heading"><div><h2>${esc(p.name)}参数</h2><p>${demoPhone ? '演示验证码模式 · 尚未配置真实短信通道' : complete ? '参数已配置' : '待补充渠道参数'}${p.reason ? ' · ' + esc(p.reason) : ''}</p></div><span class="admin-state ${complete ? 'success' : 'pending'}">${complete ? '已配置' : '待配置'}</span></div><p class="channel-form-note">保存当前渠道参数后，再到“方式开关”启用。已保存的密钥不会回显，留空保留原值；勾选清除后才会删除。</p>${providerForm(p, s)}` : ''}</div>`;
      })
      .join('')}</section>`;
  }
  function sectionTabs(key, s) {
    return `<div class="access-tabs" role="tablist" aria-label="${esc(definitions[key].title)}配置分类">${[
      ['methods', '方式开关'],
      ['providers', '渠道参数'],
    ]
      .map(
        ([id, title]) =>
          `<button type="button" role="tab" id="${key}-tab-${id}" aria-controls="${key}-panel-${id}" aria-selected="${s.section === id}" tabindex="${s.section === id ? 0 : -1}" data-access="section" data-id="${id}" ${s.busy ? 'disabled' : ''}>${title}</button>`,
      )
      .join('')}</div>`;
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
        section: 'methods',
        policyDirty: false,
        reason: '',
      }),
      async (module) => {
        const [data, profiles] = await Promise.all([
          module.adapter.call(definition.service, C.Action.PolicyGet, {}),
          module.adapter.call(definition.service, C.Action.ProviderGet, {}),
        ]);
        return {
          config: module.state.policyDirty ? module.state.config : data.config || {},
          version: module.state.policyDirty ? module.state.version : Number(data.version || 0),
          capabilities: data.capabilities || [],
          providers: profiles.items || [],
          editProvider: (profiles.items || []).some((p) => p.id === module.state.editProvider)
            ? module.state.editProvider
            : profiles.items?.[0]?.id || '',
          ready: true,
        };
      },
      (module) => {
        const s = module.state;
        return (
          U.heading(
            definition.title,
            definition.note,
            `<a class="outline" href="index.html#/preview/${key === 'loginSettings' ? 'login' : 'payment'}" target="_blank" rel="noopener">预览前台效果 ↗</a><button class="outline" data-access="refresh" ${s.busy ? 'disabled' : ''}>刷新</button>`,
          ) +
          (!s.ready
            ? window.LumoSkeleton.render('form', { count: 5 })
            : `${sectionTabs(key, s)}<div role="tabpanel" id="${key}-panel-methods" aria-labelledby="${key}-tab-methods" ${s.section === 'methods' ? '' : 'hidden'}>${
                s.section === 'methods'
                  ? `<form data-access-form class="admin-section admin-form-stack"><div class="access-switches">${definition.switches
                      .map(([name, label, id]) => {
                        const capability = s.capabilities.find((row) => row.id === id),
                          missing = capability && capability.configured === false;
                        return `<label class="access-setting"><span><strong>${esc(label)}</strong><small>${missing ? esc(capability.reason || '当前渠道尚未完成配置') : name === 'allow_registration' ? '首次通过验证码或第三方验证的用户可创建会员账号。' : '保存后用于新的登录或支付请求。'}</small></span><input type="checkbox" role="switch" name="${name}" ${s.config[name] ? 'checked' : ''} ${s.busy || missing ? 'disabled' : ''} aria-label="${esc(label)}"></label>`;
                      })
                      .join(
                        '',
                      )}</div>${key === 'loginSettings' ? `<div class="admin-form"><label class="field">验证码有效期（秒）<input type="number" min="60" max="600" step="1" required name="otp_ttl_seconds" value="${esc(s.config.otp_ttl_seconds)}"></label><label class="field">验证码发送间隔（秒）<input type="number" min="30" max="300" step="1" required name="otp_cooldown_seconds" value="${esc(s.config.otp_cooldown_seconds)}"></label></div>` : ''}<label class="field">修改原因<input name="reason" data-access-reason value="${esc(s.reason)}" required maxlength="240" placeholder="如暂停渠道维护、开放手机登录"></label><div class="admin-form-footer"><span>当前版本 ${s.version}</span><button class="primary" ${s.busy ? 'disabled' : ''}>保存配置</button></div></form>`
                  : ''
              }</div><div role="tabpanel" id="${key}-panel-providers" aria-labelledby="${key}-tab-providers" ${s.section === 'providers' ? '' : 'hidden'}>${s.section === 'providers' ? providerPanels(module, key) : ''}</div>`)
        );
      },
    );
    modules.set(key, m);
    function activateTab(type, id) {
      const s = m.state;
      if (type === 'section' && ['methods', 'providers'].includes(id)) s.section = id;
      else if (type === 'provider' && s.providers.some((p) => p.id === id)) {
        s.section = 'providers';
        s.editProvider = id;
      } else return;
      m.draw();
      const selected = [
        ...(document.getElementById('access-' + key)?.querySelectorAll('[role="tab"][data-access]') || []),
      ].find((el) => el.dataset.access === type && el.dataset.id === id);
      selected?.focus({ preventScroll: true });
      const list = selected?.closest('[role="tablist"]');
      if (list) {
        const left = selected.getBoundingClientRect().left - list.getBoundingClientRect().left;
        if (left < 0) list.scrollLeft += left;
        else if (left + selected.offsetWidth > list.clientWidth)
          list.scrollLeft += left + selected.offsetWidth - list.clientWidth;
      }
    }
    document.addEventListener('click', async (event) => {
      const el = event.target.closest('[data-access]');
      if (!el || !m.active() || m.state.busy) return;
      if (el.dataset.access === 'refresh') {
        const s = m.state;
        const dirty =
          s.policyDirty ||
          s.reason ||
          Object.keys(s.providerDrafts).length ||
          Object.keys(s.providerReasons).length ||
          Object.keys(s.providerClears).length;
        if (
          dirty &&
          window.LumoAdminFeedback &&
          !(await window.LumoAdminFeedback.confirm('刷新将放弃当前页面未保存的开关和渠道参数，重新读取已保存配置。', {
            title: '重新读取配置',
            confirmLabel: '放弃修改并刷新',
          }))
        )
          return;
        s.policyDirty = false;
        s.reason = '';
        s.providerDrafts = {};
        s.providerReasons = {};
        s.providerClears = {};
        await m.refresh();
      } else activateTab(el.dataset.access, el.dataset.id);
    });
    document.addEventListener('keydown', (event) => {
      const el = event.target.closest('[role="tab"][data-access]');
      if (!el || !m.active() || m.state.busy || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return;
      const type = el.dataset.access,
        ids = type === 'section' ? ['methods', 'providers'] : m.state.providers.map((p) => p.id),
        index = ids.indexOf(el.dataset.id);
      if (index < 0) return;
      event.preventDefault();
      const next =
        event.key === 'Home'
          ? 0
          : event.key === 'End'
            ? ids.length - 1
            : (index + (event.key === 'ArrowRight' ? 1 : -1) + ids.length) % ids.length;
      activateTab(type, ids[next]);
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
      if (el.matches('[data-access-form] input') && el.name !== 'reason') {
        s.config[el.name] = el.type === 'checkbox' ? el.checked : Number(el.value);
        s.policyDirty = true;
      }
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
      s.policyDirty = true;
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
        s.policyDirty = false;
        await m.refresh();
      }
    });
    return m;
  }
  window.LumoAccessAdmin = { create };
})();
