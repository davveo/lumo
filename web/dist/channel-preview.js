/* Explicit, read-only storefront channel showcase, separate from authentication. */
(function () {
  'use strict';
  let tab = 'password',
    method = 'wechat',
    demoCode = false;
  const active = () => location.hash.startsWith('#/preview/');
  const names = { wechat: '微信支付', alipay: '支付宝', card: '银行卡', demo: '演示支付' };
  function note() {
    return '<div class="channel-preview-banner"><span>样例效果预览</span><p>用于查看渠道展示，使用的是示例账号和金额。</p><a href="admin.html">返回管理后台 ↗</a></div>';
  }
  function page(screen) {
    if (screen === 'login')
      return (
        note() +
        LumoAuthView.page({
          tab,
          methods: [
            { id: 'password', name: '账号密码' },
            { id: 'phone', name: '手机验证码' },
            { id: 'wechat', name: '微信' },
            { id: 'qq', name: 'QQ' },
          ],
          methodsLoaded: true,
          allowRegistration: true,
          remember: false,
          phone: '13800000000',
          code: '',
          otp: { demoCode: demoCode ? '123456' : '', remaining: 0 },
          busy: false,
          checking: false,
          error: '',
        })
      );
    return `${header()}<main id="main" class="container channel-payment-preview">${note()}${steps(1)}<section class="payment-wrap panel payment-panel"><span class="channel-preview-label">支付方式展示</span><h1>选择适合你的支付方式</h1><p class="payment-intro">LUMO 品质日常 · 示例订单</p><div class="payment-amount"><span>待付金额</span><div>${price(528)}</div><p>订单编号：PREVIEW_ORDER_1001</p></div><div class="payment-methods">${Object.entries(
      names,
    )
      .map(
        ([id, name]) =>
          `<button class="payment-method ${method === id ? 'active' : ''}" data-channel-method="${id}" aria-pressed="${method === id}"><span class="method-icon channel-icon-${id}">${{ wechat: '微', alipay: '支', card: '卡', demo: 'L' }[id]}</span>${name}<small>${method === id ? '已选择' : '可选择'}</small></button>`,
      )
      .join(
        '',
      )}</div><p class="demo-note">${method === 'card' ? '支持储蓄卡和信用卡，具体以正式渠道配置为准。' : method === 'demo' ? '演示渠道用于体验页面效果。' : '正式接入后可按所选渠道完成付款。'}</p><div class="payment-actions"><button class="primary" type="button" data-channel-pay>查看付款提示</button><a class="outline" href="admin.html">返回管理后台</a></div></section></main>${footer()}`;
  }
  function explain(title) {
    modal(
      title,
      '<div class="channel-preview-notice"><p>当前为样例效果预览。</p><p>实际登录或支付请返回商城，使用后台已经配置并启用的渠道。</p><div class="modal-actions"><button class="primary" data-action="close-modal">我知道了</button></div></div>',
    );
  }
  document.addEventListener(
    'click',
    (event) => {
      if (!active()) return;
      const b = event.target.closest('[data-auth-action],[data-channel-method],[data-channel-pay]');
      if (!b) return;
      event.preventDefault();
      event.stopImmediatePropagation();
      if (b.dataset.channelMethod) {
        method = b.dataset.channelMethod;
        renderKeepScroll();
        return;
      }
      const action = b.dataset.authAction;
      if (action === 'login-tab') {
        tab = b.dataset.id;
        renderKeepScroll();
        return;
      }
      if (action === 'send-otp') {
        demoCode = true;
        renderKeepScroll();
        return;
      }
      if (action === 'toggle-password') {
        const input = document.getElementById('auth-password');
        input.type = input.type === 'password' ? 'text' : 'password';
        return;
      }
      explain(b.hasAttribute('data-channel-pay') ? '付款提示预览' : '登录方式预览');
    },
    true,
  );
  document.addEventListener(
    'submit',
    (event) => {
      if (active() && event.target.id === 'auth-login-form') {
        event.preventDefault();
        event.stopImmediatePropagation();
        explain('登录提示预览');
      }
    },
    true,
  );
  window.LumoChannelPreview = { page, active };
})();
