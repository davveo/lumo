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
  const m = U.controller(
    'staff-admin-root',
    () => ({ data: null, error: '', busy: false }),
    async (module) => ({ data: await module.adapter.call('iam', 'staff_list', { data: { page: 1, page_size: 100 } }) }),
    (module) => {
      if (!module.state.data)
        return U.heading('正在读取管理数据', '') + window.LumoSkeleton.render('rows', { count: 4 });
      const data = module.state.data || { items: [] };
      return (
        U.heading('员工与权限', '仅超级管理员可以创建员工或调整角色。角色变更立即撤销旧会话。') +
        `<section class="admin-section"><h2>员工账号</h2><table class="admin-table"><thead><tr><th>账号</th><th>角色</th><th>状态</th><th>操作</th></tr></thead><tbody>${(data.items || []).map((r) => `<tr><td>${U.esc(r.username)}</td><td>${labels[r.role] || U.esc(r.role)}</td><td>${r.active ? '启用' : '停用'}</td><td><button class="outline" data-staff-edit="${U.esc(r.id)}">编辑</button></td></tr>`).join('')}</tbody></table></section><section class="admin-section"><h2>创建或调整员工</h2><form id="staff-save-form"><input type="hidden" name="account_id"><input type="hidden" name="expected_role"><label class="field">账号<input name="username" required minlength="4" maxlength="32" autocomplete="off"></label><label class="field">角色<select name="role">${Object.entries(
          labels,
        )
          .map(([v, l]) => `<option value="${v}">${l}</option>`)
          .join(
            '',
          )}</select></label><label class="field">初始密码（编辑时留空保持）<input name="password" type="password" minlength="8" maxlength="72" autocomplete="new-password"></label><label class="field">状态<select name="active"><option value="true">启用</option><option value="false">停用</option></select></label><label class="field">操作原因<input name="reason" required maxlength="240"></label><button class="primary">保存员工</button></form></section>`
      );
    },
  );
  document.addEventListener('click', (event) => {
    const b = event.target.closest('[data-staff-edit]');
    if (!b || !m.active()) return;
    const row = m.state.data.items.find((r) => r.id === b.dataset.staffEdit);
    if (!row) return;
    const form = document.querySelector('#staff-save-form');
    form.elements.account_id.value = row.id;
    for (const key of ['username', 'role']) form.elements[key].value = row[key];
    form.elements.expected_role.value = row.role;
    form.elements.active.value = String(row.active);
    form.elements.password.value = '';
    form.scrollIntoView({ behavior: 'smooth' });
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'staff-save-form' || !m.active()) return;
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.target));
    data.active = data.active === 'true';
    const id = data.account_id;
    delete data.account_id;
    const ok = await m.run(() => m.adapter.call('iam', 'staff_upsert', { id, data }));
    if (ok) await m.refresh();
  });
  window.LumoStaffAdmin = m;
})();
