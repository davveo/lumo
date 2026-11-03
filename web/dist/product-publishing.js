(function () {
  'use strict';
  const U = window.LumoAdminUI,
    C = window.LumoConstants,
    { esc, date, money } = U;
  const initial = () => ({
    workspace: null,
    drafts: [],
    draftTotal: 0,
    draftPage: 1,
    page: 1,
    check: null,
    mode: 'on',
    runAt: '',
    endsAt: '',
    selected: null,
    scheduleKey: '',
  });
  function list(s, busy) {
    return `<section class="admin-section"><div class="admin-panel-heading"><div><h2>服务端草稿</h2><p class="admin-muted">草稿在服务器保存，可跨设备继续编辑；发布时检查商品与草稿版本。</p></div><button class="outline" data-pub="drafts" ${busy ? 'disabled' : ''}>刷新草稿</button></div><div class="publishing-drafts">${s.drafts.map((d) => `<button class="outline" data-pub="open-draft" data-id="${esc(d.product_id)}"><strong>${esc(d.name || d.product_id)}</strong><small>草稿 v${d.version} · ${date(d.updated_at)} · ${esc(d.actor_id)}</small></button>`).join('') || '<p class="admin-muted">暂无未发布草稿</p>'}</div>${U.pager(s.draftTotal, s.draftPage, 20, 'pub-draft', busy)}</section>`;
  }
  function panel(s, original, busy) {
    const w = s.workspace || {},
      d = w.draft,
      old = s.selected;
    const labels = {
      [C.PublicationStatus.Scheduled]: '待执行',
      [C.PublicationStatus.Succeeded]: '已完成',
      [C.PublicationStatus.Failed]: '执行失败',
      [C.PublicationStatus.Cancelled]: '已取消',
    };
    const fields = [
      ['name', '商品名称'],
      ['price_cents', '默认售价'],
      ['category', '分类'],
      ['image', '图片'],
      ['active', '上架状态'],
      ['brand', '品牌'],
      ['options', '规格'],
      ['colors', '颜色'],
      ['service_policy', '售后政策'],
    ];
    return `<section class="admin-section publishing-panel"><div class="admin-panel-heading"><div><h2>商品发布中心</h2><p class="admin-muted">当前商品 v${original?.product_version || 0} · ${d ? `服务器草稿 v${d.version}，保存于 ${date(d.updated_at)}` : '尚未保存服务器草稿'}</p></div><span class="admin-state">资料与库存独立</span></div><div class="admin-form-actions"><button type="button" class="outline" data-pub="save-draft" ${busy ? 'disabled' : ''}>保存服务器草稿</button>${d ? `<button type="button" class="outline" data-pub="load-draft" ${busy ? 'disabled' : ''}>载入服务器草稿</button>` : ''}<button type="button" class="outline" data-pub="check" ${busy ? 'disabled' : ''}>发布前检查</button><button type="button" class="primary" data-pub="publish" ${busy ? 'disabled' : ''}>保存草稿并立即上架</button></div>${s.check ? `<div class="publication-check ${s.check.ok ? 'success' : 'warning'}" role="status"><strong>${s.check.ok ? '发布检查通过' : '请先完成以下项目'}</strong><ul>${[...(s.check.issues || []), ...(s.check.warnings || [])].map((t) => `<li>${esc(t)}</li>`).join('') || `<li>${s.check.sku_count} 个规格已通过检查</li>`}</ul><small>检查时间 ${date(s.check.checked_at)}；正式发布时再次核验。</small></div>` : ''}<div class="publication-schedule"><h3>定时上下架</h3><div class="admin-form"><label class="field">计划操作<select data-pub-field="mode"><option value="on" ${s.mode === 'on' ? 'selected' : ''}>发布当前草稿并上架</option><option value="off" ${s.mode === 'off' ? 'selected' : ''}>下架当前已保存商品</option></select></label><label class="field">执行时间<input type="datetime-local" data-pub-field="runAt" value="${esc(s.runAt)}"></label>${s.mode === 'on' ? `<label class="field">同时预约下架（可选）<input type="datetime-local" data-pub-field="endsAt" value="${esc(s.endsAt)}"></label>` : ''}</div><div class="admin-form-actions"><button type="button" class="outline" data-pub="schedule" ${busy ? 'disabled' : ''}>${s.mode === 'on' ? '保存草稿并预约发布' : '预约下架'}</button><small>按当前时区提交；他人修改商品后，计划将停止并提示冲突。</small></div></div><div class="publication-history"><h3>发布计划</h3><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>操作 / 执行时间</th><th>状态</th><th>创建者</th><th>操作</th></tr></thead><tbody>${(w.plans || []).map((p) => `<tr><td>${p.mode === 'on' ? '上架' : '下架'} · ${date(p.run_at)}${p.error ? `<small class="admin-error">${esc(p.error)}</small>` : ''}</td><td>${esc(labels[p.status] || p.status)}</td><td>${esc(p.actor_id)}</td><td>${p.status === C.PublicationStatus.Scheduled ? `<button type="button" class="outline" data-pub="cancel-plan" data-id="${esc(p.id)}">取消计划</button>` : '—'}</td></tr>`).join('') || '<tr><td colspan="4">暂无发布计划</td></tr>'}</tbody></table></div><h3>资料版本</h3><div class="publication-versions">${(w.versions || []).map((v) => `<button type="button" class="outline" data-pub="compare" data-id="${v.version}">版本 ${v.version}<small>${date(v.created_at)} · ${esc(v.actor_id)}</small></button>`).join('') || '<p class="admin-muted">首次保存后开始记录版本</p>'}</div>${U.pager(Math.max(w.versions_total || 0, w.plans_total || 0), s.page, 20, 'pub-version', busy)}</div>${
      old
        ? `<section class="publication-compare"><div class="admin-panel-heading"><h3>版本 ${old.product_version || 0} 与当前资料</h3><button type="button" class="outline" data-pub="close-compare">关闭</button></div><table class="admin-table"><thead><tr><th>字段</th><th>历史版本</th><th>当前版本</th></tr></thead><tbody>${fields
            .map(([k, label]) => {
              const value = (v) =>
                k === 'price_cents'
                  ? money(v)
                  : k === 'active'
                    ? v
                      ? '上架'
                      : '下架'
                    : typeof v === 'object'
                      ? JSON.stringify(v)
                      : v;
              return `<tr class="${JSON.stringify(old[k]) !== JSON.stringify(original?.[k]) ? 'publication-changed' : ''}"><td>${label}</td><td>${esc(value(old[k]))}</td><td>${esc(value(original?.[k]))}</td></tr>`;
            })
            .join(
              '',
            )}</tbody></table><p class="admin-muted">恢复将创建新版本，并恢复上述商品资料及上下架状态。库存和已有订单快照保持独立。</p><div class="admin-form-actions"><button type="button" class="primary" data-pub="restore" data-id="${old.product_version || 0}" ${busy ? 'disabled' : ''}>确认恢复此版本</button><button type="button" class="outline" data-pub="close-compare">取消</button></div></section>`
        : ''
    }</section>`;
  }
  window.LumoProductPublishing = { initial, list, panel };
})();
