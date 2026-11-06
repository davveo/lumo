(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, date } = U;
  const reports = {
    'order/admin_list': '订单报表',
    'payment/admin_list': '支付报表',
    'refund/admin_list': '退款报表',
    'aftersale/admin_list': '售后报表',
    'fulfillment/admin_list': '仓储履约报表',
    'invoice/admin_list': '发票报表',
    'catalog/admin_list': '商品报表',
    'iam/admin_list': '会员账号报表',
    'iam/audit_list': '账号操作审计',
    'inventory/admin_list': '库存报表',
    'inventory/ledger': '库存流水',
    'inventory/adjustments': '盘点记录',
    'flash/admin_list': '秒杀场次',
    'flash/requests_admin': '秒杀请求',
    'group/rules_admin': '拼团规则',
    'group/admin_list': '拼团记录',
    'campaign/admin_list': '活动配额',
    'notification/templates_admin': '通知模板',
    'content/admin_list': '内容报表',
    'points/ledger': '积分流水',
    'reconciliation/admin_list': '对账任务',
    'reconciliation/get': '对账差异',
    'management/tasks': '异常任务',
  };
  function filters(text) {
    try {
      const f = JSON.parse(text || '{}'),
        names = {
          keyword: '关键词',
          status: '状态',
          from: '开始日期',
          to: '结束日期',
          sku: 'SKU',
          kind: '业务类型',
          customer_id: '顾客编号',
        };
      const rows = Object.entries(names)
        .filter(([key]) => f[key] !== undefined && f[key] !== '')
        .map(([key, label]) => label + '：' + String(f[key]));
      return rows.join('；') || '全部匹配记录';
    } catch (_) {
      return '筛选信息暂不可读';
    }
  }
  const labels = { QUEUED: '排队中', RUNNING: '正在读取', UPLOADING: '保存文件中', READY: '可下载', FAILED: '失败' };
  const m = U.controller(
    'exports-admin-root',
    () => ({ items: [], total: 0, page: 1, busy: false, error: '' }),
    async (module) => module.adapter.call('helpdesk', 'export_list', { data: { page: module.state.page } }),
    (module) => {
      const s = module.state;
      return (
        U.heading(
          '导出任务',
          '',
          `<button class="outline" data-export-refresh ${s.busy ? 'disabled' : ''}>刷新进度</button>`,
        ) +
        `<section class="admin-section export-intro"><div><span class="admin-eyebrow">后台处理</span><h2>离开页面，任务也会继续</h2><p>下载仅限任务创建者，并重新检查当前岗位权限。文件保留7天。</p></div><span>有界批次 · 私有存储</span></section><section class="admin-section"><p class="admin-muted">业务记录按任务创建时点排除后续新增项；库存、配置与异常任务按读取时点取值。处理期间源数据仍可能更新。数量或顺序变化时会停止任务并提示重试。单个CSV上限2MiB、10万条。</p><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>报表与筛选</th><th>进度</th><th>生成 / 到期</th><th>操作</th></tr></thead><tbody>${
          s.items
            .map((j) => {
              const expired = Date.parse(j.expires_at) <= Date.now(),
                pct =
                  j.total > 0
                    ? Math.min(100, Math.round((j.processed / j.total) * 100))
                    : j.status === C.ExportStatus.Ready
                      ? 100
                      : 0;
              return `<tr><td><strong>${esc(reports[j.source] || '业务报表')}</strong><small>${esc(j.id)}</small><details><summary>查看筛选条件</summary><p>${esc(filters(j.filters))}</p></details>${j.error ? `<button class="admin-text-button export-failure" data-export-error="${esc(j.id)}">查看失败原因</button>` : ''}</td><td><span class="admin-state">${expired ? '已到期' : esc(labels[j.status] || j.status)}</span><progress max="100" value="${pct}" aria-label="导出进度"></progress><small>${j.processed} / ${j.total < 0 ? '待统计' : j.total} 条</small></td><td>${date(j.created_at)}<small>到期 ${date(j.expires_at)}</small></td><td>${!expired && j.status === C.ExportStatus.Ready ? `<button class="primary" data-export-download="${esc(j.id)}">下载CSV</button>` : ''}${!expired && j.status === C.ExportStatus.Failed ? `<button class="outline" data-export-retry="${esc(j.id)}">重试此任务</button>` : ''}</td></tr>`;
            })
            .join('') || '<tr><td colspan="4">暂无任务，可在管理列表中点击“导出全部匹配记录”。</td></tr>'
        }</tbody></table></div>${U.pager(s.total, s.page, 10, 'export', s.busy)}</section>`
      );
    },
  );
  document.addEventListener('click', async (event) => {
    const errorButton = event.target.closest('[data-export-error]');
    if (errorButton && m.active()) {
      const job = m.state.items.find((j) => j.id === errorButton.dataset.exportError);
      if (job) U.feedback?.error(job.error, { title: '导出未完成' });
      return;
    }
    if (!m.active() || m.state.busy) return;
    const b = event.target.closest(
      '[data-export-page],[data-export-refresh],[data-export-retry],[data-export-download]',
    );
    if (!b) return;
    if (b.dataset.exportPage) {
      m.state.page = Math.max(1, Number(b.dataset.exportPage));
      await m.refresh();
    } else if (b.hasAttribute('data-export-refresh')) await m.refresh();
    else if (b.dataset.exportRetry) {
      if (await m.run(() => m.adapter.call('helpdesk', 'export_retry', { id: b.dataset.exportRetry })))
        await m.refresh();
    } else if (b.dataset.exportDownload) {
      await m.run(
        () => m.adapter.call('helpdesk', 'export_download', { id: b.dataset.exportDownload }),
        (result) => {
          const url = new URL(result.download_url, location.href);
          if (!['http:', 'https:'].includes(url.protocol)) throw Error('下载地址无效');
          const link = document.createElement('a');
          link.href = url.href;
          link.download = result.file_name;
          link.rel = 'noopener';
          link.click();
        },
      );
    }
  });
  window.LumoExportsAdmin = m;
})();
