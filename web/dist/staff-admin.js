(function () {
  'use strict';
  const U = window.LumoAdminUI,
    labels = {
      staff_support: '客服',
      staff_warehouse: '仓库',
      staff_operations: '运营',
      staff_finance: '财务',
      staff_analyst: '数据分析',
    };
  const fresh = () => ({
    account_id: '',
    expected_role: '',
    username: '',
    role: 'staff_support',
    password: '',
    active: 'true',
    reason: '',
  });
  const m = U.controller(
    'staff-admin-root',
    () => ({ data: null, draft: fresh(), error: '', busy: false }),
    async (module) => ({ data: await module.adapter.call('iam', 'staff_list', { data: { page: 1, page_size: 100 } }) }),
    (module) => {
      if (!module.state.data)
        return U.heading('正在读取管理数据', '') + window.LumoSkeleton.render('rows', { count: 4 });
      const data = module.state.data || { items: [] },
        d = module.state.draft;
      return (
        U.heading('员工与权限', '仅超级管理员可以创建员工或调整角色。角色变更立即撤销旧会话。') +
        module.tabs(
          [
            ['accounts', '员工账号'],
            ['editor', '员工设置'],
          ],
          '员工与权限功能',
        ) +
        module.panel(
          'accounts',
          `<section class="admin-section"><div class="admin-panel-heading"><h2>员工账号</h2><button class="primary" data-staff-new>新增员工</button></div><div class="admin-table-wrap" tabindex="0" role="region" aria-label="员工账号列表"><table class="admin-table"><thead><tr><th>账号</th><th>角色</th><th>状态</th><th>操作</th></tr></thead><tbody>${(data.items || []).map((r) => `<tr><td>${U.esc(r.username)}</td><td>${labels[r.role] || U.esc(r.role)}</td><td>${r.active ? '启用' : '停用'}</td><td><button class="outline" data-staff-edit="${U.esc(r.id)}">编辑</button></td></tr>`).join('')}</tbody></table></div></section>`,
        ) +
        module.panel(
          'editor',
          `<section class="admin-section"><div class="admin-panel-heading"><h2>${d.account_id ? '调整员工权限' : '创建员工'}</h2><button class="outline" data-staff-new>新建员工</button></div><form id="staff-save-form" class="admin-form-stack"><input type="hidden" name="account_id" value="${U.esc(d.account_id)}"><input type="hidden" name="expected_role" value="${U.esc(d.expected_role)}"><label class="field">账号<input name="username" value="${U.esc(d.username)}" required minlength="4" maxlength="32" autocomplete="off"></label><label class="field">角色<select name="role">${Object.entries(
            labels,
          )
            .map(([v, l]) => `<option value="${v}" ${d.role === v ? 'selected' : ''}>${l}</option>`)
            .join(
              '',
            )}</select></label><label class="field">初始密码（编辑时留空保持）<input name="password" value="${U.esc(d.password)}" type="password" minlength="8" maxlength="72" autocomplete="new-password"></label><label class="field">状态<select name="active"><option value="true" ${d.active === 'true' ? 'selected' : ''}>启用</option><option value="false" ${d.active === 'false' ? 'selected' : ''}>停用</option></select></label><label class="field">操作原因<input name="reason" value="${U.esc(d.reason)}" required maxlength="240"></label><div class="admin-form-actions"><button class="primary" ${module.state.busy ? 'disabled' : ''}>保存员工</button><button type="button" class="outline" data-staff-back>返回账号列表</button></div></form></section>`,
        )
      );
    },
  );
  document.addEventListener('click', (event) => {
    const b = event.target.closest('[data-staff-edit],[data-staff-new],[data-staff-back]');
    if (!b || !m.active() || m.state.busy) return;
    if (b.hasAttribute('data-staff-back')) {
      m.selectTab('accounts', true);
      return;
    }
    if (b.hasAttribute('data-staff-new')) {
      m.state.draft = fresh();
      m.state.sectionTab = 'editor';
      m.draw();
      document.querySelector('#staff-save-form [name="username"]')?.focus({ preventScroll: true });
      return;
    }
    const row = m.state.data.items.find((r) => r.id === b.dataset.staffEdit);
    if (!row) return;
    m.state.draft = {
      ...fresh(),
      account_id: row.id,
      expected_role: row.role,
      username: row.username,
      role: row.role,
      active: String(row.active),
    };
    m.state.sectionTab = 'editor';
    m.draw();
    document.querySelector('#staff-save-form [name="username"]')?.focus({ preventScroll: true });
  });
  const remember = (event) => {
    if (m.active() && event.target.closest('#staff-save-form') && event.target.name in m.state.draft)
      m.state.draft[event.target.name] = event.target.value;
  };
  document.addEventListener('input', remember);
  document.addEventListener('change', remember);
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'staff-save-form' || !m.active()) return;
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.target));
    data.active = data.active === 'true';
    const id = data.account_id;
    delete data.account_id;
    const ok = await m.run(() => m.adapter.call('iam', 'staff_upsert', { id, data }));
    if (ok) {
      m.state.draft = fresh();
      m.state.sectionTab = 'accounts';
      await m.refresh();
    }
  });
  window.LumoStaffAdmin = m;
})();
