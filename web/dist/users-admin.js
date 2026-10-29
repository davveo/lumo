(function () {
  'use strict';
  const C = window.LumoConstants,
    U = window.LumoAdminUI,
    { esc, date, pair } = U;
  const role = (value) => (value === C.Role.Admin ? '管理员' : value === C.Role.User ? '普通会员' : value);
  const m = U.controller(
    'users-admin-root',
    () => ({
      items: [],
      total: 0,
      page: 1,
      keyword: '',
      role: '',
      ready: false,
      busy: false,
      error: '',
      detail: null,
      profile: null,
      change: null,
    }),
    async (module) => {
      const s = module.state;
      const data = await module.adapter.call(C.Service.IAM, C.Action.AdminList, {
        data: { page: s.page, page_size: 20, keyword: s.keyword, status: s.role },
      });
      return { items: data.items || [], total: Number(data.total || 0), ready: true };
    },
    view,
  );
  function view(module) {
    const s = module.state;
    const heading = U.heading(
      '用户管理',
      '查看商城账号、会员资料和账号可用状态。',
      `<button class="outline" data-ua="reload" ${s.busy ? 'disabled' : ''}>刷新</button>`,
    );
    if (s.detail) {
      const row = s.detail,
        p = s.profile || {};
      return (
        heading +
        `<button class="outline" data-ua="back">← 返回用户列表</button><div class="op-detail-grid ua-detail"><section class="admin-section"><h2>账号资料</h2><dl class="op-definition">${pair('用户编号', row.id)}${pair('登录账号', row.username)}${pair('角色', role(row.role))}${pair('账号状态', row.active ? '正常' : '已停用')}${pair('创建时间', date(row.created_at))}${pair('更新时间', date(row.updated_at))}</dl></section><section class="admin-section"><h2>会员资料</h2>${s.profileError ? `<p class="admin-error">${esc(s.profileError)}</p>` : `<dl class="op-definition">${pair('昵称', p.display_name || row.username)}${pair('会员等级', p.level === C.MemberLevel.Gold ? '黄金会员' : '普通会员')}${pair('成长值', p.growth ?? 0)}${pair('资料更新时间', date(p.updated_at))}</dl>`}</section></div>`
      );
    }
    return (
      heading +
      `<section class="admin-list-panel"><form data-ua-filter class="op-filters"><label class="field">搜索用户<input name="keyword" type="search" value="${esc(s.keyword)}" placeholder="账号或用户编号"></label><label class="field">账号角色<select name="role"><option value="">全部角色</option><option value="${C.Role.User}" ${s.role === C.Role.User ? 'selected' : ''}>普通会员</option><option value="${C.Role.Admin}" ${s.role === C.Role.Admin ? 'selected' : ''}>管理员</option></select></label><button class="primary" ${s.busy ? 'disabled' : ''}>查询</button></form>${s.change ? `<form data-ua-change class="op-confirm"><h3>${s.change.active ? '停用' : '启用'}账号：${esc(s.change.username)}</h3><p>${s.change.active ? '停用后将撤销该用户现有登录会话，账号恢复后需重新登录。' : '启用后，该用户可以重新登录。'}</p><label class="field">操作原因<input name="reason" required maxlength="240" placeholder="填写管理原因"></label><div class="admin-actions"><button class="primary" ${s.busy ? 'disabled' : ''}>确认${s.change.active ? '停用' : '启用'}</button><button class="outline" type="button" data-ua="cancel">取消</button></div></form>` : ''}${!s.ready ? (s.error && !s.busy ? U.empty('数据读取失败', '请点击上方重试。') : window.LumoSkeleton.render('table', { count: 6 })) : !s.items.length ? U.empty('暂无匹配用户') : `<div class="admin-table-wrap" tabindex="0" role="region" aria-label="可滚动数据列表"><table class="admin-table"><thead><tr><th>账号 / 用户编号</th><th>角色</th><th>账号状态</th><th>创建时间</th><th>最近更新</th><th>操作</th></tr></thead><tbody>${s.items.map((row) => `<tr><td><strong>${esc(row.username)}</strong><small>${esc(row.id)}</small></td><td>${esc(role(row.role))}</td><td><span class="admin-state ${row.active ? 'success' : 'neutral'}">${row.active ? '正常' : '已停用'}</span></td><td>${date(row.created_at)}</td><td>${date(row.updated_at)}</td><td><div class="admin-actions"><button class="outline" data-ua="detail" data-id="${esc(row.id)}">会员资料</button>${row.role !== C.Role.Admin ? `<button class="outline" data-ua="status" data-id="${esc(row.id)}">${row.active ? '停用' : '启用'}</button>` : '<small>管理员账号受保护</small>'}</div></td></tr>`).join('')}</tbody></table></div>`}${U.pager(s.total, s.page, 20, 'ua', s.busy)}</section>`
    );
  }
  document.addEventListener('click', async (event) => {
    const el = event.target.closest('[data-ua],[data-ua-page]');
    if (!el || !m.active() || m.state.busy) return;
    const s = m.state,
      a = el.dataset.ua;
    if (el.dataset.uaPage) {
      s.page = Number(el.dataset.uaPage);
      await m.refresh();
      return;
    }
    if (a === 'reload') {
      s.detail = null;
      await m.refresh();
    }
    if (a === 'cancel') {
      s.change = null;
      m.draw();
    }
    if (a === 'back') {
      s.detail = null;
      m.draw();
    }
    if (a === 'status') {
      s.change = s.items.find((row) => row.id === el.dataset.id);
      m.draw();
    }
    if (a === 'detail') {
      const row = s.items.find((row) => row.id === el.dataset.id);
      if (row)
        await m.run(async () => {
          let profile = null,
            profileError = '';
          try {
            profile = await m.adapter.call(C.Service.Member, C.Action.Get, { id: row.id });
          } catch (cause) {
            profileError = '会员资料暂不可用：' + cause.message;
          }
          return { detail: row, profile, profileError };
        });
    }
  });
  document.addEventListener('submit', async (event) => {
    if (!m.active()) return;
    if (event.target.matches('[data-ua-filter]')) {
      event.preventDefault();
      if (m.state.busy) return;
      const form = new FormData(event.target);
      m.state.keyword = String(form.get('keyword') || '');
      m.state.role = String(form.get('role') || '');
      m.state.page = 1;
      await m.refresh();
    }
    if (event.target.matches('[data-ua-change]')) {
      event.preventDefault();
      const s = m.state;
      if (s.busy || !s.change) return;
      const row = s.change,
        reason = String(new FormData(event.target).get('reason') || '').trim();
      const ok = await m.run(async () => {
        await m.adapter.call(C.Service.IAM, C.Action.SetActive, {
          id: row.id,
          data: { active: !row.active, expected_active: row.active, reason },
        });
        return { change: null };
      });
      if (ok) {
        m.adapter.notify?.('账号状态已更新');
        await m.refresh();
      }
    }
  });
  window.LumoUsersAdmin = m;
})();
