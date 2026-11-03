(function () {
  'use strict';
  let current = null,
    revision = 0;
  const scope = () => JSON.stringify([LumoAPI.getBase(), LumoAPI.getToken()]);
  const parse = (v, fallback) => {
    try {
      return typeof v === 'string' ? JSON.parse(v) : v || fallback;
    } catch {
      return fallback;
    }
  };
  function reset() {
    revision++;
    if (current) for (const f of current.files) URL.revokeObjectURL(f.url);
    current = null;
  }
  function evidenceView() {
    const el = document.getElementById('after-evidence-list');
    if (!el || !current) return;
    el.innerHTML = current.files
      .map(
        (f, i) =>
          `<div class="after-evidence-card"><img src="${esc(f.url)}" alt="${esc(f.name)}"><span>${esc(f.name)}</span><button type="button" class="text-button" data-after-remove="${i}">移除</button></div>`,
      )
      .join('');
  }
  function lines(form) {
    const data = new FormData(form);
    return {
      order_id: current.order.id,
      type: { '仅退款': 'refund', '退货退款': 'return', '换货': 'exchange' }[String(data.get('type'))],
      lines: current.order.items
        .map((item, i) => ({ sku_id: LumoCommerce.skuForItem(item), quantity: Number(data.get('line-' + i)||0) }))
        .filter((i) => i.quantity > 0),
    };
  }
  function signature(form) {
    return JSON.stringify(lines(form));
  }
  function policy(p) {
    if (!p?.configured) return '<p>商家暂未约定专属处理时效；审核通过且完成必要验收后提交原支付渠道退款。</p>';
    return `<p>商家目标：${Number(p.review_hours)} 小时内审核；审核及验收完成后 ${Number(p.refund_submit_hours)} 小时内提交退款。</p><p>${esc(p.instructions || '')}</p>`;
  }
  async function preview() {
    const form = document.getElementById('member-aftersale-form');
    if (!current || current.form !== form) return;
    const state = current,
      key = signature(form),
      ticket = ++revision,
      el = document.getElementById('after-preview');
    if (!el) return;
    el.innerHTML = '<p role="status">正在核算退款与返还资产…</p>';
    try {
      const value = await LumoAPI.call('aftersale', 'preview', { data: lines(form) });
      if (
        current !== state ||
        ticket !== revision ||
        state.scope !== scope() ||
        !form.isConnected ||
        key !== signature(form)
      )
        return;
      el.innerHTML = `<div class="after-refund-preview"><span>${lines(form).type === 'exchange' ? '换货申请不退现金' : '预估原路退款'}</span><strong>¥${(Number(value.refund_cents || 0) / 100).toFixed(2)}</strong><dl class="service-definition"><div><dt>积分返还</dt><dd>预计 ${Number(value.points_return_estimate || 0)} 分，按累计退款比例处理</dd></div>${value.coupon_id ? `<div><dt>优惠券</dt><dd>${esc(value.coupon_rule)}</dd></div>` : ''}</dl><p>${esc(value.reward_rule)}</p>${policy(value.policy)}<small>${esc(value.notice)}实际到账时间以支付渠道为准。</small></div>`;
    } catch (e) {
      if (current === state && ticket === revision && state.scope === scope() && form.isConnected)
        el.innerHTML = `<p class="api-config-error" role="alert">${esc(e.message)}</p>`;
    }
  }
  function quantityLimits() {
    if(!current)return;const form=current.form,order=current.order,type=lines(form).type;
    const claimed=(LumoMember.model().aftersales||[]).filter(a=>a.orderId===order.id&&!['REJECTED','CANCELLED'].includes(a.backendStatus));
    order.items.forEach((item,i)=>{
      const sku=LumoCommerce.skuForItem(item),p=(order.fulfillment_progress||[]).find(l=>l.sku_id===sku);
      const shipped=p?Number(p.shipped||0)+Number(p.dispatching||0):['SHIPPED','COMPLETED'].includes(order.backendStatus)?Number(item.qty):0;
      let available=type==='refund'?Number(item.qty)-shipped:shipped;
      claimed.filter(a=>(type==='refund')===(a.type==='仅退款')).forEach(a=>parse(a.lines_json,[]).forEach(l=>{if(l.sku_id===sku)available-=Number(l.quantity)}));
      const input=form.elements.namedItem('line-'+i);if(input){input.max=String(Math.max(0,available));input.value=input.max;input.disabled=available<=0;}
    });
  }
  function attach(order) {
    reset();
    const form = document.getElementById('member-aftersale-form');
    if (!form) return;
    current = { form, order, scope: scope(), files: [], busy: false };
 quantityLimits();
    void preview();
  }
  function evidenceFor(form) {
    if (!current || current.form !== form) return [];
    if (current.scope !== scope()) throw Error('登录状态已变化，请重新申请');
    if (current.busy) throw Error('请等待图片上传完成');
    return current.files.map((f) => f.id);
  }
  function caseDetails(a) {
    const files = parse(a.evidence_json, []),
      p = parse(a.policy_json, {});
    return `<section class="after-case-detail"><dl class="service-definition"><div><dt>售后单号</dt><dd>${esc(a.id)}</dd></div><div><dt>关联订单</dt><dd>${esc(a.orderId || a.order_id)}</dd></div><div><dt>申请类型</dt><dd>${esc(a.type)}</dd></div><div><dt>申请金额</dt><dd>¥${(Number(a.amount_cents || 0) / 100).toFixed(2)}</dd></div><div><dt>申请原因</dt><dd>${esc(a.reason || '')}</dd></div><div><dt>寄回单号</dt><dd>${esc(a.return_tracking || '尚未提交')}</dd></div></dl>${files.length ? `<h3>申请图片凭证</h3><div class="svc-modal-actions">${files.map((f) => `<button class="outline" data-after-evidence="${esc(f.file_id)}" data-case="${esc(a.id)}">查看图片：${esc(f.file_name)}</button>`).join('')}</div>` : ''}<h3>处理与寄回说明</h3>${policy(p)}${p.return_address ? `<p>请在审核通过后寄回：${esc(p.return_recipient)} ${esc(p.return_phone)}<br>${esc(p.return_address)}</p>` : '<p>退货地址尚未配置，请在审核通过后联系客服确认，勿自行寄回。</p>'}<p class="secure-note">保存好包装与运单凭证，实际到账时间以原支付渠道为准。</p></section>`;
  }
  document.addEventListener('change', async (e) => {
 if(e.target.matches('#member-aftersale-form [name="type"]')){quantityLimits();void preview();return;}
    const form = e.target.closest('#member-aftersale-form');
    if (!form || current?.form !== form) return;
    if (e.target.name !== 'evidence_upload') {
      revision++;
      const el = document.getElementById('after-preview');
      if (el) el.innerHTML = '<p>商品或服务类型已变化，请重新核算退款。</p>';
      return;
    }
    const file = e.target.files?.[0],
      state = current,
      el = document.getElementById('after-evidence-status');
    if (!file) return;
    if (state.busy) {
      e.target.value = '';
      return;
    }
    try {
      if (state.files.length >= 5) throw Error('最多上传 5 张图片');
      if (!['image/png', 'image/jpeg', 'image/webp'].includes(file.type)) throw Error('仅支持 PNG、JPG、WebP 图片');
      state.busy = true;
      if (el) el.textContent = '正在上传并校验图片…';
      const result = await LumoServices.uploadAttachment(file);
      if (current !== state || state.scope !== scope() || !form.isConnected) return;
      state.files.push({ id: result.id, name: result.name, url: URL.createObjectURL(file) });
      evidenceView();
      if (el) el.textContent = `已上传 ${state.files.length} / 5 张图片`;
    } catch (err) {
      if (current === state && el) el.textContent = err.message;
    } finally {
      state.busy = false;
      e.target.value = '';
    }
  });
  document.addEventListener('click', async (e) => {
    if (e.target.closest('[data-after-preview]')) {
      await preview();
      return;
    }
    const remove = e.target.closest('[data-after-remove]');
    if (remove && current && !current.busy) {
      const i = Number(remove.dataset.afterRemove),
        file = current.files[i];
      if (file) {
        URL.revokeObjectURL(file.url);
        current.files.splice(i, 1);
        evidenceView();
      }
      return;
    }
    const b = e.target.closest('[data-after-evidence]');
    if (!b || b.disabled) return;
    b.disabled = true;
    try {
      const value = await LumoAPI.call('aftersale', 'evidence_download', {
        id: b.dataset.case,
        data: { file_id: b.dataset.afterEvidence },
      });
      if (!value.download_url) throw Error('图片暂不可下载');
      const a = document.createElement('a');
      a.href = value.download_url;
      a.target = '_blank';
      a.rel = 'noopener';
      a.click();
    } catch (err) {
      toast(err.message);
    } finally {
      b.disabled = false;
    }
  });
  window.addEventListener('lumo:logout', reset);
  window.LumoAftersale = { attach, evidenceFor, caseDetails };
})();
