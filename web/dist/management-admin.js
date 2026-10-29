(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI;
  const { esc, money, date, pair } = U,
    modules = new Map(),
    size = 10;
  const copy = (value) => JSON.parse(JSON.stringify(value));
  const label = {
    [C.ReconciliationStatus.Queued]: '排队中',
    [C.ReconciliationStatus.Running]: '核对中',
    [C.ReconciliationStatus.Failed]: '失败',
    [C.ReconciliationStatus.Matched]: '一致',
    [C.ReconciliationStatus.Differences]: '存在差异',
    [C.OutboxState.Pending]: '待发送',
    [C.OutboxState.Publishing]: '发送中',
    [C.ContentStatus.Published]: '已发布',
    [C.InboxState.Processing]: '处理中',
    [C.InboxState.Done]: '完成',
    [C.InboxState.Dead]: '已停止重试',
    [C.InboxState.Retry]: '等待重试',
    [C.ContentStatus.Draft]: '草稿',
    [C.ContentStatus.Scheduled]: '定时发布',
    [C.ContentStatus.Paused]: '已下线',
    [C.GroupStatus.Open]: '进行中',
    [C.GroupStatus.Formed]: '已成团',
    [C.FlashRequestStatus.Succeeded]: '成功',
  };
  const names = {
    special: '秒杀与拼团',
    stockwatch: '库存预警与流水',
    points: '会员与积分',
    shipping: '配送与运费',
    messages: '通知模板',
    recommendation: '推荐与搜索',
    tasks: '异常任务',
    audit: '账号操作审计',
    content: '内容管理',
    reconciliation: '账期对账',
  };
  const modes = {
    special: [
      ['sessions', '秒杀场次'],
      ['rules', '拼团规则'],
      ['quotas', '活动配额'],
      ['requests', '秒杀请求'],
      ['teams', '拼团记录'],
    ],
    stockwatch: [
      ['stocks', '库存预警'],
      ['movements', '出入库流水'],
      ['adjustments', '盘点记录'],
    ],
    points: [
      ['accounts', '会员账户'],
      ['pointsRules', '积分规则'],
      ['memberRules', '成长规则'],
    ],
    shipping: [['shipping', '配送模板']],
    messages: [['templates', '业务通知']],
    recommendation: [
      ['recommendation', '推荐运营'],
      ['search', '搜索策略'],
    ],
    tasks: [
      ['outbox', '待发送事件'],
      ['inbox', '消费状态'],
      ['recovery', '重放任务'],
      ['audit', '重试审计'],
    ],
    audit: [['accounts', '账号启停记录']],
    content: [
      ['notice', '商城公告'],
      ['faq', '帮助与常见问题'],
    ],
    reconciliation: [['reports', '对账任务']],
  };
  const notes = {
    special: '先维护专用活动配额，再绑定场次或拼团规则；已有订单与团保留原有规则。',
    stockwatch: '按真实可售数量筛选全部库存，库存阈值与数量分开管理。',
    points: '查询会员资料、积分资产和流水，规则变更仅影响后续报价与订单。',
    shipping: '服务端根据地区、商品金额和件数计算运费，并在订单提交时核验。',
    messages: '按业务事件配置站内信内容，保存后用于新的通知。',
    recommendation: '配置实际参与排序的权重、精选商品与屏蔽项；屏蔽和已购排除优先。',
    tasks: '各服务只查询自己的事件记录；完成和仍在处理的事件受保护。',
    audit: '查看账号启停的操作人、原因与时间。',
    content: '管理草稿、发布、定时上线和下线；保存时校验内容版本。',
    reconciliation: '按账期异步核对内部支付与订单，跟踪进度、差异与处理备注。',
  };
  const field = (key, name, type = 'text', extra = {}) => ({ key, name, type, ...extra });
  const moneyValue = (cents) => (Number(cents || 0) / 100).toFixed(2);
  const utcInput = (value) =>
    value
      ? new Date(new Date(value).getTime() - new Date(value).getTimezoneOffset() * 60000).toISOString().slice(0, 16)
      : '';
  const today = () => new Date().toLocaleDateString('en-CA', { timeZone: 'Asia/Shanghai' });
  function source(s) {
    if (s.key === 'special')
      return {
        sessions: [C.Service.Flash, C.Action.AdminList],
        rules: [C.Service.Group, C.Action.RulesAdmin],
        quotas: [C.Service.Campaign, C.Action.AdminList],
        requests: [C.Service.Flash, C.Action.RequestsAdmin],
        teams: [C.Service.Group, C.Action.AdminList],
      }[s.mode];
    if (s.key === 'stockwatch')
      return [
        C.Service.Inventory,
        s.mode === 'stocks' ? C.Action.AdminList : s.mode === 'movements' ? C.Action.Ledger : C.Action.Adjustments,
      ];
    if (s.key === 'points')
      return s.mode === 'accounts'
        ? [C.Service.IAM, C.Action.AdminList]
        : [s.mode === 'pointsRules' ? C.Service.Points : C.Service.Member, C.Action.PolicyGet];
    return {
      shipping: [C.Service.Logistics, C.Action.PolicyGet],
      messages: [C.Service.Notification, C.Action.TemplatesAdmin],
      recommendation: [s.mode === 'search' ? C.Service.Search : C.Service.Recommendation, C.Action.PolicyGet],
      tasks: [C.Service.Management, C.Action.Tasks],
      audit: [C.Service.IAM, C.Action.AuditList],
      content: [C.Service.Content, C.Action.AdminList],
      reconciliation: [C.Service.Reconciliation, C.Action.AdminList],
    }[s.key];
  }
  function query(s, page = s.page, pageSize = size) {
    const data = { ...s.filters, page, page_size: pageSize };
    if (s.key === 'special' && s.mode === 'quotas') data.kind = s.quotaKind;
    if (s.key === 'content') data.kind = s.mode;
    if (s.key === 'points' && s.mode === 'accounts') data.status = C.Role.User;
    if (s.key === 'tasks') {
      data.service = s.service;
      data.kind = s.mode;
    }
    return { ...(s.key === 'stockwatch' && s.mode !== 'stocks' && s.filters.sku ? { id: s.filters.sku } : {}), data };
  }
  const policyMode = (s) =>
    s.key === 'shipping' || s.key === 'recommendation' || (s.key === 'points' && s.mode !== 'accounts');
  function formFields(d) {
    if (d.type === 'session')
      return [
        field('id', '场次编号'),
        field('name', '场次名称'),
        field('sku_id', '商品 SKU'),
        field('activity_id', '秒杀配额编号'),
        field('active', '场次状态', 'bool'),
        field('starts_at', '开始时间', 'time'),
        field('ends_at', '结束时间', 'time'),
      ];
    if (d.type === 'rule')
      return [
        field('id', '规则编号'),
        field('name', '规则名称'),
        field('sku_id', '指定 SKU（留空适用全部在售商品）', 'text', { optional: true }),
        field('activity_id', '拼团配额编号'),
        field('required', '成团人数', 'number', { min: 2, max: 10 }),
        field('duration_minutes', '有效时长（分钟）', 'number', { min: 1, max: 10080 }),
        field('active', '规则状态', 'bool'),
        field('starts_at', '开始时间', 'time'),
        field('ends_at', '结束时间', 'time'),
      ];
    if (d.type === 'quota')
      return [
        field('id', '配额编号'),
        field('name', '活动名称'),
        field('kind', '活动类型', 'select', {
          options: [
            [C.CampaignKind.Flash, '秒杀'],
            [C.CampaignKind.Group, '拼团'],
          ],
        }),
        field('description', '活动说明', 'textarea', { optional: true }),
        field('discount_cents', '单件优惠（元）', 'money'),
        field('quota', '总活动额度（件）', 'number', { min: 1, max: 1000000000 }),
        field('active', '启用状态', 'bool'),
      ];
    if (d.type === 'threshold')
      return [field('threshold', '低库存预警阈值（件）', 'number', { min: 0, max: 1000000000 })];
    if (d.type === 'adjustPoints')
      return [field('amount', '积分变动（增加填正数，扣减填负数）', 'number', { min: -1000000, max: 1000000 })];
    if (d.type === 'profile')
      return [field('display_name', '会员昵称'), field('avatar', '头像 HTTPS 地址', 'text', { optional: true })];
    if (d.type === 'run') return [field('from', '账期开始', 'date'), field('to', '账期结束', 'date')];
    if (d.type === 'retry' || d.type === 'resume') return [];
    if (d.type === 'template')
      return [field('title', '通知标题'), field('body', '通知正文', 'textarea'), field('enabled', '发送状态', 'bool')];
    if (d.type === 'article')
      return [
        field('id', '内容编号'),
        field('kind', '内容类型', 'select', {
          options: [
            [C.ContentKind.Notice, '商城公告'],
            [C.ContentKind.Faq, '常见问题'],
          ],
        }),
        field('title', '内容标题'),
        field('body', '正文', 'textarea'),
        field('status', '发布状态', 'select', {
          options: [
            [C.ContentStatus.Draft, '草稿'],
            [C.ContentStatus.Published, '发布'],
            [C.ContentStatus.Scheduled, '定时发布'],
            [C.ContentStatus.Paused, '下线'],
          ],
        }),
        field('publish_at', '上线时间', 'time'),
        field('unpublish_at', '下线时间', 'time'),
      ];
    if (d.type === 'pointsRules')
      return [
        field('checkin_points', '每日签到积分', 'number', { min: 0, max: 10000 }),
        field('deduction_bps', '抵扣上限（基点，1000 = 10%）', 'number', { min: 0, max: 10000 }),
        field('reward_per_yuan', '每消费1元奖励积分', 'number', { min: 0, max: 100 }),
      ];
    if (d.type === 'memberRules')
      return [
        field('gold_growth', '黄金会员成长值门槛', 'number', { min: 1, max: 1000000000 }),
        field('growth_spend_cents', '每获得1成长值需消费（元）', 'money', { min: 1 }),
      ];
    if (d.type === 'shipping')
      return [
        field('name', '配送模板名称'),
        field('enabled', '配送状态', 'bool'),
        field('mode', '计费方式', 'select', {
          options: [
            ['flat', '每单固定运费'],
            ['per_item', '首件加续件'],
          ],
        }),
        field('base_fee_cents', '基础 / 首件运费（元）', 'money'),
        field('additional_fee_cents', '每续件运费（元）', 'money'),
        field('free_threshold_cents', '免邮门槛（元，0表示不按金额免邮）', 'money'),
        field('allowed_regions', '可配送地区（留空表示不限）', 'ids', { optional: true }),
        field('blocked_regions', '不配送地区', 'ids', { optional: true }),
        field('regions', '地区特殊运费', 'regions', { optional: true }),
      ];
    if (d.type === 'recommendation')
      return [
        field('favorite_weight', '收藏权重', 'decimal'),
        field('compare_weight', '对比权重', 'decimal'),
        field('history_weight', '浏览权重', 'decimal'),
        field('purchase_weight', '已购偏好权重', 'decimal'),
        field('search_weight', '搜索兴趣权重', 'decimal'),
        field('popularity_weight', '销量权重', 'decimal'),
        field('freshness_weight', '新品权重', 'decimal'),
        field('personal_weight', '个人兴趣权重', 'decimal'),
        field('similar_weight', '相似商品权重', 'decimal'),
        field('pinned_ids', '首页精选商品编号（最多20个）', 'ids', { optional: true }),
        field('excluded_ids', '推荐屏蔽商品编号（最多500个）', 'ids', { optional: true }),
      ];
    if (d.type === 'search')
      return [
        field('default_sort', '默认搜索排序', 'select', {
          options: [
            ['', '综合'],
            [C.CatalogSort.Newest, '新品'],
            [C.CatalogSort.Sales, '销量'],
            [C.CatalogSort.PriceAsc, '价格升序'],
            [C.CatalogSort.PriceDesc, '价格降序'],
          ],
        }),
        field('max_results', '单次候选数量上限', 'number', { min: 1, max: 100 }),
        field('excluded_ids', '不参与搜索的商品编号', 'ids', { optional: true }),
        field('hidden_brands', '不参与搜索的品牌', 'ids', { optional: true }),
      ];
    return [];
  }
  function control(f, d, busy) {
    let value = d.values[f.key] ?? '',
      attrs = `name="${f.key}" data-mg-field="${f.key}" ${busy ? 'disabled' : ''} ${f.optional || ['time', 'ids', 'regions'].includes(f.type) ? '' : 'required'}`;
    if (f.key === 'id' && d.original) attrs += ' readonly';
    if (f.type === 'time') value = utcInput(value);
    if (f.type === 'money') value = moneyValue(value);
    if (f.type === 'ids') value = Array.isArray(value) ? value.join('\n') : value;
    if (f.type === 'regions')
      value = Array.isArray(value)
        ? value
            .map((r) =>
              [
                r.region,
                moneyValue(r.base_fee_cents),
                moneyValue(r.additional_fee_cents),
                moneyValue(r.free_threshold_cents),
              ].join(' | '),
            )
            .join('\n')
        : value;
    let input;
    if (f.type === 'select' || f.type === 'bool')
      input = `<select ${attrs}>${(
        f.options || [
          [true, '启用'],
          [false, '停用'],
        ]
      )
        .map(
          ([v, name]) =>
            `<option value="${esc(v)}" ${String(value) === String(v) ? 'selected' : ''}>${esc(name)}</option>`,
        )
        .join('')}</select>`;
    else if (['textarea', 'ids', 'regions'].includes(f.type))
      input = `<textarea ${attrs} rows="${f.type === 'textarea' ? 6 : 4}" maxlength="${f.type === 'textarea' ? 20000 : 12000}">${esc(value)}</textarea>`;
    else
      input = `<input ${attrs} type="${f.type === 'time' ? 'datetime-local' : f.type === 'date' ? 'date' : ['number', 'decimal'].includes(f.type) ? 'number' : 'text'}" value="${esc(value)}" ${['number', 'decimal'].includes(f.type) ? `min="${f.min ?? 0}" max="${f.max ?? 100}" step="${f.type === 'decimal' ? '0.01' : '1'}"` : ''} ${f.type === 'money' ? 'inputmode="decimal"' : ''} maxlength="240">`;
    return `<label class="field ${['textarea', 'ids', 'regions'].includes(f.type) ? 'wide' : ''}">${esc(f.name)}${input}${f.type === 'regions' ? '<small>每行：地区 | 首件运费（元） | 续件运费（元） | 免邮门槛（元）。先匹配的规则生效。</small>' : ''}${f.type === 'ids' ? '<small>一行一个，也可用逗号分隔。</small>' : ''}</label>`;
  }
  function draft(s, type, row = {}) {
    const id =
      (type === 'session' ? 'flash-' : type === 'rule' ? 'group-' : type === 'quota' ? 'quota-' : 'content-') +
      crypto.randomUUID().slice(0, 12);
    const defaults =
      {
        session: { id, name: '新秒杀场次', sku_id: '', activity_id: '', active: false },
        rule: { id, name: '新拼团规则', sku_id: '', activity_id: '', required: 2, duration_minutes: 30, active: false },
        quota: {
          id,
          name: '新活动配额',
          kind: s.quotaKind,
          description: '',
          discount_cents: 0,
          quota: 100,
          active: false,
        },
        article: { id, kind: s.mode, title: '', body: '', status: C.ContentStatus.Draft },
        run: { from: today(), to: today() },
        retry: {},
        resume: {},
      }[type] || {};
    return {
      type,
      values: { ...defaults, ...copy(row) },
      version: Number(row.version || 0),
      original: !!row.id,
      key: crypto.randomUUID(),
      reason: '',
      id: row.id || id,
    };
  }
  function cents(value) {
    if (!/^(?:0|[1-9]\d*)(?:\.\d{1,2})?$/.test(String(value))) throw Error('金额请填写非负数，最多两位小数');
    const [whole, fraction = ''] = String(value).split('.');
    const result = Number(whole) * 100 + Number(fraction.padEnd(2, '0'));
    if (!Number.isSafeInteger(result) || result > 1000000000) throw Error('金额超过允许范围');
    return result;
  }
  function readForm(form, d) {
    const data = new FormData(form),
      result = { ...d.values };
    for (const f of formFields(d)) {
      const raw = String(data.get(f.key) ?? '').trim();
      let value = raw;
      if (f.type === 'bool') value = raw === 'true';
      if (f.type === 'number' || f.type === 'decimal') {
        value = Number(raw);
        if (
          !Number.isFinite(value) ||
          (f.type === 'number' && !Number.isSafeInteger(value)) ||
          value < (f.min ?? 0) ||
          value > (f.max ?? 100)
        )
          throw Error(f.name + '超出允许范围');
      }
      if (f.type === 'money') value = cents(raw);
      if (f.type === 'time') {
        if (raw && !Number.isFinite(Date.parse(raw))) throw Error('请填写有效时间');
        value = raw ? new Date(raw).toISOString() : null;
      }
      if (f.type === 'ids')
        value = raw
          .split(/[\n,，]+/)
          .map((v) => v.trim())
          .filter(Boolean);
      if (f.type === 'regions')
        value = raw
          ? raw
              .split('\n')
              .filter((v) => v.trim())
              .map((line) => {
                const [region, base, extra, free, ...rest] = line.split('|').map((v) => v.trim());
                if (!region || free === undefined || rest.length) throw Error('地区运费每行需填写4项');
                return {
                  region,
                  base_fee_cents: cents(base),
                  additional_fee_cents: cents(extra),
                  free_threshold_cents: cents(free),
                };
              })
          : [];
      result[f.key] = value;
    }
    return { values: result, reason: String(data.get('reason') || '').trim() };
  }
  function templatePreview(d) {
    const render = (value) =>
      String(value || '')
        .replaceAll('{{business_id}}', 'order-example-001')
        .replaceAll('{{event_type}}', d.values.event_type || d.values.id || '')
        .replaceAll('{{amount}}', '199.00');
    return `<h3>站内信示例预览</h3><p class="admin-muted">示例编号与金额，不会发送通知。</p><strong>${esc(render(d.values.title))}</strong><p class="mg-preview-body">${esc(render(d.values.body))}</p>`;
  }
  function editor(s) {
    const d = s.draft;
    return `<form data-mg-save class="admin-section"><div class="admin-panel-heading"><h2>${esc({ session: '秒杀场次', rule: '拼团规则', quota: '专用活动配额', threshold: '预警阈值', adjustPoints: '人工积分补偿', profile: '会员资料', run: '创建账期对账任务', retry: '重试异常事件', resume: '恢复核对', template: '通知模板', article: '内容发布', pointsRules: '积分规则', memberRules: '会员成长规则', shipping: '配送模板', recommendation: '推荐运营', search: '搜索策略' }[d.type])}</h2><button type="button" class="outline" data-mg="cancel">返回</button></div>${['session', 'rule'].includes(d.type) ? '<p class="admin-muted">先在“活动配额”维护匹配的秒杀或拼团规则，再填写对应编号。变更不修改已产生订单与拼团。</p>' : ''}${d.type === 'template' ? '<p class="admin-muted">支持 {{business_id}}、{{event_type}}、{{amount}}。仅站内信，历史通知保留原文。</p>' : ''}${d.type === 'adjustPoints' ? `<p class="admin-inline-note">会员 ${esc(d.values.user_id)}，当前可用 ${esc(d.values.expected_available)}。增加的积分会先偿还已有欠账；冻结积分不会被人工扣减。</p>` : ''}${d.type === 'retry' ? `<p class="admin-inline-note">重试 ${esc(d.values.id)}，沿用原事件与消费幂等规则。不会直接修改订单、库存或资金状态。</p>` : ''}<div class="admin-form mg-fields">${formFields(
      d,
    )
      .map((f) => control(f, d, s.busy))
      .join(
        '',
      )}<label class="field wide">${d.type === 'run' ? '任务说明' : '操作原因'}<input name="reason" data-mg-reason value="${esc(d.reason)}" required maxlength="240" ${s.busy ? 'disabled' : ''}></label>${d.type === 'template' ? `<section data-mg-preview class="mg-template-preview wide">${templatePreview(d)}</section>` : ''}<div class="admin-form-footer"><span>${d.original ? '当前版本 ' + d.version : '保存到服务器后生效'}</span><button class="primary" ${s.busy ? 'disabled' : ''}>${s.busy ? '正在保存…' : d.type === 'run' ? '创建后台任务' : '确认保存'}</button></div></div></form>`;
  }
  const stateBadge = (value) =>
    `<span class="admin-state ${[C.ReconciliationStatus.Failed, C.InboxState.Dead, C.ReconciliationStatus.Differences].includes(value) ? 'danger' : [C.ReconciliationStatus.Running, C.ReconciliationStatus.Queued, C.InboxState.Retry].includes(value) ? 'pending' : 'neutral'}">${esc(label[value] || value || '—')}</span>`;
  function columns(s) {
    if (s.key === 'special')
      return {
        sessions: [
          ['场次 / 编号', (r) => `<strong>${esc(r.name)}</strong><code>${esc(r.id)}</code>`],
          ['商品 / 配额', (r) => `${esc(r.sku_id)}<small>${esc(r.activity_id)}</small>`],
          ['时间窗', (r) => `${date(r.starts_at)}<small>至 ${date(r.ends_at)}</small>`],
          ['状态', (r) => (r.active ? '启用' : '停用')],
        ],
        rules: [
          ['规则', (r) => `<strong>${esc(r.name)}</strong><code>${esc(r.id)}</code>`],
          ['适用商品', (r) => esc(r.sku_id || '全部在售商品')],
          ['成团与时长', (r) => `${esc(r.required)} 人 · ${esc(r.duration_minutes)} 分钟`],
          ['配额 / 状态', (r) => `${esc(r.activity_id)}<small>${r.active ? '启用' : '停用'}</small>`],
        ],
        quotas: [
          ['活动配额', (r) => `<strong>${esc(r.name)}</strong><code>${esc(r.id)}</code>`],
          ['类型 / 优惠', (r) => `${r.kind === C.CampaignKind.Flash ? '秒杀' : '拼团'} · ${money(r.discount_cents)}`],
          ['名额', (r) => `总 ${esc(r.quota)} · 预占 ${esc(r.held)} · 已用 ${esc(r.used)}`],
          ['状态', (r) => (r.active ? '启用' : '停用')],
        ],
        requests: [
          ['请求编号', (r) => esc(r.id)],
          ['会员 / 场次', (r) => `${esc(r.user_id)}<small>${esc(r.session_id)}</small>`],
          ['订单 / 结果', (r) => `${esc(r.order_id || '—')}<small>${esc(r.error || '')}</small>`],
          ['状态', (r) => stateBadge(r.status)],
        ],
        teams: [
          ['拼团编号', (r) => esc(r.id)],
          ['商品 / 规则', (r) => `${esc(r.sku_id)}<small>${esc(r.rule_id)}</small>`],
          ['进度', (r) => `${esc(r.paid)} / ${esc(r.required)} 人 · 已占 ${esc(r.occupied)}`],
          ['到期 / 状态', (r) => `${date(r.expires_at)}<small>${label[r.status] || esc(r.status)}</small>`],
        ],
      }[s.mode];
    if (s.key === 'stockwatch')
      return s.mode === 'stocks'
        ? [
            [
              '商品 / SKU',
              (r) => `<strong>${esc(s.products[r.sku_id]?.name || r.sku_id)}</strong><code>${esc(r.sku_id)}</code>`,
            ],
            [
              '在库 / 占用',
              (r) => `${esc(r.on_hand)}<small>预占 ${esc(r.reserved)} · 分配 ${esc(r.allocated)}</small>`,
            ],
            ['可售 / 阈值', (r) => `<strong>${esc(r.available)}</strong><small>预警阈值 ${esc(r.threshold)}</small>`],
          ]
        : s.mode === 'movements'
          ? [
              ['SKU', (r) => esc(r.sku_id)],
              ['业务凭证', (r) => esc(r.reference_id)],
              ['类型 / 数量', (r) => `${esc(r.kind)} · ${esc(r.quantity)}`],
              ['时间', (r) => date(r.created_at)],
            ]
          : [
              ['SKU / 操作人', (r) => `${esc(r.sku_id)}<small>${esc(r.actor_id)}</small>`],
              ['数量变化', (r) => `${esc(r.before)} → ${esc(r.after)}`],
              ['原因', (r) => esc(r.reason)],
              ['时间', (r) => date(r.created_at)],
            ];
    if (s.key === 'points')
      return [
        ['会员', (r) => `<strong>${esc(r.username)}</strong><code>${esc(r.id)}</code>`],
        ['状态', (r) => (r.active ? '正常' : '已停用')],
        ['创建时间', (r) => date(r.created_at)],
      ];
    if (s.key === 'messages')
      return [
        ['业务事件', (r) => esc(r.event_type)],
        ['通知标题', (r) => esc(r.title)],
        ['状态', (r) => (r.enabled ? '发送' : '停用')],
        ['版本', (r) => esc(r.version)],
      ];
    if (s.key === 'tasks')
      return s.mode === 'audit'
        ? [
            ['事件', (r) => esc(r.event_id)],
            ['操作人', (r) => esc(r.actor_id)],
            ['操作 / 原因', (r) => `${esc(r.kind)}<small>${esc(r.reason)}</small>`],
            ['时间', (r) => date(r.created_at)],
          ]
        : [
            ['事件 / 任务', (r) => `${esc(r.event_id || r.id)}<small>${esc(r.event_type || r.consumer || '')}</small>`],
            ['状态', (r) => stateBadge(r.state)],
            ['重试 / 跟踪', (r) => `${esc(r.attempts ?? '—')}<small>${esc(r.trace_id || r.error || '')}</small>`],
            ['时间', (r) => date(r.created_at || r.updated_at)],
          ];
    if (s.key === 'audit')
      return [
        ['会员', (r) => esc(r.user_id)],
        ['操作人', (r) => esc(r.actor_id)],
        ['变更', (r) => (r.active ? '启用账号' : '停用账号')],
        ['操作原因', (r) => esc(r.reason)],
        ['时间', (r) => date(r.created_at)],
      ];
    if (s.key === 'content')
      return [
        ['内容标题', (r) => `<strong>${esc(r.title)}</strong><code>${esc(r.id)}</code>`],
        ['摘要', (r) => esc(String(r.body || '').slice(0, 100))],
        ['状态', (r) => `${stateBadge(r.status)}<small>${r.visible ? '当前可见' : '当前不可见'}</small>`],
        ['发布 / 下线', (r) => `${date(r.publish_at)}<small>至 ${date(r.unpublish_at)}</small>`],
      ];
    return [
      [
        '任务 / 账期',
        (r) =>
          `<strong>${esc(r.id)}</strong><small>${esc(r.from || '历史内部核对')} ${r.to ? '至 ' + esc(r.to) : ''}</small>`,
      ],
      ['核对进度', (r) => `${esc(r.checked ?? 0)} / ${esc(r.total ?? 0)}`],
      ['差异', (r) => esc(r.difference_count ?? 0)],
      ['状态', (r) => `${stateBadge(r.status)}<small>${esc(r.error || '')}</small>`],
    ];
  }
  function actions(s, r) {
    const b = (action, text) =>
      `<button class="outline" data-mg="${action}" data-id="${esc(r.id || r.sku_id)}">${text}</button>`;
    if (s.key === 'special')
      return (
        { sessions: b('edit', '编辑场次'), rules: b('edit', '编辑规则'), quotas: b('edit', '编辑配额') }[s.mode] || ''
      );
    if (s.key === 'stockwatch' && s.mode === 'stocks') return b('threshold', '预警阈值') + b('inventory', '调整数量');
    if (s.key === 'points') return b('points-detail', '资料与积分');
    if (s.key === 'messages' || s.key === 'content') return b('edit', '编辑');
    if (s.key === 'reconciliation') return b('report', '查看报告');
    if (s.key === 'tasks' && r.retryable) return b('retry', '重试');
    return '';
  }
  function table(s) {
    if (!s.ready) return window.LumoSkeleton.render('table', { count: 5 });
    if (!s.items.length) return U.empty('暂无匹配记录', '调整筛选条件或创建新的配置。');
    const cols = columns(s),
      rows = s.local ? s.items.slice((s.page - 1) * size, s.page * size) : s.items;
    return `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="${esc(names[s.key])}数据列表"><table class="admin-table mg-table"><thead><tr>${cols.map(([title]) => `<th>${title}</th>`).join('')}<th>操作</th></tr></thead><tbody>${rows.map((r) => `<tr>${cols.map(([, render]) => `<td>${render(r)}</td>`).join('')}<td><div class="admin-actions">${actions(s, r) || '<span class="admin-muted">—</span>'}</div></td></tr>`).join('')}</tbody></table></div>`;
  }
  function filters(s) {
    const options =
      s.key === 'stockwatch' && s.mode === 'stocks'
        ? [
            ['low', '低库存'],
            ['out', '缺货'],
            ['healthy', '库存正常'],
          ]
        : s.key === 'content'
          ? Object.entries(C.ContentStatus).map(([, v]) => [v, label[v] || v])
          : s.key === 'tasks' && ['inbox', 'outbox'].includes(s.mode)
            ? Object.entries(s.mode === 'outbox' ? C.OutboxState : C.InboxState).map(([, v]) => [v, label[v] || v])
            : [];
    const services = s.services.length
      ? s.services
      : [
          C.Service.Inventory,
          C.Service.Order,
          C.Service.Payment,
          C.Service.Fulfillment,
          C.Service.Points,
          C.Service.Notification,
        ];
    return `<form data-mg-filter class="op-filters">${s.key === 'tasks' ? `<label class="field">所属服务<select name="service">${services.map((v) => `<option ${v === s.service ? 'selected' : ''}>${esc(v)}</option>`).join('')}</select></label>` : ''}${s.key === 'special' && s.mode === 'quotas' ? `<label class="field">配额类型<select name="quotaKind"><option value="${C.CampaignKind.Flash}" ${s.quotaKind === C.CampaignKind.Flash ? 'selected' : ''}>秒杀</option><option value="${C.CampaignKind.Group}" ${s.quotaKind === C.CampaignKind.Group ? 'selected' : ''}>拼团</option></select></label>` : ''}<label class="field">${s.key === 'stockwatch' && s.mode === 'adjustments' ? '完整 SKU 编号（必填）' : '搜索记录'}<input type="search" name="${s.key === 'stockwatch' && s.mode === 'adjustments' ? 'sku' : 'keyword'}" value="${esc(s.key === 'stockwatch' && s.mode === 'adjustments' ? s.filters.sku : s.filters.keyword)}" placeholder="${s.key === 'stockwatch' ? 'SKU、操作人或凭证编号' : '名称、会员或业务编号'}"></label>${options.length ? `<label class="field">状态<select name="status"><option value="">全部状态</option>${options.map(([value, name]) => `<option value="${esc(value)}" ${s.filters.status === value ? 'selected' : ''}>${esc(name)}</option>`).join('')}</select></label>` : ''}<button class="primary" ${s.busy ? 'disabled' : ''}>查询</button><button type="button" class="outline" data-mg="reset" ${s.busy ? 'disabled' : ''}>重置</button></form>`;
  }
  function detail(s) {
    if (s.key === 'points') {
      const p = s.profile || {},
        a = s.account || {};
      return `<button class="outline" data-mg="back">← 返回会员列表</button><div class="op-detail-grid ua-detail"><section class="admin-section"><h2>会员资料</h2><dl class="op-definition">${pair('会员编号', s.detailId)}${pair('昵称', p.display_name)}${pair('成长值', p.growth)}${pair('会员等级', p.level === C.MemberLevel.Gold ? '黄金会员' : '普通会员')}</dl><div class="admin-actions"><button class="outline" data-mg="profile">编辑资料</button></div></section><section class="admin-section"><h2>积分资产</h2><dl class="op-definition">${pair('可用积分', a.available)}${pair('冻结积分', a.frozen)}${pair('待偿还欠账', a.debt)}</dl><div class="admin-actions"><button class="outline" data-mg="adjust-points">人工补偿 / 扣减</button></div></section></div><section class="admin-list-panel"><div class="admin-list-summary"><strong>积分流水</strong><button class="outline" data-mg="export">导出流水</button></div>${simpleTable(
        ['类型', '积分变动', '订单 / 原因', '操作人', '时间'],
        (s.ledger || []).map((r) => [
          esc(
            {
              [C.PointsLedgerKind.Manual]: '人工调整',
              [C.PointsLedgerKind.Freeze]: '下单冻结',
              [C.PointsLedgerKind.Confirmed]: '下单抵扣',
              [C.PointsLedgerKind.Cancelled]: '取消解冻',
              [C.PointsLedgerKind.Reward]: '消费奖励',
              [C.PointsLedgerKind.Checkin]: '签到奖励',
              [C.PointsLedgerKind.RewardReversal]: '奖励撤回',
              [C.PointsLedgerKind.Refund]: '退款返还',
            }[r.kind] || r.kind,
          ),
          esc(r.amount),
          `${esc(r.order_id || '—')}<small>${esc(r.reason || '')}</small>`,
          esc(r.actor_id || '系统'),
          date(r.created_at),
        ]),
      )}${U.pager(s.ledgerTotal, s.detailPage, 10, 'mg-detail', s.busy)}</section>`;
    }
    const r = s.report || {},
      diffs = r.differences || { items: [], total: 0 };
    return `<button class="outline" data-mg="back">← 返回对账任务</button><section class="admin-section"><div class="admin-panel-heading"><h2>账期核对报告</h2>${stateBadge(r.status)}</div><dl class="op-definition">${pair('任务编号', r.id)}${pair('账期', `${r.from || '历史内部核对'} ${r.to ? '至 ' + r.to : ''}`)}${pair('核对进度', `${r.checked ?? 0} / ${r.total ?? 0}`)}${pair('差异条数', diffs.total)}${pair('任务说明', r.reason)}${pair('创建人', r.actor_id)}${pair('创建时间', date(r.created_at))}</dl>${r.error ? `<p class="admin-error">${esc(r.error)}</p><button class="outline" data-mg="resume">继续核对</button>` : ''}<p class="admin-muted">核对内部台账，按任务创建时点固定支付记录范围；运行中会自动刷新。</p></section><section class="admin-list-panel"><div class="admin-list-summary"><strong>对账差异</strong><button class="outline" data-mg="export">导出全部差异</button></div>${simpleTable(
      ['支付单', '订单', '差异类型'],
      (diffs.items || []).map((row) => [
        esc(row.payment_id),
        esc(row.order_id || '—'),
        esc(
          {
            [C.ReconciliationDifferenceKind.OrderUnavailable]: '关联订单不可用',
            [C.ReconciliationDifferenceKind.AmountMismatch]: '金额不一致',
            [C.ReconciliationDifferenceKind.PaymentNotAcceptedOrPendingEvent]: '支付尚未被订单接纳',
          }[row.kind] || row.kind,
        ),
      ]),
    )}${U.pager(diffs.total, s.detailPage, 8, 'mg-detail', s.busy)}</section><section class="admin-section"><h2>处理备注 · 最近50条</h2><form data-mg-note><label class="field">跟进情况<textarea name="body" data-mg-note-text rows="3" maxlength="2000" required>${esc(s.noteText)}</textarea></label><button class="primary" ${s.busy ? 'disabled' : ''}>添加备注</button></form><ol class="mg-notes" tabindex="0" aria-label="对账处理备注">${(r.notes || []).map((n) => `<li><p>${esc(n.body)}</p><small>${esc(n.actor_id)} · ${date(n.created_at)}</small></li>`).join('')}</ol></section>`;
  }
  function simpleTable(headers, rows) {
    return rows.length
      ? `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="业务明细"><table class="admin-table"><thead><tr>${headers.map((v) => `<th>${v}</th>`).join('')}</tr></thead><tbody>${rows.map((cells) => `<tr>${cells.map((v) => `<td>${v}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`
      : U.empty('暂无记录', '新的业务明细会显示在这里。');
  }
  async function fetch(m, pageOverride) {
    const s = m.state,
      call = (...args) => m.adapter.call(...args);
    if (s.detailId && s.key === 'points') {
      const [account, profile, ledger] = await Promise.all([
        call(C.Service.Points, C.Action.AdminGet, { id: s.detailId }),
        call(C.Service.Member, C.Action.Get, { id: s.detailId }),
        call(C.Service.Points, C.Action.Ledger, { id: s.detailId, data: { page: s.detailPage, page_size: 10 } }),
      ]);
      return { account, profile, ledger: ledger.items || [], ledgerTotal: Number(ledger.total || 0), ready: true };
    }
    if (s.detailId && s.key === 'reconciliation')
      return {
        report: await call(C.Service.Reconciliation, C.Action.Get, {
          id: s.detailId,
          data: { page: s.detailPage, page_size: 8 },
        }),
        ready: true,
      };
    if (s.key === 'stockwatch' && s.mode === 'adjustments' && !s.filters.sku)
      return { items: [], total: 0, ready: true };
    const [service, action] = source(s),
      result = await call(service, action, query(s, pageOverride ?? s.page));
    if (policyMode(s))
      return {
        ready: true,
        policy: result.config,
        version: Number(result.version || 0),
        draft: s.draft || {
          type: s.mode,
          values: copy(result.config),
          version: Number(result.version || 0),
          original: true,
          key: crypto.randomUUID(),
          reason: '',
          id: '',
        },
      };
    if (!Array.isArray(result.items)) throw Error('管理列表返回格式无效');
    const lastPage = Math.max(1, Math.ceil(Number(result.total ?? result.items.length) / size));
    if (s.key !== 'messages' && (pageOverride ?? s.page) > lastPage) return fetch(m, lastPage);
    const local = s.key === 'messages',
      items = local ? result.items.slice().sort((a, b) => a.id.localeCompare(b.id)) : result.items;
    const products = {};
    if (s.key === 'stockwatch' && s.mode === 'stocks' && items.length) {
      const rows = await call(C.Service.Catalog, C.Action.BatchGet, { data: { ids: items.map((r) => r.sku_id) } });
      for (const row of rows.items || []) products[row.id] = row;
    }
    return {
      items,
      page: pageOverride ?? s.page,
      total: Number(result.total ?? items.length),
      products,
      local,
      services: result.services || s.services,
      ready: true,
    };
  }
  function view(m) {
    const s = m.state;
    const create =
      (s.key === 'special' && ['sessions', 'rules', 'quotas'].includes(s.mode)) ||
      s.key === 'content' ||
      s.key === 'reconciliation';
    const heading = U.heading(
      names[s.key],
      notes[s.key],
      `<button class="outline" data-mg="reload" ${s.busy ? 'disabled' : ''}>刷新</button>${!s.draft && !s.detailId && create ? '<button class="primary" data-mg="new">' + (s.key === 'reconciliation' ? '创建对账任务' : '新建配置') + '</button>' : ''}`,
    );
    const tabs = s.detailId
      ? ''
      : `<div class="mg-tabs" role="group" aria-label="管理分类">${modes[s.key].map(([mode, name]) => `<button class="outline ${mode === s.mode ? 'active' : ''}" data-mg-mode="${mode}" ${s.busy ? 'disabled' : ''}>${name}</button>`).join('')}</div>`;
    if (s.draft) return heading + tabs + editor(s);
    if (s.detailId) return heading + (!s.ready ? window.LumoSkeleton.render('form', { count: 5 }) : detail(s));
    if (policyMode(s))
      return (
        heading +
        tabs +
        (s.ready ? U.empty('配置读取失败', '点击刷新重试。') : window.LumoSkeleton.render('form', { count: 5 }))
      );
    return (
      heading +
      tabs +
      `<section class="admin-list-panel">${filters(s)}<div class="admin-list-summary"><span>共 ${s.total.toLocaleString('zh-CN')} 条记录</span><button class="outline" data-mg="export" ${s.busy || !s.ready || !s.total ? 'disabled' : ''}>导出全部匹配记录</button></div>${table(s)}${U.pager(s.total, s.page, size, 'mg', s.busy)}</section>`
    );
  }
  async function save(m, form) {
    const s = m.state,
      d = s.draft;
    if (!d || s.busy) return;
    let parsed;
    try {
      parsed = readForm(form, d);
      if (!parsed.reason) throw Error('请填写操作原因');
    } catch (e) {
      s.error = e.message;
      m.draw();
      return;
    }
    d.values = parsed.values;
    d.reason = parsed.reason;
    const row = parsed.values,
      command = {
        id: row.id || d.id,
        idempotency_key: d.key,
        data: { ...row, version: d.version, reason: parsed.reason },
      },
      call = (...args) => m.adapter.call(...args);
    let service, action;
    switch (d.type) {
      case 'session':
        service = C.Service.Flash;
        action = C.Action.Upsert;
        break;
      case 'rule':
        service = C.Service.Group;
        action = C.Action.RuleUpsert;
        break;
      case 'quota':
        service = C.Service.Campaign;
        action = C.Action.RuleUpsert;
        command.data.threshold_cents = 0;
        command.data.min_quantity = 1;
        command.data.pay_rate_bps = 0;
        break;
      case 'threshold':
        service = C.Service.Inventory;
        action = C.Action.Threshold;
        break;
      case 'adjustPoints':
        service = C.Service.Points;
        action = C.Action.Adjust;
        command.id = row.user_id;
        break;
      case 'profile':
        service = C.Service.Member;
        action = C.Action.Update;
        command.id = s.detailId;
        command.data = { display_name: row.display_name, avatar: row.avatar, reason: parsed.reason };
        break;
      case 'template':
        service = C.Service.Notification;
        action = C.Action.TemplateUpsert;
        break;
      case 'article':
        service = C.Service.Content;
        action = C.Action.AdminUpsert;
        break;
      case 'run':
        service = C.Service.Reconciliation;
        action = C.Action.Run;
        command.id = '';
        command.data.async = true;
        break;
      case 'resume':
        service = C.Service.Reconciliation;
        action = C.Action.Run;
        command.id = s.detailId;
        command.data = { retry: true, reason: parsed.reason };
        break;
      case 'retry':
        service = C.Service.Management;
        action = C.Action.Retry;
        command.data.service = s.service;
        command.data.kind = s.mode;
        break;
      default:
        service =
          d.type === 'shipping'
            ? C.Service.Logistics
            : d.type === 'pointsRules'
              ? C.Service.Points
              : d.type === 'memberRules'
                ? C.Service.Member
                : d.type === 'search'
                  ? C.Service.Search
                  : C.Service.Recommendation;
        action = C.Action.PolicyUpsert;
        command.id = '';
        command.data = { config: row, version: d.version, reason: parsed.reason };
    }
    const ok = await m.run(
      () => call(service, action, command),
      () => {
        s.draft = null;
      },
    );
    if (ok) {
      m.adapter.notify?.('已保存到服务器');
      await m.refresh();
    }
  }
  async function exportCSV(m) {
    const s = m.state,
      snapshot = copy({
        key: s.key,
        mode: s.mode,
        page: s.page,
        filters: s.filters,
        service: s.service,
        quotaKind: s.quotaKind,
        detailId: s.detailId,
      });
    const ok = await m.run(
      async () => {
        let service, action, command;
        if (snapshot.detailId) {
          service = snapshot.key === 'points' ? C.Service.Points : C.Service.Reconciliation;
          action = snapshot.key === 'points' ? C.Action.Ledger : C.Action.Get;
          command = { id: snapshot.detailId, data: { page: 1, page_size: 100 } };
        } else {
          [service, action] = source(snapshot);
          command = query(snapshot, 1, 100);
        }
        const rows = [];
        let total = 0,
          expectedTotal;
        const seen = new Set();
        do {
          command.data.page = Math.floor(rows.length / 100) + 1;
          const result = await m.adapter.call(service, action, command),
            page = snapshot.key === 'reconciliation' && snapshot.detailId ? result.differences : result;
          total = Number(page.total ?? page.items?.length ?? 0);
          if (expectedTotal !== undefined && expectedTotal !== total)
            throw Error('导出期间记录数量发生变化，请重新查询后导出');
          expectedTotal = total;
          if (total > 100000) throw Error('匹配超过10万条，请缩小筛选范围后导出');
          const items = page.items || [];
          if (!items.length) break;
          for (const row of items) {
            const id = row.id || row.sku_id || (row.payment_id ? row.payment_id + ':' + row.kind : '');
            if (id && seen.has(id)) throw Error('导出期间记录顺序发生变化，请重新查询后导出');
            if (id) seen.add(id);
          }
          rows.push(...items);
          if (snapshot.key === 'messages') break;
        } while (rows.length < total);
        if (rows.length < total) throw Error('导出过程中记录范围发生变化，请重新查询后导出');
        return rows;
      },
      (rows) => {
        const keys = [
          ...new Set(rows.flatMap((row) => Object.keys(row).filter((key) => typeof row[key] !== 'object'))),
        ];
        const quote = (value) =>
          '"' +
          String(typeof value === 'string' && /^\s*[=+\-@]/.test(value) ? "'" + value : (value ?? '')).replaceAll(
            '"',
            '""',
          ) +
          '"';
        const csv =
          '\ufeff' +
          [keys.map(quote).join(','), ...rows.map((row) => keys.map((key) => quote(row[key])).join(','))].join('\r\n');
        const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' })),
          link = document.createElement('a');
        link.href = url;
        link.download = names[s.key] + '-' + today() + '.csv';
        link.click();
        setTimeout(() => URL.revokeObjectURL(url), 1000);
      },
    );
    if (ok) m.adapter.notify?.('导出文件已生成');
  }
  function create(key) {
    if (modules.has(key)) return modules.get(key);
    const m = U.controller(
      'mg-' + key,
      () => ({
        key,
        mode: modes[key][0][0],
        page: 1,
        detailPage: 1,
        items: [],
        total: 0,
        products: {},
        services: [],
        service: C.Service.Inventory,
        quotaKind: C.CampaignKind.Flash,
        filters: { keyword: '', status: '', sku: '' },
        ready: false,
        busy: false,
        error: '',
        draft: null,
        detailId: '',
        noteText: '',
        local: false,
      }),
      fetch,
      view,
    );
    modules.set(key, m);
    document.addEventListener('click', async (event) => {
      const el = event.target.closest('[data-mg],[data-mg-mode],[data-mg-page],[data-mg-detail-page]');
      if (!el || !m.active() || m.state.busy || el.disabled) return;
      const s = m.state,
        action = el.dataset.mg;
      if (el.dataset.mgMode) {
        if (!modes[key].some(([mode]) => mode === el.dataset.mgMode)) return;
        s.mode = el.dataset.mgMode;
        s.page = 1;
        s.ready = false;
        s.draft = null;
        s.detailId = '';
        s.filters = { keyword: '', status: '', sku: '' };
        await m.refresh();
        return;
      }
      if (el.dataset.mgPage) {
        const page = Number(el.dataset.mgPage);
        if (!Number.isInteger(page) || page < 1 || page > Math.max(1, Math.ceil(s.total / size))) return;
        if (s.local) {
          s.page = page;
          m.draw();
        } else {
          const old = s.page;
          s.page = page;
          if (!(await m.refresh())) s.page = old;
        }
        return;
      }
      if (el.dataset.mgDetailPage) {
        s.detailPage = Number(el.dataset.mgDetailPage);
        await m.refresh();
        return;
      }
      if (action === 'reload') {
        s.draft = null;
        await m.refresh();
        return;
      }
      if (action === 'reset') {
        s.filters = { keyword: '', status: '', sku: '' };
        s.page = 1;
        await m.refresh();
        return;
      }
      if (action === 'cancel') {
        s.draft = null;
        if (policyMode(s)) {
          s.mode = modes[s.key][0][0];
          if (policyMode(s)) await m.refresh();
        }
        m.draw();
        return;
      }
      if (action === 'back') {
        s.detailId = '';
        s.detailPage = 1;
        s.ready = false;
        await m.refresh();
        return;
      }
      if (action === 'export') {
        await exportCSV(m);
        return;
      }
      const row = s.items.find((row) => (row.id || row.sku_id) === el.dataset.id);
      if (action === 'new')
        s.draft = draft(
          s,
          s.key === 'special'
            ? { sessions: 'session', rules: 'rule', quotas: 'quota' }[s.mode]
            : s.key === 'reconciliation'
              ? 'run'
              : 'article',
        );
      if (action === 'edit' && row)
        s.draft = draft(
          s,
          s.key === 'special'
            ? { sessions: 'session', rules: 'rule', quotas: 'quota' }[s.mode]
            : s.key === 'messages'
              ? 'template'
              : 'article',
          row,
        );
      if (action === 'threshold' && row)
        s.draft = draft(s, 'threshold', {
          id: row.sku_id,
          threshold: row.threshold,
          expected_threshold: row.threshold,
        });
      if (action === 'inventory' && row) {
        m.adapter.navigate?.('inventory', { skuId: row.sku_id });
        return;
      }
      if ((action === 'points-detail' && row) || (action === 'report' && row)) {
        s.detailId = row.id;
        s.ready = false;
        s.detailPage = 1;
        await m.refresh();
        return;
      }
      if (action === 'adjust-points')
        s.draft = draft(s, 'adjustPoints', { user_id: s.detailId, expected_available: s.account.available, amount: 0 });
      if (action === 'profile')
        s.draft = draft(s, 'profile', { display_name: s.profile.display_name || '', avatar: s.profile.avatar || '' });
      if (action === 'retry' && row) s.draft = draft(s, 'retry', row);
      if (action === 'resume') s.draft = draft(s, 'resume', { id: s.detailId });
      m.draw();
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active()) return;
      if (event.target.matches('[data-mg-save]')) {
        event.preventDefault();
        await save(m, event.target);
      }
      if (event.target.matches('[data-mg-filter]')) {
        event.preventDefault();
        if (m.state.busy) return;
        const form = new FormData(event.target),
          s = m.state;
        s.filters = {
          keyword: String(form.get('keyword') || '').trim(),
          status: String(form.get('status') || ''),
          sku: String(form.get('sku') || '').trim(),
        };
        s.service = String(form.get('service') || s.service);
        s.quotaKind = String(form.get('quotaKind') || s.quotaKind);
        s.page = 1;
        await m.refresh();
      }
      if (event.target.matches('[data-mg-note]')) {
        event.preventDefault();
        const body = String(new FormData(event.target).get('body') || '').trim();
        const ok = await m.run(
          () =>
            m.adapter.call(C.Service.Reconciliation, C.Action.Note, {
              id: m.state.detailId,
              data: { body },
              idempotency_key: crypto.randomUUID(),
            }),
          () => {
            m.state.noteText = '';
          },
        );
        if (ok) await m.refresh();
      }
    });
    document.addEventListener('input', (event) => {
      if (!m.active() || m.state.busy) return;
      const s = m.state;
      if (s.draft?.type === 'template' && ['title', 'body'].includes(event.target.dataset?.mgField)) {
        s.draft.values[event.target.dataset.mgField] = event.target.value;
        const preview = document.querySelector('[data-mg-preview]');
        if (preview) preview.innerHTML = templatePreview(s.draft);
      }
      if (event.target.matches('[data-mg-reason]') && s.draft) s.draft.reason = event.target.value;
      if (event.target.matches('[data-mg-note-text]')) s.noteText = event.target.value;
    });
    return m;
  }
  setInterval(() => {
    const m = modules.get('reconciliation');
    if (!m || !m.active() || m.state.busy || m.state.draft) return;
    if (document.activeElement?.matches?.('input,textarea,select')) return;
    const s = m.state;
    if (
      s.detailId
        ? [C.ReconciliationStatus.Queued, C.ReconciliationStatus.Running].includes(s.report?.status)
        : s.items.some((row) => [C.ReconciliationStatus.Queued, C.ReconciliationStatus.Running].includes(row.status))
    )
      m.refresh();
  }, 2000);
  window.LumoManagementAdmin = { create };
})();
