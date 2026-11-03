(function () {
  'use strict';
  const U = LumoAdminUI,
    { esc } = U;
  const initial = () => ({
    createLines: [],
    supplierID: '',
    coverageDays: 14,
    catalogQuery: '',
    catalogItems: [],
    catalogPage: 1,
    catalogTotal: 0,
    pickedProduct: null,
    supplierDraft: { name: '', contact: '', lead_days: 7, version: 0 },
    createKey: crypto.randomUUID(),
    supplierName: '',
    supplierContact: '',
    purchaseNote: '',
  });
  function render(s) {
    const suppliers = s.suppliers?.items || [],
      product = s.pickedProduct,
      variants = product
        ? product.variants?.length
          ? product.variants
          : [
              {
                sku_id: product.default_sku_id || product.id,
                color: product.defaultColor || '',
                option: product.defaultOption || '标准',
              },
            ]
        : [];
    const numberField = (key, label, value) =>
      `<label class="field">${label}<input name="${key}" data-supplier-field="${key}" type="${key === 'lead_days' ? 'number' : 'text'}" min="0" max="365" maxlength="160" value="${esc(value)}" required></label>`;
    return `<section class="admin-section"><div class="admin-panel-heading"><h2>采购选品</h2><span>按商品和规格添加，采购价由你填写</span></div><form id="procurement-catalog-form" class="admin-filter-form"><label class="field">搜索商品或SKU<input name="keyword" maxlength="100" value="${esc(s.catalogQuery)}" placeholder="名称、品牌、商品或SKU编号"></label><button class="primary" ${s.busy ? 'disabled' : ''}>搜索商品</button></form>${s.catalogItems.length ? `<div class="supply-product-results">${s.catalogItems.map((p) => `<button class="outline" type="button" data-supply-product="${esc(p.id)}"><strong>${esc(p.name)}</strong><small>${esc(p.id)} · ${esc(p.category || '')}</small></button>`).join('')}</div>${U.pager(s.catalogTotal, s.catalogPage, 10, 'supply-catalog', s.busy)}` : ''}${product ? `<div class="supply-variant-picker"><label class="field">${esc(product.name)} · 选择规格<select id="supply-variant">${variants.map((v) => `<option value="${esc(v.sku_id)}">${esc([v.color, v.option, v.sku_id].filter(Boolean).join(' / '))}</option>`).join('')}</select></label><button type="button" class="primary" data-supply-add>添加到采购明细</button></div>` : ''}</section><section class="admin-section"><div class="admin-panel-heading"><h2>新建采购单</h2><span>创建后仍需审批</span></div><form id="procurement-create-form" class="admin-form-stack"><div class="admin-form"><label class="field">选择供应商<select name="supplier_id" data-purchase-field="supplierID"><option value="">临时供应商 / 手工填写</option>${suppliers.map((v) => `<option value="${esc(v.id)}" ${s.supplierID === v.id ? 'selected' : ''}>${esc(v.name)} · 提前期 ${v.lead_days} 天</option>`).join('')}</select><small>展示最近100个档案；也可填写临时供应商。</small></label><label class="field">备货覆盖天数<input name="coverage_days" type="number" min="1" max="365" data-purchase-field="coverageDays" value="${esc(s.coverageDays)}"></label>${s.supplierID ? '' : `<label class="field">供应商名称<input name="supplier" data-purchase-field="supplierName" required maxlength="120" value="${esc(s.supplierName)}"></label><label class="field">供应商联系方式<input name="supplier_contact" data-purchase-field="supplierContact" maxlength="160" value="${esc(s.supplierContact)}"></label>`}</div><div class="admin-table-wrap"><table class="admin-table supply-lines"><thead><tr><th>商品 / 规格</th><th>可用 / 在途</th><th>已采集30天销量</th><th>采购数量</th><th>采购单价（元）</th><th></th></tr></thead><tbody>${s.createLines.map((line, index) => `<tr><td><strong>${esc(line.name)}</strong><small>${esc(line.label || line.sku_id)}</small>${line.basis ? `<small>${esc(line.basis)}</small>` : ''}</td><td>${line.available ?? '—'} / ${line.inbound ?? '—'}</td><td>${line.paid_quantity ?? '—'}</td><td><input type="number" min="0" max="1000000" value="${esc(line.quantity)}" data-purchase-line="${index}" data-field="quantity" aria-label="${esc(line.sku_id)}采购数量"></td><td><input type="number" min="0" max="10000000" step="0.01" value="${esc(line.cost)}" data-purchase-line="${index}" data-field="cost" aria-label="${esc(line.sku_id)}采购单价"></td><td><button type="button" class="admin-text-button" data-supply-remove="${index}">移除</button></td></tr>`).join('') || '<tr><td colspan="6">在上方搜索商品，选择规格后添加到这里。</td></tr>'}</tbody></table></div><div class="admin-form-actions"><button type="button" class="outline" data-supply-suggest ${s.busy || !s.createLines.length || !s.supplierID ? 'disabled' : ''}>计算补货建议</button><small>建议扣除可用库存和已审批在途数量，不会自动下单。</small></div>${s.suggestionsAt ? `<p class="admin-muted">建议生成于 ${U.date(s.suggestionsAt)}。${esc(s.salesScope || '')}</p>` : ''}<details class="supply-paste"><summary>按编号粘贴明细（可选）</summary><label class="field">每行：SKU编号,数量,采购单价分<textarea name="lines" rows="4" placeholder="coffee,100,6000"></textarea><small>仅在没有选品明细时使用。</small></label></details><label class="field">备注<input name="note" maxlength="500" data-purchase-field="purchaseNote" value="${esc(s.purchaseNote)}"></label><div class="admin-form-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>创建待审批采购单</button><button type="button" class="outline" data-supply-clear>清空草稿</button></div></form></section><section class="admin-section"><details><summary class="supply-supplier-heading">供应商档案</summary><p class="admin-muted">维护名称、联系方式与采购提前期，供补货建议使用。</p><form id="supplier-save-form" class="admin-form-stack"><div class="admin-form">${numberField('name', '供应商名称', s.supplierDraft.name)}${numberField('contact', '联系方式', s.supplierDraft.contact)}${numberField('lead_days', '采购提前期（天）', s.supplierDraft.lead_days)}</div><div class="admin-form-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>${s.supplierDraft.id ? '保存修改' : '新增供应商'}</button><button type="button" class="outline" data-supplier-new>新建档案</button></div></form><div class="supplier-list">${suppliers.map((row) => `<button class="outline" type="button" data-supplier-edit="${esc(row.id)}">${esc(row.name)}<small>提前期 ${row.lead_days} 天</small></button>`).join('')}</div></details></section>`;
  }
  function build(s, data) {
    if (!s.createLines.length) {
      data.lines = String(data.lines || '')
        .split(/\r?\n/)
        .filter((x) => x.trim())
        .map((raw) => {
          const [sku_id, q, cost] = raw.split(',').map((v) => v.trim());
          return { sku_id, quantity: Number(q), unit_cost_cents: Number(cost) };
        });
      return data;
    }
    const lines = s.createLines
      .filter((row) => Number(row.quantity) > 0)
      .map((row) => {
        const q = Number(row.quantity),
          value = String(row.cost);
        if (!Number.isSafeInteger(q) || q < 1 || q > 1000000 || !/^\d+(?:\.\d{1,2})?$/.test(value))
          throw Error('请填写有效采购数量和采购单价');
        const [whole, part = ''] = value.split('.'),
          cost = Number(whole) * 100 + Number(part.padEnd(2, '0'));
        if (!Number.isSafeInteger(cost) || cost > 1000000000) throw Error('采购单价超过允许范围');
        return { sku_id: row.sku_id, quantity: q, unit_cost_cents: cost };
      });
    if (!lines.length) throw Error('没有需要采购的规格，请调整数量后重试');
    return {
      supplier_id: s.supplierID,
      supplier: s.supplierName,
      supplier_contact: s.supplierContact,
      note: s.purchaseNote,
      lines,
    };
  }
  function bind(m) {
    const search = async (page) =>
      m.run(async () => {
        const result = await m.adapter.call('catalog', 'admin_list', {
          data: { page, page_size: 10, keyword: m.state.catalogQuery },
        });
        return { catalogItems: result.items || [], catalogTotal: Number(result.total) || 0, catalogPage: page };
      });
    document.addEventListener('input', (event) => {
      if (!m.active()) return;
      const n = event.target;
      if (n.dataset.purchaseField) m.state[n.dataset.purchaseField] = n.value;
      if (n.dataset.supplierField) m.state.supplierDraft[n.dataset.supplierField] = n.value;
      if (n.dataset.purchaseLine != null && m.state.createLines[Number(n.dataset.purchaseLine)])
        m.state.createLines[Number(n.dataset.purchaseLine)][n.dataset.field] = n.value;
    });
    document.addEventListener('change', (event) => {
      if (!m.active()) return;
      const n = event.target;
      if (n.dataset.purchaseField) {
        m.state[n.dataset.purchaseField] = n.value;
        if (n.dataset.purchaseField === 'supplierID') m.draw();
      }
    });
    document.addEventListener('submit', async (event) => {
      if (!m.active() || !['procurement-catalog-form', 'supplier-save-form'].includes(event.target.id)) return;
      event.preventDefault();
      if (event.target.id === 'procurement-catalog-form') {
        m.state.catalogQuery = String(new FormData(event.target).get('keyword') || '');
        await search(1);
      } else {
        const d = m.state.supplierDraft;
        const ok = await m.run(async () => {
          const result = await m.adapter.call('inventory', 'supplier_upsert', {
            id: d.id || '',
            data: { ...d, lead_days: Number(d.lead_days) },
          });
          return { supplierDraft: { name: '', contact: '', lead_days: 7, version: 0 }, supplierID: result.id };
        });
        if (ok) await m.refresh();
      }
    });
    document.addEventListener('click', async (event) => {
      if (!m.active() || m.state.busy) return;
      const b = event.target.closest(
        '[data-supply-product],[data-supply-add],[data-supply-remove],[data-supply-suggest],[data-supply-clear],[data-supplier-edit],[data-supplier-new],[data-supply-catalog-page]',
      );
      if (!b) return;
      const s = m.state;
      if (b.dataset.supplyProduct) {
        await m.run(async () => ({
          pickedProduct: await m.adapter.call('catalog', 'get', { id: b.dataset.supplyProduct }),
        }));
      }
      if (b.dataset.supplyAdd != null && s.pickedProduct) {
        const id = document.querySelector('#supply-variant')?.value,
          p = s.pickedProduct;
        if (!id || s.createLines.some((row) => row.sku_id === id)) return;
        if (s.createLines.length >= 100) {
          m.adapter.notify?.('每单最多100种规格');
          return;
        }
        const v = (p.variants || []).find((row) => row.sku_id === id);
        s.createLines.push({
          sku_id: id,
          name: p.name,
          label: [v?.color, v?.option, id].filter(Boolean).join(' / '),
          quantity: 1,
          cost: '',
        });
        m.draw();
      }
      if (b.dataset.supplyRemove != null) {
        s.createLines.splice(Number(b.dataset.supplyRemove), 1);
        m.draw();
      }
      if (b.dataset.supplyClear != null) {
        Object.assign(s, initial());
        m.draw();
      }
      if (b.dataset.supplierNew != null) {
        s.supplierDraft = { name: '', contact: '', lead_days: 7, version: 0 };
        m.draw();
      }
      if (b.dataset.supplierEdit) {
        s.supplierDraft = { ...(s.suppliers?.items || []).find((row) => row.id === b.dataset.supplierEdit) };
        m.draw();
      }
      if (b.dataset.supplyCatalogPage) {
        await search(Number(b.dataset.supplyCatalogPage));
      }
      if (b.dataset.supplySuggest != null) {
        await m.run(async () => {
          const result = await m.adapter.call('inventory', 'replenishment_get', {
            data: {
              ids: s.createLines.map((row) => row.sku_id),
              supplier_id: s.supplierID,
              coverage_days: Number(s.coverageDays),
            },
          });
          const results = new Map((result.items || []).map((row) => [row.sku_id, row]));
          return {
            createLines: s.createLines.map((row) => ({
              ...row,
              ...results.get(row.sku_id),
              quantity: results.get(row.sku_id)?.suggested_quantity ?? row.quantity,
            })),
            salesScope: result.sales_scope,
            suggestionsAt: new Date().toISOString(),
          };
        });
      }
    });
  }
  window.LumoProcurementComposer = { initial, render, build, bind };
})();
