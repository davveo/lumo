(function () {
  'use strict';
  const C = window.LumoConstants;
  const U = window.LumoAdminUI,
    statuses = {
      DRAFT: '待审批',
      APPROVED: '待到货',
      PARTIALLY_RECEIVED: '部分到货',
      RECEIVED: '全部到货',
      CANCELLED: '已取消剩余采购',
    };
  const m = U.controller(
    'procurement-admin-root',
    () => ({ data: null, detail: null, detailID: '', busy: false, error: '', page: 1 }),
    async (module) => ({
      data: await module.adapter.call('inventory', 'procurement_list', {
        data: { page: module.state.page, page_size: 20 },
      }),
      ...(module.state.detailID
        ? { detail: await module.adapter.call('inventory', 'procurement_get', { id: module.state.detailID }) }
        : {}),
    }),
    (module) => {
      if (!module.state.data)
        return U.heading('正在读取管理数据', '') + window.LumoSkeleton.render('rows', { count: 4 });
      const data = module.state.data || { items: [] },
        order = module.state.detail;
      return (
        U.heading('采购与补货', '从供应商采购、审批到到货验收；合格品入库，损坏品单独登记。') +
        `<section class="admin-section"><h2>采购单</h2><table class="admin-table"><thead><tr><th>编号 / 供应商</th><th>采购成本</th><th>状态</th><th>操作</th></tr></thead><tbody>${data.items.map((row) => `<tr><td>${U.esc(row.id)}<br>${U.esc(row.supplier)}</td><td>${U.money(row.total_cost_cents)}</td><td>${statuses[row.status]}</td><td><button class="outline" data-po-open="${U.esc(row.id)}">查看</button></td></tr>`).join('')}</tbody></table><div><button class="outline" data-po-page="${module.state.page - 1}" ${module.state.page <= 1 ? 'disabled' : ''}>上一页</button><span>第 ${module.state.page} 页，共 ${data.total || 0} 单</span><button class="outline" data-po-page="${module.state.page + 1}" ${module.state.page * 20 >= Number(data.total || 0) ? 'disabled' : ''}>下一页</button></div></section>${order ? detail(order) : ''}<section class="admin-section"><h2>新建采购单</h2><form id="procurement-create-form"><label class="field">供应商<input name="supplier" required maxlength="120"></label><label class="field">供应商联系方式<input name="supplier_contact" maxlength="160"></label><label class="field">采购明细（每行：SKU编号,数量,单价分）<textarea name="lines" required rows="5" placeholder="coffee,100,6000"></textarea></label><label class="field">备注<input name="note" maxlength="500"></label><button class="primary">创建待审批采购单</button></form></section>`
      );
    },
  );
  function detail(order) {
    return `<section class="admin-section"><h2>${U.esc(order.supplier)} · ${statuses[order.status]}</h2><p>创建人 ${U.esc(order.actor_id)} · 审批人 ${U.esc(order.approved_by || '—')}</p><div>${order.status === C.PurchaseStatus.Draft ? '<button class="primary" data-po="approve">审批通过</button>' : ''}${![C.PurchaseStatus.Received, C.PurchaseStatus.Cancelled].includes(order.status) ? '<button class="outline" data-po="cancel">取消未到货部分</button>' : ''}</div><form id="procurement-receive-form"><table class="admin-table"><thead><tr><th>SKU</th><th>采购 / 已到货 / 损坏</th><th>本次到货</th><th>其中损坏</th></tr></thead><tbody>${order.lines.map((line) => `<tr><td>${U.esc(line.sku_id)}</td><td>${line.quantity} / ${line.received} / ${line.damaged}</td><td><input aria-label="${U.esc(line.sku_id)}到货数量" type="number" min="0" max="${line.quantity - line.received}" value="0" data-po-sku="${U.esc(line.sku_id)}"></td><td><input aria-label="${U.esc(line.sku_id)}损坏数量" type="number" min="0" max="${line.quantity - line.received}" value="0" data-po-damaged="${U.esc(line.sku_id)}"></td></tr>`).join('')}</tbody></table><label class="field">验收备注<input name="note" maxlength="500"></label><button class="primary" ${![C.PurchaseStatus.Approved, C.PurchaseStatus.PartiallyReceived].includes(order.status) ? 'disabled' : ''}>确认本次到货并入库</button></form><h3>最近100次收货记录（共 ${order.receipts_total} 次）</h3>${(order.receipts || []).map((r) => `<p>${U.esc(r.created_at)} · 验收人 ${U.esc(r.actor_id)} · ${U.esc(r.lines.map((l) => l.sku_id + ' 到货' + l.quantity + ' 损坏' + l.damaged).join('；'))} · ${U.esc(r.note)}</p>`).join('')}</section>`;
  }
  document.addEventListener('click', async (event) => {
    if (!m.active()) return;
    const button = event.target.closest('[data-po-open],[data-po],[data-po-page]');
    if (!button) return;
    if (button.dataset.poOpen) {
      m.state.detailID = button.dataset.poOpen;
      await m.refresh();
      return;
    }
    if (button.dataset.poPage) {
      m.state.page = Number(button.dataset.poPage);
      await m.refresh();
      return;
    }
    const action = button.dataset.po,
      order = m.state.detail;
    if (!order) return;
    let reason = '';
    if (action === 'cancel') {
      reason = prompt('请填写取消剩余采购的原因');
      if (!reason) return;
    }
    const ok = await m.run(() =>
      m.adapter.call('inventory', 'procurement_' + action, {
        id: order.id,
        data: { expected_version: order.version, reason },
      }),
    );
    if (ok) await m.refresh();
  });
  document.addEventListener('submit', async (event) => {
    if (!m.active() || !['procurement-create-form', 'procurement-receive-form'].includes(event.target.id)) return;
    event.preventDefault();
    const form = event.target;
    const ok = await m.run(async () => {
      form.dataset.key ||= crypto.randomUUID();
      const data = Object.fromEntries(new FormData(form));
      if (form.id === 'procurement-create-form') {
        data.lines = data.lines
          .split(/\r?\n/)
          .filter((s) => s.trim())
          .map((s) => {
            const [sku_id, q, cost] = s.split(',').map((x) => x.trim());
            return { sku_id, quantity: Number(q), unit_cost_cents: Number(cost) };
          });
        const value = await m.adapter.call('inventory', 'procurement_create', {
          idempotency_key: form.dataset.key,
          data,
        });
        m.state.detailID = value.id;
      } else {
        data.expected_version = m.state.detail.version;
        data.lines = [...form.querySelectorAll('[data-po-sku]')]
          .map((input) => ({
            sku_id: input.dataset.poSku,
            quantity: Number(input.value),
            damaged: Number(
              [...form.querySelectorAll('[data-po-damaged]')].find((n) => n.dataset.poDamaged === input.dataset.poSku)
                .value,
            ),
          }))
          .filter((row) => row.quantity > 0);
        await m.adapter.call('inventory', 'procurement_receive', {
          id: m.state.detailID,
          idempotency_key: form.dataset.key,
          data,
        });
      }
    });
    if (ok) await m.refresh();
  });
  window.LumoProcurementAdmin = m;
})();
