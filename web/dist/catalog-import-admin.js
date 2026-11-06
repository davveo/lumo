(function () {
  'use strict';
  const C = window.LumoConstants;
  const U = window.LumoAdminUI;
  const statuses = {
    PREVIEW: '待确认',
    IMPORTING: '导入中',
    COMPLETED: '已完成',
    PARTIAL_FAILED: '部分失败',
    VALIDATION_FAILED: '校验未通过',
    VALID: '校验通过',
    INVALID: '无效',
    SUCCEEDED: '成功',
    FAILED: '失败',
  };
  const m = U.controller(
    'catalog-import-admin-root',
    () => ({ job: null, jobID: '', ready: false, busy: false, error: '' }),
    async (module) =>
      module.state.jobID
        ? { ready: true, job: await module.adapter.call('catalog', 'import_get', { id: module.state.jobID }) }
        : { ready: true },
    (module) => {
      if (!module.state.ready)
        return U.heading('正在读取导入任务', '') + window.LumoSkeleton.render('rows', { count: 3 });
      const job = module.state.job;
      return (
        U.heading(
          '商品批量导入',
          '上传CSV或XLSX首张工作表，先预览再提交。价格单位为分；库存独立管理，不会被导入重置。',
        ) +
        `<section class="admin-section"><button class="outline" data-import-template>下载Excel可打开的CSV模板</button><form id="catalog-import-form" class="admin-form-stack"><label class="field">文件（最大512KB、1000行）<input name="file" type="file" accept=".csv,.xlsx" required></label><label class="field">重复商品处理<select name="mode"><option value="create">仅新增，已存在时报错</option><option value="update">仅更新，缺失时报错</option><option value="upsert">新增及更新</option></select></label><div class="admin-form-actions"><button class="primary">校验并预览</button></div></form><p>表头：id、name、price_cents、brand、category、active、sub、image。更新时空白字段保持原值；新商品默认下架。</p></section>${
          job
            ? `<section class="admin-section"><h2>导入任务 ${U.esc(job.id)}</h2><p>状态：${U.esc(statuses[job.status] || job.status)} · 已处理 ${job.processed}/${job.total} · 成功 ${job.succeeded} · 失败 ${job.failed}</p><progress max="${job.total || 1}" value="${job.processed}"></progress><div class="admin-form-actions"><button class="primary" data-import-commit ${job.status !== C.CatalogImportStatus.Preview ? 'disabled' : ''}>确认导入</button><button class="outline" data-import-refresh>刷新进度</button><button class="outline" data-import-errors>下载行处理报告</button></div><div class="admin-table-wrap"><table class="admin-table"><thead><tr><th>行号</th><th>商品编号</th><th>商品名称</th><th>价格（元）</th><th>处理结果</th></tr></thead><tbody>${(
                job.rows || []
              )
                .slice(0, 100)
                .map(
                  (r) =>
                    `<tr><td>${r.line}</td><td>${U.esc(r.id)}</td><td>${U.esc(r.name)}</td><td>${r.price_cents == null ? '—' : (r.price_cents / 100).toFixed(2)}</td><td>${U.esc(r.error || statuses[r.status] || r.status)}</td></tr>`,
                )
                .join('')}</tbody></table></div><p>页面展示前100行，下载报告包含全部行。</p></section>`
            : ''
        }`
      );
    },
  );
  function download(text, name) {
    const url = URL.createObjectURL(new Blob(['\ufeff' + text], { type: 'text/csv;charset=utf-8' })),
      a = document.createElement('a');
    a.href = url;
    a.download = name;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  const cell = (value) =>
    '"' +
    String(value ?? '')
      .replace(/^[=+@-]/, "'$&")
      .replace(/"/g, '""') +
    '"';
  document.addEventListener('click', async (event) => {
    if (!m.active()) return;
    const b = event.target.closest(
      '[data-import-template],[data-import-commit],[data-import-refresh],[data-import-errors]',
    );
    if (!b) return;
    if (b.hasAttribute('data-import-template'))
      return download(
        'id,name,price_cents,brand,category,active,sub,image\r\nnew-product,示例商品,1999,LUMO,生活用品,false,商品简介,assets/hero.jpg\r\n',
        'lumo-products-template.csv',
      );
    if (b.hasAttribute('data-import-errors'))
      return download(
        '行号,商品编号,状态,结果\r\n' +
          (m.state.job?.rows || []).map((r) => [r.line, r.id, r.status, r.error].map(cell).join(',')).join('\r\n'),
        'lumo-import-report.csv',
      );
    if (b.hasAttribute('data-import-commit')) {
      if (!(await U.feedback.confirm('确认导入已校验的商品资料？导入不会修改库存。'))) return;
      await m.run(async () => {
        await m.adapter.call('catalog', 'import_commit', { id: m.state.jobID });
      });
    }
    await m.refresh();
  });
  document.addEventListener('submit', async (event) => {
    if (event.target.id !== 'catalog-import-form' || !m.active()) return;
    event.preventDefault();
    const form = event.target,
      file = form.elements.file.files[0];
    if (!file) return;
    await m.run(async () => {
      if (file.size > 512 * 1024) throw Error('文件不能超过512KB');
      const encoded = await new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result).split(',')[1]);
        reader.onerror = reject;
        reader.readAsDataURL(file);
      });
      const job = await m.adapter.call('catalog', 'import_preview', {
        data: { filename: file.name, file_base64: encoded, mode: form.elements.mode.value },
      });
      m.state.job = job;
      m.state.jobID = job.id;
    });
    m.render?.();
    await m.refresh();
  });
  setInterval(() => {
    if (m.active() && m.state.job?.status === C.CatalogImportStatus.Importing && !m.state.busy) void m.refresh();
  }, 2000);
  window.LumoCatalogImportAdmin = m;
})();
